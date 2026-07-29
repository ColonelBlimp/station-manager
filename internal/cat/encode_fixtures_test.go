package cat

import (
	stderr "errors"
	"testing"
)

// encodeCase pins one (rig, command name, args) → expected wire bytes
// mapping. Fixtures are the acceptance criteria for cat.Encode and must
// all pass against the real codec AND against referenceEncode (frozen
// v1 logic in reference_test.go).
//
// Command names and templates follow the project's three-burst convention
// (INIT, READ, PLAYBACK) rather than per-field commands. The rig parses
// the burst into its constituent ';'-terminated commands and emits a
// response for each; the decoder then consumes them as separate framed
// lines.
//
// INIT arms AUTO-mode push state (`AI1;`) with no response expected.
// READ requests a full identity + state snapshot (`ID;FA;FB;ST;VS;MD0;MD1;PC;`)
// — used by the bridge subsystem (M3a.3+) on each new SSE-open so a fresh
// SPA tab sees current rig state without waiting for the operator to
// wiggle the dial. The two are deliberately separated: INIT runs once per
// pipeline lifecycle (pipeline startup), READ runs once per SSE
// subscriber connect.
type encodeCase struct {
	name    string
	rigID   string
	cmdName string
	args    []any
	want    string
	wantErr bool
}

var encodeCases = []encodeCase{
	// --- Plain burst templates (no args) ---
	{name: "INIT on FTdx10", rigID: "yaesu-ftdx10", cmdName: "INIT", want: "AI1;"},
	{name: "READ on FTdx10", rigID: "yaesu-ftdx10", cmdName: "READ", want: "ID;FA;FB;ST;VS;MD0;MD1;PC;MS;"},
	{name: "INIT on FT-710", rigID: "yaesu-ft710", cmdName: "INIT", want: "AI1;"},
	{name: "READ on FT-710", rigID: "yaesu-ft710", cmdName: "READ", want: "ID;FA;FB;ST;VS;MD0;MD1;PC;"},

	// --- Template with %s arg (PLAYBACK) ---
	{name: "PLAYBACK channel 5 FTdx10", rigID: "yaesu-ftdx10", cmdName: "PLAYBACK", args: []any{"5"}, want: "PB05;"},
	{name: "PLAYBACK channel 0 FTdx10", rigID: "yaesu-ftdx10", cmdName: "PLAYBACK", args: []any{"0"}, want: "PB00;"},

	// --- Error cases ---
	{name: "unknown command name", rigID: "yaesu-ftdx10", cmdName: "NOT_A_COMMAND", wantErr: true},
}

func TestEncode(t *testing.T) {
	for _, tc := range encodeCases {
		t.Run(tc.name, func(t *testing.T) {
			def, ok := Lookup(tc.rigID)
			if !ok {
				t.Fatalf("Lookup(%q) not found", tc.rigID)
			}
			got, err := Encode(def, tc.cmdName, tc.args...)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for command %q, got nil (result: %q)", tc.cmdName, string(got))
				}
				if !stderr.Is(err, ErrUnknownCommand) {
					t.Fatalf("expected ErrUnknownCommand for %q, got unexpected error: %v", tc.cmdName, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("encode(%q on %s)\n got:  %q\n want: %q", tc.cmdName, tc.rigID, string(got), tc.want)
			}
		})
	}
}
