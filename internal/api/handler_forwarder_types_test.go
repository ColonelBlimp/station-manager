package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

// TestHandleForwarderTypes confirms the data-driven descriptor endpoint surfaces
// a registered type's display name, supported actions, and credential fields —
// what the config SPA's add-forwarder form renders from.
func TestHandleForwarderTypes(t *testing.T) {
	forwarding.RegisterForwarderType("apifwdtype-test", "API Test Forwarder",
		[]forwarding.Action{action.Insert, action.Delete},
		[]forwarding.CredentialField{{Key: "token", Label: "Token", Kind: "password", Help: "secret token"}})

	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/forwarder-types", nil)
	w := httptest.NewRecorder()
	srv.handleForwarderTypes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`"type":"apifwdtype-test"`,
		`"display_name":"API Test Forwarder"`,
		`"supported_actions"`,
		`"credential_fields"`,
		`"key":"token"`,
		`"kind":"password"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("forwarder-types body missing %q:\n%s", want, body)
		}
	}
}
