package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/utils"
)

const v4ConfigDoc = `{"version":5,"data_dir":"/tmp/x","setup_complete":true,"default_logbook_id":1,
	"logging_station":{"station_callsign":"G4ABC"},
	"active_qso_archive_id":"019fd5c5-efcc-7193-be4f-1fee532ee315",
	"qso_archives":[{"id":"019fd5c5-efcc-7193-be4f-1fee532ee315","label":"Home","ownership":"legacy","path":"/tmp/x/db/station-manager.db"}]}`

// `smd config-downgrade --to N --yes` (W-0021 slice 1): the config half of the
// rollback drill. It rewrites config.json to the named older schema through the
// registered down steps — the one operation that lets a tagged older build,
// which refuses unknown keys and a newer version, read the file again.
func TestConfigDowngrade_RewritesFileToOlderSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(v4ConfigDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runConfigDowngradeTo(&out, []string{"--config", p, "--to", "3", "--yes"}); err != nil {
		t.Fatalf("config-downgrade: %v", err)
	}
	if !strings.Contains(out.String(), "5 → 3") {
		t.Fatalf("report %q does not name the transition 4 → 3", out.String())
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("rewritten file is not JSON: %v", err)
	}
	if m["version"] != float64(3) {
		t.Fatalf("version = %v, want 3", m["version"])
	}
	if _, present := m["qso_archives"]; present {
		t.Fatal("qso_archives survived the downgrade")
	}
	if m["default_logbook_id"] != float64(1) {
		t.Fatal("unrelated keys were disturbed")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600 kept", fi.Mode().Perm())
	}
}

func TestConfigDowngrade_Refusals_LeaveTheFileUntouched(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(v4ConfigDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runConfigDowngradeTo(&out, []string{"--config", p, "--to", "3"}); err == nil {
		t.Fatal("ran without --yes")
	}
	if err := runConfigDowngradeTo(&out, []string{"--config", p, "--to", "5", "--yes"}); err == nil {
		t.Fatal("target equal to the current version accepted")
	}
	// A stray word stops flag parsing, so the --config after it is dropped and
	// the rewrite would land on the DEFAULT file. Give that default a real v4
	// file and prove it is neither rewritten nor even read into a downgrade.
	defaultDir := t.TempDir()
	t.Setenv(utils.EnvSmWorkingDir, defaultDir)
	defaultPath := filepath.Join(defaultDir, "config.json")
	if err := os.WriteFile(defaultPath, []byte(v4ConfigDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runConfigDowngradeTo(&out, []string{"--to", "3", "--yes", "stray", "--config", p})
	if err == nil {
		t.Fatal("stray positional argument accepted")
	}
	if !strings.Contains(err.Error(), "stray") {
		t.Fatalf("refusal %q does not name the stray argument", err.Error())
	}
	if got, _ := os.ReadFile(defaultPath); string(got) != v4ConfigDoc {
		t.Fatal("the stray argument redirected the rewrite to the default config.json")
	}
	if err := runConfigDowngradeTo(&out, []string{"--config", p, "--yes"}); err == nil {
		t.Fatal("missing --to accepted")
	}
	data, _ := os.ReadFile(p)
	if string(data) != v4ConfigDoc {
		t.Fatal("a refused run rewrote the file")
	}
}
