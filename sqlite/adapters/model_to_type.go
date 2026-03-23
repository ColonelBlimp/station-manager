package adapters

import (
	"strconv"

	"github.com/Station-Manager/database/sqlite/models"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/goccy/go-json"
)

func ContactedStationModelToType(model *models.ContactedStation) (types.ContactedStation, error) {
	const op errors.Op = "sqlite.adapters.ContactedStationModelToType"
	if model == nil {
		return types.ContactedStation{}, errors.New(op).Msg(errMsgNilModel)
	}

	data := types.ContactedStationAdditionalData{}
	if err := json.Unmarshal(model.AdditionalData, &data); err != nil {
		return types.ContactedStation{}, err
	}

	return types.ContactedStation{
		CSID:         model.ID,
		Name:         model.Name,
		Call:         model.Call,
		Country:      model.Country,
		Address:      data.Address,
		Age:          data.Age,
		Altitude:     data.Altitude,
		Cont:         data.Cont,
		ContactedOp:  data.ContactedOp,
		CQZ:          data.CQZ,
		DXCC:         data.DXCC,
		Email:        data.Email,
		EqCall:       data.EqCall,
		Gridsquare:   data.Gridsquare,
		Iota:         data.Iota,
		IotaIslandId: data.IotaIslandId,
		ITUZ:         data.ITUZ,
		Lat:          data.Lat,
		Lon:          data.Lon,
		QTH:          data.QTH,
		Sig:          data.Sig,
		SigInfo:      data.SigInfo,
		Web:          data.Web,
		WwffRef:      data.WwffRef,
	}, nil
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

func QsoModelToType(model *models.Qso) (types.Qso, error) {
	const op errors.Op = "sqlite.adapters.QsoModelToType"
	if model == nil {
		return types.Qso{}, errors.New(op).Msg(errMsgNilModel)
	}

	typesQso := types.Qso{}
	if err := json.Unmarshal(model.AdditionalData, &typesQso); err != nil {
		return typesQso, err
	}

	typesQso.ID = model.ID
	typesQso.QsoDetails.Band = model.Band
	typesQso.QsoDetails.Freq = strconv.FormatInt(model.Freq, 10)
	typesQso.QsoDetails.Mode = model.Mode
	typesQso.QsoDetails.QsoDate = model.QsoDate
	typesQso.QsoDetails.RstRcvd = model.RstRcvd
	typesQso.QsoDetails.RstSent = model.RstSent
	typesQso.QsoDetails.TimeOff = model.TimeOff
	typesQso.QsoDetails.TimeOn = model.TimeOn
	typesQso.LogbookID = model.LogbookID
	typesQso.SessionID = model.SessionID
	typesQso.ContactedStation.Country = model.Country
	typesQso.ContactedStation.Call = model.Call

	return typesQso, nil
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
