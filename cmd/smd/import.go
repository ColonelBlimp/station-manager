package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// runImport is the entry point for `smd import <file.adi>`.
//
// Wires a minimal DI container — config + logging + sqlite +
// qsoservice + events.Hub — without the HTTP server, forwarder
// workers, bridge, enrichment pipeline, or mailer. The daemon must
// not be running when this runs (sqlite single-writer); the install-
// day sequence stops the user service first.
//
// For each ADIF record:
//
//   - calls qsoservice.Submit (validation + atomic write + audit
//     all inherited from the canonical path),
//   - if the record carries app_qrzlog_logid + qrzcom_qso_upload_status=Y
//     AND a QRZ forwarder is configured, stamps the QRZ qso_upload
//     row as success with upstream_id = the LOGID (the QSO row's
//     additional_data already carries qrzcom_qso_upload_date /
//     _status from the parser, so the historical dates are preserved
//     without a daemon-side rewrite).
//
// Prints a summary line at the end and exits 0 on full success or 1
// if errors > 0. Errors are individual per-record failures (bad
// data, DB constraint) — the import keeps going past them so a
// single malformed record doesn't sink the rest.
func runImport(args []string) error {
	const op errors.Op = "smd.import"

	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	var configPath string
	var logbookID int64
	var dryRun bool
	var progressEvery int
	fs.StringVar(&configPath, "config", "", "path to config.json (default: $SM_WORKING_DIR/config.json or ./config.json)")
	fs.Int64Var(&logbookID, "logbook", 0, "target logbook id (default: default_logbook from config)")
	fs.BoolVar(&dryRun, "dry-run", false, "parse and validate only — no DB writes")
	fs.IntVar(&progressEvery, "progress-every", 100, "emit a progress line every N records (0 disables)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: smd import [flags] <file.adi>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New(op).WithMsg("expected exactly one ADIF file path")
	}
	adifPath := fs.Arg(0)

	// ---- Load + validate the ADIF file before touching anything else.
	// Parser is liberal; a broken file might still parse to zero records
	// but that's the loud failure we want here, not silently importing
	// nothing.
	raw, err := os.ReadFile(adifPath)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("read %s", adifPath)
	}
	parsed, err := adif.Parse(raw)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("parse ADIF")
	}
	if len(parsed.Records) == 0 {
		return errors.New(op).WithMsgf("%s contains no records", adifPath)
	}

	// Dry-run is a file-sanity mode: confirm the ADIF parses, count
	// records, exit. No DB touches, no logbook checks, no config
	// validation. Useful before the install-day import to verify the
	// QRZ export downloaded cleanly.
	if dryRun {
		fmt.Fprintf(os.Stdout, "dry-run: %s parses to %d record(s); no DB writes\n",
			adifPath, len(parsed.Records))
		return nil
	}

	// ---- Load config (same precedence as the daemon path).
	cfg, _, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	cfgSvc := config.New(cfg)

	// ---- Optional modes override, exactly as the daemon does at boot.
	// Keeps the import's validation surface aligned with the live
	// daemon's, so a mode that imports fine here will also live-log fine.
	if err := modes.LoadOverride(cfg.DataDir); err != nil {
		return errors.New(op).WithErr(err).WithMsg("load modes override")
	}

	// ---- Build minimal DI container.
	container := iocdi.New()
	hub := events.NewHub()
	if err := container.RegisterInstance(config.ServiceName, cfgSvc); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register config service")
	}
	if err := container.RegisterInstance(events.ServiceName, hub); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register event hub")
	}
	if err := container.Register(logging.ServiceName, reflect.TypeFor[*logging.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register logging service")
	}
	if err := container.Register(types.SqliteServiceName, reflect.TypeFor[*sqlite.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register sqlite service")
	}
	if err := container.Register(qsoservice.ServiceName, reflect.TypeFor[*qsoservice.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register qso service")
	}

	iocdi.SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		if id == "workingdir" && targetType.Kind() == reflect.String {
			return cfgSvc.WorkingDir(), true, nil
		}
		return nil, false, nil
	})

	if err := container.Build(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("build container")
	}

	loggerSvc, err := iocdi.ResolveAs[*logging.Service](container, logging.ServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve logging service")
	}
	defer func() {
		if cerr := loggerSvc.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "smd import: logger close error: %v\n", cerr)
		}
	}()
	dbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.SqliteServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve sqlite service")
	}
	qsoSvc, err := iocdi.ResolveAs[*qsoservice.Service](container, qsoservice.ServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve qso service")
	}

	if err := dbSvc.Open(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("open database")
	}
	defer func() {
		if cerr := dbSvc.Close(); cerr != nil {
			loggerSvc.ErrorWith().Err(cerr).Msg("database close error")
		}
	}()
	if err := dbSvc.Migrate(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("run migrations")
	}

	// ---- Resolve target logbook.
	if logbookID == 0 {
		logbookID = cfg.DefaultLogbookID
	}
	if logbookID < 1 {
		return errors.New(op).WithMsg("no target logbook (config has no default_logbook_id; pass --logbook)")
	}
	if _, ferr := dbSvc.FetchLogbookByIDWithContext(context.Background(), logbookID); ferr != nil {
		return errors.New(op).WithErr(ferr).WithMsgf("target logbook id=%d does not exist", logbookID)
	}

	// ---- Detect QRZ forwarder presence. Records with an LOGID get the
	// pre-success stamp only when the QRZ forwarder is configured at
	// import time (otherwise no qso_upload row exists to stamp). A
	// missing forwarder is a warning, not an error — the QSO still
	// imports cleanly, just without the upstream-id metadata that a
	// future PATCH/DELETE would need.
	qrzConfigured := false
	for _, f := range cfg.Forwarders {
		if !f.Enabled {
			continue
		}
		if strings.EqualFold(f.Type, "qrz") {
			qrzConfigured = true
			break
		}
	}

	loggerSvc.InfoWith().
		Str("file", adifPath).
		Int("records", len(parsed.Records)).
		Int64("logbook_id", logbookID).
		Bool("qrz_forwarder_configured", qrzConfigured).
		Bool("dry_run", dryRun).
		Msg("import starting")

	// ---- Iterate records.
	type errLine struct {
		index  int
		call   string
		reason string
	}
	stored := 0
	duplicate := 0
	stampSkipped := 0
	var errLines []errLine

	ctx := context.Background()
	start := time.Now()

	for i, rec := range parsed.Records {
		normalizeImportedMode(&rec)
		// Import preserves the source UUID (APP_SM_QSO_ID) when present + valid,
		// so a Station Manager export restores its canonical identities (ADR
		// 0016; review 2026-06-04 H1). The public POST /v1/qso path uses Submit
		// (always mints) instead.
		res, serr := qsoSvc.SubmitImport(ctx, logbookID, rec, false)
		if serr != nil {
			if sub := qsoservice.IsSubmitError(serr); sub != nil {
				errLines = append(errLines, errLine{
					index:  i,
					call:   rec.Call,
					reason: sub.Code + ": " + sub.Message,
				})
			} else {
				errLines = append(errLines, errLine{
					index:  i,
					call:   rec.Call,
					reason: serr.Error(),
				})
			}
			continue
		}
		if res.Status == "duplicate" {
			duplicate++
		} else {
			stored++
			// Pre-stamp the QRZ qso_upload row IFF we have both a LOGID
			// AND the QRZ forwarder is configured at import time. The
			// historical qrzcom_qso_upload_status/_date values on the
			// QSO row come from RecordToQso, so we don't touch them.
			if qrzConfigured && rec.AppQrzlogLogid != "" && rec.QrzComQsoUploadStatus == adif.YesString {
				if serr := stampQrzUpload(ctx, dbSvc, res.ID, rec.AppQrzlogLogid); serr != nil {
					loggerSvc.WarnWith().
						Err(serr).
						Str("call", rec.Call).
						Str("logid", rec.AppQrzlogLogid).
						Int64("qso_id", res.ID).
						Msg("could not stamp QRZ upload row")
					stampSkipped++
				}
			}
		}

		if progressEvery > 0 && (i+1)%progressEvery == 0 {
			elapsed := time.Since(start)
			rate := float64(i+1) / elapsed.Seconds()
			fmt.Fprintf(os.Stderr, "  %d/%d (stored=%d, duplicate=%d, errors=%d) — %.1f rec/s\n",
				i+1, len(parsed.Records), stored, duplicate, len(errLines), rate)
		}
	}

	elapsed := time.Since(start)
	fmt.Fprintf(os.Stdout, "imported %d records in %s\n", len(parsed.Records), elapsed.Round(time.Millisecond))
	fmt.Fprintf(os.Stdout, "  stored:     %d\n", stored)
	fmt.Fprintf(os.Stdout, "  duplicate:  %d\n", duplicate)
	fmt.Fprintf(os.Stdout, "  errors:     %d\n", len(errLines))
	if stampSkipped > 0 {
		fmt.Fprintf(os.Stdout, "  qrz-stamp-skipped: %d (stored, but qso_upload row not found / update failed; forwarder retry will sort it)\n", stampSkipped)
	}
	if len(errLines) > 0 {
		fmt.Fprintln(os.Stdout, "errored records (first 20):")
		limit := len(errLines)
		if limit > 20 {
			limit = 20
		}
		for _, e := range errLines[:limit] {
			fmt.Fprintf(os.Stdout, "  #%d %s: %s\n", e.index, e.call, e.reason)
		}
		return errors.New(op).WithMsgf("%d record(s) failed to import", len(errLines))
	}
	return nil
}

// normalizeImportedMode handles loggers that emit a submode as MODE
// — most notably QRZ, which exports `MODE=USB` (or LSB / CW-U /
// DATA-U) where the ADIF spec wants `MODE=SSB SUBMODE=USB`. The strict
// validator in qsoservice.Submit rejects the submode-as-mode shape,
// so the import path massages the record back to the canonical pair
// before submission. No-op when MODE is already a valid main mode or
// when MODE is empty (the daemon's existing fallback path handles the
// MODE-empty / SUBMODE-present case).
func normalizeImportedMode(rec *adif.Record) {
	if rec.Mode == "" {
		return
	}
	upper := strings.ToUpper(strings.TrimSpace(rec.Mode))
	if modes.IsValidMode(upper) {
		return
	}
	parent, ok := modes.GetModeBySubmode(upper)
	if !ok {
		return // not a known submode either — let Submit's validator surface the error
	}
	if rec.Submode == "" {
		rec.Submode = upper
	}
	rec.Mode = parent.String()
}

// stampQrzUpload finds the QRZ qso_upload row for the just-stored QSO
// and stamps it as success with upstream_id = logid. Returns
// ErrNotFound when no QRZ row exists for the QSO (e.g. the operator
// disabled the forwarder between Submit and this call; harmless).
func stampQrzUpload(ctx context.Context, db *sqlite.Service, qsoID int64, logid string) error {
	const op errors.Op = "smd.import.stampQrzUpload"
	rows, err := db.FetchUploadsByQsoIDWithContext(ctx, qsoID)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("fetch uploads")
	}
	for _, r := range rows {
		if strings.EqualFold(r.ForwarderType, "qrz") {
			return db.MarkUploadSuccessWithContext(ctx, r.ID, logid)
		}
	}
	return errors.ErrNotFound
}
