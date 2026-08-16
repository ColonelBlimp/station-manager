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
	"strings"
	"testing"

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
