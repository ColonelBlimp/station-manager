package main

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0028: logging a completed FT8/FT4 exchange never depends on enrichment
   (AGENTS.md "Enrichment never blocks logging"; operator ruling 2026-11-12).
   These drive the real completed-QSO sink against the daemon's real archive.
*/

// hangingProvider is a callsign provider that never answers: it returns only
// when its context ends, as a hung upstream bounded by a deadline would.
type hangingProvider struct{ entered chan struct{} }

func (hangingProvider) Name() string                     { return "hanging" }
func (hangingProvider) Initialize(context.Context) error { return nil }

func (p hangingProvider) Lookup(call string) (types.ContactedStation, error) {
	return p.LookupWithContext(context.Background(), call)
}

func (p hangingProvider) LookupWithContext(ctx context.Context, _ string) (types.ContactedStation, error) {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return types.ContactedStation{}, ctx.Err()
}

// withHangingLookup gives the daemon a lookup chain of one hanging provider
// over its real enrichment cache (reference.db), so a cache miss reaches it.
func withHangingLookup(t *testing.T, d *daemon) hangingProvider {
	t.Helper()
	if d.refDB == nil {
		t.Fatal("the daemon has no enrichment cache database")
	}
	p := hangingProvider{entered: make(chan struct{}, 1)}
	d.enrich = &lookup.Orchestrator{DB: d.refDB, LogDB: d.db, Logger: d.logger,
		Chain: []lookup.CallsignProvider{p}}
	return p
}

// The write has its own context: a lookup that uses up the job's whole time
// limit must not leave the write with an expired one. Before W-0028 the lookup
// and the write shared one deadline, so the contact was not stored.
func TestFt8QsoLog_ALookupThatUsesTheWholeLimitStillLogsTheContact(t *testing.T) {
	short := 300 * time.Millisecond
	old := ft8QsoLogTimeout
	ft8QsoLogTimeout = short
	t.Cleanup(func() { ft8QsoLogTimeout = old })

	f := newChangeFixture(t)
	d := f.mustStart(t)
	defer f.stop(t, d)
	p := withHangingLookup(t, d)

	start := time.Now()
	d.ft8QsoLogger()(context.Background(), ft8.CompletedQso{Mode: "FT8", LogbookID: f.a, TheirCall: "JA1XYZ",
		HasOurReport: true, OurReport: -10, HasTheirReport: true, TheirReport: -12,
		StartedAt: time.Now().UTC().Add(-time.Minute), DialFreqMHz: 14.074})
	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup never ran; the test would not exercise a hanging provider")
	}
	d.qsoLogWG.Wait()
	if waited := time.Since(start); waited < short {
		t.Fatalf("the job ended after %v, before the lookup's limit %v; the hang was not exercised", waited, short)
	}
	if n := f.qsosIn(t, f.a); n != 1 {
		t.Fatalf("QSOs stored = %d; want 1: a lookup that used the whole limit cost the contact", n)
	}
}
