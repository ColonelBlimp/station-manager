package sqlite

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/stationevents"
)

// Migration 0013 (ADR 0071 activation follow-up): the notification category
// admits the two archive outcomes; the down step restores 0009's pairs and
// discards only those rows, keeping every other row with its id.
func TestMigrate0013_ArchiveOutcomesAreNotificationKinds_DownDiscardsOnlyThem(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 13 {
		t.Fatalf("schema version = %d, want 13", v)
	}
	for _, kind := range []string{stationevents.KindArchiveActivated, stationevents.KindArchiveActivationFailed} {
		if err := insertOperatorEvent(t, svc, stationevents.CategoryNotification, kind, "info", "v", `{"archive_id":"x","label":"Drill"}`); err != nil {
			t.Errorf("(notification, %s) must insert at head: %v", kind, err)
		}
		if err := insertOperatorEvent(t, svc, stationevents.CategoryAlarm, kind, "info", "v", `{}`); err == nil {
			t.Errorf("(alarm, %s) must violate the pair CHECK", kind)
		}
	}
	if err := insertOperatorEvent(t, svc, stationevents.CategoryNotification, stationevents.KindForwardFailed, "warn", "v", `{"forwarder":"qrz"}`); err != nil {
		t.Fatalf("a forward.failed row must insert: %v", err)
	}
	if _, err := svc.DowngradeLogSchemaTo(12); err != nil {
		t.Fatalf("downgrade to 12: %v", err)
	}
	if v := schemaVersion(t, svc); v != 12 {
		t.Fatalf("schema version = %d after the down step, want 12", v)
	}
	var archiveRows, otherRows int
	if err := svc.handle.QueryRow(`select count(*) from operator_event where kind like 'archive.%'`).Scan(&archiveRows); err != nil {
		t.Fatal(err)
	}
	if err := svc.handle.QueryRow(`select count(*) from operator_event where kind not like 'archive.%'`).Scan(&otherRows); err != nil {
		t.Fatal(err)
	}
	if archiveRows != 0 || otherRows != 1 {
		t.Fatalf("after the down step: archive rows = %d (want 0, discarded), other rows = %d (want 1, kept)", archiveRows, otherRows)
	}
	if err := insertOperatorEvent(t, svc, stationevents.CategoryNotification, stationevents.KindArchiveActivated, "info", "v", `{}`); err == nil {
		t.Error("archive.activated must be refused under 0009's CHECK")
	}
}
