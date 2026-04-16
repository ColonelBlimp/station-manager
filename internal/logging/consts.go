package logging

import "github.com/ColonelBlimp/station-manager/internal/types"

const (
	// ServiceName is the DI/service locator name for the logging service.
	ServiceName = types.LoggingServiceName
	emptyString = ""
)

// defaultShutdownTimeoutMS is the fallback drain timeout used by
// Service.Close() when LoggingConfig.ShutdownTimeoutMS is unset or zero.
// 100ms is a pragmatic choice: long enough for a single in-flight log
// write to finish on a reasonably loaded system, short enough that
// daemon shutdown isn't perceptibly delayed by the logger.
const defaultShutdownTimeoutMS = 100

// Error message strings. Per Go convention these start with a lowercase
// letter and have no trailing punctuation, so they compose naturally
// when concatenated into larger error chains via %w and the
// internal/errors "op: msg: cause" format.
const (
	errMsgNilConfig     = "logging config is nil"
	errMsgNilService    = "logger service is nil"
	errMsgAppCfgNotSet  = "application config is not set"
	errMsgConfigInvalid = "logging configuration is invalid"
)
