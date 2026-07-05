package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// TestMigrate0004_NormalisesTimestampsToUTC seeds pre-0004 rows in every
// timestamp format the dogfood DB can carry — naive-local (a DEFAULT/trigger
// write), pre-fix Go `time.Time.String()` debris, and already-canonical UTC —
// then applies 0004 and asserts each value ends canonical UTC + datetime()-
// parseable, and that the recreated stamp trigger writes UTC.
func TestMigrate0004_NormalisesTimestampsToUTC(t *testing.T) {
	svc := testServiceWithoutMigrations(t)
	applyMigrationSteps(t, svc, 3) // up to 0003 — the localtime schema
	db := svc.handle

	if _, err := db.Exec(`INSERT INTO logbook (id, callsign, name) VALUES (1, 'G4ABC', 'L')`); err != nil {
		t.Fatalf("seed logbook: %v", err)
	}

	// The shiftable formats carry a 14:00 LOCAL (CAT, UTC+2) wall-clock prefix, so
	// their correct UTC instant is 12:00. The canonical row is already 12:00 UTC.
	seed := func(id int, ts string) {
		t.Helper()
		uuid := fmt.Sprintf("01920000-0000-7000-8000-%012d", id)
		_, err := db.Exec(`INSERT INTO qso
			(id, created_at, modified_at, deleted_at, uuid, call, band, mode, freq,
			 qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?,?,?,?,?, 'M0CMC','40m','SSB',7050000,
			        '20250508','0845','0845','59','59','Test',?,1)`,
			id, ts, ts, ts, uuid, fmt.Sprintf("%064d", id))
		if err != nil {
			t.Fatalf("seed qso %d: %v", id, err)
		}
	}
	seed(1, "2026-07-05 14:00:00")                            // naive-local (DEFAULT/trigger)
	seed(2, "2026-07-05 14:00:00.123456789 +0200 CAT m=+0.5") // pre-fix Go modified_at debris (LOCAL)
	seed(3, "2026-07-05 12:00:00.123456+00:00")               // already canonical UTC
	seed(4, "2026-07-05 12:00:00.123456789 +0000 UTC")        // PRE-fix sqlboiler UTC — must NOT shift

	applyMigrationSteps(t, svc, 1) // apply 0004

	// All four formats resolve to the SAME UTC instant (12:00:00), debris-free and
	// parseable — including the `+0000 UTC` row, which is already UTC and must be
	// reformatted, not shifted (the staged-review bug).
	for _, id := range []int{1, 2, 3, 4} {
		for _, col := range []string{"created_at", "modified_at", "deleted_at"} {
			var raw string
			var dt sql.NullString
			if err := db.QueryRow(
				fmt.Sprintf(`SELECT quote(%s), datetime(%s) FROM qso WHERE id=?`, col, col), id,
			).Scan(&raw, &dt); err != nil {
				t.Fatalf("read qso %d %s: %v", id, col, err)
			}
			if strings.Contains(raw, "m=+") {
				t.Errorf("qso %d %s still carries monotonic debris: %s", id, col, raw)
			}
			if !dt.Valid || dt.String != "2026-07-05 12:00:00" {
				t.Errorf("qso %d %s datetime() = %q (raw %s), want 2026-07-05 12:00:00",
					id, col, dt.String, raw)
			}
		}
	}
	// The already-canonical row is preserved verbatim (offset + sub-second kept),
	// not re-shifted.
	var canon string
	_ = db.QueryRow(`SELECT quote(modified_at) FROM qso WHERE id=3`).Scan(&canon)
	if !strings.Contains(canon, "+00:00") {
		t.Errorf("canonical UTC row was rewritten, want it preserved: %s", canon)
	}

	// The recreated stamp trigger writes UTC (datetime('now')), not localtime.
	var trig string
	_ = db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='trg_qso_set_modified_at'`,
	).Scan(&trig)
	if strings.Contains(trig, "localtime") || !strings.Contains(trig, "datetime('now')") {
		t.Errorf("trg_qso_set_modified_at not UTC after 0004:\n%s", trig)
	}
}
