package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeToModelDateConverter(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    time.Time
		wantErr bool
	}{
		{
			name:    "YYYYMMDD format",
			input:   "20251108",
			want:    time.Date(2025, 11, 8, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "YYYY-MM-DD format",
			input:   "2025-11-08",
			want:    time.Date(2025, 11, 8, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "leap year date",
			input:   "2024-02-29",
			want:    time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "first day of year",
			input:   "2025-01-01",
			want:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "last day of year",
			input:   "2025-12-31",
			want:    time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "invalid date format (too short)",
			input:   "2025-11",
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "invalid date format (too long)",
			input:   "2025-11-089",
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "non-string input",
			input:   20251108,
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    time.Time{},
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
			gotTime, ok := got.(time.Time)
			require.True(t, ok, "result should be time.Time")
			assert.Equal(t, tt.want, gotTime)
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
			name:    "valid date",
			input:   time.Date(2025, 11, 8, 0, 0, 0, 0, time.UTC),
			want:    "2025-11-08",
			wantErr: false,
		},
		{
			name:    "leap year",
			input:   time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
			want:    "2024-02-29",
			wantErr: false,
		},
		{
			name:    "first day of year",
			input:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			want:    "2025-01-01",
			wantErr: false,
		},
		{
			name:    "last day of year",
			input:   time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
			want:    "2025-12-31",
			wantErr: false,
		},
		{
			name:    "zero time",
			input:   time.Time{},
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-time.Time input",
			input:   "2025-11-08",
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
			// Convert to model (time.Time)
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
		want    time.Time
		wantErr bool
	}{
		{
			name:    "HH:MM format",
			input:   "11:40",
			want:    time.Date(0, 1, 1, 11, 40, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "HHMM format",
			input:   "1140",
			want:    time.Date(0, 1, 1, 11, 40, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "midnight",
			input:   "00:00",
			want:    time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "end of day",
			input:   "23:59",
			want:    time.Date(0, 1, 1, 23, 59, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "noon",
			input:   "12:00",
			want:    time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "invalid format",
			input:   "11:4",
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "invalid hour",
			input:   "25:00",
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "invalid minute",
			input:   "11:60",
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "non-string input",
			input:   1140,
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "nil input",
			input:   nil,
			want:    time.Time{},
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
			gotTime, ok := got.(time.Time)
			require.True(t, ok, "result should be time.Time")
			assert.Equal(t, tt.want, gotTime)
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
			name:    "valid time",
			input:   time.Date(0, 1, 1, 11, 40, 0, 0, time.UTC),
			want:    "11:40",
			wantErr: false,
		},
		{
			name:    "midnight",
			input:   time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC),
			want:    "00:00",
			wantErr: false,
		},
		{
			name:    "end of day",
			input:   time.Date(0, 1, 1, 23, 59, 0, 0, time.UTC),
			want:    "23:59",
			wantErr: false,
		},
		{
			name:    "noon",
			input:   time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC),
			want:    "12:00",
			wantErr: false,
		},
		{
			name:    "zero time",
			input:   time.Time{},
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-time.Time input",
			input:   "11:40",
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
			// Convert to model (time.Time)
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
	t.Run("HH:MM format", func(t *testing.T) {
		result, err := TypeToModelTimeConverter("11:40")
		require.NoError(t, err)
		resultTime, ok := result.(time.Time)
		require.True(t, ok)
		assert.Equal(t, 11, resultTime.Hour())
		assert.Equal(t, 40, resultTime.Minute())
	})

	t.Run("HHMM format", func(t *testing.T) {
		result, err := TypeToModelTimeConverter("1140")
		require.NoError(t, err)
		resultTime, ok := result.(time.Time)
		require.True(t, ok)
		assert.Equal(t, 11, resultTime.Hour())
		assert.Equal(t, 40, resultTime.Minute())
	})
}

func TestDateAlternateFormats(t *testing.T) {
	t.Run("YYYY-MM-DD format", func(t *testing.T) {
		result, err := TypeToModelDateConverter("2025-11-08")
		require.NoError(t, err)
		resultTime, ok := result.(time.Time)
		require.True(t, ok)
		assert.Equal(t, 2025, resultTime.Year())
		assert.Equal(t, time.November, resultTime.Month())
		assert.Equal(t, 8, resultTime.Day())
	})

	t.Run("YYYYMMDD format", func(t *testing.T) {
		result, err := TypeToModelDateConverter("20251108")
		require.NoError(t, err)
		resultTime, ok := result.(time.Time)
		require.True(t, ok)
		assert.Equal(t, 2025, resultTime.Year())
		assert.Equal(t, time.November, resultTime.Month())
		assert.Equal(t, 8, resultTime.Day())
	})
}
