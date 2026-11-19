package config

// W-0027 C1c, ADR 0103 point 2: server.app_port, the desktop app listener's port. Default
// 8443 (operator ruling 2026-11-10), always on 127.0.0.1, configurable. It is a read/serve
// resolver: the default is never written into config.json, because an older smd refuses a
// key it does not know, so writing the default would stop a downgrade from starting.

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAppPort_DefaultsTo8443(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	if got := cfg.Server.EffectiveAppPort(); got != 8443 {
		t.Fatalf("default app port = %d, want 8443", got)
	}
}

func TestAppPort_ExplicitValueWins(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Server.AppPort = 9443
	if got := cfg.Server.EffectiveAppPort(); got != 9443 {
		t.Fatalf("app port = %d, want 9443", got)
	}
}

func TestAppPort_DefaultIsNotWrittenToTheFile(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "app_port") {
		t.Fatalf("the default app port was written into the file: %s", b)
	}
	cfg.Server.AppPort = 9443
	b, _ = json.Marshal(cfg)
	if !strings.Contains(string(b), `"app_port":9443`) {
		t.Fatal("an explicit app port is not written")
	}
}

func appPortFindings(cfg Config) []Finding {
	var out []Finding
	for _, f := range Validate(cfg) {
		if f.Field == "server.app_port" {
			out = append(out, f)
		}
	}
	return out
}

func TestAppPort_Validation(t *testing.T) {
	for _, c := range []struct {
		name     string
		port     int
		protocol string
		socket   string
		want     string // "" none, "fatal"
	}{
		{"absent (default)", 0, "tcp", "127.0.0.1:8080", ""},
		{"explicit valid", 9443, "tcp", "127.0.0.1:8080", ""},
		{"negative", -1, "tcp", "127.0.0.1:8080", "fatal"},
		{"above 65535", 70000, "tcp", "127.0.0.1:8080", "fatal"},
		// A given value is from 1024 to 65535 (operator ruling 2026-11-19): below 1024 an
		// ordinary user cannot bind on a stock system. The listener is TCP whatever the main
		// listener is, so the range holds under a Unix-socket main listener too.
		{"one below the range", 1023, "tcp", "127.0.0.1:8080", "fatal"},
		{"the lowest port", 1024, "tcp", "127.0.0.1:8080", ""},
		{"the highest port", 65535, "tcp", "127.0.0.1:8080", ""},
		{"one above the range", 65536, "tcp", "127.0.0.1:8080", "fatal"},
		{"a low port under a Unix-socket main listener", 80, "unix", "/run/user/1000/smd.sock", "fatal"},
		// A main listener that already holds the desktop app's endpoint (127.0.0.1 and the
		// port) is refused. smd serves only the desktop app, so a daemon whose app listener
		// cannot bind does not stay up (W-0027 C2e): the conflict is named at load, and by
		// `smd config-check`, not left to the bind. It was a warning while the app listener's
		// failure was only reported.
		{"the main listener's address and port", 8080, "tcp", "127.0.0.1:8080", "fatal"},
		{"the default colliding with the main listener", 0, "tcp", "127.0.0.1:8443", "fatal"},
		{"127.0.0.1 written as an IPv4-mapped address", 0, "tcp", "[::ffff:127.0.0.1]:8443", "fatal"},
		{"every address, no host", 0, "tcp", ":8443", "fatal"},
		{"every address, 0.0.0.0", 0, "tcp", "0.0.0.0:8443", "fatal"},
		{"every address, [::]", 0, "tcp", "[::]:8443", "fatal"},
		// The same port number on an address that is not the app's is no conflict: both bind.
		{"another IPv4 loopback address", 0, "tcp", "127.0.0.2:8443", ""},
		{"the IPv6 loopback address", 0, "tcp", "[::1]:8443", ""},
		{"an explicit port on another address", 9443, "tcp", "127.0.0.2:9443", ""},
		// A host name's address is known only when it is resolved: left to the bind, which
		// stops the daemon with its own message if the two do collide.
		{"a host name", 0, "tcp", "localhost:8443", ""},
		// A port written as a service name is no conflict finding either: socket_path's own
		// rule refuses it before any port is compared (listener_ports_test.go).
		{"a port written as a service name", 8080, "tcp", "127.0.0.1:http-alt", ""},
		// A Unix-socket main listener holds no TCP port at all.
		{"a Unix-socket main listener", 0, "unix", "/run/user/1000/smd-8443.sock", ""},
		{"a Unix-socket path that looks like an address", 0, "unix", "127.0.0.1:8443", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir())
			cfg.Server.AppPort = c.port
			cfg.Server.Protocol = c.protocol
			cfg.SocketPath = c.socket
			got := appPortFindings(cfg)
			switch {
			case c.want == "" && len(got) != 0:
				t.Fatalf("unexpected finding: %+v", got)
			case c.want != "" && len(got) != 1:
				t.Fatalf("findings = %+v, want one %s", got, c.want)
			case c.want == "fatal" && got[0].Warning:
				t.Errorf("an app port the daemon cannot start on is only a warning: %+v", got[0])
			}
		})
	}
}

// The conflict stops the load itself, with a message that names the port and both settings,
// so the operator reads the cause in the refusal and not in a failed bind after the start.
func TestAppPort_AFileThatSharesThePortIsRefusedAtLoad(t *testing.T) {
	for name, body := range map[string]string{
		"the default app port": `{"version":7,"socket_path":"127.0.0.1:8443","server":{"protocol":"tcp"}}`,
		"an explicit app port": `{"version":7,"socket_path":"127.0.0.1:8080","server":{"protocol":"tcp","app_port":8080}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatal("a file whose desktop app listener shares the main listener's port was loaded")
			}
			for _, want := range []string{"app_port_conflict", "server.app_port", "socket_path"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not name %q: %v", want, err)
				}
			}
		})
	}
}

// Files with no conflict load: separate ports, and the same port number on an address
// that is not the desktop app's (review, 2026-11-19: the refusal first compared only the
// port numbers and turned these away).
func TestAppPort_FilesWithNoConflictLoad(t *testing.T) {
	for name, body := range map[string]string{
		"separate ports":                 `{"version":7,"socket_path":"127.0.0.1:8080","server":{"protocol":"tcp","app_port":8443}}`,
		"the same port on 127.0.0.2":     `{"version":7,"socket_path":"127.0.0.2:8443","server":{"protocol":"tcp","app_port":8443}}`,
		"the same port on the IPv6 loop": `{"version":7,"socket_path":"[::1]:8443","server":{"protocol":"tcp"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err != nil {
				t.Fatalf("a file with no listener conflict was refused: %v", err)
			}
		})
	}
}

// The rule against the operating system, on free ports: a main listener is bound on each
// kind of address, then 127.0.0.1 with the same port, as api.BindApp binds the desktop
// app's listener. Where the second bind is refused the validation must refuse too, and
// where it binds the validation must not. Nothing is served and every listener is closed
// at once. An address this machine cannot bind (no IPv6, say) is skipped, not passed.
func TestAppPort_TheRuleAgreesWithARealBind(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::ffff:127.0.0.1", "", "0.0.0.0", "::", "127.0.0.2", "::1"} {
		t.Run("main listener on "+strconv.Quote(host), func(t *testing.T) {
			probe, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := probe.Addr().(*net.TCPAddr).Port
			_ = probe.Close()
			socket := net.JoinHostPort(host, strconv.Itoa(port))
			mainListener, err := net.Listen("tcp", socket)
			if err != nil {
				t.Skipf("this machine cannot bind %s: %v", socket, err)
			}
			defer func() { _ = mainListener.Close() }()
			app, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			bound := err == nil
			if bound {
				_ = app.Close()
			}

			cfg := DefaultConfig(t.TempDir())
			cfg.Server.Protocol = "tcp"
			cfg.Server.AppPort = port
			cfg.SocketPath = socket
			refused := len(appPortFindings(cfg)) == 1
			if refused == bound {
				t.Fatalf("with the main listener on %s the app's bind on 127.0.0.1 succeeded=%v (%v), "+
					"but the validation refused=%v", socket, bound, err, refused)
			}
		})
	}
}
