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
		{name: "swap_vfo valueless", cmd: "swap_vfo", value: "", want: "SV;"},
		{name: "band_up valueless", cmd: "band_up", value: "", want: "BU0;"},
		{name: "band_down valueless", cmd: "band_down", value: "", want: "BD0;"},
		{name: "set_freq missing value", cmd: "set_freq", value: "", wantErr: ErrMissingValue},
		{name: "set_band 160m", cmd: "set_band", value: "160m", want: "BS00;"},
		{name: "set_band 20m", cmd: "set_band", value: "20m", want: "BS05;"},
		{name: "set_band 6m", cmd: "set_band", value: "6m", want: "BS10;"},
		{name: "set_mode unknown literal", cmd: "set_mode", value: "NOT-A-MODE", wantErr: ErrUnmappedValue},
		// Padded-value validation (review 2026-06-05 M1): non-digit / over-wide
		// values must be rejected, not padded into a malformed CAT line.
		{name: "set_power non-digit", cmd: "set_power", value: "abc", wantErr: ErrInvalidPaddedValue},
		{name: "set_power dotted number", cmd: "set_power", value: "1.5", wantErr: ErrInvalidPaddedValue},
		{name: "set_power negative", cmd: "set_power", value: "-1", wantErr: ErrInvalidPaddedValue},
		{name: "set_freq non-digit", cmd: "set_freq", value: "14a74000", wantErr: ErrInvalidPaddedValue},
		{name: "set_freq over-wide", cmd: "set_freq", value: "1407400000", wantErr: ErrInvalidPaddedValue},
		{name: "set_power at width", cmd: "set_power", value: "100", want: "PC100;"},
		{name: "set_power padded", cmd: "set_power", value: "20", want: "PC020;"},
		// Semantic range validation (review 2026-06-19 M2): width-valid but
		// out-of-range Yaesu values must be rejected, not put on the wire (Yaesu
		// has no command ACK). PC range 005-100 W, FA/FB range 30k-75M Hz.
		{name: "set_power over max (PC999)", cmd: "set_power", value: "999", wantErr: ErrInvalidPaddedValue},
		{name: "set_power below min", cmd: "set_power", value: "4", wantErr: ErrInvalidPaddedValue},
		{name: "set_power at min", cmd: "set_power", value: "5", want: "PC005;"},
		{name: "set_freq below min", cmd: "set_freq", value: "29999", wantErr: ErrInvalidPaddedValue},
		{name: "set_freq at min", cmd: "set_freq", value: "30000", want: "FA000030000;"},
		{name: "set_freq at max", cmd: "set_freq", value: "75000000", want: "FA075000000;"},
		{name: "set_freq over max", cmd: "set_freq", value: "75000001", wantErr: ErrInvalidPaddedValue},
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

// TestExposedCommands pins the advertised op vocabulary: both shipped Yaesu
// rigdefs expose the same safe inbound commands in rigdef order (FA/FB/MD0/
// PC/SV/BU/BD/BS — all non-transmitting; their FT-710 formats are byte-identical
// to the FTdx10, confirmed against FT-710_CAT_OM_ENG_2306-C). The TX-keying
// commands (tx_on/tx_off) are deliberately absent from BOTH exposed lists: they
// are not Exposed, so they never reach the generic command path (ADR 0027) —
// only the tune controller may key TX. (Both rigdefs now carry tx_on/tx_off —
// the FT-710's added 2026-06-06 — but neither is Exposed, so the advertised
// vocabulary is unchanged.)
func TestExposedCommands(t *testing.T) {
	want := []string{"set_freq", "set_freq_b", "set_mode", "swap_vfo", "band_up", "band_down", "set_band", "set_power"}
	for _, id := range []string{"yaesu-ftdx10", "yaesu-ft710"} {
		def, ok := Lookup(id)
		if !ok {
			t.Fatalf("Lookup(%q) not found", id)
		}
		got := ExposedCommands(def)
		if len(got) != len(want) {
			t.Fatalf("ExposedCommands(%s) = %v, want %v", id, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("ExposedCommands(%s)[%d] = %q, want %q", id, i, got[i], want[i])
			}
		}
	}
}

// TestEncodeCommand_FT710 pins that the FT-710's added write entries encode to
// the same wire bytes as the FTdx10 (formats confirmed byte-identical against
// FT-710_CAT_OM_ENG_2306-C). Guards the new rigdef entries + the FT-710 BAND
// value_map.
func TestEncodeCommand_FT710(t *testing.T) {
	def, ok := Lookup("yaesu-ft710")
	if !ok {
		t.Fatal(`Lookup("yaesu-ft710") not found`)
	}
	cases := []struct {
		cmd, value, want string
	}{
		{"set_freq", "14074000", "FA014074000;"},
		{"set_freq_b", "7074000", "FB007074000;"},
		{"set_mode", "USB", "MD02;"},
		{"set_mode", "RTTY-U", "MD09;"},
		{"set_power", "20", "PC020;"},
		{"swap_vfo", "", "SV;"},
		{"band_up", "", "BU0;"},
		{"band_down", "", "BD0;"},
		{"set_band", "20m", "BS05;"},
		{"set_band", "6m", "BS10;"},
	}
	for _, c := range cases {
		got, err := EncodeCommand(def, c.cmd, c.value)
		if err != nil {
			t.Errorf("EncodeCommand(%q, %q): %v", c.cmd, c.value, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("EncodeCommand(%q, %q) = %q, want %q", c.cmd, c.value, string(got), c.want)
		}
	}
}

// TestEncodeCommand_SetPower pins the tune controller's power command: PC with
// a 3-wide zero-padded value (ADR 0027). set_power is Exposed (setting power
// never transmits) so it also rides the generic command path.
func TestEncodeCommand_SetPower(t *testing.T) {
	def, _ := Lookup("yaesu-ftdx10")
	cases := []struct{ in, want string }{
		{"20", "PC020;"},
		{"5", "PC005;"},
		{"100", "PC100;"},
	}
	for _, c := range cases {
		got, err := EncodeCommand(def, "set_power", c.in)
		if err != nil {
			t.Fatalf("EncodeCommand(set_power,%q): %v", c.in, err)
		}
		if string(got) != c.want {
			t.Errorf("EncodeCommand(set_power,%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestTxCommands_NotExposed is the safety gate for ADR 0027: the TX-keying
// commands exist in the rigdef but are NOT Exposed, so the generic command
// path (EncodeCommand) refuses them — only the tune controller may key TX,
// via the low-level Encode (verified here too).
func TestTxCommands_NotExposed(t *testing.T) {
	// Both shipped Yaesu rigdefs carry tx_on/tx_off (the FT-710's added
	// 2026-06-06); the safety gate must hold for both.
	for _, id := range []string{"yaesu-ftdx10", "yaesu-ft710"} {
		def, ok := Lookup(id)
		if !ok {
			t.Fatalf("Lookup(%q) not found", id)
		}

		for _, name := range []string{"tx_on", "tx_off"} {
			if _, err := EncodeCommand(def, name, ""); !stderr.Is(err, ErrCommandNotExposed) {
				t.Errorf("%s: EncodeCommand(%q) err = %v, want ErrCommandNotExposed", id, name, err)
			}
		}

		// The low-level Encode (controller-internal) still produces the bytes.
		for _, c := range []struct{ name, want string }{
			{"tx_on", "TX1;"},
			{"tx_off", "TX0;"},
		} {
			got, err := Encode(def, c.name)
			if err != nil {
				t.Fatalf("%s: Encode(%q): %v", id, c.name, err)
			}
			if string(got) != c.want {
				t.Errorf("%s: Encode(%q) = %q, want %q", id, c.name, got, c.want)
			}
		}
	}
}
