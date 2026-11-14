package main

import (
	stderr "errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// runConfigCheck is the read-only deployment preflight (W-0006 AC8, W-0008 CC-6).
// It evaluates a config.json exactly as far as the daemon's own startup does
// before it needs any runtime dependency: the same unknown-key gate (paths only,
// values omitted), then config.Load — migration in memory, defaulting,
// normalisation and validation — then construction of every ENABLED forwarder
// through the same probe the config PUT handler uses. A file that clears all
// three exits 0; a file startup would refuse at any of them exits non-zero with
// the daemon's own diagnostic, so a deploy pipeline can gate on the refusal
// before it happens (alpha.2 dogfood Findings #1 and #6 were both refusals this
// check used to miss).
//
// Boundary: a passing check means the file loads, validates and its enabled
// forwarders construct. Databases, listeners and other runtime dependencies are
// outside its scope, and the success text says so.
//
// Output is a short report, one fact per line: a verdict, the file, then what was
// checked and what was not; a refusal gives the file and the reason the same way
// (operator, 2026-11-14: the earlier single sentence "is not easy to read").
//
// Output is safe for terminals, logs and automation by contract: unknown keys are
// reported as paths without values, and the forwarder probe's raw constructor
// cause is discarded here (the API keeps it for protected daemon logging) —
// defense in depth on top of forwarding.Build's rule that constructors never
// embed a credential value. Validation diagnostics may name ordinary configured
// values such as forwarder names and actions. Nothing is started, bound,
// migrated in place or written.
//
//	smd config-check                 # the daemon's own default-resolved config.json
//	smd config-check --config <path> # an explicit file
func runConfigCheck(args []string) error { return runConfigCheckTo(os.Stdout, args) }

// runConfigCheckTo is runConfigCheck with the report destination injected for
// tests; the command's own output goes to stdout, diagnostics to the error.
func runConfigCheckTo(out io.Writer, args []string) error {
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
	if len(unknown) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d unrecognised configuration %s (values omitted):", len(unknown), oneOrMany(len(unknown), "key", "keys"))
		for _, k := range unknown {
			b.WriteString("\n    - ")
			b.WriteString(k)
		}
		return configCheckRefusal(path, stderr.New(b.String()))
	}

	// Stage 2 — the daemon's own Load: defaulting, normalisation, validation.
	// Load never writes; its error is the exact line startup prints
	// ("invalid config (<code>): <message>"), which names fields, forwarder names
	// and actions but no credential value.
	cfg, err := config.Load(path)
	if err != nil {
		return configCheckRefusal(path, err)
	}

	// Stage 3 — construct every enabled forwarder, the same Build gate
	// spawnForwarderWorkers applies. The cause is deliberately dropped: only the
	// stable finding is public output (see config.ForwarderStartupFinding).
	if f, _ := config.ForwarderStartupFinding(cfg.Forwarders); f != nil {
		return configCheckRefusal(path, fmt.Errorf("%s (%s)", f.Message, f.Code))
	}
	enabled := 0
	for _, fc := range cfg.Forwarders {
		if fc.Enabled {
			enabled++
		}
	}
	// From config v6 (W-0021 5C) an entry is enabled only while it still holds
	// its deprecated v5 fields; uploads are decided by each archive's
	// destination bindings, which live in the archive file and are checked at
	// startup — so the count says what it counts, and they head "Not checked".
	forwarders := "no forwarder is enabled, so none was constructed"
	if enabled > 0 {
		forwarders = fmt.Sprintf("%d enabled %s", enabled, oneOrMany(enabled, "forwarder constructs", "forwarders construct"))
	}
	_, err = fmt.Fprintf(out, "config-check: passed\n"+
		"  File: %s\n"+
		"  Checked:\n"+
		"    - no unrecognised keys\n"+
		"    - the file loads and validates as startup would\n"+
		"    - %s (legacy entries)\n"+
		"  Not checked:\n"+
		"    - upload destinations: each archive holds its own, checked at startup\n"+
		"    - databases, listeners and other runtime dependencies\n",
		path, forwarders)
	return err
}

// configCheckRefusal is the report for a file startup would refuse: the verdict,
// the file, then the reason on a line of its own. The reason is the daemon's own
// diagnostic, unchanged and still reachable with errors.Is and errors.As.
func configCheckRefusal(path string, reason error) error {
	return fmt.Errorf("config-check FAILED: startup would refuse this file\n  File: %s\n  Reason: %w", path, reason)
}

// oneOrMany picks the wording for a count; provision.go's plural only adds an "s".
func oneOrMany(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
