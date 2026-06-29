package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
// For each ADIF record it calls qsoservice.SubmitImport (validation +
// atomic write + audit, all inherited from the canonical path), which
// preserves a Station Manager UUID (APP_SM_QSO_ID) when present.
//
// Uploads default to OFF: import queues NO forwarder upload rows, so
// seeding a historical log never re-uploads it to QRZ/ClubLog
// (retrospective backfill is operator-driven, never automatic — ADR
// 0022; bulk-forwarding an already-stored log is the logbook SPA's job).
// The operator opts in per-forwarder with --forward (see below). The QRZ
// per-QSO LOGID (app_qrzlog_logid, on every QRZ export record) and the
// historical qrzcom_qso_upload_date/_status are preserved on the QSO row
// itself by RecordToQso, independent of any upload row.
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
	var forwardFwds string
	fs.StringVar(&configPath, "config", "", "path to config.json (default: $SM_WORKING_DIR, else the XDG data dir for a system install, else the executable's directory)")
	fs.Int64Var(&logbookID, "logbook", 0, "target logbook id (default: default_logbook from config)")
	fs.BoolVar(&dryRun, "dry-run", false, "parse and validate only — no DB writes")
	fs.IntVar(&progressEvery, "progress-every", 100, "emit a progress line every N records (0 disables)")
	fs.StringVar(&forwardFwds, "forward", "", "comma-separated forwarder name(s) to QUEUE the imported QSOs for upload to (e.g. \"qrz\"). DEFAULT IS NONE: import uploads nothing unless you name a forwarder here. Use only when you actually want the imported log pushed to that service.")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "usage: smd import [flags] <file.adi>")
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
		_, _ = fmt.Fprintf(os.Stdout, "dry-run: %s parses to %d record(s); no DB writes\n",
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
	// Reference-cache bean (reference.db) — qsoservice injects it for the
	// best-effort contacted_station upsert during import (reference.db / log-db
	// split). Without it the DI build can't satisfy qsoservice.RefDB.
	if err := container.Register(types.ReferenceDBServiceName, reflect.TypeFor[*sqlite.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register reference-db sqlite service")
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
			_, _ = fmt.Fprintf(os.Stderr, "smd import: logger close error: %v\n", cerr)
		}
	}()
	dbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.SqliteServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve sqlite service")
	}
	refDbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.ReferenceDBServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve reference-db sqlite service")
	}
	qsoSvc, err := iocdi.ResolveAs[*qsoservice.Service](container, qsoservice.ServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve qso service")
	}

	// reference.db / log-db split: log connection for qso/queue/history, a
	// reference connection for the enrichment caches qsoservice warms on import.
	dbSvc.SetMigrationSets(sqlite.MigrationSetLog)
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

	refDbSvc.SetMigrationSets(sqlite.MigrationSetReference)
	refDbSvc.SetDatabasePath(filepath.Join(filepath.Dir(dbSvc.DatabaseConfig.Path), referenceDBFilename))
	if err := refDbSvc.Open(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("open reference database")
	}
	defer func() {
		if cerr := refDbSvc.Close(); cerr != nil {
			loggerSvc.ErrorWith().Err(cerr).Msg("reference database close error")
		}
	}()
	if err := refDbSvc.Migrate(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("run reference migrations")
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

	// ---- --forward: forwarder names to QUEUE the imported QSOs for upload to.
	// DEFAULT IS NONE — import uploads nothing unless the operator opts in here,
	// so seeding a historical log never re-sends it (ADR 0022). Validate each
	// name against the configured forwarders (enabled or not — a row enqueued
	// for a disabled-but-present forwarder drains when it's re-enabled, ADR
	// 0022). Matched case-insensitively; passed to SubmitImport as forwardTo.
	var forwardTo []string
	if s := strings.TrimSpace(forwardFwds); s != "" {
		configured := map[string]bool{}
		for _, f := range cfg.Forwarders {
			configured[strings.ToLower(strings.TrimSpace(f.Name))] = true
		}
		for _, raw := range strings.Split(s, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if !configured[strings.ToLower(name)] {
				return errors.New(op).WithMsgf(
					"--forward: no forwarder named %q in config (check forwarders[].name)", name)
			}
			forwardTo = append(forwardTo, name)
		}
	}

	loggerSvc.InfoWith().
		Str("file", adifPath).
		Int("records", len(parsed.Records)).
		Int64("logbook_id", logbookID).
		Int("forward_forwarders", len(forwardTo)).
		Bool("dry_run", dryRun).
		Msg("import starting")

	// ---- Bulk import.
	// Normalize modes in place, then write through qsoservice.SubmitImportBatch:
	// chunked transactions + a hoisted logbook lookup + deduped contacted_station
	// upserts, far cheaper than per-record submit on a big logbook. Import
	// preserves each record's source UUID (APP_SM_QSO_ID) when valid (ADR 0016).
	// forwardTo is empty by default, so no upload rows are enqueued unless
	// --forward named a forwarder.
	for i := range parsed.Records {
		normalizeImportedMode(&parsed.Records[i])
	}

	ctx := context.Background()
	total := len(parsed.Records)
	start := time.Now()

	res, ierr := qsoSvc.SubmitImportBatch(ctx, logbookID, parsed.Records, forwardTo, 0,
		func(done int, r qsoservice.ImportBatchResult) {
			if progressEvery <= 0 {
				return
			}
			rate := float64(done) / time.Since(start).Seconds()
			_, _ = fmt.Fprintf(os.Stderr, "  %d/%d (stored=%d, duplicate=%d, errors=%d) — %.1f rec/s\n",
				done, total, r.Stored, r.Duplicate, len(r.Errors), rate)
		})
	if ierr != nil {
		return errors.New(op).WithErr(ierr).WithMsg("bulk import failed")
	}

	elapsed := time.Since(start)
	_, _ = fmt.Fprintf(os.Stdout, "imported %d records in %s\n", total, elapsed.Round(time.Millisecond))
	_, _ = fmt.Fprintf(os.Stdout, "  stored:     %d\n", res.Stored)
	_, _ = fmt.Fprintf(os.Stdout, "  duplicate:  %d\n", res.Duplicate)
	_, _ = fmt.Fprintf(os.Stdout, "  errors:     %d\n", len(res.Errors))
	if len(forwardTo) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "  queued for upload to: %s\n", strings.Join(forwardTo, ", "))
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "  uploads: none (use --forward to queue a forwarder)")
	}
	if len(res.Errors) > 0 {
		_, _ = fmt.Fprintln(os.Stdout, "errored records (first 20):")
		limit := len(res.Errors)
		if limit > 20 {
			limit = 20
		}
		for _, e := range res.Errors[:limit] {
			_, _ = fmt.Fprintf(os.Stdout, "  #%d %s: %s\n", e.Index, e.Call, e.Reason)
		}
		return errors.New(op).WithMsgf("%d record(s) failed to import", len(res.Errors))
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
