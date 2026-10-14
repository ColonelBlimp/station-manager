package main

import (
	"context"
	"encoding/json"
	stderr "errors"
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
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The restore command's network boundaries: the whole-tenant export (the
// legacy, by-name source) and one logbook's scoped export (by identity). Kept
// as seams so command tests can exercise the real restore/database/close path
// offline.
var (
	fetchSMCloudExport = smcloud.FetchExport
	fetchScopedExport  = smcloud.FetchScopedExport
)

// runRestore is the entry point for `smd restore` — the SM Cloud restore
// path (ADR 0040 S5): insert a cloud logbook's QSOs into a LOCAL logbook with
// UUID + additional_data + modified_at + revision + tombstones preserved.
// NEVER an ADIF re-import (which mints new UUIDs and flattens additional_data).
//
// By default the source is the target logbook's OWN cloud logbook, addressed
// by the target archive's catalogue id and the logbook's UUID through the
// scoped export (W-0021 5F.5, ADR 0088 refinement 2026-10-13, rulings K1–K6):
// in every archive, Home included; no binding is needed, and none is created,
// enabled or stamped. The server's identity support is checked first and
// nothing falls back. --cloud-logbook NAME selects the legacy, by-name source
// (the whole-tenant export) instead, refused for a target whose SM Cloud
// binding is born on identity (ADR 0092 refinement). --forwarder is retired.
//
// Credentials come from the SM Cloud station account in config.json (url and
// token); restoring only reads the cloud and needs no upload consent. The
// target is resolved, and every refusal made, from the closed file read-only;
// the file is opened for writing only after the export was accepted, and
// never on --dry-run. The daemon must not be running (sqlite single-writer),
// same as `smd import`.
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
	fs.StringVar(&forwarderName, "forwarder", "", "retired: name the legacy cloud logbook with --cloud-logbook NAME instead")
	fs.StringVar(&cloudLogbook, "cloud-logbook", "", "restore a LEGACY cloud logbook by its cloud-side name, through the whole-tenant export (default: the target logbook's own cloud logbook, by identity)")
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
	if forwarderName != "" {
		return errors.New(op).WithMsg("--forwarder is retired: a restore no longer reads a destination binding. " +
			"With no flag, restore takes the target logbook's own cloud logbook by identity; to restore a legacy cloud " +
			"logbook, pass --cloud-logbook NAME, where NAME is the cloud logbook's name (as the SM Cloud server lists it), not a binding's name")
	}

	// ---- Config, the SM Cloud station account and the target, read-only.
	cfg, _, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	cfgSvc := config.New(cfg)
	account, ok := smcloudAccount(cfg) // enabled or not: restoring only reads
	if !ok {
		return errors.New(op).WithMsg("no SM Cloud station account in config.json — add one (url + token) before restoring")
	}
	paths, err := resolveArchivePaths(cfg, *archiveID, os.Stdout)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	legacy := cloudLogbook != ""
	target, err := restoreTarget(cfg, paths, logbookID, legacy, dryRun)
	if err != nil {
		return errors.New(op).WithErr(err)
	}

	// ---- Pull and accept the export.
	ctx := context.Background()
	var records []smcloud.ScopedExportRecord
	var source string
	if legacy {
		records, err = legacyRecords(ctx, account, cloudLogbook)
		source = fmt.Sprintf("cloud logbook %q", cloudLogbook)
	} else {
		records, err = identityRecords(ctx, account, paths.Entry.ID, target.UUID)
		source = fmt.Sprintf("cloud archive %s logbook %s", paths.Entry.ID, target.UUID)
	}
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	tombstones := 0
	for _, r := range records {
		if r.DeletedAt != nil {
			tombstones++
		}
	}
	_, _ = fmt.Fprintf(os.Stderr, "export: %d record(s) in %s (%d tombstone(s))\n", len(records), source, tombstones)

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

	// ---- Open for writing: the export was accepted.
	paths, closeDBs, err := openArchiveDatabases(cfg, cfgSvc, *archiveID, dbSvc, refDbSvc, loggerSvc, nil)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	defer closeArchiveDatabasesAndRecordSummary(cfg, paths, dbSvc, closeDBs, loggerSvc)
	logbookID = target.ID
	if err := checkRestoreTarget(ctx, dbSvc, paths, target, legacy); err != nil {
		return errors.New(op).WithErr(err)
	}

	// ---- Restore loop: per-record best-effort, errors reported at the end.
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

// restoreTarget resolves the LOCAL target logbook from the closed archive file,
// read-only (ruling K1 and the read-only amendment): no migration, no file
// created, no summary written. The identity path needs an adopted archive and
// the logbook's UUID. The legacy path refuses a target whose SM Cloud binding is
// born on identity, enabled or not, in every archive (ruling K4); a dry-run of it
// against a missing file reads nothing (that file holds no binding), and a real
// one is refused when the file is opened.
func restoreTarget(cfg config.Config, paths archive.Paths, requested int64, legacy, dryRun bool) (types.Logbook, error) {
	if !legacy && paths.Entry == nil {
		return types.Logbook{}, fmt.Errorf("this install has no archive identity yet (the daemon adopts Home at its next start), so there is no " +
			"cloud logbook to restore by identity; to restore a legacy cloud logbook, pass --cloud-logbook NAME")
	}
	if _, err := os.Stat(paths.QSO); legacy && dryRun && os.IsNotExist(err) {
		id, err := legacyTargetID(requested, paths, cfg, sqlite.PeekedBindings{})
		return types.Logbook{ID: id}, err
	}
	if err := verifyArchiveIdentity(paths); err != nil {
		return types.Logbook{}, err
	}
	peek, err := sqlite.PeekDestinationBindings(paths.QSO)
	if err != nil {
		return types.Logbook{}, fmt.Errorf("read the target archive read-only: %w", err)
	}
	if legacy {
		id, err := legacyTargetID(requested, paths, cfg, peek)
		if err != nil {
			return types.Logbook{}, err
		}
		for _, b := range peek.Bindings {
			if b.LogbookID == id && b.Destination == smcloud.Type && b.IdentitySince != nil {
				return types.Logbook{}, stampedRefusal(id, paths)
			}
		}
		return types.Logbook{ID: id}, nil
	}
	id := requested
	if id == 0 {
		if id, err = legacyTargetID(0, paths, cfg, peek); err != nil {
			return types.Logbook{}, err
		}
	}
	for _, lb := range peek.Logbooks {
		if lb.ID == id {
			if lb.UUID == "" {
				return types.Logbook{}, fmt.Errorf("target logbook id=%d has no UUID yet; start the daemon once, then restore", id)
			}
			return lb, nil
		}
	}
	return types.Logbook{}, fmt.Errorf("target logbook id=%d does not exist in archive %s (%s)", id, paths.Entry.Label, paths.Entry.ID)
}

// legacyTargetID is the --logbook value, else the archive's own default from
// its identity row, else config's projection where that applies.
func legacyTargetID(requested int64, paths archive.Paths, cfg config.Config, peek sqlite.PeekedBindings) (int64, error) {
	switch {
	case requested != 0:
		return requested, nil
	case peek.HasIdentity && peek.Identity.DefaultLogbookID > 0:
		return peek.Identity.DefaultLogbookID, nil
	}
	return configDefaultLogbook(paths, cfg)
}

func stampedRefusal(logbookID int64, paths archive.Paths) error {
	where := "this archive"
	if paths.Entry != nil {
		where = fmt.Sprintf("archive %s (%s)", paths.Entry.Label, paths.Entry.ID)
	}
	return fmt.Errorf("logbook id=%d in %s is backed up to SM Cloud by identity, so a legacy restore by name into it is refused: "+
		"its QSOs would keep the legacy cloud's UUIDs, and an edit would later move those cloud rows. "+
		"Restore it by identity instead: run smd restore without --cloud-logbook", logbookID, where)
}

// identityRecords is the identity path's source (rulings K1, K5): the server
// must answer the bounded support check as supported, with no error, before
// the scoped export is requested; nothing falls back to the whole-tenant
// export. The preflight cannot prove the server is unchanged by the time the
// export is requested, so a 404 is reported as what it is and nothing more.
func identityRecords(ctx context.Context, account types.ForwarderConfig, archiveUUID, logbookUUID string) ([]smcloud.ScopedExportRecord, error) {
	const nothing = "; nothing was restored"
	res, err := checkSmcloudSupport(ctx, account)
	switch {
	case err == nil && res.State == smcloud.SupportSupported:
	case err == nil:
		return nil, fmt.Errorf("the SM Cloud server does not report archive identity support, which a restore by identity needs%s. "+
			"A legacy cloud logbook is restored by its name with --cloud-logbook NAME", nothing)
	case res.State == smcloud.SupportUnverified:
		return nil, fmt.Errorf("the SM Cloud server could not be reached to check its archive identity support (%v)%s", err, nothing)
	default:
		return nil, fmt.Errorf("the SM Cloud server's archive identity support could not be confirmed (%v)%s", err, nothing)
	}
	_, _ = fmt.Fprintf(os.Stderr, "fetching cloud archive %s logbook %s…\n", archiveUUID, logbookUUID)
	export, err := fetchScopedExport(ctx, account, archiveUUID, logbookUUID)
	switch {
	case stderr.Is(err, smcloud.ErrScopedLogbookNotFound):
		return nil, fmt.Errorf("the SM Cloud server has no logbook %s in archive %s (HTTP 404)%s. "+
			"If this logbook's QSOs were only ever uploaded by name, restore them with --cloud-logbook NAME", logbookUUID, archiveUUID, nothing)
	case err != nil:
		return nil, fmt.Errorf("fetch the scoped export: %w", err)
	}
	return export.Qsos, nil
}

// legacyRecords is the --cloud-logbook source: the whole-tenant export,
// filtered to the cloud logbook of that name.
func legacyRecords(ctx context.Context, account types.ForwarderConfig, cloudLogbook string) ([]smcloud.ScopedExportRecord, error) {
	_, _ = fmt.Fprintln(os.Stderr, "fetching export via the SM Cloud station account…")
	export, err := fetchSMCloudExport(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("fetch export: %w", err)
	}
	var cloudLogbookID int64
	names := make([]string, 0, len(export.Logbooks))
	for _, lb := range export.Logbooks {
		names = append(names, lb.Name)
		if lb.Name == cloudLogbook && cloudLogbookID == 0 {
			cloudLogbookID = lb.ID
		}
	}
	if cloudLogbookID == 0 {
		return nil, fmt.Errorf("cloud logbook %q not found (cloud has: %s)", cloudLogbook, strings.Join(names, ", "))
	}
	var records []smcloud.ScopedExportRecord
	for _, r := range export.Qsos {
		if r.LogbookID == cloudLogbookID {
			records = append(records, smcloud.ScopedExportRecord{UUID: r.UUID, ModifiedAt: r.ModifiedAt, Revision: r.Revision, DeletedAt: r.DeletedAt, Qso: r.Qso})
		}
	}
	return records, nil
}

// checkRestoreTarget re-reads the target through the writing connection, as
// the read-only resolution saw it: the logbook exists, an identity restore's
// logbook still has the UUID its export was fetched for, and a legacy
// restore's logbook has no SM Cloud binding born on identity (ruling K4; the
// read-only check already refused one, so this holds only if the file changed
// meanwhile).
func checkRestoreTarget(ctx context.Context, db *sqlite.Service, paths archive.Paths, target types.Logbook, legacy bool) error {
	lb, err := db.FetchLogbookByIDWithContext(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("target logbook id=%d does not exist: %w", target.ID, err)
	}
	if !legacy {
		if lb.UUID != target.UUID {
			return fmt.Errorf("target logbook id=%d changed while the export was fetched (UUID %s, fetched for %s); nothing was restored", target.ID, lb.UUID, target.UUID)
		}
		return nil
	}
	bindings, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("read the target archive's bindings: %w", err)
	}
	for _, b := range bindings {
		if b.LogbookID == target.ID && b.Destination == smcloud.Type && b.IdentitySince != nil {
			return stampedRefusal(target.ID, paths)
		}
	}
	return nil
}
