package config

// Evidence sync's SM Cloud credentials. Characterized under config v5 (W-0021
// 5C, commit a); the cases marked CHANGED took their 5C form with config v6
// (ruling R4): evidence sync needs a complete SM Cloud station account, and
// evidence.sync stays the consent switch — neither binding enablement nor the
// active archive decides it.
//
//   E1  KEPT     — an enabled SM Cloud entry with url + token supplies exactly
//                  those two values (never the logbook, never another entry's).
//   E2  CHANGED  — a complete SM Cloud account supplies the credentials whether
//                  or not a legacy `enabled` flag is set (v5 refused a disabled
//                  entry); a stripped account, which has no flag, does too.
//   E3  KEPT     — an incomplete account (url or token missing) is refused.
//   E4  CHANGED  — Validate with evidence.sync on accepts the accounts of E2 and
//                  still reports evidence_sync_needs_smcloud with no account.

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

	// E2 (CHANGED, R4): a complete account is the source, enabled or not.
	for name, fc := range map[string]types.ForwarderConfig{
		"legacy disabled": charSMCEntry(false, charCompleteCreds()),
		"stripped":        {Type: "smcloud", Credentials: json.RawMessage(charCompleteCreds())},
	} {
		url, token, err := EvidenceSyncCredentials(Config{Forwarders: []types.ForwarderConfig{qrz, fc}})
		if err != nil || url != charSMCURL || token != charSMCToken {
			t.Fatalf("E2 %s: resolved (%q, %q, %v); want the account's url and token", name, url, token, err)
		}
	}
	if _, _, err := EvidenceSyncCredentials(Config{Forwarders: []types.ForwarderConfig{qrz}}); err == nil ||
		!strings.Contains(err.Error(), "no SM Cloud station account") {
		t.Fatalf("E2: no SM Cloud account: err = %v; want it named", err)
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
	// CHANGED (R4): a complete account satisfies evidence.sync, enabled or not.
	if finding(charSMCEntry(false, charCompleteCreds())) {
		t.Fatal("E4: a complete SM Cloud account must satisfy evidence.sync whatever its legacy enabled flag")
	}
	if !finding() {
		t.Fatal("E4: evidence.sync without any SM Cloud account must still be refused")
	}
}
