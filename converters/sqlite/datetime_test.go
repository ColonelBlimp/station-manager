package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeToModelDateConverter(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:    "YYYY-MM-DD format",
			input:   "2025-11-08",
			want:    "20251108",
			wantErr: false,
		},
		{
			name:    "YYYYMMDD format",
			input:   "20251108",
			want:    "20251108",
			wantErr: false,
		},
		{
			name:    "leap year date",
			input:   "2024-02-29",
			want:    "20240229",
			wantErr: false,
		},
		{
			name:    "first day of year",
			input:   "2025-01-01",
			want:    "20250101",
			wantErr: false,
		},
		{
			name:    "last day of year",
			input:   "2025-12-31",
			want:    "20251231",
			wantErr: false,
		},
		{
			name:    "invalid date format (too short)",
			input:   "2025-11",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid date format (too long)",
			input:   "2025-11-089",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string input",
			input:   20251108,
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
			name:    "wrong separator",
			input:   "2025/11/08",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TypeToModelDateConverter(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModelToTypeDateConverter(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:    "valid YYYYMMDD",
			input:   "20251108",
			want:    "2025-11-08",
			wantErr: false,
		},
		{
			name:    "leap year",
			input:   "20240229",
			want:    "2024-02-29",
			wantErr: false,
		},
		{
			name:    "first day of year",
			input:   "20250101",
			want:    "2025-01-01",
			wantErr: false,
		},
		{
			name:    "last day of year",
			input:   "20251231",
			want:    "2025-12-31",
			wantErr: false,
		},
		{
			name:    "invalid length (too short)",
			input:   "2025110",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid length (too long)",
			input:   "202511088",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid date value",
			input:   "20251301",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string input",
			input:   20251108,
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
			got, err := ModelToTypeDateConverter(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDateRoundTrip(t *testing.T) {
	testCases := []string{
		"2025-11-08",
		"2024-02-29",
		"2025-01-01",
		"2025-12-31",
		"1999-12-31",
	}

	for _, date := range testCases {
		t.Run(date, func(t *testing.T) {
			// Convert to model (YYYYMMDD)
			modelDate, err := TypeToModelDateConverter(date)
			require.NoError(t, err)

			// Convert back to type (YYYY-MM-DD)
			typeDate, err := ModelToTypeDateConverter(modelDate)
			require.NoError(t, err)

			assert.Equal(t, date, typeDate)
		})
	}
}

func TestTypeToModelTimeConverter(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:    "HH:MM format",
			input:   "11:40",
			want:    "1140",
			wantErr: false,
		},
		{
			name:    "HHMM format",
			input:   "1140",
			want:    "1140",
			wantErr: false,
		},
		{
			name:    "midnight",
			input:   "00:00",
			want:    "0000",
			wantErr: false,
		},
		{
			name:    "end of day",
			input:   "23:59",
			want:    "2359",
			wantErr: false,
		},
		{
			name:    "noon",
			input:   "12:00",
			want:    "1200",
			wantErr: false,
		},
		{
			name:    "single digit hour with colon",
			input:   "09:30",
			want:    "0930",
			wantErr: false,
		},
		{
			name:    "invalid format (too short)",
			input:   "11:4",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid format (too long)",
			input:   "11:400",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid hour",
			input:   "25:00",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid minute",
			input:   "11:60",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string input",
			input:   1140,
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
			got, err := TypeToModelTimeConverter(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModelToTypeTimeConverter(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:    "valid HHMM",
			input:   "1140",
			want:    "11:40",
			wantErr: false,
		},
		{
			name:    "midnight",
			input:   "0000",
			want:    "00:00",
			wantErr: false,
		},
		{
			name:    "end of day",
			input:   "2359",
			want:    "23:59",
			wantErr: false,
		},
		{
			name:    "noon",
			input:   "1200",
			want:    "12:00",
			wantErr: false,
		},
		{
			name:    "early morning",
			input:   "0930",
			want:    "09:30",
			wantErr: false,
		},
		{
			name:    "invalid length (too short)",
			input:   "114",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid length (too long)",
			input:   "11400",
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid time value",
			input:   "2560",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string input",
			input:   1140,
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
			got, err := ModelToTypeTimeConverter(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTimeRoundTrip(t *testing.T) {
	testCases := []string{
		"11:40",
		"00:00",
		"23:59",
		"12:00",
		"09:30",
		"15:45",
	}

	for _, timeStr := range testCases {
		t.Run(timeStr, func(t *testing.T) {
			// Convert to model (HHMM)
			modelTime, err := TypeToModelTimeConverter(timeStr)
			require.NoError(t, err)

			// Convert back to type (HH:MM)
			typeTime, err := ModelToTypeTimeConverter(modelTime)
			require.NoError(t, err)

			assert.Equal(t, timeStr, typeTime)
		})
	}
}

func TestTimeAlternateFormats(t *testing.T) {
	t.Run("HH:MM to HHMM", func(t *testing.T) {
		result, err := TypeToModelTimeConverter("11:40")
		require.NoError(t, err)
		assert.Equal(t, "1140", result)
	})

	t.Run("HHMM passthrough", func(t *testing.T) {
		result, err := TypeToModelTimeConverter("1140")
		require.NoError(t, err)
		assert.Equal(t, "1140", result)
	})
}

func TestDateAlternateFormats(t *testing.T) {
	t.Run("YYYY-MM-DD to YYYYMMDD", func(t *testing.T) {
		result, err := TypeToModelDateConverter("2025-11-08")
		require.NoError(t, err)
		assert.Equal(t, "20251108", result)
	})

	t.Run("YYYYMMDD passthrough", func(t *testing.T) {
		result, err := TypeToModelDateConverter("20251108")
		require.NoError(t, err)
		assert.Equal(t, "20251108", result)
	})
}
