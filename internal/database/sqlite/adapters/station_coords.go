package adapters

import (
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The readable / inside-the-cell predicates live in utils so provider ingress,
// this merge and operator-config validation cannot drift on what "contradicts"
// means. Thin aliases keep the call sites below readable.
func coordsReadable(lat, lon string) bool { return utils.CoordsReadable(lat, lon) }

func isPlaceholderGrid(grid string) bool { return utils.IsPlaceholderGrid(grid) }

func coordsInsideGrid(grid, lat, lon string) bool {
	return utils.CoordsInsideGrid(grid, lat, lon)
}

// ReconcileStationCoords keeps a station's coordinates and its gridsquare from
// contradicting each other.
//
// QRZ returns the two as INDEPENDENT fields and mergeContactedStation merges
// every field independently, so the pair comes apart without either write being
// wrong on its own: QRZ's placeholder grid and its matching coordinates are
// cached together (consistent, nothing to object to), then a later lookup
// supplies the station's REAL grid — from the on-air exchange — and replaces the
// gridsquare alone. The stale coordinates stay beside it. Five rows across four
// stations reached QRZ and ClubLog that way before this existed (2026-08-04).
//
// TWO CALLERS, ONE RULE, and it lives in this package so it can serve both.
// writeContactedStation applies it to the enrichment CACHE; QsoTypeToModel
// applies it to the QSO ROW, which is the copy that leaves the station (ADIF
// export reads it, and the forwarding worker re-reads it from the database
// before submitting to QRZ / ClubLog / SM Cloud). Guarding only the cache is
// exactly the gap dogfooding found on 2026-08-05: a QSO is assembled from the
// enrichment RESULT rather than read back from the reconciled cache row, so
// US2YW was stored with grid KN28 beside coordinates ~500 km outside it while
// the cache held the corrected pair. A second implementation for the QSO side
// is how the two would start disagreeing about what "contradicts" means, which
// is the fault the shared utils predicates were extracted to prevent.
//
// Coordinates are KEPT when they agree with the grid and DERIVED from it when
// they do not (operator's ruling, 2026-08-04). Not "always derive": a 6-char
// locator is a ~5x10 km cell and QRZ's position is usually the station's actual
// one, so flattening every station to a cell centre would discard real precision
// to fix a rare contradiction.
//
// Deliberately does NOT invent coordinates for a grid that has none — that would
// be manufacturing a position nobody supplied — and leaves coordinates untouched
// when there is no valid grid to contradict them, because a plausibility rule
// would relocate stations on a guess with nothing to show it had happened.
func ReconcileStationCoords(st types.ContactedStation) types.ContactedStation {
	// A sentinel grid is not an arbiter — coordinates derived from it agree with
	// it by construction, so it can neither confirm nor contradict anything. It
	// is REJECTED AT INGRESS (lookup.NormalizeProviderStation) so it should never
	// reach here; a historical row that still holds one is left exactly as it is.
	//
	// Clearing here is what the earliest version did, and it destroyed data: the
	// merge retains coordinates when the incoming record has none, so a refresh
	// carrying only the sentinel wiped a real grid AND the precise coordinates
	// beside it (codex c3d99362 P1). Reconciliation must never be the thing that
	// removes a location.
	if isPlaceholderGrid(st.Gridsquare) {
		return st
	}
	if st.Lat == "" && st.Lon == "" {
		return st // nothing supplied; a grid alone is not a position
	}
	cLat, cLon, _, _, ok := utils.MaidenheadToCell(st.Gridsquare)
	if !ok {
		return st // no usable locator, so nothing can contradict them
	}
	if !coordsReadable(st.Lat, st.Lon) {
		// Unreadable is NOT contradictory — nothing was compared, so nothing was
		// disproved, and overwriting would destroy the supplied value on a
		// judgement never made. Costs nothing on screen: the map's parseFloat
		// rejects it either way and falls back to the same cell centre for
		// DISPLAY. Storage keeps what arrived.
		return st
	}
	if coordsInsideGrid(st.Gridsquare, st.Lat, st.Lon) {
		return st // more precise than the cell, and consistent with it
	}
	// 6 dp matches what enrichment already stores and is ~0.1 m — far finer
	// than the cell it came from, so it loses nothing the grid ever carried.
	st.Lat = strconv.FormatFloat(cLat, 'f', 6, 64)
	st.Lon = strconv.FormatFloat(cLon, 'f', 6, 64)
	return st
}
