package main

import (
	"context"
	"strings"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/safego"
)

// ft8LookupsKept is how many contacts the handoff remembers: the live one,
// plus recent ones whose completion may still be on its way (a Call-CQ run
// moves to the next answerer as the last one completes) or that were abandoned
// and will never complete. It bounds MEMORY only. It is not a time limit, and
// it is not a bound on running lookups: each of those ends by its own deadline
// (ft8LookupTimeout) or at stop, whether or not its contact is still
// remembered.
const ft8LookupsKept = 16

// ft8Lookups is the per-contact enrichment handoff for FT8/FT4 (W-0028).
//
// The sequencer reports each contact's identity and partner as soon as they are
// known (ft8.SetPartnerSink). One lookup is started for that contact then,
// through the ordinary enrichment pipeline — which applies its own cache and
// freshness rules — and runs alongside the ladder. The completed-QSO sink takes
// the contact's result only if it has already arrived. Completion never waits:
// a contact whose lookup has not answered is logged without its details, which
// a later edit can add (operator ruling 2026-11-12).
//
// Everything is keyed by the contact's identity (ft8.CompletedQso.ContactID),
// never by callsign alone:
//
//   - the rungs of one contact share its one lookup; there is no retry;
//   - a new contact — a different station, a replacement or automatic next
//     partner, or the same station worked again — has a new identity and gets a
//     new lookup through the pipeline;
//   - a result belongs to the contact it was started for. A late answer for an
//     earlier or abandoned contact can never serve another one, even with the
//     same station;
//   - a contact ends when its completion takes it, ready or not. It stays
//     remembered as taken, so a status published afterwards cannot restart its
//     lookup and a lookup returning afterwards cannot revive it;
//   - an abandoned contact is never taken. Nothing else can reach its entry,
//     and it is forgotten once ft8LookupsKept newer contacts have begun. Its
//     lookup, if still running, finishes by itself and only warms the
//     enrichment cache, as every lookup does.
//
// Identities come from one sequencer, so one ft8Lookups serves one daemon
// generation: a restarted daemon builds a new one with a new sequencer.
type ft8Lookups struct {
	lookup  func(ctx context.Context, call string) lookup.Result
	onPanic safego.PanicHandler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	stopped  bool
	contacts map[uint64]*ft8Lookup
	order    []uint64 // identities in the order their contacts began; always the keys of contacts
}

type ft8Lookup struct {
	call  string // the station this contact's lookup was started for
	done  bool   // the lookup has answered
	taken bool   // the contact's completion has taken it: the contact is over
	res   lookup.Result
}

func newFt8Lookups(fn func(ctx context.Context, call string) lookup.Result, onPanic safego.PanicHandler) *ft8Lookups {
	ctx, cancel := context.WithCancel(context.Background())
	return &ft8Lookups{lookup: fn, onPanic: onPanic, ctx: ctx, cancel: cancel, contacts: map[uint64]*ft8Lookup{}}
}

func ft8LookupKey(call string) string { return strings.ToUpper(strings.TrimSpace(call)) }

// partner starts the one lookup of a contact, the first time the contact is
// reported; every later report of the same contact is a no-op, whatever state
// the lookup or the contact is in. Called under the FT8 sequencer's lock: it
// only schedules.
func (l *ft8Lookups) partner(contact uint64, call string) {
	if l == nil || contact == 0 {
		return
	}
	key := ft8LookupKey(call)
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopped {
		return
	}
	if _, known := l.contacts[contact]; known {
		return
	}
	e := &ft8Lookup{call: key}
	l.contacts[contact] = e
	l.order = append(l.order, contact)
	for len(l.order) > ft8LookupsKept {
		delete(l.contacts, l.order[0])
		l.order = l.order[1:]
	}
	safego.GoTracked(l.ctx, "ft8.lookup", l.onPanic, func() {
		ctx, cancel := context.WithTimeout(l.ctx, ft8LookupTimeout)
		defer cancel()
		res := l.lookup(ctx, key)
		// The answer goes to this contact's own entry and nowhere else. If the
		// contact was taken or forgotten meanwhile, nothing can read it.
		l.mu.Lock()
		e.done, e.res = true, res
		l.mu.Unlock()
	}, false /* one-shot */, &l.wg)
}

// take ends a contact at its completion and returns its lookup result if that
// has already arrived. It never waits. It returns nothing for an identity it
// does not know (zero, forgotten, or never reported), for a contact already
// taken, and when call is not the station the lookup was started for: there
// is no fallback to another contact's result.
func (l *ft8Lookups) take(contact uint64, call string) (lookup.Result, bool) {
	if l == nil || contact == 0 {
		return lookup.Result{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.contacts[contact]
	if !ok || e.taken {
		return lookup.Result{}, false
	}
	e.taken = true
	if !e.done || e.call != ft8LookupKey(call) {
		return lookup.Result{}, false
	}
	return e.res, true
}

// stop refuses new lookups, cancels the running ones and waits for them, so
// none outlives the enrichment cache it writes to.
func (l *ft8Lookups) stop() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.stopped = true
	l.mu.Unlock()
	l.cancel()
	l.wg.Wait()
}
