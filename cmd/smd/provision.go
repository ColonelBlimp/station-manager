package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"

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
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// runProvision is `smd provision` (W-0021 Q7, ADR 0071 refinement 2026-10-16):
// re-create cloud archive U as a new managed archive file from its
// whole-archive export, every row round-tripped (archive.Manager.Provision).
//
//	smd provision --list [--config PATH]
//	smd provision --archive UUID [--default-logbook UUID] [--dry-run] [--config PATH]
//
// It runs with the daemon stopped and no other offline writer, like `smd
// import` and `smd restore`. --list and --dry-run only read the SM Cloud
// server and config.json: they open no archive and start no file logger, whose
// initialization would create the log directory. A missing config.json is an
// error, never seeded. Success exits 0, also with the caveats a published
// archive can carry; they are printed as warnings.
func runProvision(args []string) error {
	const op errors.Op = "smd.provision"

	fs := flag.NewFlagSet("provision", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to config.json (default: config.json in $SM_WORKING_DIR, else the XDG data dir, else the executable's directory)")
	archiveID := fs.String("archive", "", "UUID of the cloud archive to provision as a new managed archive; run with the daemon stopped")
	defaultLogbook := fs.String("default-logbook", "", "UUID of the archive's logbook to make its default (required when it has more than one)")
	list := fs.Bool("list", false, "list the station account's cloud archives and their logbooks")
	dryRun := fs.Bool("dry-run", false, "fetch the export and report the plan; nothing is written")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "usage: smd provision --archive UUID [--default-logbook UUID] [--dry-run] [--config PATH]")
		_, _ = fmt.Fprintln(os.Stderr, "       smd provision --list [--config PATH]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	switch {
	case fs.NArg() > 0:
		return errors.New(op).WithMsgf("unexpected argument %q", fs.Arg(0))
	case *list && (set["archive"] || set["default-logbook"] || set["dry-run"]):
		return errors.New(op).WithMsg("--list takes no flag but --config")
	case !*list && !set["archive"]:
		return errors.New(op).WithMsg("--archive UUID or --list is required")
	case set["archive"] && !utils.IsValidUUIDv7(*archiveID):
		return errors.New(op).WithMsg("--archive must be a UUIDv7")
	case set["default-logbook"] && !utils.IsValidUUIDv7(*defaultLogbook):
		return errors.New(op).WithMsg("--default-logbook must be a UUIDv7")
	}

	path, err := provisionConfigPath(*configPath)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("load %s", path)
	}
	account, ok := smcloudAccount(cfg) // enabled or not: provisioning only reads the cloud
	if !ok {
		return errors.New(op).WithMsg("no SM Cloud station account in config.json — add one (url + token) before provisioning")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *list {
		archives, err := smcloud.ListArchives(ctx, account)
		if err != nil {
			return errors.New(op).WithErr(err)
		}
		printArchiveListing(os.Stdout, cfg, archives)
		return nil
	}
	export, err := smcloud.FetchArchiveExport(ctx, account, *archiveID)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	req := archive.ProvisionRequest{Export: export, DefaultLogbook: *defaultLogbook}
	cfgSvc := config.New(cfg)
	cfgSvc.SetPath(path)

	if *dryRun {
		// An uninitialized logger is a no-op: the dry run writes nothing.
		m := archive.NewManager(cfgSvc, &logging.Service{}, qsoservice.IsValidCallsign)
		plan, err := m.PreflightProvision(req)
		if err != nil {
			return errors.New(op).WithErr(err)
		}
		reportPlan(os.Stdout, plan, export.Label, cfg.ActiveQsoArchiveID == plan.ArchiveUUID)
		_, _ = fmt.Fprintln(os.Stdout, "dry run: nothing was written. Database construction and round-trip compatibility were not tested.")
		return nil
	}

	// Only a real run builds the services: the logger's initialization
	// creates the log directory, which --list and --dry-run must not.
	logger, qsoSvc, closeServices, err := provisionServices(cfgSvc)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	defer closeServices()
	m := archive.NewManager(cfgSvc, logger, qsoservice.IsValidCallsign)
	m.SetRowRestorer(func(ctx context.Context, db *sqlite.Service, logbookID int64, q types.Qso) error {
		st, err := qsoSvc.RestoreInto(ctx, db, logbookID, q)
		if err == nil && st != qsoservice.RestoreStored {
			return fmt.Errorf("qso %s was already present", q.UUID)
		}
		return err
	})
	res, err := m.Provision(ctx, req)
	if err != nil {
		// Provision's errors name rows, fields and paths, never values.
		logger.ErrorWith().Err(err).Str("archive_id", *archiveID).Msg("provision: refused or failed; nothing was published")
		return errors.New(op).WithErr(err)
	}
	reportProvision(os.Stdout, res, len(export.Qsos), len(export.Logbooks), cfgSvc.Snapshot().ActiveQsoArchiveID == res.Entry.ID)
	return nil
}

// provisionServices wires the real run's services through the container, as
// `smd import` and `smd restore` do: config, the event hub, logging, the
// sqlite beans and the QSO service. The sqlite beans are initialized and never
// opened: no active archive is touched. Rows go into the archive manager's
// file under construction through RestoreInto, which the manager owns and
// closes.
func provisionServices(cfgSvc *config.Service) (*logging.Service, *qsoservice.Service, func(), error) {
	container := iocdi.New()
	hub := events.NewHub()
	if err := container.RegisterInstance(config.ServiceName, cfgSvc); err != nil {
		return nil, nil, nil, errors.New("").WithErr(err).WithMsg("register config service")
	}
	if err := container.RegisterInstance(events.ServiceName, hub); err != nil {
		return nil, nil, nil, errors.New("").WithErr(err).WithMsg("register event hub")
	}
	for _, r := range []struct {
		name string
		typ  reflect.Type
	}{
		{logging.ServiceName, reflect.TypeFor[*logging.Service]()},
		{types.SqliteServiceName, reflect.TypeFor[*sqlite.Service]()},
		{types.ReferenceDBServiceName, reflect.TypeFor[*sqlite.Service]()},
		{qsoservice.ServiceName, reflect.TypeFor[*qsoservice.Service]()},
	} {
		if err := container.Register(r.name, r.typ); err != nil {
			return nil, nil, nil, errors.New("").WithErr(err).WithMsgf("register %s", r.name)
		}
	}
	iocdi.SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		if id == "workingdir" && targetType.Kind() == reflect.String {
			return cfgSvc.WorkingDir(), true, nil
		}
		return nil, false, nil
	})
	if err := container.Build(); err != nil {
		hub.Close()
		return nil, nil, nil, errors.New("").WithErr(err).WithMsg("build container")
	}
	logger, err := iocdi.ResolveAs[*logging.Service](container, logging.ServiceName)
	if err != nil {
		hub.Close()
		return nil, nil, nil, errors.New("").WithErr(err).WithMsg("resolve logging service")
	}
	closeAll := func() {
		hub.Close()
		if cerr := logger.Close(); cerr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "smd provision: logger close error: %v\n", cerr)
		}
	}
	qsoSvc, err := iocdi.ResolveAs[*qsoservice.Service](container, qsoservice.ServiceName)
	if err != nil {
		closeAll()
		return nil, nil, nil, errors.New("").WithErr(err).WithMsg("resolve qso service")
	}
	return logger, qsoSvc, closeAll, nil
}

// provisionConfigPath is --config, else config.json in the resolved working
// directory, which is not created: a missing config is reported, not seeded.
func provisionConfigPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	dir, err := utils.ResolveWorkingDir()
	if err != nil {
		return "", fmt.Errorf("resolving the working directory for the default config path: %w", err)
	}
	return filepath.Join(dir, "config.json"), nil
}

// printArchiveListing writes the --list report: each cloud archive, what this
// station's catalogue holds for it, and its logbooks.
func printArchiveListing(w io.Writer, cfg config.Config, archives []smcloud.CloudArchive) {
	if len(archives) == 0 {
		_, _ = fmt.Fprintln(w, "the station account has no cloud archive")
		return
	}
	for _, a := range archives {
		legacy := ""
		if a.Legacy {
			legacy = "  legacy (Home; cannot be provisioned)"
		}
		_, _ = fmt.Fprintf(w, "archive %s  %q%s  here: %s\n", a.UUID, a.Label, legacy, catalogueState(cfg, a.UUID))
		for _, lb := range a.Logbooks {
			id := "(not adopted)"
			if lb.UUID != nil {
				id = *lb.UUID
			} else if lb.LegacyName != nil {
				id = fmt.Sprintf("(not adopted, stored name %q)", *lb.LegacyName)
			}
			_, _ = fmt.Fprintf(w, "  logbook %s  %q  %s  %s\n", id, lb.Label, lb.Callsign, qsoCount(lb.Count, lb.Tombstones))
		}
	}
}

func catalogueState(cfg config.Config, id string) string {
	entry := cfg.QsoArchiveByID(id)
	if entry == nil {
		return "not catalogued"
	}
	state := fmt.Sprintf("catalogued as %s %q", entry.Ownership, entry.Label)
	if cfg.ActiveQsoArchiveID == entry.ID {
		state += ", active"
	}
	return state
}

// reportPlan writes the dry run's plan: where U would go, the catalogue
// outcome and the logbooks.
func reportPlan(w io.Writer, plan archive.ProvisionPlan, label string, active bool) {
	_, _ = fmt.Fprintf(w, "archive %s %q would be provisioned at %s\n", plan.ArchiveUUID, label, plan.Path)
	_, _ = fmt.Fprintln(w, catalogueLine(plan.Entry == nil, plan.Entry, active))
	writePlannedLogbooks(w, plan.Logbooks)
}

// reportProvision writes a published provisioning's outcome and every caveat
// it carries: the archive is in place, so these are warnings, not failures.
func reportProvision(w io.Writer, res archive.ProvisionResult, qsos, logbooks int, active bool) {
	_, _ = fmt.Fprintf(w, "provisioned archive %s at %s: %s in %s\n", res.Plan.ArchiveUUID, res.Plan.Path,
		plural(qsos, "QSO"), plural(logbooks, "logbook"))
	_, _ = fmt.Fprintln(w, catalogueLine(res.Added, &res.Entry, active))
	writePlannedLogbooks(w, res.Plan.Logbooks)
	if res.DurabilityUncertain {
		_, _ = fmt.Fprintln(w, "warning: durability uncertain: a directory sync failed after the archive was published; it is in place")
	}
	if res.LeftoverTemp != "" {
		_, _ = fmt.Fprintf(w, "warning: the temporary name %s could not be removed; the next start reports it\n", res.LeftoverTemp)
	}
}

func catalogueLine(added bool, entry *types.QsoArchiveConfig, active bool) string {
	if added {
		return "catalogue: a new inactive entry"
	}
	state := "inactive"
	if active {
		state = "active"
	}
	return fmt.Sprintf("catalogue: the existing entry %q kept, %s", entry.Label, state)
}

func writePlannedLogbooks(w io.Writer, lbs []archive.PlannedLogbook) {
	for _, lb := range lbs {
		def := ""
		if lb.Default {
			def = "  (default)"
		}
		_, _ = fmt.Fprintf(w, "  logbook %s  %q  %s  %s%s\n", lb.UUID, lb.Name, lb.Callsign, qsoCount(lb.Rows, lb.Tombstones), def)
	}
}

// qsoCount is a logbook's rows, tombstones included and counted apart.
func qsoCount(rows, tombstones int) string {
	if tombstones == 0 {
		return plural(rows, "QSO")
	}
	return fmt.Sprintf("%s (%s)", plural(rows, "QSO"), plural(tombstones, "tombstone"))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
