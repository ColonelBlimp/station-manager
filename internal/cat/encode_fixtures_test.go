package cat

import (
	"testing"
)

// encodeCase pins one (rig, command name, args) → expected wire string
// mapping. Fixtures are the acceptance criteria for cat.Encode once it
// exists; they must all pass against referenceEncode (frozen v1 logic).
//
// Command names and templates follow v1's battle-tested defaults: three
// burst categories (INIT, READ, PLAYBACK) rather than per-field commands.
// The rig parses the burst into its constituent ';'-terminated commands
// and emits a response for each; the decoder then consumes them as
// separate framed lines.
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
	{name: "INIT on FTdx10", rigID: "yaesu-ftdx10", cmdName: "INIT", want: "AI1;ID;"},
	{name: "READ on FTdx10", rigID: "yaesu-ftdx10", cmdName: "READ", want: "FA;FB;ST;VS;MD0;MD1;PC;"},
	{name: "INIT on FT-710", rigID: "yaesu-ft710", cmdName: "INIT", want: "AI1;ID;"},
	{name: "READ on FT-710", rigID: "yaesu-ft710", cmdName: "READ", want: "FA;FB;ST;VS;MD0;MD1;PC;"},

	// --- Template with %s arg (PLAYBACK) ---
	{name: "PLAYBACK channel 5 FTdx10", rigID: "yaesu-ftdx10", cmdName: "PLAYBACK", args: []any{"5"}, want: "PB05;"},
	{name: "PLAYBACK channel 0 FTdx10", rigID: "yaesu-ftdx10", cmdName: "PLAYBACK", args: []any{"0"}, want: "PB00;"},

	// --- Error cases ---
	{name: "unknown command name", rigID: "yaesu-ftdx10", cmdName: "NOT_A_COMMAND", wantErr: true},
}

func TestReferenceEncode(t *testing.T) {
	for _, tc := range encodeCases {
		t.Run(tc.name, func(t *testing.T) {
			def, ok := Lookup(tc.rigID)
			if !ok {
				t.Fatalf("Lookup(%q) not found", tc.rigID)
			}
			got, err := referenceEncode(def, tc.cmdName, tc.args...)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for command %q, got nil (result: %q)", tc.cmdName, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("encode(%q on %s)\n got:  %q\n want: %q", tc.cmdName, tc.rigID, got, tc.want)
			}
		})
	}
}
