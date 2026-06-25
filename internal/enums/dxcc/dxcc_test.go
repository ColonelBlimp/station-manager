package dxcc

import (
	"os"
	"path/filepath"
	"testing"
)

// The embedded baseline must load and resolve the entities that motivated this
// table — in particular the split entities a country-name match can't handle.
func TestDXCCForPrefix_EmbeddedBaseline(t *testing.T) {
	cases := []struct {
		prefix string
		want   string
	}{
		{"UA", "54"},  // European Russia
		{"UA9", "15"}, // Asiatic Russia (the split the name conflates)
		{"DL", "230"}, // Germany (hamnut "Fed. Rep. of Germany")
		{"HK", "116"}, // Colombia (collision resolved by name-match, not misfile)
		{"K", "291"},  // United States
	}
	for _, c := range cases {
		got, ok := DXCCForPrefix(c.prefix)
		if !ok {
			t.Errorf("DXCCForPrefix(%q): ok=false, want %s", c.prefix, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("DXCCForPrefix(%q) = %s, want %s", c.prefix, got, c.want)
		}
	}
}

func TestDXCCForPrefix_CaseAndWhitespaceInsensitive(t *testing.T) {
	got, ok := DXCCForPrefix("  ua  ")
	if !ok || got != "54" {
		t.Errorf("DXCCForPrefix(\"  ua  \") = %q, %v; want \"54\", true", got, ok)
	}
}

func TestDXCCForPrefix_UnknownAndEmpty(t *testing.T) {
	if _, ok := DXCCForPrefix("ZZZ-NOPE"); ok {
		t.Error("unknown prefix should return ok=false")
	}
	if _, ok := DXCCForPrefix(""); ok {
		t.Error("empty prefix should return ok=false")
	}
}

// LoadOverride merges an operator file on top of the baseline: a new prefix is
// added, and an existing one can be corrected (operator wins).
func TestLoadOverride_AddsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	override := `{"entities":[
		{"prefix":"XX","dxcc":999,"name":"Testland"},
		{"prefix":"DL","dxcc":1000,"name":"Override Germany"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "dxcc-entities.json"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadOverride(dir); err != nil {
		t.Fatalf("LoadOverride: %v", err)
	}
	if got, ok := DXCCForPrefix("XX"); !ok || got != "999" {
		t.Errorf("override add: DXCCForPrefix(\"XX\") = %q, %v; want \"999\"", got, ok)
	}
	if got, ok := DXCCForPrefix("DL"); !ok || got != "1000" {
		t.Errorf("override win: DXCCForPrefix(\"DL\") = %q, %v; want \"1000\"", got, ok)
	}
	// Re-apply the embedded baseline so this test doesn't leak the DL override
	// into others (package-level state; tests in a package share it).
	mu.Lock()
	prefixToDXCC = make(map[string]string)
	mu.Unlock()
	cat, _ := parseCatalogue(embeddedCatalogueJSON)
	mu.Lock()
	applyCatalogue(cat)
	mu.Unlock()
}

// A missing override file is not an error.
func TestLoadOverride_MissingFileOK(t *testing.T) {
	if err := LoadOverride(t.TempDir()); err != nil {
		t.Errorf("missing override should be nil, got %v", err)
	}
}
