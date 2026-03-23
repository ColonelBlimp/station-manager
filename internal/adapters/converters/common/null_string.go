package common

import (
	"github.com/Station-Manager/errors"
	"github.com/aarondl/null/v8"
)

// TypeToModelStringConverter converts a string to a model null.String.
func TypeToModelStringConverter(src any) (any, error) {
	const op errors.Op = "converters.common.TypeToModelStringConverter"
	srcVal, ok := src.(string)
	if !ok {
		return null.String{}, errors.New(op).Errorf("Given parameter not a string, got %T", src)
	}
	// Treat empty string as null
	if srcVal == "" {
		return null.String{}, nil
	}
	return null.StringFrom(srcVal), nil
}

// ModelToTypeStringConverter converts a model null.String to a string.
func ModelToTypeStringConverter(src any) (any, error) {
	const op errors.Op = "converters.common.ModelToTypeStringConverter"

	// Handle null.String type
	if nullStr, ok := src.(null.String); ok {
		if !nullStr.Valid {
			return "", nil
		}
		return nullStr.String, nil
	}

	// Handle plain string directly (including empty string)
	if s, ok := src.(string); ok {
		return s, nil
	}

	return "", errors.New(op).Errorf("Given parameter not a string or null.String, got %T", src)
}
