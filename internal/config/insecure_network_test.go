package config

// ST-3a (docs/reviews/internal-security-trust-boundary-audit.md) — a non-loopback TCP
// listener exposes the ENTIRE unauthenticated, unencrypted API (read/write log data,
// mutate config, restart the daemon, command the rig, key TX, run FT8) to every host
// that can reach the port. Before ST-3a this drifted through on an ADVISORY only: the
// daemon started and logged a warning that named just "QSO submits". Operator rulings
// 2026-08-16:
//
//   - Fail-closed. Every TCP bind that is not recognisably loopback — a specific
//     LAN/public IP, a wildcard (0.0.0.0 / :: / empty host), or a non-localhost
//     hostname — is FATAL at Load without an explicit acknowledgement. (Wildcard
//     included: loopback-Host trust in the CSRF guard is a DNS-rebinding defence, not
//     peer authentication, so a native LAN client can forge the Host — a wildcard is
//     exposed too.)
//   - The acknowledgement is server.allow_insecure_network (bool, default false),
//     config-file/startup-only and deliberately NOT on the /v1/config wire surface.
//   - With the ack, the daemon starts and logs a STANDING advisory enumerating the full
//     API + RF exposure ("including" phrasing so it stays accurate as routes are added).
//   - Loopback TCP and Unix sockets are unaffected.
//
// This is ST-3a (closing silent/unacknowledged exposure + doc drift). It does NOT claim
// authenticated LAN access is solved — that is ST-3b, an open topology decision.
//
// Acceptance criteria (operator-observable):
//   AC-1  non-loopback + unacknowledged  → Load returns an error naming allow_insecure_network
//         (daemon refuses to start). Apart from: a clean loopback start, and the old
//         advisory behaviour that started anyway.
//   AC-2  non-loopback + acknowledged    → Load succeeds AND Warnings() returns the
//         comprehensive advisory (names config mutation, rig control, FT8 — not just QSO
//         submit); the Validate finding is advisory (Warning==true). Apart from: the
//         fatal case, and a silent start.
//   AC-3  loopback / Unix                → no insecure finding, no advisory, ack or not.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The non-loopback binds that MUST require the acknowledgement (operator ruling 2).
var nonLoopbackBinds = []struct {
	name   string
	socket string
}{
	{"specific LAN IPv4", "192.168.1.10:8080"},
	{"specific public IPv4", "203.0.113.7:8080"},
	{"specific IPv6", "[2001:db8::1]:8080"},
	{"wildcard 0.0.0.0", "0.0.0.0:8080"},
	{"wildcard ::", "[::]:8080"},
	{"wildcard empty host", ":8080"},
	{"non-localhost hostname", "not-a-host:8080"},
}

// The binds that are recognisably safe and must NEVER require the acknowledgement.
var loopbackBinds = []struct {
	name     string
	protocol string
	socket   string
}{
	{"loopback IPv4", "tcp", "127.0.0.1:8080"},
	{"loopback IPv6", "tcp", "[::1]:8080"},
	{"localhost name", "tcp", "localhost:8080"},
	{"unix socket", "unix", "/tmp/smd.sock"},
}

// AC-1: an unacknowledged non-loopback TCP bind makes the daemon refuse to start —
// Load returns an error, and the message names the acknowledgement key so the operator
// knows the one deliberate switch that unblocks it.
func TestLoad_NonLoopbackBind_UnacknowledgedRefusesToStart(t *testing.T) {
	for _, tc := range nonLoopbackBinds {
		t.Run(tc.name, func(t *testing.T) {
			path := writeMinimalConfig(t, tc.socket, false)
			_, err := Load(path)
			if err == nil {
				t.Fatalf("Load(%q) succeeded; want a fatal refusal (non-loopback bind, no ack)", tc.socket)
			}
			if !strings.Contains(err.Error(), "allow_insecure_network") {
				t.Errorf("Load(%q) error = %q, want it to name allow_insecure_network", tc.socket, err)
			}
		})
	}
}

// AC-2 (start half): the SAME binds, once acknowledged, start cleanly.
func TestLoad_NonLoopbackBind_AcknowledgedStarts(t *testing.T) {
	for _, tc := range nonLoopbackBinds {
		t.Run(tc.name, func(t *testing.T) {
			path := writeMinimalConfig(t, tc.socket, true)
			if _, err := Load(path); err != nil {
				t.Fatalf("Load(%q) with allow_insecure_network=true failed: %v", tc.socket, err)
			}
		})
	}
}

// AC-2 (advisory half): an acknowledged non-loopback bind produces a Validate finding
// that is ADVISORY (not fatal), and a Warnings() line that names the FULL exposure —
// config mutation, rig control and FT8, not merely QSO submission. The old warning
// named only submits; this pins the comprehensive replacement.
func TestValidate_AcknowledgedNonLoopback_ComprehensiveAdvisory(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Server.Protocol = "tcp"
	cfg.SocketPath = "192.168.1.10:8080"
	cfg.Server.AllowInsecureNetwork = true

	// The finding must be advisory, never fatal (an acknowledged posture starts).
	var found bool
	for _, f := range Validate(cfg) {
		if f.Code == "insecure_bind" {
			found = true
			if f.Warning != true {
				t.Errorf("acknowledged insecure bind: finding Warning=%v, want advisory (true)", f.Warning)
			}
		}
		if f.Code == "insecure_bind_unacknowledged" {
			t.Errorf("acknowledged bind still produced a FATAL insecure_bind_unacknowledged finding")
		}
	}
	if !found {
		t.Fatalf("acknowledged non-loopback bind produced no advisory finding; findings = %+v", Validate(cfg))
	}

	ws := Warnings(cfg)
	joined := strings.ToLower(strings.Join(ws, "\n"))
	if len(ws) == 0 {
		t.Fatal("Warnings() returned nothing for an acknowledged non-loopback bind")
	}
	// The comprehensive-exposure requirement (operator ruling 5): the advisory must go
	// beyond "QSO submits" and name the read/config/RF effects. Assert a representative
	// spread rather than exact wording.
	for _, want := range []string{"unauthenticated", "config", "rig", "ft8"} {
		if !strings.Contains(joined, want) {
			t.Errorf("advisory does not mention %q; got: %s", want, joined)
		}
	}
}

// AC-1 (Validate half): the fatal finding is emitted for every non-loopback shape and
// is NON-advisory (Warning==false), which is what makes Load refuse to start.
func TestValidate_UnacknowledgedNonLoopback_Fatal(t *testing.T) {
	for _, tc := range nonLoopbackBinds {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir())
			cfg.Server.Protocol = "tcp"
			cfg.SocketPath = tc.socket
			cfg.Server.AllowInsecureNetwork = false

			var fatal bool
			for _, f := range Validate(cfg) {
				if f.Code == "insecure_bind_unacknowledged" {
					fatal = true
					if f.Warning {
						t.Errorf("insecure_bind_unacknowledged is advisory; must be fatal (Warning==false)")
					}
				}
			}
			if !fatal {
				t.Errorf("bind %q produced no fatal finding; findings = %+v", tc.socket, Validate(cfg))
			}
		})
	}
}

// AC-3: loopback TCP and Unix sockets never require the acknowledgement and never emit
// an insecure finding, whether or not the flag is set.
func TestValidate_LoopbackAndUnix_NeverInsecure(t *testing.T) {
	for _, ack := range []bool{false, true} {
		for _, tc := range loopbackBinds {
			t.Run(tc.name, func(t *testing.T) {
				cfg := DefaultConfig(t.TempDir())
				cfg.Server.Protocol = tc.protocol
				cfg.SocketPath = tc.socket
				cfg.Server.AllowInsecureNetwork = ack

				for _, f := range Validate(cfg) {
					if f.Code == "insecure_bind" || f.Code == "insecure_bind_unacknowledged" {
						t.Errorf("bind %q (ack=%v) produced insecure finding %q; want none",
							tc.socket, ack, f.Code)
					}
				}
				if ws := Warnings(cfg); len(ws) != 0 {
					t.Errorf("bind %q (ack=%v) produced advisories %v; want none", tc.socket, ack, ws)
				}
			})
		}
	}
}

// writeMinimalConfig writes a config.json with the given TCP socket + acknowledgement,
// otherwise defaults. Returns the file path.
func writeMinimalConfig(t *testing.T, socket string, ack bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	ackJSON := ""
	if ack {
		ackJSON = `, "allow_insecure_network": true`
	}
	content := `{
		"socket_path": "` + socket + `",
		"server": {"protocol": "tcp"` + ackJSON + `},
		"datastore": {"driver": "sqlite", "path": "/tmp/test.db"},
		"logging": {"level": "warn"}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}
