package config

import "github.com/Station-Manager/types"

const (
	errMsgNotInitialized = "Config service is not initialized."
)

const (
	ServiceName    = types.ConfigServiceName
	configFileName = "config.json"
	// EnvSmDefaultDB selects the default datastore driver when generating a new config.json.
	// Accepts: "sqlite" (default), "postgres", and common aliases like "postgresql" or "pg".
	EnvSmDefaultDB = "SM_DEFAULT_DB"
	userAgent      = "station-manager/0.1.0"
)
