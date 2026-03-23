package adapters

import (
	"testing"

	"github.com/Station-Manager/database/sqlite/models"
	"github.com/Station-Manager/types"
	"github.com/aarondl/null/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ContactedStationModelToType Tests
// =============================================================================
func TestContactedStationModelToType_NilModel(t *testing.T) {
	result, err := ContactedStationModelToType(nil)
	assert.Error(t, err)
	assert.Equal(t, types.ContactedStation{}, result)
	assert.Contains(t, err.Error(), errMsgNilModel)
}
func TestContactedStationModelToType_ValidModel_MinimalData(t *testing.T) {
	model := &models.ContactedStation{
		ID:             123,
		Name:           "John Doe",
		Call:           "W1ABC",
		Country:        "United States",
		AdditionalData: []byte("{}"),
	}
	result, err := ContactedStationModelToType(model)
	require.NoError(t, err)
	assert.Equal(t, int64(123), result.CSID)
	assert.Equal(t, "John Doe", result.Name)
	assert.Equal(t, "W1ABC", result.Call)
	assert.Equal(t, "United States", result.Country)
}
func TestContactedStationModelToType_InvalidJSON(t *testing.T) {
	model := &models.ContactedStation{
		ID:             789,
		Name:           "Test",
		Call:           "TEST",
		Country:        "Test Country",
		AdditionalData: []byte("{invalid json"),
	}
	result, err := ContactedStationModelToType(model)
	assert.Error(t, err)
	assert.Equal(t, types.ContactedStation{}, result)
}

// =============================================================================
// CountryModelToType Tests
// =============================================================================
func TestCountryModelToType_NilModel(t *testing.T) {
	result, err := CountryModelToType(nil)
	assert.Error(t, err)
	assert.Equal(t, types.Country{}, result)
	assert.Contains(t, err.Error(), errMsgNilModel)
}
func TestCountryModelToType_ValidModel(t *testing.T) {
	model := &models.Country{
		ID:         1,
		Name:       "United States",
		Prefix:     "K",
		Continent:  "NA",
		Ccode:      "US",
		DXCCPrefix: "K",
		TimeOffset: "-5",
		CQZone:     "5",
		ItuZone:    "8",
	}
	result, err := CountryModelToType(model)
	require.NoError(t, err)
	assert.Equal(t, "United States", result.Name)
	assert.Equal(t, "K", result.Prefix)
	assert.Equal(t, "NA", result.Continent)
	assert.Equal(t, "US", result.Ccode)
	assert.Equal(t, "K", result.DXCCPrefix)
	assert.Equal(t, "-5", result.TimeOffset)
	assert.Equal(t, "5", result.CQZone)
	assert.Equal(t, "8", result.ITUZone)
}

// =============================================================================
// QsoModelToType Tests
// =============================================================================
func TestQsoModelToType_NilModel(t *testing.T) {
	result, err := QsoModelToType(nil)
	assert.Error(t, err)
	assert.Equal(t, types.Qso{}, result)
	assert.Contains(t, err.Error(), errMsgNilModel)
}
func TestQsoModelToType_ValidModel_MinimalData(t *testing.T) {
	model := &models.Qso{
		ID:             100,
		LogbookID:      1,
		SessionID:      10,
		Call:           "DL1ABC",
		Band:           "20m",
		Mode:           "SSB",
		Freq:           14250000,
		QsoDate:        "20250107",
		TimeOn:         "1430",
		TimeOff:        "1445",
		RstSent:        "59",
		RstRcvd:        "57",
		Country:        "Germany",
		AdditionalData: []byte("{}"),
	}
	result, err := QsoModelToType(model)
	require.NoError(t, err)
	assert.Equal(t, int64(100), result.ID)
	assert.Equal(t, int64(1), result.LogbookID)
	assert.Equal(t, int64(10), result.SessionID)
	assert.Equal(t, "DL1ABC", result.ContactedStation.Call)
	assert.Equal(t, "20m", result.QsoDetails.Band)
	assert.Equal(t, "SSB", result.QsoDetails.Mode)
	assert.Equal(t, "14250000", result.QsoDetails.Freq)
}
func TestQsoModelToType_InvalidJSON(t *testing.T) {
	model := &models.Qso{
		ID:             300,
		LogbookID:      1,
		SessionID:      1,
		Call:           "TEST",
		Band:           "20m",
		Mode:           "SSB",
		Freq:           14250000,
		QsoDate:        "20250107",
		TimeOn:         "1200",
		TimeOff:        "1215",
		RstSent:        "59",
		RstRcvd:        "59",
		Country:        "Test",
		AdditionalData: []byte("not valid json"),
	}
	result, err := QsoModelToType(model)
	assert.Error(t, err)
	assert.Equal(t, types.Qso{}, result)
}

// =============================================================================
// LogbookModelToType Tests
// =============================================================================
func TestLogbookModelToType_NilModel(t *testing.T) {
	result, err := LogbookModelToType(nil)
	assert.Error(t, err)
	assert.Equal(t, types.Logbook{}, result)
	assert.Contains(t, err.Error(), errMsgNilModel)
}
func TestLogbookModelToType_ValidModel_WithAPIKey(t *testing.T) {
	model := &models.Logbook{
		ID:          1,
		Name:        "My Logbook",
		Callsign:    "W1ABC",
		APIKey:      null.StringFrom("test-api-key-123"),
		Description: null.StringFrom("My primary logbook"),
	}
	result, err := LogbookModelToType(model)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.ID)
	assert.Equal(t, "My Logbook", result.Name)
	assert.Equal(t, "W1ABC", result.Callsign)
	assert.Equal(t, "test-api-key-123", result.APIKey)
	assert.Equal(t, "My primary logbook", result.Description)
}

// =============================================================================
// QsoTypeToModel Tests
// =============================================================================
func TestQsoTypeToModel_ValidQso_MinimalData(t *testing.T) {
	qso := types.Qso{
		ID:        500,
		LogbookID: 1,
		SessionID: 10,
		QsoDetails: types.QsoDetails{
			Band:    "20m",
			Mode:    "SSB",
			Freq:    "14250000",
			QsoDate: "20250107",
			TimeOn:  "1430",
			TimeOff: "1445",
			RstSent: "59",
			RstRcvd: "57",
		},
		ContactedStation: types.ContactedStation{
			Call:    "DL1ABC",
			Country: "Germany",
		},
	}
	result, err := QsoTypeToModel(qso)
	require.NoError(t, err)
	assert.Equal(t, int64(500), result.ID)
	assert.Equal(t, int64(1), result.LogbookID)
	assert.Equal(t, int64(10), result.SessionID)
	assert.Equal(t, "DL1ABC", result.Call)
	assert.Equal(t, "20m", result.Band)
	assert.Equal(t, "SSB", result.Mode)
	assert.Equal(t, int64(14250000), result.Freq)
	assert.Equal(t, "20250107", result.QsoDate)
	assert.NotEmpty(t, result.AdditionalData)
}
func TestQsoTypeToModel_DateNormalization_WithDashes(t *testing.T) {
	qso := types.Qso{
		LogbookID: 1,
		SessionID: 1,
		QsoDetails: types.QsoDetails{
			Band:    "20m",
			Mode:    "SSB",
			Freq:    "14250000",
			QsoDate: "2025-01-07",
			TimeOn:  "14:30",
			TimeOff: "14:45",
			RstSent: "59",
			RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{
			Call:    "TEST",
			Country: "Test",
		},
	}
	result, err := QsoTypeToModel(qso)
	require.NoError(t, err)
	assert.Equal(t, "20250107", result.QsoDate)
	assert.Equal(t, "1430", result.TimeOn)
	assert.Equal(t, "1445", result.TimeOff)
}
func TestQsoTypeToModel_InvalidFrequency(t *testing.T) {
	qso := types.Qso{
		LogbookID: 1,
		SessionID: 1,
		QsoDetails: types.QsoDetails{
			Band:    "20m",
			Mode:    "SSB",
			Freq:    "not-a-number",
			QsoDate: "20250107",
			TimeOn:  "1200",
			TimeOff: "1215",
			RstSent: "59",
			RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{
			Call:    "TEST",
			Country: "Test",
		},
	}
	result, err := QsoTypeToModel(qso)
	assert.Error(t, err)
	assert.Equal(t, models.Qso{}, result)
	assert.Contains(t, err.Error(), "failed to parse frequency")
}
func TestQsoTypeToModel_EmptyFrequency(t *testing.T) {
	qso := types.Qso{
		LogbookID: 1,
		SessionID: 1,
		QsoDetails: types.QsoDetails{
			Band:    "20m",
			Mode:    "SSB",
			Freq:    "",
			QsoDate: "20250107",
			TimeOn:  "1200",
			TimeOff: "1215",
			RstSent: "59",
			RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{
			Call:    "TEST",
			Country: "Test",
		},
	}
	result, err := QsoTypeToModel(qso)
	assert.Error(t, err)
	assert.Equal(t, models.Qso{}, result)
}

// =============================================================================
// ContactedStationTypeToModel Tests
// =============================================================================
func TestContactedStationTypeToModel_ValidStation_MinimalData(t *testing.T) {
	station := types.ContactedStation{
		CSID:    100,
		Name:    "John Doe",
		Call:    "W1ABC",
		Country: "United States",
	}
	result, err := ContactedStationTypeToModel(station)
	require.NoError(t, err)
	assert.Equal(t, int64(100), result.ID)
	assert.Equal(t, "John Doe", result.Name)
	assert.Equal(t, "W1ABC", result.Call)
	assert.Equal(t, "United States", result.Country)
	assert.NotEmpty(t, result.AdditionalData)
}

// =============================================================================
// CountryTypeToModel Tests
// =============================================================================
func TestCountryTypeToModel_ValidCountry(t *testing.T) {
	country := types.Country{
		ID:         1,
		Name:       "Germany",
		CQZone:     "14",
		ITUZone:    "28",
		Continent:  "EU",
		Prefix:     "DL",
		Ccode:      "DE",
		DXCCPrefix: "DL",
		TimeOffset: "+1",
	}
	result, err := CountryTypeToModel(country)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.ID)
	assert.Equal(t, "Germany", result.Name)
	assert.Equal(t, "14", result.CQZone)
	assert.Equal(t, "28", result.ItuZone)
	assert.Equal(t, "EU", result.Continent)
	assert.Equal(t, "DL", result.Prefix)
}

// =============================================================================
// LogbookTypeToModel Tests
// =============================================================================
func TestLogbookTypeToModel_ValidLogbook_WithAPIKey(t *testing.T) {
	logbook := types.Logbook{
		ID:          1,
		Name:        "My Logbook",
		Callsign:    "W1ABC",
		APIKey:      "secret-api-key",
		Description: "Primary logbook",
	}
	result, err := LogbookTypeToModel(logbook)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.ID)
	assert.Equal(t, "My Logbook", result.Name)
	assert.Equal(t, "W1ABC", result.Callsign)
	assert.True(t, result.APIKey.Valid)
	assert.Equal(t, "secret-api-key", result.APIKey.String)
}
func TestLogbookTypeToModel_ValidLogbook_WithoutAPIKey(t *testing.T) {
	logbook := types.Logbook{
		ID:          2,
		Name:        "Contest Log",
		Callsign:    "K1XYZ",
		APIKey:      "",
		Description: "For contests",
	}
	result, err := LogbookTypeToModel(logbook)
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.ID)
	assert.Equal(t, "Contest Log", result.Name)
	assert.Equal(t, "K1XYZ", result.Callsign)
	assert.False(t, result.APIKey.Valid)
}

// =============================================================================
// Round-Trip Tests
// =============================================================================
func TestQsoRoundTrip(t *testing.T) {
	original := types.Qso{
		ID:                1000,
		LogbookID:         5,
		SessionID:         50,
		SmQsoUploadDate:   "20250101",
		SmQsoUploadStatus: "uploaded",
		QsoDetails: types.QsoDetails{
			Band:    "20m",
			Mode:    "SSB",
			Freq:    "14250000",
			QsoDate: "20250107",
			TimeOn:  "1430",
			TimeOff: "1445",
			RstSent: "59",
			RstRcvd: "57",
			Comment: "Great signal",
		},
		ContactedStation: types.ContactedStation{
			Call:       "JA1ABC",
			Country:    "Japan",
			Name:       "Taro",
			Gridsquare: "PM95",
		},
		LoggingStation: types.LoggingStation{
			StationCallsign: "W1ABC",
			MyGridsquare:    "FN42",
		},
	}
	model, err := QsoTypeToModel(original)
	require.NoError(t, err)
	result, err := QsoModelToType(&model)
	require.NoError(t, err)
	assert.Equal(t, original.ID, result.ID)
	assert.Equal(t, original.LogbookID, result.LogbookID)
	assert.Equal(t, original.SessionID, result.SessionID)
	assert.Equal(t, original.ContactedStation.Call, result.ContactedStation.Call)
	assert.Equal(t, original.QsoDetails.Band, result.QsoDetails.Band)
	assert.Equal(t, original.SmQsoUploadDate, result.SmQsoUploadDate)
	assert.Equal(t, original.LoggingStation.StationCallsign, result.LoggingStation.StationCallsign)
}
func TestCountryRoundTrip(t *testing.T) {
	original := types.Country{
		ID:         99,
		Name:       "Brazil",
		CQZone:     "11",
		ITUZone:    "15",
		Continent:  "SA",
		Prefix:     "PY",
		Ccode:      "BR",
		DXCCPrefix: "PY",
		TimeOffset: "-3",
	}
	model, err := CountryTypeToModel(original)
	require.NoError(t, err)
	result, err := CountryModelToType(&model)
	require.NoError(t, err)
	assert.Equal(t, original.Name, result.Name)
	assert.Equal(t, original.CQZone, result.CQZone)
	assert.Equal(t, original.ITUZone, result.ITUZone)
	assert.Equal(t, original.Continent, result.Continent)
	assert.Equal(t, original.Prefix, result.Prefix)
}
func TestLogbookRoundTrip(t *testing.T) {
	original := types.Logbook{
		ID:          10,
		Name:        "DX Log",
		Callsign:    "ZL1ABC",
		APIKey:      "my-secret-key",
		Description: "For DX contacts",
	}
	model, err := LogbookTypeToModel(original)
	require.NoError(t, err)
	result, err := LogbookModelToType(&model)
	require.NoError(t, err)
	assert.Equal(t, original.ID, result.ID)
	assert.Equal(t, original.Name, result.Name)
	assert.Equal(t, original.Callsign, result.Callsign)
	assert.Equal(t, original.APIKey, result.APIKey)
	assert.Equal(t, original.Description, result.Description)
}
func TestContactedStationRoundTrip(t *testing.T) {
	original := types.ContactedStation{
		CSID:       500,
		Name:       "Test Operator",
		Call:       "VK3ABC",
		Country:    "Australia",
		Address:    "Melbourne",
		Gridsquare: "QF22",
		CQZ:        "30",
		Cont:       "OC",
	}
	model, err := ContactedStationTypeToModel(original)
	require.NoError(t, err)
	result, err := ContactedStationModelToType(&model)
	require.NoError(t, err)
	assert.Equal(t, original.CSID, result.CSID)
	assert.Equal(t, original.Name, result.Name)
	assert.Equal(t, original.Call, result.Call)
	assert.Equal(t, original.Country, result.Country)
	assert.Equal(t, original.Address, result.Address)
	assert.Equal(t, original.Gridsquare, result.Gridsquare)
}
