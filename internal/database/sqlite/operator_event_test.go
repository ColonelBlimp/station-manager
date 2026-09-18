package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Persistence proofs for W-0001 / ADR 0076 — RecordOperatorEvent, the
// store-owned insert-and-prune writer. The producing boundaries (browser
// export.adif_failed, daemon forward.failed) are a later slice; these tests
// drive the writer directly with a wired notification kind.

// recordNotification records one forward.failed notification tagged with a
// sequence number in its detail, so the ring's contents are identifiable by n.
func recordNotification(t *testing.T, svc *Service, n int) error {
	t.Helper()
	return svc.RecordOperatorEvent(context.Background(), OperatorEventInput{
		Category: "notification",
		Kind:     "forward.failed",
		Severity: "warn",
		Build:    "v-test",
		Detail:   json.RawMessage(fmt.Sprintf(`{"n":%d}`, n)),
	})
}

// remainingNs returns the detail `n` values of every operator_event row in the
// category, ordered by id (insertion order).
func remainingNs(t *testing.T, svc *Service, category string) []int {
	t.Helper()
	rows, err := svc.handle.Query(
		`SELECT json_extract(detail,'$.n') FROM operator_event WHERE category = ? ORDER BY id`, category)
	if err != nil {
		t.Fatalf("query ring: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan n: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	return out
}

// Success: with the ring full at 500 (n 1..500), recording the 501st event
// evicts exactly the oldest — the survivors are precisely events 2..501.
func TestRecordOperatorEvent_Prunes501stLeaving2Through501(t *testing.T) {
	svc := testService(t)

	for i := 1; i <= 500; i++ {
		if err := recordNotification(t, svc, i); err != nil {
			t.Fatalf("seed event %d: %v", i, err)
		}
	}
	// The ring is exactly full; nothing has been pruned yet.
	if got := remainingNs(t, svc, "notification"); len(got) != 500 || got[0] != 1 || got[499] != 500 {
		t.Fatalf("pre-501 ring is not [1..500]: len=%d first=%v last=%v", len(got), got[0], got[len(got)-1])
	}

	if err := recordNotification(t, svc, 501); err != nil {
		t.Fatalf("record 501st: %v", err)
	}

	got := remainingNs(t, svc, "notification")
	want := make([]int, 500)
	for i := range want {
		want[i] = i + 2 // 2..501
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after 501st insert ring = %v..%v (len %d), want exactly events 2..501",
			firstN(got, 3), lastN(got, 3), len(got))
	}
}

// Failure: a forced prune failure must roll back BOTH the new event and the
// attempted eviction — leaving the prior ring unchanged at 500 — and must not
// touch a separately committed QSO. The writer surfaces an operation-tagged
// error.
func TestRecordOperatorEvent_PruneFailureRollsBackLeavingRingIntact(t *testing.T) {
	svc := testService(t)

	// A separately committed QSO, written in its own transaction before the
	// failing history write. It must survive untouched.
	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}
	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	if _, err := svc.InsertQso(qso); err != nil {
		t.Fatalf("insert qso: %v", err)
	}

	// Fill the ring to exactly 500 (n 1..500).
	for i := 1; i <= 500; i++ {
		if err := recordNotification(t, svc, i); err != nil {
			t.Fatalf("seed event %d: %v", i, err)
		}
	}

	// Force the prune's DELETE to fail: a BEFORE DELETE trigger that aborts. The
	// 501st insert will succeed inside the writer's tx, then its prune DELETE of
	// the oldest row hits this trigger, so the whole tx must roll back.
	if _, err := svc.handle.Exec(
		`CREATE TRIGGER test_block_oe_delete BEFORE DELETE ON operator_event
		 BEGIN SELECT RAISE(ABORT, 'prune blocked by test'); END;`); err != nil {
		t.Fatalf("install abort trigger: %v", err)
	}

	err = recordNotification(t, svc, 501)
	if err == nil {
		t.Fatal("record 501st should fail: the prune DELETE is blocked, so the tx must roll back")
	}
	if !strings.Contains(err.Error(), "sqlite.Service.RecordOperatorEvent") {
		t.Errorf("error is not operation-tagged: %v", err)
	}

	// Prior ring unchanged: still exactly n 1..500, so the new event (501) was
	// rolled back AND the oldest (1) was NOT evicted.
	got := remainingNs(t, svc, "notification")
	if len(got) != 500 || got[0] != 1 || got[499] != 500 {
		t.Fatalf("ring changed after rollback: len=%d first=%v last=%v — want exactly [1..500]",
			len(got), firstN(got, 3), lastN(got, 3))
	}
	for _, n := range got {
		if n == 501 {
			t.Fatalf("rolled-back event 501 is present — the insert was not undone")
		}
	}

	// The separately committed QSO is intact.
	if _, err := svc.FetchQsoByUUIDWithContext(context.Background(), qso.UUID); err != nil {
		t.Fatalf("separately committed QSO must survive the history-write failure: %v", err)
	}
}

func firstN(xs []int, n int) []int {
	if len(xs) < n {
		return xs
	}
	return xs[:n]
}

func lastN(xs []int, n int) []int {
	if len(xs) < n {
		return xs
	}
	return xs[len(xs)-n:]
}

// W-0020: a producer-supplied occurrence time is stored as given (UTC), and a
// zero one keeps the column default — the write time. The recorder queues
// facts asynchronously, so without this a row would date an alarm at the write,
// not at the event.
func TestRecordOperatorEvent_StoresTheSuppliedOccurrenceTimeOrDefaultsToNow(t *testing.T) {
	svc := testService(t)
	at := time.Date(2026, 9, 18, 7, 30, 15, 0, time.UTC)
	if err := svc.RecordOperatorEvent(context.Background(), OperatorEventInput{
		Category: "alarm", Kind: "tx_alarm.raised", Severity: "error", Build: "v-test",
		Detail: json.RawMessage(`{"code":"tx_unconfirmed"}`), OccurredAt: at,
	}); err != nil {
		t.Fatalf("record with occurrence time: %v", err)
	}
	before := time.Now().UTC().Add(-2 * time.Second)
	if err := recordNotification(t, svc, 1); err != nil { // zero OccurredAt
		t.Fatalf("record without occurrence time: %v", err)
	}
	rows, err := svc.FetchOperatorEventsByCategoryWithContext(context.Background(), "alarm", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("fetch alarm rows: %v, n=%d", err, len(rows))
	}
	if !rows[0].OccurredAt.Equal(at) {
		t.Errorf("occurred_at = %v, want the supplied %v", rows[0].OccurredAt, at)
	}
	rows, err = svc.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("fetch notification rows: %v, n=%d", err, len(rows))
	}
	if rows[0].OccurredAt.Before(before) {
		t.Errorf("occurred_at = %v, want the write time (after %v) when none is supplied", rows[0].OccurredAt, before)
	}
}

// W-0020 slice 4: the cross-category read. Newest first by id across
// categories; category and severity narrow; the limit is bounded by the whole
// ring (retention × categories), and out of range is an error, never a clamp.
func TestFetchOperatorEvents_CrossCategoryNewestFirstWithFilters(t *testing.T) {
	svc := testService(t)
	rec := func(category, kind, severity string) {
		t.Helper()
		if err := svc.RecordOperatorEvent(context.Background(), OperatorEventInput{
			Category: category, Kind: kind, Severity: severity, Build: "v-test", Detail: json.RawMessage(`{}`),
		}); err != nil {
			t.Fatalf("record %s/%s: %v", category, kind, err)
		}
	}
	rec("notification", "forward.failed", "warn")
	rec("alarm", "tx_alarm.raised", "error")
	rec("alarm", "tx_alarm.cleared", "info")
	rec("notification", "export.adif_failed", "error")

	all, err := svc.FetchOperatorEventsWithContext(context.Background(), OperatorEventFilter{}, 10)
	if err != nil {
		t.Fatalf("fetch all: %v", err)
	}
	kinds := func(evs []types.OperatorEvent) []string {
		out := make([]string, 0, len(evs))
		for _, e := range evs {
			out = append(out, e.Kind)
		}
		return out
	}
	if got := kinds(all); strings.Join(got, ",") != "export.adif_failed,tx_alarm.cleared,tx_alarm.raised,forward.failed" {
		t.Errorf("all = %v, want newest first across categories", got)
	}
	alarms, _ := svc.FetchOperatorEventsWithContext(context.Background(), OperatorEventFilter{Category: "alarm"}, 10)
	if got := kinds(alarms); strings.Join(got, ",") != "tx_alarm.cleared,tx_alarm.raised" {
		t.Errorf("alarm = %v", got)
	}
	errs, _ := svc.FetchOperatorEventsWithContext(context.Background(), OperatorEventFilter{Severity: "error"}, 10)
	if got := kinds(errs); strings.Join(got, ",") != "export.adif_failed,tx_alarm.raised" {
		t.Errorf("severity=error = %v", got)
	}
	both, _ := svc.FetchOperatorEventsWithContext(context.Background(), OperatorEventFilter{Category: "alarm", Severity: "info"}, 10)
	if got := kinds(both); strings.Join(got, ",") != "tx_alarm.cleared" {
		t.Errorf("alarm+info = %v", got)
	}
	if two, _ := svc.FetchOperatorEventsWithContext(context.Background(), OperatorEventFilter{}, 2); len(two) != 2 {
		t.Errorf("limit 2 returned %d", len(two))
	}
	if OperatorEventFetchLimitMax != operatorEventRetentionPerCategory*2 {
		t.Errorf("fetch limit max = %d, want retention × the two categories = %d", OperatorEventFetchLimitMax, operatorEventRetentionPerCategory*2)
	}
	for _, bad := range []int{0, -1, OperatorEventFetchLimitMax + 1} {
		if _, err := svc.FetchOperatorEventsWithContext(context.Background(), OperatorEventFilter{}, bad); err == nil {
			t.Errorf("limit %d must be an error, not a clamp", bad)
		}
	}
}
