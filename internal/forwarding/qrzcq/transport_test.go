package qrzcq

// ST-4a — QRZCQ credentials travel in the request URL, so the resolved endpoint must be
// https, or http only to a loopback host. allow_insecure_http is SM-Cloud-only. A1/A8/A9.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestNew_TransportPolicy(t *testing.T) {
	creds, err := json.Marshal(map[string]string{"call": "M0ABC", "key": "KEY-123"})
	if err != nil {
		t.Fatal(err)
	}
	const leak = "qrzcq.leaky.example"
	cases := []struct {
		name     string
		endpoint string
		wantOK   bool
	}{
		{"https default", "", true},
		{"http loopback", "http://127.0.0.1:9/api/logupload", true},
		{"https remote", "https://" + leak + "/api/logupload", true},
		{"http remote — refused", "http://" + leak + "/api/logupload", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := types.ForwarderConfig{Name: "qc", Type: Type, Credentials: creds}
			if tc.endpoint != "" {
				fc.Endpoints = map[string]string{"insert": tc.endpoint}
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

// ST-4b — New installs the same-origin redirect policy on the client it builds.
func TestNew_InstallsRedirectPolicy(t *testing.T) {
	creds, err := json.Marshal(map[string]string{"call": "M0ABC", "key": "K"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := New(types.ForwarderConfig{Name: "qc", Type: Type, Credentials: creds})
	if err != nil {
		t.Fatal(err)
	}
	if f.(*Forwarder).client.CheckRedirect == nil {
		t.Error("New did not install the same-origin redirect policy (ST-4b)")
	}
}
