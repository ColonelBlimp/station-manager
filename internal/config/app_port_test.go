package config

// W-0027 C1c, ADR 0103 point 2: server.app_port, the desktop app listener's port. Default
// 8443 (operator ruling 2026-11-10), always on 127.0.0.1, configurable. It is a read/serve
// resolver: the default is never written into config.json, because an older smd refuses a
// key it does not know, so writing the default would stop a downgrade from starting.

import (
	"encoding/json"
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
		name   string
		port   int
		socket string
		want   string // "" none, "fatal", "warning"
	}{
		{"absent (default)", 0, "127.0.0.1:8080", ""},
		{"explicit valid", 9443, "127.0.0.1:8080", ""},
		{"negative", -1, "127.0.0.1:8080", "fatal"},
		{"above 65535", 70000, "127.0.0.1:8080", "fatal"},
		// Sharing the browser listener's port never stops the station (the app listener's bind
		// fails and is reported instead): a warning.
		{"the browser listener's port", 8080, "127.0.0.1:8080", "warning"},
		{"the default colliding with the browser listener", 0, "127.0.0.1:8443", "warning"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir())
			cfg.Server.AppPort = c.port
			cfg.SocketPath = c.socket
			got := appPortFindings(cfg)
			switch {
			case c.want == "" && len(got) != 0:
				t.Fatalf("unexpected finding: %+v", got)
			case c.want != "" && len(got) != 1:
				t.Fatalf("findings = %+v, want one %s", got, c.want)
			case c.want == "fatal" && got[0].Warning:
				t.Errorf("an out-of-range app port is only a warning: %+v", got[0])
			case c.want == "warning" && !got[0].Warning:
				t.Errorf("a port shared with the browser listener stops the station: %+v", got[0])
			}
		})
	}
}
