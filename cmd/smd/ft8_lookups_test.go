package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/lookup"
)

/*
   W-0028: the per-contact enrichment handoff (ft8_lookups.go). These pin its own
   rules with a scripted lookup; ft8qsolog_enrich_test.go drives the same rules
   through the real completed-QSO sink and the real enrichment pipeline.
*/

// scriptedLookups is a lookup function whose every invocation is held until
// the test answers it, by its order of arrival.
type scriptedLookups struct {
	mu    sync.Mutex
	calls []*scriptedCall
}

type scriptedCall struct {
	call   string
	answer chan lookup.Result
	ended  chan struct{} // closed when the invocation returned
}

func (s *scriptedLookups) fn(ctx context.Context, call string) lookup.Result {
	c := &scriptedCall{call: call, answer: make(chan lookup.Result, 1), ended: make(chan struct{})}
	s.mu.Lock()
	s.calls = append(s.calls, c)
	s.mu.Unlock()
	defer close(c.ended)
	select {
	case r := <-c.answer:
		return r
	case <-ctx.Done():
		return lookup.Result{}
	}
}

func (s *scriptedLookups) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// nth waits for the nth invocation (from 1) to have started.
func (s *scriptedLookups) nth(t *testing.T, n int) *scriptedCall {
	t.Helper()
	waitFor(t, fmt.Sprintf("lookup %d to start", n), func() bool { return s.count() >= n })
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[n-1]
}

func named(name string) lookup.Result {
	r := lookup.Result{StationSource: "scripted", CountrySource: lookup.SourceNone}
	r.Station.Name = name
	return r
}

// answered reports whether a contact's lookup has answered, without taking
// it. Test seam only.
func (l *ft8Lookups) answered(contact uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.contacts[contact]
	return ok && e.done
}

// answer answers one invocation and waits until the handoff has recorded it
// for contact (0 = just until the invocation returned).
func answer(t *testing.T, l *ft8Lookups, c *scriptedCall, contact uint64, r lookup.Result) {
	t.Helper()
	c.answer <- r
	<-c.ended
	if contact != 0 {
		waitFor(t, fmt.Sprintf("contact %d's lookup to be recorded", contact), func() bool { return l.answered(contact) })
	}
}

func scripted() (*ft8Lookups, *scriptedLookups) {
	s := &scriptedLookups{}
	return newFt8Lookups(s.fn, noopPanic), s
}

// started stops the handoff and returns how many lookups were ever started.
// Exact, where a count taken while workers may still be starting is not:
// stop waits for every worker, and each one registers its invocation before it
// can end.
func started(l *ft8Lookups, s *scriptedLookups) int {
	l.stop()
	return s.count()
}

// The rungs of one contact share its one lookup — even one that found nothing
// (no Internet): there is no retry.
func TestFt8Lookups_OneLookupPerContact(t *testing.T) {
	l, s := scripted()
	defer l.stop()

	l.partner(1, "ja1xyz")
	c := s.nth(t, 1)
	if c.call != "JA1XYZ" {
		t.Fatalf("looked up %q; want the normalised JA1XYZ", c.call)
	}
	l.partner(1, "JA1XYZ") // the next rung, while the lookup runs
	answer(t, l, c, 0, lookup.Result{CountrySource: lookup.SourceNone, StationSource: lookup.SourceNone})
	l.partner(1, " JA1XYZ ") // a later rung, after it found nothing
	if got := started(l, s); got != 1 {
		t.Fatalf("lookups for one contact = %d; want 1", got)
	}
}

// A later contact with the SAME station is a new contact: it goes through the
// lookup pipeline again and takes only its own result.
func TestFt8Lookups_TheSameStationAgainIsANewLookup(t *testing.T) {
	l, s := scripted()
	defer l.stop()

	l.partner(1, "JA1XYZ")
	answer(t, l, s.nth(t, 1), 1, named("first"))
	l.partner(2, "JA1XYZ")
	answer(t, l, s.nth(t, 2), 2, named("second"))

	if r, ok := l.take(2, "JA1XYZ"); !ok || r.Station.Name != "second" {
		t.Fatalf("contact 2 took %q (%v); want its own result \"second\"", r.Station.Name, ok)
	}
	if r, ok := l.take(1, "JA1XYZ"); !ok || r.Station.Name != "first" {
		t.Fatalf("the delayed completion of contact 1 took %q (%v); want its own result \"first\"", r.Station.Name, ok)
	}
}

// Contact A (abandoned, or simply earlier) answers late, while contact B with
// the same station is still waiting for its own lookup: B gets nothing.
func TestFt8Lookups_ALateResultOfAnEarlierContactDoesNotServeALaterOne(t *testing.T) {
	l, s := scripted()
	defer l.stop()

	l.partner(1, "JA1XYZ")
	a := s.nth(t, 1)
	l.partner(2, "JA1XYZ")
	s.nth(t, 2) // B's lookup is running, unanswered
	answer(t, l, a, 1, named("old"))

	if r, ok := l.take(2, "JA1XYZ"); ok {
		t.Fatalf("contact 2 took %q; want nothing: only contact 1's lookup had answered", r.Station.Name)
	}
}

// Taking ends the contact, ready or not: a lookup returning afterwards does not
// revive it, and a status published afterwards does not restart its lookup.
func TestFt8Lookups_TakeEndsTheContact(t *testing.T) {
	l, s := scripted()
	defer l.stop()

	l.partner(1, "JA1XYZ")
	c := s.nth(t, 1)
	if _, ok := l.take(1, "JA1XYZ"); ok {
		t.Fatal("took a result before the lookup answered")
	}
	answer(t, l, c, 0, named("late")) // the lookup returns after the take
	l.wg.Wait()                       // and its worker has finished
	if r, ok := l.take(1, "JA1XYZ"); ok {
		t.Fatalf("a taken contact gave %q on a second take; want nothing", r.Station.Name)
	}
	l.partner(1, "JA1XYZ") // the contact's status, published after its completion
	if got := started(l, s); got != 1 {
		t.Fatalf("lookups = %d; want 1: a taken contact's lookup must not restart", got)
	}
}

// No fallback: a mismatched station, a zero identity and an unknown identity
// all yield nothing, even though a result for that station is in hand.
func TestFt8Lookups_NoFallbackToAnotherContactsResult(t *testing.T) {
	l, s := scripted()
	defer l.stop()

	l.partner(1, "JA1XYZ")
	answer(t, l, s.nth(t, 1), 1, named("Taro"))
	if _, ok := l.take(0, "JA1XYZ"); ok {
		t.Fatal("a zero identity took a result")
	}
	if _, ok := l.take(99, "JA1XYZ"); ok {
		t.Fatal("an unknown identity took a result")
	}
	l.partner(0, "DL9UW")
	if r, ok := l.take(1, "DL9UW"); ok {
		t.Fatalf("contact 1 (JA1XYZ) gave %q to a completion with DL9UW", r.Station.Name)
	}
	if got := started(l, s); got != 1 {
		t.Fatalf("lookups = %d; want 1: a zero identity starts none", got)
	}
}

// The handoff remembers a bounded number of contacts: the oldest is forgotten
// (its completion then finds nothing), the newest kept.
func TestFt8Lookups_Bounded(t *testing.T) {
	l, s := scripted()
	defer l.stop()
	last := uint64(ft8LookupsKept + 1)
	for id := uint64(1); id <= last; id++ {
		call := fmt.Sprintf("K%dABC", id)
		l.partner(id, call)
		answer(t, l, s.nth(t, int(id)), 0, named(call))
	}
	waitFor(t, "the newest lookup to be recorded", func() bool { return l.answered(last) })
	if _, ok := l.take(1, "K1ABC"); ok {
		t.Fatal("the oldest contact is still remembered beyond the bound")
	}
	if r, ok := l.take(last, fmt.Sprintf("K%dABC", last)); !ok || r.Station.Name == "" {
		t.Fatal("the newest contact was not kept")
	}
	l.mu.Lock()
	n, m := len(l.contacts), len(l.order)
	l.mu.Unlock()
	if n != ft8LookupsKept || m != ft8LookupsKept {
		t.Fatalf("remembered %d contacts in %d order slots; want %d of each", n, m, ft8LookupsKept)
	}
}

// Stop cancels a running lookup and waits for it, so none outlives the
// enrichment cache; nothing starts afterwards.
func TestFt8Lookups_StopCancelsWaitsAndRefusesNew(t *testing.T) {
	l, s := scripted()
	l.partner(1, "JA1XYZ")
	c := s.nth(t, 1)

	stopped := make(chan struct{})
	go func() { l.stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not cancel and return")
	}
	select {
	case <-c.ended:
	default:
		t.Fatal("stop returned before the running lookup ended")
	}
	l.partner(2, "DL9UW")
	l.wg.Wait() // a lookup wrongly started after stop would have registered by now
	if got := s.count(); got != 1 {
		t.Fatalf("lookups started = %d; want none after stop", got)
	}
}

// Each lookup has the lookup deadline of its own, independent of the job's.
func TestFt8Lookups_ALookupIsBoundedByTheLookupTimeout(t *testing.T) {
	old := ft8LookupTimeout
	ft8LookupTimeout = 50 * time.Millisecond
	t.Cleanup(func() { ft8LookupTimeout = old })

	l, s := scripted()
	defer l.stop()
	l.partner(1, "JA1XYZ")
	c := s.nth(t, 1)
	select {
	case <-c.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup outlived its deadline")
	}
}

// A nil handoff (enrichment not initialised) is inert.
func TestFt8Lookups_NilIsInert(t *testing.T) {
	var l *ft8Lookups
	l.partner(1, "JA1XYZ")
	if _, ok := l.take(1, "JA1XYZ"); ok {
		t.Fatal("a nil handoff gave a result")
	}
	l.stop()
}
