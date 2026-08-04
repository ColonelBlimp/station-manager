package adif

import (
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// adifLocation renders a coordinate as the ADIF Location type ("XDDD MM.MMM" —
// degrees, decimal minutes, hemisphere letter).
//
// Enrichment stores the contacted station's coordinates as DECIMAL DEGREES,
// because that is what QRZ returns and what the SPA map consumes. MY_LAT /
// MY_LON were already correct — config.go derives them via
// utils.MaidenheadToADIFLatLon — so one binary was emitting two different
// formats for the same ADIF type until this existed (2026-08-04).
//
// The conversion happens HERE, at the ADIF boundary, and storage is left alone
// on purpose: the map parses these fields with parseFloat and falls back to the
// grid when that fails, so storing the ADIF form would silently drop every
// station to cell-centre precision — fixing an export bug by breaking display.
//
// Anything that will not parse as a decimal is returned UNCHANGED, which is what
// makes this idempotent: an import-era row already holding "N051 30.000" fails
// ParseFloat and rides out as it came in, rather than being mangled or blanked.
// The same path covers genuine garbage — a bad coordinate must not abort an
// entire session export.
func adifLocation(v string, isLat bool) string {
	if v == "" {
		return "" // absent, not the equator: never invent "N000 00.000"
	}
	if out, err := utils.ConvertToXDDDMMM(v, isLat); err == nil {
		return out
	}
	// Already an ADIF Location FOR THIS AXIS — an import-era value, which rides
	// out as it came in. The axis check matters: "E022 58.119" is a valid
	// Location but not a valid LATITUDE.
	if _, err := utils.ConvertFromXDDDMMM(v, isLat); err == nil {
		return v
	}
	// Neither. Omit rather than emit it raw: "<MY_LAT:4>91.0" is not an ADIF
	// Location and a consumer may reject the whole record, whereas an absent
	// field is valid ADIF. The value stays in storage — nothing is lost that was
	// not already unusable (codex fbaafe73 P1).
	return ""
}

// storageLocation is adifLocation's inverse: an ADIF Location becomes signed
// decimal degrees, which is the shape storage and the SPA map use. isLat is
// load-bearing — without it a LAT field accepted "E022 58.119" and a hemisphere
// from the wrong axis was silently converted (codex c3d99362 P1). Anything that
// is not a Location — notably a bare decimal, which is what SM's own files
// carried before 2026-08-04 — is returned UNCHANGED, so importing a
// decimal-bearing file is a no-op rather than a corruption.
func storageLocation(v string, isLat bool) string {
	if v == "" {
		return ""
	}
	out, err := utils.ConvertFromXDDDMMM(v, isLat)
	if err != nil {
		return v
	}
	return out
}

// contactedStationToStorage converts the coordinate pair back to storage shape
// on the way IN, so an ADIF round trip is lossless in shape. Converting only the
// export would leave our own export→import cycle storing Location strings the
// map cannot parse — trading a wire defect for a silent display one.
func contactedStationToStorage(st types.ContactedStation) types.ContactedStation {
	st.Lat = storageLocation(st.Lat, true)
	st.Lon = storageLocation(st.Lon, false)
	return st
}

// loggingStationToStorage is contactedStationToStorage's twin for the operator's
// own position, so the round trip is lossless in shape on both sides of a QSO.
func loggingStationToStorage(ls types.LoggingStation) types.LoggingStation {
	ls.MyLat = storageLocation(ls.MyLat, true)
	ls.MyLon = storageLocation(ls.MyLon, false)
	return ls
}
