package sqlite

import (
	"math"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// coordsReadable reports whether both values parse as decimal degrees — i.e.
// whether a comparison against a grid is possible AT ALL.
//
// Separate from coordsInsideGrid on purpose. "These coordinates contradict the
// grid" and "these coordinates cannot be read here" are different facts: the
// first is a comparison that happened and failed, the second is a comparison
// that never took place. One predicate returning false for both let an
// unreadable value be recorded as a contradiction and overwritten with the cell
// centre — 3.57 km, silently, destroying the supplied value (2026-08-04).
func coordsReadable(lat, lon string) bool {
	_, errLat := strconv.ParseFloat(lat, 64)
	_, errLon := strconv.ParseFloat(lon, 64)
	return errLat == nil && errLon == nil
}

// coordsInsideGrid reports whether decimal lat/lon fall within the locator's
// own cell. The cell IS the test — a locator declares an extent, so anything
// inside it is consistent with it by definition and no distance threshold has
// to be invented. Callers must establish coordsReadable first: an unreadable
// value is not inside the cell, but it is not outside it either.
//
// Same predicate as agreesWithCell() in the SPA's map layer; they must not
// drift, or the map and the stored row disagree about what contradicts what.
func coordsInsideGrid(grid, lat, lon string) bool {
	cLat, cLon, latSpan, lonSpan, ok := utils.MaidenheadToCell(grid)
	if !ok {
		return false
	}
	gotLat, errLat := strconv.ParseFloat(lat, 64)
	gotLon, errLon := strconv.ParseFloat(lon, 64)
	if errLat != nil || errLon != nil {
		return false
	}
	return math.Abs(gotLat-cLat) <= latSpan/2 && math.Abs(gotLon-cLon) <= lonSpan/2
}

// isPlaceholderGrid reports whether a locator is the AA00 sentinel — the
// all-minimum square that means "never set", not a position.
//
// EVIDENCE (2026-08-04, R9LAU's QRZ profile at www.qrz.com/db/R9LAU): "Grid
// Square AA00aa / Geo Source: From Grid". QRZ DERIVES coordinates from it, so
// the grid and the coordinates agree by construction and the mismatch rule
// cannot reject them — the pair has to be caught by the sentinel itself.
//
// AA00 is the ONLY locator treated this way, because it is the only one there is
// evidence for. Adding others on suspicion would start relocating real stations.
// A station genuinely in AA00 (Antarctic, at the antimeridian) is knowingly
// accepted collateral: vanishingly rare, and an on-air grid still corrects it.
// This catches sentinels and self-contradiction — a plausible but FALSE grid is
// indistinguishable from a true one with the data held here, and nothing in this
// file should pretend otherwise.
func isPlaceholderGrid(grid string) bool {
	g := strings.ToUpper(strings.TrimSpace(grid))
	return strings.HasPrefix(g, "AA00") && utils.IsValidMaidenhead(g)
}

// reconcileStationCoords keeps a station's coordinates and its gridsquare from
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
func reconcileStationCoords(st types.ContactedStation) types.ContactedStation {
	// The sentinel first: it is self-consistent with the coordinates derived
	// from it, so agreement cannot reject it. Both fields go — a placeholder is
	// an absence of location, and storing it would plot and EXPORT a station at
	// the South Pole.
	if isPlaceholderGrid(st.Gridsquare) {
		st.Gridsquare, st.Lat, st.Lon = "", "", ""
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
