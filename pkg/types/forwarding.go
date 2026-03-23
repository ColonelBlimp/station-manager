package types

import "time"

type ForwarderConfig struct {
	Name           string        `json:"name"`
	Enabled        bool          `json:"enabled"`
	URL            string        `json:"url"`
	APIKey         string        `json:"apikey,omitempty"`
	Username       string        `json:"username,omitempty"`
	Password       string        `json:"password,omitempty"`
	UserAgent      string        `json:"useragent"`
	HttpTimeoutSec time.Duration `json:"timeout_sec"`
}
