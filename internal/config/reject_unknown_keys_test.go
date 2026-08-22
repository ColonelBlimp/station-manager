package config

import (
	"bytes"
	"encoding/json"
	stderr "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// W-0006 / ADR 0074 — reject unknown configuration keys before any write.
//
// Load deliberately ran a LENIENT unmarshal (unknown keys silently dropped) for
// forward-compatibility, but the newer-than-supported VERSION guard already
// covers that case, so an unknown key at a supported version is a typo, not a
// forward-compat field. These rules pin the reject gate: a supported-version
// file with an unrecognised key refuses Load, naming every offending path and
// never a value, so a startup can never succeed while silently deleting the typo.

// defaultConfigMap returns the marshalled default config as a mutable top-level
// map — the confusable clean baseline the existing TestUnknownKeys proves flags
// zero unknown keys — so a dirty case differs from a loading one by exactly the
// injected key.
func defaultConfigMap(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal default config: %v", err)
	}
	return m
}

// writeConfigMap marshals m to a fresh temp config.json and returns its path.
func writeConfigMap(t *testing.T, m map[string]json.RawMessage) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal config map: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	return path
}

// injectSub replaces m[key] with its object plus an extra subkey → val.
func injectSub(t *testing.T, m map[string]json.RawMessage, key, subkey string, val json.RawMessage) {
	t.Helper()
	sub := map[string]json.RawMessage{}
	if raw, ok := m[key]; ok {
		if err := json.Unmarshal(raw, &sub); err != nil {
			t.Fatalf("unmarshal %s object: %v", key, err)
		}
	}
	sub[subkey] = val
	b, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("marshal %s object: %v", key, err)
	}
	m[key] = b
}

// AC1 + AC3 — an unknown TOP-LEVEL key refuses Load, and the refusal names the
// path but never the value. The confusable wrong outcome — the SAME config minus
// the key loading fine, i.e. a start that silently drops the typo — is pinned by
// the clean half loading without error.
func TestLoad_RejectsUnknownTopLevelKeyWithoutLeakingValue(t *testing.T) {
	const secret = "S3CR3T-DO-NOT-LEAK"

	clean := defaultConfigMap(t)
	if _, err := Load(writeConfigMap(t, clean)); err != nil {
		t.Fatalf("the clean default config must load (confusable baseline): %v", err)
	}

	dirty := defaultConfigMap(t)
	dirty["brige"] = json.RawMessage(`"` + secret + `"`) // typo for "bridge", value is a secret
	_, err := Load(writeConfigMap(t, dirty))
	if err == nil {
		t.Fatal("Load accepted a config with an unknown top-level key — startup would drop the typo silently")
	}
	var uke *UnknownKeysError
	if !stderr.As(err, &uke) {
		t.Fatalf("error is %T (%v), want *UnknownKeysError", err, err)
	}
	if !containsPath(uke.Keys, "brige") {
		t.Fatalf("offending path not reported; keys = %v", uke.Keys)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the key's VALUE leaked into the refusal message: %q", err.Error())
	}
}

// AC2 — an unknown NESTED key (inside a struct block) refuses Load with a dotted
// path.
func TestLoad_RejectsUnknownNestedKey(t *testing.T) {
	m := defaultConfigMap(t)
	injectSub(t, m, "smtp", "timeot_sec", json.RawMessage(`5`)) // typo for timeout_sec
	_, err := Load(writeConfigMap(t, m))
	var uke *UnknownKeysError
	if !stderr.As(err, &uke) {
		t.Fatalf("error is %T (%v), want *UnknownKeysError", err, err)
	}
	if !containsPath(uke.Keys, "smtp.timeot_sec") {
		t.Fatalf("nested path not reported; keys = %v", uke.Keys)
	}
}

// AC2 + CC-2 — an unknown key inside a STRUCT SLICE element refuses Load with an
// indexed path. The pre-fix walker treated slices as leaves, so these were blind
// spots.
func TestLoad_RejectsUnknownKeyInStructSlice(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  json.RawMessage
		want string
	}{
		{"rigs", "rigs", json.RawMessage(`[{"typo": 1}]`), "rigs[0].typo"},
		{"forwarders", "forwarders", json.RawMessage(`[{"typo": 1}]`), "forwarders[0].typo"},
		{"operators", "operators", json.RawMessage(`[{"typo": 1}]`), "operators[0].typo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := defaultConfigMap(t)
			m[tc.key] = tc.val
			_, err := Load(writeConfigMap(t, m))
			var uke *UnknownKeysError
			if !stderr.As(err, &uke) {
				t.Fatalf("error is %T (%v), want *UnknownKeysError", err, err)
			}
			if !containsPath(uke.Keys, tc.want) {
				t.Fatalf("indexed path %q not reported; keys = %v", tc.want, uke.Keys)
			}
		})
	}
}

// AC3 — the refusal reports EVERY offending path, not just the first.
func TestLoad_ReportsEveryOffendingPath(t *testing.T) {
	m := defaultConfigMap(t)
	m["bogus_top"] = json.RawMessage(`1`)
	injectSub(t, m, "smtp", "timeot_sec", json.RawMessage(`5`))
	_, err := Load(writeConfigMap(t, m))
	var uke *UnknownKeysError
	if !stderr.As(err, &uke) {
		t.Fatalf("error is %T (%v), want *UnknownKeysError", err, err)
	}
	for _, want := range []string{"bogus_top", "smtp.timeot_sec"} {
		if !containsPath(uke.Keys, want) {
			t.Fatalf("path %q missing from refusal; keys = %v", want, uke.Keys)
		}
	}
}

// AC4 — recognised raw migrations run BEFORE the check, so a key a migration
// consumes (bridge.mode_mappings, removed from the schema in the v1→v2 migration)
// is not falsely reported as unknown.
func TestLoad_MigrationConsumedKeyIsNotFlagged(t *testing.T) {
	v1 := []byte(`{"version":1,"bridge":{"mode_mappings":{"drv":{"USB":{"mode":"USB"}}}}}`)
	if got := UnknownKeys(v1); containsPath(got, "bridge.mode_mappings") {
		t.Fatalf("a migration-consumed key was flagged as unknown: %v", got)
	}
}

// AC5 — arbitrary keys inside a map (forwarder endpoints) and a json.RawMessage
// (forwarder credentials) are operator DATA, not schema, and are never reported.
func TestLoad_AcceptsArbitraryMapAndRawMessageKeys(t *testing.T) {
	doc := []byte(`{
		"version": 2,
		"forwarders": [{
			"name": "x", "type": "qrz", "enabled": false,
			"credentials": {"any_key_here": "v", "another": 2},
			"endpoints": {"weird-endpoint-name": "https://example.test"}
		}]
	}`)
	if got := UnknownKeys(doc); len(got) != 0 {
		t.Fatalf("arbitrary map / credential keys were reported as unknown: %v", got)
	}
}

// AC6 — malformed JSON, a newer-than-supported version, and unknown keys produce
// THREE distinct diagnostics; none is reported as another.
func TestLoad_DistinctDiagnostics(t *testing.T) {
	malformedPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(malformedPath, []byte(`{"version": 2,`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, malErr := Load(malformedPath)
	var uke *UnknownKeysError
	if malErr == nil || stderr.As(malErr, &uke) {
		t.Fatalf("malformed JSON must not surface as an unknown-key error: %v", malErr)
	}

	newerPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(newerPath, []byte(`{"version": 9999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, verErr := Load(newerPath)
	if verErr == nil || stderr.As(verErr, &uke) {
		t.Fatalf("a newer-version file must not surface as an unknown-key error: %v", verErr)
	}
	if !strings.Contains(verErr.Error(), "newer") {
		t.Fatalf("newer-version diagnostic lost its wording: %v", verErr)
	}

	m := defaultConfigMap(t)
	m["bogus_top"] = json.RawMessage(`1`)
	_, unkErr := Load(writeConfigMap(t, m))
	if !stderr.As(unkErr, &uke) {
		t.Fatalf("an unknown key must surface as *UnknownKeysError, got %T: %v", unkErr, unkErr)
	}
}

// CC-2 (walker unit) — UnknownKeys now recurses struct-slice elements for every
// slice the dossier enumerates, reporting indexed paths.
func TestUnknownKeys_RecursesStructSlices(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"rigs", `{"rigs":[{"typo":1}]}`, "rigs[0].typo"},
		{"forwarders", `{"forwarders":[{"typo":1}]}`, "forwarders[0].typo"},
		{"operators", `{"operators":[{"typo":1}]}`, "operators[0].typo"},
		{"lookup.chain", `{"lookup":{"chain":[{"typo":1}]}}`, "lookup.chain[0].typo"},
		{"evidence.antennas", `{"evidence":{"antennas":[{"typo":1}]}}`, "evidence.antennas[0].typo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := UnknownKeys([]byte(tc.doc))
			if !containsPath(got, tc.want) {
				t.Fatalf("expected %q among unknown keys; got %v", tc.want, got)
			}
		})
	}
}

// AC1 — the refusal is READ-ONLY: an unknown-key file is not rewritten, and its
// content, mtime, and mode are all untouched. This pins the confusable wrong
// outcome — a startup that "fixes" the file by silently dropping the key.
func TestLoad_UnknownKeyLeavesFileUntouched(t *testing.T) {
	m := defaultConfigMap(t)
	m["bogus_top"] = json.RawMessage(`"x"`)
	path := writeConfigMap(t, m)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fi0, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown-key config")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("Load rewrote the config file on the reject path")
	}
	if !fi0.ModTime().Equal(fi1.ModTime()) {
		t.Errorf("mtime moved: %v → %v (the reject path must not write)", fi0.ModTime(), fi1.ModTime())
	}
	if fi0.Mode().Perm() != fi1.Mode().Perm() {
		t.Errorf("mode changed: %v → %v", fi0.Mode().Perm(), fi1.Mode().Perm())
	}
}

// AC8 — the read-only preflight reports unknown key paths (values omitted) and
// keeps malformed / newer-version as distinct errors, all WITHOUT writing.
func TestPreflightUnknownKeys(t *testing.T) {
	t.Run("clean config reports nothing", func(t *testing.T) {
		path := writeConfigMap(t, defaultConfigMap(t))
		got, err := PreflightUnknownKeys(path)
		if err != nil || len(got) != 0 {
			t.Fatalf("clean preflight: got %v, err %v", got, err)
		}
	})

	t.Run("unknown keys reported without value; file untouched", func(t *testing.T) {
		const secret = "PREFLIGHT-SECRET"
		m := defaultConfigMap(t)
		m["bogus_top"] = json.RawMessage(`"` + secret + `"`)
		m["rigs"] = json.RawMessage(`[{"typo": 1}]`)
		path := writeConfigMap(t, m)

		past := time.Now().Add(-time.Hour).Truncate(time.Second)
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatal(err)
		}

		got, err := PreflightUnknownKeys(path)
		if err != nil {
			t.Fatalf("preflight errored on a merely-unknown-key config: %v", err)
		}
		for _, want := range []string{"bogus_top", "rigs[0].typo"} {
			if !containsPath(got, want) {
				t.Errorf("preflight omitted %q; got %v", want, got)
			}
		}
		for _, k := range got {
			if strings.Contains(k, secret) {
				t.Errorf("preflight path leaked a value: %q", k)
			}
		}
		if fi, err := os.Stat(path); err != nil {
			t.Fatal(err)
		} else if !fi.ModTime().Equal(past) {
			t.Error("preflight wrote to the file — it must be read-only")
		}
	})

	t.Run("malformed and newer-version stay distinct errors", func(t *testing.T) {
		mal := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(mal, []byte(`{"version":3,`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PreflightUnknownKeys(mal); err == nil {
			t.Error("malformed config passed preflight")
		}

		newer := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(newer, []byte(`{"version":9999}`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := PreflightUnknownKeys(newer)
		if err == nil || !strings.Contains(err.Error(), "newer") {
			t.Errorf("newer-version preflight error = %v, want the downgrade-guard wording", err)
		}
	})
}

// [P1] (codex 3c45fc48) — a map whose VALUE type is a struct carries schema in
// each value (rigs[].mode_mappings: map[string]ModeMapping). A typo inside a value
// must be rejected, while the arbitrary map KEY stays operator data and a
// scalar-valued map (endpoints) never false-flags.
func TestUnknownKeys_RecursesStructValuedMaps(t *testing.T) {
	t.Run("value typo detected with the map key in the path", func(t *testing.T) {
		doc := []byte(`{"rigs":[{"mode_mappings":{"DATA-U":{"mode":"USB","submdoe":"FT8"}}}]}`)
		got := UnknownKeys(doc)
		if !containsPath(got, "rigs[0].mode_mappings[DATA-U].submdoe") {
			t.Fatalf("mode-mapping value typo not detected; got %v", got)
		}
	})
	t.Run("valid mapping and a scalar-valued map stay clean", func(t *testing.T) {
		doc := []byte(`{"rigs":[{"mode_mappings":{"DATA-U":{"mode":"USB","submode":"FT8"}}}],` +
			`"forwarders":[{"endpoints":{"whatever-key":"http://x"}}]}`)
		if got := UnknownKeys(doc); len(got) != 0 {
			t.Fatalf("valid struct-valued map or scalar-valued map false-flagged: %v", got)
		}
	})
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}
