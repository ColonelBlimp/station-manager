package adapters

import (
	"encoding/json"
	"strings"
	"time"

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

	// Coordinates must not contradict the gridsquare stored beside them. Applied
	// HERE because all five QSO write paths (Insert/Update × context/tx, plus the
	// manifest restore) convert through this function, and the defect this fixes
	// existed precisely because one call site was the entire mechanism. Ahead of
	// both the blob marshal and the promoted-column reads below, so the stored
	// row is reconciled whichever copy a reader takes.
	//
	// The QSO row is the one that leaves the station: ADIF export reads it, and
	// the forwarding worker re-reads it from the database before submitting.
	//
	// YES, THIS INCLUDES THE SM CLOUD RESTORE WRITER, deliberately — clean-room
	// review bcfbd8ea filed that as a P1 and it was refuted on evidence, with
	// the three checks and the accepted residue written up in
	// qso_coords_test.go (Q7, which pins both halves: coordinates reconciled,
	// modified_at + revision still verbatim). The short version: a restored row
	// is exported and uploaded like any other, so exempting it would reopen
	// this leak for exactly the pre-fix backups most likely to carry it.
	qso.ContactedStation = ReconcileStationCoords(qso.ContactedStation)

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

	// NB: qso.ModifiedAt / qso.DeletedAt / qso.Revision are deliberately NOT
	// mapped here. This adapter also serves the UPDATE path, which round-trips
	// a FETCHED qso (whose ModifiedAt the read overlay populated) — writing it
	// back explicitly would defeat the stamp trigger on never-edited rows (NEW
	// ≠ NULL-OLD → its modified_at CASE keeps the stale write), and revision
	// belongs to the same trigger (ADR 0050, combined in migration 0005): it
	// bumps on every update that does not itself change the column —
	// updateActiveQso's map omits it for exactly that reason. The one writer
	// that must preserve them — SM Cloud restore — sets the model fields
	// itself (InsertRestoredQsoWithContext).
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
		ID:              station.CSID,
		Call:            station.Call,
		Country:         station.Country,
		Name:            station.Name,
		AdditionalData:  jsonData,
		LastRefreshedAt: nullableTime(station.LastRefreshedAt),
	}, nil
}

func CountryTypeToModel(country types.Country) (models.Country, error) {
	return models.Country{
		ID:              country.ID,
		Name:            country.Name,
		CQZone:          country.CQZone,
		ItuZone:         country.ITUZone,
		Continent:       country.Continent,
		Prefix:          country.Prefix,
		Ccode:           country.Ccode,
		DXCCPrefix:      country.DXCCPrefix,
		TimeOffset:      country.TimeOffset,
		LastRefreshedAt: nullableTime(country.LastRefreshedAt),
	}, nil
}

// nullableTime maps a Go zero-time to a NULL DB column and a non-zero
// time to a populated null.Time. Adapter-internal helper for the
// last_refreshed_at columns added in ADR 0017's enrichment pipeline.
func nullableTime(t time.Time) null.Time {
	if t.IsZero() {
		return null.Time{}
	}
	return null.TimeFrom(t)
}

func LogbookTypeToModel(logbook types.Logbook) (models.Logbook, error) {
	return models.Logbook{
		ID:          logbook.ID,
		Name:        logbook.Name,
		Callsign:    logbook.Callsign,
		Description: null.StringFrom(logbook.Description),
	}, nil
}
