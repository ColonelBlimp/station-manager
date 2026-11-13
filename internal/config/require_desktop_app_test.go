package config

// W-0027 C2a: server.require_desktop_app, the switch for the operating-client check. Off by
// default and temporary (operator ruling 2026-11-13). False is never written, so a file
// that never set it still starts on a build that does not know the key.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireDesktopApp_IsOffByDefaultAndNotWritten(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	if cfg.Server.RequireDesktopApp {
		t.Fatal("the operating-client check is on by default")
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "require_desktop_app") {
		t.Fatalf("the default (off) was written into the file: %s", b)
	}
}

func TestRequireDesktopApp_OnIsWrittenAndLoadedBack(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig(dir)
	cfg.Server.RequireDesktopApp = true
	path := filepath.Join(dir, "config.json")
	if _, err := WriteJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("a file with server.require_desktop_app is refused: %v", err)
	}
	if !loaded.Server.RequireDesktopApp {
		t.Fatal("server.require_desktop_app=true was not loaded back")
	}
	for _, f := range Validate(loaded) {
		if strings.Contains(f.Field, "require_desktop_app") {
			t.Fatalf("unexpected finding: %+v", f)
		}
	}
}
