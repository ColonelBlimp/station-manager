package types

import "time"

type LookupConfig struct {
	Name           string        `json:"name"`
	Enabled        bool          `json:"enabled"`
	URL            string        `json:"url"`
	Username       string        `json:"username,omitempty"`
	Password       string        `json:"password,omitempty"`
	UserAgent      string        `json:"useragent"`
	HttpTimeoutSec time.Duration `json:"timeout_sec"` // Seconds
	ViewUrl        string        `json:"view_url,omitempty"`
}
