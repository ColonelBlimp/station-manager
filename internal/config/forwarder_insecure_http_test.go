package config

// ST-4a (A4) — allow_insecure_http is SM-Cloud-only. Setting it on any other forwarder
// type is REJECTED at validation (fatal at Load / 400 at PUT), not silently ignored: an
// operator who thinks they enabled cleartext for QRZ must be told it does nothing there.

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func hasInsecureHTTPFinding(fs []Finding) bool {
	for _, f := range fs {
		if !f.Warning && strings.Contains(f.Message, "allow_insecure_http") {
			return true
		}
	}
	return false
}

func TestValidateForwarders_AllowInsecureHTTPSmcloudOnly(t *testing.T) {
	for _, typ := range []string{"qrz", "qrzcq", "clublog"} {
		cfg := DefaultConfig(t.TempDir())
		cfg.Forwarders = []types.ForwarderConfig{{Name: "f", Type: typ, AllowInsecureHTTP: true}}
		if !hasInsecureHTTPFinding(Validate(cfg)) {
			t.Errorf("allow_insecure_http on %q should be rejected as a fatal finding; findings = %+v",
				typ, Validate(cfg))
		}
	}

	// smcloud is the one type where the acknowledgement is valid.
	cfg := DefaultConfig(t.TempDir())
	cfg.Forwarders = []types.ForwarderConfig{{Name: "cloud", Type: "smcloud", AllowInsecureHTTP: true}}
	if hasInsecureHTTPFinding(Validate(cfg)) {
		t.Errorf("allow_insecure_http on smcloud should be accepted; findings = %+v", Validate(cfg))
	}
}
