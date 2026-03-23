package types

import "time"

type RequiredConfigs struct {
	SetupComplete      bool   `json:"setup_complete"`
	DefaultLogbookID   int64  `json:"default_logbook_id"`
	DefaultRigID       int64  `json:"default_rig_id"`
	DefaultFreq        string `json:"default_freq"`
	DefaultMode        string `json:"default_mode"`
	DefaultIsRandomQso bool   `json:"default_is_random_qso"`
	PowerMultiplier    int    `json:"power_multiplier"`
	DefaultTxPower     int    `json:"default_tx_power"`
	UsePowerMultiplier bool   `json:"use_power_multiplier"`
	// The default TO email address.
	DefaultFwdEmail string `json:"default_fwd_email"`

	// QsoForwardingIntervalSeconds determines the interval at which QSO forwarding occurs, defined as a duration in
	// seconds. The forwarding process checks for queued QSOs at the interval defined by this setting.
	QsoForwardingPollIntervalSeconds time.Duration `json:"qso_forwarding_poll_interval_seconds"`
	QsoForwardingWorkerCount         int           `json:"qso_forwarding_worker_count"`

	// QsoForwardingQueueSize defines the maximum number of QSOs that can be stored in the forwarding queue awaiting processing.
	QsoForwardingQueueSize int `json:"qso_forwarding_queue_size"`

	// The maximum number of QSOs to retrieve from the database for forwarding.
	// This the SQL select LIMIT clause.
	QsoForwardingRowLimit int `json:"qso_forwarding_row_limit"`
	// This is related to forwarding. Writes to the SQLite database are serialized to prevent concurrent write conflicts.
	// and busy signal.
	DatabaseWriteQueueSize int `json:"database_write_queue_size"`

	PagingationPageSize int `json:"pagingation_page_size"`
}
