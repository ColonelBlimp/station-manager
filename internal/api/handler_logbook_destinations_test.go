package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Codex P2 on 7990011b: the logbook view must offer the SELECTED logbook's own
// binding names — an additional logbook's `qrz.<uuid>`, never the default
// logbook's `qrz` — so a backfill from it routes instead of skipping every row.
func TestLogbookDestinations_ListsTheLogbooksOwnBindings(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert"))
	lb1 := createTestLogbook(t, srv, "Main", "M0ABC")
	lb2 := createTestLogbook(t, srv, "Second", "M0XYZ") // bindRoutesFromConfig names it qrz.<uuid>
	second := srv.qso.DestinationRoutes()[1].Config.Name

	get := func(id int64) string {
		req := httptest.NewRequest(http.MethodGet, "/v1/logbook/1/destinations", nil)
		req.SetPathValue("id", strconv.FormatInt(id, 10))
		w := httptest.NewRecorder()
		srv.handleLogbookDestinations(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if b := get(lb1); !strings.Contains(b, `"name":"qrz"`) || strings.Contains(b, second) {
		t.Fatalf("logbook 1 destinations = %s; want qrz only", b)
	}
	if b := get(lb2); !strings.Contains(b, `"name":"`+second+`"`) || strings.Contains(b, `"name":"qrz"`) {
		t.Fatalf("logbook 2 destinations = %s; want its own binding only", b)
	}
	if b := get(99); !strings.Contains(b, `"destinations":[]`) {
		t.Fatalf("unknown logbook = %s; want an empty list", b)
	}
	// Disabled bindings are listed with their state; nothing carries a credential.
	srv.qso.SetDestinationRoutes([]forwarding.BoundForwarder{{LogbookID: lb1, Config: types.ForwarderConfig{
		Name: "qrz", Type: "qrz", Enabled: false, Credentials: []byte(`{"api_key":"SECRET"}`)}}})
	if b := get(lb1); !strings.Contains(b, `"enabled":false`) || strings.Contains(b, "SECRET") {
		t.Fatalf("disabled binding = %s; want enabled:false and no credential", b)
	}
}

// missing_from addresses a binding name too: the additional logbook's binding
// filters its own logbook; a name no binding carries is refused by that wording.
func TestQsoList_MissingFromAcceptsBindingNames(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert"))
	createTestLogbook(t, srv, "Main", "M0ABC")
	lb2 := createTestLogbook(t, srv, "Second", "M0XYZ")
	second := srv.qso.DestinationRoutes()[1].Config.Name

	req := httptest.NewRequest(http.MethodGet, "/v1/logbook/2/qso?missing_from="+second, nil)
	req.SetPathValue("id", strconv.FormatInt(lb2, 10))
	w := httptest.NewRecorder()
	srv.handleListQsoByLogbook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("missing_from with a binding name: status %d body %s", w.Code, w.Body.String())
	}
	// Another logbook's binding is not a destination of THIS logbook, nor is a
	// station config name the archive holds no binding for (operator review, P2).
	for _, name := range []string{"qrz", "nope"} {
		req = httptest.NewRequest(http.MethodGet, "/v1/logbook/2/qso?missing_from="+name, nil)
		req.SetPathValue("id", strconv.FormatInt(lb2, 10))
		w = httptest.NewRecorder()
		srv.handleListQsoByLogbook(w, req)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "binding of this logbook") {
			t.Fatalf("missing_from=%s on logbook 2: status %d body %s; want 400 naming this logbook's bindings", name, w.Code, w.Body.String())
		}
	}
	// The count endpoint applies the same scope.
	req = httptest.NewRequest(http.MethodGet, "/v1/logbook/2/count?missing_from=qrz", nil)
	req.SetPathValue("id", strconv.FormatInt(lb2, 10))
	w = httptest.NewRecorder()
	srv.handleLogbookCount(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("count with another logbook's binding: status %d body %s; want 400", w.Code, w.Body.String())
	}
}
