package types

type ListenerConfig struct {
	Name       string `json:"name" validate:"required"`
	Enabled    bool   `json:"enabled"`
	Host       string `json:"host" validate:"required,hostname"`
	Port       int    `json:"port" validate:"required,min=1001,max=65535"`
	Protocol   string `json:"protocol" validate:"required,oneof=UDP TCP"` // UDP or TCP
	BufferSize int    `json:"buffer_size" validate:"required,min=1024,max=4096"`

	// LogPayload enables logging of payload previews for debugging.
	// WARNING: This may expose sensitive data in logs. Disabled by default.
	LogPayload bool `json:"log_payload"`

	// Handler specifies which packet handler to use (e.g., "wsjtx", "n1mm").
	// If empty, packets are logged but not processed.
	Handler string `json:"handler,omitempty"`

	// HandlerConfig provides handler-specific configuration options.
	// The structure depends on the handler being used.
	HandlerConfig map[string]any `json:"handler_config,omitempty"`
}
