package qrz

// ST-4a — the QRZ API key travels in the request URL, so the resolved endpoint must be
// https, or http only to a loopback host. allow_insecure_http is SM-Cloud-only, so QRZ
// never bypasses this. Criteria A1/A8/A9.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestNew_TransportPolicy(t *testing.T) {
	creds, err := json.Marshal(map[string]string{"api_key": "KEY-123"})
	if err != nil {
		t.Fatal(err)
	}
	const leak = "qrz.leaky.example"
	cases := []struct {
		name     string
		endpoint string // "" = keep the https default
		wantOK   bool
	}{
		{"https default", "", true},
		{"http loopback", "http://127.0.0.1:9/logbook", true},
		{"https remote", "https://" + leak + "/logbook", true},
		{"http remote — refused", "http://" + leak + "/logbook", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := types.ForwarderConfig{Name: "q", Type: Type, Credentials: creds}
			if tc.endpoint != "" {
				fc.Endpoints = map[string]string{
					"insert": tc.endpoint, "update": tc.endpoint, "delete": tc.endpoint,
				}
			}
			_, err := New(fc)
			if (err == nil) != tc.wantOK {
				t.Fatalf("New(endpoint=%q) err=%v, wantOK=%v", tc.endpoint, err, tc.wantOK)
			}
			if err != nil && strings.Contains(err.Error(), leak) {
				t.Errorf("A9: error leaked the URL host: %q", err.Error())
			}
		})
	}
}
