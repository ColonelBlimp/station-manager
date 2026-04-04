package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- QsoAdditionalData ---

func TestQsoAdditionalData_EmptyMarshal(t *testing.T) {
	data := QsoAdditionalData{}
	b, err := json.Marshal(data)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(b))
}

func TestQsoAdditionalData_PartialMarshal(t *testing.T) {
	data := QsoAdditionalData{
		Comment: "test comment",
		Notes:   "test notes",
	}
	b, err := json.Marshal(data)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.Equal(t, "test comment", result["comment"])
	assert.Equal(t, "test notes", result["notes"])
	assert.NotContains(t, result, "band_rx") // omitted when empty
}

func TestQsoAdditionalData_RoundTrip(t *testing.T) {
	original := QsoAdditionalData{
		SmQsoUploadDate:     "20240101",
		SmQsoUploadStatus:   "Y",
		SmFwrdByEmailDate:   "20240102",
		SmFwrdByEmailStatus: "Y",
		QrzComUploadDate:    "20240103",
		QrzComUploadStatus:  "Y",
		Comment:             "hello",
		Rig:                 "Yaesu FT-991A",
		Operator:            "W1AW",
		StationCallsign:     "W1AW/1",
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded QsoAdditionalData
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- ContactedStationAdditionalData ---

func TestContactedStationAdditionalData_EmptyMarshal(t *testing.T) {
	data := ContactedStationAdditionalData{}
	b, err := json.Marshal(data)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(b))
}

func TestContactedStationAdditionalData_RoundTrip(t *testing.T) {
	original := ContactedStationAdditionalData{
		Address:    "1 Ham Radio Way",
		Gridsquare: "FN20",
		Web:        "https://example.com",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ContactedStationAdditionalData
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- ContactedStation ---

func TestContactedStation_CSIDJsonKey(t *testing.T) {
	cs := ContactedStation{CSID: 42, Name: "John"}
	b, err := json.Marshal(cs)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.Equal(t, float64(42), result["csid"], "CSID must use json key 'csid'")
	assert.NotContains(t, result, "id", "ContactedStation must not have an 'id' key")
}

func TestContactedStation_OmitEmptyCallAndCountry(t *testing.T) {
	cs := ContactedStation{Name: "John"}
	b, err := json.Marshal(cs)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "call", "empty Call should be omitted")
	assert.NotContains(t, result, "country", "empty Country should be omitted")
}

func TestContactedStation_OmitEmptyCallAndCountry_WhenSet(t *testing.T) {
	cs := ContactedStation{Call: "W1AW", Country: "United States"}
	b, err := json.Marshal(cs)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.Equal(t, "W1AW", result["call"])
	assert.Equal(t, "United States", result["country"])
}

func TestContactedStation_RoundTrip(t *testing.T) {
	original := ContactedStation{
		CSID:       99,
		Call:       "VK2XYZ",
		Country:    "Australia",
		Name:       "Bob",
		Gridsquare: "QF56",
		DXCC:       "150",
		CQZ:        "29",
		ITUZ:       "55",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ContactedStation
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- Qso ---

func TestQso_EmbeddedFieldsFlattened(t *testing.T) {
	qso := Qso{
		ID:        1,
		LogbookID: 10,
		SessionID: 5,
		QsoDetails: QsoDetails{
			Band: "40m",
			Mode: "SSB",
		},
		ContactedStation: ContactedStation{
			CSID: 20,
			Call: "G3XYZ",
		},
	}
	b, err := json.Marshal(qso)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))

	// Top-level Qso fields
	assert.Equal(t, float64(1), result["id"])
	assert.Equal(t, float64(10), result["logbook_id"])
	assert.Equal(t, float64(5), result["session_id"])

	// Embedded QsoDetails fields promoted to top level
	assert.Equal(t, "40m", result["band"])
	assert.Equal(t, "SSB", result["mode"])

	// Embedded ContactedStation fields promoted; CSID uses "csid"
	assert.Equal(t, float64(20), result["csid"])
	assert.Equal(t, "G3XYZ", result["call"])

	// "id" must only be the Qso.ID, not ContactedStation.CSID
	assert.Equal(t, float64(1), result["id"])
}

func TestQso_RoundTrip(t *testing.T) {
	original := Qso{
		ID:        1,
		LogbookID: 2,
		SessionID: 3,
		QsoDetails: QsoDetails{
			Band:    "20m",
			Mode:    "CW",
			Freq:    "14025.0",
			QsoDate: "20240315",
			TimeOn:  "1200",
		},
		ContactedStation: ContactedStation{
			CSID:    10,
			Call:    "DL1ABC",
			Country: "Germany",
			Name:    "Klaus",
		},
		LoggingStation: LoggingStation{
			StationCallsign: "W1AW",
			Operator:        "N1XYZ",
		},
		Qsl: Qsl{
			QslRcvd: "Y",
			QslSent: "Y",
		},
	}

	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Qso
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- QsoSlice ---

func TestQsoSlice_RoundTrip(t *testing.T) {
	original := QsoSlice{
		{ID: 1, LogbookID: 1, SessionID: 1},
		{ID: 2, LogbookID: 1, SessionID: 1},
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded QsoSlice
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Len(t, decoded, 2)
	assert.Equal(t, int64(1), decoded[0].ID)
	assert.Equal(t, int64(2), decoded[1].ID)
}

// --- Logbook ---

func TestLogbook_OmitEmptyFields(t *testing.T) {
	lb := Logbook{ID: 1, Name: "My Logbook", Callsign: "W1AW"}
	b, err := json.Marshal(lb)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "user_id", "zero UserID should be omitted")
	assert.NotContains(t, result, "api_key", "empty APIKey should be omitted")
	assert.NotContains(t, result, "description", "empty Description should be omitted")
}

func TestLogbook_RoundTrip(t *testing.T) {
	original := Logbook{
		ID:          5,
		UserID:      2,
		Name:        "Contest Log",
		Callsign:    "K1TTT",
		APIKey:      "abc123",
		Description: "My contest logbook",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Logbook
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- LogbookList ---

func TestLogbookList_RoundTrip(t *testing.T) {
	original := LogbookList{
		{ID: 1, Name: "Log A", Callsign: "W1A"},
		{ID: 2, Name: "Log B", Callsign: "W1B"},
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded LogbookList
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- User ---

func TestUser_RoundTrip(t *testing.T) {
	original := User{
		ID:             1,
		Callsign:       "W1AW",
		PassHash:       "hashed_password",
		Email:          "op@example.com",
		EmailConfirmed: true,
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded User
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

func TestUser_OmitOIDCFields(t *testing.T) {
	u := User{ID: 1, Callsign: "W1AW", PassHash: "h", Email: "a@b.com"}
	b, err := json.Marshal(u)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "issuer")
	assert.NotContains(t, result, "subject")
}

// --- ApiKey ---

func TestApiKey_RoundTrip(t *testing.T) {
	original := ApiKey{
		ID:        1,
		LogbookID: 2,
		KeyName:   "Default",
		KeyHash:   "sha256hash",
		KeyPrefix: "abc",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ApiKey
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- Country ---

func TestCountry_RoundTrip(t *testing.T) {
	original := Country{
		Name:              "United States",
		Prefix:            "K",
		Ccode:             "US",
		Continent:         "NA",
		CQZone:            "5",
		ITUZone:           "8",
		DXCCPrefix:        "K",
		ShortPathDistance: "0",
		IsNewEntity:       false,
		LocalTime:         "UTC-5",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Country
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- Qsl ---

func TestQsl_RoundTrip(t *testing.T) {
	original := Qsl{
		QslMsg:     "Thanks for QSO",
		QslRcvd:    "Y",
		QslSent:    "Y",
		QslRDate:   "20240101",
		QslSDate:   "20240101",
		QslRcvdVia: "B",
		QslSendVia: "B",
		QslVia:     "W1XYZ",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Qsl
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- QsoDetails ---

func TestQsoDetails_OmitEmptyKeyFields(t *testing.T) {
	d := QsoDetails{} // all empty
	b, err := json.Marshal(d)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "band")
	assert.NotContains(t, result, "mode")
	assert.NotContains(t, result, "freq")
	assert.NotContains(t, result, "qso_date")
	assert.NotContains(t, result, "time_on")
	assert.NotContains(t, result, "time_off")
	assert.NotContains(t, result, "rst_sent")
	assert.NotContains(t, result, "rst_rcvd")
}

func TestQsoDetails_RoundTrip(t *testing.T) {
	original := QsoDetails{
		Band:    "20m",
		Mode:    "FT8",
		Freq:    "14074.0",
		QsoDate: "20240315",
		TimeOn:  "1200",
		TimeOff: "1205",
		RstSent: "59",
		RstRcvd: "59",
		Comment: "Good signal",
		Notes:   "First contact",
		TxPwr:   "100",
		Rig:     "Icom IC-7300",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded QsoDetails
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- LoggingStation ---

func TestLoggingStation_AntAzOmitWhenEmpty(t *testing.T) {
	ls := LoggingStation{StationCallsign: "W1AW"}
	b, err := json.Marshal(ls)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "ant_az")
}

func TestLoggingStation_RoundTrip(t *testing.T) {
	original := LoggingStation{
		StationCallsign: "W1AW",
		Operator:        "N1XYZ",
		OwnerCallsign:   "W1AW",
		MyGridsquare:    "FN20",
		MyCountry:       "United States",
		MyCqZone:        "5",
		MyITUZone:       "8",
		AntennaAzimuth:  "45",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded LoggingStation
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- ContactHistory ---

func TestContactHistory_RoundTrip(t *testing.T) {
	original := ContactHistory{
		ID:      1,
		Band:    "20m",
		Freq:    "14025.0",
		Mode:    "CW",
		QsoDate: "20240315",
		TimeOn:  "1200",
		Name:    "Klaus",
		Country: "Germany",
		Call:    "DL1ABC",
		RstSent: "599",
		RstRcvd: "599",
		Notes:   "Strong signal",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ContactHistory
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- PostRequest ---

func TestPostRequest_NilOptionalFields(t *testing.T) {
	req := PostRequest{Callsign: "W1AW", Key: "apikey123"}
	b, err := json.Marshal(req)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "logbook")
	assert.NotContains(t, result, "qso")
}

func TestPostRequest_WithOptionalFields(t *testing.T) {
	req := PostRequest{
		Callsign: "W1AW",
		Key:      "apikey123",
		Logbook:  &Logbook{ID: 1, Name: "My Log", Callsign: "W1AW"},
		Qso:      &Qso{ID: 1, LogbookID: 1, SessionID: 1},
	}
	b, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded PostRequest
	require.NoError(t, json.Unmarshal(b, &decoded))
	require.NotNil(t, decoded.Logbook)
	require.NotNil(t, decoded.Qso)
	assert.Equal(t, int64(1), decoded.Logbook.ID)
	assert.Equal(t, int64(1), decoded.Qso.ID)
}

// --- ServerConfig ---

func TestServerConfig_RoundTrip(t *testing.T) {
	original := ServerConfig{
		Name:         "station-manager",
		Host:         "localhost",
		Port:         8080,
		TLSEnabled:   false,
		ReadTimeout:  30,
		WriteTimeout: 30,
		IdleTimeout:  60,
		BodyLimit:    4096,
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ServerConfig
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- LoggingConfig ---

func TestLoggingConfig_RoundTrip(t *testing.T) {
	original := LoggingConfig{
		Level:             "info",
		SkipFrameCount:    0,
		ConsoleLogging:    true,
		FileLogging:       true,
		RelLogFileDir:     "./logs",
		LogFileMaxBackups: 3,
		LogFileMaxAgeDays: 30,
		LogFileMaxSizeMB:  10,
		WithTimestamp:     true,
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded LoggingConfig
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- DatastoreConfig ---

func TestDatastoreConfig_RoundTrip_Postgres(t *testing.T) {
	original := DatastoreConfig{
		Driver:                    PostgresDriverName,
		Host:                      "localhost",
		Port:                      5432,
		User:                      "dbuser",
		Password:                  "secret",
		Database:                  "stationdb",
		SSLMode:                   "disable",
		MaxOpenConns:              10,
		MaxIdleConns:              5,
		ConnMaxLifetime:           30,
		ConnMaxIdleTime:           10,
		ContextTimeout:            5,
		TransactionContextTimeout: 10,
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded DatastoreConfig
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

func TestDatastoreConfig_RoundTrip_Sqlite(t *testing.T) {
	original := DatastoreConfig{
		Driver:                    SqliteDriverName,
		Path:                      "/var/db/station.db",
		Options:                   map[string]string{"mode": "rwc"},
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            5,
		TransactionContextTimeout: 5,
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded DatastoreConfig
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- EmailConfig ---

func TestEmailConfig_RoundTrip(t *testing.T) {
	original := EmailConfig{
		Name:               "alerts",
		Enabled:            true,
		Username:           "smtp_user",
		Password:           "smtp_pass",
		Host:               "smtp.example.com",
		Port:               587,
		From:               "from@example.com",
		To:                 "to@example.com",
		Subject:            "QSO Forward",
		Body:               "See attached",
		SmtpDialTimeoutSec: 10,
		SmtpRetryCount:     3,
		SmtpRetryDelaySec:  2,
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded EmailConfig
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- ForwarderConfig ---

func TestForwarderConfig_OmitEmptyAuth(t *testing.T) {
	cfg := ForwarderConfig{Name: "qrz", Enabled: true, URL: "https://logbook.qrz.com/api"}
	b, err := json.Marshal(cfg)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "apikey")
	assert.NotContains(t, result, "username")
	assert.NotContains(t, result, "password")
}

func TestForwarderConfig_RoundTrip(t *testing.T) {
	original := ForwarderConfig{
		Name:           "qrz",
		Enabled:        true,
		URL:            "https://logbook.qrz.com/api",
		APIKey:         "mykey",
		Username:       "user",
		Password:       "pass",
		UserAgent:      "station-manager/1.0",
		HttpTimeoutSec: 30,
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ForwarderConfig
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- LookupConfig ---

func TestLookupConfig_OmitEmptyAuth(t *testing.T) {
	cfg := LookupConfig{Name: "hamdb", Enabled: true, URL: "https://hamdb.org/api"}
	b, err := json.Marshal(cfg)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "username")
	assert.NotContains(t, result, "password")
	assert.NotContains(t, result, "view_url")
}

// --- ListenerConfig ---

func TestListenerConfig_RoundTrip(t *testing.T) {
	original := ListenerConfig{
		Name:       "wsjtx-listener",
		Enabled:    true,
		Host:       "localhost",
		Port:       2237,
		Protocol:   "UDP",
		BufferSize: 1500,
		Handler:    "wsjtx",
		HandlerConfig: map[string]any{
			"enable_decode": true,
		},
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ListenerConfig
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Protocol, decoded.Protocol)
	assert.Equal(t, original.Handler, decoded.Handler)
	assert.Equal(t, true, decoded.HandlerConfig["enable_decode"])
}

func TestListenerConfig_OmitEmptyOptionalFields(t *testing.T) {
	cfg := ListenerConfig{
		Name:       "n1mm",
		Enabled:    true,
		Host:       "localhost",
		Port:       12060,
		Protocol:   "UDP",
		BufferSize: 2048,
	}
	b, err := json.Marshal(cfg)
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(b, &result))
	assert.NotContains(t, result, "handler")
	assert.NotContains(t, result, "handler_config")
}

// --- RequiredConfigs ---

func TestRequiredConfigs_RoundTrip(t *testing.T) {
	original := RequiredConfigs{
		SetupComplete:                true,
		DefaultLogbookID:             1,
		DefaultRigID:                 2,
		DefaultFreq:                  "14025.0",
		DefaultMode:                  "CW",
		DefaultIsRandomQso:           false,
		PowerMultiplier:              1,
		DefaultTxPower:               100,
		UsePowerMultiplier:           false,
		QsoForwardingPollIntervalSec: 60,
		QsoForwardingWorkerCount:     2,
		QsoForwardingQueueSize:       100,
		QsoForwardingRowLimit:        50,
		DatabaseWriteQueueSize:       10,
		PaginationPageSize:           25,
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded RequiredConfigs
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- UiConfig ---

func TestUiConfig_RoundTrip(t *testing.T) {
	original := UiConfig{
		DefaultRigID:       1,
		Logbook:            Logbook{ID: 1, Name: "My Log", Callsign: "W1AW"},
		RigName:            "Icom IC-7300",
		DefaultFreq:        "14025.0",
		DefaultMode:        "CW",
		DefaultIsRandomQso: false,
		UsePowerMultiplier: false,
		PowerMultiplier:    1,
		DefaultTxPower:     100,
		DefaultFwdEmail:    "op@example.com",
		OwnerCallsign:      "W1AW",
		PaginationPageSize: 25,
		QrzViewUrl:         "https://www.qrz.com/db/",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded UiConfig
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- OptionalConfigs ---

func TestOptionalConfigs_RoundTrip(t *testing.T) {
	original := OptionalConfigs{QrzViewUrl: "https://www.qrz.com/db/"}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded OptionalConfigs
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original, decoded)
}

// --- QsoUpload ---

func TestQsoUpload_RoundTrip(t *testing.T) {
	original := QsoUpload{
		ID:      1,
		QsoID:   42,
		Service: "qrz",
		Action:  "upload",
		Status:  "pending",
	}
	b, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded QsoUpload
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.QsoID, decoded.QsoID)
	assert.Equal(t, original.Service, decoded.Service)
	assert.Equal(t, original.Status, decoded.Status)
}
