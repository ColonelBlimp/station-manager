package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Retrieval proofs for W-0001 / ADR 0076 — the newest-N-per-category read-path.
// The producing boundaries and any HTTP surface are later slices; these drive
// the store method directly.

// Newest-first with a limit: five events (n 1..5), limit 3, returns exactly the
// three newest — n 5, 4, 3 in that order.
func TestFetchOperatorEventsByCategory_NewestFirstWithLimit(t *testing.T) {
	svc := testService(t)
	for i := 1; i <= 5; i++ {
		if err := recordNotification(t, svc, i); err != nil {
			t.Fatalf("seed event %d: %v", i, err)
		}
	}

	got, err := svc.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 3)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantNs := []int{5, 4, 3}
	for i, ev := range got {
		var d struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(ev.Detail, &d); err != nil {
			t.Fatalf("row %d detail not JSON: %v", i, err)
		}
		if d.N != wantNs[i] {
			t.Fatalf("row %d n = %d, want %d — result is not newest-first [5,4,3]", i, d.N, wantNs[i])
		}
	}
}

// A returned row carries the full structured fields, not just an id/detail.
func TestFetchOperatorEventsByCategory_ReturnsFullFields(t *testing.T) {
	svc := testService(t)
	if err := svc.RecordOperatorEvent(context.Background(), OperatorEventInput{
		Category: "notification",
		Kind:     "export.adif_failed",
		Severity: "error",
		Build:    "v2.3.4-1-gabc",
		Detail:   json.RawMessage(`{"logbook_id":7}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := svc.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 10)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.ID <= 0 {
		t.Errorf("ID = %d, want a positive assigned id", ev.ID)
	}
	if ev.Category != "notification" {
		t.Errorf("Category = %q, want notification", ev.Category)
	}
	if ev.Kind != "export.adif_failed" {
		t.Errorf("Kind = %q, want export.adif_failed", ev.Kind)
	}
	if ev.Severity != "error" {
		t.Errorf("Severity = %q, want error", ev.Severity)
	}
	if ev.Build != "v2.3.4-1-gabc" {
		t.Errorf("Build = %q, want v2.3.4-1-gabc", ev.Build)
	}
	if ev.OccurredAt.IsZero() {
		t.Errorf("OccurredAt is zero — the daemon-stamped occurrence time is missing")
	}
	if string(ev.Detail) != `{"logbook_id":7}` {
		t.Errorf("Detail = %q, want the stored JSON verbatim", string(ev.Detail))
	}
}

// A limit outside [1, 500] fails with an operation-tagged error — it must NOT
// silently clamp. The boundaries 1 and 500 are valid.
func TestFetchOperatorEventsByCategory_RejectsOutOfRangeLimit(t *testing.T) {
	svc := testService(t)
	// Seed a handful so a silently-clamping implementation would return rows
	// rather than error — the failure mode this test exists to catch.
	for i := 1; i <= 5; i++ {
		if err := recordNotification(t, svc, i); err != nil {
			t.Fatalf("seed event %d: %v", i, err)
		}
	}

	for _, bad := range []int{0, -1, 501, 1000} {
		got, err := svc.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", bad)
		if err == nil {
			t.Errorf("limit %d must be rejected, got %d rows (silent clamp)", bad, len(got))
			continue
		}
		if !strings.Contains(err.Error(), "sqlite.Service.FetchOperatorEventsByCategoryWithContext") {
			t.Errorf("limit %d error is not operation-tagged: %v", bad, err)
		}
	}

	// The inclusive boundaries are valid.
	for _, ok := range []int{1, 500} {
		if _, err := svc.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", ok); err != nil {
			t.Errorf("limit %d must be accepted (inclusive boundary): %v", ok, err)
		}
	}
}

// The read performs no mutation: the full table state is identical before and
// after a fetch.
func TestFetchOperatorEventsByCategory_ReadIsNonMutating(t *testing.T) {
	svc := testService(t)
	for i := 1; i <= 5; i++ {
		if err := recordNotification(t, svc, i); err != nil {
			t.Fatalf("seed event %d: %v", i, err)
		}
	}

	snapshot := func() string {
		t.Helper()
		rows, err := svc.handle.Query(
			`SELECT id, category, kind, severity, occurred_at, build, detail
			 FROM operator_event ORDER BY id`)
		if err != nil {
			t.Fatalf("snapshot query: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var b strings.Builder
		for rows.Next() {
			var id int64
			var category, kind, severity, occurredAt, build, detail string
			if err := rows.Scan(&id, &category, &kind, &severity, &occurredAt, &build, &detail); err != nil {
				t.Fatalf("snapshot scan: %v", err)
			}
			fmt.Fprintf(&b, "%d|%s|%s|%s|%s|%s|%s\n",
				id, category, kind, severity, occurredAt, build, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("snapshot rows err: %v", err)
		}
		return b.String()
	}

	before := snapshot()
	if _, err := svc.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 3); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	after := snapshot()
	if before != after {
		t.Fatalf("read mutated the table:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
