package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Service name constants ---

func TestServiceNameConstants(t *testing.T) {
	assert.Equal(t, "configservice", ConfigServiceName)
	assert.Equal(t, "databaseservice", DatabaseServiceName)
	assert.Equal(t, "sqliteservice", SqliteServiceName)
	assert.Equal(t, "loggingservice", LoggingServiceName)
	assert.Equal(t, "catservice", CatServiceName)
	assert.Equal(t, "hamnutlookupservice", HamNutLookupServiceName)
	assert.Equal(t, "qrzlookupservice", QrzLookupServiceName)
	assert.Equal(t, "qrzforwardingservice", QrzForwardingServiceName)
	assert.Equal(t, "emailservice", EmailServiceName)
	assert.Equal(t, "networklisteners", ListenersServiceName)
}

func TestServiceNameConstants_AreUnique(t *testing.T) {
	names := []string{
		ConfigServiceName,
		DatabaseServiceName,
		SqliteServiceName,
		LoggingServiceName,
		CatServiceName,
		HamNutLookupServiceName,
		QrzLookupServiceName,
		QrzForwardingServiceName,
		EmailServiceName,
		ListenersServiceName,
	}
	seen := make(map[string]bool)
	for _, name := range names {
		assert.False(t, seen[name], "duplicate service name: %s", name)
		seen[name] = true
	}
}

// --- Driver name constants ---

func TestDriverNameConstants(t *testing.T) {
	assert.Equal(t, "postgres", PostgresDriverName)
	assert.Equal(t, "sqlite", SqliteDriverName)
	assert.NotEqual(t, PostgresDriverName, SqliteDriverName)
}

// --- Type aliases ---

func TestADIFBand(t *testing.T) {
	var b ADIFBand = "20m"
	assert.Equal(t, ADIFBand("20m"), b)
	assert.Equal(t, "20m", string(b))
}

func TestADIFFreq(t *testing.T) {
	var f ADIFFreq = 14.025
	assert.Equal(t, ADIFFreq(14.025), f)
	assert.Equal(t, 14.025, float64(f))
}

func TestCatStatus(t *testing.T) {
	status := CatStatus{
		"freq": "14025000",
		"mode": "USB",
	}
	assert.Equal(t, "14025000", status["freq"])
	assert.Equal(t, "USB", status["mode"])
	assert.Equal(t, "", status["nonexistent"])
}

func TestStateValues(t *testing.T) {
	sv := StateValues{
		"mode": {
			"1": "LSB",
			"2": "USB",
			"3": "CW",
		},
	}
	assert.Equal(t, "LSB", sv["mode"]["1"])
	assert.Equal(t, "USB", sv["mode"]["2"])
	assert.Equal(t, "CW", sv["mode"]["3"])
}

// --- QsoSlice ---

func TestQsoSlice_IsSlice(t *testing.T) {
	var qs QsoSlice
	assert.Len(t, qs, 0)

	qs = append(qs, Qso{ID: 1})
	qs = append(qs, Qso{ID: 2})
	assert.Len(t, qs, 2)
	assert.Equal(t, int64(1), qs[0].ID)
	assert.Equal(t, int64(2), qs[1].ID)
}

// --- LogbookList ---

func TestLogbookList_IsSlice(t *testing.T) {
	var ll LogbookList
	assert.Len(t, ll, 0)

	ll = append(ll, Logbook{ID: 1, Name: "A"})
	ll = append(ll, Logbook{ID: 2, Name: "B"})
	assert.Len(t, ll, 2)
	assert.Equal(t, "A", ll[0].Name)
	assert.Equal(t, "B", ll[1].Name)
}

// --- CatCommand ---

func TestCatCommand_Fields(t *testing.T) {
	cmd := CatCommand{Name: "get_freq", Cmd: "FA;"}
	assert.Equal(t, "get_freq", cmd.Name)
	assert.Equal(t, "FA;", cmd.Cmd)
}

// --- CatState and Marker ---

func TestCatState_WithMarkers(t *testing.T) {
	state := CatState{
		Prefix: "FA",
		Data:   "FA00014025000;",
		Markers: []Marker{
			{
				Tag:    "freq",
				Index:  2,
				Length: 11,
				ValueMappings: []ValueMapping{
					{Key: "00014025000", Value: "14025000"},
				},
			},
		},
	}
	assert.Equal(t, "FA", state.Prefix)
	assert.Len(t, state.Markers, 1)
	assert.Equal(t, "freq", state.Markers[0].Tag)
	assert.Equal(t, 2, state.Markers[0].Index)
	assert.Equal(t, 11, state.Markers[0].Length)
	assert.Len(t, state.Markers[0].ValueMappings, 1)
	assert.Equal(t, "14025000", state.Markers[0].ValueMappings[0].Value)
}

// --- Qso composition ---

func TestQso_ComposedFields(t *testing.T) {
	qso := Qso{
		ID:        1,
		LogbookID: 10,
		SessionID: 5,
		QsoDetails: QsoDetails{
			Band:    "40m",
			Mode:    "SSB",
			QsoDate: "20240315",
		},
		ContactedStation: ContactedStation{
			CSID: 20,
			Call: "G3ABC",
			Name: "Bob",
		},
		LoggingStation: LoggingStation{
			StationCallsign: "W1AW",
			Operator:        "W1AW",
		},
		Qsl: Qsl{
			QslRcvd: "Y",
		},
	}

	// QsoDetails fields accessible via embedding
	assert.Equal(t, "40m", qso.Band)
	assert.Equal(t, "SSB", qso.Mode)
	assert.Equal(t, "20240315", qso.QsoDate)

	// ContactedStation fields accessible via embedding
	assert.Equal(t, int64(20), qso.CSID)
	assert.Equal(t, "G3ABC", qso.Call)
	assert.Equal(t, "Bob", qso.Name)

	// LoggingStation fields accessible via embedding
	assert.Equal(t, "W1AW", qso.StationCallsign)
	assert.Equal(t, "W1AW", qso.Operator)

	// Qsl fields accessible via embedding
	assert.Equal(t, "Y", qso.QslRcvd)
}

// --- Country ---

func TestCountry_IsNewEntityFlag(t *testing.T) {
	c := Country{Name: "Heard Island", IsNewEntity: true}
	assert.True(t, c.IsNewEntity)
}

// --- ContactHistory ---

func TestContactHistory_Fields(t *testing.T) {
	ch := ContactHistory{
		ID:      1,
		Band:    "20m",
		Mode:    "CW",
		Call:    "DL1ABC",
		RstSent: "599",
		RstRcvd: "599",
	}
	assert.Equal(t, int64(1), ch.ID)
	assert.Equal(t, "DL1ABC", ch.Call)
	assert.Equal(t, "599", ch.RstSent)
}
