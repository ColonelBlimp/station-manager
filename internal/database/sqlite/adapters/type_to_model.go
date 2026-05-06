package adapters

import (
	"encoding/json"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/aarondl/null/v8"
)

// QsoTypeToModel converts a types.Qso to the sqlite row shape.
//
// Design: types.Qso follows the ADIF specification. Fields that are queryable,
// indexed, or frequently filtered on (call, band, mode, freq, etc.) are promoted
// to real columns; every other field travels through the additional_data JSON
// blob via json.Marshal of the whole struct. Promoted fields are duplicated
// (once in the column, once in the blob) — the duplication is trivial (~50 bytes
// per row) and the column is authoritative on read (see QsoModelToType, which
// overlays the column values over the unmarshaled blob).
//
// Consequence: adding a new ADIF field to types.Qso is a one-line change. This
// adapter does not need to be updated unless you want to promote the new field
// to a real column (a deliberate indexing decision, not a spec-tracking one).
func QsoTypeToModel(qso types.Qso) (models.Qso, error) {
	const op errors.Op = "sqlite.adapters.QsoTypeToModel"

	// qso.QsoDetails.Freq is the ADIF-native MHz decimal string (e.g.
	// "14.074"). The sqlite schema stores integer kHz; convert here at the
	// type→model boundary.
	freqKHz, err := utils.ParseFreqMHz(qso.QsoDetails.Freq)
	if err != nil {
		return models.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to parse frequency")
	}

	// Normalize date and time fields for the promoted columns.
	date := qso.QsoDetails.QsoDate
	if strings.Contains(date, "-") {
		date = strings.ReplaceAll(date, "-", "")
	}
	timeOn := qso.QsoDetails.TimeOn
	if strings.Contains(timeOn, ":") {
		timeOn = strings.ReplaceAll(timeOn, ":", "")
	}
	timeOff := qso.QsoDetails.TimeOff
	if strings.Contains(timeOff, ":") {
		timeOff = strings.ReplaceAll(timeOff, ":", "")
	}

	jsonData, err := json.Marshal(qso)
	if err != nil {
		return models.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to marshal qso to JSON")
	}
	if len(jsonData) == 0 {
		jsonData = []byte("{}")
	}

	return models.Qso{
		ID:             qso.ID,
		UUID:           qso.UUID,
		LogbookID:      qso.LogbookID,
		Call:           qso.ContactedStation.Call,
		Band:           qso.QsoDetails.Band,
		Mode:           qso.QsoDetails.Mode,
		Freq:           freqKHz,
		QsoDate:        date,
		TimeOn:         timeOn,
		TimeOff:        timeOff,
		RstSent:        qso.QsoDetails.RstSent,
		RstRcvd:        qso.QsoDetails.RstRcvd,
		Country:        qso.ContactedStation.Country,
		AdditionalData: jsonData,
		DedupeKey:      qso.DedupeKey,
	}, nil
}

// ContactedStationTypeToModel converts a types.ContactedStation to the sqlite row shape.
// Same pattern as QsoTypeToModel: promoted columns + json.Marshal of the whole struct
// for the blob. See QsoTypeToModel for the design rationale.
func ContactedStationTypeToModel(station types.ContactedStation) (models.ContactedStation, error) {
	const op errors.Op = "sqlite.adapters.ContactedStationTypeToModel"

	jsonData, err := json.Marshal(station)
	if err != nil {
		return models.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("failed to marshal contacted_station to JSON")
	}
	if len(jsonData) == 0 {
		jsonData = []byte("{}")
	}

	return models.ContactedStation{
		ID:             station.CSID,
		Call:           station.Call,
		Country:        station.Country,
		Name:           station.Name,
		AdditionalData: jsonData,
	}, nil
}

func CountryTypeToModel(country types.Country) (models.Country, error) {
	return models.Country{
		ID:         country.ID,
		Name:       country.Name,
		CQZone:     country.CQZone,
		ItuZone:    country.ITUZone,
		Continent:  country.Continent,
		Prefix:     country.Prefix,
		Ccode:      country.Ccode,
		DXCCPrefix: country.DXCCPrefix,
		TimeOffset: country.TimeOffset,
	}, nil
}

func LogbookTypeToModel(logbook types.Logbook) (models.Logbook, error) {
	return models.Logbook{
		ID:          logbook.ID,
		Name:        logbook.Name,
		Callsign:    logbook.Callsign,
		Description: null.StringFrom(logbook.Description),
	}, nil
}
