package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// `smd db-downgrade --to N --yes` (W-0021 slice 1): the operator half of the
// rollback drill. It migrates the LOG schema down to N on the config's own
// database, with the daemon stopped, and reports the transition. Nothing
// migrates up here, and nothing runs without --yes.
func TestDBDowngrade_MigratesLogSchemaDownAndReports(t *testing.T) {
	tmp := setupImportTestbed(t, nil)
	cfgPath := filepath.Join(tmp, "config.json")

	var out strings.Builder
	if err := runDBDowngradeTo(&out, []string{"--config", cfgPath, "--to", "10", "--yes"}); err != nil {
		t.Fatalf("db-downgrade: %v", err)
	}
	if !strings.Contains(out.String(), "11 → 10") {
		t.Fatalf("report %q does not name the transition 11 → 10", out.String())
	}

	db := reopenDB(t, tmp)
	v, dirty, err := db.SchemaVersionWithContext(context.Background())
	if err != nil || dirty {
		t.Fatalf("schema version after: %d dirty=%v err=%v", v, dirty, err)
	}
	if v != 10 {
		t.Fatalf("schema version = %d, want 10", v)
	}
	if _, err := db.FetchLogbookByIDWithContext(context.Background(), 1); err != nil {
		t.Fatalf("logbook row lost: %v", err)
	}
}

func TestDBDowngrade_RefusesWithoutYesAndRefusesUpward(t *testing.T) {
	tmp := setupImportTestbed(t, nil)
	cfgPath := filepath.Join(tmp, "config.json")

	var out strings.Builder
	if err := runDBDowngradeTo(&out, []string{"--config", cfgPath, "--to", "10"}); err == nil {
		t.Fatal("ran without --yes; want a refusal")
	}
	if err := runDBDowngradeTo(&out, []string{"--config", cfgPath, "--to", "11", "--yes"}); err == nil {
		t.Fatal("target equal to the current version accepted; want a refusal")
	}
	if err := runDBDowngradeTo(&out, []string{"--config", cfgPath, "--yes"}); err == nil {
		t.Fatal("missing --to accepted; want a refusal")
	}

	db := reopenDB(t, tmp)
	v, _, err := db.SchemaVersionWithContext(context.Background())
	if err != nil || v != 11 {
		t.Fatalf("schema version = %d (%v) after refusals, want 11 untouched", v, err)
	}
}

// A stray positional argument makes Go's flag parser stop, so every flag after
// it is dropped: `--to 10 --yes stray --config <scratch>` would ignore the
// scratch config and downgrade the DEFAULT database. The command must refuse
// before loading any config, and the default database must be untouched.
func TestDBDowngrade_RefusesStrayArgumentBeforeTouchingTheDefaultDatabase(t *testing.T) {
	tmp := setupImportTestbed(t, nil) // sets SM_WORKING_DIR: this is the DEFAULT database

	var out strings.Builder
	err := runDBDowngradeTo(&out, []string{"--to", "10", "--yes", "stray",
		"--config", filepath.Join(t.TempDir(), "does-not-exist.json")})
	if err == nil {
		t.Fatal("a stray positional argument was accepted; the flags after it were silently dropped")
	}
	if !strings.Contains(err.Error(), "stray") {
		t.Fatalf("refusal %q does not name the stray argument", err.Error())
	}

	db := reopenDB(t, tmp)
	v, _, verr := db.SchemaVersionWithContext(context.Background())
	if verr != nil || v != 11 {
		t.Fatalf("default database schema version = %d (%v); the stray argument redirected the downgrade", v, verr)
	}
}
