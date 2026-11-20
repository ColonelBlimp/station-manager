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
//   - a file at 0008 keeps every `notification` row across the rebuild, and
//     `alarm` rows (which cannot exist under 0008's CHECKs) are admitted after it;
//   - the rebuild carries the id high-water mark, so an evicted row's id is
//     never reissued after the migration;
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
	if v := schemaVersion(t, svc); v != 19 {
		t.Fatalf("schema version = %d, want 19", v)
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

// A file at 0008 holds notification rows only: 0008's CHECK refuses an alarm.
// Going up through 0009 keeps every notification row with its detail and
// admits the alarm pairs. The file is built up to 8, as that build left it;
// checked straight after 0009 and at the head.
func TestMigrate0009_AFileAt8KeepsItsNotificationRowsAndGainsAlarms(t *testing.T) {
	svc := testServiceAtVersion(t, 8)
	if err := insertOperatorEvent(t, svc, "notification", "forward.failed", "warn", "v",
		`{"qso_id":7,"forwarder":"qrz","action":"insert","attempts":5}`); err != nil {
		t.Fatalf("seed notification row at 8: %v", err)
	}
	if err := insertOperatorEvent(t, svc, "alarm", "tx_alarm.raised", "error", "v", `{}`); err == nil {
		t.Fatal("at 8 an alarm row inserted; the fixture is not 0008's closed CHECK")
	}

	kept := func(when string) {
		t.Helper()
		var n int
		if err := svc.handle.QueryRow(`SELECT COUNT(*) FROM operator_event WHERE category = 'notification'`).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: notification rows = %d (%v), want 1", when, n, err)
		}
		var detail string
		if err := svc.handle.QueryRow(`SELECT detail FROM operator_event WHERE kind = 'forward.failed'`).Scan(&detail); err != nil {
			t.Fatalf("%s: read detail: %v", when, err)
		}
		if !strings.Contains(detail, `"forwarder":"qrz"`) || !strings.Contains(detail, `"attempts":5`) {
			t.Errorf("%s: notification detail not preserved: %s", when, detail)
		}
	}

	migrateToVersion(t, svc, 9)
	kept("after 0009")
	if err := insertOperatorEvent(t, svc, "alarm", "tx_alarm.raised", "error", "v", `{"code":"tx_unconfirmed"}`); err != nil {
		t.Errorf("after 0009: an alarm row must insert: %v", err)
	}

	if err := svc.Migrate(); err != nil {
		t.Fatalf("migrate to the head: %v", err)
	}
	if v := schemaVersion(t, svc); v != 19 {
		t.Fatalf("schema version = %d, want 19", v)
	}
	kept("at the head")
	if err := insertOperatorEvent(t, svc, "alarm", "tx_alarm.cleared", "info", "v", `{}`); err != nil {
		t.Errorf("at the head: an alarm row must insert: %v", err)
	}
}

// The rebuild must carry AUTOINCREMENT's high-water mark. Without it an empty
// table restarts at 1 and an id that retention already evicted is reissued —
// and id is the monotonic arrival order both the ring and the SPA key on. On a
// file built up to 8: a row is written and evicted there, then 0009 rebuilds
// the empty table.
func TestMigrate0009_RebuildKeepsTheIdHighWaterMark(t *testing.T) {
	svc := testServiceAtVersion(t, 8)
	if err := insertOperatorEvent(t, svc, "notification", "forward.failed", "warn", "v", `{}`); err != nil {
		t.Fatalf("seed at 8: %v", err)
	}
	if _, err := svc.handle.Exec(`DELETE FROM operator_event`); err != nil { // retention evicted it
		t.Fatalf("evict: %v", err)
	}
	migrateToVersion(t, svc, 9)
	if err := insertOperatorEvent(t, svc, "alarm", "tx_alarm.raised", "error", "v", `{}`); err != nil {
		t.Fatalf("insert after the rebuild: %v", err)
	}
	var id int64
	if err := svc.handle.QueryRow(`SELECT MAX(id) FROM operator_event`).Scan(&id); err != nil {
		t.Fatalf("max id: %v", err)
	}
	if id != 2 {
		t.Errorf("id after the rebuild = %d, want 2 — the rebuild reissued an evicted id", id)
	}
}
