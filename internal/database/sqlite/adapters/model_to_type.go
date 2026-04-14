package adapters

import (
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/goccy/go-json"
)

// ContactedStationModelToType converts a sqlite row back to types.ContactedStation.
//
// The additional_data blob is shaped like types.ContactedStation (written by
// ContactedStationTypeToModel as json.Marshal(station)), so we unmarshal it
// directly into the target struct and then overlay the promoted column values,
// which are authoritative.
func ContactedStationModelToType(model *models.ContactedStation) (types.ContactedStation, error) {
	const op errors.Op = "sqlite.adapters.ContactedStationModelToType"
	if model == nil {
		return types.ContactedStation{}, errors.New(op).Msg(errMsgNilModel)
	}

	station := types.ContactedStation{}
	if err := json.Unmarshal(model.AdditionalData, &station); err != nil {
		return types.ContactedStation{}, errors.New(op).Err(err)
	}

	// Overlay promoted columns — authoritative over anything in the blob.
	station.CSID = model.ID
	station.Name = model.Name
	station.Call = model.Call
	station.Country = model.Country

	return station, nil
}

func CountryModelToType(model *models.Country) (types.Country, error) {
	const op errors.Op = "sqlite.adapters.CountryModelToType"
	if model == nil {
		return types.Country{}, errors.New(op).Msg(errMsgNilModel)
	}
	return types.Country{
		Name:       model.Name,
		Prefix:     model.Prefix,
		Continent:  model.Continent,
		Ccode:      model.Ccode,
		DXCCPrefix: model.DXCCPrefix,
		TimeOffset: model.TimeOffset,
		CQZone:     model.CQZone,
		ITUZone:    model.ItuZone,
	}, nil
}

// QsoModelToType converts a sqlite row back to types.Qso.
//
// The additional_data blob is shaped like types.Qso (written by QsoTypeToModel
// as json.Marshal(qso)), so we unmarshal it directly into the target struct
// and then overlay the promoted column values, which are authoritative. Any
// disagreement between the blob's copy of a promoted field and the column's
// copy is resolved in favor of the column — so if the blob is ever stale, the
// next write rewrites it correctly.
func QsoModelToType(model *models.Qso) (types.Qso, error) {
	const op errors.Op = "sqlite.adapters.QsoModelToType"
	if model == nil {
		return types.Qso{}, errors.New(op).Msg(errMsgNilModel)
	}

	qso := types.Qso{}
	if err := json.Unmarshal(model.AdditionalData, &qso); err != nil {
		return qso, errors.New(op).Err(err)
	}

	// Overlay promoted columns — authoritative over anything in the blob.
	qso.ID = model.ID
	qso.LogbookID = model.LogbookID
	qso.SessionID = model.SessionID
	qso.ContactedStation.Call = model.Call
	qso.ContactedStation.Country = model.Country
	qso.QsoDetails.Band = model.Band
	qso.QsoDetails.Mode = model.Mode
	qso.QsoDetails.Freq = strconv.FormatInt(model.Freq, 10)
	qso.QsoDetails.QsoDate = model.QsoDate
	qso.QsoDetails.TimeOn = model.TimeOn
	qso.QsoDetails.TimeOff = model.TimeOff
	qso.QsoDetails.RstSent = model.RstSent
	qso.QsoDetails.RstRcvd = model.RstRcvd

	return qso, nil
}

func LogbookModelToType(model *models.Logbook) (types.Logbook, error) {
	const op errors.Op = "sqlite.adapters.LogbookModelToType"
	if model == nil {
		return types.Logbook{}, errors.New(op).Msg(errMsgNilModel)
	}
	return types.Logbook{
		ID:          model.ID,
		Name:        model.Name,
		Callsign:    model.Callsign,
		APIKey:      model.APIKey.String,
		Description: model.Description.String,
	}, nil
}
