package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// AC8 — even default-path resolution is read-only. The daemon's ordinary
// WorkingDir resolver creates a first-run directory, but config-check must not:
// a missing live install should produce a not-found error and no filesystem
// artifact.
func TestRunConfigCheck_DefaultPathDoesNotCreateWorkingDir(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "missing-station-manager")
	t.Setenv(utils.EnvSmWorkingDir, workDir)

	err := runConfigCheck(nil)
	if err == nil {
		t.Fatal("config-check unexpectedly accepted a missing config")
	}
	if _, statErr := os.Stat(workDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("config-check created its default working directory; stat error = %v", statErr)
	}
}

// ---- W-0008 CC-6: parity with config.Load plus construction of every enabled
// forwarder (alpha.2 dogfood Findings #1 and #6). The nearest confusable outcome
// is a check that passes while the daemon would still refuse; the two dogfood
// shapes below are exactly that outcome against the key-only check.

func writeCheckConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return p
}

func runCheck(t *testing.T, path string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := runConfigCheckTo(&out, []string{"--config", path})
	return out.String(), err
}

// PIN 1 — a file startup's validation would refuse (Finding #6's shape in a
// version-3 document) fails the check with the daemon's own startup diagnostic.
func TestRunConfigCheck_RefusesWhatStartupValidationRefuses(t *testing.T) {
	p := writeCheckConfig(t, `{"version":3,"forwarders":[
		{"name":"qrzcq","type":"qrzcq","action_filter":["insert","update","delete"]}]}`)
	out, err := runCheck(t, p)
	if err == nil {
		t.Fatalf("config-check passed a file startup would refuse; output: %q", out)
	}
	// The complete startup diagnostic: forwarder, type, rejected action and the
	// supported set — the operator must not need a daemon start to learn any of it.
	want := `invalid config (invalid_forwarder): forwarder "qrzcq": type "qrzcq" does not support action "update" (supports [insert])`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %q, want it to contain %q", err.Error(), want)
	}
}

// PIN 2 — an enabled forwarder that would not construct (Finding #1's shape)
// fails the check naming the forwarder and the fault class only: no credential
// value, no URL component, no constructor text, no pointer to the daemon log.
func TestRunConfigCheck_RefusesUnconstructableForwarderWithoutValues(t *testing.T) {
	p := writeCheckConfig(t, `{"version":3,"forwarders":[
		{"name":"smcloud","type":"smcloud","enabled":true,
		 "credentials":{"url":"http://canary-remote-host.invalid:8080","token":"CANARY-TOKEN-8f3a"}}]}`)
	out, err := runCheck(t, p)
	if err == nil {
		t.Fatalf("config-check passed a forwarder startup could not construct; output: %q", out)
	}
	for _, want := range []string{"(forwarder_unusable)", `forwarder "smcloud" is enabled but its credentials are incomplete or invalid`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %q, want the fault code and the value-free finding %q", err.Error(), want)
		}
	}
	combined := out + err.Error()
	for _, leak := range []string{"canary-remote-host", "CANARY-TOKEN", ":8080", "allow_insecure_http", "daemon log"} {
		if strings.Contains(combined, leak) {
			t.Fatalf("config-check output exposes %q: %q", leak, combined)
		}
	}
}

// PIN 3 — a file that passes all three stages exits 0 and states the parity and
// its boundary: it does not claim the whole daemon would start.
func TestRunConfigCheck_SuccessStatesParityAndBoundary(t *testing.T) {
	p := writeCheckConfig(t, `{"version":3,"forwarders":[
		{"name":"smcloud","type":"smcloud","enabled":true,
		 "credentials":{"url":"https://cloud.example","token":"t"}}]}`)
	out, err := runCheck(t, p)
	if err != nil {
		t.Fatalf("config-check refused a valid file: %v", err)
	}
	for _, want := range []string{"no unrecognised keys", "loads and validates", "1 enabled forwarder", "Not checked: databases, listeners"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q lacks %q", out, want)
		}
	}
}

// PIN 4 — the deeper check stays read-only: the file's mtime is unchanged and
// nothing appears beside it (config.Load never writes; construction never does).
func TestRunConfigCheck_LoadStageWritesNothing(t *testing.T) {
	p := writeCheckConfig(t, `{"version":3,"forwarders":[
		{"name":"smcloud","type":"smcloud","enabled":true,
		 "credentials":{"url":"https://cloud.example","token":"t"}}]}`)
	before, _ := os.Stat(p)
	if _, err := runCheck(t, p); err != nil {
		t.Fatalf("config-check: %v", err)
	}
	after, _ := os.Stat(p)
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatalf("config-check touched the file: before %v/%d, after %v/%d", before.ModTime(), before.Size(), after.ModTime(), after.Size())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("config-check left artifacts beside the file: %d entries", len(entries))
	}
}

// PIN 5 — the existing unknown-key diagnostic still comes first, unchanged, even
// when the same file would also fail validation.
func TestRunConfigCheck_UnknownKeysStillReportedFirst(t *testing.T) {
	p := writeCheckConfig(t, `{"version":3,"typo_key":true,"forwarders":[
		{"name":"qrzcq","type":"qrzcq","action_filter":["insert","update","delete"]}]}`)
	_, err := runCheck(t, p)
	if err == nil || !strings.Contains(err.Error(), "unrecognised configuration key") || !strings.Contains(err.Error(), "typo_key") {
		t.Fatalf("err = %v, want the unknown-key diagnostic naming typo_key", err)
	}
	if strings.Contains(err.Error(), "does not support action") {
		t.Fatalf("validation diagnostic pre-empted the unknown-key diagnostic: %v", err)
	}
}
