package archive

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0084 slice 2b: the active archive's summary is kept current off the QSO
// path. Notify is the post-commit dirty signal — non-blocking, coalescing; a
// worker recounts; the summary reads "stale" from a notification until a
// recount that no notification overtook succeeds.

// gatedSource counts per logbook from a mutable map and can hold a recount at the
// gate, so a test can notify while one is in progress.
type gatedSource struct {
	mu     sync.Mutex
	count  int64
	calls  int
	fail   bool
	gate   chan struct{} // nil: never blocks
	inside chan struct{} // signalled when a recount reaches the gate
}

func (g *gatedSource) FetchAllLogbooksWithContext(ctx context.Context) ([]types.Logbook, error) {
	g.mu.Lock()
	g.calls++
	gate, inside, fail := g.gate, g.inside, g.fail
	g.mu.Unlock()
	if inside != nil {
		inside <- struct{}{}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if fail {
		return nil, errors.New("read failed")
	}
	return []types.Logbook{{ID: 1, UUID: "lb-1", Name: "Default", Callsign: "7Q5MLV"}}, nil
}

func (g *gatedSource) FetchQsoCountByLogbookIdWithContext(context.Context, int64, string, bool) (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.count, nil
}

func (g *gatedSource) set(f func(*gatedSource)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f(g)
}

func runTracker(t *testing.T, a *ActiveSummary) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestActiveSummary_StartsCurrentWithTheOpeningCount(t *testing.T) {
	a := NewActiveSummary("arch-1", &gatedSource{}, []LogbookSummary{{Name: "Default", QsoCount: 5}}, nil)
	id, lbs, status := a.Snapshot()
	if id != "arch-1" || status != ContentsCurrent || len(lbs) != 1 || lbs[0].QsoCount != 5 {
		t.Fatalf("snapshot = %s %+v %s; want arch-1, 5 QSOs, current", id, lbs, status)
	}
}

func TestActiveSummary_AFailedOpeningBuildIsUnknownUntilARecount(t *testing.T) {
	src := &gatedSource{count: 2}
	a := NewActiveSummary("arch-1", src, nil, errors.New("boom"))
	if _, lbs, status := a.Snapshot(); status != ContentsUnknown || lbs == nil || len(lbs) != 0 {
		t.Fatalf("snapshot = %#v %s; want [] and unknown", lbs, status)
	}
	runTracker(t, a) // an unknown summary is recounted without waiting for a write
	waitFor(t, "the recount", func() bool { _, _, s := a.Snapshot(); return s == ContentsCurrent })
}

func TestActiveSummary_ANotificationRecountsAndReadsStaleUntilDone(t *testing.T) {
	src := &gatedSource{count: 1, gate: make(chan struct{}), inside: make(chan struct{}, 8)}
	a := NewActiveSummary("arch-1", src, []LogbookSummary{{Name: "Default", QsoCount: 1}}, nil)
	runTracker(t, a)

	src.set(func(g *gatedSource) { g.count = 2 })
	a.Notify()
	if _, _, status := a.Snapshot(); status != ContentsStale {
		t.Fatalf("status right after a notification = %s, want stale", status)
	}
	<-src.inside // the recount is running
	if _, _, status := a.Snapshot(); status != ContentsStale {
		t.Fatalf("status during the recount = %s, want stale", status)
	}
	src.gate <- struct{}{}
	waitFor(t, "current with 2 QSOs", func() bool {
		_, lbs, s := a.Snapshot()
		return s == ContentsCurrent && len(lbs) == 1 && lbs[0].QsoCount == 2
	})
}

// A notification that arrives while a recount runs must schedule another: the
// running recount may have read the count before the write that notified.
func TestActiveSummary_ANotificationDuringARecountSchedulesAnother(t *testing.T) {
	src := &gatedSource{count: 1, gate: make(chan struct{}), inside: make(chan struct{}, 8)}
	a := NewActiveSummary("arch-1", src, []LogbookSummary{{Name: "Default", QsoCount: 1}}, nil)
	runTracker(t, a)

	a.Notify()
	<-src.inside // recount 1 is inside, holding the old count
	src.set(func(g *gatedSource) { g.count = 3 })
	a.Notify() // the write that recount 1 may have missed
	src.gate <- struct{}{}
	// Recount 1 finished, but a notification overtook it: still stale.
	<-src.inside // recount 2 started
	if _, _, status := a.Snapshot(); status != ContentsStale {
		t.Fatalf("status after an overtaken recount = %s, want stale", status)
	}
	src.gate <- struct{}{}
	waitFor(t, "current with 3 QSOs", func() bool {
		_, lbs, s := a.Snapshot()
		return s == ContentsCurrent && lbs[0].QsoCount == 3
	})
}

// Coalescing: many notifications while a recount is held become ONE more recount.
func TestActiveSummary_NotificationsCoalesce(t *testing.T) {
	src := &gatedSource{count: 1, gate: make(chan struct{}), inside: make(chan struct{}, 64)}
	a := NewActiveSummary("arch-1", src, []LogbookSummary{{Name: "Default", QsoCount: 1}}, nil)
	runTracker(t, a)
	a.Notify()
	<-src.inside
	for i := 0; i < 50; i++ {
		a.Notify()
	}
	src.gate <- struct{}{}
	<-src.inside // the one follow-up
	src.gate <- struct{}{}
	waitFor(t, "current", func() bool { _, _, s := a.Snapshot(); return s == ContentsCurrent })
	src.mu.Lock()
	calls := src.calls
	src.mu.Unlock()
	if calls != 2 {
		t.Fatalf("%d recounts for 51 notifications; want 2 (coalesced)", calls)
	}
}

// The QSO path's guarantee: Notify never waits for a recount, however stuck.
func TestActiveSummary_NotifyNeverBlocksOnAStuckRecount(t *testing.T) {
	src := &gatedSource{count: 1, gate: make(chan struct{}), inside: make(chan struct{}, 1)}
	a := NewActiveSummary("arch-1", src, []LogbookSummary{{Name: "Default", QsoCount: 1}}, nil)
	runTracker(t, a)
	a.Notify()
	<-src.inside // the recount is stuck at the gate and stays there
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			a.Notify()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Notify blocked behind a stuck recount")
	}
}

func TestActiveSummary_AFailedRecountStaysStaleUntilOneSucceeds(t *testing.T) {
	src := &gatedSource{count: 1, fail: true}
	a := NewActiveSummary("arch-1", src, []LogbookSummary{{Name: "Default", QsoCount: 1}}, nil)
	runTracker(t, a)
	a.Notify()
	waitFor(t, "the failed recount", func() bool { src.mu.Lock(); defer src.mu.Unlock(); return src.calls >= 1 })
	if _, lbs, status := a.Snapshot(); status != ContentsStale || lbs[0].QsoCount != 1 {
		t.Fatalf("after a failed recount: %+v %s; want the last counts, stale", lbs, status)
	}
	src.set(func(g *gatedSource) { g.fail = false; g.count = 4 })
	a.Notify()
	waitFor(t, "current with 4", func() bool {
		_, lbs, s := a.Snapshot()
		return s == ContentsCurrent && lbs[0].QsoCount == 4
	})
}

// ---- GET /v1/qso-archives contents (Manager.List) ----

type fixedActive struct {
	id       string
	logbooks []LogbookSummary
	status   string
}

func (f fixedActive) Snapshot() (string, []LogbookSummary, string) {
	return f.id, f.logbooks, f.status
}

func listByID(m *Manager) map[string]types.QsoArchiveView {
	out := map[string]types.QsoArchiveView{}
	for _, v := range m.List() {
		out[v.ID] = v
	}
	return out
}

// Each archive's contents: the active one from the live tracker, every other one
// from the sidecar judged against its file now — current, stale when the file
// changed since, unknown (with []) when there is no summary. Logbooks is never nil.
func TestList_ContentsOfEachArchive(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	ctx := context.Background()
	mk := func(key, label string) CreateResult {
		res, err := m.Create(ctx, CreateRequest{RequestKey: key, Label: label, LogbookName: label, LogbookCallsign: "7Q5MLV"})
		if err != nil {
			t.Fatalf("create %s: %v", label, err)
		}
		return res
	}
	active, fresh, changed, unknown := mk("a", "Home"), mk("b", "Drill"), mk("c", "Contest"), mk("d", "Old")
	if _, err := cfgSvc.Update(func(c *config.Config) error { c.ActiveQsoArchiveID = active.Entry.ID; return nil }); err != nil {
		t.Fatal(err)
	}
	// "changed": the file was replaced after its summary was taken.
	repl := changed.Path + ".repl"
	raw, _ := os.ReadFile(changed.Path)
	if err := os.WriteFile(repl, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repl, changed.Path); err != nil {
		t.Fatal(err)
	}
	// "unknown": no summary for it at all.
	sidecar := SummariesPath(cfgSvc.Snapshot())
	all, _ := ReadSummaries(sidecar)
	delete(all, unknown.Entry.ID)
	if err := WriteSummaries(sidecar, all); err != nil {
		t.Fatal(err)
	}
	m.SetActiveSummary(fixedActive{
		id:       active.Entry.ID,
		logbooks: []LogbookSummary{{Name: "Home", Callsign: "7Q5MLV", QsoCount: 7468}},
		status:   ContentsStale,
	})

	got := listByID(m)
	if v := got[active.Entry.ID]; v.ContentsStatus != ContentsStale || len(v.Logbooks) != 1 || v.Logbooks[0].QsoCount != 7468 {
		t.Fatalf("active = %+v; want the live tracker's 7468 QSOs, stale", v)
	}
	if v := got[fresh.Entry.ID]; v.ContentsStatus != ContentsCurrent || len(v.Logbooks) != 1 || v.Logbooks[0].Name != "Drill" || v.Logbooks[0].QsoCount != 0 {
		t.Fatalf("untouched inactive = %+v; want Drill, 0 QSOs, current", v)
	}
	if v := got[changed.Entry.ID]; v.ContentsStatus != ContentsStale || len(v.Logbooks) != 1 || v.Logbooks[0].Name != "Contest" {
		t.Fatalf("changed inactive = %+v; want its last-known Contest logbook, stale", v)
	}
	if v := got[unknown.Entry.ID]; v.ContentsStatus != ContentsUnknown || v.Logbooks == nil || len(v.Logbooks) != 0 {
		t.Fatalf("no-summary inactive = %#v; want [] and unknown", v)
	}
}

// A tracker for another archive (a stale wiring, a switch in flight) must never
// label this one: the active archive then reads from the sidecar like any other.
func TestList_ATrackerForAnotherArchiveIsIgnored(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	res, err := m.Create(context.Background(), CreateRequest{RequestKey: "a", Label: "Home", LogbookName: "Home", LogbookCallsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfgSvc.Update(func(c *config.Config) error { c.ActiveQsoArchiveID = res.Entry.ID; return nil }); err != nil {
		t.Fatal(err)
	}
	m.SetActiveSummary(fixedActive{id: "someone-else", logbooks: []LogbookSummary{{Name: "Wrong", QsoCount: 99}}, status: ContentsCurrent})
	v := listByID(m)[res.Entry.ID]
	if len(v.Logbooks) != 1 || v.Logbooks[0].Name != "Home" || v.ContentsStatus != ContentsCurrent {
		t.Fatalf("active with a foreign tracker = %+v; want its own sidecar summary", v)
	}
}

// POST /v1/qso-archives answers with the archive as the list shows it — its one
// logbook, current — never a null logbooks.
func TestCreateArchive_AnswersWithTheNewArchiveContents(t *testing.T) {
	m, _, _ := testManager(t)
	got, err := m.CreateArchive(context.Background(), types.QsoArchiveCreateRequest{
		RequestKey: "k", Label: "Drill", LogbookName: "Drill", LogbookCallsign: "7Q5MLV",
	})
	if err != nil {
		t.Fatal(err)
	}
	v := got.Archive
	if v.ContentsStatus != ContentsCurrent || len(v.Logbooks) != 1 || v.Logbooks[0].Name != "Drill" {
		t.Fatalf("created archive view = %+v; want its Drill logbook, current", v)
	}
}
