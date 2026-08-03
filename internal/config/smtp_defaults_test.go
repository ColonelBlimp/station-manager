package config

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	SMTP PORT / TIMEOUT DEFAULTS RESOLVE ON EVERY PATH, NOT JUST LOAD.

	Acceptance criterion (operator, 2026-08-03 — "blank means default"):

	    When I clear the port or the timeout and save, the daemon resolves them
	    to 587 / 30 there and then, and the value I see afterwards is the one it
	    stored — I can tell that apart from it storing 0 and surprising me with
	    a different number after the next restart.

	The defect this pins: applyDefaults stamps the two defaults, and it is called
	only by Load (config.go:570) and Default (config.go:664). The PUT handler runs
	Normalize + Validate and NOT applyDefaults (handler_config.go:717), so a blank
	port arriving over the API was stored as 0 and only became 587 at the NEXT
	daemon start. On an ENABLED block it never even got that far — validateSmtp
	rejected port 0 with a 400 that told the operator to type a number the daemon
	already knew.

	Normalize is the right home because it is the one transform BOTH paths share,
	and it runs before Validate on both — so the range check sees the resolved
	value rather than the hole.

	D3 is the rule that keeps this honest: a default that also overwrote an
	explicit value would pass D1 and D2 and be a far worse bug than the one being
	fixed. Its fixture uses 2525/15 — values that differ from the defaults, so the
	stamped and unstamped paths cannot agree.
*/

// D1: a zero port is resolved to the default by Normalize alone (the transform
// the PUT path runs), not only by applyDefaults.
func TestNormalize_ResolvesZeroSmtpPortToDefault(t *testing.T) {
	cfg := Config{Smtp: types.SmtpConfig{Enabled: true, Host: "smtp.example.org", From: "tx@example.org"}}
	Normalize(&cfg)

	if got := cfg.Smtp.Port; got != defaultSmtpPort {
		t.Errorf("Smtp.Port = %d after Normalize, want the default %d", got, defaultSmtpPort)
	}
}

// D2: same for the timeout.
func TestNormalize_ResolvesZeroSmtpTimeoutToDefault(t *testing.T) {
	cfg := Config{Smtp: types.SmtpConfig{Enabled: true, Host: "smtp.example.org", From: "tx@example.org"}}
	Normalize(&cfg)

	if got := cfg.Smtp.TimeoutSec; got != defaultSmtpTimeoutSec {
		t.Errorf("Smtp.TimeoutSec = %d after Normalize, want the default %d", got, defaultSmtpTimeoutSec)
	}
}

// D3: an operator-supplied value survives. Both fields carry non-default values
// here, so a Normalize that stamped unconditionally would fail this and pass D1/D2.
func TestNormalize_KeepsExplicitSmtpPortAndTimeout(t *testing.T) {
	cfg := Config{Smtp: types.SmtpConfig{
		Enabled: true, Host: "smtp.example.org", From: "tx@example.org",
		Port: 2525, TimeoutSec: 15,
	}}
	Normalize(&cfg)

	if got := cfg.Smtp.Port; got != 2525 {
		t.Errorf("Smtp.Port = %d, want the operator's 2525 left alone", got)
	}
	if got := cfg.Smtp.TimeoutSec; got != 15 {
		t.Errorf("Smtp.TimeoutSec = %d, want the operator's 15 left alone", got)
	}
}

// D4: the resolution must not reach past the two numeric holes. A blank host on
// an enabled block stays blank so validateSmtp can still refuse it — "blank means
// default" is a rule about the port and the timeout, which HAVE defaults, not
// about the fields only the operator can supply.
func TestNormalize_DoesNotInventSmtpHostOrFrom(t *testing.T) {
	cfg := Config{Smtp: types.SmtpConfig{Enabled: true}}
	Normalize(&cfg)

	if cfg.Smtp.Host != "" || cfg.Smtp.From != "" {
		t.Errorf("Normalize invented host/from: %+v", cfg.Smtp)
	}
	if f := firstFatal(Validate(cfg)); f == nil {
		t.Error("an enabled SMTP block with no host still has to fail validation")
	}
}

// firstFatal returns the first blocking finding, or nil when only advisories fired.
func firstFatal(findings []Finding) *Finding {
	for i := range findings {
		if !findings[i].Warning {
			return &findings[i]
		}
	}
	return nil
}
