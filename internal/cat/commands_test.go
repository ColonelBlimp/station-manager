package cat

import (
	stderr "errors"
	"testing"
)

func TestHasCommand(t *testing.T) {
	def, ok := Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal(`Lookup("yaesu-ftdx10") not found`)
	}
	cases := []struct {
		name string
		want bool
	}{
		{"set_freq", true},
		{"set_mode", true},
		{"READ", true},
		{"NOT_A_COMMAND", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasCommand(def, tc.name); got != tc.want {
				t.Errorf("HasCommand(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestEncodeCommand exercises the data-driven inbound path (ADR 0026): the
// Exposed gate, ValueMap inversion (set_mode), and Pad (set_freq). The
// not-exposed cases are the safety boundary — PLAYBACK keys TX and INIT/READ
// are internal, so none may be reachable via EncodeCommand even though they
// exist in the rigdef.
func TestEncodeCommand(t *testing.T) {
	def, ok := Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal(`Lookup("yaesu-ftdx10") not found`)
	}
	cases := []struct {
		name    string
		cmd     string
		value   string
		want    string
		wantErr error
	}{
		{name: "set_freq 20m FT8", cmd: "set_freq", value: "14074000", want: "FA014074000;"},
		{name: "set_freq 6m FT8", cmd: "set_freq", value: "50313000", want: "FA050313000;"},
		{name: "set_mode DATA-U", cmd: "set_mode", value: "DATA-U", want: "MD0C;"},
		{name: "set_mode USB", cmd: "set_mode", value: "USB", want: "MD02;"},
		{name: "set_mode LSB", cmd: "set_mode", value: "LSB", want: "MD01;"},
		{name: "set_vfo A", cmd: "set_vfo", value: "VFO-A", want: "VS0;"},
		{name: "set_vfo B", cmd: "set_vfo", value: "VFO-B", want: "VS1;"},
		{name: "band_up valueless", cmd: "band_up", value: "", want: "BU0;"},
		{name: "band_down valueless", cmd: "band_down", value: "", want: "BD0;"},
		{name: "set_freq missing value", cmd: "set_freq", value: "", wantErr: ErrMissingValue},
		{name: "set_band 160m", cmd: "set_band", value: "160m", want: "BS00;"},
		{name: "set_band 20m", cmd: "set_band", value: "20m", want: "BS05;"},
		{name: "set_band 6m", cmd: "set_band", value: "6m", want: "BS10;"},
		{name: "set_mode unknown literal", cmd: "set_mode", value: "NOT-A-MODE", wantErr: ErrUnmappedValue},
		{name: "PLAYBACK not exposed", cmd: "PLAYBACK", value: "5", wantErr: ErrCommandNotExposed},
		{name: "READ not exposed", cmd: "READ", value: "", wantErr: ErrCommandNotExposed},
		{name: "unknown command", cmd: "NOT_A_COMMAND", value: "x", wantErr: ErrUnknownCommand},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeCommand(def, tc.cmd, tc.value)
			if tc.wantErr != nil {
				if !stderr.Is(err, tc.wantErr) {
					t.Fatalf("EncodeCommand(%q, %q) error = %v, want %v", tc.cmd, tc.value, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("EncodeCommand(%q, %q) = %q, want %q", tc.cmd, tc.value, string(got), tc.want)
			}
		})
	}
}

// TestEncodeCommandBijection pins the inversion set_mode relies on: every
// mode literal the MAINMODE decoder can emit must round-trip through
// EncodeCommand to the same wire code (MD0<key>;). If a future rigdef edit
// introduced a duplicate literal, the send-side inversion would silently
// pick the wrong code — this fails instead.
func TestEncodeCommandBijection(t *testing.T) {
	def, ok := Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal(`Lookup("yaesu-ftdx10") not found`)
	}
	for _, st := range def.States {
		for _, mk := range st.Markers {
			if mk.Tag != "MAINMODE" {
				continue
			}
			for _, vm := range mk.ValueMappings {
				got, err := EncodeCommand(def, "set_mode", vm.Value)
				if err != nil {
					t.Errorf("EncodeCommand(set_mode, %q) unexpected error: %v", vm.Value, err)
					continue
				}
				want := "MD0" + vm.Key + ";"
				if string(got) != want {
					t.Errorf("literal %q encoded to %q, want %q (duplicate literal?)", vm.Value, string(got), want)
				}
			}
		}
	}
}

// TestExposedCommands pins the advertised op vocabulary: the FTdx10 exposes
// exactly set_freq + set_mode (rigdef order), while the FT-710 — which has no
// inbound command path yet — exposes nothing, proving the default-deny of the
// Exposed flag.
func TestExposedCommands(t *testing.T) {
	def, ok := Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal(`Lookup("yaesu-ftdx10") not found`)
	}
	got := ExposedCommands(def)
	want := []string{"set_freq", "set_mode", "set_vfo", "band_up", "band_down", "set_band"}
	if len(got) != len(want) {
		t.Fatalf("ExposedCommands(ftdx10) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ExposedCommands[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	ft710, ok := Lookup("yaesu-ft710")
	if !ok {
		t.Fatal(`Lookup("yaesu-ft710") not found`)
	}
	if ops := ExposedCommands(ft710); len(ops) != 0 {
		t.Errorf("ExposedCommands(ft710) = %v, want empty (no exposed commands)", ops)
	}
}
