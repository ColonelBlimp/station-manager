package lookup_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestAcceptance_PrioritisedCallsignLookupFillsOnlyBlanks(t *testing.T) {
	db := newTestSqlite(t)
	preferred := &stubCallsignProvider{
		name: "preferred",
		result: types.ContactedStation{
			Call: "7Q8AC", Name: "Preferred Name", QTH: "Preferred QTH",
			Country: "provider country must be filtered",
		},
	}
	fallback := &stubCallsignProvider{
		name: "fallback",
		result: types.ContactedStation{
			Call: "7Q8AC", Name: "Fallback Name", QTH: "Fallback QTH",
			Gridsquare: "KH67", Address: "Fallback Address", Country: "also filtered",
		},
	}
	unneeded := &stubCallsignProvider{
		name:   "unneeded",
		result: types.ContactedStation{Call: "7Q8AC", Web: "https://unneeded.example"},
	}
	o := &lookup.Orchestrator{
		DB:              db,
		Chain:           []lookup.CallsignProvider{preferred, fallback, unneeded},
		ContinueIfBlank: []string{"name", "gridsquare"},
		StationTTL:      time.Hour,
		Refresher:       &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "7Q8AC")
	if got.StationSource != "preferred" {
		t.Fatalf("StationSource = %q, want highest-priority contributor", got.StationSource)
	}
	if got.Station.Name != "Preferred Name" || got.Station.QTH != "Preferred QTH" {
		t.Fatalf("fallback overwrote preferred data: %+v", got.Station)
	}
	if got.Station.Gridsquare != "KH67" || got.Station.Address != "Fallback Address" {
		t.Fatalf("fallback did not opportunistically fill blanks: %+v", got.Station)
	}
	if got.Station.Country != "" {
		t.Fatalf("callsign-provider country entered the result: %+v", got.Station)
	}
	if preferred.calls != 1 || fallback.calls != 1 || unnecessaryCalls(unneeded) != 0 {
		t.Fatalf("provider calls = preferred:%d fallback:%d unneeded:%d, want 1/1/0",
			preferred.calls, fallback.calls, unneeded.calls)
	}

	// An incompletely-known field outside the completion policy (Web) does not
	// bypass a fresh cache hit and repeatedly call the upstream chain.
	again := o.Enrich(context.Background(), "7Q8AC")
	if again.StationSource != lookup.SourceContactedTable {
		t.Fatalf("fresh second lookup source = %q, want cache", again.StationSource)
	}
	if preferred.calls != 1 || fallback.calls != 1 || unneeded.calls != 0 {
		t.Fatalf("fresh cache hit made another provider call: %d/%d/%d",
			preferred.calls, fallback.calls, unneeded.calls)
	}
}

func TestAcceptance_CompletionIsEvaluatedAfterProviderNormalisation(t *testing.T) {
	preferred := &stubCallsignProvider{
		name:   "preferred",
		result: types.ContactedStation{Call: "7Q7CT", Name: "Known", Gridsquare: "AA00"},
	}
	fallback := &stubCallsignProvider{
		name:   "fallback",
		result: types.ContactedStation{Call: "7Q7CT", Gridsquare: "KH67"},
	}
	o := &lookup.Orchestrator{
		DB:              newTestSqlite(t),
		Chain:           []lookup.CallsignProvider{preferred, fallback},
		ContinueIfBlank: []string{"name", "gridsquare"},
		StationTTL:      time.Hour,
		Refresher:       &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "7Q7CT")
	if got.Station.Gridsquare != "KH67" || fallback.calls != 1 {
		t.Fatalf("placeholder grid stopped fallback: station=%+v fallback_calls=%d",
			got.Station, fallback.calls)
	}
}

func TestAcceptance_CompletePreferredResultSkipsEveryFallback(t *testing.T) {
	preferred := &stubCallsignProvider{
		name:   "preferred",
		result: types.ContactedStation{Name: "Known", Gridsquare: "KH67"},
	}
	fallback := &stubCallsignProvider{
		name:   "fallback",
		result: types.ContactedStation{Name: "Must not run", Address: "Must not run"},
	}
	o := &lookup.Orchestrator{
		DB:              newTestSqlite(t),
		Chain:           []lookup.CallsignProvider{preferred, fallback},
		ContinueIfBlank: []string{"name", "gridsquare"},
		StationTTL:      time.Hour,
		Refresher:       &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "7Q7EB")
	if got.Station.Name != "Known" || got.Station.Gridsquare != "KH67" {
		t.Fatalf("station = %+v", got.Station)
	}
	if preferred.calls != 1 || fallback.calls != 0 {
		t.Fatalf("provider calls = preferred:%d fallback:%d, want 1/0", preferred.calls, fallback.calls)
	}
}

func TestAcceptance_ExhaustedPartialResultSurvivesErrorAndIsCached(t *testing.T) {
	partial := &stubCallsignProvider{
		name:   "partial",
		result: types.ContactedStation{Name: "Known", Address: "Known address"},
	}
	failing := &stubCallsignProvider{name: "failing", err: errors.New("temporary failure")}
	empty := &stubCallsignProvider{name: "empty"}
	o := &lookup.Orchestrator{
		DB:              newTestSqlite(t),
		Chain:           []lookup.CallsignProvider{partial, failing, empty},
		ContinueIfBlank: []string{"name", "gridsquare"},
		StationTTL:      time.Hour,
		Refresher:       &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "7Q8AC")
	if got.StationSource != "partial" || got.Station.Name != "Known" ||
		got.Station.Address != "Known address" || got.Station.Gridsquare != "" {
		t.Fatalf("exhausted partial result = %+v", got)
	}
	if partial.calls != 1 || failing.calls != 1 || empty.calls != 1 {
		t.Fatalf("first pass calls = %d/%d/%d, want 1/1/1", partial.calls, failing.calls, empty.calls)
	}

	again := o.Enrich(context.Background(), "7Q8AC")
	if again.StationSource != lookup.SourceContactedTable || again.Station.Name != "Known" {
		t.Fatalf("fresh cached partial = %+v", again)
	}
	if partial.calls != 1 || failing.calls != 1 || empty.calls != 1 {
		t.Fatalf("fresh cache hit retried exhausted chain: %d/%d/%d", partial.calls, failing.calls, empty.calls)
	}
}

func unnecessaryCalls(p *stubCallsignProvider) int { return p.calls }
