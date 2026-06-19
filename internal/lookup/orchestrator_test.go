package lookup_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	sqlsvc "github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// ----- test infrastructure -----

// newTestSqlite spins up an in-memory sqlite.Service, mirrors the
// pattern from sqlite/service_test.go's testService helper. Inlined
// here (rather than imported) because the test-helper isn't exported.
func newTestSqlite(t *testing.T) *sqlsvc.Service {
	t.Helper()

	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"

	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}

	logSvc := &logging.Service{}
	logSvc.ConfigService = cfgSvc
	logSvc.WorkingDir = cfgSvc.WorkingDir()
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}

	svc := &sqlsvc.Service{}
	svc.ConfigService = cfgSvc
	svc.LoggerService = logSvc
	if err := svc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	svc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      ":memory:",
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := svc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := svc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = svc.Close()
		_ = logSvc.Close()
	})
	return svc
}

// ----- stub providers -----

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

// ----- async-refresher stubs -----

// syncRefresher runs scheduled work immediately and synchronously —
// makes test outcomes deterministic. The production AsyncRefresher
// (task #61) will run things in the background; tests pin behaviour
// without flakiness via this stub.
type syncRefresher struct{ scheduled int }

func (r *syncRefresher) Schedule(fn func(ctx context.Context)) {
	r.scheduled++
	fn(context.Background())
}

// recordingRefresher records the schedule count without running the
// fn — for tests that only care that scheduling happened.
type recordingRefresher struct{ scheduled int }

func (r *recordingRefresher) Schedule(_ func(ctx context.Context)) { r.scheduled++ }

// ----- IsEmpty -----

func TestIsEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   types.ContactedStation
		want bool
	}{
		{"all empty", types.ContactedStation{}, true},
		{"only call (echo of input)", types.ContactedStation{Call: "M0CMC"}, true},
		{"name set", types.ContactedStation{Call: "M0CMC", Name: "Marc"}, false},
		{"qth set", types.ContactedStation{QTH: "Lilongwe"}, false},
		{"grid set", types.ContactedStation{Gridsquare: "KH53"}, false},
		// country/cont/cqz/ituz/dxcc don't count — orchestrator filters them.
		{"only country (gets stripped)", types.ContactedStation{Country: "Malawi"}, true},
		{"only cqz (gets stripped)", types.ContactedStation{CQZ: "37"}, true},
		{"only ituz (gets stripped)", types.ContactedStation{ITUZ: "27"}, true},
		{"only dxcc (gets stripped)", types.ContactedStation{DXCC: "223"}, true},
		{"only cont (gets stripped)", types.ContactedStation{Cont: "EU"}, true},
		// storage metadata never counts as provider data.
		{"only csid (metadata)", types.ContactedStation{CSID: 42}, true},
		// L1 (review 2026-06-04): all station-provider-owned fields now count,
		// so a provider returning only one of these is NOT treated as empty.
		{"lat set", types.ContactedStation{Lat: "52.5"}, false},
		{"lon set", types.ContactedStation{Lon: "-1.9"}, false},
		{"altitude set", types.ContactedStation{Altitude: "100"}, false},
		{"age set", types.ContactedStation{Age: "45"}, false},
		{"eq_call set", types.ContactedStation{EqCall: "M0XYZ"}, false},
		{"iota set", types.ContactedStation{Iota: "EU-005"}, false},
		{"sig set", types.ContactedStation{Sig: "POTA"}, false},
		{"sig_info set", types.ContactedStation{SigInfo: "GB-0001"}, false},
		{"wwff set", types.ContactedStation{WwffRef: "GFF-1234"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lookup.IsEmpty(c.in); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// ----- L3: boundary callsign normalization -----

// TestEnrich_LowercaseInput_HitsCanonicalRow pins the L3 fix: the orchestrator
// uppercases the callsign at its own boundary, so a lower-case input resolves
// to the canonical (upper-case) cache row instead of missing it, hitting
// upstream, and writing a duplicate row (review 2026-06-04 L3). The HTTP
// handler already uppercases; this guarantees it for direct package callers.
func TestEnrich_LowercaseInput_HitsCanonicalRow(t *testing.T) {
	db := newTestSqlite(t)
	// Seed the canonical, upper-case station cache row (fresh on write).
	if err := db.UpsertContactedStationWithContext(context.Background(), types.ContactedStation{
		Call: "M0CMC",
		Name: "Marc",
	}); err != nil {
		t.Fatalf("seed station: %v", err)
	}
	// A station provider that, if reached, proves a cache MISS (and returns
	// the wrong data).
	qrz := &stubCallsignProvider{name: "qrz", result: types.ContactedStation{Call: "M0CMC", Name: "WRONG"}}
	o := &lookup.Orchestrator{
		DB:         db,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "m0cmc") // lower-case input

	if got.Callsign != "M0CMC" {
		t.Errorf("Result.Callsign = %q, want M0CMC (uppercased at the boundary)", got.Callsign)
	}
	if got.Station.Name != "Marc" {
		t.Errorf("Station.Name = %q, want Marc (lower-case input must hit the canonical cache row)", got.Station.Name)
	}
	if qrz.calls != 0 {
		t.Errorf("station provider called %d times, want 0 (cache hit on the canonical row, no upstream)", qrz.calls)
	}
}

// ----- H1: force/async refresh replaces the row (clears emptied fields) -----

// TestEnrichRefresh_ReplacesRow_ClearsEmptiedField pins the H1 fix: a
// force-refresh whose provider no longer returns a field must CLEAR that field
// in the cache, not retain the stale value via a merge (review 2026-06-04 H1).
func TestEnrichRefresh_ReplacesRow_ClearsEmptiedField(t *testing.T) {
	db := newTestSqlite(t)
	if err := db.UpsertContactedStationWithContext(context.Background(), types.ContactedStation{
		Call:  "M0CMC",
		Name:  "Marc",
		QTH:   "Lilongwe",
		Email: "old@example.com",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The refresh provider no longer returns an email (it went away upstream).
	qrz := &stubCallsignProvider{name: "qrz", result: types.ContactedStation{
		Call: "M0CMC",
		Name: "Marc Veary",
		QTH:  "Lilongwe",
	}}
	o := &lookup.Orchestrator{
		DB:         db,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.EnrichRefresh(context.Background(), "M0CMC")
	if got.Station.Email != "" {
		t.Errorf("Result.Station.Email = %q, want empty after force-refresh", got.Station.Email)
	}

	stored, err := db.FetchContactedStationByCallsignWithContext(context.Background(), "M0CMC")
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if stored.Email != "" {
		t.Errorf("stored Email = %q, want cleared (replace, not merge)", stored.Email)
	}
	if stored.Name != "Marc Veary" {
		t.Errorf("stored Name = %q, want the refresh value", stored.Name)
	}
}

// ----- H2: result JSON shape + cold-miss timestamp -----

// TestResult_MarshalJSON_OmitsLayersOnSourceNone pins the H2 fix: a no-data
// layer must be absent from the JSON, not serialized as an empty object.
func TestResult_MarshalJSON_OmitsLayersOnSourceNone(t *testing.T) {
	r := lookup.Result{Callsign: "M0CMC", CountrySource: lookup.SourceNone, StationSource: lookup.SourceNone}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, `"country"`) {
		t.Errorf("country object present on source=none: %s", s)
	}
	if strings.Contains(s, `"station"`) {
		t.Errorf("station object present on source=none: %s", s)
	}
	if !strings.Contains(s, `"country_source":"none"`) || !strings.Contains(s, `"callsign":"M0CMC"`) {
		t.Errorf("scalars missing from result JSON: %s", s)
	}
}

// TestResult_MarshalJSON_IncludesPresentLayer confirms a populated layer still
// serializes (only the source=none layers are dropped).
func TestResult_MarshalJSON_IncludesPresentLayer(t *testing.T) {
	r := lookup.Result{
		Callsign:      "M0CMC",
		Country:       types.Country{Name: "England"},
		CountrySource: lookup.SourceHamnut,
		StationSource: lookup.SourceNone,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"country"`) || !strings.Contains(s, "England") {
		t.Errorf("present country layer should serialize: %s", s)
	}
	if strings.Contains(s, `"station"`) {
		t.Errorf("station should be omitted (source=none): %s", s)
	}
}

// TestEnrich_ColdMiss_CarriesLastRefreshedAt pins the H2 cold-miss-timestamp
// fix: a freshly fetched layer carries a real last_refreshed_at, not the zero
// value, so the response matches the cache-hit path.
func TestEnrich_ColdMiss_CarriesLastRefreshedAt(t *testing.T) {
	db := newTestSqlite(t)
	hamnut := &stubCountryProvider{name: "hamnut", result: types.Country{Name: "England", Prefix: "M"}}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}
	got := o.Enrich(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceHamnut {
		t.Fatalf("expected cold-miss hamnut hit, got %q", got.CountrySource)
	}
	if got.Country.LastRefreshedAt.IsZero() {
		t.Error("cold-miss country should carry a non-zero LastRefreshedAt (H2)")
	}
}

// ----- cold-miss paths -----

func TestEnrich_ColdMiss_HamnutHit_StoresAndReturns(t *testing.T) {
	db := newTestSqlite(t)
	hamnut := &stubCountryProvider{
		name: "hamnut",
		result: types.Country{
			Name:      "England",
			Prefix:    "M",
			CQZone:    "14",
			ITUZone:   "27",
			Continent: "EU",
		},
	}
	ref := &syncRefresher{}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		Chain:      nil,
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  ref,
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceHamnut {
		t.Errorf("CountrySource = %q, want %q", got.CountrySource, lookup.SourceHamnut)
	}
	if got.Country.Name != "England" {
		t.Errorf("Country.Name = %q, want England", got.Country.Name)
	}
	if hamnut.calls != 1 {
		t.Errorf("hamnut calls = %d, want 1", hamnut.calls)
	}

	// Row was written through to the cache — the next Tab against the
	// same prefix should hit fresh and skip the upstream call.
	stored, err := db.FetchCountryByPrefix("M")
	if err != nil {
		t.Fatalf("country not persisted: %v", err)
	}
	if stored.LastRefreshedAt.IsZero() {
		t.Error("stored row missing LastRefreshedAt")
	}
}

func TestEnrich_ColdMiss_ChainFirstHit_NoCountrySource_Stores(t *testing.T) {
	// Chain returns QRZ-bug country fields, no hamnut wired. After
	// filter the country fields are empty; the merge has nothing to
	// fill in (no country source); the persisted row carries empty
	// country fields. Per ADR 0017 #2 this is correct: better empty
	// than QRZ-bug values silently surviving.
	db := newTestSqlite(t)
	qrz := &stubCallsignProvider{
		name: "qrz",
		result: types.ContactedStation{
			Call:       "M0CMC",
			Name:       "Marc Veary",
			QTH:        "Lilongwe",
			Gridsquare: "KH53",
			Country:    "Malawi", // QRZ-bug per ADR 0017 example
			CQZ:        "37",
			ITUZ:       "53",
		},
	}
	hamqth := &stubCallsignProvider{name: "hamqth"}
	o := &lookup.Orchestrator{
		DB:         db,
		Chain:      []lookup.CallsignProvider{qrz, hamqth},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.StationSource != "qrz" {
		t.Errorf("StationSource = %q, want qrz", got.StationSource)
	}
	if got.Station.Name != "Marc Veary" {
		t.Errorf("Name = %q, want Marc Veary", got.Station.Name)
	}
	// Filter zeroed the QRZ-bug country fields; with no country
	// source available, merge is a no-op so they stay empty.
	if got.Station.Country != "" {
		t.Errorf("Country leaked: %q (filter must zero, merge had no data to fill)", got.Station.Country)
	}
	if got.Station.CQZ != "" {
		t.Errorf("CQZ leaked: %q", got.Station.CQZ)
	}
	if got.Station.ITUZ != "" {
		t.Errorf("ITUZ leaked: %q", got.Station.ITUZ)
	}
	if hamqth.calls != 0 {
		t.Errorf("second provider called %d time(s); should not run after first hit", hamqth.calls)
	}

	stored, err := db.FetchContactedStationByCallsign("M0CMC")
	if err != nil {
		t.Fatalf("station not persisted: %v", err)
	}
	if stored.Country != "" {
		t.Errorf("stored row carries country %q — filter must run before write", stored.Country)
	}
}

func TestEnrich_ColdMiss_ChainAndHamnut_MergesAndStoresHamnutTruth(t *testing.T) {
	// The flagship case — both layers cold-miss, both providers run,
	// the orchestrator filters QRZ-bug fields and merges hamnut's
	// truth into the station before persisting. Pinned because the
	// merge step is what makes ADR 0017 #2 ("country is hamnut-
	// exclusive on write") actually load-bearing in the writeback path.
	db := newTestSqlite(t)
	qrz := &stubCallsignProvider{
		name: "qrz",
		result: types.ContactedStation{
			Call:       "M0CMC",
			Name:       "Marc Veary",
			QTH:        "Lilongwe",
			Gridsquare: "KH53",
			Country:    "Malawi", // QRZ-bug per ADR 0017
			CQZ:        "37",
			ITUZ:       "53",
		},
	}
	hamnut := &stubCountryProvider{
		name: "hamnut",
		result: types.Country{
			Name:       "England",
			Prefix:     "M",
			Continent:  "EU",
			CQZone:     "14",
			ITUZone:    "27",
			DXCCPrefix: "G",
		},
	}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "M0CMC")

	// Station's country fields now carry hamnut's truth, NOT QRZ-bug
	// values. This is the load-bearing assertion: ADR 0017 #2
	// "country is hamnut-exclusive on write" landed end-to-end.
	if got.Station.Country != "England" {
		t.Errorf("Station.Country = %q, want \"England\" (hamnut truth, not Malawi)", got.Station.Country)
	}
	if got.Station.Cont != "EU" {
		t.Errorf("Station.Cont = %q, want EU", got.Station.Cont)
	}
	if got.Station.CQZ != "14" {
		t.Errorf("Station.CQZ = %q, want \"14\" (not QRZ's bogus 37)", got.Station.CQZ)
	}
	if got.Station.ITUZ != "27" {
		t.Errorf("Station.ITUZ = %q, want \"27\" (not QRZ's bogus 53)", got.Station.ITUZ)
	}
	// DXCC (the numeric ADIF entity field) must stay empty — hamnut's
	// alphabetic prefix is NOT a valid DXCC entity code, so it is no
	// longer merged onto the station. The prefix lives on Country.DXCCPrefix.
	if got.Station.DXCC != "" {
		t.Errorf("Station.DXCC = %q, want \"\" (prefix must not fill the numeric DXCC field)", got.Station.DXCC)
	}
	if got.Country.DXCCPrefix != "G" {
		t.Errorf("Country.DXCCPrefix = %q, want G", got.Country.DXCCPrefix)
	}

	// Result.Country also carries hamnut's truth — the country layer's
	// independent return.
	if got.Country.Name != "England" {
		t.Errorf("Country.Name = %q, want England", got.Country.Name)
	}

	// Persisted row picks up the merged values.
	stored, err := db.FetchContactedStationByCallsign("M0CMC")
	if err != nil {
		t.Fatalf("station not persisted: %v", err)
	}
	if stored.Country != "England" {
		t.Errorf("stored.Country = %q, want \"England\" (merge must apply before write)", stored.Country)
	}
	if stored.CQZ != "14" {
		t.Errorf("stored.CQZ = %q, want \"14\"", stored.CQZ)
	}
}

func TestEnrich_ColdStation_FreshCountry_MergesFromCache(t *testing.T) {
	// Cold-station + fresh-country: chain runs, hamnut does NOT run
	// (country is cached fresh), and the merge populates from the
	// cached country value.
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:    "England",
		Prefix:  "M",
		CQZone:  "14",
		ITUZone: "27",
	}); err != nil {
		t.Fatalf("seed country: %v", err)
	}

	hamnut := &stubCountryProvider{name: "hamnut"} // should not be called
	qrz := &stubCallsignProvider{
		name: "qrz",
		result: types.ContactedStation{
			Call: "M0CMC",
			Name: "Marc",
			QTH:  "Birmingham",
		},
	}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour, // freshly written, not stale
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.Station.Country != "England" {
		t.Errorf("Station.Country = %q, want England (merged from cached country)", got.Station.Country)
	}
	if got.CountrySource != lookup.SourceCountryTable {
		t.Errorf("CountrySource = %q, want %q", got.CountrySource, lookup.SourceCountryTable)
	}
	if hamnut.calls != 0 {
		t.Errorf("hamnut called %d times; should be zero (country was cached fresh)", hamnut.calls)
	}
}

func TestEnrich_FreshStation_FreshCountry_MergeIsNoOp(t *testing.T) {
	// Both layers fresh-cached. Already-correct denormalized country
	// fields on the station; merge sees same value on both sides and
	// is a no-op. No upstream calls; no async refreshes.
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:    "England",
		Prefix:  "M",
		CQZone:  "14",
		ITUZone: "27",
	}); err != nil {
		t.Fatalf("seed country: %v", err)
	}
	if err := db.UpsertContactedStation(types.ContactedStation{
		Call:    "M0CMC",
		Name:    "Marc",
		QTH:     "Birmingham",
		Country: "England", // already denormalized correctly
		CQZ:     "14",
		ITUZ:    "27",
	}); err != nil {
		t.Fatalf("seed station: %v", err)
	}

	hamnut := &stubCountryProvider{name: "hamnut"}
	qrz := &stubCallsignProvider{name: "qrz"}
	ref := &recordingRefresher{}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  ref,
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.Station.Country != "England" {
		t.Errorf("Station.Country = %q, want England", got.Station.Country)
	}
	if hamnut.calls != 0 || qrz.calls != 0 {
		t.Errorf("upstreams called: hamnut=%d qrz=%d, want 0/0 (both fresh)", hamnut.calls, qrz.calls)
	}
	if ref.scheduled != 0 {
		t.Errorf("refresher fired %d times; want 0 (nothing stale)", ref.scheduled)
	}
}

func TestEnrich_StaleStation_FreshCountry_AsyncRefreshReMerges(t *testing.T) {
	// Stale-station refresh fires async; that fn re-runs the chain,
	// reads country (cached fresh), merges, writes. Verify the
	// post-refresh row carries hamnut-truth country fields even
	// though the chain returned the QRZ-bug values.
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:    "England",
		Prefix:  "M",
		CQZone:  "14",
		ITUZone: "27",
	}); err != nil {
		t.Fatalf("seed country: %v", err)
	}
	if err := db.UpsertContactedStation(types.ContactedStation{
		Call: "M0CMC",
		Name: "OldName",
	}); err != nil {
		t.Fatalf("seed station: %v", err)
	}

	hamnut := &stubCountryProvider{name: "hamnut"} // not called for fresh country
	qrz := &stubCallsignProvider{
		name: "qrz",
		result: types.ContactedStation{
			Call: "M0CMC",
			Name: "NewName",
			// QRZ-bug values — async refresh must filter + merge them out.
			Country: "Malawi",
			CQZ:     "37",
			ITUZ:    "53",
		},
	}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour,           // country fresh
		StationTTL: 1 * time.Nanosecond, // station stale
		Refresher:  &syncRefresher{},    // synchronous so the test can read the post-refresh row
	}
	time.Sleep(2 * time.Millisecond) // ensure stationTTL crossed

	got := o.Enrich(context.Background(), "M0CMC")

	// Synchronous return uses the cached stale row but with merged
	// country (from cached fresh country).
	if got.Station.Name != "OldName" {
		t.Errorf("immediate return: Name = %q, want OldName (stale-cached value)", got.Station.Name)
	}
	if got.Station.Country != "England" {
		t.Errorf("immediate return: Country = %q, want England (merged from cached country)", got.Station.Country)
	}

	// After the sync refresh, the row carries the chain's new Name
	// AND hamnut's country values (NOT QRZ-bug Malawi/37/53).
	postRefresh, err := db.FetchContactedStationByCallsign("M0CMC")
	if err != nil {
		t.Fatalf("post-refresh fetch: %v", err)
	}
	if postRefresh.Name != "NewName" {
		t.Errorf("post-refresh: Name = %q, want NewName (chain refreshed)", postRefresh.Name)
	}
	if postRefresh.Country != "England" {
		t.Errorf("post-refresh: Country = %q, want England (merge in async refresh)", postRefresh.Country)
	}
	if postRefresh.CQZ != "14" {
		t.Errorf("post-refresh: CQZ = %q, want \"14\" (hamnut truth)", postRefresh.CQZ)
	}
	if postRefresh.ITUZ != "27" {
		t.Errorf("post-refresh: ITUZ = %q, want \"27\" (hamnut truth)", postRefresh.ITUZ)
	}
}

func TestMergeStationFromCountry(t *testing.T) {
	cases := []struct {
		name string
		s    types.ContactedStation
		c    types.Country
		want types.ContactedStation
	}{
		{
			"empty station fills from country",
			types.ContactedStation{Name: "Marc"},
			types.Country{Name: "England", CQZone: "14", ITUZone: "27", Continent: "EU", DXCCPrefix: "G"},
			// DXCCPrefix is intentionally NOT merged onto the station's
			// numeric DXCC field (it's an alphabetic prefix, not an entity code).
			types.ContactedStation{Name: "Marc", Country: "England", CQZ: "14", ITUZ: "27", Cont: "EU"},
		},
		{
			"only when different — same value is a no-op",
			types.ContactedStation{Country: "England", CQZ: "14"},
			types.Country{Name: "England", CQZone: "14"},
			types.ContactedStation{Country: "England", CQZ: "14"},
		},
		{
			"different value overwrites",
			types.ContactedStation{Country: "Malawi"}, // QRZ-bug value
			types.Country{Name: "England"},
			types.ContactedStation{Country: "England"},
		},
		{
			"empty country leaves station alone",
			types.ContactedStation{Country: "Existing"},
			types.Country{},
			types.ContactedStation{Country: "Existing"},
		},
		{
			"partial fill — only fields with country data",
			types.ContactedStation{Country: "England"},
			types.Country{CQZone: "14"}, // Name empty, CQZone set
			types.ContactedStation{Country: "England", CQZ: "14"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lookup.MergeStationFromCountry(c.s, c.c)
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestEnrich_ColdMiss_FirstEmptyFallsThrough(t *testing.T) {
	db := newTestSqlite(t)
	qrz := &stubCallsignProvider{name: "qrz"} // empty result, nil err
	hamqth := &stubCallsignProvider{
		name:   "hamqth",
		result: types.ContactedStation{Call: "M0CMC", Name: "From HamQTH"},
	}
	o := &lookup.Orchestrator{
		DB:         db,
		Chain:      []lookup.CallsignProvider{qrz, hamqth},
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.StationSource != "hamqth" {
		t.Errorf("StationSource = %q, want hamqth (empty QRZ falls through)", got.StationSource)
	}
	if got.Station.Name != "From HamQTH" {
		t.Errorf("Name = %q, want From HamQTH", got.Station.Name)
	}
	if qrz.calls != 1 || hamqth.calls != 1 {
		t.Errorf("call counts: qrz=%d hamqth=%d, want 1/1", qrz.calls, hamqth.calls)
	}
}

func TestEnrich_ColdMiss_FirstErrorFallsThrough(t *testing.T) {
	db := newTestSqlite(t)
	qrz := &stubCallsignProvider{name: "qrz", err: errors.ErrNotFound}
	hamqth := &stubCallsignProvider{
		name:   "hamqth",
		result: types.ContactedStation{Call: "M0CMC", Name: "From HamQTH"},
	}
	o := &lookup.Orchestrator{
		DB:         db,
		Chain:      []lookup.CallsignProvider{qrz, hamqth},
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.StationSource != "hamqth" {
		t.Errorf("StationSource = %q, want hamqth (errored QRZ falls through per ADR 0017 #8)", got.StationSource)
	}
}

func TestEnrich_ColdMiss_AllEmpty_NoRow(t *testing.T) {
	db := newTestSqlite(t)
	o := &lookup.Orchestrator{
		DB: db,
		Chain: []lookup.CallsignProvider{
			&stubCallsignProvider{name: "qrz"},
			&stubCallsignProvider{name: "hamqth"},
		},
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "ZZ9XYZ")
	if got.StationSource != lookup.SourceNone {
		t.Errorf("StationSource = %q, want %q", got.StationSource, lookup.SourceNone)
	}

	// Per ADR 0017 #9, full-miss writes no row.
	if _, err := db.FetchContactedStationByCallsign("ZZ9XYZ"); err == nil {
		t.Error("contacted_station row written for all-empty chain — should be no-row")
	}
}

func TestEnrich_ColdMiss_HamnutDown_NoRow(t *testing.T) {
	db := newTestSqlite(t)
	hamnut := &stubCountryProvider{name: "hamnut", err: stderrErrTransport()}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		CountryTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceNone {
		t.Errorf("CountrySource = %q, want %q (transport failure → empty per ADR 0017 #7)",
			got.CountrySource, lookup.SourceNone)
	}
	if _, err := db.FetchCountryByPrefix("M"); err == nil {
		t.Error("country row written despite upstream failure")
	}
}

func TestEnrich_ColdMiss_HamnutDisabledSentinel_TreatedAsNone(t *testing.T) {
	db := newTestSqlite(t)
	hamnut := &stubCountryProvider{
		name:   "hamnut",
		result: types.Country{Name: "Unknown"}, // disabled sentinel from hamnut.Service
	}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		CountryTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceNone {
		t.Errorf("CountrySource = %q, want %q (disabled sentinel must not be cached)",
			got.CountrySource, lookup.SourceNone)
	}
}

// ----- fresh / stale hits -----

func TestEnrich_FreshHit_NoUpstreamCall(t *testing.T) {
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:   "England",
		Prefix: "M",
		CQZone: "14",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	hamnut := &stubCountryProvider{name: "hamnut"}
	ref := &recordingRefresher{}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		CountryTTL: time.Hour, // freshly written, not stale
		Refresher:  ref,
	}

	got := o.Enrich(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceCountryTable {
		t.Errorf("CountrySource = %q, want %q", got.CountrySource, lookup.SourceCountryTable)
	}
	if got.Country.Name != "England" {
		t.Errorf("Country.Name = %q, want England", got.Country.Name)
	}
	if hamnut.calls != 0 {
		t.Errorf("hamnut called %d times on fresh hit; should be zero", hamnut.calls)
	}
	if ref.scheduled != 0 {
		t.Errorf("refresher fired %d times on fresh hit; should be zero", ref.scheduled)
	}
}

func TestEnrich_StaleHit_ServesStaleAndSchedulesRefresh(t *testing.T) {
	db := newTestSqlite(t)
	// Seed with last_refreshed_at far enough in the past to be stale
	// against the orchestrator's TTL. We seed via UpsertCountry which
	// stamps now; then we directly back-date by re-writing through the
	// model layer would be the precise approach. Simpler: configure a
	// negative-now TTL of zero by setting TTL to 1ms and sleeping past
	// it. (TTL <= 0 means "never stale" per isStale, so we use small-
	// but-positive.)
	if err := db.UpsertCountry(types.Country{
		Name:   "England",
		Prefix: "M",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	hamnut := &stubCountryProvider{
		name: "hamnut",
		result: types.Country{
			Name:    "England",
			Prefix:  "M",
			CQZone:  "14",
			ITUZone: "27",
		},
	}
	ref := &syncRefresher{}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		CountryTTL: 1 * time.Nanosecond, // anything seeded > 1ns ago is stale
		Refresher:  ref,
	}

	// Sleep to ensure the seeded row's LastRefreshedAt is past the
	// 1ns TTL boundary.
	time.Sleep(2 * time.Millisecond)

	got := o.Enrich(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceCountryTable {
		t.Errorf("CountrySource = %q, want %q (stale-hit serves cached)",
			got.CountrySource, lookup.SourceCountryTable)
	}
	if ref.scheduled != 1 {
		t.Errorf("refresher fired %d times on stale hit; should be 1", ref.scheduled)
	}
	// syncRefresher ran immediately, so the upstream was called as
	// part of that schedule.
	if hamnut.calls != 1 {
		t.Errorf("hamnut calls = %d, want 1 (from the async refresh)", hamnut.calls)
	}
}

// ----- empty input / nil chain edge cases -----

func TestEnrich_EmptyCallsign_NoneNone(t *testing.T) {
	db := newTestSqlite(t)
	o := &lookup.Orchestrator{DB: db}
	got := o.Enrich(context.Background(), "")
	if got.CountrySource != lookup.SourceNone || got.StationSource != lookup.SourceNone {
		t.Errorf("empty callsign should return source=none/none; got %q/%q",
			got.CountrySource, got.StationSource)
	}
}

// ----- panic safety (M2 regression) -----

// panickingCountryProvider panics on Lookup. Pre-fix, the orchestrator
// spawned readCountry in a bare goroutine — the panic would crash the
// daemon. Post-fix, safego.Go recovers the panic, the deferred channel
// send delivers a zero-value outcome, and Enrich returns with
// CountrySource=none rather than deadlocking.
type panickingCountryProvider struct{ name string }

func (p *panickingCountryProvider) Name() string                       { return p.name }
func (p *panickingCountryProvider) Initialize(_ context.Context) error { return nil }
func (p *panickingCountryProvider) Lookup(_ string) (types.Country, error) {
	panic("synthetic country panic")
}
func (p *panickingCountryProvider) LookupWithContext(_ context.Context, _ string) (types.Country, error) {
	panic("synthetic country panic")
}

type panickingCallsignProvider struct{ name string }

func (p *panickingCallsignProvider) Name() string                       { return p.name }
func (p *panickingCallsignProvider) Initialize(_ context.Context) error { return nil }
func (p *panickingCallsignProvider) Lookup(_ string) (types.ContactedStation, error) {
	panic("synthetic chain panic")
}
func (p *panickingCallsignProvider) LookupWithContext(_ context.Context, _ string) (types.ContactedStation, error) {
	panic("synthetic chain panic")
}

func TestEnrich_PanicInCountryProvider_RecoveredAndNoDeadlock(t *testing.T) {
	db := newTestSqlite(t)
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    &panickingCountryProvider{name: "panic-country"},
		Chain:      nil,
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	// Bound the call so a regression that re-introduces the deadlock
	// surfaces as a test timeout rather than hanging the suite.
	done := make(chan lookup.Result, 1)
	go func() { done <- o.Enrich(context.Background(), "M0CMC") }()

	select {
	case got := <-done:
		// Country panic → CountrySource normalised to "none";
		// station empty since no chain is wired.
		if got.CountrySource != lookup.SourceNone {
			t.Errorf("CountrySource = %q, want %q (panic should be recovered as no-data)",
				got.CountrySource, lookup.SourceNone)
		}
		if got.StationSource != lookup.SourceNone {
			t.Errorf("StationSource = %q, want %q", got.StationSource, lookup.SourceNone)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enrich deadlocked — defer-send-zero pattern broken")
	}
}

func TestEnrich_PanicInChainProvider_RecoveredAndNoDeadlock(t *testing.T) {
	db := newTestSqlite(t)
	o := &lookup.Orchestrator{
		DB: db,
		Chain: []lookup.CallsignProvider{
			&panickingCallsignProvider{name: "panic-chain"},
		},
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	done := make(chan lookup.Result, 1)
	go func() { done <- o.Enrich(context.Background(), "M0CMC") }()

	select {
	case got := <-done:
		if got.StationSource != lookup.SourceNone {
			t.Errorf("StationSource = %q, want %q (panic should be recovered as no-data)",
				got.StationSource, lookup.SourceNone)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enrich deadlocked — defer-send-zero pattern broken")
	}
}

func TestEnrich_NoProviders_NoneNone(t *testing.T) {
	db := newTestSqlite(t)
	o := &lookup.Orchestrator{DB: db, CountryTTL: time.Hour, StationTTL: time.Hour}
	got := o.Enrich(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceNone || got.StationSource != lookup.SourceNone {
		t.Errorf("no providers + empty cache should return source=none/none; got %q/%q",
			got.CountrySource, got.StationSource)
	}
}

// ----- helper: synthetic transport-failure sentinel -----

// stderrErrTransport returns a non-ErrNotFound error suitable for
// stub providers that simulate transport failure. Distinct error
// instance from any sentinel the orchestrator special-cases.
func stderrErrTransport() error {
	return &transportError{}
}

type transportError struct{}

func (e *transportError) Error() string { return "synthetic transport failure" }

// ---- local-time recompute (cache-symmetry) ----

// TestEnrich_FreshHit_LocalTimeRecomputed — cached country row carries
// only TimeOffset; the orchestrator must compute LocalTime at return
// time so the SPA's response shape is uniform regardless of which
// cache state the country layer landed in.
func TestEnrich_FreshHit_LocalTimeRecomputed(t *testing.T) {
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:       "Malawi",
		Prefix:     "7Q",
		TimeOffset: "2h 0m",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	o := &lookup.Orchestrator{
		DB:         db,
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
	}

	got := o.Enrich(context.Background(), "7Q7EB")
	if got.CountrySource != lookup.SourceCountryTable {
		t.Fatalf("CountrySource = %q, want %q (precondition: this test exercises the cache-hit path)",
			got.CountrySource, lookup.SourceCountryTable)
	}
	if got.Country.LocalTime == "" {
		t.Fatal("LocalTime is empty on fresh-hit response — orchestrator must recompute from TimeOffset")
	}
	parsed, err := time.Parse(time.RFC3339, got.Country.LocalTime)
	if err != nil {
		t.Fatalf("LocalTime %q is not RFC 3339: %v", got.Country.LocalTime, err)
	}
	_, offsetSec := parsed.Zone()
	if want := int((2 * time.Hour).Seconds()); offsetSec != want {
		t.Errorf("LocalTime zone offset = %ds, want %ds (TimeOffset 2h 0m)", offsetSec, want)
	}
}

// TestEnrich_ColdMiss_LocalTimeRecomputed — cold-miss path also runs
// the recompute so the response carries the daemon's clock-shifted
// value, not the upstream provider's potentially-stale wire timestamp.
func TestEnrich_ColdMiss_LocalTimeRecomputed(t *testing.T) {
	db := newTestSqlite(t)
	hamnut := &stubCountryProvider{
		name: "hamnut",
		result: types.Country{
			Name:       "Malawi",
			Prefix:     "7Q",
			TimeOffset: "2h 0m",
			LocalTime:  "stale-upstream-string-orchestrator-must-replace",
		},
	}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
	}

	got := o.Enrich(context.Background(), "7Q7EB")
	if got.CountrySource != lookup.SourceHamnut {
		t.Fatalf("CountrySource = %q, want %q (precondition: cold-miss path)",
			got.CountrySource, lookup.SourceHamnut)
	}
	if got.Country.LocalTime == "stale-upstream-string-orchestrator-must-replace" {
		t.Fatal("orchestrator returned upstream LocalTime verbatim; must recompute from TimeOffset")
	}
	if got.Country.LocalTime == "" {
		t.Fatal("LocalTime is empty on cold-miss response with valid TimeOffset")
	}
	if _, err := time.Parse(time.RFC3339, got.Country.LocalTime); err != nil {
		t.Fatalf("LocalTime %q is not RFC 3339: %v", got.Country.LocalTime, err)
	}
}

// TestEnrich_NoTimeOffset_LocalTimeStaysEmpty — country row with no
// TimeOffset (e.g., legacy import, missing upstream field) must NOT
// fabricate a UTC LocalTime. Empty in → empty out.
func TestEnrich_NoTimeOffset_LocalTimeStaysEmpty(t *testing.T) {
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:   "Atlantis",
		Prefix: "ZZ",
		// TimeOffset deliberately empty.
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	o := &lookup.Orchestrator{DB: db, CountryTTL: time.Hour, StationTTL: time.Hour}

	got := o.Enrich(context.Background(), "ZZ1ABC")
	if got.Country.LocalTime != "" {
		t.Errorf("LocalTime = %q, want empty (no TimeOffset → no fabricated time)", got.Country.LocalTime)
	}
}

// ---- IsNewEntity ----

// TestEnrich_IsNewEntity_NoPriorQso — fresh log with no QSO for this
// country must report IsNewEntity=true so the SPA can show the
// "new DXCC" asterisk.
func TestEnrich_IsNewEntity_NoPriorQso(t *testing.T) {
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:   "Malawi",
		Prefix: "7Q",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	o := &lookup.Orchestrator{DB: db, CountryTTL: time.Hour, StationTTL: time.Hour}

	got := o.Enrich(context.Background(), "7Q7EB")
	if !got.Country.IsNewEntity {
		t.Errorf("IsNewEntity = false, want true (no prior QSO with country=Malawi)")
	}
}

// TestEnrich_IsNewEntity_WithPriorQso — once the operator has logged
// any QSO whose country matches, IsNewEntity flips to false.
func TestEnrich_IsNewEntity_WithPriorQso(t *testing.T) {
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:   "Malawi",
		Prefix: "7Q",
	}); err != nil {
		t.Fatalf("seed country: %v", err)
	}
	// Seed a logbook + a prior QSO with country=Malawi via raw insert
	// (the high-level Submit path needs a fully-formed types.Qso plus
	// service wiring; for this test we just need the row to exist).
	if _, err := db.InsertLogbookWithContext(context.Background(), types.Logbook{
		Name:     "Default",
		Callsign: "7Q5MLV",
	}); err != nil {
		t.Fatalf("seed logbook: %v", err)
	}
	if _, err := db.InsertQsoWithContext(context.Background(), types.Qso{
		LogbookID: 1,
		UUID:      utils.NewUUIDv7(),
		DedupeKey: strings.Repeat("a", 64),
		QsoDetails: types.QsoDetails{
			Band:    "20m",
			Mode:    "SSB",
			Freq:    "14250",
			QsoDate: "20260507",
			TimeOn:  "1200",
			TimeOff: "1205",
			RstSent: "59",
			RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{
			Call:    "7Q3ABC",
			Country: "Malawi",
		},
	}); err != nil {
		t.Fatalf("seed prior QSO: %v", err)
	}

	o := &lookup.Orchestrator{DB: db, CountryTTL: time.Hour, StationTTL: time.Hour}
	got := o.Enrich(context.Background(), "7Q7EB")
	if got.Country.IsNewEntity {
		t.Errorf("IsNewEntity = true after a prior QSO with same country; want false")
	}
}

// TestEnrich_IsNewEntity_EmptyCountry — defensive: orchestrator path
// where the country layer returned no data must NOT flag IsNewEntity
// (true would mislead — there is no entity to report newness for).
func TestEnrich_IsNewEntity_EmptyCountry(t *testing.T) {
	db := newTestSqlite(t)
	o := &lookup.Orchestrator{DB: db, CountryTTL: time.Hour, StationTTL: time.Hour}
	got := o.Enrich(context.Background(), "ZZ1ABC")
	if got.Country.IsNewEntity {
		t.Errorf("IsNewEntity = true with no country data; want false")
	}
}

// ----- EnrichRefresh: cache-bypass escape valve -----

// TestEnrichRefresh_BypassesFreshCache_CallsUpstreamAndOverwrites —
// the operator's "the cache is wrong" path. A fresh country + station
// row exists; EnrichRefresh must hit hamnut + the chain anyway, and
// the cache rows must end up with the new upstream values.
func TestEnrichRefresh_BypassesFreshCache_CallsUpstreamAndOverwrites(t *testing.T) {
	db := newTestSqlite(t)
	// Pre-seed a fresh country row with stale-looking values; the
	// upstream stub returns DIFFERENT values so we can verify the
	// overwrite landed.
	if err := db.UpsertCountry(types.Country{
		Name:   "OldName",
		Prefix: "M",
		CQZone: "0",
	}); err != nil {
		t.Fatalf("seed country: %v", err)
	}
	if err := db.UpsertContactedStation(types.ContactedStation{
		Call: "M0CMC",
		Name: "OldName",
		QTH:  "OldQTH",
	}); err != nil {
		t.Fatalf("seed station: %v", err)
	}

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
		},
	}
	ref := &recordingRefresher{}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour, // pre-seed is fresh — would be a cache hit on regular Enrich
		StationTTL: time.Hour,
		Refresher:  ref,
	}

	got := o.EnrichRefresh(context.Background(), "M0CMC")

	if hamnut.calls != 1 {
		t.Errorf("hamnut calls = %d, want 1 (force-refresh must hit upstream)", hamnut.calls)
	}
	if qrz.calls != 1 {
		t.Errorf("qrz calls = %d, want 1 (force-refresh must hit upstream)", qrz.calls)
	}
	if got.CountrySource != lookup.SourceHamnut {
		t.Errorf("CountrySource = %q, want %q (force-refresh treats success as cold-miss)",
			got.CountrySource, lookup.SourceHamnut)
	}
	if got.StationSource != "qrz" {
		t.Errorf("StationSource = %q, want qrz", got.StationSource)
	}
	if got.Country.Name != "England" {
		t.Errorf("Country.Name = %q, want England (overwrite from upstream)", got.Country.Name)
	}
	if got.Station.Name != "Marc Veary" {
		t.Errorf("Station.Name = %q, want Marc Veary (overwrite from upstream)", got.Station.Name)
	}

	// Cache must now reflect the new upstream values. Read back via
	// FetchCountryByCallsign / FetchContactedStationByCallsign — same
	// path the next regular Enrich would use.
	cachedCountry, err := db.FetchCountryByCallsign("M0CMC")
	if err != nil {
		t.Fatalf("re-read country: %v", err)
	}
	if cachedCountry.Name != "England" {
		t.Errorf("cached country Name = %q after refresh, want England", cachedCountry.Name)
	}
	cachedStation, err := db.FetchContactedStationByCallsign("M0CMC")
	if err != nil {
		t.Fatalf("re-read station: %v", err)
	}
	if cachedStation.Name != "Marc Veary" {
		t.Errorf("cached station Name = %q after refresh, want Marc Veary", cachedStation.Name)
	}

	if ref.scheduled != 0 {
		t.Errorf("refresher fired %d times on force-refresh; should be 0 (synchronous work)", ref.scheduled)
	}
}

// TestEnrichRefresh_UpstreamDown_ReturnsNoneWithoutFallback — the
// operator asked for fresh data; if upstream can't deliver, returning
// the cached row silently would defeat the purpose. Both layers
// must surface source=none rather than the seeded values.
func TestEnrichRefresh_UpstreamDown_ReturnsNoneWithoutFallback(t *testing.T) {
	db := newTestSqlite(t)
	if err := db.UpsertCountry(types.Country{
		Name:   "England",
		Prefix: "M",
	}); err != nil {
		t.Fatalf("seed country: %v", err)
	}
	if err := db.UpsertContactedStation(types.ContactedStation{
		Call: "M0CMC",
		Name: "Cached",
	}); err != nil {
		t.Fatalf("seed station: %v", err)
	}

	hamnut := &stubCountryProvider{name: "hamnut", err: stderrErrTransport()}
	qrz := &stubCallsignProvider{name: "qrz", err: stderrErrTransport()}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &recordingRefresher{},
	}

	got := o.EnrichRefresh(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceNone {
		t.Errorf("CountrySource = %q, want %q (upstream-down on force-refresh = none, not fallback to cache)",
			got.CountrySource, lookup.SourceNone)
	}
	if got.StationSource != lookup.SourceNone {
		t.Errorf("StationSource = %q, want %q", got.StationSource, lookup.SourceNone)
	}
	if got.Country.Name == "England" {
		t.Errorf("Country.Name leaked through from cache on force-refresh-with-upstream-down")
	}
	if got.Station.Name == "Cached" {
		t.Errorf("Station.Name leaked through from cache on force-refresh-with-upstream-down")
	}
}

// TestEnrichRefresh_NoCacheRow_BehavesLikeColdMiss — calling
// EnrichRefresh on a callsign that was never enriched works the
// same as a regular cold-miss Enrich: hits upstream, writes back.
// This is the trivial case but worth pinning so a future
// readCountry/readStation refactor can't accidentally short-circuit
// the no-row path under force.
func TestEnrichRefresh_NoCacheRow_BehavesLikeColdMiss(t *testing.T) {
	db := newTestSqlite(t)
	hamnut := &stubCountryProvider{
		name: "hamnut",
		result: types.Country{
			Name:   "England",
			Prefix: "M",
		},
	}
	qrz := &stubCallsignProvider{
		name: "qrz",
		result: types.ContactedStation{
			Call: "M0CMC",
			Name: "Marc Veary",
		},
	}
	o := &lookup.Orchestrator{
		DB:         db,
		Country:    hamnut,
		Chain:      []lookup.CallsignProvider{qrz},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &recordingRefresher{},
	}

	got := o.EnrichRefresh(context.Background(), "M0CMC")
	if got.CountrySource != lookup.SourceHamnut {
		t.Errorf("CountrySource = %q, want %q", got.CountrySource, lookup.SourceHamnut)
	}
	if got.StationSource != "qrz" {
		t.Errorf("StationSource = %q, want qrz", got.StationSource)
	}
	if hamnut.calls != 1 || qrz.calls != 1 {
		t.Errorf("expected 1 call to each upstream; hamnut=%d qrz=%d", hamnut.calls, qrz.calls)
	}
}

// blockingCallsignProvider ignores ctx and blocks in LookupWithContext until
// released — models a misbehaving provider for the cancellation test.
type blockingCallsignProvider struct{ release chan struct{} }

func (b *blockingCallsignProvider) Name() string                       { return "blocking" }
func (b *blockingCallsignProvider) Initialize(_ context.Context) error { return nil }
func (b *blockingCallsignProvider) Lookup(_ string) (types.ContactedStation, error) {
	return types.ContactedStation{}, nil
}
func (b *blockingCallsignProvider) LookupWithContext(_ context.Context, _ string) (types.ContactedStation, error) {
	<-b.release // deliberately ignores ctx
	return types.ContactedStation{}, nil
}

// TestEnrich_ReturnsPromptlyOnContextCancel guards review 2026-06-19 M3: if a
// provider ignores ctx and blocks, Enrich must still return when the request is
// cancelled rather than hanging the handler. The blocked goroutine finishes
// later into the buffered channel (released at test end) — no leak.
func TestEnrich_ReturnsPromptlyOnContextCancel(t *testing.T) {
	db := newTestSqlite(t)
	blocking := &blockingCallsignProvider{release: make(chan struct{})}
	defer close(blocking.release)

	o := &lookup.Orchestrator{
		DB:         db,
		Country:    &stubCountryProvider{name: "hamnut"}, // returns fast → country layer doesn't block
		Chain:      []lookup.CallsignProvider{blocking},  // station layer blocks, ignoring ctx
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan lookup.Result, 1)
	go func() { done <- o.Enrich(ctx, "m0cmc") }()
	cancel()

	select {
	case <-done:
		// Returned promptly after cancellation — correct.
	case <-time.After(2 * time.Second):
		t.Fatal("Enrich did not return after context cancellation; it blocked on a ctx-ignoring provider")
	}
}
