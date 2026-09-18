package recorder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/stationevents"
)

// The recorder IS the observer both seams take (W-0020 slice 3 wires it).
var (
	_ bridge.AlarmObserver = (*Recorder)(nil)
	_ ft8.SessionObserver  = (*Recorder)(nil)
)

// ---- test doubles ----

// fakeStore collects rows. When `hold` is set, every write blocks until it is
// released or the write's context ends — the stalled store of AC8.
type fakeStore struct {
	mu     sync.Mutex
	rows   []sqlite.OperatorEventInput
	calls  int
	hold   chan struct{} // nil = never block
	failOn map[int]error // call index (1-based) → error to return
	first  chan struct{} // closed once the first call is IN the store
	fOnce  sync.Once
}

func newFakeStore() *fakeStore {
	return &fakeStore{first: make(chan struct{}), failOn: map[int]error{}}
}

func (s *fakeStore) RecordOperatorEvent(ctx context.Context, ev sqlite.OperatorEventInput) error {
	s.mu.Lock()
	s.calls++
	n := s.calls
	hold := s.hold
	ferr := s.failOn[n]
	s.mu.Unlock()
	s.fOnce.Do(func() { close(s.first) })
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if ferr != nil {
		return ferr
	}
	s.mu.Lock()
	s.rows = append(s.rows, ev)
	s.mu.Unlock()
	return nil
}

func (s *fakeStore) snapshot() []sqlite.OperatorEventInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sqlite.OperatorEventInput(nil), s.rows...)
}

func (s *fakeStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuf) lines(substr string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, l := range strings.Split(b.b.String(), "\n") {
		if strings.Contains(l, substr) {
			out = append(out, l)
		}
	}
	return out
}

func newRecorder(t *testing.T, store Store) (*Recorder, *syncBuf) {
	t.Helper()
	buf := &syncBuf{}
	r := New(store, logging.NewForWriter(buf), "v2.0.0-test")
	return r, buf
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func detailOf(t *testing.T, row sqlite.OperatorEventInput) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(row.Detail, &m); err != nil {
		t.Fatalf("detail %s: %v", row.Detail, err)
	}
	return m
}

// ---- the conversion table (AC7) ----

// Every sealed fact converts to its ruled (kind, severity) with a detail whose
// keys are EXACTLY the fixed set for that kind — enumerated here so a fact
// added without a table entry, or a detail that grows a free-text field, fails.
func TestConversion_IsTotalOverTheSealedFactSetWithFixedTypedDetails(t *testing.T) {
	at := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	raised := at.Add(-1500 * time.Millisecond)
	cases := []struct {
		fact     stationevents.Fact
		kind     string
		severity string
		keys     []string
	}{
		{stationevents.TxAlarmRaised{Code: "tx_unconfirmed", At: at}, stationevents.KindTxAlarmRaised, "error", []string{"code"}},
		{stationevents.TxAlarmCleared{Code: "tx_still_keyed", RaisedAt: raised, At: at}, stationevents.KindTxAlarmCleared, "info", []string{"active_ms", "code"}},
		{stationevents.DriveAlarmRaised{Code: "drive_no_output", At: at}, stationevents.KindDriveAlarmRaised, "error", []string{"code"}},
		{stationevents.DriveAlarmRecovered{Code: "drive_no_output", At: at}, stationevents.KindDriveAlarmCleared, "info", []string{"code"}},
		{stationevents.TxDisarmed{Cause: "cat_lost", At: at}, stationevents.KindTxDisarmed, "warn", []string{"cause"}},
		{stationevents.ExchangeTerminated{Cause: "unattended", PartnerCall: "w1abc", Rung: "rr73", At: at}, stationevents.KindSessionTerminated, "warn", []string{"cause", "partner_call", "rung"}},
	}
	// The sealed set: one case per fact type, no more, no fewer.
	seen := map[reflect.Type]bool{}
	for _, c := range cases {
		seen[reflect.TypeOf(c.fact)] = true
		row := rowFor(c.fact, "v-build")
		if row.Category != stationevents.CategoryAlarm || row.Kind != c.kind || row.Severity != c.severity {
			t.Errorf("%T → (%s, %s, %s), want (alarm, %s, %s)", c.fact, row.Category, row.Kind, row.Severity, c.kind, c.severity)
		}
		if row.Build != "v-build" || !row.OccurredAt.Equal(at) {
			t.Errorf("%T: build=%q occurred_at=%v, want v-build at %v", c.fact, row.Build, row.OccurredAt, at)
		}
		m := detailOf(t, row)
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}
		if len(keys) != len(c.keys) || !containsAll(keys, c.keys) {
			t.Errorf("%T detail keys = %v, want exactly %v", c.fact, keys, c.keys)
		}
		if _, listed := stationevents.KindsByCategory()[stationevents.CategoryAlarm]; !listed {
			t.Fatal("alarm category missing from the vocabulary")
		}
		if !contains(stationevents.KindsByCategory()[stationevents.CategoryAlarm], row.Kind) {
			t.Errorf("kind %q is not in the vocabulary's alarm kinds — the schema would refuse it", row.Kind)
		}
	}
	if len(seen) != 6 {
		t.Errorf("cases cover %d fact types, want the 6 sealed ones", len(seen))
	}
	// The cleared row says how long the alarm stood, in ms from the two stamps.
	m := detailOf(t, rowFor(cases[1].fact, "v"))
	if m["active_ms"] != float64(1500) {
		t.Errorf("active_ms = %v, want 1500", m["active_ms"])
	}
	// … and omits it when the bridge had no raise stamp, rather than inventing 0.
	m = detailOf(t, rowFor(stationevents.TxAlarmCleared{Code: "tx_unconfirmed", At: at}, "v"))
	if _, has := m["active_ms"]; has {
		t.Errorf("active_ms present without a raise stamp: %v", m)
	}
}

// Identifiers our own code minted are re-checked; partner_call is decoded
// input and is normalised and bounded (ruling 5). Neither can carry free text.
func TestConversion_BoundsIdentifiersAndNormalisesThePartnerCall(t *testing.T) {
	at := time.Now()
	m := detailOf(t, rowFor(stationevents.ExchangeTerminated{
		Cause: "rig said: NO!", PartnerCall: "  w1abc/p\t", Rung: strings.Repeat("r", 33), At: at}, "v"))
	if m["cause"] != invalidIdent || m["rung"] != invalidIdent {
		t.Errorf("free-text cause / over-long rung must be replaced, got %v", m)
	}
	if m["partner_call"] != "W1ABC/P" {
		t.Errorf("partner_call = %v, want W1ABC/P (trimmed, upper-cased)", m["partner_call"])
	}
	long := detailOf(t, rowFor(stationevents.ExchangeTerminated{
		Cause: "unattended", PartnerCall: strings.Repeat("A", 40) + "\u00e9x", Rung: "tx1", At: at}, "v"))
	if pc := long["partner_call"].(string); len(pc) != stationevents.PartnerCallMaxLen || strings.ContainsRune(pc, 'É') {
		t.Errorf("partner_call = %q (len %d), want %d printable-ASCII chars", pc, len(pc), stationevents.PartnerCallMaxLen)
	}
	if m := detailOf(t, rowFor(stationevents.TxAlarmRaised{Code: "", At: at}, "v")); m["code"] != invalidIdent {
		t.Errorf("empty code must be replaced, got %v", m)
	}
}

// ---- the recorder ----

func TestRecorder_WritesEachFactWithItsOccurrenceTimeAndTheBuild(t *testing.T) {
	store := newFakeStore()
	r, _ := newRecorder(t, store)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	r.TxAlarmRaised("tx_unconfirmed", at)
	r.TxAlarmCleared("tx_unconfirmed", at, at.Add(700*time.Millisecond))
	waitFor(t, func() bool { return len(store.snapshot()) == 2 }, "two rows were not written")
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	rows := store.snapshot()
	if rows[0].Kind != stationevents.KindTxAlarmRaised || !rows[0].OccurredAt.Equal(at) || rows[0].Build != "v2.0.0-test" {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[1].Kind != stationevents.KindTxAlarmCleared || detailOf(t, rows[1])["active_ms"] != float64(700) {
		t.Errorf("row 1 = %+v", rows[1])
	}
}

// AC8 / ruling 8: with the store stalled, producers never wait; the queue holds
// QueueCapacity facts behind the one stuck in the store, every fact beyond that
// is the NEWEST and is dropped with one warning carrying its kind and the
// cumulative count; when the store frees, everything queued lands in order and
// the dropped facts never appear.
func TestRecorder_StalledStoreNeverBlocksProducers_DropsNewestAndLogsEachDrop(t *testing.T) {
	store := newFakeStore()
	store.hold = make(chan struct{})
	r, buf := newRecorder(t, store)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	r.TxAlarmRaised("c0", at) // taken by the worker, now stuck in the store
	<-store.first
	const extra = 3
	for i := 1; i <= QueueCapacity+extra; i++ {
		start := time.Now()
		r.TxDisarmed("cat_lost", at.Add(time.Duration(i)*time.Millisecond))
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Fatalf("enqueue %d took %v — a producer waited on the store", i, d)
		}
	}
	if got := r.Drops(); got != extra {
		t.Fatalf("drops = %d, want %d (queue %d full behind one stuck write)", got, extra, QueueCapacity)
	}
	warns := buf.lines("event dropped, not recorded")
	if len(warns) != extra {
		t.Fatalf("drop warnings = %d, want %d:\n%s", len(warns), extra, strings.Join(warns, "\n"))
	}
	for i, l := range warns {
		if !strings.Contains(l, `"kind":"tx.disarmed"`) || !strings.Contains(l, `"drops":`+itoa(i+1)) {
			t.Errorf("drop warning %d lacks the kind or the cumulative count: %s", i+1, l)
		}
	}

	close(store.hold)
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	rows := store.snapshot()
	if len(rows) != 1+QueueCapacity {
		t.Fatalf("rows written = %d, want %d (the stuck one plus the full queue; the %d newest dropped)", len(rows), 1+QueueCapacity, extra)
	}
	// Drop-newest: the survivors are the FIRST QueueCapacity disarms, in order.
	for i := 1; i <= QueueCapacity; i++ {
		want := at.Add(time.Duration(i) * time.Millisecond)
		if !rows[i].OccurredAt.Equal(want) {
			t.Fatalf("row %d occurred_at = %v, want %v — the queue did not keep the earliest facts", i, rows[i].OccurredAt, want)
		}
	}
}

func TestRecorder_StopDrainsWhatIsQueuedBeforeReturning(t *testing.T) {
	store := newFakeStore()
	store.hold = make(chan struct{})
	r, _ := newRecorder(t, store)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.TxAlarmRaised("c0", time.Now())
	<-store.first
	for i := 0; i < 10; i++ {
		r.DriveAlarmRaised("drive_no_output", time.Now())
	}
	// Free the store just before Stop: the stuck write completes, the worker is
	// cancelled, and teardown writes the ten still queued.
	close(store.hold)
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if n := len(store.snapshot()); n != 11 {
		t.Errorf("rows after Stop = %d, want 11 — Stop must drain the queue", n)
	}
	if r.Drops() != 0 {
		t.Errorf("drops = %d, want 0", r.Drops())
	}
}

func TestRecorder_BeforeStartAndAfterStopFactsAreDroppedAndLogged(t *testing.T) {
	store := newFakeStore()
	r, buf := newRecorder(t, store)
	r.TxAlarmRaised("tx_unconfirmed", time.Now()) // before Start
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	r.TxDisarmed("dial_moved", time.Now()) // after Stop
	if got := r.Drops(); got != 2 {
		t.Errorf("drops = %d, want 2", got)
	}
	if n := len(buf.lines(`"reason":"recorder not running"`)); n != 2 {
		t.Errorf("not-running warnings = %d, want 2", n)
	}
	if n := len(store.snapshot()); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestRecorder_AFailedWriteIsLoggedOnceAndNeverRetried(t *testing.T) {
	store := newFakeStore()
	store.failOn = map[int]error{1: errors.New("disk full")}
	r, buf := newRecorder(t, store)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.TxAlarmRaised("tx_unconfirmed", time.Now())
	r.TxAlarmCleared("tx_unconfirmed", time.Now(), time.Now())
	waitFor(t, func() bool { return len(store.snapshot()) == 1 }, "the second fact was not written")
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if store.callCount() != 2 {
		t.Errorf("store calls = %d, want 2 — the failed write must not be retried", store.callCount())
	}
	if got := r.Drops(); got != 1 {
		t.Errorf("drops = %d, want 1 for the failed write", got)
	}
	fails := buf.lines(`"reason":"write failed"`)
	if len(fails) != 1 || !strings.Contains(fails[0], `"kind":"tx_alarm.raised"`) || !strings.Contains(fails[0], `"drops":1`) {
		t.Errorf("failed-write warnings = %v, want one naming tx_alarm.raised with cumulative count 1", fails)
	}
	if n := len(buf.lines("station events:")); n != 1 {
		t.Errorf("warnings = %d, want one for the failed write", n)
	}
}

// Shutdown with a failing store: Stop waits for the write in flight (in
// production the store's own context timeout bounds it — the fake releases it
// by hand), then the drain stops at its FIRST failure and names every fact that
// never landed, one warning each.
func TestRecorder_StopWithAFailingStoreStopsDrainingAtTheFirstFailureAndNamesTheLostFacts(t *testing.T) {
	store := newFakeStore()
	store.hold = make(chan struct{}) // never closed: only ctx frees a write
	r, buf := newRecorder(t, store)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.TxAlarmRaised("c0", time.Now())
	<-store.first
	for i := 0; i < 5; i++ {
		r.TxDisarmed("cat_lost", time.Now())
	}
	stopped := make(chan error, 1)
	go func() { stopped <- r.Stop() }()
	// Stop has sealed admission and cancelled the worker before the in-flight
	// write is released by hand (standing in for the store's timeout); every
	// write after it fails.
	waitFor(t, func() bool { return r.life.Context().Err() != nil }, "Stop did not cancel the worker")
	store.mu.Lock()
	for i := 2; i <= 10; i++ {
		store.failOn[i] = errors.New("store closed")
	}
	store.mu.Unlock()
	close(store.hold)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return with a failing store")
	}
	if n := len(store.snapshot()); n != 1 {
		t.Errorf("rows = %d, want 1 (the write in flight completed; the drain's first write failed)", n)
	}
	if store.callCount() != 2 {
		t.Errorf("store calls = %d, want 2 — the drain must stop at its first failure", store.callCount())
	}
	// One failed-at-shutdown for the fact that failed, the other four abandoned.
	if got := r.Drops(); got != 5 {
		t.Errorf("drops = %d, want 5 (1 failed + 4 abandoned)", got)
	}
	if n := len(buf.lines("station events:")); n != 5 {
		t.Errorf("warnings = %d, want one for each of the 5 lost facts", n)
	}
	if n := len(buf.lines(`"reason":"write failed at shutdown"`)); n != 1 {
		t.Errorf("failed-at-shutdown warnings = %d, want 1", n)
	}
	if n := len(buf.lines(`"reason":"store unavailable at shutdown"`)); n != 4 {
		t.Errorf("abandoned warnings = %d, want 4", n)
	}
}

func containsAll(have, want []string) bool {
	for _, w := range want {
		if !contains(have, w) {
			return false
		}
	}
	return true
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
