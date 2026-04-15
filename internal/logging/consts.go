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

const (
	errMsgNilConfig     = "Logging config is nil."
	errMsgNilService    = "Logger service is nil."
	errMsgAppCfgNotSet  = "Application config is not set."
	errMsgConfigInvalid = "Logging configuration is invalid."
)
