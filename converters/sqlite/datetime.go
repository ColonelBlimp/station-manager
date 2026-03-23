package sqlite

import (
	"github.com/Station-Manager/adapters/converters"
	"github.com/Station-Manager/errors"
	"time"
)

// TypeToModelDateConverter converts a date value from a string to a correctly formatted string.
// The source value is expected to be a string representation of a date in YYYYMMDD or YYYY-MM-DD format.
// Returns the formatted date (YYYYMMDD) or an error if the source is invalid or conversion fails.
//
// This is a converter that can only be used with an sqlite database, which stores dates as a string.
func TypeToModelDateConverter(src any) (any, error) {
	const op errors.Op = "converters.sqlite.TypeToModelDateConverter"
	srcVal, err := converters.CheckString(op, src)
	if err != nil {
		return "", errors.New(op).Err(err)
	}

	// Accept multiple date formats and converts to YYYYMMDD
	var retVal time.Time
	switch len(srcVal) {
	case 10:
		// Try YYYY-MM-DD format
		if srcVal[4] == '-' && srcVal[7] == '-' {
			retVal, err = time.Parse("2006-01-02", srcVal)
			if err != nil {
				return "", errors.New(op).Err(err).Msg(converters.ErrMsgBadDateFormat)
			}
		} else {
			return "", errors.New(op).Msg(converters.ErrMsgBadDateFormat)
		}
	case 8:
		retVal, err = time.Parse("20060102", srcVal)
		if err != nil {
			return "", errors.New(op).Err(err).Msg(converters.ErrMsgBadDateFormat)
		}
	default:
		return "", errors.New(op).Msg(converters.ErrMsgBadDateFormat)
	}

	return retVal.Format("20060102"), nil
}

// ModelToTypeDateConverter converts a date value from a string to a correctly formatted string
// The source value is expected to be a string representation of a date in YYYYMMDD or YYYY-MM-DD format.
// Returns the formatted date (YYYY-MM-DD) or an error if the source is invalid or conversion fails.
//
// This is a converter that can only be used with an sqlite database, which stores dates as a string.
func ModelToTypeDateConverter(src any) (any, error) {
	const op errors.Op = "converters.sqlite.ModelToTypeDateConverter"
	srcVal, err := converters.CheckString(op, src)
	if err != nil {
		return "", errors.New(op).Err(err)
	}

	if len(srcVal) != 8 {
		return "", errors.New(op).Msg(converters.ErrMsgBadDateFormat)
	}

	retVal, err := time.Parse("20060102", srcVal)
	if err != nil {
		return "", errors.New(op).Err(err).Msg(converters.ErrMsgBadDateFormat)
	}

	return retVal.Format("2006-01-02"), nil
}

// TypeToModelTimeConverter converts a string time value from to a correctly formatted string.
// The source value is expected to be a string representation of a time in HHMM or HH:MM format.
// Returns the formatted time (HHMM) or an error if the source is invalid or conversion fails.
//
// This is a converter that can only be used with an sqlite database, which stores times as a string.
func TypeToModelTimeConverter(src any) (any, error) {
	const op errors.Op = "converters.sqlite.TypeToModelTimeConverter"
	srcVal, err := converters.CheckString(op, src)
	if err != nil {
		return nil, errors.New(op).Err(err)
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

	return retVal.Format("1504"), nil
}

// ModelToTypeTimeConverter converts a string time value from to a correctly formatted string.
// The source value is expected to be a string representation of a time in HHMM format.
// Returns the formatted time (HH:MM) or an error if the source is invalid or conversion fails.
//
// This is a converter that can only be used with an sqlite database, which stores times as a string.
func ModelToTypeTimeConverter(src any) (any, error) {
	const op errors.Op = "converters.sqlite.ModelToTypeDateConverter"
	srcVal, err := converters.CheckString(op, src)
	if err != nil {
		return "", errors.New(op).Err(err)
	}

	if len(srcVal) != 4 {
		return "", errors.New(op).Msg(converters.ErrMsgBadTimeFormat)
	}

	retVal, err := time.Parse("1504", srcVal)
	if err != nil {
		return "", errors.New(op).Err(err).Msg(converters.ErrMsgBadTimeFormat)
	}

	return retVal.Format("15:04"), nil
}
