package lookup

import (
	"context"
	"encoding/json"
	stderrs "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	sqlsvc "github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/dxcc"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Source values for the Result's per-layer source indicators (per
// ADR 0017 #12). Callsign-class results carry the provider's Name()
// directly — currently the DI service name (e.g. "qrzlookupservice",
// review 2026-06-04 L2) — or SourceContactedTable on a station cache
// hit; the country layer uses the fixed constants below because no
// provider object exists for the cache-hit case.
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

// MarshalJSON omits the country / station layers entirely when their source is
// SourceNone. Go's encoding/json does not treat a zero-value struct as empty,
// so the value-typed fields above would otherwise serialize as `"country":{}`
// / `"station":{}` on a no-data result — contradicting the SPA contract that a
// failed/absent layer is "source=none with no object" (review 2026-06-04 H2).
// A present layer (cache hit or provider hit) serializes as before, including
// its last_refreshed_at.
func (r Result) MarshalJSON() ([]byte, error) {
	type wire struct {
		Callsign      string                  `json:"callsign"`
		Country       *types.Country          `json:"country,omitempty"`
		Station       *types.ContactedStation `json:"station,omitempty"`
		CountrySource string                  `json:"country_source"`
		StationSource string                  `json:"station_source"`
	}
	w := wire{
		Callsign:      r.Callsign,
		CountrySource: r.CountrySource,
		StationSource: r.StationSource,
	}
	if r.CountrySource != SourceNone {
		c := r.Country
		w.Country = &c
	}
	if r.StationSource != SourceNone {
		st := r.Station
		w.Station = &st
	}
	return json.Marshal(w)
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
	// DB is the enrichment-cache connection (reference.db): country +
	// contacted_station reads/writes.
	DB *sqlsvc.Service
	// LogDB is the log connection, used only for the new-entity check
	// (HasQsoForDxcc / HasQsoForCountry query the qso table). Optional: nil in
	// single-connection mode (tests, pre-split), where logCheckDB falls back to
	// DB. new-entity is presentation, never load-bearing for logging.
	LogDB   *sqlsvc.Service
	Country CountryProvider
	Chain   []CallsignProvider
	// ContinueIfBlank is ADR 0068's chain-wide completion gate. Empty retains
	// ADR 0017's first-substantive-result behaviour for direct/test callers;
	// production config normalises it to name + gridsquare.
	ContinueIfBlank []string
	CountryTTL      time.Duration
	StationTTL      time.Duration
	Refresher       AsyncRefresher
	Logger          *logging.Service
}

// IsEmpty returns true when a callsign-class provider response had no
// substantive station data. It tests every station-provider-owned field, and
// deliberately ignores three groups:
//
//   - Call — the lookup input echoed back, not data;
//   - Country / Cont / CQZ / ITUZ / DXCC — hamnut-exclusive, stripped by
//     FilterToCallsignFields, so they never count as station data;
//   - CSID / LastRefreshedAt — storage metadata, not provider data.
//
// The chain runner uses this to decide whether to advance to the next provider
// per ADR 0017 #8. The field set was widened (review 2026-06-04 L1) beyond the
// original name/QTH/grid/email/web/address/contacted-op so a future provider
// that returns only e.g. lat/lon, IOTA, or a WWFF reference isn't mistaken for
// an empty response.
func IsEmpty(cs types.ContactedStation) bool {
	return strings.TrimSpace(cs.Name) == "" &&
		strings.TrimSpace(cs.QTH) == "" &&
		strings.TrimSpace(cs.Gridsquare) == "" &&
		strings.TrimSpace(cs.Email) == "" &&
		strings.TrimSpace(cs.Web) == "" &&
		strings.TrimSpace(cs.Address) == "" &&
		strings.TrimSpace(cs.ContactedOp) == "" &&
		strings.TrimSpace(cs.Age) == "" &&
		strings.TrimSpace(cs.Altitude) == "" &&
		strings.TrimSpace(cs.EqCall) == "" &&
		strings.TrimSpace(cs.Iota) == "" &&
		strings.TrimSpace(cs.IotaIslandId) == "" &&
		strings.TrimSpace(cs.Lat) == "" &&
		strings.TrimSpace(cs.Lon) == "" &&
		strings.TrimSpace(cs.Sig) == "" &&
		strings.TrimSpace(cs.SigInfo) == "" &&
		strings.TrimSpace(cs.WwffRef) == ""
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
	// Populate the ADIF DXCC field (ContactedStation.DXCC) — the numeric DXCC
	// *entity code* (e.g. "291" for the USA) — from the country's alphabetic
	// prefix via the curated enums/dxcc map. SM computes and stores it rather
	// than leaving it empty for an uploader to backfill: some services don't,
	// and an operator who uses no online services would otherwise never get a
	// DXCC. The alphabetic prefix ("K"/"VK") is NEVER written into this numeric
	// field — that would make QRZ file the QSO as NON-DXCC (the isAsciiDigits
	// guard in the ADIF emitter is the belt-and-suspenders backstop); an
	// unmapped prefix leaves DXCC empty (graceful degradation). Only fill when
	// empty so a genuine numeric DXCC from an upstream is never overwritten.
	if s.DXCC == "" {
		if code, ok := dxcc.DXCCForPrefix(c.DXCCPrefix); ok {
			s.DXCC = code
		}
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
	return o.enrich(ctx, callsign, false)
}

// EnrichRefresh runs the read pipeline with the cache bypassed —
// the operator's escape valve for "the cached row is wrong." Skips
// the cache-fetch in both layers, goes straight to upstream (hamnut
// for country, chain for station), and writes back on success
// (overwriting any existing row). On upstream failure the relevant
// layer returns source=none rather than falling back to the cached
// row — the operator asked for fresh data, returning stale data
// silently would defeat the purpose. Async stale-refresh scheduling
// is suppressed (we just did the work synchronously).
func (o *Orchestrator) EnrichRefresh(ctx context.Context, callsign string) Result {
	return o.enrich(ctx, callsign, true)
}

func (o *Orchestrator) enrich(ctx context.Context, callsign string, force bool) Result {
	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return Result{Callsign: callsign, CountrySource: SourceNone, StationSource: SourceNone}
	}
	// Canonicalise at the package boundary (review 2026-06-04 L3): callsigns
	// are upper-case ASCII, the station-cache fetch is an exact-equality
	// lookup, and the upsert keys off the incoming case — so a direct
	// (non-HTTP) caller passing lower/mixed case must not miss or duplicate the
	// canonical row. The HTTP handler already uppercases; doing it here makes
	// the guarantee hold for every caller.
	callsign = strings.ToUpper(callsign)

	// The two read paths run concurrently per ADR 0017 #5. Both
	// goroutines route through safego.Go so a panic in readCountry /
	// readStation (or anything they call — DB row mapper, upstream
	// HTTP body parser, future logger field) is recovered and logged
	// instead of crashing the daemon. The api `recoverPanic`
	// middleware doesn't reach here — it only wraps the request
	// goroutine, not its children.
	//
	// Each goroutine pre-declares its outcome variable and uses a
	// deferred channel send so a panicking readCountry / readStation
	// still produces a zero-value result on its channel (sources will
	// read as "" and the merge below treats them as "no data"). That
	// keeps Enrich's `<-cCh` / `<-sCh` from deadlocking when safego's
	// recovered panic skips the inline send.
	cCh := make(chan countryReadResult, 1)
	sCh := make(chan stationReadResult, 1)
	safego.Go(ctx, "lookup.enrich.country", o.onPanic, func() {
		var out countryReadResult
		defer func() { cCh <- out }()
		out = o.readCountry(ctx, callsign, force)
	}, false /* respawn — Enrich is request-scoped, one-shot */)
	safego.Go(ctx, "lookup.enrich.station", o.onPanic, func() {
		var out stationReadResult
		defer func() { sCh <- out }()
		out = o.readStation(ctx, callsign, force)
	}, false)
	// Wait for both layers, but bail out on request cancellation rather than
	// blocking forever if a provider/DB call ignores ctx (review 2026-06-19 M3).
	// The goroutines finish later into the buffered (cap-1) channels, so there's
	// no leak; the SourceNone zero-values make every conditional below (coldMiss,
	// staleHit, write-backs, new-entity query) a no-op, so a cancelled Enrich
	// returns promptly with empty data instead of hanging the handler.
	var c countryReadResult
	var s stationReadResult
	select {
	case c = <-cCh:
	case <-ctx.Done():
		c = countryReadResult{source: SourceNone}
	}
	select {
	case s = <-sCh:
	case <-ctx.Done():
		s = stationReadResult{source: SourceNone}
	}

	// On panic, the source field on the recovered outcome is the zero
	// value (""), not SourceNone. Normalise so downstream consumers
	// (the SPA's countrySource / stationSource indicators) always see
	// one of the documented enum values.
	if c.source == "" {
		c.source = SourceNone
	}
	if s.source == "" {
		s.source = SourceNone
	}

	// Strip QRZ-bug country fields off the station, then re-populate
	// from hamnut's truth (if available). After these two steps the
	// station carries either hamnut's country values or empty —
	// never the upstream's wrong values.
	s.data = o.normalizeProviderStation(callsign, s.data)
	s.data = MergeStationFromCountry(s.data, c.data)
	if !IsEmpty(s.data) && s.data.Call == "" {
		s.data.Call = callsign // already upper-cased at the boundary (L3)
	}

	// Synchronous write-backs for cold misses. Per ADR 0017 #6, cold
	// miss is the path that pays the upstream-call latency AND the
	// write back; stale and fresh hits don't write here. `now` is also
	// reflected onto the returned data so a cold-miss response carries
	// last_refreshed_at, matching the cache-hit path (review 2026-06-04 H2).
	// The o.DB != nil guards on the synchronous write-back + new-entity paths
	// keep a miswired/test Orchestrator (nil DB) from panicking here — the async
	// readCountry/readStation already run under safego, but these run inline
	// (review 2026-06-19 L1). Production wiring in cmd/smd always supplies DB.
	// One-char prefixes are skipped, not attempted-and-warned: hamnut returns
	// them for whole ITU blocks (G/M/U/R…) that span multiple DXCC entities,
	// so they can never be cached (sqlite.IsCacheableCountryPrefix) — and
	// calls in those blocks are common enough that a warn per cold miss would
	// be steady-state log noise. The result still reaches the caller; only
	// the cache row is forgone.
	now := time.Now()
	if c.coldMiss && c.data.Prefix != "" && o.DB != nil {
		if sqlsvc.IsCacheableCountryPrefix(c.data.Prefix) {
			if werr := o.DB.UpsertCountryWithContext(ctx, c.data); werr != nil {
				o.warn("country upsert failed", werr)
			}
		}
		// Stamped even when the write is skipped — the response's freshness
		// timestamp matches the cache-hit shape either way (H2).
		c.data.LastRefreshedAt = now
	}
	if s.coldMiss && !IsEmpty(s.data) && o.DB != nil {
		// Replace, not merge: a provider refresh (force-refresh, or the cold
		// miss itself) is authoritative, so a field that's empty upstream must
		// clear in the cache rather than retain a stale prior value (H1).
		if werr := o.DB.ReplaceContactedStationWithContext(ctx, s.data); werr != nil {
			o.warn("contacted_station replace failed", werr)
		}
		s.data.LastRefreshedAt = now
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

	// Recompute LocalTime at the boundary so every return path carries
	// the daemon's current wall-clock-shifted-by-offset value. Without
	// this, cold-miss responses retained hamnut's wire-time string
	// (potentially seconds stale) and cache hits returned empty —
	// neither is what the SPA needs to display "what time is it
	// where the contacted station lives, right now." TimeOffset is
	// the persisted source of truth; LocalTime is presentation.
	c.data = applyLocalTime(c.data, now)

	// IsNewEntity = "operator has never logged a QSO with this DXCC entity
	// before." Preferred match is the numeric ADIF DXCC code: hamnut's
	// primaryDXCCPrefix maps to the code via enums/dxcc, and the qso table is
	// queried for any non-deleted row carrying that code. The code is the right
	// key because it distinguishes split entities the display name conflates
	// (European vs Asiatic Russia) and survives the naming gap between hamnut
	// ("Fed. Rep. of Germany") and an imported QSO's country field ("Germany").
	// When the prefix isn't in the table (or hamnut gave none), fall back to the
	// country-name match so partial table coverage degrades gracefully rather
	// than silently flagging everything new. A failed lookup leaves the flag at
	// its zero value (false) and logs a warn rather than failing Enrich —
	// new-entity is presentation, never load-bearing for logging.
	if logDB := o.logCheckDB(); logDB != nil {
		if code, ok := dxcc.DXCCForPrefix(c.data.DXCCPrefix); ok {
			exists, hErr := logDB.HasQsoForDxccWithContext(ctx, code)
			if hErr != nil {
				o.warn("new-entity check (dxcc) failed", hErr)
			} else {
				c.data.IsNewEntity = !exists
			}
		} else if c.data.Name != "" {
			exists, hErr := logDB.HasQsoForCountryWithContext(ctx, c.data.Name)
			if hErr != nil {
				o.warn("new-entity check (country) failed", hErr)
			} else {
				c.data.IsNewEntity = !exists
			}
		}
	}

	return Result{
		Callsign:      callsign,
		Country:       c.data,
		Station:       s.data,
		CountrySource: c.source,
		StationSource: s.source,
	}
}

// applyLocalTime overwrites c.LocalTime with `now` shifted by
// c.TimeOffset, formatted as RFC 3339. Returns c unchanged when
// TimeOffset is empty or unparseable so a malformed cache row doesn't
// fabricate a misleading time.
//
// Centralising the derivation here keeps the response shape uniform
// across cache states and lets the persisted column carry only the
// stable fact (offset) rather than a timestamp that's stale the
// instant it lands in the row.
func applyLocalTime(c types.Country, now time.Time) types.Country {
	if c.TimeOffset == "" {
		return c
	}
	d, ok := parseOffsetDuration(c.TimeOffset)
	if !ok {
		return c
	}
	loc := time.FixedZone("offset", int(d.Round(time.Second).Seconds()))
	c.LocalTime = now.In(loc).Format(time.RFC3339)
	return c
}

// parseOffsetDuration accepts the two formats hamnut emits for
// TimeOffset and returns a signed time.Duration:
//
//	"2h 0m"   / "-5h 30m" — Go-duration-shaped after stripping spaces.
//	"+02:00"  / "-08:00"  — RFC 3339 zone format.
//
// Returns (0, false) for empty input, unrecognised formats, or any
// parse failure. The caller must treat false as "leave LocalTime
// untouched" rather than substituting UTC — an unparseable offset is
// a data-quality signal, not a default-to-UTC trigger.
func parseOffsetDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if d, err := time.ParseDuration(strings.ReplaceAll(s, " ", "")); err == nil {
		return d, true
	}
	if len(s) == 6 && (s[0] == '+' || s[0] == '-') {
		hours, hErr := strconv.Atoi(s[1:3])
		mins, mErr := strconv.Atoi(s[4:6])
		if hErr == nil && mErr == nil && s[3] == ':' {
			d := time.Duration(hours)*time.Hour + time.Duration(mins)*time.Minute
			if s[0] == '-' {
				d = -d
			}
			return d, true
		}
	}
	return 0, false
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
//
// When force is true, the cache lookup is skipped entirely — go
// straight to hamnut and treat the result as a cold miss. This is
// the EnrichRefresh path: an upstream success overwrites the cache
// row via the same writeback the cold-miss branch uses; an upstream
// failure returns source=none rather than falling back to the
// cached row (the operator asked for fresh data).
func (o *Orchestrator) readCountry(ctx context.Context, callsign string, force bool) countryReadResult {
	if !force {
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
//
// When force is true, the cache lookup is skipped entirely — run
// the chain and treat the result as a cold miss. Same EnrichRefresh
// semantics as readCountry: upstream success → writeback overwrite;
// upstream failure → source=none, no fallback to the cached row.
func (o *Orchestrator) readStation(ctx context.Context, callsign string, force bool) stationReadResult {
	if !force {
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
	}

	station, source := o.runChain(ctx, callsign)
	if source == SourceNone {
		return stationReadResult{source: SourceNone}
	}
	return stationReadResult{data: station, source: source, coldMiss: true}
}

// runChain iterates configured callsign providers in ADR 0068 priority order.
// Each response is normalised before the completion check; lower providers
// fill blanks without overwriting higher-priority data. Empty responses and
// errors advance, while cancellation stops the remaining chain promptly.
//
// Returns the provider's Name() as the source on success, or
// SourceNone if every provider returned empty / error / nothing-to-
// run.
func (o *Orchestrator) runChain(ctx context.Context, callsign string) (types.ContactedStation, string) {
	accumulated := types.ContactedStation{}
	source := SourceNone
	for _, p := range o.Chain {
		if ctx.Err() != nil {
			break
		}
		if p == nil {
			continue
		}
		station, err := p.LookupWithContext(ctx, callsign)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if !stderrs.Is(err, errors.ErrNotFound) {
				o.warn("callsign provider error: "+p.Name(), err)
			}
			continue
		}
		if ctx.Err() != nil {
			break
		}
		station = o.normalizeProviderStation(callsign, station)
		if IsEmpty(station) {
			continue
		}
		if source == SourceNone {
			source = p.Name()
		}
		accumulated = fillCallsignBlanks(accumulated, station)
		if !o.needsNextCallsignProvider(accumulated) {
			return accumulated, source
		}
	}
	if IsEmpty(accumulated) {
		return types.ContactedStation{}, SourceNone
	}
	return accumulated, source
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
		// The uncacheable-prefix skip mirrors the sync writeback: hamnut may
		// return a one-char block prefix on a refresh even when the stale row's
		// own prefix was longer; leave the stale row rather than warn per fire.
		if res.Name == "Unknown" || !sqlsvc.IsCacheableCountryPrefix(res.Prefix) {
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
		station = o.normalizeProviderStation(callsign, station)

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

		// Replace, not merge: the async stale-refresh is a provider refresh, so
		// it must clear fields that went empty upstream (review 2026-06-04 H1).
		if werr := o.DB.ReplaceContactedStationWithContext(ctx, station); werr != nil {
			o.warn("async contacted_station replace failed", werr)
		}
	})
}

// warn is a small wrapper that no-ops when LoggerService is nil so
// the orchestrator stays usable in tests that don't wire a logger.
//
// A cancelled context logs at DEBUG, not warn: cancellation is the CALLER's
// decision — the SPA aborted its enrichment fetch, the HTTP request died with
// its connection, or the daemon is shutting down — not a provider or DB
// fault, and it arrived in bursts of full Warn error-chains that drowned the
// real warnings (dogfood 2026-08-07: 9 of one session's 24 warnings). The
// demotion lives HERE so all call sites share one rule and cannot drift.
// context.DeadlineExceeded deliberately stays a warn: that is our own timeout
// catching a slow provider, a fact the operator tunes against.
func (o *Orchestrator) warn(msg string, err error) {
	if o.Logger == nil {
		return
	}
	if stderrs.Is(err, context.Canceled) {
		o.Logger.DebugWith().Err(err).Msg(msg)
		return
	}
	o.Logger.WarnWith().Err(err).Msg(msg)
}

// normalizeProviderStation applies the ingress perimeter to a provider result
// (lookup.NormalizeProviderStation) and REPORTS a coordinate it had to drop.
//
// The report is the point of the wrapper: a silently dropped coordinate is
// indistinguishable from a provider that sends none, which is the normal case
// for most of them — so a provider that changed format, or one whose format we
// never established, would degrade in complete silence. Warn, because it is a
// real loss of data we were offered; never fatal, because enrichment must never
// block logging.
func (o *Orchestrator) normalizeProviderStation(callsign string, cs types.ContactedStation) types.ContactedStation {
	lat, lon := cs.Lat, cs.Lon
	out, dropped := NormalizeProviderStation(cs)
	if dropped && o.Logger != nil {
		o.Logger.WarnWith().
			Str("callsign", callsign).
			Str("lat", lat).
			Str("lon", lon).
			Msg("lookup: provider coordinates were not decimal or ADIF Location; dropped")
	}
	return out
}

// logCheckDB returns the connection used for the new-entity qso-table check:
// the dedicated log connection when wired (the file-split daemon), else DB
// (single-connection mode — tests and the pre-split shape).
func (o *Orchestrator) logCheckDB() *sqlsvc.Service {
	if o.LogDB != nil {
		return o.LogDB
	}
	return o.DB
}

// onPanic is the safego panic handler for the two Enrich-spawned
// goroutines (readCountry / readStation). Logs the recovered panic
// with the goroutine label + stack so a daemon-wide post-mortem can
// pin the origin. No-ops when Logger is nil so tests that skip
// logger wiring don't blow up.
func (o *Orchestrator) onPanic(name string, p any, stack []byte) {
	if o.Logger == nil {
		return
	}
	o.Logger.ErrorWith().
		Str("worker", name).
		Str("panic", fmt.Sprintf("%v", p)).
		Bytes("stack", stack).
		Msg("orchestrator: enrich goroutine panicked")
}
