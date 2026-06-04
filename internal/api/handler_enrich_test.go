package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ---- stubs ----

type stubCountryProvider struct {
	name   string
	result types.Country
	err    error
	calls  int
}

func (s *stubCountryProvider) Name() string                       { return s.name }
func (s *stubCountryProvider) Initialize(_ context.Context) error { return nil }
func (s *stubCountryProvider) Lookup(_ string) (types.Country, error) {
	s.calls++
	return s.result, s.err
}
func (s *stubCountryProvider) LookupWithContext(_ context.Context, _ string) (types.Country, error) {
	s.calls++
	return s.result, s.err
}

type stubCallsignProvider struct {
	name   string
	result types.ContactedStation
	err    error
	calls  int
}

func (s *stubCallsignProvider) Name() string                       { return s.name }
func (s *stubCallsignProvider) Initialize(_ context.Context) error { return nil }
func (s *stubCallsignProvider) Lookup(_ string) (types.ContactedStation, error) {
	s.calls++
	return s.result, s.err
}
func (s *stubCallsignProvider) LookupWithContext(_ context.Context, _ string) (types.ContactedStation, error) {
	s.calls++
	return s.result, s.err
}

type syncRefresher struct{}

func (r *syncRefresher) Schedule(fn func(ctx context.Context)) { fn(context.Background()) }

// testServerWithEnrichment wraps testServer and replaces the nil
// orchestrator with a real one wired against the supplied provider
// stubs. The DB is the same in-memory sqlite the rest of the test
// server uses, so the cache read/write paths exercise real storage.
func testServerWithEnrichment(t *testing.T, country lookup.CountryProvider, chain []lookup.CallsignProvider) *Server {
	t.Helper()
	srv := testServer(t)
	srv.enrich = &lookup.Orchestrator{
		DB:         srv.db,
		Country:    country,
		Chain:      chain,
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}
	return srv
}

func enrichRequest(call string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/enrich/callsign?call="+call, nil)
	return req
}

// ---- happy path ----

func TestEnrich_HappyPath_ReturnsMergedResult(t *testing.T) {
	hamnut := &stubCountryProvider{
		name: "hamnut",
		result: types.Country{
			Name:    "England",
			Prefix:  "M",
			CQZone:  "14",
			ITUZone: "27",
		},
	}
	qrz := &stubCallsignProvider{
		name: "qrz",
		result: types.ContactedStation{
			Call:       "M0CMC",
			Name:       "Marc Veary",
			QTH:        "Birmingham",
			Gridsquare: "IO92",
			// QRZ-bug values per ADR 0017 — orchestrator must filter
			// + replace with hamnut truth before the response leaves.
			Country: "Malawi",
			CQZ:     "37",
			ITUZ:    "53",
		},
	}
	srv := testServerWithEnrichment(t, hamnut, []lookup.CallsignProvider{qrz})

	w := httptest.NewRecorder()
	srv.handleEnrichCallsign(w, enrichRequest("M0CMC"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var got lookup.Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Callsign != "M0CMC" {
		t.Errorf("Callsign = %q, want M0CMC", got.Callsign)
	}
	if got.Country.Name != "England" {
		t.Errorf("Country.Name = %q, want England", got.Country.Name)
	}
	if got.Station.Country != "England" {
		t.Errorf("Station.Country = %q, want England (filter+merge must apply at handler boundary)", got.Station.Country)
	}
	if got.Station.CQZ != "14" {
		t.Errorf("Station.CQZ = %q, want \"14\" (not QRZ's bogus 37)", got.Station.CQZ)
	}
	if got.CountrySource != lookup.SourceHamnut {
		t.Errorf("CountrySource = %q, want %q", got.CountrySource, lookup.SourceHamnut)
	}
	if got.StationSource != "qrz" {
		t.Errorf("StationSource = %q, want qrz", got.StationSource)
	}
}

// ---- always-200 contract per ADR 0017 #12 ----

func TestEnrich_AllProvidersDown_StillReturns200WithEmpty(t *testing.T) {
	srv := testServerWithEnrichment(t,
		&stubCountryProvider{name: "hamnut", err: errSyntheticTransport()},
		[]lookup.CallsignProvider{
			&stubCallsignProvider{name: "qrz", err: errSyntheticTransport()},
		},
	)

	w := httptest.NewRecorder()
	srv.handleEnrichCallsign(w, enrichRequest("M0CMC"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (always-200 per ADR 0017 #12)", w.Code)
	}

	var got lookup.Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.CountrySource != lookup.SourceNone || got.StationSource != lookup.SourceNone {
		t.Errorf("sources = %q/%q, want none/none", got.CountrySource, got.StationSource)
	}
}

// ---- input validation ----

func TestEnrich_MissingCall_Returns400(t *testing.T) {
	srv := testServerWithEnrichment(t, nil, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/enrich/callsign", nil)
	srv.handleEnrichCallsign(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestEnrich_InvalidCall_Returns400(t *testing.T) {
	srv := testServerWithEnrichment(t, nil, nil)

	cases := []string{
		"AB",      // too short
		"NODIGIT", // no digit
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.handleEnrichCallsign(w, enrichRequest(c))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for %q", w.Code, c)
			}
		})
	}
}

// ---- nil orchestrator (lookup pipeline disabled) ----

func TestEnrich_NilOrchestrator_Returns200WithEmpty(t *testing.T) {
	// Default testServer constructs Server with nil orchestrator —
	// matches the daemon shape when no providers are enabled in
	// config. Handler must not 500 on this path; it returns the
	// uniform empty-result shape so the SPA's response handler
	// doesn't need a special-case branch.
	srv := testServer(t)

	w := httptest.NewRecorder()
	srv.handleEnrichCallsign(w, enrichRequest("M0CMC"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (nil orchestrator → empty result)", w.Code)
	}

	var got lookup.Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.CountrySource != lookup.SourceNone || got.StationSource != lookup.SourceNone {
		t.Errorf("sources = %q/%q, want none/none", got.CountrySource, got.StationSource)
	}
	// M3 fix (review 2026-06-04): the validated callsign is echoed back even
	// with a nil orchestrator (was "" before — validation ran only after the
	// nil-branch early return).
	if got.Callsign != "M0CMC" {
		t.Errorf("Callsign = %q, want M0CMC (echoed even with nil orchestrator)", got.Callsign)
	}
}

// TestEnrich_NilOrchestrator_InvalidCallStill400 pins the other half of the M3
// fix: malformed input is validated BEFORE the nil-orchestrator branch, so a
// missing/invalid call still 400s rather than getting a 200 empty result.
func TestEnrich_NilOrchestrator_InvalidCallStill400(t *testing.T) {
	srv := testServer(t) // nil orchestrator
	for _, c := range []string{"", "X"} {
		w := httptest.NewRecorder()
		srv.handleEnrichCallsign(w, enrichRequest(c))
		if w.Code != http.StatusBadRequest {
			t.Errorf("call=%q: status = %d, want 400 (validate before the nil-orchestrator branch)", c, w.Code)
		}
	}
}

// ---- ?refresh=true: cache-bypass routing ----

// TestEnrich_RefreshTrue_BypassesFreshCache — pre-seed the country
// table with values that differ from what the upstream stub returns.
// A regular Enrich call would short-circuit on the fresh cache hit
// and never call hamnut; ?refresh=true must invert that and hit
// upstream regardless. Asserting on hamnut.calls is the cleanest
// behavioural check at the handler boundary — the orchestrator-level
// tests cover the writeback semantics.
func TestEnrich_RefreshTrue_BypassesFreshCache(t *testing.T) {
	hamnut := &stubCountryProvider{
		name: "hamnut",
		result: types.Country{
			Name:   "England",
			Prefix: "M",
		},
	}
	srv := testServerWithEnrichment(t, hamnut, nil)

	// Pre-seed a fresh cached row with a different name. The cache
	// fetch on regular Enrich would return this and skip hamnut.
	if err := srv.db.UpsertCountry(types.Country{
		Name:   "OldName",
		Prefix: "M",
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/enrich/callsign?call=M0CMC&refresh=true", nil)
	w := httptest.NewRecorder()
	srv.handleEnrichCallsign(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if hamnut.calls != 1 {
		t.Errorf("hamnut.calls = %d, want 1 (refresh=true must bypass the fresh cache)", hamnut.calls)
	}

	var got lookup.Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Country.Name != "England" {
		t.Errorf("Country.Name = %q, want England (upstream value, not cached OldName)", got.Country.Name)
	}
	if got.CountrySource != lookup.SourceHamnut {
		t.Errorf("CountrySource = %q, want %q (force-refresh treats success as cold-miss)",
			got.CountrySource, lookup.SourceHamnut)
	}
}

// TestEnrich_RefreshOmitted_UsesCacheAsBefore — regression guard
// for the default path: omitting refresh entirely must keep the
// pre-existing cache-aware behaviour untouched.
func TestEnrich_RefreshOmitted_UsesCacheAsBefore(t *testing.T) {
	hamnut := &stubCountryProvider{
		name:   "hamnut",
		result: types.Country{Name: "England", Prefix: "M"},
	}
	srv := testServerWithEnrichment(t, hamnut, nil)
	if err := srv.db.UpsertCountry(types.Country{Name: "Cached", Prefix: "M"}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	w := httptest.NewRecorder()
	srv.handleEnrichCallsign(w, enrichRequest("M0CMC"))

	if hamnut.calls != 0 {
		t.Errorf("hamnut.calls = %d on regular Enrich with fresh cache; should be zero", hamnut.calls)
	}
	var got lookup.Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Country.Name != "Cached" {
		t.Errorf("Country.Name = %q, want Cached (cache-aware path)", got.Country.Name)
	}
}

// TestEnrich_RefreshNonTrueValues_TreatedAsDefault — operator types
// "1" / "yes" / "TRUE" in some other client; the contract is "true"
// only. Anything else routes through the default cache-aware path.
// Pinning this as a behavioural promise keeps the contract obvious.
func TestEnrich_RefreshNonTrueValues_TreatedAsDefault(t *testing.T) {
	cases := []string{"1", "TRUE", "yes", "false", ""}
	for _, v := range cases {
		t.Run("refresh="+v, func(t *testing.T) {
			hamnut := &stubCountryProvider{
				name:   "hamnut",
				result: types.Country{Name: "England", Prefix: "M"},
			}
			srv := testServerWithEnrichment(t, hamnut, nil)
			if err := srv.db.UpsertCountry(types.Country{Name: "Cached", Prefix: "M"}); err != nil {
				t.Fatalf("seed cache: %v", err)
			}
			url := "/v1/enrich/callsign?call=M0CMC"
			if v != "" {
				url += "&refresh=" + v
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			srv.handleEnrichCallsign(w, req)
			if hamnut.calls != 0 {
				t.Errorf("refresh=%q: hamnut.calls = %d, want 0 (only literal \"true\" forces refresh)",
					v, hamnut.calls)
			}
		})
	}
}

// ---- helpers ----

type syntheticTransportError struct{}

func (e *syntheticTransportError) Error() string { return "synthetic transport failure" }

func errSyntheticTransport() error { return &syntheticTransportError{} }
