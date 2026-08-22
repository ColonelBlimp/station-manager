package sqlite

import (
	"strings"
	"testing"
)

// Migration proof for W-0001 / ADR 0076 — operator_event, the local
// notification-history pilot store. This file pins the SCHEMA contract only.
// The producing-boundary Go guards (the allowlisted typed browser request; the
// forward.failed producer that persists QSO id/forwarder/action/attempts but
// NEVER the provider Reason) and the retention prune are separate slices and are
// deliberately NOT claimed here.
//
// Shape (ADR 0076): a categorised operator-facing event store in the log DB,
// following qso_history's JSON-detail pattern but BOUNDED rather than
// append-only — retention prunes the oldest rows per category, so DELETE is
// permitted and only UPDATE is refused. Only the `notification` category and
// exactly two durable kinds are wired now; both CHECKs are CLOSED so an
// unplanned category/kind fails loudly. severity/occurred_at/build are stamped
// daemon-side; `detail` carries only typed JSON, never raw provider text.

// insertOperatorEvent is a raw-SQL writer so the schema proofs exercise the
// column CHECKs directly, not a Go-side guard that does not exist yet.
func insertOperatorEvent(t *testing.T, svc *Service, category, kind, severity, build, detail string) error {
	t.Helper()
	_, err := svc.handle.Exec(
		`INSERT INTO operator_event (category, kind, severity, build, detail) VALUES (?,?,?,?,?)`,
		category, kind, severity, build, detail)
	return err
}

// The table exists at head and accepts each wired (category, kind) with a
// daemon-stamped severity/build and typed JSON detail — one browser-originated
// kind and one daemon-originated kind, the two paths ADR 0076 wires.
func TestMigrate0008_OperatorEventAcceptsWiredKinds(t *testing.T) {
	svc := testService(t)

	if err := insertOperatorEvent(t, svc,
		"notification", "export.adif_failed", "error", "v2.3.4-1-gabc", `{"logbook_id":1}`); err != nil {
		t.Errorf("browser-originated export.adif_failed should insert: %v", err)
	}
	if err := insertOperatorEvent(t, svc,
		"notification", "forward.failed", "warn", "v2.3.4-1-gabc",
		`{"qso_id":7,"forwarder":"qrz","action":"insert","attempts":5}`); err != nil {
		t.Errorf("daemon-originated forward.failed should insert: %v", err)
	}
}

// Both closed CHECKs bite. A valid row inserts first, so "rejected" means the
// CHECK rejected this specific value — not that the table refuses everything.
// When a later category/kind is deliberately added, its own migration widens the
// CHECK (the 0002/0003/0006 pattern); until then the failure is the point.
func TestMigrate0008_OperatorEventClosesCategoryKindAndSeverity(t *testing.T) {
	svc := testService(t)
	if err := insertOperatorEvent(t, svc,
		"notification", "forward.failed", "error", "v", `{}`); err != nil {
		t.Fatalf("a wired row must insert, or the rejections below prove nothing: %v", err)
	}

	// Categories left Proposed by ADR 0076 (alarm/daemon/qso) are not wired.
	if err := insertOperatorEvent(t, svc,
		"daemon", "export.adif_failed", "error", "v", `{}`); err == nil {
		t.Error("category outside {'notification'} must violate the CHECK")
	}
	// A kind not on the two-kind allowlist (a later boundary, deliberately unwired).
	if err := insertOperatorEvent(t, svc,
		"notification", "bridge.disconnected", "error", "v", `{}`); err == nil {
		t.Error("kind outside the two wired durable kinds must violate the CHECK")
	}
	// severity is a closed set matching the toast levels (ADR 0008).
	if err := insertOperatorEvent(t, svc,
		"notification", "forward.failed", "fatal", "v", `{}`); err == nil {
		t.Error("severity outside {info,warn,error} must violate the CHECK")
	}
}

// build is NOT NULL with no default: an insert that omits the daemon-stamped
// build version must fail LOUDLY, never acquire a value nobody chose — the rule
// 0007 established for qso_upload.origin. A build-stamped store exists precisely
// to keep un-attributable rows out.
func TestMigrate0008_OperatorEventRequiresBuildAndValidDetail(t *testing.T) {
	svc := testService(t)

	if _, err := svc.handle.Exec(
		`INSERT INTO operator_event (category, kind, severity, detail)
		 VALUES ('notification','forward.failed','error','{}')`); err == nil {
		t.Error("omitting build must fail (NOT NULL, no default)")
	}
	// detail must be valid JSON — the store never holds a raw non-JSON blob.
	if err := insertOperatorEvent(t, svc,
		"notification", "forward.failed", "error", "v", `not json`); err == nil {
		t.Error("a non-JSON detail must violate CHECK(json_valid(detail))")
	}
}

// A recorded event is an immutable fact: UPDATE is refused by trigger. DELETE is
// deliberately NOT refused — retention prunes the oldest rows per category, so
// the ring must be able to evict. This is the one place operator_event diverges
// from qso_history's append-only (no-update AND no-delete) guard.
func TestMigrate0008_OperatorEventIsImmutableButPrunable(t *testing.T) {
	svc := testService(t)
	if err := insertOperatorEvent(t, svc,
		"notification", "forward.failed", "error", "v", `{}`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := svc.handle.Exec(`UPDATE operator_event SET severity='warn' WHERE id=1`); err == nil {
		t.Error("UPDATE must be refused — a recorded event is immutable")
	}
	if _, err := svc.handle.Exec(`DELETE FROM operator_event WHERE id=1`); err != nil {
		t.Errorf("DELETE must be permitted so retention can prune: %v", err)
	}
	var n int
	if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM operator_event`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("row count after prune = %d, want 0", n)
	}
}

// The per-category, insertion-ordered index backs both retention directions
// (newest-N retrieval and oldest-first eviction) on one index. Its absence is a
// silent full-scan, not an error, so the key is asserted explicitly.
func TestMigrate0008_OperatorEventHasPerCategoryIndex(t *testing.T) {
	svc := testService(t)
	var ddl string
	if err := svc.handle.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='index'
		 AND tbl_name='operator_event' AND name='idx_operator_event_category_id'`).Scan(&ddl); err != nil {
		t.Fatalf("idx_operator_event_category_id missing: %v", err)
	}
	norm := strings.Join(strings.Fields(strings.ToLower(ddl)), " ")
	if !strings.Contains(norm, "(category, id)") {
		t.Errorf("index is not keyed (category, id): %s", norm)
	}
}

// The migration is reversible: down drops the table, up restores it. Doubles as
// the in-suite reversion guard — a build that ships the table without the
// migration, or a down that forgets it, fails here.
func TestMigrate0008_DownDropsTableUpRestoresIt(t *testing.T) {
	svc := testService(t)

	if v := schemaVersion(t, svc); v != 8 {
		t.Fatalf("schema version = %d, want 8 — 0008 must be head for the step-back to target it", v)
	}
	assertTable := func(when string, want bool) {
		t.Helper()
		has, err := hasTable(svc.handle, "operator_event")
		if err != nil {
			t.Fatalf("%s: hasTable: %v", when, err)
		}
		if has != want {
			t.Errorf("%s: operator_event present=%v, want %v", when, has, want)
		}
	}
	assertTable("after up", true)
	applyMigrationSteps(t, svc, -1)
	assertTable("after down", false)
	applyMigrationSteps(t, svc, 1)
	assertTable("after re-up", true)
}
