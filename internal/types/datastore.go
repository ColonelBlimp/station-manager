package types

const SqliteDriverName = "sqlite"

// DatastoreConfig holds the sqlite database configuration. The v1 version of
// this struct carried postgres fields for the planned SM-Online server; those
// were removed during the v2 review (the postgres backend relocated to the
// future server repo per docs/v1-analysis/design-decisions-log.md).
type DatastoreConfig struct {
	Driver                    string            `json:"driver" validate:"oneof=sqlite"`
	Path                      string            `json:"path" validate:"required"`
	Options                   map[string]string `json:"options" validate:"omitempty"`
	MaxOpenConns              int               `json:"max_open_conns" validate:"min=1"`
	MaxIdleConns              int               `json:"max_idle_conns" validate:"min=1"`
	ConnMaxLifetime           int               `json:"conn_max_lifetime" validate:"min=0"`           // Minutes
	ConnMaxIdleTime           int               `json:"conn_max_idle_time" validate:"min=0"`          // Minutes
	ContextTimeout            int               `json:"context_timeout" validate:"min=5"`             // Seconds
	TransactionContextTimeout int               `json:"transaction_context_timeout" validate:"min=5"` // Seconds
}
