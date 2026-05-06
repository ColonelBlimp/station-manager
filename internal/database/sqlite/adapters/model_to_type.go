package adapters

import (
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
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
		return types.ContactedStation{}, errors.New(op).WithMsg(errMsgNilModel)
	}

	station := types.ContactedStation{}
	if err := json.Unmarshal(model.AdditionalData, &station); err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err)
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
		return types.Country{}, errors.New(op).WithMsg(errMsgNilModel)
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
		return types.Qso{}, errors.New(op).WithMsg(errMsgNilModel)
	}

	qso := types.Qso{}
	if err := json.Unmarshal(model.AdditionalData, &qso); err != nil {
		return qso, errors.New(op).WithErr(err)
	}

	// Overlay promoted columns — authoritative over anything in the blob.
	qso.ID = model.ID
	qso.UUID = model.UUID
	qso.LogbookID = model.LogbookID
	qso.ContactedStation.Call = model.Call
	qso.ContactedStation.Country = model.Country
	qso.QsoDetails.Band = model.Band
	qso.QsoDetails.Mode = model.Mode
	// model.Freq is integer kHz (DB storage). types.Qso.Freq is the
	// ADIF-native MHz decimal string.
	qso.QsoDetails.Freq = utils.FormatFreqMHz(model.Freq)
	qso.QsoDetails.QsoDate = model.QsoDate
	qso.QsoDetails.TimeOn = model.TimeOn
	qso.QsoDetails.TimeOff = model.TimeOff
	qso.QsoDetails.RstSent = model.RstSent
	qso.QsoDetails.RstRcvd = model.RstRcvd
	qso.DedupeKey = model.DedupeKey

	return qso, nil
}

func LogbookModelToType(model *models.Logbook) (types.Logbook, error) {
	const op errors.Op = "sqlite.adapters.LogbookModelToType"
	if model == nil {
		return types.Logbook{}, errors.New(op).WithMsg(errMsgNilModel)
	}
	return types.Logbook{
		ID:          model.ID,
		Name:        model.Name,
		Callsign:    model.Callsign,
		Description: model.Description.String,
	}, nil
}

// QsoUploadModelToType converts a sqlite row to types.QsoUpload.
//
// The model has several nullable columns (created_at, modified_at,
// last_attempt_at, last_error, upstream_id); the DTO flattens them to
// zero-values so downstream consumers (worker, pull-endpoint handler)
// don't have to handle null-vs-value distinctions the daemon doesn't
// care about.
func QsoUploadModelToType(model *models.QsoUpload) (types.QsoUpload, error) {
	const op errors.Op = "sqlite.adapters.QsoUploadModelToType"
	if model == nil {
		return types.QsoUpload{}, errors.New(op).WithMsg(errMsgNilModel)
	}
	return types.QsoUpload{
		ID:            model.ID,
		CreatedAt:     model.CreatedAt,
		ModifiedAt:    model.ModifiedAt.Time,
		QsoID:         model.QsoID,
		ForwarderName: model.ForwarderName,
		ForwarderType: model.ForwarderType,
		Action:        model.Action,
		Status:        model.Status,
		Attempts:      model.Attempts,
		LastAttemptAt: model.LastAttemptAt.Int64,
		NextAttemptAt: model.NextAttemptAt,
		LastError:     model.LastError.String,
		UpstreamID:    model.UpstreamID.String,
	}, nil
}
