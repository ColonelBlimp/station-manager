package converters

import (
	"testing"
	"time"

	"github.com/Station-Manager/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckString(t *testing.T) {
	op := errors.Op("test.CheckString")

	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:    "valid string",
			input:   "test string",
			want:    "test string",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string (int)",
			input:   123,
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string (nil)",
			input:   nil,
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string (bool)",
			input:   true,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckString(op, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckFloat64(t *testing.T) {
	op := errors.Op("test.CheckFloat64")

	tests := []struct {
		name    string
		input   interface{}
		want    float64
		wantErr bool
	}{
		{
			name:    "valid float64",
			input:   123.45,
			want:    123.45,
			wantErr: false,
		},
		{
			name:    "zero float64",
			input:   0.0,
			want:    0,
			wantErr: true,
		},
		{
			name:    "non-float64 (int)",
			input:   123,
			want:    0,
			wantErr: true,
		},
		{
			name:    "non-float64 (string)",
			input:   "123.45",
			want:    0,
			wantErr: true,
		},
		{
			name:    "non-float64 (nil)",
			input:   nil,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckFloat64(op, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckInt64(t *testing.T) {
	op := errors.Op("test.CheckInt64")

	tests := []struct {
		name    string
		input   interface{}
		want    int64
		wantErr bool
	}{
		{
			name:    "valid int64",
			input:   int64(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "int",
			input:   123,
			want:    123,
			wantErr: false,
		},
		{
			name:    "int32",
			input:   int32(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "int16",
			input:   int16(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "int8",
			input:   int8(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "uint",
			input:   uint(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "uint64",
			input:   uint64(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "uint32",
			input:   uint32(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "uint16",
			input:   uint16(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "uint8",
			input:   uint8(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "float64 with integer value",
			input:   float64(123),
			want:    123,
			wantErr: false,
		},
		{
			name:    "float64 from JSON unmarshalling",
			input:   float64(14320000),
			want:    14320000,
			wantErr: false,
		},
		{
			name:    "float64 with decimal (invalid)",
			input:   123.45,
			want:    -1,
			wantErr: true,
		},
		{
			name:    "non-integer (string)",
			input:   "123",
			want:    -1,
			wantErr: true,
		},
		{
			name:    "non-integer (nil)",
			input:   nil,
			want:    -1,
			wantErr: true,
		},
		{
			name:    "non-integer (bool)",
			input:   true,
			want:    -1,
			wantErr: true,
		},
		{
			name:    "negative int64",
			input:   int64(-123),
			want:    -123,
			wantErr: false,
		},
		{
			name:    "zero int64",
			input:   int64(0),
			want:    0,
			wantErr: false,
		},
		{
			name:    "large int64",
			input:   int64(9223372036854775807),
			want:    9223372036854775807,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckInt64(op, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckTime(t *testing.T) {
	op := errors.Op("test.CheckTime")

	now := time.Now()
	zeroTime := time.Time{}

	tests := []struct {
		name    string
		input   interface{}
		want    time.Time
		wantErr bool
	}{
		{
			name:    "valid time.Time",
			input:   now,
			want:    now,
			wantErr: false,
		},
		{
			name:    "zero time.Time",
			input:   zeroTime,
			want:    zeroTime,
			wantErr: false,
		},
		{
			name:    "non-time.Time (string)",
			input:   "2025-11-08",
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "non-time.Time (int)",
			input:   123,
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "non-time.Time (nil)",
			input:   nil,
			want:    time.Time{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckTime(op, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test CheckInt64 with JSON unmarshalling scenario
func TestCheckInt64_JSONUnmarshalling(t *testing.T) {
	op := errors.Op("test.CheckInt64_JSONUnmarshalling")

	// Simulate JSON unmarshalling where numbers become float64
	jsonNumber := float64(14320000)

	result, err := CheckInt64(op, jsonNumber)
	require.NoError(t, err)
	assert.Equal(t, int64(14320000), result)
}
