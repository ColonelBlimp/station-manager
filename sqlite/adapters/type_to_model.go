package adapters

import (
	"strconv"
	"strings"

	"github.com/Station-Manager/database/sqlite/models"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/aarondl/null/v8"
	"github.com/goccy/go-json"
)

func QsoTypeToModel(qso types.Qso) (models.Qso, error) {
	const op errors.Op = "sqlite.adapters.QsoTypeToModel"

	freqHz, err := strconv.ParseInt(qso.QsoDetails.Freq, 10, 64)
	if err != nil {
		return models.Qso{}, errors.New(op).Err(err).Msg("failed to parse frequency")
	}

	// Normalize date and time fields
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

	additionalData := types.QsoAdditionalData{
		// Upload status fields
		SmQsoUploadDate:     qso.SmQsoUploadDate,
		SmQsoUploadStatus:   qso.SmQsoUploadStatus,
		SmFwrdByEmailDate:   qso.SmFwrdByEmailDate,
		SmFwrdByEmailStatus: qso.SmFwrdByEmailStatus,
		QrzComUploadDate:    qso.QrzComUploadDate,
		QrzComUploadStatus:  qso.QrzComUploadStatus,

		// QsoDetails fields
		AIndex:      qso.QsoDetails.AIndex,
		AntPath:     qso.QsoDetails.AntPath,
		BandRx:      qso.QsoDetails.BandRx,
		Comment:     qso.QsoDetails.Comment,
		ContestId:   qso.QsoDetails.ContestId,
		Distance:    qso.QsoDetails.Distance,
		FreqRx:      qso.QsoDetails.FreqRx,
		Submode:     qso.QsoDetails.Submode,
		Notes:       qso.QsoDetails.Notes,
		QsoDateOff:  qso.QsoDetails.QsoDateOff,
		QsoRandom:   qso.QsoDetails.QsoRandom,
		QsoComplete: qso.QsoDetails.QsoComplete,
		RxPwr:       qso.QsoDetails.RxPwr,
		SRX:         qso.QsoDetails.SRX,
		STX:         qso.QsoDetails.STX,
		TxPwr:       qso.QsoDetails.TxPwr,
		Rig:         qso.QsoDetails.Rig,

		// ContactedStation fields
		Address:      qso.ContactedStation.Address,
		Age:          qso.ContactedStation.Age,
		Altitude:     qso.ContactedStation.Altitude,
		Cont:         qso.ContactedStation.Cont,
		ContactedOp:  qso.ContactedStation.ContactedOp,
		CQZ:          qso.ContactedStation.CQZ,
		DXCC:         qso.ContactedStation.DXCC,
		Email:        qso.ContactedStation.Email,
		EqCall:       qso.ContactedStation.EqCall,
		Gridsquare:   qso.ContactedStation.Gridsquare,
		Iota:         qso.ContactedStation.Iota,
		IotaIslandId: qso.ContactedStation.IotaIslandId,
		ITUZ:         qso.ContactedStation.ITUZ,
		Lat:          qso.ContactedStation.Lat,
		Lon:          qso.ContactedStation.Lon,
		Name:         qso.ContactedStation.Name,
		QTH:          qso.ContactedStation.QTH,
		Sig:          qso.ContactedStation.Sig,
		SigInfo:      qso.ContactedStation.SigInfo,
		Web:          qso.ContactedStation.Web,
		WwffRef:      qso.ContactedStation.WwffRef,

		// LoggingStation fields
		AntennaAzimuth:  qso.LoggingStation.AntennaAzimuth,
		MyAltitude:      qso.LoggingStation.MyAltitude,
		MyAntenna:       qso.LoggingStation.MyAntenna,
		MyCity:          qso.LoggingStation.MyCity,
		MyCountry:       qso.LoggingStation.MyCountry,
		MyCqZone:        qso.LoggingStation.MyCqZone,
		MyDXCC:          qso.LoggingStation.MyDXCC,
		MyGridsquare:    qso.LoggingStation.MyGridsquare,
		MyIota:          qso.LoggingStation.MyIota,
		MyIotaIslandID:  qso.LoggingStation.MyIotaIslandID,
		MyITUZone:       qso.LoggingStation.MyITUZone,
		MyLat:           qso.LoggingStation.MyLat,
		MyLon:           qso.LoggingStation.MyLon,
		MyMorseKeyInfo:  qso.LoggingStation.MyMorseKeyInfo,
		MyMorseKeyType:  qso.LoggingStation.MyMorseKeyType,
		MyName:          qso.LoggingStation.MyName,
		MyPostalCode:    qso.LoggingStation.MyPostalCode,
		MyRig:           qso.LoggingStation.MyRig,
		MySig:           qso.LoggingStation.MySig,
		MySigInfo:       qso.LoggingStation.MySigInfo,
		MyStreet:        qso.LoggingStation.MyStreet,
		MyWwffRef:       qso.LoggingStation.MyWwffRef,
		Operator:        qso.LoggingStation.Operator,
		OwnerCallsign:   qso.LoggingStation.OwnerCallsign,
		StationCallsign: qso.LoggingStation.StationCallsign,
	}

	jsonData, err := json.Marshal(additionalData)
	if err != nil {
		return models.Qso{}, err
	}

	if len(jsonData) == 0 {
		jsonData = []byte("{}")
	}

	return models.Qso{
		ID:        qso.ID,
		LogbookID: qso.LogbookID,
		SessionID: qso.SessionID,

		// Field name matches between types and models
		Call:           qso.ContactedStation.Call,
		Band:           qso.QsoDetails.Band,
		Mode:           qso.QsoDetails.Mode,
		Freq:           freqHz,
		QsoDate:        date,
		TimeOn:         timeOn,
		TimeOff:        timeOff,
		RstSent:        qso.QsoDetails.RstSent,
		RstRcvd:        qso.QsoDetails.RstRcvd,
		Country:        qso.ContactedStation.Country,
		AdditionalData: jsonData,
	}, nil
}

func ContactedStationTypeToModel(station types.ContactedStation) (models.ContactedStation, error) {
	additionalData := types.ContactedStationAdditionalData{
		Address:      station.Address,
		Age:          station.Age,
		Altitude:     station.Altitude,
		Cont:         station.Cont,
		ContactedOp:  station.ContactedOp,
		CQZ:          station.CQZ,
		DXCC:         station.DXCC,
		Email:        station.Email,
		EqCall:       station.EqCall,
		Gridsquare:   station.Gridsquare,
		Iota:         station.Iota,
		IotaIslandId: station.IotaIslandId,
		ITUZ:         station.ITUZ,
		Lat:          station.Lat,
		Lon:          station.Lon,
		QTH:          station.QTH,
		Sig:          station.Sig,
		SigInfo:      station.SigInfo,
		Web:          station.Web,
		WwffRef:      station.WwffRef,
	}

	jsonData, err := json.Marshal(additionalData)
	if err != nil {
		return models.ContactedStation{}, err
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
	var apiKey null.String
	if logbook.APIKey != "" {
		apiKey = null.StringFrom(logbook.APIKey)
	}

	return models.Logbook{
		ID:          logbook.ID,
		Name:        logbook.Name,
		Callsign:    logbook.Callsign,
		APIKey:      apiKey,
		Description: null.StringFrom(logbook.Description),
	}, nil
}
