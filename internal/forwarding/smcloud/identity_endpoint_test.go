package smcloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

// T5: an identity-path 404 says the endpoint is unavailable, even if its body
// cannot be read. Every Submit makes just one request; no name fallback.
// Startup construction of identity workers is the later 5F.3 switching slice.
func TestSubmit_IdentityEndpointUnavailable(t *testing.T) {
	for _, brokenBody := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain 404", true: "truncated 404"}[brokenBody], func(t *testing.T) {
			const path = "/v1/archives/0197f9a0-0000-7000-8000-000000000001/logbooks/0197f9a0-0000-7000-8000-000000000002/qsos"
			requests := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodPut || r.URL.Path != path {
					t.Errorf("unexpected request (name fallback): %s %s", r.Method, r.URL.Path)
				}
				if brokenBody {
					w.Header().Set("Content-Length", "100")
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			}))
			defer ts.Close()
			f, err := New(testConfig(ts.URL))
			if err != nil {
				t.Fatal(err)
			}
			fwd := f.(*Forwarder)
			fwd.putURL, fwd.identity = ts.URL+path, true
			for i := 1; i <= 7; i++ {
				res := fwd.Submit(context.Background(), testQso("0197f9a0-0000-7000-8000-000000000003"), action.Insert, "")
				if res.Outcome != forwarding.Outcome("endpoint_unavailable") {
					t.Fatalf("attempt %d: outcome = %q (%v), want endpoint_unavailable", i, res.Outcome, res.Err)
				}
				if res.Err == nil || !strings.Contains(res.Err.Error(), "identity endpoint is unavailable") || !strings.Contains(res.Err.Error(), "404") {
					t.Fatalf("missing identity endpoint diagnostic: %v", res.Err)
				}
				if res.Class != "" || res.UpstreamID != "" || requests != i {
					t.Fatalf("404 must neither complete nor fall back: result=%+v requests=%d attempts=%d", res, requests, i)
				}
			}
		})
	}
}

func TestSubmit_Legacy404RemainsTerminal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/qsos" {
			t.Errorf("legacy path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	f, err := New(testConfig(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	res := f.Submit(context.Background(), testQso("0197f9a0-0000-7000-8000-000000000003"), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal || res.Err == nil || res.Class != "" {
		t.Fatalf("legacy 404 changed: %+v", res)
	}
}
