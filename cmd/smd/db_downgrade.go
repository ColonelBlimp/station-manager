package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// runDBDowngrade is `smd db-downgrade --to <version> --yes [--config <path>]`:
// the operator's half of a rollback (W-0021 slice 1). It migrates the LOG schema
// of the config's own database DOWN to <version>, so a tagged older build whose
// bundled migrations stop at that version can open the file again. Down only —
// the daemon migrates up on its next start — and the reference database is never
// touched. It refuses to run without --yes because a down migration drops what
// the newer schema added, and it must run with the daemon stopped (the daemon
// holds the same file).
//
//	smd db-downgrade --to 11 --yes                 # the daemon's own config.json
//	smd db-downgrade --to 11 --yes --config <path> # an explicit file
func runDBDowngrade(args []string) error { return runDBDowngradeTo(os.Stdout, args) }

func runDBDowngradeTo(out io.Writer, args []string) error {
	const op errors.Op = "smd.runDBDowngrade"
	fs := flag.NewFlagSet("db-downgrade", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to config.json (default: the daemon's own resolution)")
	target := fs.Int("to", -1, "log schema version to migrate DOWN to (required)")
	yes := fs.Bool("yes", false, "confirm: a down migration drops what newer migrations added")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "usage: smd db-downgrade --to <version> --yes [--config <path>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Go's flag parsing stops at the first positional argument, so a stray word
	// would silently drop every flag after it — including a --config meant to
	// point this at a scratch copy — and the downgrade would land on the DEFAULT
	// database. Refuse before any config is loaded.
	if fs.NArg() != 0 {
		fs.Usage()
		return errors.New(op).WithMsgf("unexpected argument %q: db-downgrade takes flags only", fs.Arg(0))
	}
	if *target < 0 {
		fs.Usage()
		return errors.New(op).WithMsg("--to <version> is required")
	}
	if !*yes {
		return errors.New(op).WithMsgf("refusing to migrate the log schema down to %d without --yes "+
			"(stop the daemon first; a down migration drops what newer migrations added)", *target)
	}

	cfg, _, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	cfgSvc := config.New(cfg)

	// The same container wiring `smd import` uses: no hand-wired construction.
	container := iocdi.New()
	if err := container.RegisterInstance(config.ServiceName, cfgSvc); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register config service")
	}
	if err := container.RegisterInstance(events.ServiceName, events.NewHub()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register event hub")
	}
	if err := container.Register(logging.ServiceName, reflect.TypeFor[*logging.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register logging service")
	}
	if err := container.Register(types.SqliteServiceName, reflect.TypeFor[*sqlite.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register sqlite service")
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
			_, _ = fmt.Fprintf(os.Stderr, "smd db-downgrade: logger close error: %v\n", cerr)
		}
	}()
	dbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.SqliteServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve sqlite service")
	}

	// Open WITHOUT Migrate(): the whole point is not to bring the file up.
	dbSvc.SetMigrationSets(sqlite.MigrationSetLog)
	if err := dbSvc.Open(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("open database")
	}
	defer func() {
		if cerr := dbSvc.Close(); cerr != nil {
			loggerSvc.ErrorWith().Err(cerr).Msg("database close error")
		}
	}()

	from, err := dbSvc.DowngradeLogSchemaTo(uint(*target))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "db-downgrade: %s — log schema %d → %d. Start the matching older build, "+
		"or the current daemon will migrate it back up on its next start.\n",
		dbSvc.DatabaseConfig.Path, from, *target)
	return nil
}
