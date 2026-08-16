package smcloud

// ST-4a (docs/reviews/internal-security-trust-boundary-audit.md) — the SM-Cloud client
// carries a bearer token + full QSO/evidence data, so its URL must be https, or http
// only to a loopback host, UNLESS the operator sets allow_insecure_http (the LAN-staging
// acknowledgement). Criteria: A1 (remote http refused without ack), A3-build (acknowledged
// remote http builds), A7 (export/reconcile fail before any request), A8 (loopback/https
// accepted), A9 (errors carry no URL).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const leakHost = "cloud.leaky.example" // if this appears in an error, the URL leaked

func fcWith(t *testing.T, url string, ack bool) types.ForwarderConfig {
	t.Helper()
	creds, err := json.Marshal(map[string]string{"url": url, "token": "tok-123", "logbook": "main"})
	if err != nil {
		t.Fatal(err)
	}
	return types.ForwarderConfig{Name: "cloud", Type: Type, Credentials: creds, AllowInsecureHTTP: ack}
}

func TestNew_TransportPolicy(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		ack    bool
		wantOK bool
	}{
		{"https remote", "https://" + leakHost + "/", false, true},
		{"http loopback", "http://127.0.0.1:8091", false, true},
		{"http localhost", "http://localhost:8091", false, true},
		{"http remote, NO ack — refused", "http://" + leakHost + ":8091", false, false},
		{"http remote + ack — built", "http://" + leakHost + ":8091", true, true},
		{"http RFC1918, NO ack — refused (no private inference)", "http://192.168.1.20:8091", false, false},
		{"http RFC1918 + ack — built", "http://192.168.1.20:8091", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(fcWith(t, tc.url, tc.ack))
			if (err == nil) != tc.wantOK {
				t.Fatalf("New(%q, ack=%v) err=%v, wantOK=%v", tc.url, tc.ack, err, tc.wantOK)
			}
			if err != nil && strings.Contains(err.Error(), leakHost) {
				t.Errorf("A9: error leaked the URL host: %q", err.Error())
			}
		})
	}
}

func TestInsecureRemoteURL(t *testing.T) {
	cases := []struct {
		url  string
		ack  bool
		want bool
	}{
		{"http://" + leakHost + ":8091", true, true}, // cleartext remote → warn
		{"https://" + leakHost, true, false},         // encrypted → no warn
		{"http://127.0.0.1:8091", false, false},      // loopback → no warn
	}
	for _, tc := range cases {
		if got := InsecureRemoteURL(fcWith(t, tc.url, tc.ack)); got != tc.want {
			t.Errorf("InsecureRemoteURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
	// Non-smcloud type never warns, whatever its URL.
	other := types.ForwarderConfig{Name: "x", Type: "qrz", AllowInsecureHTTP: true}
	if InsecureRemoteURL(other) {
		t.Error("InsecureRemoteURL returned true for a non-smcloud forwarder")
	}
}

// A7: FetchExport reuses New for credential parsing + validation BEFORE it builds the
// export GET, so an unacknowledged remote-http config fails before any request leaves.
// (CloudLogbookName routes through New too — a second entry point that rejects.) Neither
// error may leak the URL.
func TestExport_FailsBeforeRequest(t *testing.T) {
	bad := fcWith(t, "http://"+leakHost+":8091", false)

	if _, err := FetchExport(context.Background(), bad); err == nil {
		t.Error("FetchExport accepted an unacknowledged remote-http config (should fail before the GET)")
	} else if strings.Contains(err.Error(), leakHost) {
		t.Errorf("A9: FetchExport error leaked the URL host: %q", err.Error())
	}

	if _, err := CloudLogbookName(bad); err == nil {
		t.Error("CloudLogbookName accepted an unacknowledged remote-http config")
	} else if strings.Contains(err.Error(), leakHost) {
		t.Errorf("A9: CloudLogbookName error leaked the URL host: %q", err.Error())
	}
}

// ST-4b — a cross-origin redirect from the configured origin is refused: the foreign
// sink receives ZERO requests, the token never leaves the origin, and the Result error
// carries no URL or credential. Covers the Submit (bearer PUT) and FetchExport paths.
func redirectPair(t *testing.T, token string) (originURL string, foreignHits *int) {
	t.Helper()
	hits := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(foreign.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+r.URL.Path, http.StatusFound) // cross-port → cross-origin
	}))
	t.Cleanup(origin.Close)
	return origin.URL, &hits
}

func TestSubmit_RefusesCrossOriginRedirect(t *testing.T) {
	const token = "BEARER-SENTINEL-xyz"
	originURL, foreignHits := redirectPair(t, token)
	creds, err := json.Marshal(map[string]string{"url": originURL, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	f, err := New(types.ForwarderConfig{Name: "cloud", Type: Type, Credentials: creds})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res := f.Submit(context.Background(), testQso("uuid-redir"), action.Insert, "")
	if res.Err == nil {
		t.Fatal("Submit followed a cross-origin redirect; want a failure")
	}
	if *foreignHits != 0 {
		t.Errorf("foreign sink received %d requests, want 0", *foreignHits)
	}
	if s := res.Err.Error(); strings.Contains(s, token) || strings.Contains(s, "127.0.0.1") {
		t.Errorf("Result error leaked URL/token: %q", s)
	}
}

func TestFetchExport_RefusesCrossOriginRedirect(t *testing.T) {
	const token = "BEARER-SENTINEL-exp"
	originURL, foreignHits := redirectPair(t, token)
	creds, err := json.Marshal(map[string]string{"url": originURL, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	fc := types.ForwarderConfig{Name: "cloud", Type: Type, Credentials: creds}

	_, err = FetchExport(context.Background(), fc)
	if err == nil {
		t.Fatal("FetchExport followed a cross-origin redirect; want a failure")
	}
	if *foreignHits != 0 {
		t.Errorf("foreign sink received %d requests, want 0", *foreignHits)
	}
	if s := err.Error(); strings.Contains(s, token) || strings.Contains(s, "127.0.0.1") {
		t.Errorf("FetchExport error leaked URL/token: %q", s)
	}
}
