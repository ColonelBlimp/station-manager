package types

// SmtpConfig holds the operator's SMTP submission credentials. The
// daemon's general-purpose mailer (internal/email) reads this block;
// callers (POST /v1/session/email today, future "alert" subsystems
// like forwarder-backlog or refresher-failure notifications later)
// build their own Message and call Service.Send.
//
// Empty Host disables the mailer — Send returns ErrMailerDisabled
// and callers fold that into a user-visible "email not configured"
// path rather than a 500. This matches the project's "external
// services degrade, not crash" invariant from ADR 0017.
//
// Port defaults to 587 (SMTP submission with STARTTLS). DefaultStartTLS
// is on by default — operators on stricter networks (corp / shared
// infra) need it; cleartext to a local fake is the explicit opt-out
// via StartTLS=false.
//
// DefaultRecipient pre-fills the SessionPanel's recipient input —
// most operators send each session's ADIF to the same QSL manager
// every time, so a sticky default makes the icon-click flow one
// step instead of two.
//
// TimeoutSec bounds the entire connect+auth+send round-trip. SMTP
// is best-effort during slow-internet sessions (per the operator's
// network memory); 30s is a sensible default that's well within
// most SMTP server expectations.
type SmtpConfig struct {
	Host             string `json:"host,omitempty"`
	Port             int    `json:"port,omitempty"`
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	From             string `json:"from,omitempty"`
	DefaultRecipient string `json:"default_recipient,omitempty"`
	StartTLS         bool   `json:"starttls,omitempty"`
	TimeoutSec       int    `json:"timeout_sec,omitempty"`
}
