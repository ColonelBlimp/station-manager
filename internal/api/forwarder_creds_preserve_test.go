package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// A11 — MALFORMED STORED FORWARDER CREDENTIALS MUST NOT BE SILENTLY DROPPED.
//
// CRITERION (operator, 2026-08-12):
//
//	When a forwarder's stored credentials cannot be decoded (a corrupt or
//	hand-edited config.json), a config PUT that does NOT re-enter them PRESERVES
//	them unchanged — it never silently blanks them — and the daemon logs a warning
//	naming the forwarder + type + the decode error, never the credential bytes.
//	Tellable apart from the current bug (and from a normal save) by: the stored
//	value survives the round-trip, and smd.log carries the decode warning.
//
// DECISION (operator, 2026-08-12): on decode failure ALWAYS preserve the stored
// bytes and ignore this PUT's credential edits for that forwarder until config.json
// is fixed — the most conservative reading of "refuse to erase what we cannot
// classify" (handler_config.go clearableCredentialKeys). The masked-GET display
// half (credentialKeysSet returning nil for a corrupt blob) is deferred as A11b.
//
// THE PRE-FIX BUG: mergeForwarders did `_ = json.Unmarshal(ex.Credentials, &base)`
// and, on failure, rebuilt the credential block from an EMPTY base. A masked-on-GET
// save (which carries no credential values) therefore DROPPED the stored secret and
// still returned 200 — data loss the operator learns about only when the forwarder
// later stops working.

// a11CorruptCred is valid JSON but a STRING, not the object the merge expects, so it
// fails to unmarshal into map[string]json.RawMessage. The recognizable token lets the
// no-leak assertion catch any path that logs the blob itself.
const a11CorruptCred = `"SEKRET-abc123-should-never-appear"`

// corruptCredServer seeds ONE clublog forwarder holding the given credential bytes
// and captures the server log in a buffer.
func corruptCredServer(t *testing.T, creds string) (*Server, *strings.Builder) {
	t.Helper()
	buf := &strings.Builder{}
	srv := testServerWithLogger(t, func(c *config.Config) {
		c.Forwarders = []types.ForwarderConfig{{
			Name: "clublog", Type: "clublog", Enabled: true,
			Credentials: json.RawMessage(creds),
		}}
	}, nil, logging.NewForWriter(buf))
	return srv, buf
}

// putForwarderDisable sends exactly what the Forwarding tab sends for an unrelated
// change: name/type/enabled, NO credentials. This masked-on-GET default is the drop
// trigger. Disabling the forwarder is also what lets the save COMMIT (an ENABLED
// forwarder with dropped/corrupt creds would fail config.ForwarderStartupFinding → 400).
func putForwarderDisable(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"forwarders":[{"name":"clublog","type":"clublog","enabled":false}]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	return w
}

// credWarnRecords parses the captured log and returns records whose message contains
// sub.
func credWarnRecords(t *testing.T, buf *strings.Builder, sub string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if msg, _ := rec["message"].(string); strings.Contains(msg, sub) {
			out = append(out, rec)
		}
	}
	return out
}

// A11a — CORRUPT STORED CREDENTIALS SURVIVE AN UNRELATED SAVE. The data-loss half:
// pre-fix the empty-base rebuild left the stored bytes blank after a committing PUT.
func TestForwarderCreds_CorruptBlobPreservedAcrossSave(t *testing.T) {
	srv, _ := corruptCredServer(t, a11CorruptCred)

	if w := putForwarderDisable(t, srv); w.Code != http.StatusOK {
		t.Fatalf("fixture: the save must COMMIT or the rule proves nothing; got %d: %s",
			w.Code, w.Body.String())
	}

	stored := srv.cfg.Snapshot().Forwarders
	if len(stored) != 1 {
		t.Fatalf("fixture: want 1 stored forwarder, got %d: %+v", len(stored), stored)
	}
	// The save really applied (not a no-op that would prove nothing)…
	if stored[0].Enabled {
		t.Fatalf("fixture: the save did not apply; forwarder still enabled: %+v", stored[0])
	}
	// …and the stored credential bytes are unchanged, not blanked.
	if got := string(stored[0].Credentials); got != a11CorruptCred {
		t.Errorf("stored credentials = %q after an unrelated save, want %q — an undecodable "+
			"blob was rebuilt from an empty base and DROPPED (A11 data-loss)", got, a11CorruptCred)
	}
}

// A11-log — THE PRESERVATION IS ANNOUNCED, AND THE SECRET NEVER LEAKS. The corruption
// must be visible in smd.log (name + type) so the operator can fix config.json, and
// the credential bytes must not appear anywhere in the 0644 log.
func TestForwarderCreds_CorruptBlobLogsWarningWithoutLeaking(t *testing.T) {
	srv, buf := corruptCredServer(t, a11CorruptCred)

	if w := putForwarderDisable(t, srv); w.Code != http.StatusOK {
		t.Fatalf("fixture: the save must COMMIT; got %d: %s", w.Code, w.Body.String())
	}

	// Match the A11a merge message specifically: buildConfigResponse (called to build
	// the PUT's 200 body) ALSO emits the A11b "masked view" warning for the same
	// corruption, and both messages share "credentials undecodable".
	recs := credWarnRecords(t, buf, "preserved unchanged, edits ignored")
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 merge-preserve warning, got %d; log:\n%s", len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn", lvl)
	}
	if fwd, _ := recs[0]["forwarder"].(string); fwd != "clublog" {
		t.Errorf("forwarder = %q, want clublog", fwd)
	}
	if typ, _ := recs[0]["type"].(string); typ != "clublog" {
		t.Errorf("type = %q, want clublog", typ)
	}
	// The credential bytes must NEVER reach the log (smd.log is 0644).
	if strings.Contains(buf.String(), "SEKRET-abc123") {
		t.Errorf("credential value leaked into smd.log:\n%s", buf.String())
	}
}

// A11c — A VALID STORED BLOB IS NOT FLAGGED (positive control). A decodable credential
// set is preserved across the same save AND logs no warning, so the corruption warning
// is tellable apart from an ordinary merge. (Green before and after the fix by design —
// it guards the fix against false positives, it does not pin it.)
func TestForwarderCreds_ValidBlobPreservedNoWarning(t *testing.T) {
	srv, buf := corruptCredServer(t, `{"email":"a@b.com","callsign":"7Q5MLV"}`)

	if w := putForwarderDisable(t, srv); w.Code != http.StatusOK {
		t.Fatalf("fixture: the save must COMMIT; got %d: %s", w.Code, w.Body.String())
	}

	stored := srv.cfg.Snapshot().Forwarders
	if len(stored) != 1 || !strings.Contains(string(stored[0].Credentials), "7Q5MLV") {
		t.Fatalf("valid credentials were not preserved across the save: %+v", stored)
	}
	if recs := credWarnRecords(t, buf, "credentials undecodable"); len(recs) != 0 {
		t.Fatalf("a valid credential blob produced %d undecodable warning(s): %v", len(recs), recs)
	}
}
