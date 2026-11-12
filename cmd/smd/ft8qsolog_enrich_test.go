package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0028: logging a completed FT8/FT4 exchange never depends on enrichment
   (AGENTS.md "Enrichment never blocks logging"; operator ruling 2026-11-12:
   "If enrichment occurs with the boundaries of the ladder - great, otherwise log
   and move on"). These drive the real completed-QSO sink and the real enrichment
   pipeline against a real daemon archive with an enabled insert route, for an
   FT8 and an FT4 contact.

   Three properties, each with its own reversion (codex-093, codex-095):
     NW  completion never waits for a lookup: the contact and its upload row are
         stored while the provider is still held.
     ID  a lookup belongs to the contact it was started for: a completion uses
         only its own contact's result — not another station's, not an earlier
         contact's with the same station, and nothing without an identity.
     WC  the write has its own context: an expired job context cannot cancel it.
*/

// heldProvider is a callsign provider whose every lookup is held until the
// test answers it, by its order of arrival (or until its context ends).
type heldProvider struct {
	mu    sync.Mutex
	calls []*heldCall
}

type heldCall struct {
	call   string
	answer chan types.ContactedStation
	ended  chan struct{} // closed when the lookup returned
}

func (*heldProvider) Name() string                     { return "held" }
func (*heldProvider) Initialize(context.Context) error { return nil }

func (p *heldProvider) Lookup(call string) (types.ContactedStation, error) {
	return p.LookupWithContext(context.Background(), call)
}

func (p *heldProvider) LookupWithContext(ctx context.Context, call string) (types.ContactedStation, error) {
	c := &heldCall{call: call, answer: make(chan types.ContactedStation, 1), ended: make(chan struct{})}
	p.mu.Lock()
	p.calls = append(p.calls, c)
	p.mu.Unlock()
	defer close(c.ended)
	select {
	case s := <-c.answer:
		return s, nil
	case <-ctx.Done():
		return types.ContactedStation{}, ctx.Err()
	}
}

// nth waits for the nth lookup (from 1) to have reached the provider.
func (p *heldProvider) nth(t *testing.T, n int) *heldCall {
	t.Helper()
	waitFor(t, fmt.Sprintf("lookup %d to reach the provider", n), func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.calls) >= n
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[n-1]
}

// answerContact answers one held lookup with a station name and waits until
// the handoff has recorded the result for contact.
func answerContact(t *testing.T, d *daemon, c *heldCall, contact uint64, name string) {
	t.Helper()
	c.answer <- types.ContactedStation{Name: name}
	<-c.ended
	waitFor(t, fmt.Sprintf("contact %d's lookup to be recorded", contact), func() bool { return d.ft8Lookups.answered(contact) })
}

// newFt8LogDaemon starts a daemon whose default logbook has one enabled insert
// route (a stub forwarder that ticks too rarely to send during the test), and
// whose lookup chain is one held provider over the real enrichment cache
// (reference.db), so a cache miss reaches it.
func newFt8LogDaemon(t *testing.T) (*daemon, *heldProvider) {
	t.Helper()
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{{Name: "qrz", Type: stub.Type, Enabled: true,
			Credentials: stubCreds(t), ActionFilter: []string{"insert"}, TickIntervalSec: 3600, BatchSize: 1}}
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { stopGen(t, orch) })
	if d.refDB == nil {
		t.Fatal("the daemon has no enrichment cache database")
	}
	p := &heldProvider{}
	d.enrich = &lookup.Orchestrator{DB: d.refDB, LogDB: d.db, Logger: d.logger,
		Chain: []lookup.CallsignProvider{p}}
	return d, p
}

// ftMode is one profile the sink logs (ADR 0080): the name a completed exchange
// carries, and the ADIF pair it is stored under. FT4 is a submode of MFSK, so it
// takes a different path through the submit's validation than FT8 does.
type ftMode struct{ name, adifMode, adifSubmode string }

var ftModes = []ftMode{{"FT8", "FT8", ""}, {"FT4", "MFSK", "FT4"}}

// eachFtMode runs fn once per profile, each against its own daemon.
func eachFtMode(t *testing.T, fn func(t *testing.T, m ftMode)) {
	t.Helper()
	for _, m := range ftModes {
		t.Run(m.name, func(t *testing.T) { fn(t, m) })
	}
}

// complete hands the sink the completed exchange of contact with call, as
// the sequencer would, and waits for the earned logging job. The job must
// finish promptly: still running after the limit means it waited for
// something it must not.
func (m ftMode) complete(t *testing.T, d *daemon, contact uint64, call string) {
	t.Helper()
	d.ft8QsoLogger()(context.Background(), ft8.CompletedQso{Mode: m.name, LogbookID: 1, ContactID: contact,
		TheirCall: call, HasOurReport: true, OurReport: -10, HasTheirReport: true, TheirReport: -12,
		StartedAt: time.Now().UTC().Add(-time.Minute), DialFreqMHz: 14.074})
	done := make(chan struct{})
	go func() { d.qsoLogWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the completed contact was not logged promptly: the job waited for a lookup")
	}
}

// stored asserts the single stored QSO is call's, filed under m's ADIF pair,
// with one insert row queued for the enabled route, and returns its name.
func (m ftMode) stored(t *testing.T, d *daemon, call string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	qsos, err := d.db.FetchQsoSliceByLogbookIdWithContext(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(qsos) != 1 {
		t.Fatalf("QSOs stored = %d; want 1", len(qsos))
	}
	q := qsos[0]
	if q.Call != call || q.Mode != m.adifMode || q.Submode != m.adifSubmode {
		t.Fatalf("stored %q as MODE %q SUBMODE %q; want %q as %q / %q", q.Call, q.Mode, q.Submode, call, m.adifMode, m.adifSubmode)
	}
	ups, err := d.db.FetchUploadsByQsoIDWithContext(ctx, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 1 || ups[0].Action != "insert" {
		t.Fatalf("upload rows = %+v; want one insert row", ups)
	}
	return q.ContactedStation.Name
}

// NW: the provider is held for the whole test; the contact and its upload row
// are stored anyway, promptly. Before W-0028 the sink looked the call up first
// and so waited for the provider.
func TestFt8QsoLog_NW_CompletionDoesNotWaitForAHeldLookup(t *testing.T) {
	eachFtMode(t, func(t *testing.T, m ftMode) {
		d, p := newFt8LogDaemon(t)
		d.ft8Lookups.partner(1, "JA1XYZ") // the contact began
		p.nth(t, 1)                       // its lookup is with the provider, unanswered
		m.complete(t, d, 1, "JA1XYZ")
		if name := m.stored(t, d, "JA1XYZ"); name != "" {
			t.Fatalf("stored name = %q; want none (the lookup had not answered)", name)
		}
	})
}

// A lookup that answered during the ladder decorates its contact.
func TestFt8QsoLog_AReadyLookupIsUsed(t *testing.T) {
	eachFtMode(t, func(t *testing.T, m ftMode) {
		d, p := newFt8LogDaemon(t)
		d.ft8Lookups.partner(1, "JA1XYZ")
		answerContact(t, d, p.nth(t, 1), 1, "Taro")
		m.complete(t, d, 1, "JA1XYZ")
		if name := m.stored(t, d, "JA1XYZ"); name != "Taro" {
			t.Fatalf("stored name = %q; want Taro from the lookup made during the ladder", name)
		}
	})
}

// ID: a result for one station never decorates a contact with another.
func TestFt8QsoLog_ID_AnotherStationsLookupIsNotUsed(t *testing.T) {
	eachFtMode(t, func(t *testing.T, m ftMode) {
		d, p := newFt8LogDaemon(t)
		d.ft8Lookups.partner(1, "JA1XYZ")
		answerContact(t, d, p.nth(t, 1), 1, "Taro")
		d.ft8Lookups.partner(2, "DL9UW")
		p.nth(t, 2) // DL9UW's own lookup is unanswered
		m.complete(t, d, 2, "DL9UW")
		if name := m.stored(t, d, "DL9UW"); name != "" {
			t.Fatalf("stored name %q; want none (JA1XYZ's result is not DL9UW's)", name)
		}
	})
}

// ID: the same station, an earlier contact (abandoned: it never completes) and
// a later one, both lookups running. The earlier one answers late, while the
// later contact is still waiting for its own. The later contact is logged
// without it. A handoff keyed by callsign gave it the earlier contact's result.
func TestFt8QsoLog_ID_AnEarlierContactsLateLookupDoesNotServeALaterContactWithTheSameStation(t *testing.T) {
	eachFtMode(t, func(t *testing.T, m ftMode) {
		d, p := newFt8LogDaemon(t)
		d.ft8Lookups.partner(1, "JA1XYZ") // contact A
		a := p.nth(t, 1)
		d.ft8Lookups.partner(2, "JA1XYZ") // abandoned; contact B, the same station
		p.nth(t, 2)                       // B's own lookup is unanswered
		answerContact(t, d, a, 1, "Old")  // A's lookup answers late
		m.complete(t, d, 2, "JA1XYZ")
		if name := m.stored(t, d, "JA1XYZ"); name != "" {
			t.Fatalf("stored name %q; want none: only the earlier contact's lookup had answered", name)
		}
	})
}

// ID: a delayed completion of the earlier contact, arriving after the next
// contact with the same station began, takes its own result.
func TestFt8QsoLog_ID_ADelayedCompletionTakesItsOwnResult(t *testing.T) {
	eachFtMode(t, func(t *testing.T, m ftMode) {
		d, p := newFt8LogDaemon(t)
		d.ft8Lookups.partner(1, "JA1XYZ")
		a := p.nth(t, 1)
		d.ft8Lookups.partner(2, "JA1XYZ")
		p.nth(t, 2)
		answerContact(t, d, a, 1, "Own")
		m.complete(t, d, 1, "JA1XYZ") // the earlier contact's completion, late
		if name := m.stored(t, d, "JA1XYZ"); name != "Own" {
			t.Fatalf("stored name %q; want Own: the earlier contact's own result", name)
		}
	})
}

// ID: a completion without an identity, or with one the handoff does not know
// (forgotten, or never reported), is logged at once without details. There is
// no fallback to the station's result from another contact, and a missing
// entry never suppresses logging.
func TestFt8QsoLog_ID_NoIdentityOrAnUnknownOneLogsWithoutDetails(t *testing.T) {
	for _, contact := range []uint64{0, 99} {
		t.Run(fmt.Sprintf("contact %d", contact), func(t *testing.T) {
			eachFtMode(t, func(t *testing.T, m ftMode) {
				d, p := newFt8LogDaemon(t)
				d.ft8Lookups.partner(1, "JA1XYZ")
				answerContact(t, d, p.nth(t, 1), 1, "Taro")
				m.complete(t, d, contact, "JA1XYZ")
				if name := m.stored(t, d, "JA1XYZ"); name != "" {
					t.Fatalf("stored name %q; want none: contact 1's result is not contact %d's", name, contact)
				}
			})
		})
	}
}

// WC: the write has its own context. The job's context has already expired
// when the write starts; the contact is still stored.
func TestFt8QsoLog_WC_AnExpiredJobContextCannotCancelTheWrite(t *testing.T) {
	old := ft8QsoLogTimeout
	ft8QsoLogTimeout = time.Nanosecond
	t.Cleanup(func() { ft8QsoLogTimeout = old })

	eachFtMode(t, func(t *testing.T, m ftMode) {
		d, _ := newFt8LogDaemon(t)
		m.complete(t, d, 0, "JA1XYZ")
		m.stored(t, d, "JA1XYZ")
	})
}
