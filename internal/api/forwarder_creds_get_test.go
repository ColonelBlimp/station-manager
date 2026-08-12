package api

import (
	"strings"
	"testing"
)

// A11b — THE MASKED GET SIDE OF THE CORRUPT-CREDENTIALS DEFECT.
//
// CRITERION (operator, 2026-08-12):
//
//	When a forwarder's stored credentials cannot be decoded, GET /v1/config still
//	reports "no credentials set" for it (the masked view can enumerate no keys) — but
//	the daemon logs a warning naming the forwarder so the operator can find the
//	corruption, and it logs at most ONCE however many times the config is read.
//
// GET /v1/config is re-called freely (every Settings open, every save), so a bare
// per-GET log would flood; the warning is latched once per forwarder per process
// (cf. bridge B14 — a latch, never a guessed rate). The A11a merge fix already stops
// the data loss; this half is display + observability only, so the masked view's
// "unset" reporting is deliberately unchanged.
//
// Reuses corruptCredServer / a11CorruptCred / credWarnRecords from
// forwarder_creds_preserve_test.go and getConfigForwarders / findInfo from
// forwarder_label_test.go (same package).

// A11b-latch — A CORRUPT BLOB WARNS ONCE ACROSS REPEATED READS, AND SHOWS AS UNSET.
func TestForwarderCreds_CorruptBlobGetLogsOnceMaskedUnset(t *testing.T) {
	srv, buf := corruptCredServer(t, a11CorruptCred)

	// The masked view is rebuilt on each GET; the warning must NOT be.
	list := getConfigForwarders(t, srv)
	_ = getConfigForwarders(t, srv)

	recs := credWarnRecords(t, buf, "masked view shows them unset")
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 masked-unset warning across 2 GETs (latch), got %d; log:\n%s",
			len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn", lvl)
	}
	if fwd, _ := recs[0]["forwarder"].(string); fwd != "clublog" {
		t.Errorf("forwarder = %q, want clublog", fwd)
	}

	// The masked view reports NO credential keys for the corrupt blob — unchanged
	// display behaviour (the fix is the log + the A11a preservation, not the display).
	cl, ok := findInfo(list, "clublog")
	if !ok {
		t.Fatalf("clublog missing from GET: %+v", list)
	}
	if len(cl.CredentialsSet) != 0 {
		t.Errorf("CredentialsSet = %v, want empty for a corrupt blob", cl.CredentialsSet)
	}
	// The credential bytes must never reach the 0644 log.
	if strings.Contains(buf.String(), "SEKRET-abc123") {
		t.Errorf("credential value leaked into smd.log:\n%s", buf.String())
	}
}

// A11b-control — A VALID BLOB IS NOT FLAGGED AND ITS KEYS ARE LISTED. Tells the
// corruption warning apart from a normal masked view. (Green before and after the
// fix by design — it guards against a false positive, it does not pin the fix.)
func TestForwarderCreds_ValidBlobGetNoWarningKeysListed(t *testing.T) {
	srv, buf := corruptCredServer(t, `{"email":"a@b.com","callsign":"7Q5MLV"}`)

	list := getConfigForwarders(t, srv)

	if recs := credWarnRecords(t, buf, "masked view shows them unset"); len(recs) != 0 {
		t.Fatalf("a valid credential blob produced %d masked-unset warning(s): %v", len(recs), recs)
	}
	cl, ok := findInfo(list, "clublog")
	if !ok {
		t.Fatalf("clublog missing from GET: %+v", list)
	}
	if len(cl.CredentialsSet) == 0 {
		t.Errorf("CredentialsSet empty for valid creds; want the keys listed (email, callsign)")
	}
}
