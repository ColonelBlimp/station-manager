package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// runConfigDowngrade is `smd config-downgrade --to <version> --yes [--config <path>]`:
// the config half of a rollback (W-0021 slice 1, beside `smd db-downgrade`).
// It rewrites config.json to the named older schema version through the
// registered down steps, so a tagged older build — which refuses unknown keys
// and a newer version — can read the file again. Down only, and only through
// versions that have a down step (the command names the floor otherwise).
// The rewrite is the same crash-durable replacement the daemon uses and keeps
// the file's mode. Run it with the daemon stopped: the daemon persists its
// current shape at start and on every settings save.
//
//	smd config-downgrade --to 3 --yes                 # the daemon's own config.json
//	smd config-downgrade --to 3 --yes --config <path> # an explicit file
func runConfigDowngrade(args []string) error { return runConfigDowngradeTo(os.Stdout, args) }

func runConfigDowngradeTo(out io.Writer, args []string) error {
	const op errors.Op = "smd.runConfigDowngrade"
	fs := flag.NewFlagSet("config-downgrade", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to config.json (default: the daemon's own resolution)")
	target := fs.Int("to", -1, "config schema version to rewrite the file DOWN to (required)")
	yes := fs.Bool("yes", false, "confirm: a down step drops the keys newer versions added")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "usage: smd config-downgrade --to <version> --yes [--config <path>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	// A stray word stops flag parsing and would drop a --config that follows it,
	// pointing the rewrite at the default file. Refuse before touching anything.
	if fs.NArg() != 0 {
		fs.Usage()
		return errors.New(op).WithMsgf("unexpected argument %q: config-downgrade takes flags only", fs.Arg(0))
	}
	if *target < 0 {
		fs.Usage()
		return errors.New(op).WithMsg("--to <version> is required")
	}
	if !*yes {
		return errors.New(op).WithMsgf("refusing to rewrite the config to schema version %d without --yes "+
			"(stop the daemon first; the down steps drop the keys newer versions added)", *target)
	}

	path := *configPath
	if path == "" {
		dir, err := utils.ResolveWorkingDir()
		if err != nil {
			return err
		}
		path = filepath.Join(dir, "config.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("read %s", path)
	}
	from, err := config.DocumentSchemaVersion(data)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	downgraded, err := config.DowngradeDocument(data, *target)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	dur, err := config.WriteDocument(path, downgraded)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("write %s", path)
	}
	caveat := ""
	if dur == config.DurabilityUncertain {
		caveat = " (written; directory fsync failed, so crash durability is unconfirmed)"
	}
	_, _ = fmt.Fprintf(out, "config-downgrade: %s — config schema %d → %d%s. Start the matching older build, "+
		"or the current daemon will migrate it back up on its next start.\n", path, from, *target, caveat)
	return nil
}
