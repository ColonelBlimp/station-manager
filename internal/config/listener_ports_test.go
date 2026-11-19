package config

// The ports of smd's two listeners (operator ruling 2026-11-19, "approved, leave explicit 0
// as the default"; W-0027 C2e follow-up).
//
// The main listener, when server.protocol is "tcp": socket_path is host:port with a plain
// decimal port from 1024 to 65535. A service name, an empty or missing port, a sign, zero
// and anything outside the range are refused at load, on socket_path itself. A host name is
// allowed (what it resolves to is the bind's to find out); a Unix-socket path is not an
// address and is outside the rule.
//
// The desktop app's listener: server.app_port left out, or 0, means the default 8443. A
// value that is given must be from 1024 to 65535.
//
// 1024 is where an ordinary user may bind on a stock Linux system, and smd runs as a user
// service. It is Station Manager's supported range, not a claim about every system.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func firstBlocking(cfg Config) *Finding {
	for _, f := range Validate(cfg) {
		if !f.Warning {
			return &f
		}
	}
	return nil
}

func TestMainListenerPort_Validation(t *testing.T) {
	for _, c := range []struct {
		name     string
		protocol string
		socket   string
		refused  bool
	}{
		{"the default", "tcp", "127.0.0.1:8080", false},
		{"the lowest port", "tcp", "127.0.0.1:1024", false},
		{"the highest port", "tcp", "127.0.0.1:65535", false},
		{"leading zeros are decimal", "tcp", "127.0.0.1:08080", false},
		{"the IPv6 loopback", "tcp", "[::1]:8080", false},
		// A host name is allowed: only its port is judged here.
		{"a host name with a numeric port", "tcp", "localhost:8080", false},

		{"one below the range", "tcp", "127.0.0.1:1023", true},
		{"a low port", "tcp", "127.0.0.1:80", true},
		{"one above the range", "tcp", "127.0.0.1:65536", true},
		{"far above the range", "tcp", "127.0.0.1:99999", true},
		{"more digits than any integer", "tcp", "127.0.0.1:99999999999999999999999", true},
		// Zero and an empty port ask the system to choose a port; nothing would say which.
		{"zero", "tcp", "127.0.0.1:0", true},
		{"an empty port", "tcp", "127.0.0.1:", true},
		{"no port at all", "tcp", "127.0.0.1", true},
		{"a service name", "tcp", "127.0.0.1:http-alt", true},
		{"a plus sign", "tcp", "127.0.0.1:+8080", true},
		{"a minus sign", "tcp", "127.0.0.1:-8080", true},
		{"a space in the port", "tcp", "127.0.0.1: 8080", true},
		{"digits that are not ASCII", "tcp", "127.0.0.1:８０８０", true},
		{"an IPv6 address without brackets", "tcp", "::1:8080", true},
		{"not an address", "tcp", "not an address", true},

		// A Unix-socket path is not an address, whatever it looks like.
		{"a Unix-socket path", "unix", "/run/user/1000/smd.sock", false},
		{"a Unix-socket path that looks like a low port", "unix", "127.0.0.1:80", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir())
			cfg.Server.Protocol = c.protocol
			cfg.SocketPath = c.socket
			var got []Finding
			for _, f := range Validate(cfg) {
				if f.Field == "socket_path" {
					got = append(got, f)
				}
			}
			if !c.refused {
				if len(got) != 0 {
					t.Fatalf("socket_path %q was refused: %+v", c.socket, got)
				}
				return
			}
			if len(got) != 1 || got[0].Warning || got[0].Code != "invalid_field_value" {
				t.Fatalf("socket_path %q: findings on socket_path = %+v; want one blocking invalid_field_value", c.socket, got)
			}
			for _, want := range []string{"1024", "65535"} {
				if !strings.Contains(got[0].Message, want) {
					t.Errorf("the refusal does not give the range (%s): %s", want, got[0].Message)
				}
			}
			// It is the refusal the operator reads: nothing else may come before it and
			// explain a malformed address as something it is not (a network exposure, say).
			if first := firstBlocking(cfg); first == nil || first.Field != "socket_path" {
				t.Fatalf("socket_path %q: the first blocking finding is %+v; want the one on socket_path", c.socket, first)
			}
		})
	}
}

func loadBody(t *testing.T, body string) (Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestMainListenerPort_AFileWithAnUnusablePortIsRefusedAtLoad(t *testing.T) {
	for name, socket := range map[string]string{
		"a service name": "127.0.0.1:http-alt",
		"a low port":     "127.0.0.1:80",
		"zero":           "127.0.0.1:0",
		"an empty port":  "127.0.0.1:",
		"no port":        "127.0.0.1",
		"out of range":   "127.0.0.1:65536",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadBody(t, `{"version":7,"socket_path":"`+socket+`","server":{"protocol":"tcp","app_port":9443}}`)
			if err == nil {
				t.Fatalf("a file whose main listener is %q was loaded", socket)
			}
			for _, want := range []string{"invalid_field_value", "socket_path", "1024"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not name %q: %v", want, err)
				}
			}
		})
	}
}

func TestMainListenerPort_FilesWithAUsablePortLoad(t *testing.T) {
	for name, body := range map[string]string{
		"the lowest port":  `{"version":7,"socket_path":"127.0.0.1:1024","server":{"protocol":"tcp"}}`,
		"the highest port": `{"version":7,"socket_path":"127.0.0.1:65535","server":{"protocol":"tcp"}}`,
		"no socket_path":   `{"version":7,"server":{"protocol":"tcp"}}`,
		"a Unix socket":    `{"version":7,"socket_path":"` + filepath.Join(t.TempDir(), "smd.sock") + `","server":{"protocol":"unix"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadBody(t, body); err != nil {
				t.Fatalf("a file with a usable main listener was refused: %v", err)
			}
		})
	}
}

// The app port's range, at load: a given value below 1024 or above 65535 is refused; the
// ends of the range load.
func TestAppPort_TheRangeAtLoad(t *testing.T) {
	for _, c := range []struct {
		port    string
		refused bool
	}{
		{"1", true}, {"80", true}, {"1023", true}, {"65536", true}, {"-1", true},
		{"1024", false}, {"65535", false},
	} {
		t.Run(c.port, func(t *testing.T) {
			_, err := loadBody(t, `{"version":7,"socket_path":"127.0.0.1:8080","server":{"protocol":"tcp","app_port":`+c.port+`}}`)
			switch {
			case c.refused && err == nil:
				t.Fatalf("a file with server.app_port %s was loaded", c.port)
			case !c.refused && err != nil:
				t.Fatalf("a file with server.app_port %s was refused: %v", c.port, err)
			case c.refused:
				for _, want := range []string{"invalid_field_value", "server.app_port", "1024", "65535"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal does not name %q: %v", want, err)
					}
				}
			}
		})
	}
}

// Left out and an explicit 0 both mean the default: 0 is how "left out" is stored, and it
// never asks the system to choose a port (operator ruling 2026-11-19).
func TestAppPort_LeftOutAndZeroBothMeanTheDefault(t *testing.T) {
	for name, server := range map[string]string{
		"left out":       `{"protocol":"tcp"}`,
		"an explicit 0":  `{"protocol":"tcp","app_port":0}`,
		"on a Unix main": `{"protocol":"unix"}`,
	} {
		t.Run(name, func(t *testing.T) {
			socket := "127.0.0.1:8080"
			if strings.Contains(server, "unix") {
				socket = filepath.Join(t.TempDir(), "smd.sock")
			}
			cfg, err := loadBody(t, `{"version":7,"socket_path":"`+socket+`","server":`+server+`}`)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if got := cfg.Server.EffectiveAppPort(); got != 8443 {
				t.Fatalf("the desktop app listener's port = %d, want the default 8443", got)
			}
		})
	}
}
