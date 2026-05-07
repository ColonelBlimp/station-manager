package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/lookup/hamnut"
	lookupqrz "github.com/ColonelBlimp/station-manager/internal/lookup/qrz"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// E2E tests that wire REAL hamnut.Service + real lookupqrz.Service
// against httptest upstreams, with a real *sqlite.Service and the
// real Orchestrator. The only stubs are at the network boundary
// (httptest) and the AsyncRefresher (synchronous so tests pin
// outcomes deterministically). Closes the integration loop that the
// per-package unit tests don't cover.

func wireE2EHamnut(t *testing.T, srv *httptest.Server, enabled bool) *hamnut.Service {
	t.Helper()
	cfg := types.LookupConfig{
		Name:           hamnut.ServiceName,
		Enabled:        enabled,
		URL:            srv.URL,
		UserAgent:      "smd/e2e",
		HttpTimeoutSec: 5,
	}
	s := hamnut.NewService(&logging.Service{}, nil, &cfg, srv.Client())
	if err := s.Initialize(); err != nil {
		t.Fatalf("hamnut Initialize: %v", err)
	}
	return s
}

func wireE2EQRZ(t *testing.T, srv *httptest.Server) *lookupqrz.Service {
	t.Helper()
	cfg := types.LookupConfig{
		Name:           lookupqrz.ServiceName,
		Enabled:        true,
		URL:            srv.URL,
		UserAgent:      "smd/e2e",
		HttpTimeoutSec: 5,
		Username:       "tester",
		Password:       "secret",
	}
	s := lookupqrz.NewService(&logging.Service{}, nil, &cfg, srv.Client())
	if err := s.Initialize(); err != nil {
		t.Fatalf("qrz Initialize: %v", err)
	}
	return s
}

// TestE2E_Enrich_HappyPath_RealProviders wires real hamnut + real
// QRZ services against httptest servers and exercises the full
// pipeline: HTTP request → Server → orchestrator → real providers →
// upstream → cache write → response. Proves all the package
// boundaries cohere end-to-end.
func TestE2E_Enrich_HappyPath_RealProviders(t *testing.T) {
	hamnutSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("prefix") != "M0CMC" {
			t.Errorf("hamnut: unexpected prefix query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "OK",
			"found": true,
			"continent": "EU",
			"countryName": "England",
			"cqZone": 14,
			"ituZone": 27,
			"prefix": "M",
			"primaryDXCCPrefix": "G",
			"countryCode": "GBR"
		}`))
	}))
	defer hamnutSrv.Close()

	// QRZ XML server: handles both session-key fetch (Initialize) and
	// callsign lookup (Lookup) on the same endpoint, distinguished by
	// the `username` vs `s` query params.
	qrzSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("username") != "" {
			// Session-key fetch.
			_, _ = w.Write([]byte(`<?xml version="1.0"?>
				<QRZDatabase>
					<Session><Key>e2e-session-key</Key></Session>
				</QRZDatabase>`))
			return
		}
		// Callsign lookup. Returns QRZ-bug country data for M0CMC
		// (ADR 0017's cautionary example) — the orchestrator's
		// filter+merge must replace it with hamnut's truth.
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
			<QRZDatabase>
				<Callsign>
					<call>M0CMC</call>
					<fname>Marc</fname>
					<name>Veary</name>
					<addr2>Birmingham</addr2>
					<country>Malawi</country>
					<grid>IO92</grid>
					<cqzone>37</cqzone>
					<ituzone>53</ituzone>
				</Callsign>
				<Session><Key>e2e-session-key</Key></Session>
			</QRZDatabase>`))
	}))
	defer qrzSrv.Close()

	srv := testServer(t)
	srv.enrich = &lookup.Orchestrator{
		DB:         srv.db,
		Country:    wireE2EHamnut(t, hamnutSrv, true),
		Chain:      []lookup.CallsignProvider{wireE2EQRZ(t, qrzSrv)},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	w := httptest.NewRecorder()
	srv.handleEnrichCallsign(w, enrichRequest("M0CMC"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var got lookup.Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Country fields come from hamnut.
	if got.Country.Name != "England" {
		t.Errorf("Country.Name = %q, want England", got.Country.Name)
	}
	if got.Country.CQZone != "14" {
		t.Errorf("Country.CQZone = %q, want 14", got.Country.CQZone)
	}
	if got.CountrySource != lookup.SourceHamnut {
		t.Errorf("CountrySource = %q, want %q", got.CountrySource, lookup.SourceHamnut)
	}

	// Station fields: name/QTH/grid from QRZ; country fields from hamnut
	// (filter stripped QRZ's bogus Malawi/37/53; merge filled hamnut's
	// England/14/27).
	if got.Station.Name != "Marc Veary" {
		t.Errorf("Station.Name = %q, want \"Marc Veary\"", got.Station.Name)
	}
	if got.Station.QTH != "Birmingham" {
		t.Errorf("Station.QTH = %q, want Birmingham", got.Station.QTH)
	}
	if got.Station.Country != "England" {
		t.Errorf("Station.Country = %q, want England (filter+merge replaced QRZ's Malawi)", got.Station.Country)
	}
	if got.Station.CQZ != "14" {
		t.Errorf("Station.CQZ = %q, want \"14\" (not QRZ's bogus 37)", got.Station.CQZ)
	}
	if got.StationSource != "qrzlookupservice" {
		t.Errorf("StationSource = %q, want qrzlookupservice", got.StationSource)
	}

	// Cache writes: both layers persist for next-Tab fast path.
	stored, err := srv.db.FetchCountryByPrefixWithContext(context.Background(), "M")
	if err != nil {
		t.Fatalf("country not persisted: %v", err)
	}
	if stored.Name != "England" {
		t.Errorf("stored country name = %q", stored.Name)
	}
	storedStation, err := srv.db.FetchContactedStationByCallsignWithContext(context.Background(), "M0CMC")
	if err != nil {
		t.Fatalf("contacted_station not persisted: %v", err)
	}
	if storedStation.Country != "England" {
		t.Errorf("stored station country = %q, want England (merge applies before write)", storedStation.Country)
	}
	if storedStation.CQZ != "14" {
		t.Errorf("stored station CQZ = %q, want \"14\"", storedStation.CQZ)
	}
}

// TestE2E_Enrich_NextTabIsCacheHit verifies the second Tab against
// the same callsign hits the cache without contacting upstream — the
// load-bearing benefit of the offline-first design per ADR 0017.
func TestE2E_Enrich_NextTabIsCacheHit(t *testing.T) {
	hamnutCalls := 0
	hamnutSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hamnutCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK","found":true,"countryName":"England","prefix":"M","cqZone":14,"ituZone":27,"primaryDXCCPrefix":"G","countryCode":"GBR","continent":"EU"}`))
	}))
	defer hamnutSrv.Close()

	qrzCalls := 0
	qrzSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("username") != "" {
			// Session fetch — counts once at Initialize, doesn't matter.
			_, _ = w.Write([]byte(`<?xml version="1.0"?><QRZDatabase><Session><Key>k</Key></Session></QRZDatabase>`))
			return
		}
		qrzCalls++
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
			<QRZDatabase>
				<Callsign><call>M0CMC</call><fname>Marc</fname><name>Veary</name></Callsign>
				<Session><Key>k</Key></Session>
			</QRZDatabase>`))
	}))
	defer qrzSrv.Close()

	srv := testServer(t)
	srv.enrich = &lookup.Orchestrator{
		DB:         srv.db,
		Country:    wireE2EHamnut(t, hamnutSrv, true),
		Chain:      []lookup.CallsignProvider{wireE2EQRZ(t, qrzSrv)},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	// First Tab — cold miss on both layers, hits upstream.
	w1 := httptest.NewRecorder()
	srv.handleEnrichCallsign(w1, enrichRequest("M0CMC"))
	if w1.Code != http.StatusOK {
		t.Fatalf("first tab: status = %d", w1.Code)
	}
	if hamnutCalls != 1 {
		t.Errorf("after first tab: hamnut calls = %d, want 1", hamnutCalls)
	}
	if qrzCalls != 1 {
		t.Errorf("after first tab: qrz calls = %d, want 1", qrzCalls)
	}

	// Second Tab — both layers fresh in cache; no upstream calls.
	w2 := httptest.NewRecorder()
	srv.handleEnrichCallsign(w2, enrichRequest("M0CMC"))
	if w2.Code != http.StatusOK {
		t.Fatalf("second tab: status = %d", w2.Code)
	}
	if hamnutCalls != 1 {
		t.Errorf("after second tab: hamnut calls = %d, want 1 (cache hit, no upstream)", hamnutCalls)
	}
	if qrzCalls != 1 {
		t.Errorf("after second tab: qrz calls = %d, want 1 (cache hit, no upstream)", qrzCalls)
	}

	var got lookup.Result
	if err := json.Unmarshal(w2.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CountrySource != lookup.SourceCountryTable {
		t.Errorf("second tab CountrySource = %q, want %q", got.CountrySource, lookup.SourceCountryTable)
	}
	if got.StationSource != lookup.SourceContactedTable {
		t.Errorf("second tab StationSource = %q, want %q", got.StationSource, lookup.SourceContactedTable)
	}
}

// TestE2E_Enrich_HamnutDown_FallsThrough exercises ADR 0017 #7 —
// hamnut transport failure folds into source=none rather than failing
// the response. The chain still runs and produces a station result;
// merge has no country data to fill in, so station.Country stays empty.
func TestE2E_Enrich_HamnutDown_FallsThrough(t *testing.T) {
	hamnutSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer hamnutSrv.Close()

	qrzSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("username") != "" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><QRZDatabase><Session><Key>k</Key></Session></QRZDatabase>`))
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
			<QRZDatabase>
				<Callsign>
					<call>M0CMC</call>
					<fname>Marc</fname>
					<name>Veary</name>
					<country>Malawi</country>
					<cqzone>37</cqzone>
				</Callsign>
				<Session><Key>k</Key></Session>
			</QRZDatabase>`))
	}))
	defer qrzSrv.Close()

	srv := testServer(t)
	srv.enrich = &lookup.Orchestrator{
		DB:         srv.db,
		Country:    wireE2EHamnut(t, hamnutSrv, true),
		Chain:      []lookup.CallsignProvider{wireE2EQRZ(t, qrzSrv)},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	w := httptest.NewRecorder()
	srv.handleEnrichCallsign(w, enrichRequest("M0CMC"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (always-200 per ADR 0017 #12)", w.Code)
	}
	var got lookup.Result
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CountrySource != lookup.SourceNone {
		t.Errorf("CountrySource = %q, want %q (hamnut down)", got.CountrySource, lookup.SourceNone)
	}
	if got.StationSource != "qrzlookupservice" {
		t.Errorf("StationSource = %q, want qrzlookupservice", got.StationSource)
	}
	// Filter applied even without merge — station.Country stays empty
	// rather than retaining QRZ's bogus "Malawi."
	if got.Station.Country != "" {
		t.Errorf("Station.Country = %q, want empty (hamnut down + filter ran)", got.Station.Country)
	}
	if got.Station.CQZ != "" {
		t.Errorf("Station.CQZ = %q, want empty (hamnut down + filter ran)", got.Station.CQZ)
	}
	// Station name preserved (operator-shaped field; not filtered).
	if got.Station.Name != "Marc Veary" {
		t.Errorf("Station.Name = %q, want Marc Veary", got.Station.Name)
	}
}
