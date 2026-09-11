package modes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadOverride_BoundsModeLength pins the storage-domain guard: an operator
// modes.json mode (or submode parent) longer than MaxModeLen is dropped at load, so
// IsValidMode/GetModeBySubmode never yield a value that would fail the qso.mode
// CHECK on insert (codex c03f36e5 P1). A name AT the ceiling is still accepted.
func TestLoadOverride_BoundsModeLength(t *testing.T) {
	over := strings.Repeat("X", MaxModeLen+1)  // 21 chars
	atLimit := strings.Repeat("Y", MaxModeLen) // 20 chars
	dir := t.TempDir()
	payload := `{"main_modes": ["` + over + `", "` + atLimit + `"],` +
		`"submodes": {"SUB_OVER": "` + over + `", "SUB_OK": "` + atLimit + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "modes.json"), []byte(payload), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := LoadOverride(dir); err != nil {
		t.Fatalf("LoadOverride: %v", err)
	}

	if IsValidMode(over) {
		t.Errorf("over-length mode (%d chars) must not be admitted", len(over))
	}
	if _, ok := GetModeBySubmode("SUB_OVER"); ok {
		t.Errorf("submode mapping to an over-length parent must be dropped")
	}
	// The boundary value is fine — it fits the CHECK exactly.
	if !IsValidMode(atLimit) {
		t.Errorf("mode at exactly MaxModeLen (%d) should be admitted", MaxModeLen)
	}
	if parent, ok := GetModeBySubmode("SUB_OK"); !ok || parent.String() != atLimit {
		t.Errorf("submode with an at-limit parent should resolve: got (%q, %v)", parent, ok)
	}

	// An over-length parent for an EXISTING submode key must REMOVE the mapping,
	// not silently leave the prior (inherited) parent active (codex 1680267a P2).
	// A fresh key (never in the baseline) keeps this hermetic — no restore needed.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "modes.json"),
		[]byte(`{"submodes": {"REMAP_TEST": "SSB"}}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := LoadOverride(dir2); err != nil {
		t.Fatalf("LoadOverride: %v", err)
	}
	if parent, ok := GetModeBySubmode("REMAP_TEST"); !ok || parent != SSB {
		t.Fatalf("precondition: REMAP_TEST should resolve to SSB, got (%q, %v)", parent, ok)
	}
	dir3 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir3, "modes.json"),
		[]byte(`{"submodes": {"REMAP_TEST": "`+over+`"}}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := LoadOverride(dir3); err != nil {
		t.Fatalf("LoadOverride: %v", err)
	}
	if parent, ok := GetModeBySubmode("REMAP_TEST"); ok {
		t.Errorf("over-length reassignment must remove the mapping, not retain it: got %q", parent)
	}

	// Distinct raw keys that canonicalize identically ("COLLIDE_TEST" and
	// " collide_test "), one valid and one over-length, must resolve
	// DETERMINISTICALLY to the valid parent regardless of map iteration order — and
	// stay that way across repeated loads (codex 26e55f6d P2). A fresh key keeps it
	// hermetic.
	dir4 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir4, "modes.json"),
		[]byte(`{"submodes": {"COLLIDE_TEST": "SSB", " collide_test ": "`+over+`"}}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	for i := 0; i < 10; i++ {
		if err := LoadOverride(dir4); err != nil {
			t.Fatalf("LoadOverride: %v", err)
		}
		if parent, ok := GetModeBySubmode("COLLIDE_TEST"); !ok || parent != SSB {
			t.Fatalf("collision load %d: valid parent must win, got (%q, %v)", i, parent, ok)
		}
	}
}

func TestIsValidMode(t *testing.T) {
	valid := []string{
		"AM", "cw", "  fm  ", "RTTY", "ssb", "digitalvoice", "MFSK", "PSK", "hell", "packet",
		// FT8 and the JT family are main modes in ADIF 3.x (FT8 used to be an
		// MFSK submode under v1's catalogue).
		"FT8", "JT65", "MSK144",
	}
	for _, in := range valid {
		if !IsValidMode(in) {
			t.Fatalf("expected %q to be valid", in)
		}
	}

	// FT4, FST4, FST4W, JS8 and Q65 are NOT: ADIF 3.1.5's Submode Enumeration
	// lists them under MFSK, so a bare MODE=FT4 is as invalid as MODE=USB
	// (W-0019 baseline fix).
	invalid := []string{"", "foo", "lsb", "usb", "FT4", "FST4", "FST4W", "JS8", "Q65"}
	for _, in := range invalid {
		if IsValidMode(in) {
			t.Fatalf("expected %q to be invalid", in)
		}
	}
}

func TestIsValidSubMode(t *testing.T) {
	valid := []string{"PSK31", "psk63", " dmr ", "USB", "lsb", "aprs", "C4FM", "ft4", "FST4", "fst4w", "JS8", "Q65"}
	for _, in := range valid {
		if !IsValidSubMode(in) {
			t.Fatalf("expected %q to be valid", in)
		}
	}

	// FT8 used to live here under v1 (an MFSK submode) — it is a main mode in
	// the ADIF 3.x catalogue, so it no longer counts as a submode; FT4, FST4,
	// FST4W, JS8 and Q65 stay submodes of MFSK per ADIF 3.1.5. Test pinned so
	// we catch regressions either way.
	invalid := []string{"", "AM", "CW", "foo", "FT8", "JT65"}
	for _, in := range invalid {
		if IsValidSubMode(in) {
			t.Fatalf("expected %q to be invalid", in)
		}
	}
}

func TestGetModeBySubmode(t *testing.T) {
	cases := []struct {
		in   string
		want Mode
	}{
		{in: "QPSK31", want: PSK},
		{in: "  dstar ", want: DIGITALVOICE},
		{in: "PSKHELL", want: HELL},
		{in: "APRS", want: PACKET},
		{in: "USB", want: SSB},
		{in: "lsb", want: SSB},
		{in: "ft4", want: MFSK}, // ADIF 3.1.5: the pair is MODE=MFSK SUBMODE=FT4
		{in: "FST4", want: MFSK},
		{in: "fst4w", want: MFSK},
		{in: "JS8", want: MFSK},
		{in: "Q65", want: MFSK},
	}

	for _, tc := range cases {
		got, ok := GetModeBySubmode(tc.in)
		if !ok {
			t.Fatalf("expected mapping for %q", tc.in)
		}
		if got != tc.want {
			t.Fatalf("GetModeBySubmode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if got, ok := GetModeBySubmode("unknown"); ok || got != "" {
		t.Fatalf("GetModeBySubmode(%q) = (%q, %v), want (%q, %v)", "unknown", got, ok, "", false)
	}
}

func TestCanonical(t *testing.T) {
	cases := []struct {
		mode, submode string
		wantMode      string
		wantSub       string
		ok            bool
	}{
		{"FT4", "", "MFSK", "FT4", true},      // the bare form the old catalogue accepted
		{" ft4 ", "ft4", "MFSK", "FT4", true}, // both halves naming the submode
		{"usb", "", "SSB", "USB", true},       // the operator-facing literal
		{"JS8", "", "MFSK", "JS8", true},
		{"MFSK", "FT4", "MFSK", "FT4", true}, // already canonical
		{"CW", "", "CW", "", true},
		{"CW", "CW-N", "CW", "CW-N", true},  // an unknown submode passes through
		{"", "FT4", "", "FT4", true},        // MODE absent: Submit derives it
		{"FT4", "JS8", "FT4", "JS8", false}, // a contradiction is refused, not guessed
	}
	for _, tc := range cases {
		m, s, ok := Canonical(tc.mode, tc.submode)
		if m != tc.wantMode || s != tc.wantSub || ok != tc.ok {
			t.Fatalf("Canonical(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.mode, tc.submode, m, s, ok, tc.wantMode, tc.wantSub, tc.ok)
		}
	}
}

func TestStringMethods(t *testing.T) {
	if got := AM.String(); got != "AM" {
		t.Fatalf("AM.String() = %q, want %q", got, "AM")
	}
	if got := USB.String(); got != "USB" {
		t.Fatalf("USB.String() = %q, want %q", got, "USB")
	}
}

func TestMainModes_IncludesBaseline(t *testing.T) {
	all := MainModes()
	want := map[string]bool{"AM": false, "CW": false, "FM": false, "FT8": false, "SSB": false}
	for _, m := range all {
		if _, ok := want[m]; ok {
			want[m] = true
		}
	}
	for m, found := range want {
		if !found {
			t.Fatalf("MainModes() missing %q", m)
		}
	}
}

func TestSubModes_IncludesBaseline(t *testing.T) {
	all := SubModes()
	want := map[string]bool{"USB": false, "LSB": false, "PSK31": false, "APRS": false}
	for _, m := range all {
		if _, ok := want[m]; ok {
			want[m] = true
		}
	}
	for m, found := range want {
		if !found {
			t.Fatalf("SubModes() missing %q", m)
		}
	}
}

// TestLoadOverride exercises the operator-override flow: a modes.json
// in the working dir extends the embedded catalogue with new main
// modes and submode entries. Missing file is a clean no-op; malformed
// file is an error. Operator's value wins on submode key collision.
func TestLoadOverride(t *testing.T) {
	t.Run("missing file is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		if err := LoadOverride(dir); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("malformed file errors", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "modes.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if err := LoadOverride(dir); err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("extends main_modes and submodes", func(t *testing.T) {
		// Sanity: SUPERMODE_TEST is not in the embedded baseline.
		if IsValidMode("SUPERMODE_TEST") {
			t.Fatalf("precondition failed: SUPERMODE_TEST should not exist before override")
		}
		dir := t.TempDir()
		payload := `{
			"main_modes": ["SUPERMODE_TEST"],
			"submodes": {"OVERRIDE_TEST": "SUPERMODE_TEST"}
		}`
		path := filepath.Join(dir, "modes.json")
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if err := LoadOverride(dir); err != nil {
			t.Fatalf("LoadOverride: %v", err)
		}
		if !IsValidMode("SUPERMODE_TEST") {
			t.Fatalf("expected SUPERMODE_TEST to be a valid main mode after override")
		}
		if !IsValidSubMode("OVERRIDE_TEST") {
			t.Fatalf("expected OVERRIDE_TEST to be a valid submode after override")
		}
		parent, ok := GetModeBySubmode("OVERRIDE_TEST")
		if !ok || parent != "SUPERMODE_TEST" {
			t.Fatalf("GetModeBySubmode(OVERRIDE_TEST) = (%q, %v); want (SUPERMODE_TEST, true)", parent, ok)
		}
	})

	t.Run("override submode key wins over embedded", func(t *testing.T) {
		// USB ships as SSB submode in the embedded baseline.
		parent, _ := GetModeBySubmode("USB")
		if parent != SSB {
			t.Fatalf("precondition: USB should resolve to SSB, got %q", parent)
		}
		dir := t.TempDir()
		// Operator reassigns USB → DIGITALVOICE (weird, but pins the
		// override-wins contract).
		payload := `{"submodes": {"USB": "DIGITALVOICE"}}`
		path := filepath.Join(dir, "modes.json")
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if err := LoadOverride(dir); err != nil {
			t.Fatalf("LoadOverride: %v", err)
		}
		parent, ok := GetModeBySubmode("USB")
		if !ok || parent != DIGITALVOICE {
			t.Fatalf("after override GetModeBySubmode(USB) = (%q, %v); want (DIGITALVOICE, true)", parent, ok)
		}
		// Restore for subsequent tests in the same process — the
		// override is mutating package state and these are not
		// hermetic without explicit reset. Cheap to roll back here.
		dir2 := t.TempDir()
		restore := `{"submodes": {"USB": "SSB"}}`
		if err := os.WriteFile(filepath.Join(dir2, "modes.json"), []byte(restore), 0o600); err != nil {
			t.Fatalf("write restore fixture: %v", err)
		}
		if err := LoadOverride(dir2); err != nil {
			t.Fatalf("LoadOverride restore: %v", err)
		}
	})
}
