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
	"context"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Provider is the common surface every lookup provider exposes —
// independent of which fields the provider supplies. Used by the
// orchestrator (and logging) to identify which provider produced a
// given result without needing the concrete kind in scope.
type Provider interface {
	// Name returns a stable lowercase identifier — "hamnut", "qrz",
	// "hamqth", "qrzcq". Doubles as the value the SPA receives in
	// the response's source indicator (countrySource / stationSource
	// per ADR 0017 #12) and as the log/event label.
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
// Lat / Lon / Altitude pass through — they're either provider-supplied
// directly or derived from gridsquare downstream, but they aren't
// part of the country-table-derived data.
func FilterToCallsignFields(cs types.ContactedStation) types.ContactedStation {
	cs.Country = ""
	cs.Cont = ""
	cs.CQZ = ""
	cs.ITUZ = ""
	cs.DXCC = ""
	return cs
}
