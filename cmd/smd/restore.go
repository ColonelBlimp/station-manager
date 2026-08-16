package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// runRestore is the entry point for `smd restore` — the SM Cloud restore
// path (ADR 0040 S5): pull GET /v1/export from the operator's smcloud
// service and insert every QSO of one cloud logbook into a LOCAL logbook
// with UUID + additional_data + modified_at + tombstones preserved. NEVER an
// ADIF re-import (which mints new UUIDs and flattens additional_data).
//
// Credentials come from the config's smcloud forwarder entry (enabled or
// not — restore only reads the cloud), so a freshly set-up install restores
// with zero extra flags once the forwarder is configured. The daemon must
// not be running (sqlite single-writer), same as `smd import`.
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
	fs.StringVar(&forwarderName, "forwarder", "", "config name of the smcloud forwarder to read url/token from (default: the first type=\"smcloud\" entry)")
	fs.StringVar(&cloudLogbook, "cloud-logbook", "", "cloud-side logbook name to restore from (default: the forwarder's configured logbook)")
	fs.Int64Var(&logbookID, "logbook", 0, "LOCAL target logbook id (default: default_logbook from config)")
	fs.BoolVar(&dryRun, "dry-run", false, "fetch the export and report what would be restored — no DB writes")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "usage: smd restore [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// ---- Config + the smcloud forwarder entry (the credential source).
	cfg, _, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	cfgSvc := config.New(cfg)

	var fc *types.ForwarderConfig
	for i := range cfg.Forwarders {
		f := &cfg.Forwarders[i]
		if f.Type != smcloud.Type {
			continue
		}
		if forwarderName == "" || strings.EqualFold(f.Name, forwarderName) {
			fc = f
			break
		}
	}
	if fc == nil {
		return errors.New(op).WithMsg("no smcloud forwarder in config.forwarders — add one (url + token) before restoring")
	}
	if cloudLogbook == "" {
		if cloudLogbook, err = smcloud.CloudLogbookName(*fc); err != nil {
			return errors.New(op).WithErr(err)
		}
	}

	// ---- Pull the export.
	_, _ = fmt.Fprintf(os.Stderr, "fetching export via forwarder %q…\n", fc.Name)
	export, err := smcloud.FetchExport(context.Background(), *fc)
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

	logDBDir := filepath.Dir(dbSvc.DatabaseConfig.Path)
	refPath := filepath.Join(logDBDir, referenceDBFilename)
	if err := sqlite.BootstrapReferenceSplit(
		dbSvc.DatabaseConfig.Path, refPath, filepath.Join(logDBDir, "backups"), loggerSvc,
	); err != nil {
		return errors.New(op).WithErr(err).WithMsg("bootstrap reference split")
	}
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
	refDbSvc.SetDatabasePath(refPath)
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

	// ST-6: restore opens/writes the same databases, so it must make them owner-private too
	// (else a permissive-umask restore leaves readable QSO data until a later daemon start).
	if err := sqlite.SecureDataFiles(cfgSvc.WorkingDir(), filepath.Join(logDBDir, "backups"),
		loggerSvc, dbSvc.DatabaseConfig.Path, refPath); err != nil {
		return errors.New(op).WithErr(err).WithMsg("secure database files")
	}

	// ---- Resolve the LOCAL target logbook.
	if logbookID == 0 {
		logbookID = cfg.DefaultLogbookID
	}
	if logbookID < 1 {
		return errors.New(op).WithMsg("no target logbook (config has no default_logbook_id; pass -logbook)")
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
