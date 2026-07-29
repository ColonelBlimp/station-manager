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

	// --- RM READ METER (FTdx10 CAT ref 2308-F p.20) ---
	//
	// The rig answers/pushes `RM P1 P2P2P2 P3P3P3;` where P1 names the meter
	// (1:S 3:COMP 4:ALC 5:PO 6:SWR 7:IDD 8:VDD), P2 is the 0-255 reading and
	// P3 is a fixed "000". RM is marked AI=O in the command index, so these
	// arrive unprompted while transmitting — the bridge listens, it does not
	// poll (no CAT traffic is added to the key-down path).
	//
	// Each fixture below pins a way a plausible implementation could go wrong:
	{
		// A swapped 4/5 mapping still decodes "a meter", so the two fixtures
		// carry DIFFERENT readings — 090 vs 128 — and a swap fails on value.
		name:     "RM4 → ALC carries its own reading",
		rigID:    "yaesu-ftdx10",
		input:    "RM4090000",
		expected: map[string]string{"ALC": "090"},
	},
	{
		name:     "RM5 → PO carries its own reading",
		rigID:    "yaesu-ftdx10",
		input:    "RM5128000",
		expected: map[string]string{"PO": "128"},
	},
	{
		// The reading is P2 alone. A marker length of 6 would swallow the
		// fixed P3 and yield "128000" — plausible, and wrong by 1000x.
		name:     "RM reading is P2 only, not P2+P3",
		rigID:    "yaesu-ftdx10",
		input:    "RM5128000",
		expected: map[string]string{"PO": "128"},
	},
	{
		// THE case this feature exists to catch. A zero reading must decode as
		// a reading, not vanish into an empty/absent tag: "the rig keyed and
		// PO read 000" is the drive-collapse signature, and a codec that
		// dropped it would be blind to precisely the fault under investigation.
		name:     "RM5 zero power decodes as a reading, not as absent",
		rigID:    "yaesu-ftdx10",
		input:    "RM5000000",
		expected: map[string]string{"PO": "000"},
	},
	{
		// Full-scale bound, the other end of the 0-255 range.
		name:     "RM4 full-scale ALC",
		rigID:    "yaesu-ftdx10",
		input:    "RM4255000",
		expected: map[string]string{"ALC": "255"},
	},
	{
		// SWR foldback is a candidate CAUSE of the drive collapse: a rig
		// protecting its finals into a bad load drops drive, which reads at
		// the PO meter exactly like the fault under investigation. A third
		// distinct value (030) so any two-way mix-up between the three
		// modelled meters fails on value, not just on tag.
		name:     "RM6 → SWR carries its own reading",
		rigID:    "yaesu-ftdx10",
		input:    "RM6030000",
		expected: map[string]string{"SWR": "030"},
	},

	// --- RM0: the meter the rig actually PUSHES ---
	//
	// Measured on the dogfood FTdx10 (2026-07-29): with AI armed the rig pushes
	// RM0nnn000 at ~26 Hz and pushes NOTHING under RM4/RM5/RM6 — those are
	// query-only. RM0 carries the value of whatever meter is currently
	// selected, which is why it is tagged METER rather than any specific one:
	// naming it PO would be an inference the frame does not carry. MS reports
	// the selection (see below), and the two together are the interpretation.
	{
		name:     "RM0 push decodes as the selected-meter value",
		rigID:    "yaesu-ftdx10",
		input:    "RM0124000",
		expected: map[string]string{"METER": "124"},
	},
	{
		// The drive-collapse signature, on the prefix the rig really sends.
		name:     "RM0 zero decodes as a reading, not as absent",
		rigID:    "yaesu-ftdx10",
		input:    "RM0000000",
		expected: map[string]string{"METER": "000"},
	},
	{
		// RM0 must not swallow the explicit query answers: longest-prefix-wins
		// has to keep RM4/RM5/RM6 distinct from RM0, or a polled PO reply would
		// be filed as the generic selected meter.
		name:     "RM0 does not swallow the RM5 query answer",
		rigID:    "yaesu-ftdx10",
		input:    "RM5128000",
		expected: map[string]string{"PO": "128"},
	},

	// --- MS METER SW: which meter RM0 is reporting (CAT ref 2308-F p.16) ---
	{
		name:     "MS0 → PO selected",
		rigID:    "yaesu-ftdx10",
		input:    "MS00",
		expected: map[string]string{"METERSEL": "PO"},
	},
	{
		name:     "MS2 → ALC selected",
		rigID:    "yaesu-ftdx10",
		input:    "MS20",
		expected: map[string]string{"METERSEL": "ALC"},
	},
	{
		name:     "MS5 → SWR selected",
		rigID:    "yaesu-ftdx10",
		input:    "MS50",
		expected: map[string]string{"METERSEL": "SWR"},
	},

	// --- No-match cases ---
	{
		// Guards against a lazy bare-"RM" prefix: the S-meter shares the RM
		// stem, so a prefix that ignored P1 would silently report an S-meter
		// reading as ALC, PO, SWR or the selected meter. Unmodelled selectors
		// must not match at all.
		name:     "RM1 (S meter) is not attributed to a modelled meter",
		rigID:    "yaesu-ftdx10",
		input:    "RM1015000",
		expected: nil,
	},
	{
		// Second unmodelled selector, so the guard isn't pinned to one value.
		name:     "RM3 (COMP) is not attributed to a modelled meter",
		rigID:    "yaesu-ftdx10",
		input:    "RM3120000",
		expected: nil,
	},
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
