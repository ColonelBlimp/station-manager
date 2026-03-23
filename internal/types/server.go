package types

type RequestAction string

func (s RequestAction) String() string {
	return string(s)
}

const (
	// RegisterLogbookAction registers a new logbook with the server.
	RegisterLogbookAction RequestAction = "register_logbook"
	InsertQsoAction                     = "insert_qso"
)

type PostRequest struct {
	Callsign string `json:"callsign"` // The callsign associated with the user account *NOT THE LOGBOOK CALLSIGN*. The logbook callsign is associated with Key.
	Key      string `json:"key"`      // Logbook's API Key, or if registering, the Bootstrap Key
	// For RegisterLogbookAction, a Logbook must be provided.
	Logbook *Logbook `json:"logbook,omitempty"`
	// For InsertQsoAction, Qso must be provided.
	Qso *Qso `json:"qso,omitempty"`
}

type ServerConfig struct {
	Name         string `json:"name"` // AppName for goFiber
	Host         string `json:"host" validate:"required,hostname"`
	Port         int    `json:"port" validate:"required,min=3000,max=65535"`
	TLSEnabled   bool   `json:"tls_enabled"`
	TLSCertFile  string `json:"tls_cert_file" validate:"required_if=TLSEnabled true"`
	TLSKeyFile   string `json:"tls_key_file" validate:"required_if=TLSEnabled true"`
	ReadTimeout  int    `json:"read_timeout" validate:"required"`  // Seconds
	WriteTimeout int    `json:"write_timeout" validate:"required"` // Seconds
	IdleTimeout  int    `json:"idle_timeout" validate:"required"`  // Seconds
	BodyLimit    int    `json:"body_limit" validate:"required"`
}
