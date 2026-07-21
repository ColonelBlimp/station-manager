package sqlite

import (
	"fmt"
	"strings"
	"testing"
)

// TestMigrate0006_WidensModeAndCall verifies the widened CHECK constraints admit
// the full range the qsoservice validation layer already accepts: DIGITALVOICE (a
// 12-char ADIF MODE, previously capped at 10) and a 32-char CALL (previously capped
// at 20). Before 0006 both passed validation but violated the column CHECK on
// insert — a 500 on a live submit and, worse, a hard abort of a bulk import. Values
// past the new ceilings still reject, so the guard isn't simply removed.
func TestMigrate0006_WidensModeAndCall(t *testing.T) {
	svc := testService(t) // all migrations applied, including 0006
	db := svc.handle

	if _, err := db.Exec(`INSERT INTO logbook (id, callsign, name) VALUES (1, 'G4ABC', 'L')`); err != nil {
		t.Fatalf("seed logbook: %v", err)
	}

	insert := func(id int, call, mode string) error {
		uuid := fmt.Sprintf("01920000-0000-7000-8000-%012d", id)
		_, err := db.Exec(`INSERT INTO qso
			(id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
			 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?,?,?,'40m',?,7050000,'20250508','0845','0845','59','59','Test',?,1)`,
			id, uuid, call, mode, fmt.Sprintf("%064d", id))
		return err
	}

	// A 32-char CALL (with a digit, mirroring IsValidCallsign) + DIGITALVOICE MODE
	// both store now.
	longCall := "A" + strings.Repeat("1", 31) // 32 chars
	if len(longCall) != 32 {
		t.Fatalf("test setup: longCall is %d chars, want 32", len(longCall))
	}
	if err := insert(1, longCall, "DIGITALVOICE"); err != nil {
		t.Fatalf("DIGITALVOICE MODE + 32-char CALL should insert after 0006: %v", err)
	}

	// Past the new ceilings the CHECKs still bite.
	if err := insert(2, "G4ABC", strings.Repeat("X", 21)); err == nil {
		t.Fatalf("21-char MODE should still violate CHECK(length(trim(mode)) <= 20)")
	}
	if err := insert(3, "A"+strings.Repeat("1", 32), "SSB"); err == nil {
		t.Fatalf("33-char CALL should still violate CHECK(length(trim(call)) BETWEEN 1 AND 32)")
	}
}

// TestMigrate0006_DownRoundTrips exercises the down migration: from head, roll 0006
// back (restoring the <=10 / 1..20 ceilings) then re-apply it. Proves the down SQL
// parses and rebuilds the three tables cleanly, and that the narrower CHECK is truly
// back in force between the two steps.
func TestMigrate0006_DownRoundTrips(t *testing.T) {
	svc := testService(t) // at head (0006)

	// Roll back just 0006.
	applyMigrationSteps(t, svc, -1)

	if _, err := svc.handle.Exec(`INSERT INTO logbook (id, callsign, name) VALUES (1, 'G4ABC', 'L')`); err != nil {
		t.Fatalf("seed logbook: %v", err)
	}
	// DIGITALVOICE (12 chars) must be rejected again once 0006 is reverted.
	_, err := svc.handle.Exec(`INSERT INTO qso
		(uuid, call, band, mode, freq, qso_date, time_on, time_off,
		 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		VALUES ('01920000-0000-7000-8000-000000000001','G4ABC','40m','DIGITALVOICE',
		        7050000,'20250508','0845','0845','59','59','Test',?,1)`,
		strings.Repeat("0", 64))
	if err == nil {
		t.Fatalf("after 0006 down, DIGITALVOICE MODE should violate the restored CHECK(mode <= 10)")
	}

	// Re-apply 0006; DIGITALVOICE stores again.
	applyMigrationSteps(t, svc, 1)
	if _, err := svc.handle.Exec(`INSERT INTO qso
		(uuid, call, band, mode, freq, qso_date, time_on, time_off,
		 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		VALUES ('01920000-0000-7000-8000-000000000001','G4ABC','40m','DIGITALVOICE',
		        7050000,'20250508','0845','0845','59','59','Test',?,1)`,
		strings.Repeat("0", 64)); err != nil {
		t.Fatalf("after 0006 re-applied, DIGITALVOICE should insert: %v", err)
	}
}
