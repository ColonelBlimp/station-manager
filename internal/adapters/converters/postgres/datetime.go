package postgres

import (
	"github.com/Station-Manager/adapters/converters"
	"github.com/Station-Manager/errors"
	"time"
)

// TypeToModelDateConverter converts a date value from a string to a time.Time.
// The source value is expected to be a string representation of a date in YYYYMMDD or YYYY-MM-DD format.
// Returns the converted date or an error if the source is invalid or conversion fails.
func TypeToModelDateConverter(src any) (any, error) {
	const op errors.Op = "converters.postgres.TypeToModelDateConverter"
	srcVal, err := converters.CheckString(op, src)
	if err != nil {
		return "", errors.New(op).Err(err)
	}

	// Accept multiple date formats and convert to YYYYMMDD
	var retVal time.Time
	switch len(srcVal) {
	case 8:
		// YYYYMMDD format
		retVal, err = time.Parse("20060102", srcVal)
	case 10:
		// Try YYYY-MM-DD format
		if srcVal[4] == '-' && srcVal[7] == '-' {
			retVal, err = time.Parse("2006-01-02", srcVal)
		} else {
			err = errors.New(op).Msg(converters.ErrMsgBadDateFormat)
		}
	default:
		return "", errors.New(op).Msg(converters.ErrMsgBadDateFormat)
	}

	if err != nil {
		return "", errors.New(op).Err(err).Msg(converters.ErrMsgBadDateFormat)
	}

	return retVal, nil
}

// ModelToTypeDateConverter converts a date value (time.Time) from to a correctly formatted string (YYYY-MM-DD).
// The source value is expected to be a time.Time.
// Returns the converted date or an error if the source is invalid or conversion fails.
func ModelToTypeDateConverter(src any) (any, error) {
	const op errors.Op = "converters.postgres.ModelToTypeDateConverter"
	srcVal, err := converters.CheckTime(op, src)
	if err != nil {
		return "", errors.New(op).Err(err)
	}

	if srcVal.IsZero() {
		return "", errors.New(op).Msg(converters.ErrMsgBadDateFormat)
	}

	return srcVal.Format("2006-01-02"), nil
}

// TypeToModelTimeConverter converts a time value from a string to a time.Time.
// The source value is expected to be a string representation of a date in HHMM or HH:MM format.
// Returns the converted time or an error if the source is invalid or the conversion fails.
func TypeToModelTimeConverter(src any) (any, error) {
	const op errors.Op = "converters.postgres.TypeToModelTimeConverter"
	srcVal, err := converters.CheckString(op, src)
	if err != nil {
		return "", errors.New(op).Err(err)
	}

	// Accept both HH:MM and HHMM formats
	var retVal time.Time
	if len(srcVal) == 5 && srcVal[2] == ':' {
		// HH:MM format - parse and convert to HHMM
		retVal, err = time.Parse("15:04", srcVal)
		if err != nil {
			return "", errors.New(op).Err(err).Msg(converters.ErrMsgBadTimeFormat)
		}
	} else if len(srcVal) == 4 {
		// HHMM format
		retVal, err = time.Parse("1504", srcVal)
		if err != nil {
			return "", errors.New(op).Err(err).Msg(converters.ErrMsgBadTimeFormat)
		}
	} else {
		return "", errors.New(op).Msg(converters.ErrMsgBadTimeFormat)
	}

	return retVal, nil
}

// ModelToTypeTimeConverter converts a time value to a correctly formatted string (HH:MM).
// The source value is expected to be a string representation of a time in HHMM or HH:MM format.
func ModelToTypeTimeConverter(src any) (any, error) {
	const op errors.Op = "converters.postgres.TypeToModelTimeConverter"
	srcVal, err := converters.CheckTime(op, src)
	if err != nil {
		return "", errors.New(op).Err(err)
	}

	if srcVal.IsZero() {
		return "", errors.New(op).Msg(converters.ErrMsgBadTimeFormat)
	}
	return srcVal.Format("15:04"), nil
}
