package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeToModelFreqConverter(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    int64
		wantErr bool
	}{
		{
			name:    "valid MHz frequency",
			input:   "14.320",
			want:    14320000,
			wantErr: false,
		},
		{
			name:    "valid MHz frequency with decimals",
			input:   "7.074",
			want:    7074000,
			wantErr: false,
		},
		{
			name:    "whole number MHz",
			input:   "144",
			want:    144000000,
			wantErr: false,
		},
		{
			name:    "very low frequency",
			input:   "0.137",
			want:    137000,
			wantErr: false,
		},
		{
			name:    "high VHF frequency",
			input:   "146.520",
			want:    146520000,
			wantErr: false,
		},
		{
			name:    "frequency requiring rounding",
			input:   "14.3205",
			want:    14320500,
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid string",
			input:   "not a number",
			want:    0,
			wantErr: true,
		},
		{
			name:    "non-string input",
			input:   123,
			want:    0,
			wantErr: true,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TypeToModelFreqConverter(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModelToTypeFreqConverter(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:    "valid Hz frequency",
			input:   int64(14320000),
			want:    "14.320",
			wantErr: false,
		},
		{
			name:    "valid Hz frequency with more precision",
			input:   int64(7074000),
			want:    "7.074",
			wantErr: false,
		},
		{
			name:    "whole MHz",
			input:   int64(144000000),
			want:    "144.000",
			wantErr: false,
		},
		{
			name:    "VHF frequency",
			input:   int64(146520000),
			want:    "146.520",
			wantErr: false,
		},
		{
			name:    "zero frequency",
			input:   int64(0),
			want:    "0.000",
			wantErr: false,
		},
		{
			name:    "non-int64 input",
			input:   "14.320",
			want:    "",
			wantErr: true,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    "",
			wantErr: true,
		},
		{
			name:    "int input (should be converted)",
			input:   14320000,
			want:    "14.320",
			wantErr: false,
		},
		{
			name:    "float64 integer value",
			input:   float64(14320000),
			want:    "14.320",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ModelToTypeFreqConverter(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFrequencyRoundTrip(t *testing.T) {
	testCases := []string{
		"14.320",
		"7.074",
		"144.520",
		"50.313",
		"1.840",
	}

	for _, freq := range testCases {
		t.Run(freq, func(t *testing.T) {
			// Convert to model
			hz, err := TypeToModelFreqConverter(freq)
			require.NoError(t, err)

			// Convert back to type
			mhz, err := ModelToTypeFreqConverter(hz)
			require.NoError(t, err)

			assert.Equal(t, freq, mhz)
		})
	}
}
