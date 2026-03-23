package converters

import (
	"github.com/Station-Manager/errors"
	"math"
	"time"
)

func CheckString(op errors.Op, src any) (string, error) {
	srcVal, ok := src.(string)
	if !ok {
		return "", errors.New(op).Errorf("Given parameter not a string, got %T", src)
	}
	if srcVal == "" {
		return "", errors.New(op).Msg(ErrMsgFreqParamEmpty)
	}
	return srcVal, nil
}

func CheckFloat64(op errors.Op, src any) (float64, error) {
	srcVal, ok := src.(float64)
	if !ok {
		return 0, errors.New(op).Errorf("Given parameter not a float64, got %T", src)
	}
	if srcVal == 0 {
		return 0, errors.New(op).Msg(ErrMsgFreqParamEmpty)
	}
	return srcVal, nil
}

func CheckInt64(op errors.Op, src any) (int64, error) {
	// Accept multiple numeric representations that may come from JSON unmarshalling:
	// - int, intX, uintX
	// - float64 (JSON default for numbers) when it is an integer value
	switch v := src.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case uint:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case float64:
		// JSON numbers are float64; accept if it's an integer value.
		if math.Trunc(v) != v {
			return -1, errors.New(op).Errorf("Given float64 not an integer, got %v", v)
		}
		return int64(v), nil
	default:
		return -1, errors.New(op).Errorf("Given parameter not an integer, got %T", src)
	}
}

func CheckTime(op errors.Op, src any) (time.Time, error) {
	srcVal, ok := src.(time.Time)
	if !ok {
		return time.Time{}, errors.New(op).Errorf("Given parameter not a string, got %T", src)
	}
	// We don't report if it is a Zero Time instant.
	return srcVal, nil
}
