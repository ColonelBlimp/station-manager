package common

import (
	"github.com/Station-Manager/errors"
	"github.com/aarondl/null/v8"
	"time"
)

// TypeToModelTimeConverter converts a time.Time to a model null.Time.
func TypeToModelTimeConverter(src any) (any, error) {
	const op errors.Op = "converters.common.TypeToModelTimeConverter"
	srcVal, ok := src.(time.Time)
	if !ok {
		return null.Time{}, errors.New(op).Errorf("Given parameter not a string, got %T", src)
	}
	// Treat empty time as a valid time, do not error
	if srcVal.IsZero() {
		return srcVal.UTC(), nil
	}
	return null.TimeFrom(srcVal), nil
}

// ModelToTypeTimeConverter converts a model null.Time to a time.Time.
func ModelToTypeTimeConverter(src any) (any, error) {
	const op errors.Op = "converters.common.ModelToTypeTimeConverter"

	if nullTime, ok := src.(null.Time); ok {
		if !nullTime.Valid {
			return time.Time{}, nil
		}
		return nullTime.Time, nil
	}

	if s, ok := src.(time.Time); ok {
		return s, nil
	}

	return time.Time{}, errors.New(op).Errorf("Given parameter not a string or null.String, got %T", src)
}
