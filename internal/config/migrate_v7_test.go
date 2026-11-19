package config

import (
	"encoding/json"
	stderrors "errors"
	"os"
	"strings"
	"testing"
)

// v7 (W-0027 C2e part 3): server.require_desktop_app, the temporary switch for the
// operating-client check, is gone: the check is always on. The 6→7 step removes the key
// from a file that holds it, whatever it said.

func loadV7(t *testing.T, doc string) (Config, map[string]any) {
	t.Helper()
	path := writeTempConfig(t, doc)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := migrateDocument(raw)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(migrated, &m); err != nil {
		t.Fatal(err)
	}
	return cfg, m
}

func TestMigrateV6toV7_RemovesTheSwitchWhateverItSaid(t *testing.T) {
	for name, server := range map[string]string{
		"on, as at the station": `{"require_desktop_app":true,"app_port":8443}`,
		"off, written by hand":  `{"require_desktop_app":false,"app_port":8443}`,
		"never set":             `{"app_port":8443}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, m := loadV7(t, `{"version":6,"data_dir":"/tmp/x","server":`+server+`}`)
			if cfg.Version != 7 || m["version"] != float64(7) {
				t.Fatalf("version: loaded %d, document %v; want 7", cfg.Version, m["version"])
			}
			srv, ok := m["server"].(map[string]any)
			if !ok {
				t.Fatalf("the server block did not survive: %v", m["server"])
			}
			if _, has := srv["require_desktop_app"]; has {
				t.Fatalf("the switch survived the migration: %v", srv)
			}
			if srv["app_port"] != float64(8443) || cfg.Server.AppPort != 8443 {
				t.Fatalf("the rest of the server block was not kept: document %v, loaded %d", srv, cfg.Server.AppPort)
			}
			b, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "require_desktop_app") {
				t.Fatalf("the loaded config still writes the switch: %s", b)
			}
		})
	}
}

// A file with no server block, or a null one, loads as it did at version 6.
func TestMigrateV6toV7_NoServerBlockIsLeftAlone(t *testing.T) {
	for name, doc := range map[string]string{
		"absent": `{"version":6,"data_dir":"/tmp/x"}`,
		"null":   `{"version":6,"data_dir":"/tmp/x","server":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, m := loadV7(t, doc)
			if cfg.Version != 7 || m["version"] != float64(7) {
				t.Fatalf("version: loaded %d, document %v; want 7", cfg.Version, m["version"])
			}
			if name == "absent" {
				if _, has := m["server"]; has {
					t.Fatalf("the migration invented a server block: %v", m["server"])
				}
			} else if v, has := m["server"]; !has || v != nil {
				t.Fatalf("a null server block was rewritten: %v (present %v)", v, has)
			}
		})
	}
}

// A server value that is not an object is not the migration's to judge: it is left as it
// is, and the typed load refuses it, as it did at version 6.
func TestMigrateV6toV7_AMalformedServerBlockIsStillRefusedByTheLoad(t *testing.T) {
	for name, value := range map[string]string{"a number": `5`, "an array": `[1]`} {
		t.Run(name, func(t *testing.T) {
			doc := `{"version":6,"data_dir":"/tmp/x","server":` + value + `}`
			migrated, err := migrateDocument([]byte(doc))
			if err != nil {
				t.Fatalf("the migration judged the value itself: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(migrated, &m); err != nil {
				t.Fatal(err)
			}
			want, _ := json.Marshal(m["server"])
			if string(want) != value {
				t.Fatalf("the migration rewrote the value: %s, want %s", want, value)
			}
			_, err = Load(writeTempConfig(t, doc))
			if err == nil || !strings.Contains(err.Error(), `"server" expects an object`) {
				t.Fatalf("load: %v; want the wrong-type refusal naming server", err)
			}
		})
	}
}

// At version 7 the key is unknown, so a file that names it is refused and not ignored.
func TestConfigV7_TheSwitchIsAnUnknownKey(t *testing.T) {
	_, err := Load(writeTempConfig(t, `{"version":7,"data_dir":"/tmp/x","server":{"require_desktop_app":true}}`))
	var unknown *UnknownKeysError
	if !stderrors.As(err, &unknown) {
		t.Fatalf("load: %v; want an unknown-keys refusal", err)
	}
	if len(unknown.Keys) != 1 || unknown.Keys[0] != "server.require_desktop_app" {
		t.Fatalf("unknown keys = %v; want server.require_desktop_app alone", unknown.Keys)
	}
}

// There is no way back to version 6, or to any older version: a build does not rewrite a
// file for an older one, and a file newer than the build is refused (loaderr_test.go).
// The registry holds forward steps only, one for every version below the head.
func TestMigrations_EveryVersionBelowTheHeadHasItsForwardStep(t *testing.T) {
	for v := 1; v < currentConfigVersion; v++ {
		if m := migrationFrom(v); m == nil || m.apply == nil {
			t.Fatalf("no forward migration registered from version %d", v)
		}
	}
	if m := migrationFrom(currentConfigVersion); m != nil {
		t.Fatalf("a migration is registered from the head version %d", currentConfigVersion)
	}
}

// An older file still reaches the head, through every step.
func TestMigrateV6toV7_AVersion5FileReachesTheHead(t *testing.T) {
	cfg, m := loadV7(t, v5Doc)
	if cfg.Version != 7 || m["version"] != float64(7) {
		t.Fatalf("version: loaded %d, document %v; want 7", cfg.Version, m["version"])
	}
	if CurrentSchemaVersion() != 7 {
		t.Fatalf("CurrentSchemaVersion() = %d, want 7", CurrentSchemaVersion())
	}
}
