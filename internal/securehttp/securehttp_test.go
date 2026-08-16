package securehttp

// ST-4a (docs/reviews/internal-security-trust-boundary-audit.md) — the shared
// credential-transport policy. Operator rulings 2026-08-16:
//   - HTTPS to any valid host; plain HTTP only to an exact localhost or a literal
//     loopback address (netip.ParseAddr().IsLoopback(), so zoned ::1%lo is accepted
//     consistently with ST-3a). No hostname resolution, no RFC1918 inference.
//   - allowInsecure (the SM-Cloud acknowledgement) permits plain HTTP to ANY host and
//     nothing else — an unparseable/hostless value is still rejected.
//   - Errors never echo the URL or any component of it (a scheme/userinfo can be a
//     pasted credential).
//
// Criteria exercised here: A8 (loopback accepted for every client), A9 (errors carry no
// URL), plus the IsInsecureRemote trigger for the A3 startup warning.

import (
	"strings"
	"testing"
)

func TestCheckCredentialedURL(t *testing.T) {
	// A URL host that, if it ever leaked into an error, we can grep for.
	const secretHost = "leaky-host.example"
	cases := []struct {
		name          string
		url           string
		allowInsecure bool
		wantOK        bool
	}{
		{"https remote", "https://" + secretHost + "/api", false, true},
		{"https loopback", "https://127.0.0.1:8080", false, true},
		{"http loopback v4", "http://127.0.0.1:8080", false, true},
		{"http loopback v6", "http://[::1]:8080", false, true},
		{"http zoned loopback v6", "http://[::1%25lo]:8080", false, true}, // %25 = URL-encoded zone (RFC 6874)
		{"http localhost", "http://localhost:8080", false, true},
		{"http remote — REJECT", "http://" + secretHost + ":8091", false, false},
		{"http RFC1918 — REJECT (no private inference)", "http://192.168.1.20:8091", false, false},
		{"http remote + ack — OK", "http://" + secretHost + ":8091", true, true},
		{"http RFC1918 + ack — OK", "http://192.168.1.20:8091", true, true},
		{"unparseable + ack still REJECT", "://nope", true, false},
		{"schemeless + ack still REJECT", "leaky-host.example:8091", true, false},
		{"ftp scheme — REJECT", "ftp://" + secretHost, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckCredentialedURL(tc.url, tc.allowInsecure)
			if (err == nil) != tc.wantOK {
				t.Fatalf("CheckCredentialedURL(%q, ack=%v) err=%v, wantOK=%v", tc.url, tc.allowInsecure, err, tc.wantOK)
			}
			// A9: on rejection, the error must not echo the URL host or any part of it.
			if err != nil && strings.Contains(err.Error(), secretHost) {
				t.Errorf("error leaked the URL host: %q", err.Error())
			}
			if err != nil && strings.Contains(err.Error(), "192.168.1.20") {
				t.Errorf("error leaked the URL host: %q", err.Error())
			}
		})
	}
}

func TestIsInsecureRemote(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://192.168.1.20:8091", true},
		{"http://cloud.example:8091", true},
		{"https://cloud.example", false},  // encrypted
		{"http://127.0.0.1:8091", false},  // loopback
		{"http://[::1%25lo]:8091", false}, // zoned loopback (URL-encoded zone)
		{"http://localhost:8091", false},  // localhost
		{"not a url", false},              // unparseable → not classified insecure-remote
	}
	for _, tc := range cases {
		if got := IsInsecureRemote(tc.url); got != tc.want {
			t.Errorf("IsInsecureRemote(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}
