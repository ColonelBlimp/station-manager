package dxcc

import (
	"os"
	"path/filepath"
	"strings"
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

// The embedded catalogue covers every entity, not only those one operator had
// worked. It used to be built from a log, so it knew 166 entities, and a contact
// with any other got the right country and no DXCC number: Christmas Island
// (VK9X) among them, found 2026-11-24 (W-0031). It is now generated from the
// Amateur Radio Country Files' cty.csv (scripts/gen-dxcc-entities.py).
func TestEmbeddedCatalogue_KnowsEntitiesTheOldOneLacked(t *testing.T) {
	cat, err := parseCatalogue(embeddedCatalogueJSON)
	if err != nil {
		t.Fatal(err)
	}
	byPrefix := map[string]int{}
	for _, e := range cat.Entities {
		byPrefix[e.Prefix] = e.DXCC
	}
	for prefix, want := range map[string]int{
		"VK9X": 35,  // Christmas Island
		"VK9N": 189, // Norfolk Island
		"TN":   412, // Republic of the Congo (the inbox item of 2026-10-31)
		"GJ":   122, // Jersey
		"GU":   106, // Guernsey
		"JW":   259, // Svalbard
		"3D2":  176, // Fiji
		"AP":   372, // Pakistan
	} {
		if got := byPrefix[prefix]; got != want {
			t.Errorf("%s = %d, want %d", prefix, got, want)
		}
	}
}

// hamnut does not write a prefix as cty.csv does, in two ways, and the catalogue
// is keyed as hamnut writes it (both seen through the daemon's own lookup):
//   - an entity told apart by a suffix is `3Y/b` in cty.csv and `3Y…B` from
//     hamnut, with a horizontal ellipsis and in capitals;
//   - an entity that is not a DXCC entity of its own is starred in cty.csv
//     (`*IT9`) and plain from hamnut, and carries its parent's number.
func TestEmbeddedCatalogue_IsKeyedAsHamnutWritesThePrefix(t *testing.T) {
	for prefix, want := range map[string]string{
		"3Y…B": "24",  // Bouvet
		"FO…C": "36",  // Clipperton Island
		"E5…S": "234", // South Cook Islands
		"FT…G": "99",  // Glorioso Islands
		"GM…S": "279", // Shetland: counts as Scotland
		"IT9":  "248", // Sicily: counts as Italy
		"4U1V": "206", // Vienna International Centre: counts as Austria
	} {
		got, ok := DXCCForPrefix(prefix)
		if !ok || got != want {
			t.Errorf("DXCCForPrefix(%q) = %q, %v; want %q", prefix, got, ok, want)
		}
	}
	cat, err := parseCatalogue(embeddedCatalogueJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range cat.Entities {
		if strings.ContainsAny(e.Prefix, "/*") || e.Prefix != strings.ToUpper(e.Prefix) || strings.TrimSpace(e.Prefix) != e.Prefix {
			t.Errorf("%q is not as hamnut writes a prefix", e.Prefix)
		}
	}
}

// The whole of it, pinned: a regeneration that drops entities, repeats a prefix
// or carries an unusable number is caught here and not at a contact.
func TestEmbeddedCatalogue_IsCompleteAndWellFormed(t *testing.T) {
	cat, err := parseCatalogue(embeddedCatalogueJSON)
	if err != nil {
		t.Fatal(err)
	}
	prefixes := map[string]bool{}
	numbers := map[int]bool{}
	for _, e := range cat.Entities {
		if e.Prefix == "" || e.DXCC <= 0 || e.Name == "" {
			t.Errorf("unusable entry: %+v", e)
		}
		if prefixes[e.Prefix] {
			t.Errorf("prefix %q appears twice", e.Prefix)
		}
		prefixes[e.Prefix] = true
		numbers[e.DXCC] = true
	}
	// cty.csv of 2026-11-14: 346 entities, of which six are not DXCC entities of
	// their own and share their parent's number.
	if len(prefixes) < 346 || len(numbers) < 340 {
		t.Errorf("%d prefixes and %d distinct DXCC numbers; want at least 346 and 340", len(prefixes), len(numbers))
	}
}
