package sqlite

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/stationevents"
)

// Migration 0013 (ADR 0071 activation follow-up): the notification category
// admits the two archive outcomes, and only as notifications.
func TestMigrate0013_ArchiveOutcomesAreNotificationKinds(t *testing.T) {
	svc := testService(t)
	if v := schemaVersion(t, svc); v != 19 {
		t.Fatalf("schema version = %d, want 19", v)
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
}
