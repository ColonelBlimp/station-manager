package types

// DI bean name constants. Shared across packages that need a stable identifier
// for service registration with internal/iocdi. App-specific services (e.g.
// the daemon's own facade or a command's CLI-facing service) define their own
// ServiceName constants locally — only shared names live here.
const (
	ConfigServiceName  = "configservice"
	SqliteServiceName  = "sqliteservice"
	LoggingServiceName = "loggingservice"

	// Lookup provider service names (ADR 0017). Used both as the DI
	// bean ID and as the LookupConfig.Name value the orchestrator
	// matches on when picking which provider to invoke.
	HamNutLookupServiceName = "hamnutlookupservice"
	QRZLookupServiceName    = "qrzlookupservice"

	// Fixed provider endpoints, defaulted daemon-side (config.Normalize) so the
	// config SPA's Enrichment tab is frictionless — the operator supplies only
	// credentials, never a URL. These are the canonical public endpoints.
	HamNutLookupDefaultURL  = "https://api.hamnut.com/v1/call-signs/prefixes"
	QRZLookupDefaultURL     = "https://xmldata.qrz.com/xml/current/"
	QRZLookupDefaultViewURL = "https://www.qrz.com/db/"
)
