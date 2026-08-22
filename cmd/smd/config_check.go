package main

import (
	stderr "errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// runConfigCheck is the read-only deployment preflight (W-0006 AC8): it evaluates
// the live config.json against the current schema and reports unrecognised key
// paths — values omitted — WITHOUT starting the daemon, migrating in place, or
// writing anything. A clean file exits 0; unknown keys (or a malformed / newer
// version) exit non-zero, so a deploy pipeline can gate on a would-be startup
// refusal before it happens.
//
//	smd config-check                 # the daemon's own default-resolved config.json
//	smd config-check --config <path> # an explicit file
func runConfigCheck(args []string) error {
	fs := flag.NewFlagSet("config-check", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to config.json (default: the daemon's own resolution)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path := *configPath
	if path == "" {
		dir, err := utils.ResolveWorkingDir()
		if err != nil {
			return err
		}
		path = filepath.Join(dir, "config.json")
	}

	unknown, err := config.PreflightUnknownKeys(path)
	if err != nil {
		return err // malformed / newer-than-supported — a distinct diagnostic
	}
	if len(unknown) == 0 {
		fmt.Printf("config-check: %s — no unrecognised keys; startup would not refuse for unknown keys.\n", path)
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s has %d unrecognised configuration key(s) — startup would refuse (values omitted):", path, len(unknown))
	for _, k := range unknown {
		b.WriteString("\n  ")
		b.WriteString(k)
	}
	return stderr.New(b.String())
}
