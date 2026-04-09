package service

import (
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/go-playground/validator/v10"
)

var (
	validate     *validator.Validate
	validateOnce sync.Once
)

// validateConfig validates the FT8 configuration using struct-tag validation
// rules defined on [types.FT8Config]. This is called early in [Initialize],
// before defaults are applied, so the raw config from config.json is checked.
func validateConfig(cfg *types.FT8Config) error {
	const op errors.Op = "ft8.validateConfig"

	if cfg == nil {
		return errors.New(op).Msg("FT8 config is nil")
	}

	validateOnce.Do(func() {
		validate = validator.New(validator.WithRequiredStructEnabled())
	})

	if err := validate.Struct(cfg); err != nil {
		return errors.New(op).Err(err).Msg("invalid FT8 configuration")
	}

	return nil
}
