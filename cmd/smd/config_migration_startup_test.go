package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

// W-0006 / ADR 0075 — persistResolvedConfig writes only for a named reason, and a
// schema migration is persisted exactly once. A boot that resolves to what is
// already on disk touches nothing.

// AC7 — a semantic no-op startup leaves config.json content AND mtime untouched.
// The file is stamped an hour into the past so that ANY write would move mtime,
// making a silent rewrite fail the test rather than pass by clock granularity.
func TestPersistResolvedConfig_NoOpDoesNotTouchFile(t *testing.T) {
	const ua = "station-manager/2.0.0-test"
	svc := startupConfigService(t, func(c *config.Config) { c.UserAgent = ua })

	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(svc.Path, past, past); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(svc.Path)
	if err != nil {
		t.Fatal(err)
	}

	changes, err := persistResolvedConfig(svc, ua)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("a no-op startup reported changes: %v", changeFields(changes))
	}

	after, err := os.ReadFile(svc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("no-op startup rewrote config.json content")
	}
	fi, err := os.Stat(svc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(past) {
		t.Errorf("no-op startup moved mtime: %v (a quiet log must mean a quiet file)", fi.ModTime())
	}
}

// AC7 — a legacy wide-mode (0644) file is still tightened to 0600 on a content
// no-op, as an explicit permission action — without rewriting content or moving
// mtime (chmod moves ctime, not mtime).
func TestPersistResolvedConfig_TightensWideModeOnNoOp(t *testing.T) {
	const ua = "station-manager/2.0.0-test"
	svc := startupConfigService(t, func(c *config.Config) { c.UserAgent = ua })

	if err := os.Chmod(svc.Path, 0o644); err != nil { // legacy wide mode
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(svc.Path, past, past); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(svc.Path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := persistResolvedConfig(svc, ua); err != nil {
		t.Fatalf("persist: %v", err)
	}

	fi, err := os.Stat(svc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("wide-mode file not tightened: mode = %v, want 0600", fi.Mode().Perm())
	}
	after, err := os.ReadFile(svc.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("permission tighten rewrote content")
	}
	if !fi.ModTime().Equal(past) {
		t.Errorf("permission tighten moved mtime: %v", fi.ModTime())
	}
}

// PIN 5 — a below-current on-disk document is migrated and persisted EXACTLY
// ONCE, under an explicit schema_version reason that names no value; the retired
// key is gone from disk; and a second boot writes nothing.
func TestPersistResolvedConfig_PersistsMigrationOnceUnderNamedReason(t *testing.T) {
	const ua = "station-manager/2.0.0-test"
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	v2 := `{"version":2,"logging_station":{"station_callsign":"M0ABC"},` +
		`"ft8":{"tx":{"auto_work_callers":true,"caller_answer_mode":"auto_first"}}}`
	if err := os.WriteFile(path, []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path) // migrates v2→v3 in memory (consumes the retired key)
	if err != nil {
		t.Fatalf("Load a v2 config with a retired key must succeed: %v", err)
	}
	cfg.UserAgent = ua
	svc := config.New(cfg)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	svc.SetPath(path)

	changes, err := persistResolvedConfig(svc, ua)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}

	var reason *config.FieldChange
	for i := range changes {
		if changes[i].Field == "schema_version" {
			reason = &changes[i]
		}
	}
	if reason == nil {
		t.Fatalf("migration not reported under a named reason; changes = %v", changeFields(changes))
	}
	if reason.From != "" || reason.To != "" {
		t.Errorf("migration reason named a value: %+v; want the schema_version path only", *reason)
	}
	if strings.Contains(reason.From, "M0ABC") || strings.Contains(reason.To, "M0ABC") {
		t.Errorf("the migration reason leaked a config value: %+v", *reason)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "auto_work_callers") {
		t.Error("the retired key survived on disk after the migration write")
	}
	if v, verr := config.FileSchemaVersion(path); verr != nil || v != config.CurrentSchemaVersion() {
		t.Errorf("on-disk version = %d (err %v), want current %d", v, verr, config.CurrentSchemaVersion())
	}

	// Second boot: already current, nothing to persist — mtime must not move.
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	changes2, err := persistResolvedConfig(svc, ua)
	if err != nil {
		t.Fatalf("second persist: %v", err)
	}
	if len(changes2) != 0 {
		t.Fatalf("a current-version boot re-persisted: %v — migration must land exactly once", changeFields(changes2))
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(past) {
		t.Errorf("second boot rewrote config.json (mtime moved to %v)", fi.ModTime())
	}
}
