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
	out, err := utils.ConvertToXDDDMMM(v, isLat)
	if err != nil {
		return v
	}
	return out
}

// storageLocation is adifLocation's inverse: an ADIF Location becomes signed
// decimal degrees, which is the shape storage and the SPA map use. Anything that
// is not a Location — notably a bare decimal, which is what SM's own files
// carried before 2026-08-04 — is returned UNCHANGED, so importing a
// decimal-bearing file is a no-op rather than a corruption.
func storageLocation(v string) string {
	if v == "" {
		return ""
	}
	out, err := utils.ConvertFromXDDDMMM(v)
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
	st.Lat = storageLocation(st.Lat)
	st.Lon = storageLocation(st.Lon)
	return st
}
