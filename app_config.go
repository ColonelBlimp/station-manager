package types

type AppConfig struct {
	DatastoreConfig      DatastoreConfig   `json:"datastore_config"`
	LoggingConfig        LoggingConfig     `json:"logging_config"`
	RequiredConfigs      RequiredConfigs   `json:"required_configs"`
	ServerConfig         *ServerConfig     `json:"server_config,omitempty"`
	RigConfigs           []RigConfig       `json:"rig_configs,omitempty"`
	LookupServiceConfigs []LookupConfig    `json:"lookup_service_configs,omitempty"`
	ForwardingConfigs    []ForwarderConfig `json:"forwarding_configs,omitempty"`
	EmailConfigs         EmailConfig       `json:"email_configs,omitempty"`
	LoggingStation       LoggingStation    `json:"logging_station"`
	OptionalConfigs      OptionalConfigs   `json:"optional_configs"`
	ListenerConfigs      []ListenerConfig  `json:"listener_configs,omitempty"`
}

type UiConfig struct {
	DefaultRigID int64   `json:"default_rig_id"`
	Logbook      Logbook `json:"logbook"`
	RigName      string  `json:"rig_name"`
	// DefaultFreq is in khz. This is used when the CAT system is unavailable.
	DefaultFreq string `json:"default_freq"`
	// DefaultMode is the CAT mode when the CAT system is unavailable.
	DefaultMode        string `json:"default_mode"`
	OwnersCallsign     string `json:"owners_callsign"`
	DefaultIsRandomQso bool   `json:"default_is_random_qso"`
	UsePowerMultiplier bool   `json:"use_power_multiplier"`
	PowerMultiplier    int    `json:"power_multiplier"`
	DefaultTxPower     int    `json:"default_tx_power"`
	DefaultFwdEmail    string `json:"default_fwd_email"`
	OwnerCallsign      string `json:"owner_callsign"`

	PaginationPageSize int `json:"pagination_page_size"`

	QrzViewUrl string `json:"qrz_view_url"`
}
