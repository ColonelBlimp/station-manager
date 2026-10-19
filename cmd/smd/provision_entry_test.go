package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

/*
   W-0021 Q7 commit 6 (operator review 2026-10-19): the real entry point.
   Each case re-runs this test binary as `smd ARGS…`: the helper sets
   os.Args and calls main() itself, with the daemon replaced by a marker, so
   main's own dispatch runs and no daemon can start.

     EP1 characterization of main before its switch is extracted: each
         existing subcommand (import, restore, config-check, db-downgrade,
         config-downgrade) is routed to its own flag set, and an unknown flag
         exits ExitError with "smd: " and that subcommand's usage; no
         subcommand, or the daemon's own flags, fall through to the daemon.
     EP2 `smd provision` reaches provisioning through main: an invalid
         invocation exits ExitError with the provisioning error, never the
         daemon.
*/

const entryMarker = "ENTRY-HELPER: the daemon would start"

// TestEntryHelper is the subprocess body; it runs only when asked.
func TestEntryHelper(t *testing.T) {
	raw := os.Getenv("SMD_ENTRY_HELPER_ARGS")
	if raw == "" {
		t.Skip("the entry-point helper runs only as a subprocess")
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		fmt.Fprintln(os.Stderr, "helper args:", err)
		os.Exit(99)
	}
	runDaemon = func() error {
		fmt.Println(entryMarker)
		return nil
	}
	os.Args = append([]string{"smd"}, args...)
	main()
	os.Exit(0)
}

// runEntry runs `smd args…` through main in a subprocess.
func runEntry(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	raw, _ := json.Marshal(args)
	cmd := exec.Command(os.Args[0], "-test.run=^TestEntryHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "SMD_ENTRY_HELPER_ARGS="+string(raw), "SM_WORKING_DIR="+t.TempDir())
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run the helper: %v", err)
	}
	return code, stdout.String(), stderr.String()
}

func TestEntryPoint_EP1_ExistingRoutesAndTheDaemonFallthrough(t *testing.T) {
	for _, c := range []struct{ sub, usage string }{
		{"import", "usage: smd import [flags] <file.adi>"},
		{"restore", "usage: smd restore [flags]"},
		{"config-check", "Usage of config-check:"},
		{"db-downgrade", "usage: smd db-downgrade --to <version> --yes [--config <path>]"},
		{"config-downgrade", "usage: smd config-downgrade --to <version> --yes [--config <path>]"},
	} {
		t.Run(c.sub, func(t *testing.T) {
			code, stdout, stderr := runEntry(t, c.sub, "--zz-not-a-flag")
			if code != ExitError || !strings.Contains(stderr, "smd: flag provided but not defined: -zz-not-a-flag") ||
				!strings.Contains(stderr, c.usage) || strings.Contains(stdout, entryMarker) {
				t.Fatalf("smd %s --zz-not-a-flag: exit %d, stdout %q, stderr %q; want %d, routed to its own flag set", c.sub, code, stdout, stderr, ExitError)
			}
		})
	}
	for name, args := range map[string][]string{
		"no arguments":          nil,
		"the daemon's --config": {"--config", "/nonexistent/config.json"},
	} {
		t.Run("daemon: "+name, func(t *testing.T) {
			code, stdout, stderr := runEntry(t, args...)
			if code != 0 || !strings.Contains(stdout, entryMarker) {
				t.Fatalf("smd %v: exit %d, stdout %q, stderr %q; want the daemon", args, code, stdout, stderr)
			}
		})
	}
}

func TestEntryPoint_EP2_ProvisionIsASubcommand(t *testing.T) {
	code, stdout, stderr := runEntry(t, "provision")
	if code != ExitError || strings.Contains(stdout, entryMarker) || !strings.Contains(stderr, "smd: ") ||
		!strings.Contains(stderr, "--archive UUID or --list is required") {
		t.Fatalf("smd provision: exit %d, stdout %q, stderr %q; want %d with the provisioning error, never the daemon", code, stdout, stderr, ExitError)
	}
}
