package logging

import "github.com/ColonelBlimp/station-manager/internal/types"

// ValidateLoggingConfig exposes the unexported consumer validator so the CC-3 parity
// test (in package logging_test) can confirm it agrees with config.Validate on every
// input. Compiled ONLY under test, so it never becomes a production entry point.
func ValidateLoggingConfig(cfg *types.LoggingConfig) error {
	return validateConfig(cfg)
}
