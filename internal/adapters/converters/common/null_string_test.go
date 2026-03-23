package common

import (
	"testing"

	"github.com/aarondl/null/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeToModelStringConverter(t *testing.T) {
	tests := []struct {
		name      string
		input     interface{}
		wantValid bool
		wantValue string
		wantErr   bool
	}{
		{
			name:      "valid non-empty string",
			input:     "England",
			wantValid: true,
			wantValue: "England",
			wantErr:   false,
		},
		{
			name:      "empty string treated as null",
			input:     "",
			wantValid: false,
			wantValue: "",
			wantErr:   false,
		},
		{
			name:      "string with spaces",
			input:     "United States",
			wantValid: true,
			wantValue: "United States",
			wantErr:   false,
		},
		{
			name:      "non-string input (int)",
			input:     123,
			wantValid: false,
			wantValue: "",
			wantErr:   true,
		},
		{
			name:      "non-string input (nil)",
			input:     nil,
			wantValid: false,
			wantValue: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TypeToModelStringConverter(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			nullStr, ok := got.(null.String)
			require.True(t, ok, "result should be null.String")
			assert.Equal(t, tt.wantValid, nullStr.Valid)
			if tt.wantValid {
				assert.Equal(t, tt.wantValue, nullStr.String)
			}
		})
	}
}

func TestModelToTypeStringConverter(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:    "valid null.String",
			input:   null.StringFrom("England"),
			want:    "England",
			wantErr: false,
		},
		{
			name:    "invalid null.String (null)",
			input:   null.String{},
			want:    "",
			wantErr: false,
		},
		{
			name:    "plain string",
			input:   "United States",
			want:    "United States",
			wantErr: false,
		},
		{
			name:    "empty string plain",
			input:   "",
			want:    "",
			wantErr: false,
		},
		{
			name:    "non-string input",
			input:   123,
			want:    "",
			wantErr: true,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ModelToTypeStringConverter(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStringRoundTrip(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{"non-empty", "England"},
		{"with spaces", "United States"},
		{"with special chars", "São Paulo"},
		{"empty becomes null then empty", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Convert to model
			modelVal, err := TypeToModelStringConverter(tc.value)
			require.NoError(t, err)

			// Convert back to type
			typeVal, err := ModelToTypeStringConverter(modelVal)
			require.NoError(t, err)

			assert.Equal(t, tc.value, typeVal)
		})
	}
}

func TestNullStringRoundTrip(t *testing.T) {
	// Test round trip with invalid null.String
	modelVal := null.String{}

	// Convert to type
	typeVal, err := ModelToTypeStringConverter(modelVal)
	require.NoError(t, err)
	assert.Equal(t, "", typeVal, "invalid null.String should convert to empty string")
}
