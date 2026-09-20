package sqlite

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/stationevents"
)

// Migration proof for W-0020 slice 1 — 0009 widens operator_event to the alarm
// family. The schema contract this file pins:
//
//   - the CHECK enforces the valid (category, kind) PAIRS, not two independent
//     allowlists — `notification` + `tx_alarm.raised` must be illegal, and so
//     must `alarm` + `forward.failed`;
//   - the pair table is the one in internal/stationevents, enumerated in both
//     directions so the Go vocabulary and the schema cannot drift apart;
//   - the down-migration to 0008 preserves every `notification` row and
//     deliberately discards `alarm` rows (they cannot exist under 0008's CHECKs);
//   - the rebuild carries the id high-water mark, so an evicted row's id is
//     never reissued after a migration round-trip;
//   - the immutability trigger, the per-category index, the severity CHECK and
//     the detail/build rules of 0008 survive the rebuild.

func alarmRow(kind string) (category, severity, detail string) {
	return stationevents.CategoryAlarm, "warn", `{"code":"tx_unconfirmed"}`
}

// Every pair in the Go vocabulary inserts; each kind under the OTHER category
// is refused. A valid row inserts first in every case, so a refusal is the
// pair CHECK and not a table that refuses everything.
func TestMigrate0009_EnforcesTheCategoryKindPairsFromTheVocabulary(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 11 {
		t.Fatalf("schema version = %d, want 11", v)
	}
	pairs := stationevents.KindsByCategory()
	for cat, kinds := range pairs {
		for _, kind := range kinds {
			if err := insertOperatorEvent(t, svc, cat, kind, "info", "v2.0.0-alpha.2-110-gabc", `{}`); err != nil {
				t.Errorf("(%s, %s) must insert: %v", cat, kind, err)
			}
			for other := range pairs {
				if other == cat {
					continue
				}
				if err := insertOperatorEvent(t, svc, other, kind, "info", "v", `{}`); err == nil {
					t.Errorf("(%s, %s) must violate the pair CHECK — %s belongs to %s", other, kind, kind, cat)
				}
			}
		}
	}
	// A category outside the table, even with a real kind.
	if err := insertOperatorEvent(t, svc, "daemon", stationevents.KindTxAlarmRaised, "info", "v", `{}`); err == nil {
		t.Error("category outside {notification, alarm} must violate the CHECK")
	}
	// A kind outside the table, under a real category.
	if err := insertOperatorEvent(t, svc, stationevents.CategoryAlarm, "bridge.disconnected", "info", "v", `{}`); err == nil {
		t.Error("an unlisted kind must violate the CHECK even under a real category")
	}
}

// 0008's other guarantees survive the table rebuild: closed severity, build
// NOT NULL, JSON detail, UPDATE refused, DELETE permitted, the index keyed
// (category, id).
func TestMigrate0009_RebuildKeepsSeverityBuildDetailTriggerAndIndex(t *testing.T) {
	svc := testService(t)
	cat, _, detail := alarmRow(stationevents.KindTxAlarmRaised)

	if err := insertOperatorEvent(t, svc, cat, stationevents.KindTxAlarmRaised, "fatal", "v", detail); err == nil {
		t.Error("severity outside {info,warn,error} must violate the CHECK after the rebuild")
	}
	if _, err := svc.handle.Exec(
		`INSERT INTO operator_event (category, kind, severity, detail) VALUES ('alarm','tx_alarm.raised','error','{}')`); err == nil {
		t.Error("omitting build must fail (NOT NULL, no default) after the rebuild")
	}
	if err := insertOperatorEvent(t, svc, cat, stationevents.KindTxAlarmRaised, "error", "v", `not json`); err == nil {
		t.Error("a non-JSON detail must violate CHECK(json_valid(detail)) after the rebuild")
	}
	if err := insertOperatorEvent(t, svc, cat, stationevents.KindTxAlarmRaised, "error", "v", detail); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.handle.Exec(`UPDATE operator_event SET severity='warn'`); err == nil {
		t.Error("UPDATE must still be refused — the immutability trigger must be recreated by the rebuild")
	}
	if _, err := svc.handle.Exec(`DELETE FROM operator_event`); err != nil {
		t.Errorf("DELETE must still be permitted so retention can prune: %v", err)
	}
	var ddl string
	if err := svc.handle.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='index'
		 AND tbl_name='operator_event' AND name='idx_operator_event_category_id'`).Scan(&ddl); err != nil {
		t.Fatalf("idx_operator_event_category_id missing after the rebuild: %v", err)
	}
	if norm := strings.Join(strings.Fields(strings.ToLower(ddl)), " "); !strings.Contains(norm, "(category, id)") {
		t.Errorf("index is not keyed (category, id): %s", norm)
	}
}

// Down to 0008 keeps every notification row (with its detail) and discards the
// alarm rows — the stated policy of the down file's header — and the 0008
// CHECK is back in force; up restores the alarm pairs with the notification
// rows intact.
func TestMigrate0009_DownKeepsNotificationRowsAndDiscardsAlarmRows(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 11 {
		t.Fatalf("schema version = %d, want 11", v)
	}
	if err := insertOperatorEvent(t, svc, "notification", "forward.failed", "warn", "v",
		`{"qso_id":7,"forwarder":"qrz","action":"insert","attempts":5}`); err != nil {
		t.Fatalf("seed notification row: %v", err)
	}
	if err := insertOperatorEvent(t, svc, "alarm", "tx_alarm.raised", "error", "v", `{"code":"tx_unconfirmed"}`); err != nil {
		t.Fatalf("seed alarm row: %v", err)
	}

	count := func(when, category string) int {
		t.Helper()
		var n int
		if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM operator_event WHERE category = ?`, category).Scan(&n); err != nil {
			t.Fatalf("%s: count %s: %v", when, category, err)
		}
		return n
	}

	migrateToVersion(t, svc, 8) // crosses 0009 down → 0008's closed CHECKs
	if n := count("after down", "notification"); n != 1 {
		t.Errorf("after down: notification rows = %d, want 1 (preserved)", n)
	}
	if n := count("after down", "alarm"); n != 0 {
		t.Errorf("after down: alarm rows = %d, want 0 (discarded by policy)", n)
	}
	var detail string
	if err := svc.handle.QueryRow(`SELECT detail FROM operator_event WHERE category='notification'`).Scan(&detail); err != nil {
		t.Fatalf("after down: read detail: %v", err)
	}
	if !strings.Contains(detail, `"forwarder":"qrz"`) {
		t.Errorf("after down: notification detail not preserved: %s", detail)
	}
	if err := insertOperatorEvent(t, svc, "alarm", "tx_alarm.raised", "error", "v", `{}`); err == nil {
		t.Error("after down: an alarm row must be refused by 0008's CHECK")
	}

	migrateToVersion(t, svc, 11) // re-up through head
	if n := count("after re-up", "notification"); n != 1 {
		t.Errorf("after re-up: notification rows = %d, want 1", n)
	}
	if err := insertOperatorEvent(t, svc, "alarm", "tx_alarm.cleared", "info", "v", `{}`); err != nil {
		t.Errorf("after re-up: an alarm row must insert again: %v", err)
	}
}

// The rebuild must carry AUTOINCREMENT's high-water mark. Without it an empty
// table restarts at 1 and an id that retention already evicted is reissued —
// and id is the monotonic arrival order both the ring and the SPA key on.
func TestMigrate0009_RebuildKeepsTheIdHighWaterMark(t *testing.T) {
	svc := testService(t)
	if err := insertOperatorEvent(t, svc, "notification", "forward.failed", "warn", "v", `{}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.handle.Exec(`DELETE FROM operator_event`); err != nil { // retention evicted it
		t.Fatalf("evict: %v", err)
	}
	migrateToVersion(t, svc, 8)
	migrateToVersion(t, svc, 11)
	if err := insertOperatorEvent(t, svc, "alarm", "tx_alarm.raised", "error", "v", `{}`); err != nil {
		t.Fatalf("insert after round-trip: %v", err)
	}
	var id int64
	if err := svc.handle.QueryRow(`SELECT MAX(id) FROM operator_event`).Scan(&id); err != nil {
		t.Fatalf("max id: %v", err)
	}
	if id != 2 {
		t.Errorf("id after the round-trip = %d, want 2 — the rebuild reissued an evicted id", id)
	}
}
