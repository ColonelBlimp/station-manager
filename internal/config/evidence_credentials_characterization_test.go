package config

// Characterization (W-0021 5C, commit a): how evidence sync finds its SM Cloud
// credentials TODAY, under config v5. These tests pass against the current code
// and pin what 5C must keep or deliberately change:
//
//   E1  KEPT     — an enabled SM Cloud entry with url + token supplies exactly
//                  those two values (never the logbook, never another entry's).
//   E2  CHANGES  — a DISABLED but complete SM Cloud entry is refused today
//                  ("no enabled smcloud forwarder"). 5C (ruling R4) makes evidence
//                  sync depend on a complete SM Cloud station account; `enabled`
//                  leaves the station entry, and evidence.sync stays the consent.
//   E3  KEPT     — an incomplete account (url or token missing) is refused.
//   E4  CHANGES  — Validate with evidence.sync on reports
//                  evidence_sync_needs_smcloud for the disabled-but-complete
//                  entry of E2; after 5C it reports nothing.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Synthetic, distinct values: selecting the wrong source fails visibly.
const (
	charSMCURL     = "https://smc-char.example.test/"
	charSMCToken   = "SMC-TOKEN-CHAR-31"
	charSMCLogbook = "cloud-book-char"
)

func charSMCEntry(enabled bool, creds string) types.ForwarderConfig {
	return types.ForwarderConfig{
		Name: "cloud-char", Type: "smcloud", Enabled: enabled,
		Credentials: json.RawMessage(creds),
	}
}

func charCompleteCreds() string {
	return `{"url":"` + charSMCURL + `","token":"` + charSMCToken + `","logbook":"` + charSMCLogbook + `"}`
}

func TestCharacterize_EvidenceSyncCredentials(t *testing.T) {
	qrz := types.ForwarderConfig{
		Name: "qrz-char", Type: "qrz", Enabled: true,
		Credentials: json.RawMessage(`{"api_key":"QRZ-KEY-CHAR-17"}`),
	}

	// E1: the enabled SM Cloud entry's url and token, beside another destination.
	url, token, err := EvidenceSyncCredentials(Config{Forwarders: []types.ForwarderConfig{
		qrz, charSMCEntry(true, charCompleteCreds()),
	}})
	if err != nil || url != charSMCURL || token != charSMCToken {
		t.Fatalf("E1: resolved (%q, %q, %v); want the SM Cloud entry's url and token", url, token, err)
	}

	// E2 (5C CHANGES, R4): disabled but complete is refused today.
	_, _, err = EvidenceSyncCredentials(Config{Forwarders: []types.ForwarderConfig{
		charSMCEntry(false, charCompleteCreds()),
	}})
	if err == nil || !strings.Contains(err.Error(), "no enabled smcloud forwarder") {
		t.Fatalf("E2: err = %v; today a disabled SM Cloud entry is not a credential source", err)
	}

	// E3: an incomplete account is refused, naming what is missing — never a value.
	for name, creds := range map[string]string{
		"no token": `{"url":"` + charSMCURL + `","logbook":"` + charSMCLogbook + `"}`,
		"no url":   `{"token":"` + charSMCToken + `"}`,
	} {
		_, _, err := EvidenceSyncCredentials(Config{Forwarders: []types.ForwarderConfig{charSMCEntry(true, creds)}})
		if err == nil || !strings.Contains(err.Error(), "missing url or token") {
			t.Fatalf("E3 %s: err = %v; want the incomplete account refused", name, err)
		}
		if strings.Contains(err.Error(), charSMCToken) || strings.Contains(err.Error(), charSMCURL) {
			t.Fatalf("E3 %s: the refusal carries a credential value: %v", name, err)
		}
	}
}

func TestCharacterize_EvidenceSyncValidation(t *testing.T) {
	finding := func(fwds ...types.ForwarderConfig) bool {
		cfg := Config{
			Evidence:   types.EvidenceConfig{Capture: true, Sync: true, CapBytes: 524288000},
			Forwarders: fwds,
		}
		for _, f := range Validate(cfg) {
			if f.Code == "evidence_sync_needs_smcloud" {
				return true
			}
		}
		return false
	}
	if finding(charSMCEntry(true, charCompleteCreds())) {
		t.Fatal("E4: an enabled, complete SM Cloud entry must satisfy evidence.sync")
	}
	// 5C CHANGES (R4): today the disabled-but-complete entry fails validation.
	if !finding(charSMCEntry(false, charCompleteCreds())) {
		t.Fatal("E4: today evidence.sync refuses a disabled SM Cloud entry")
	}
}
