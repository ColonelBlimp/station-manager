package facade

import (
	"fmt"
	"strings"

	"github.com/Station-Manager/enums/bands"
	"github.com/Station-Manager/enums/modes"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/utils"
	"github.com/go-playground/validator/v10"
)

func (s *Service) initializeValidation() error {
	const op errors.Op = "facade.Service.initializeValidation"

	s.validate = validator.New(validator.WithRequiredStructEnabled())

	if err := registerBandValidator(s.validate); err != nil {
		return errors.New(op).Errorf("registerBandValidator: %w", err)
	}
	if err := registerModeValidator(s.validate); err != nil {
		return errors.New(op).Errorf("registerModeValidator: %w", err)
	}
	if err := registerDateValidator(s.validate); err != nil {
		return errors.New(op).Errorf("registerDateValidator: %w", err)
	}
	if err := registerTimeValidator(s.validate); err != nil {
		return errors.New(op).Errorf("registerTimeValidator: %w", err)
	}
	if err := registerRSTValidator(s.validate); err != nil {
		return errors.New(op).Errorf("registerRSTValidator: %w", err)
	}
	if err := registerFrequencyValidator(s.validate); err != nil {
		return errors.New(op).Errorf("registerFrequencyValidator: %w", err)
	}
	return nil
}

func registerBandValidator(v *validator.Validate) error {
	return v.RegisterValidation("band", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		return bands.IsValidBand(strings.ToLower(value))
	})
}

func registerModeValidator(v *validator.Validate) error {
	return v.RegisterValidation("mode", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		return modes.IsValidMode(value)
	})
}

func registerDateValidator(v *validator.Validate) error {
	if err := v.RegisterValidation("qso_date", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		return utils.IsValidDateYYYYMMDD(value)
	}); err != nil {
		return fmt.Errorf("registering 'qso_date' validator: %w", err)
	}
	if err := v.RegisterValidation("qso_date_off", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		return utils.IsValidDateYYYYMMDD(value)
	}); err != nil {
		return fmt.Errorf("registering 'qso_date_off' validator: %w", err)
	}
	return nil
}

func registerTimeValidator(v *validator.Validate) error {
	if err := v.RegisterValidation("time_on", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		return utils.IsValidTimeADIF(value)
	}); err != nil {
		return fmt.Errorf("registering 'time_on' validator: %w", err)
	}
	if err := v.RegisterValidation("time_off", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		return utils.IsValidTimeADIF(value)
	}); err != nil {
		return fmt.Errorf("registering 'time_off' validator: %w", err)
	}
	return nil
}

func registerRSTValidator(v *validator.Validate) error {
	if err := v.RegisterValidation("rst_sent", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		if len(value) > 3 || len(value) < 2 {
			return false
		}
		return isAllNumbers(value)
	}); err != nil {
		return fmt.Errorf("registering 'rst_sent' validator: %w", err)
	}
	if err := v.RegisterValidation("rst_rcvd", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		if len(value) > 3 || len(value) < 2 {
			return false
		}
		return isAllNumbers(value)
	}); err != nil {
		return fmt.Errorf("registering 'rst_rcvd' validator: %w", err)
	}
	return nil
}

func registerFrequencyValidator(v *validator.Validate) error {
	return v.RegisterValidation("freq", func(fl validator.FieldLevel) bool {
		value, ok := fl.Field().Interface().(string)
		if !ok {
			return false
		}
		// Frequency must be in MHz format per ADIF (integer part and optional decimal with up to 6 digits)
		return utils.IsValidFrequencyMHz(value)
	})
}
