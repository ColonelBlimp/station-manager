package lookup

import (
	"context"
	stderrs "errors"
	"strings"
	"time"

	sqlsvc "github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Source values for the Result's per-layer source indicators (per
// ADR 0017 #12). Callsign-class results carry the provider's Name()
// directly (e.g., "qrz", "hamqth"); the country layer has a fixed
// pair of constants because no provider object exists for the
// cache-hit case.
const (
	// SourceNone — no data was available; either no row in the cache
	// AND no provider returned data, or the provider chain was empty
	// / disabled.
	SourceNone = "none"
	// SourceCountryTable — the country fields came from the local
	// country table (read-side cache hit, fresh or stale).
	SourceCountryTable = "country_table"
	// SourceHamnut — the country fields came from a hamnut call (cold
	// miss path).
	SourceHamnut = "hamnut"
	// SourceContactedTable — the station fields came from the local
	// contacted_station table (read-side cache hit).
	SourceContactedTable = "contacted_station"
)

// AsyncRefresher schedules background work — the stale-hit branch of
// the orchestrator's three-state read policy uses it to fire a
// refresh after returning the stale row to the caller. The fn is
// passed a context that the implementation manages (typically tied
// to the daemon's lifetime so shutdown cancels in-flight refreshes).
//
// Task #61 ships the bounded production implementation. Tests can
// pass a synchronous stub that runs fn immediately.
type AsyncRefresher interface {
	Schedule(fn func(ctx context.Context))
}

// Result is the orchestrator's response to the HTTP handler. Always
// returned (never error) per ADR 0017 #12 — failures fall through to
// "empty fields, source=none" rather than non-2xx.
type Result struct {
	Callsign      string                 `json:"callsign"`
	Country       types.Country          `json:"country,omitempty"`
	Station       types.ContactedStation `json:"station,omitempty"`
	CountrySource string                 `json:"country_source"`
	StationSource string                 `json:"station_source"`
}

// Orchestrator implements the ADR 0017 read pipeline. Single entry
// point (Enrich) plus internal helpers that branch on the three
// cache states (cold / stale / fresh) and merge the country and
// station layers before return / write-back.
//
// Construction shape: pass concrete dependencies in. The chain slice
// is filtered to enabled providers by the caller (task #62 wires
// this from operator config); the orchestrator itself doesn't read
// the per-provider Enabled flag at runtime.
type Orchestrator struct {
	DB         *sqlsvc.Service
	Country    CountryProvider
	Chain      []CallsignProvider
	CountryTTL time.Duration
	StationTTL time.Duration
	Refresher  AsyncRefresher
	Logger     *logging.Service
}

// IsEmpty returns true when a callsign-class provider response had
// no substantive data — Call alone (the lookup input echoed back)
// doesn't count as data, and country / CQ / ITU / DXCC fields are
// hamnut-exclusive (filtered out by FilterToCallsignFields), so they
// don't count either. The chain runner uses this to decide whether
// to advance to the next provider per ADR 0017 #8.
func IsEmpty(cs types.ContactedStation) bool {
	return strings.TrimSpace(cs.Name) == "" &&
		strings.TrimSpace(cs.QTH) == "" &&
		strings.TrimSpace(cs.Gridsquare) == "" &&
		strings.TrimSpace(cs.Email) == "" &&
		strings.TrimSpace(cs.Web) == "" &&
		strings.TrimSpace(cs.Address) == "" &&
		strings.TrimSpace(cs.ContactedOp) == ""
}

// MergeStationFromCountry copies the hamnut-truth country fields
// from c onto s, but only when the source value is non-empty AND
// differs from s's existing value.
//
// "Only when different" matters in two scenarios:
//
//   - Avoids a no-op overwrite when s already has the same denormalized
//     country values from a previous merge (saves a `modified_at`
//     bump that would record a non-change).
//   - Pairs with FilterToCallsignFields to make the country fields on
//     the station deterministic at the orchestrator boundary: filter
//     zeroes the QRZ-bug values, then this merge fills in hamnut's
//     truth — never the upstream's wrong values.
//
// Caller-side discipline: this should run AFTER FilterToCallsignFields
// so any QRZ-bug country/CQ/ITU/DXCC values are cleared first; the
// merge then fills the empty fields with hamnut data. If the country
// argument has no data (hamnut down + no cached row), the station's
// country fields stay empty rather than retaining QRZ-bug values.
func MergeStationFromCountry(s types.ContactedStation, c types.Country) types.ContactedStation {
	if c.Name != "" && c.Name != s.Country {
		s.Country = c.Name
	}
	if c.Continent != "" && c.Continent != s.Cont {
		s.Cont = c.Continent
	}
	if c.CQZone != "" && c.CQZone != s.CQZ {
		s.CQZ = c.CQZone
	}
	if c.ITUZone != "" && c.ITUZone != s.ITUZ {
		s.ITUZ = c.ITUZone
	}
	if c.DXCCPrefix != "" && c.DXCCPrefix != s.DXCC {
		s.DXCC = c.DXCCPrefix
	}
	return s
}

// Enrich runs the read pipeline for callsign and returns aggregated
// country + station data. Per ADR 0017 #12, this method does not
// return an error — transient failures are logged and folded into
// "empty fields, source=none."
//
// Pipeline (per ADR 0017 + the design refinement of 2026-05-07):
//
//  1. Read both layers concurrently. Each layer's cold-miss path
//     blocks on its respective upstream (hamnut for country, chain
//     for station); stale and fresh hits return immediately from
//     the cache.
//  2. Filter QRZ-bug country fields off the station result.
//  3. Merge hamnut country fields into the station (only when
//     different). After this step, the station's country / CQ / ITU
//     / DXCC fields carry hamnut's truth (or stay empty if no
//     country source had data).
//  4. Write back synchronously for cold misses — country to the
//     country table, merged station to contacted_station.
//  5. Schedule async refreshes for stale hits (country: hamnut
//     refresh + write; station: chain re-run + country read + merge
//     + write).
//  6. Return the merged Result.
//
// The merge runs on every path — fresh hit, stale hit, cold miss —
// so the SPA always receives the same shape regardless of which
// cache state each layer landed in.
func (o *Orchestrator) Enrich(ctx context.Context, callsign string) Result {
	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return Result{Callsign: callsign, CountrySource: SourceNone, StationSource: SourceNone}
	}

	cCh := make(chan countryReadResult, 1)
	sCh := make(chan stationReadResult, 1)
	go func() { cCh <- o.readCountry(ctx, callsign) }()
	go func() { sCh <- o.readStation(ctx, callsign) }()
	c := <-cCh
	s := <-sCh

	// Strip QRZ-bug country fields off the station, then re-populate
	// from hamnut's truth (if available). After these two steps the
	// station carries either hamnut's country values or empty —
	// never the upstream's wrong values.
	s.data = FilterToCallsignFields(s.data)
	s.data = MergeStationFromCountry(s.data, c.data)
	if !IsEmpty(s.data) && s.data.Call == "" {
		s.data.Call = strings.ToUpper(callsign)
	}

	// Synchronous write-backs for cold misses. Per ADR 0017 #6, cold
	// miss is the path that pays the upstream-call latency AND the
	// write back; stale and fresh hits don't write here.
	if c.coldMiss && c.data.Prefix != "" {
		if werr := o.DB.UpsertCountryWithContext(ctx, c.data); werr != nil {
			o.warn("country upsert failed", werr)
		}
	}
	if s.coldMiss && !IsEmpty(s.data) {
		if werr := o.DB.UpsertContactedStationWithContext(ctx, s.data); werr != nil {
			o.warn("contacted_station upsert failed", werr)
		}
	}

	// Async refreshes for stale hits. Country refresh runs hamnut
	// and writes the country table. Station refresh runs the chain,
	// reads country (cached value at refresh time), merges, and
	// writes contacted_station — keeping the denormalized country
	// fields aligned with hamnut's current truth.
	if c.staleHit {
		o.scheduleCountryRefresh(callsign)
	}
	if s.staleHit {
		o.scheduleStationRefresh(callsign)
	}

	return Result{
		Callsign:      callsign,
		Country:       c.data,
		Station:       s.data,
		CountrySource: c.source,
		StationSource: s.source,
	}
}

// countryReadResult carries the country layer's read outcome plus
// the cache-state flags Enrich uses to drive write-back / async-
// refresh decisions.
type countryReadResult struct {
	data     types.Country
	source   string
	coldMiss bool // we just blocked on hamnut and got data
	staleHit bool // cached but past TTL
}

// stationReadResult is the symmetric result type for the station layer.
type stationReadResult struct {
	data     types.ContactedStation
	source   string
	coldMiss bool
	staleHit bool
}

// readCountry runs the read half of the country layer — cache lookup
// (longest-prefix match) and, on cold miss, a synchronous hamnut
// call. Does NOT write back; that's Enrich's responsibility.
//
// Branches:
//
//	fresh hit  → return cached, no flags set
//	stale hit  → return cached, staleHit=true
//	cold miss  → block on hamnut; on success return + coldMiss=true
//	cold miss  + hamnut down/disabled → return empty, source=none
func (o *Orchestrator) readCountry(ctx context.Context, callsign string) countryReadResult {
	cached, err := o.DB.FetchCountryByCallsignWithContext(ctx, callsign)
	if err == nil {
		if !o.isStale(cached.LastRefreshedAt, o.CountryTTL) {
			return countryReadResult{data: cached, source: SourceCountryTable}
		}
		return countryReadResult{data: cached, source: SourceCountryTable, staleHit: true}
	}
	if !stderrs.Is(err, errors.ErrNotFound) {
		o.warn("country fetch failed", err)
		return countryReadResult{source: SourceNone}
	}

	if o.Country == nil {
		return countryReadResult{source: SourceNone}
	}
	res, lerr := o.Country.LookupWithContext(ctx, callsign)
	if lerr != nil {
		// ErrNotFound + transport failures both fall here. Per ADR
		// 0017 #9, no row is written.
		return countryReadResult{source: SourceNone}
	}
	if res.Name == "Unknown" {
		// Hamnut's "disabled" sentinel — treat as no-data so the
		// SPA's countrySource reads "none" and we don't cache the
		// sentinel.
		return countryReadResult{source: SourceNone}
	}
	return countryReadResult{data: res, source: SourceHamnut, coldMiss: true}
}

// readStation runs the read half of the station layer — cache lookup
// (callsign-keyed) and, on cold miss, the synchronous chain runner.
// Does NOT filter or merge or write — Enrich orchestrates those.
func (o *Orchestrator) readStation(ctx context.Context, callsign string) stationReadResult {
	cached, err := o.DB.FetchContactedStationByCallsignWithContext(ctx, callsign)
	if err == nil {
		if !o.isStale(cached.LastRefreshedAt, o.StationTTL) {
			return stationReadResult{data: cached, source: SourceContactedTable}
		}
		return stationReadResult{data: cached, source: SourceContactedTable, staleHit: true}
	}
	if !stderrs.Is(err, errors.ErrNotFound) {
		o.warn("contacted_station fetch failed", err)
		return stationReadResult{source: SourceNone}
	}

	station, source := o.runChain(ctx, callsign)
	if source == SourceNone {
		return stationReadResult{source: SourceNone}
	}
	return stationReadResult{data: station, source: source, coldMiss: true}
}

// runChain iterates the configured callsign-class providers in
// priority order and returns the first non-empty result per ADR
// 0017 #8. Empty responses and errors both advance to the next
// provider; the chain runner does not distinguish them at the
// dispatch layer.
//
// Returns the provider's Name() as the source on success, or
// SourceNone if every provider returned empty / error / nothing-to-
// run.
func (o *Orchestrator) runChain(ctx context.Context, callsign string) (types.ContactedStation, string) {
	for _, p := range o.Chain {
		if p == nil {
			continue
		}
		station, err := p.LookupWithContext(ctx, callsign)
		if err != nil {
			if !stderrs.Is(err, errors.ErrNotFound) {
				o.warn("callsign provider error: "+p.Name(), err)
			}
			continue
		}
		if IsEmpty(station) {
			continue
		}
		return station, p.Name()
	}
	return types.ContactedStation{}, SourceNone
}

// isStale returns true when last is the zero time (NULL in DB →
// never refreshed) or older than now - ttl. TTL <= 0 is interpreted
// as "trust the cache indefinitely" (defensive against config typos
// and gives the operator an explicit knob to disable staleness).
func (o *Orchestrator) isStale(last time.Time, ttl time.Duration) bool {
	if last.IsZero() {
		return true
	}
	if ttl <= 0 {
		return false
	}
	return time.Since(last) > ttl
}

// scheduleCountryRefresh fires an async hamnut call + write-back.
// The caller (Enrich) returns the stale row to the operator
// immediately; this refresh updates the row for the next Tab. Does
// not cascade to contacted_station rows — those refresh via their
// own staleness path.
func (o *Orchestrator) scheduleCountryRefresh(callsign string) {
	if o.Refresher == nil || o.Country == nil {
		return
	}
	o.Refresher.Schedule(func(ctx context.Context) {
		res, err := o.Country.LookupWithContext(ctx, callsign)
		if err != nil {
			// Fail-quiet: stale row stays in DB, next Tab tries
			// again. ADR 0017 #7 implicit fall-through.
			return
		}
		if res.Name == "Unknown" || res.Prefix == "" {
			return
		}
		if werr := o.DB.UpsertCountryWithContext(ctx, res); werr != nil {
			o.warn("async country upsert failed", werr)
		}
	})
}

// scheduleStationRefresh fires an async chain re-run + country read
// + merge + write. The merge keeps the contacted_station's
// denormalized country fields aligned with hamnut's current truth
// at the time the refresh fires (which may differ from the cached
// country values used during Enrich's synchronous return).
func (o *Orchestrator) scheduleStationRefresh(callsign string) {
	if o.Refresher == nil || len(o.Chain) == 0 {
		return
	}
	o.Refresher.Schedule(func(ctx context.Context) {
		station, source := o.runChain(ctx, callsign)
		if source == SourceNone {
			return
		}
		station = FilterToCallsignFields(station)

		// Read the country (cached value at refresh time). We don't
		// trigger a hamnut call here — country has its own staleness
		// path; the station refresh just consumes whatever's
		// currently cached.
		country, ferr := o.DB.FetchCountryByCallsignWithContext(ctx, callsign)
		if ferr != nil && !stderrs.Is(ferr, errors.ErrNotFound) {
			o.warn("async country fetch (during station refresh) failed", ferr)
			// Fall through with empty country — merge becomes a no-op,
			// station's country fields stay empty.
		}
		station = MergeStationFromCountry(station, country)
		if station.Call == "" {
			station.Call = strings.ToUpper(callsign)
		}

		if werr := o.DB.UpsertContactedStationWithContext(ctx, station); werr != nil {
			o.warn("async contacted_station upsert failed", werr)
		}
	})
}

// warn is a small wrapper that no-ops when LoggerService is nil so
// the orchestrator stays usable in tests that don't wire a logger.
func (o *Orchestrator) warn(msg string, err error) {
	if o.Logger == nil {
		return
	}
	o.Logger.WarnWith().Err(err).Msg(msg)
}
