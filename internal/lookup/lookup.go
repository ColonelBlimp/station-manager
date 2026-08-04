// Package lookup defines the provider abstraction for callsign and
// country enrichment, per ADR 0017.
//
// Two distinct provider kinds:
//
//   - CountryProvider supplies country / CQ-zone / ITU-zone / continent
//     / DXCC data keyed on callsign prefix. Hamnut is currently the
//     only implementation. Country data is hamnut-exclusive on write
//     per ADR 0017 #2 — callsign-class providers do NOT implement
//     this interface even if their upstream API can return country
//     data.
//
//   - CallsignProvider supplies operator-shaped data (name, QTH,
//     gridsquare, license class, etc.) by callsign. QRZ.com, HamQTH,
//     QRZCQ all implement this interface. The orchestrator runs
//     configured CallsignProviders in priority order, first-non-
//     empty-wins, fallback through the rest on empty or error.
//
// Concrete providers live in subpackages: lookup/hamnut, lookup/qrz,
// (future) lookup/hamqth, (future) lookup/qrzcq.
//
// FilterToCallsignFields enforces the ADR 0017 #2 narrowing at the
// orchestrator's merge boundary — country / continent / CQ / ITU /
// DXCC fields returned by a CallsignProvider are zeroed before write.
package lookup

import (
	"math"
	"strconv"

	"context"
	"github.com/ColonelBlimp/station-manager/internal/utils"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Provider is the common surface every lookup provider exposes —
// independent of which fields the provider supplies. Used by the
// orchestrator (and logging) to identify which provider produced a
// given result without needing the concrete kind in scope.
type Provider interface {
	// Name returns the provider's stable identifier — the DI service NAME
	// (hamnut's types.HamNutLookupServiceName = "hamnutlookupservice", QRZ's
	// "qrzlookupservice"). NOTE this is distinct from the public country SOURCE
	// constant "hamnut" (SourceHamnut): the country layer reports the fixed
	// Source* constants (SourceHamnut / SourceCountryTable) and does NOT use a
	// provider Name(). For a CallsignProvider, Name() is exactly what the
	// orchestrator puts in the response's station_source indicator (ADR 0017 #12)
	// and the log/event label — so the station public source value IS the service
	// name, not a short label. A future split of DI-name vs public label is
	// deferred (review 2026-06-04 L2).
	Name() string

	// Initialize wires dependencies (logger, config, HTTP client) and
	// validates provider-specific configuration. Idempotent — safe to
	// call multiple times. Returns an error if the provider is
	// misconfigured (missing creds, unreachable URL on probe, etc.).
	//
	// ctx is the daemon-lifecycle context the caller threads through
	// from cmd/smd. Providers that perform I/O during initialization
	// (notably QRZ's session-key fetch) propagate ctx into their HTTP
	// calls so daemon shutdown can cancel a stuck handshake;
	// providers that do no I/O (hamnut) accept ctx for signature
	// uniformity but don't use it. Pre-fix (review M4), the QRZ
	// session-key path used http.NewRequest with no context — a
	// future config-reload-during-running scenario would have no
	// way to interrupt a hung TLS handshake.
	Initialize(ctx context.Context) error
}

// CountryProvider supplies country / CQ-zone / ITU-zone / continent /
// DXCC data by callsign. The implementation is responsible for
// extracting the relevant prefix from the callsign and resolving it
// upstream; callers pass the full callsign.
//
// The returned types.Country carries the country-table-relevant
// fields. Per-QSO calculated fields (ShortPathDistance, IsNewEntity,
// LocalTime, etc.) are not the provider's concern — those are
// populated downstream when a Qso is built.
type CountryProvider interface {
	Provider

	// Lookup performs a blocking lookup with context.Background().
	// Convenience wrapper over LookupWithContext.
	Lookup(callsign string) (types.Country, error)

	// LookupWithContext performs the lookup honouring ctx for
	// cancellation and deadline. The orchestrator uses this so that
	// SPA-side AbortController cancellation propagates all the way
	// to the upstream HTTP call (per ADR 0017 #12).
	LookupWithContext(ctx context.Context, callsign string) (types.Country, error)
}

// CallsignProvider supplies operator-shaped fields — name, QTH,
// gridsquare, license class, optionally email/web/lat/lon/etc. — by
// callsign. QRZ.com, HamQTH, QRZCQ all implement this interface.
//
// The returned types.ContactedStation may include country / continent
// / CQ / ITU / DXCC fields if the upstream API populates them, but
// the orchestrator IGNORES those fields per ADR 0017 #2 — country
// data is hamnut-exclusive on write. Provider implementations don't
// need to remember to clear those fields; the orchestrator runs
// FilterToCallsignFields at the merge boundary.
type CallsignProvider interface {
	Provider

	// Lookup performs a blocking lookup with context.Background().
	// Convenience wrapper over LookupWithContext.
	Lookup(callsign string) (types.ContactedStation, error)

	// LookupWithContext performs the lookup honouring ctx for
	// cancellation and deadline.
	LookupWithContext(ctx context.Context, callsign string) (types.ContactedStation, error)
}

// FilterToCallsignFields returns a copy of cs with country / continent
// / CQ / ITU / DXCC fields zeroed. Per ADR 0017 #2, callsign-class
// providers must not write these — they're hamnut-exclusive. The
// orchestrator calls this on every CallsignProvider result before
// merging into contacted_station storage so provider authors don't
// have to remember to clear the fields manually.
//
// Altitude passes through — it isn't country-table-derived data.
//
// Lat / Lon used to pass through too. They no longer do: see
// NormalizeProviderStation, which owns the coordinate FORMAT at this boundary.
// Passing them through stored whatever a provider happened to send, which
// worked only because QRZ sends decimals — luck, not design.
func FilterToCallsignFields(cs types.ContactedStation) types.ContactedStation {
	cs.Country = ""
	cs.Cont = ""
	cs.CQZ = ""
	cs.ITUZ = ""
	cs.DXCC = ""
	return cs
}

// NormalizeProviderStation makes a CallsignProvider result safe to merge: it
// applies the ADR 0017 #2 narrowing and converts coordinates to the canonical
// internal form (decimal degrees).
//
// This is the ingress half of the perimeter rule (operator, 2026-08-04): every
// boundary converts to decimal on the way in and away from it on the way out, so
// nothing in the interior ever has to ask what format a value is in. It lives at
// the shared seam for the same reason FilterToCallsignFields does — "so provider
// authors don't have to remember" — which means a provider added tomorrow
// inherits it without writing any code.
//
// A coordinate that cannot be interpreted does NOT enter. Admitting it would
// break the one guarantee the boundary exists to make, and everything
// downstream — map, bearing, distance, the grid-agreement test — parses these
// as decimals. The pair is dropped together: a readable latitude beside an
// unreadable longitude is not half a position, it is no position. The grid is
// untouched, so a station usually keeps a usable location anyway.
//
// It deliberately does NOT arbitrate between the coordinates and the gridsquare.
// That contradiction arrives across TWO writes from two sources — a provider's
// grid in one, the on-air exchange's in another, which never passes through here
// at all — so it can only be judged where the merged record exists
// (sqlite.reconcileStationCoords). Format is local to one input; truth is not.
func NormalizeProviderStation(cs types.ContactedStation) (types.ContactedStation, bool) {
	cs = FilterToCallsignFields(cs)
	// The AA00 sentinel means "this provider has no location for the station",
	// so it supplies neither a grid nor the coordinates derived from it. Rejected
	// HERE and not at the storage merge: it is a property of one input, and
	// applying it to the merged record destroyed a location we already held —
	// a refresh returning the sentinel wiped a real grid and precise coordinates
	// (codex c3d99362 P1). Nothing downstream should ever see it.
	if utils.IsPlaceholderGrid(cs.Gridsquare) {
		cs.Gridsquare, cs.Lat, cs.Lon = "", "", ""
		return cs, false
	}
	if cs.Lat == "" && cs.Lon == "" {
		return cs, false // nothing supplied; nothing to normalise or report
	}
	lat, latOK := canonicalCoord(cs.Lat, true)
	lon, lonOK := canonicalCoord(cs.Lon, false)
	if !latOK || !lonOK {
		cs.Lat, cs.Lon = "", ""
		return cs, true // dropped — the caller reports it; a silent drop reads
		// exactly like a provider that sends no coordinates at all
	}
	cs.Lat, cs.Lon = lat, lon
	return cs, false
}

// canonicalCoord returns v as decimal degrees for the given axis. Decimal is
// already canonical; an ADIF Location is converted; anything else is refused.
//
// "Parses as a float" is NOT the same as "is a coordinate" (codex fd3062b7 P1).
// strconv.ParseFloat accepts NaN and ±Inf, and says nothing about range, so a
// latitude of 91 or a longitude of 181 passed straight through this boundary and
// on into the map and distance maths — while ADIF export, which DOES validate,
// then emitted nothing. The boundary that promises a canonical value has to
// enforce what makes it one.
func canonicalCoord(v string, isLat bool) (string, bool) {
	limit := 180.0
	if isLat {
		limit = 90.0
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > limit {
			return "", false
		}
		return v, true
	}
	if dec, err := utils.ConvertFromXDDDMMM(v, isLat); err == nil {
		return dec, true
	}
	return "", false
}
