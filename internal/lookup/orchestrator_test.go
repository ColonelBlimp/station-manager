package lookup_test

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	sqlsvc "github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
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

func (s *stubCountryProvider) Name() string      { return s.name }
func (s *stubCountryProvider) Initialize() error { return nil }
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

func (s *stubCallsignProvider) Name() string      { return s.name }
func (s *stubCallsignProvider) Initialize() error { return nil }
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
		// country/cqz/ituz/dxcc don't count — orchestrator filters them.
		{"only country (gets stripped)", types.ContactedStation{Country: "Malawi"}, true},
		{"only cqz (gets stripped)", types.ContactedStation{CQZ: "37"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lookup.IsEmpty(c.in); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
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
	if got.Station.DXCC != "G" {
		t.Errorf("Station.DXCC = %q, want G", got.Station.DXCC)
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
			types.ContactedStation{Name: "Marc", Country: "England", CQZ: "14", ITUZ: "27", Cont: "EU", DXCC: "G"},
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
