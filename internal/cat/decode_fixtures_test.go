package cat

import (
	stderr "errors"
	"reflect"
	"testing"
)

// decodeCase pins one (rig, input bytes) → expected CatStatus mapping.
// Fixtures are the source of truth for the §4 carve-out acceptance
// criteria: they must all pass against cat.Decode (the real codec) AND
// against referenceDecode (frozen v1 logic in reference_test.go). The
// reference stays frozen; this suite is what proves cat.Decode is
// equivalent to v1.
//
// Fixture values here follow v1's battle-tested FTdx10 config (lifted
// from internal/config/defaults.go on the v1 branch), so any drift from
// v1 behaviour shows up here first.
type decodeCase struct {
	name     string
	rigID    string
	input    string            // line bytes without the terminator
	expected map[string]string // nil means "no state should match"
}

var decodeCases = []decodeCase{
	// --- IDENTITY: 4-char rig id with value mapping ---
	{
		name:     "ID known code maps to FTdx10",
		rigID:    "yaesu-ftdx10",
		input:    "ID0761",
		expected: map[string]string{"IDENTITY": "FTdx10"},
	},
	{
		name:     "ID unknown code → empty string (v1 quirk)",
		rigID:    "yaesu-ftdx10",
		input:    "ID9999",
		expected: map[string]string{"IDENTITY": ""},
	},
	{
		name:     "ID on FT-710 — manual says P1=0800 is the FT-710 identifier",
		rigID:    "yaesu-ft710",
		input:    "ID0800",
		expected: map[string]string{"IDENTITY": "FT-710"},
	},
	{
		name:     "ID on FT-710 unknown code → empty string (v1 quirk)",
		rigID:    "yaesu-ft710",
		input:    "ID9999",
		expected: map[string]string{"IDENTITY": ""},
	},

	// --- FT-710-specific: ST only supports 0/1 (no "ON+"). ST2 decodes to
	// empty string per v1's unmapped-value quirk, unlike the FTdx10 which
	// maps 2 → "ON+". This fixture pins the rig-specific difference.
	{
		name:     "ST=2 on FT-710 → empty (rig only has 0/1, no ON+)",
		rigID:    "yaesu-ft710",
		input:    "ST2",
		expected: map[string]string{"SPLIT": ""},
	},
	{
		name:     "ST=1 on FT-710 → ON",
		rigID:    "yaesu-ft710",
		input:    "ST1",
		expected: map[string]string{"SPLIT": "ON"},
	},

	// --- FA / FB: 9-digit frequency, no mappings ---
	{
		name:     "FA VFO A at 14.250 MHz",
		rigID:    "yaesu-ftdx10",
		input:    "FA014250000",
		expected: map[string]string{"VFOAFREQ": "014250000"},
	},
	{
		name:     "FB VFO B at 7.074 MHz",
		rigID:    "yaesu-ftdx10",
		input:    "FB007074000",
		expected: map[string]string{"VFOBFREQ": "007074000"},
	},
	{
		name:     "FA at zero",
		rigID:    "yaesu-ftdx10",
		input:    "FA000000000",
		expected: map[string]string{"VFOAFREQ": "000000000"},
	},
	{
		name:     "FA on FT-710 (same schema)",
		rigID:    "yaesu-ft710",
		input:    "FA014074000",
		expected: map[string]string{"VFOAFREQ": "014074000"},
	},

	// --- Case-insensitive lookup ---
	{
		name:     "lowercase fa prefix matches FA state",
		rigID:    "yaesu-ftdx10",
		input:    "fa014250000",
		expected: map[string]string{"VFOAFREQ": "014250000"},
	},

	// --- ST / VS with value mappings ---
	{
		name:     "ST split off",
		rigID:    "yaesu-ftdx10",
		input:    "ST0",
		expected: map[string]string{"SPLIT": "OFF"},
	},
	{
		name:     "ST split on",
		rigID:    "yaesu-ftdx10",
		input:    "ST1",
		expected: map[string]string{"SPLIT": "ON"},
	},
	{
		name:     "VS VFO-A selected",
		rigID:    "yaesu-ftdx10",
		input:    "VS0",
		expected: map[string]string{"SELECT": "VFO-A"},
	},

	// --- MD0 / MD1 (prefix carries main/sub selector; longest-prefix-wins disambiguates) ---
	{
		name:     "MD0 main mode USB",
		rigID:    "yaesu-ftdx10",
		input:    "MD02",
		expected: map[string]string{"MAINMODE": "USB"},
	},
	{
		name:     "MD0 main mode LSB",
		rigID:    "yaesu-ftdx10",
		input:    "MD01",
		expected: map[string]string{"MAINMODE": "LSB"},
	},
	{
		name:     "MD0 main mode PSK (was wrong in my initial guess — v1 has E=PSK not C4FM)",
		rigID:    "yaesu-ftdx10",
		input:    "MD0E",
		expected: map[string]string{"MAINMODE": "PSK"},
	},
	{
		name:     "MD0 main mode DATA-FM-N",
		rigID:    "yaesu-ftdx10",
		input:    "MD0F",
		expected: map[string]string{"MAINMODE": "DATA-FM-N"},
	},
	{
		name:     "MD1 sub mode CW-U",
		rigID:    "yaesu-ftdx10",
		input:    "MD13",
		expected: map[string]string{"SUBMODE": "CW-U"},
	},
	{
		name:     "MD0 unknown mode code Z → empty string",
		rigID:    "yaesu-ftdx10",
		input:    "MD0Z",
		expected: map[string]string{"MAINMODE": ""},
	},

	// --- PC (TX power): 3 unmapped chars ---
	{
		name:     "PC TX power 050",
		rigID:    "yaesu-ftdx10",
		input:    "PC050",
		expected: map[string]string{"TXPWR": "050"},
	},

	// --- Bounds handling: tail too short for markers ---
	{
		name:     "MD0 with no tail — marker out of range, skipped",
		rigID:    "yaesu-ftdx10",
		input:    "MD0",
		expected: map[string]string{},
	},
	{
		name:     "FA with no tail — marker out of range, skipped",
		rigID:    "yaesu-ftdx10",
		input:    "FA",
		expected: map[string]string{},
	},
	{
		name:     "PC tail shorter than marker length → clamped slice",
		rigID:    "yaesu-ftdx10",
		input:    "PC05",
		expected: map[string]string{"TXPWR": "05"},
	},

	// --- No-match cases ---
	{
		name:     "unknown prefix ZZ",
		rigID:    "yaesu-ftdx10",
		input:    "ZZ999",
		expected: nil,
	},
	{
		name:     "empty input",
		rigID:    "yaesu-ftdx10",
		input:    "",
		expected: nil,
	},
}

func TestDecode(t *testing.T) {
	for _, tc := range decodeCases {
		t.Run(tc.name, func(t *testing.T) {
			def, ok := Lookup(tc.rigID)
			if !ok {
				t.Fatalf("Lookup(%q) not found", tc.rigID)
			}

			got, err := Decode(def, []byte(tc.input))

			if tc.expected == nil {
				if err == nil {
					t.Fatalf("expected ErrNoMatch for %q, got success with status=%v", tc.input, got)
				}
				if !stderr.Is(err, ErrNoMatch) {
					t.Fatalf("expected ErrNoMatch for %q, got unexpected error: %v", tc.input, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error decoding %q: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("decode(%q on %s)\n got:  %v\n want: %v",
					tc.input, tc.rigID, got, tc.expected)
			}
		})
	}
}

// TestReferenceLookupLongestWins pins the longest-prefix-wins behaviour
// directly. This matters in the live schema: "MD" is not a prefix, but
// "MD0" and "MD1" are — and if anyone ever adds a bare "MD" prefix the
// longer "MD0" / "MD1" must still win for MD0xxx / MD1xxx inputs.
func TestReferenceLookupLongestWins(t *testing.T) {
	states := []State{
		{Prefix: "MD", Markers: []Marker{{Tag: "short", Index: 0, Length: 2}}},
		{Prefix: "MD0", Markers: []Marker{{Tag: "long", Index: 0, Length: 1}}},
	}
	state, tail, ok := referenceLookup([]byte("MD02"), states)
	if !ok {
		t.Fatal("no match")
	}
	if state.Prefix != "MD0" {
		t.Errorf("prefix = %q, want MD0", state.Prefix)
	}
	if tail != "2" {
		t.Errorf("tail = %q, want %q", tail, "2")
	}
}
