package logging

import "github.com/Station-Manager/types"

const (
	// ServiceName is the DI/service locator name for the logging service.
	ServiceName = types.LoggingServiceName
	emptyString = ""
)

const (
	errMsgNilConfig     = "Logging config is nil."
	errMsgNilService    = "Logger service is nil."
	errMsgAppCfgNotSet  = "Application config is not set."
	errMsgConfigInvalid = "Logging configuration is invalid."
)
