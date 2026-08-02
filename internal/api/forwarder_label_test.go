package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// FORWARDER DISPLAY LABEL — OPERATOR-SET IN config.json, READ-ONLY EVERYWHERE ELSE.
//
// CRITERION (operator, 2026-08-02):
//
//	When I set a forwarder's label in config.json and restart, the Settings tab
//	shows my label — and a destination with no label still shows the built-in
//	name rather than a blank or a raw type string.
//
// WHY IT IS NOT A GO CONSTANT. The built-in display name lives in the binary
// (smcloud.go:96 "SM Cloud backup"), so changing it is a build + deploy, and the
// operator cannot do it at all. That name is already dated — the service is
// growing past "backup" — and will move again. A config field makes renaming an
// edit and a restart.
//
// WHY IT IS NOT `name`. `name` is the durable key: qso_upload constrains
// UNIQUE (qso_id, forwarder_name, action) on it, so renaming it would make the
// daemon forget which QSOs had already been sent and re-upload them to ClubLog
// and QRZ. `label` is free text nothing joins on.
//
// THE RULE THAT MATTERS IS L2, AND IT IS NOT ABOUT THE LABEL. mergeForwarders
// rebuilds each entry from the SPA's payload and carries only what it names
// explicitly (tick/batch/retry). `label` is not on the wire — the SPA has no
// control for it, deliberately — so without an explicit carry-over, ANY save
// from the Settings tab silently deletes a label the operator hand-set. The
// operator's "no guarantees if you hand-edit" caveat does not cover us deleting
// their value during an unrelated save.
//
// SAME CLASS, ALREADY PRESENT: `Endpoints` is NOT carried over either. A save
// writes it out empty and the next Load re-seeds the registry defaults
// (config.go:1077), so a CUSTOMISED endpoint is silently reverted. L3 pins it,
// because the Forwarding tab has just made that path reachable from the app.

func labelTestServer(t *testing.T, mutate func(*config.Config)) *Server {
	t.Helper()
	return testServerWithCfg(t, mutate)
}

func seedForwarders(fwds ...types.ForwarderConfig) func(*config.Config) {
	return func(c *config.Config) { c.Forwarders = fwds }
}

// getConfigForwarders reads the masked forwarder list back out of GET /v1/config.
func getConfigForwarders(t *testing.T, srv *Server) []ForwarderInfo {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/config = %d: %s", w.Code, w.Body.String())
	}
	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Forwarders
}

func findInfo(list []ForwarderInfo, name string) (ForwarderInfo, bool) {
	for _, f := range list {
		if f.Name == name {
			return f, true
		}
	}
	return ForwarderInfo{}, false
}

// L1 — A LABEL SET IN config.json REACHES THE CLIENT. Without this the field is
// write-only and the SPA can never show it.
func TestForwarderLabel_IsServedToTheClient(t *testing.T) {
	srv := labelTestServer(t, seedForwarders(
		types.ForwarderConfig{Name: "smcloud", Type: "smcloud", Label: "Shack cloud"},
		types.ForwarderConfig{Name: "qrz", Type: "qrz"},
	))

	list := getConfigForwarders(t, srv)
	sm, ok := findInfo(list, "smcloud")
	if !ok {
		t.Fatalf("smcloud missing from GET: %+v", list)
	}
	if sm.Label != "Shack cloud" {
		t.Errorf("label = %q, want %q", sm.Label, "Shack cloud")
	}

	// An unlabelled destination reports NO label, so the client can fall back to
	// the built-in display name rather than rendering an empty heading.
	qrz, ok := findInfo(list, "qrz")
	if !ok {
		t.Fatal("qrz missing from GET")
	}
	if qrz.Label != "" {
		t.Errorf("unlabelled forwarder reported label %q, want empty", qrz.Label)
	}
}

// L2 — A SETTINGS SAVE MUST NOT DELETE THE LABEL. The SPA has no label control,
// so the field is absent from every PUT it sends. mergeForwarders carries over
// only what it names, and anything unnamed is silently dropped.
func TestForwarderLabel_SurvivesASaveThatDoesNotCarryIt(t *testing.T) {
	srv := labelTestServer(t, seedForwarders(
		types.ForwarderConfig{Name: "smcloud", Type: "smcloud", Label: "Shack cloud", Enabled: true},
	))

	// Exactly what the Forwarding tab sends: name/type/enabled, no label.
	body := `{"forwarders":[{"name":"smcloud","type":"smcloud","enabled":false}]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("fixture: the save must COMMIT or this rule proves nothing; got %d: %s",
			w.Code, w.Body.String())
	}

	// The save really happened (so the rule is not passing on a no-op)…
	stored := srv.cfg.Snapshot().Forwarders
	if len(stored) != 1 || stored[0].Enabled {
		t.Fatalf("fixture: the save did not apply; stored = %+v", stored)
	}
	// …and the label the operator set by hand is still there.
	if stored[0].Label != "Shack cloud" {
		t.Errorf("label = %q after an unrelated save, want %q — a Settings save "+
			"deleted a value only config.json can set", stored[0].Label, "Shack cloud")
	}
}

// L3 — AND NEITHER MAY A SAVE DISCARD CUSTOM ENDPOINTS. Same defect class,
// already present before the label existed: mergeForwarders does not carry
// Endpoints, so the save writes them empty and the next Load re-seeds the
// registry DEFAULT over the operator's override.
func TestForwarderEndpoints_SurviveASaveThatDoesNotCarryThem(t *testing.T) {
	custom := map[string]string{"insert": "https://mirror.example.com/realtime.php"}
	srv := labelTestServer(t, seedForwarders(
		types.ForwarderConfig{
			Name: "clublog", Type: "clublog", Enabled: true, Endpoints: custom,
		},
	))

	body := `{"forwarders":[{"name":"clublog","type":"clublog","enabled":false}]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("fixture: the save must COMMIT; got %d: %s", w.Code, w.Body.String())
	}

	stored := srv.cfg.Snapshot().Forwarders
	if len(stored) != 1 || stored[0].Enabled {
		t.Fatalf("fixture: the save did not apply; stored = %+v", stored)
	}
	if got := stored[0].Endpoints["insert"]; got != custom["insert"] {
		t.Errorf("insert endpoint = %q after an unrelated save, want %q — the "+
			"operator's override was dropped and will be replaced by the "+
			"registry default at the next start", got, custom["insert"])
	}
}
