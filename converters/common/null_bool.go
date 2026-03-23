package common

import (
	"github.com/Station-Manager/errors"
	"github.com/aarondl/null/v8"
)

// TypeToModelBoolConverter converts a bool to a model null.Bool.
func TypeToModelBoolConverter(src any) (any, error) {
	const op errors.Op = "converters.common.TypeToModelBoolConverter"
	srcVal, ok := src.(bool)
	if !ok {
		return null.Bool{}, errors.New(op).Errorf("Given parameter not a bool, got %T", src)
	}

	return null.BoolFrom(srcVal), nil
}

// ModelToTypeBoolConverter converts a model null.Bool to a bool.
func ModelToTypeBoolConverter(src any) (any, error) {
	const op errors.Op = "converters.common.ModelToTypeBoolConverter"

	if nullStr, ok := src.(null.Bool); ok {
		if !nullStr.Valid {
			return false, nil
		}
		return nullStr.Bool, nil
	}

	// Handle plain string directly (including empty string)
	if s, ok := src.(bool); ok {
		return s, nil
	}

	return false, errors.New(op).Errorf("Given parameter not a string or null.String, got %T", src)
}
