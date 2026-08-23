package adapters

import (
	"encoding/json"
	"time"

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
	// last_refreshed_at lives only on the column (cache-plumbing, not
	// part of the additional_data blob shape — enforced by `json:"-"`
	// on the field, so the blob never carries it). Zero time when NULL,
	// which the orchestrator treats as "never refreshed → stale."
	station.LastRefreshedAt = model.LastRefreshedAt.Time

	return station, nil
}

func CountryModelToType(model *models.Country) (types.Country, error) {
	const op errors.Op = "sqlite.adapters.CountryModelToType"
	if model == nil {
		return types.Country{}, errors.New(op).WithMsg(errMsgNilModel)
	}
	return types.Country{
		ID:         model.ID,
		Name:       model.Name,
		Prefix:     model.Prefix,
		Continent:  model.Continent,
		Ccode:      model.Ccode,
		DXCCPrefix: model.DXCCPrefix,
		TimeOffset: model.TimeOffset,
		CQZone:     model.CQZone,
		ITUZone:    model.ItuZone,
		// last_refreshed_at — zero time when NULL in DB; orchestrator
		// treats zero as "never refreshed → stale" per ADR 0017.
		LastRefreshedAt: model.LastRefreshedAt.Time,
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
	// Storage metadata (json:"-" on types.Qso — column-only, never in the
	// blob): the SM Cloud reconcile drift signal + the soft-delete tombstone.
	// modified_at is NULL until the update trigger first fires (no INSERT
	// default), so a never-edited row's modified time IS its creation time —
	// the same fallback the reconcile manifest query applies; the two readers
	// MUST agree or reconcile sees phantom drift on fresh rows.
	//
	// Truncated to SECONDS: the update trigger (datetime('now')) defines the
	// local modified_at protocol precision as one second, while sqlboiler's
	// created_at carries sub-second digits. Un-truncated, a same-second
	// edit/delete after the first push would carry a modified_at BELOW the
	// cloud's stored (created_at-derived) value and be rejected as a stale
	// push forever — reconcile would re-enqueue it every cycle.
	qso.ModifiedAt = model.ModifiedAt.Time.UTC().Truncate(time.Second)
	if !model.ModifiedAt.Valid {
		qso.ModifiedAt = model.CreatedAt.UTC().Truncate(time.Second)
	}
	qso.DeletedAt = model.DeletedAt.Time
	qso.Revision = model.Revision
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

// QsoHistoryModelToType converts a sqlite row to types.QsoHistory.
// BeforeImage is passed through as raw JSON bytes so the consumer can
// either deserialize into types.Qso for display or forward the
// snapshot intact to SM Cloud sync without re-marshaling.
func QsoHistoryModelToType(model *models.QsoHistory) (types.QsoHistory, error) {
	const op errors.Op = "sqlite.adapters.QsoHistoryModelToType"
	if model == nil {
		return types.QsoHistory{}, errors.New(op).WithMsg(errMsgNilModel)
	}
	return types.QsoHistory{
		ID:          model.ID,
		QsoUUID:     model.QsoUUID,
		Op:          model.Op,
		At:          model.At,
		Source:      model.Source,
		BeforeImage: []byte(model.BeforeImage),
	}, nil
}

// OperatorEventModelToType converts a sqlite operator_event row to
// types.OperatorEvent (ADR 0076, the W-0001 notification pilot). Detail is the
// raw stored JSON (json_valid-checked on write), returned verbatim as
// json.RawMessage.
func OperatorEventModelToType(model *models.OperatorEvent) (types.OperatorEvent, error) {
	const op errors.Op = "sqlite.adapters.OperatorEventModelToType"
	if model == nil {
		return types.OperatorEvent{}, errors.New(op).WithMsg(errMsgNilModel)
	}
	return types.OperatorEvent{
		ID:         model.ID,
		Category:   model.Category,
		Kind:       model.Kind,
		Severity:   model.Severity,
		OccurredAt: model.OccurredAt,
		Build:      model.Build,
		Detail:     json.RawMessage(model.Detail),
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
		Origin:        model.Origin,
	}, nil
}
