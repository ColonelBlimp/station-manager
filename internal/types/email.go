package types

// SmtpConfig holds the operator's SMTP submission credentials. The
// daemon's general-purpose mailer (internal/email) reads this block;
// callers (POST /v1/session/email today, future "alert" subsystems
// like forwarder-backlog or refresher-failure notifications later)
// build their own Message and call Service.Send.
//
// Enabled is the explicit kill-switch. When false, Send returns
// ErrMailerDisabled and callers fold that into a user-visible
// "email not configured" path rather than a 500 — matching the
// project's "external services degrade, not crash" invariant from
// ADR 0017. DefaultConfig writes Enabled=false so a fresh install
// has a visible-but-inert SMTP template the operator can fill in.
//
// Port defaults to 587 (SMTP submission with STARTTLS). StartTLS
// defaults to true — operators on stricter networks (corp / shared
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
//
// No omitempty tags here — every field renders in the on-disk JSON
// so the operator sees the complete shape on first run, including
// empty strings for credentials they haven't supplied yet.
type SmtpConfig struct {
	Enabled          bool   `json:"enabled"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	From             string `json:"from"`
	DefaultRecipient string `json:"default_recipient"`
	StartTLS         bool   `json:"starttls"`
	TimeoutSec       int    `json:"timeout_sec"`
}
