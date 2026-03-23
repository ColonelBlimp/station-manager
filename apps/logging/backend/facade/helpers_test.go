package facade

import (
	"testing"

	"github.com/Station-Manager/types"
)

func TestParseCallsign(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple callsign",
			input:    "W1AW",
			expected: "W1AW",
		},
		{
			name:     "callsign with whitespace",
			input:    "  W1AW  ",
			expected: "W1AW",
		},
		{
			name:     "callsign with portable",
			input:    "W1AW/P",
			expected: "W1AW",
		},
		{
			name:     "callsign with PORTABLE",
			input:    "W1AW/PORTABLE",
			expected: "W1AW",
		},
		{
			name:     "callsign with mobile",
			input:    "W1AW/M",
			expected: "W1AW",
		},
		{
			name:     "callsign with MOBILE",
			input:    "W1AW/MOBILE",
			expected: "W1AW",
		},
		{
			name:     "callsign with MM (maritime mobile)",
			input:    "W1AW/MM",
			expected: "W1AW",
		},
		{
			name:     "callsign with QRP",
			input:    "W1AW/QRP",
			expected: "W1AW",
		},
		{
			name:     "callsign with QRO",
			input:    "W1AW/QRO",
			expected: "W1AW",
		},
		{
			name:     "callsign with AM",
			input:    "W1AW/AM",
			expected: "W1AW",
		},
		{
			name:     "callsign with PM",
			input:    "W1AW/PM",
			expected: "W1AW",
		},
		{
			name:     "callsign with area number",
			input:    "W1AW/5",
			expected: "W1AW/5",
		},
		{
			name:     "callsign with country prefix",
			input:    "DL/W1AW",
			expected: "DL/W1AW",
		},
		{
			name:     "callsign with prefix and portable",
			input:    "DL/W1AW/P",
			expected: "DL/W1AW",
		},
		{
			name:     "callsign with multiple trailing modifiers",
			input:    "W1AW/QRP/P",
			expected: "W1AW",
		},
		{
			name:     "callsign with mixed case modifier",
			input:    "W1AW/portable",
			expected: "W1AW",
		},
		{
			name:     "callsign with whitespace in modifier",
			input:    "W1AW/ P ",
			expected: "W1AW",
		},
		{
			name:     "empty callsign",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "unknown modifier preserved",
			input:    "W1AW/KP4",
			expected: "W1AW/KP4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.parseCallsign(tt.input)
			if result != tt.expected {
				t.Errorf("parseCallsign(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMergeCountryIntoContactedStation(t *testing.T) {
	tests := []struct {
		name    string
		station *types.ContactedStation
		country types.Country
		wantErr bool
	}{
		{
			name: "valid merge",
			station: &types.ContactedStation{
				Call: "W1AW",
			},
			country: types.Country{
				Name:      "United States",
				Continent: "NA",
			},
			wantErr: false,
		},
		{
			name:    "nil station",
			station: nil,
			country: types.Country{
				Name:      "United States",
				Continent: "NA",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mergeCountryIntoContactedStation(tt.station, tt.country)
			if (err != nil) != tt.wantErr {
				t.Errorf("mergeCountryIntoContactedStation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.station != nil {
				if tt.station.Country != tt.country.Name {
					t.Errorf("Country = %v, want %v", tt.station.Country, tt.country.Name)
				}
				if tt.station.Cont != tt.country.Continent {
					t.Errorf("Continent = %v, want %v", tt.station.Cont, tt.country.Continent)
				}
			}
		})
	}
}

func TestMergeIntoQso(t *testing.T) {
	tests := []struct {
		name    string
		qso     *types.Qso
		country types.Country
		history []types.ContactHistory
		wantErr bool
	}{
		{
			name: "valid merge with history",
			qso: &types.Qso{
				QsoDetails: types.QsoDetails{},
			},
			country: types.Country{
				Name:      "United States",
				Continent: "NA",
			},
			history: []types.ContactHistory{
				{Band: "20m", Mode: "SSB"},
			},
			wantErr: false,
		},
		{
			name: "valid merge without history",
			qso: &types.Qso{
				QsoDetails: types.QsoDetails{},
			},
			country: types.Country{
				Name:      "Germany",
				Continent: "EU",
			},
			history: nil,
			wantErr: false,
		},
		{
			name:    "nil qso",
			qso:     nil,
			country: types.Country{},
			history: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mergeIntoQso(tt.qso, tt.country, tt.history)
			if (err != nil) != tt.wantErr {
				t.Errorf("mergeIntoQso() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.qso != nil {
				if tt.qso.CountryDetails.Name != tt.country.Name {
					t.Errorf("CountryDetails.Name = %v, want %v", tt.qso.CountryDetails.Name, tt.country.Name)
				}
				if tt.history == nil && tt.qso.ContactHistory == nil {
					t.Error("ContactHistory should not be nil when nil history is passed")
				}
				if tt.history == nil && len(tt.qso.ContactHistory) != 0 {
					t.Errorf("ContactHistory length = %d, want 0", len(tt.qso.ContactHistory))
				}
			}
		})
	}
}

func TestIsAllNumbers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "all digits",
			input:    "123456",
			expected: true,
		},
		{
			name:     "single digit",
			input:    "5",
			expected: true,
		},
		{
			name:     "with letters",
			input:    "12A34",
			expected: false,
		},
		{
			name:     "with special chars",
			input:    "123-456",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "spaces",
			input:    "123 456",
			expected: false,
		},
		{
			name:     "decimal point",
			input:    "123.456",
			expected: false,
		},
		{
			name:     "negative number",
			input:    "-123",
			expected: false,
		},
		{
			name:     "all zeros",
			input:    "000",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAllNumbers(tt.input)
			if result != tt.expected {
				t.Errorf("isAllNumbers(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
