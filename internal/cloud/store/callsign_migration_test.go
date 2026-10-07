package store

import (
	"strings"
	"testing"
)

// W-0021 5F.2, ruling S4 (2026-10-07, ADR 0089): migration 0008 adds the
// logbook callsign the cloud receives (ADR 0082 part 8). Its down step runs
// only while every callsign is empty — 0007's guard does not cover an 8→7
// downgrade — and otherwise refuses, changing nothing.
//
//	CS1  the up step: version 8, every existing logbook's callsign is ''.
//	CS2  the down step with every callsign empty: the column is gone.
//	CS3  the down step with a callsign recorded: refused, the column kept.

func TestCallsign_CS1_UpAddsAnEmptyCallsign(t *testing.T) {
	db := openMigrationDB(t)
	version6WithData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if v := scalar[int](t, db, `SELECT version FROM schema_migrations`); v != 8 {
		t.Fatalf("schema version = %d; want 8", v)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM logbooks WHERE callsign <> ''`); n != 0 {
		t.Fatalf("logbooks with a callsign after the upgrade = %d; want 0", n)
	}
	if _, err := db.Exec(`UPDATE logbooks SET callsign = repeat('A', 33)`); err == nil {
		t.Fatal("a 33-character callsign was accepted; want the length check")
	}
}

func TestCallsign_CS2_DownWithEmptyCallsigns(t *testing.T) {
	db := openMigrationDB(t)
	version6WithData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	execSQLFile(t, db, "migrations/0008_logbook_callsign.down.sql")
	if n := scalar[int](t, db, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'logbooks' AND column_name = 'callsign'`); n != 0 {
		t.Fatal("the callsign column survived the down step")
	}
}

func TestCallsign_CS3_DownRefusesARecordedCallsign(t *testing.T) {
	db := openMigrationDB(t)
	version6WithData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE logbooks SET callsign = '7Q5MLV' WHERE legacy_name = 'main'`); err != nil {
		t.Fatal(err)
	}
	b, err := migrationsFS.ReadFile("migrations/0008_logbook_callsign.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(string(b))
	if err == nil || !strings.Contains(err.Error(), "callsign") {
		t.Fatalf("down with a recorded callsign: %v; want a refusal naming the callsign", err)
	}
	if got := scalar[string](t, db, `SELECT callsign FROM logbooks WHERE legacy_name = 'main'`); got != "7Q5MLV" {
		t.Fatalf("callsign after the refused down = %q; want it kept", got)
	}
}
