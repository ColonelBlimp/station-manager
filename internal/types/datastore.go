package types

const (
	PostgresDriverName = "postgres"
	SqliteDriverName   = "sqlite"
)

type DatastoreConfig struct {
	Driver                    string            `json:"driver" validate:"oneof=postgres sqlite"`
	Path                      string            `json:"path" validate:"required_if=Driver sqlite,omitempty"`    // Used for sqlite only
	Options                   map[string]string `json:"options" validate:"required_if=Driver sqlite,omitempty"` // Used for sqlite only
	Host                      string            `json:"host,omitempty" validate:"required_if=Driver postgres,omitempty,hostname|ip"`
	Port                      int               `json:"port,omitempty" validate:"required_if=Driver postgres,omitempty,min=1,max=65535"`
	User                      string            `json:"user,omitempty" validate:"required_if=Driver postgres,omitempty,min=1,max=63,alphanum|contains=_|contains=-"`
	Password                  string            `json:"pass,omitempty" validate:"required_if=Driver postgres,omitempty,min=1"`
	Database                  string            `json:"database,omitempty" validate:"required_if=Driver postgres,omitempty,min=1,max=63,alphanum|contains=_|contains=-"`
	SSLMode                   string            `json:"ssl_mode,omitempty" validate:"required_if=Driver postgres,omitempty,oneof=disable require verify-ca verify-full"`
	MaxOpenConns              int               `json:"max_open_conns" validate:"min=1"` // For Postgres, minimum is 5 (enforced in database validation)
	MaxIdleConns              int               `json:"max_idle_conns" validate:"min=1"`
	ConnMaxLifetime           int               `json:"conn_max_lifetime" validate:"min=0"`           // Number of minutes
	ConnMaxIdleTime           int               `json:"conn_max_idle_time" validate:"min=0"`          // Number of minutes
	ContextTimeout            int               `json:"context_timeout" validate:"min=5"`             // Seconds
	TransactionContextTimeout int               `json:"transaction_context_timeout" validate:"min=5"` // Seconds
	Debug                     bool              // Enable SQLBoiler query logging
	Params                    map[string]string `json:"params,omitempty" validate:"omitempty"`
}
