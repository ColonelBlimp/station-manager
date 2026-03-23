package logging

import (
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog"
	"path/filepath"
	"strings"
	"sync"
)

var validate *validator.Validate
var once sync.Once

// validateConfig validates the LoggingConfig structure using struct tags
// and additional semantic checks such as a valid log level, reasonable
// caller skip frame bounds, and RelLogFileDir safety (no traversal, relative path only).
func validateConfig(cfg *types.LoggingConfig) error {
	const op errors.Op = "logging.validateConfig"
	if cfg == nil {
		return errors.New(op).Msg(errMsgNilConfig)
	}

	once.Do(func() {
		validate = validator.New(validator.WithRequiredStructEnabled())
	})

	if err := validate.Struct(cfg); err != nil {
		return errors.New(op).Err(err).Msg(errMsgConfigInvalid)
	}

	// Validate log level
	if _, err := zerolog.ParseLevel(cfg.Level); err != nil {
		return errors.New(op).Errorf("invalid log level '%s': %w", cfg.Level, err)
	}

	// Validate skip frame count is reasonable
	if cfg.SkipFrameCount < 0 || cfg.SkipFrameCount > 20 {
		return errors.New(op).Msg("SkipFrameCount must be between 0 and 20")
	}

	// Validate RelLogFileDir for path traversal
	if cfg.RelLogFileDir == "" {
		return errors.New(op).Msg("RelLogFileDir cannot be empty")
	}

	// Clean the path and check for directory traversal attempts
	cleanPath := filepath.Clean(cfg.RelLogFileDir)
	if strings.Contains(cleanPath, "..") {
		return errors.New(op).Msg("RelLogFileDir cannot contain '..' (directory traversal)")
	}

	// Ensure it's a relative path (doesn't start with / or drive letter on Windows)
	if filepath.IsAbs(cleanPath) {
		return errors.New(op).Msg("RelLogFileDir must be a relative path")
	}

	return nil
}
