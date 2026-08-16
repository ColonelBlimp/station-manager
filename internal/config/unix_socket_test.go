package config

// ST-5 — the default Unix socket path must resolve to an owner-private runtime directory
// ($XDG_RUNTIME_DIR, else $XDG_STATE_HOME, else $HOME/.local/state), NEVER /tmp, and a
// unix listener with no resolvable private path is a fatal config finding.

import (
	"strings"
	"testing"
)

func TestDefaultUnixSocketPath_Resolution(t *testing.T) {
	t.Run("XDG_RUNTIME_DIR wins", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
		t.Setenv("XDG_STATE_HOME", "/state")
		t.Setenv("HOME", "/home/op")
		p, err := defaultUnixSocketPath()
		if err != nil || p != "/run/user/1000/station-manager/smd.sock" {
			t.Fatalf("got (%q, %v), want /run/user/1000/station-manager/smd.sock", p, err)
		}
	})
	t.Run("XDG_STATE_HOME fallback", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "")
		t.Setenv("XDG_STATE_HOME", "/state")
		t.Setenv("HOME", "/home/op")
		p, err := defaultUnixSocketPath()
		if err != nil || p != "/state/station-manager/run/smd.sock" {
			t.Fatalf("got (%q, %v), want /state/station-manager/run/smd.sock", p, err)
		}
	})
	t.Run("HOME fallback", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "")
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("HOME", "/home/op")
		p, err := defaultUnixSocketPath()
		if err != nil || p != "/home/op/.local/state/station-manager/run/smd.sock" {
			t.Fatalf("got (%q, %v), want /home/op/.local/state/station-manager/run/smd.sock", p, err)
		}
	})
	t.Run("never /tmp; unresolvable is an error", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "")
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("HOME", "")
		p, err := defaultUnixSocketPath()
		if err == nil {
			t.Fatalf("want an error when no private base resolves, got %q", p)
		}
		if strings.Contains(p, "/tmp") {
			t.Errorf("resolver fell back to /tmp: %q", p)
		}
	})
}

// A unix listener whose default path could not be resolved (SocketPath left empty) is a
// FATAL finding — the daemon must not fall back to a shared directory.
func TestValidateServer_UnixUnresolvedIsFatal(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Server.Protocol = "unix"
	cfg.SocketPath = "" // resolution failed

	var fatal bool
	for _, f := range Validate(cfg) {
		if f.Code == "unix_socket_unresolved" {
			fatal = true
			if f.Warning {
				t.Error("unix_socket_unresolved must be fatal, not advisory")
			}
		}
	}
	if !fatal {
		t.Errorf("unix + empty socket_path produced no fatal finding; findings = %+v", Validate(cfg))
	}

	// A resolved (non-empty) unix path produces no such finding.
	cfg.SocketPath = "/run/user/1000/station-manager/smd.sock"
	for _, f := range Validate(cfg) {
		if f.Code == "unix_socket_unresolved" {
			t.Errorf("resolved unix path still flagged unresolved: %+v", f)
		}
	}
}
