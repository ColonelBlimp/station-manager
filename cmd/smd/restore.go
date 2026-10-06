package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// fetchSMCloudExport is the restore command's network boundary. Kept as a seam
// so command tests can exercise the real restore/database/close path offline.
var fetchSMCloudExport = smcloud.FetchExport

// runRestore is the entry point for `smd restore` — the SM Cloud restore
// path (ADR 0040 S5): pull GET /v1/export from the operator's smcloud
// service and insert every QSO of one cloud logbook into a LOCAL logbook
// with UUID + additional_data + modified_at + tombstones preserved. NEVER an
// ADIF re-import (which mints new UUIDs and flattens additional_data).
//
// Credentials come from the SM Cloud station account in config.json (url and
// token). The cloud logbook comes from Home's default-logbook SM Cloud binding,
// read from Home's closed file — enabled or not: restoring only reads the cloud
// and needs no upload consent (config v6, W-0021 5C ruling R2). --forwarder
// names another SM Cloud binding in Home; --cloud-logbook names the cloud
// logbook outright and reads no archive file at all. When neither resolves,
// restore asks for --cloud-logbook rather than guessing. The daemon must not
// be running (sqlite single-writer), same as `smd import`.
//
// Existing rows (by UUID, tombstones included) are SKIPPED — re-running is
// idempotent and restore never overwrites; healing a diverged existing row
// is the reconciler's job. No upload rows are queued (the cloud already
// holds these QSOs) and no enrichment runs (the stored fields ARE the
// enrichment).
func runRestore(args []string) error {
	const op errors.Op = "smd.restore"

	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	var configPath, forwarderName, cloudLogbook string
	var logbookID int64
	var dryRun bool
	fs.StringVar(&configPath, "config", "", "path to config.json (default: $SM_WORKING_DIR, else the XDG data dir, else the executable's directory)")
	fs.StringVar(&forwarderName, "forwarder", "", "name of an SM Cloud destination binding in the Home archive whose cloud logbook to restore (default: the binding on Home's default logbook)")
	fs.StringVar(&cloudLogbook, "cloud-logbook", "", "cloud-side logbook name to restore from, overriding any binding (reads no archive file)")
	fs.Int64Var(&logbookID, "logbook", 0, "LOCAL target logbook id (default: the target archive's default logbook)")
	archiveID := fs.String("archive", "", "catalogue id (uuid) of the archive to restore into (default: the active archive); run with the daemon stopped")
	fs.BoolVar(&dryRun, "dry-run", false, "fetch the export and report what would be restored — no DB writes")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "usage: smd restore [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// ---- Config, the SM Cloud station account and the cloud logbook.
	cfg, _, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	cfgSvc := config.New(cfg)

	fc, via, err := restoreSource(cfg, forwarderName, &cloudLogbook)
	if err != nil {
		return errors.New(op).WithErr(err)
	}

	// ---- Pull the export.
	_, _ = fmt.Fprintf(os.Stderr, "fetching export via %s…\n", via)
	export, err := fetchSMCloudExport(context.Background(), fc)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("fetch export")
	}

	var cloudLogbookID int64
	for _, lb := range export.Logbooks {
		if lb.Name == cloudLogbook {
			cloudLogbookID = lb.ID
			break
		}
	}
	if cloudLogbookID == 0 {
		names := make([]string, 0, len(export.Logbooks))
		for _, lb := range export.Logbooks {
			names = append(names, lb.Name)
		}
		return errors.New(op).WithMsgf("cloud logbook %q not found (cloud has: %s)",
			cloudLogbook, strings.Join(names, ", "))
	}
	var records []smcloud.ExportRecord
	tombstones := 0
	for _, r := range export.Qsos {
		if r.LogbookID != cloudLogbookID {
			continue
		}
		if r.DeletedAt != nil {
			tombstones++
		}
		records = append(records, r)
	}
	_, _ = fmt.Fprintf(os.Stderr, "export: %d record(s) in cloud logbook %q (%d tombstone(s))\n",
		len(records), cloudLogbook, tombstones)

	if dryRun {
		_, _ = fmt.Fprintln(os.Stdout, "dry-run: no DB writes")
		return nil
	}

	// ---- Minimal DI container (same shape as `smd import`): config +
	// logging + sqlite (+ reference bean qsoservice's DI needs) + qsoservice.
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
			_, _ = fmt.Fprintf(os.Stderr, "smd restore: logger close error: %v\n", cerr)
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

	paths, closeDBs, err := openArchiveDatabases(cfg, cfgSvc, *archiveID, dbSvc, refDbSvc, loggerSvc, os.Stdout)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	defer closeArchiveDatabasesAndRecordSummary(cfg, paths, dbSvc, closeDBs, loggerSvc)

	// ---- Resolve the LOCAL target logbook.
	if logbookID, err = targetLogbook(logbookID, dbSvc, paths, cfg); err != nil {
		return errors.New(op).WithErr(err)
	}
	if _, ferr := dbSvc.FetchLogbookByIDWithContext(context.Background(), logbookID); ferr != nil {
		return errors.New(op).WithErr(ferr).WithMsgf("target logbook id=%d does not exist", logbookID)
	}

	// ---- Restore loop: per-record best-effort, errors reported at the end.
	ctx := context.Background()
	start := time.Now()
	var stored, skipped, failed int
	var firstErrs []string
	for i, r := range records {
		var qso types.Qso
		if err := json.Unmarshal(r.Qso, &qso); err != nil {
			failed++
			if len(firstErrs) < 20 {
				firstErrs = append(firstErrs, fmt.Sprintf("#%d %s: payload: %v", i, r.UUID, err))
			}
			continue
		}
		qso.ModifiedAt = r.ModifiedAt
		qso.Revision = r.Revision
		if r.DeletedAt != nil {
			qso.DeletedAt = *r.DeletedAt
		}
		status, err := qsoSvc.Restore(ctx, logbookID, qso)
		switch {
		case err != nil:
			failed++
			if len(firstErrs) < 20 {
				firstErrs = append(firstErrs, fmt.Sprintf("#%d %s: %v", i, r.UUID, err))
			}
		case status == qsoservice.RestoreSkippedExisting:
			skipped++
		default:
			stored++
		}
		if (i+1)%500 == 0 {
			_, _ = fmt.Fprintf(os.Stderr, "  %d/%d (stored=%d, skipped=%d, failed=%d)\n",
				i+1, len(records), stored, skipped, failed)
		}
	}

	// The DURABLE run summary (logging-gaps Q1): stdout dies with the terminal,
	// and a restore is the record least able to rely on anything else having
	// survived — an idempotent re-run and a real recovery must stay tellable
	// apart in smd.log after the fact. Per-row outcomes are the service's
	// Debug lines; this is the one-line Info account of the run.
	loggerSvc.InfoWith().
		Int("requested", len(records)).
		Int("stored", stored).
		Int("skipped_existing", skipped).
		Int("failed", failed).
		Int64("logbook_id", logbookID).
		Msg("restore: run complete")

	_, _ = fmt.Fprintf(os.Stdout, "restored %d record(s) in %s\n", len(records), time.Since(start).Round(time.Millisecond))
	_, _ = fmt.Fprintf(os.Stdout, "  stored:            %d\n", stored)
	_, _ = fmt.Fprintf(os.Stdout, "  skipped (existing): %d\n", skipped)
	_, _ = fmt.Fprintf(os.Stdout, "  failed:            %d\n", failed)
	if len(firstErrs) > 0 {
		_, _ = fmt.Fprintln(os.Stdout, "errored records (first 20):")
		for _, e := range firstErrs {
			_, _ = fmt.Fprintf(os.Stdout, "  %s\n", e)
		}
		return errors.New(op).WithMsgf("%d record(s) failed to restore", failed)
	}
	return nil
}

// restoreSource resolves what a restore reads: the SM Cloud station account
// (url, token) and, unless *cloudLogbook is already set, the cloud logbook
// named by a Home binding (R2). It returns the account to fetch with and a
// description of where the logbook came from.
func restoreSource(cfg config.Config, forwarderName string, cloudLogbook *string) (types.ForwarderConfig, string, error) {
	var account types.ForwarderConfig
	found := false
	for _, f := range cfg.Forwarders {
		if f.Type == smcloud.Type {
			account, found = f, true
			break
		}
	}
	if !found {
		return types.ForwarderConfig{}, "", fmt.Errorf("no SM Cloud station account in config.json — add one (url + token) before restoring")
	}
	if *cloudLogbook != "" {
		if forwarderName != "" {
			return types.ForwarderConfig{}, "", fmt.Errorf("--forwarder and --cloud-logbook both name the cloud logbook; give one")
		}
		return account, "the SM Cloud station account", nil
	}
	binding, err := restoreBinding(cfg, forwarderName)
	if err != nil {
		return types.ForwarderConfig{}, "", err
	}
	fc, err := forwarding.BindingConfig(binding, account)
	if err != nil {
		return types.ForwarderConfig{}, "", err
	}
	if *cloudLogbook, err = smcloud.CloudLogbookName(fc); err != nil {
		return types.ForwarderConfig{}, "", err
	}
	return account, fmt.Sprintf("the SM Cloud station account (binding %q)", binding.ForwarderName), nil
}

// restoreBinding finds the SM Cloud binding in Home that names the cloud
// logbook: the one --forwarder names (case-insensitively), else the one on
// Home's default logbook. Either may be disabled. Neither found asks for
// --cloud-logbook.
func restoreBinding(cfg config.Config, forwarderName string) (types.LogbookDestination, error) {
	const ask = "; name the cloud logbook with --cloud-logbook"
	peek, err := archive.ReadHomeBindings(cfg)
	if err != nil {
		return types.LogbookDestination{}, fmt.Errorf("%w%s", err, ask)
	}
	if forwarderName != "" {
		for _, b := range peek.Bindings {
			if b.Destination == smcloud.Type && strings.EqualFold(b.ForwarderName, forwarderName) {
				return b, nil
			}
		}
		return types.LogbookDestination{}, fmt.Errorf("no SM Cloud destination binding named %q in the Home archive%s", forwarderName, ask)
	}
	defaultID := cfg.DefaultLogbookID
	if peek.HasIdentity && peek.Identity.DefaultLogbookID > 0 {
		defaultID = peek.Identity.DefaultLogbookID
	}
	for _, b := range peek.Bindings {
		if b.Destination == smcloud.Type && b.LogbookID == defaultID {
			return b, nil
		}
	}
	return types.LogbookDestination{}, fmt.Errorf("the Home archive's default logbook has no SM Cloud destination binding%s", ask)
}
