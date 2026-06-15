package cat

import (
	stderr "errors"
	"testing"
)

// civTestDef is an IC-7300-shaped CI-V rigdef used across the engine tests.
// It mirrors the schema ADR 0034 settled on: hex command bytes in Cmd, a
// per-literal mode_seq with one frame for non-data modes and two for the data
// mode, and BCD-freq / byte markers on the decode states. The exact bytes are
// the ones bench-validated on the rig (addr 0x94, freq LE BCD, 06 self-clears
// the data flag).
func civTestDef() RigDefinition {
	modeMaps := []ValueMapping{
		{Key: "00", Value: "LSB"},
		{Key: "01", Value: "USB"},
		{Key: "03", Value: "CW"},
	}
	return RigDefinition{
		Protocol:   ProtocolIcomCIV,
		CivAddress: "94",
		Terminator: "\xfd",
		Commands: []Command{
			{Name: "READ", Cmd: "03", Encoding: EncodingNone},
			{Name: "set_freq", Cmd: "05", Encoding: EncodingBCDFreq, Exposed: true},
			{Name: "set_mode", Cmd: "06", Encoding: EncodingModeSeq, Exposed: true, ModeSeq: []ModeSequence{
				{Mode: "LSB", Frames: []string{"0600:01"}},
				{Mode: "USB", Frames: []string{"0601:01"}},
				{Mode: "CW", Frames: []string{"0603:01"}},
				{Mode: "USB-D", Frames: []string{"0601:01", "1A06:0101"}},
			}},
			{Name: "set_power", Cmd: "140A", Encoding: EncodingBCDPower, Exposed: true},
			{Name: "tx_on", Cmd: "1C0001", Encoding: EncodingNone},
			{Name: "tx_off", Cmd: "1C0000", Encoding: EncodingNone},
		},
		States: []State{
			{Prefix: "00", Markers: []Marker{{Tag: "VFOAFREQ", Kind: MarkerKindBCDFreq, Index: 0, Length: 5}}},
			{Prefix: "03", Markers: []Marker{{Tag: "VFOAFREQ", Kind: MarkerKindBCDFreq, Index: 0, Length: 5}}},
			{Prefix: "01", Markers: []Marker{{Tag: "MAINMODE", Kind: MarkerKindByte, Index: 0, Length: 1, ValueMappings: modeMaps}}},
			{Prefix: "04", Markers: []Marker{{Tag: "MAINMODE", Kind: MarkerKindByte, Index: 0, Length: 1, ValueMappings: modeMaps}}},
		},
	}
}

func eqBytes(t *testing.T, got, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length: got % X (%d), want % X (%d)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got % X, want % X", i, got, want)
		}
	}
}

func TestFreqToBCD_RoundTrip(t *testing.T) {
	cases := []struct {
		hz  string
		bcd []byte
	}{
		{"14074000", []byte{0x00, 0x40, 0x07, 0x14, 0x00}},  // bench-validated 20m FT8
		{"7074000", []byte{0x00, 0x40, 0x07, 0x07, 0x00}},   // 40m
		{"146520000", []byte{0x00, 0x00, 0x52, 0x46, 0x01}}, // 2m
		{"0", []byte{0x00, 0x00, 0x00, 0x00, 0x00}},
	}
	for _, c := range cases {
		got, err := freqToBCD(c.hz)
		if err != nil {
			t.Fatalf("freqToBCD(%q): %v", c.hz, err)
		}
		eqBytes(t, got, c.bcd)

		back, err := bcdToFreq(c.bcd)
		if err != nil {
			t.Fatalf("bcdToFreq(% X): %v", c.bcd, err)
		}
		want := c.hz
		if want == "0" {
			want = "0"
		}
		if back != want {
			t.Errorf("bcdToFreq(% X) = %q, want %q", c.bcd, back, want)
		}
	}
}

func TestFreqToBCD_Rejects(t *testing.T) {
	for _, bad := range []string{"", "abc", "14.074", "-100", "123456789012"} {
		if _, err := freqToBCD(bad); err == nil {
			t.Errorf("freqToBCD(%q) = nil error, want error", bad)
		}
	}
}

func TestBCDToFreq_RejectsBadNibble(t *testing.T) {
	// 0xAB is not a valid BCD pair (nibbles > 9).
	if _, err := bcdToFreq([]byte{0xAB, 0x00, 0x00, 0x00, 0x00}); err == nil {
		t.Errorf("bcdToFreq(invalid BCD) = nil error, want error")
	}
}

func TestLevelToBCD(t *testing.T) {
	cases := []struct {
		dec string
		bcd []byte
	}{
		{"0", []byte{0x00, 0x00}},
		{"100", []byte{0x01, 0x00}},
		{"255", []byte{0x02, 0x55}},
	}
	for _, c := range cases {
		got, err := levelToBCD(c.dec, civPowerBCDLen)
		if err != nil {
			t.Fatalf("levelToBCD(%q): %v", c.dec, err)
		}
		eqBytes(t, got, c.bcd)
	}
}

func TestEncodeCIV_None(t *testing.T) {
	def := civTestDef()

	read, err := Encode(def, "READ")
	if err != nil {
		t.Fatalf("Encode READ: %v", err)
	}
	eqBytes(t, read, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x03, 0xFD})

	on, err := Encode(def, "tx_on")
	if err != nil {
		t.Fatalf("Encode tx_on: %v", err)
	}
	eqBytes(t, on, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x1C, 0x00, 0x01, 0xFD})

	off, err := Encode(def, "tx_off")
	if err != nil {
		t.Fatalf("Encode tx_off: %v", err)
	}
	eqBytes(t, off, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x1C, 0x00, 0x00, 0xFD})
}

func TestEncodeCIV_UnknownCommand(t *testing.T) {
	_, err := Encode(civTestDef(), "nope")
	if !stderr.Is(err, ErrUnknownCommand) {
		t.Errorf("Encode unknown: err = %v, want ErrUnknownCommand", err)
	}
}

func TestEncodeCommandCIV_Freq(t *testing.T) {
	got, err := EncodeCommand(civTestDef(), "set_freq", "14074000")
	if err != nil {
		t.Fatalf("EncodeCommand set_freq: %v", err)
	}
	eqBytes(t, got, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x05, 0x00, 0x40, 0x07, 0x14, 0x00, 0xFD})
}

func TestEncodeCommandCIV_ModeSeq(t *testing.T) {
	def := civTestDef()

	// Non-data mode: one frame, 06 self-clears the data flag (bench-validated).
	usb, err := EncodeCommand(def, "set_mode", "USB")
	if err != nil {
		t.Fatalf("EncodeCommand set_mode USB: %v", err)
	}
	eqBytes(t, usb, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x06, 0x01, 0x01, 0xFD})

	// Data mode: two frames concatenated — base mode then 1A 06 data ON.
	usbD, err := EncodeCommand(def, "set_mode", "USB-D")
	if err != nil {
		t.Fatalf("EncodeCommand set_mode USB-D: %v", err)
	}
	eqBytes(t, usbD, []byte{
		0xFE, 0xFE, 0x94, 0xE0, 0x06, 0x01, 0x01, 0xFD,
		0xFE, 0xFE, 0x94, 0xE0, 0x1A, 0x06, 0x01, 0x01, 0xFD,
	})
}

func TestEncodeCommandCIV_Power(t *testing.T) {
	got, err := EncodeCommand(civTestDef(), "set_power", "100")
	if err != nil {
		t.Fatalf("EncodeCommand set_power: %v", err)
	}
	eqBytes(t, got, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x14, 0x0A, 0x01, 0x00, 0xFD})
}

func TestEncodeCommandCIV_Errors(t *testing.T) {
	def := civTestDef()

	if _, err := EncodeCommand(def, "READ", ""); !stderr.Is(err, ErrCommandNotExposed) {
		t.Errorf("unexposed: err = %v, want ErrCommandNotExposed", err)
	}
	if _, err := EncodeCommand(def, "nope", "x"); !stderr.Is(err, ErrUnknownCommand) {
		t.Errorf("unknown: err = %v, want ErrUnknownCommand", err)
	}
	if _, err := EncodeCommand(def, "set_mode", "FM"); !stderr.Is(err, ErrUnmappedValue) {
		t.Errorf("unmapped mode: err = %v, want ErrUnmappedValue", err)
	}
	if _, err := EncodeCommand(def, "set_freq", ""); !stderr.Is(err, ErrMissingValue) {
		t.Errorf("missing freq: err = %v, want ErrMissingValue", err)
	}
	if _, err := EncodeCommand(def, "set_freq", "abc"); !stderr.Is(err, ErrInvalidPaddedValue) {
		t.Errorf("bad freq: err = %v, want ErrInvalidPaddedValue", err)
	}
}

func TestDecodeCIV_FreqBroadcast(t *testing.T) {
	// FE FE 00 94 00 <5 BCD> — Transceive broadcast to 0x00 from the rig.
	line := []byte{0xFE, 0xFE, 0x00, 0x94, 0x00, 0x00, 0x40, 0x07, 0x14, 0x00}
	st, err := Decode(civTestDef(), line)
	if err != nil {
		t.Fatalf("Decode freq broadcast: %v", err)
	}
	if st["VFOAFREQ"] != "14074000" {
		t.Errorf("VFOAFREQ = %q, want 14074000", st["VFOAFREQ"])
	}
}

func TestDecodeCIV_FreqReply(t *testing.T) {
	// FE FE E0 94 03 <5 BCD> — transponded reply to the controller from the rig.
	line := []byte{0xFE, 0xFE, 0xE0, 0x94, 0x03, 0x00, 0x40, 0x07, 0x14, 0x00}
	st, err := Decode(civTestDef(), line)
	if err != nil {
		t.Fatalf("Decode freq reply: %v", err)
	}
	if st["VFOAFREQ"] != "14074000" {
		t.Errorf("VFOAFREQ = %q, want 14074000", st["VFOAFREQ"])
	}
}

func TestDecodeCIV_ModeBroadcast(t *testing.T) {
	// FE FE 00 94 01 01 01 — mode broadcast: base mode 01 (USB), filter 01.
	line := []byte{0xFE, 0xFE, 0x00, 0x94, 0x01, 0x01, 0x01}
	st, err := Decode(civTestDef(), line)
	if err != nil {
		t.Fatalf("Decode mode broadcast: %v", err)
	}
	if st["MAINMODE"] != "USB" {
		t.Errorf("MAINMODE = %q, want USB", st["MAINMODE"])
	}
}

func TestDecodeCIV_EchoFiltered(t *testing.T) {
	// from == controller (E0): our own echoed send — must be dropped.
	line := []byte{0xFE, 0xFE, 0x94, 0xE0, 0x05, 0x00, 0x40, 0x07, 0x14, 0x00}
	_, err := Decode(civTestDef(), line)
	if !stderr.Is(err, ErrNoMatch) {
		t.Errorf("echo: err = %v, want ErrNoMatch", err)
	}
}

func TestDecodeCIV_UnknownCommandByte(t *testing.T) {
	// Command byte 0x27 (scope) — not in the state table; dropped as no-match.
	line := []byte{0xFE, 0xFE, 0x00, 0x94, 0x27, 0x00}
	_, err := Decode(civTestDef(), line)
	if !stderr.Is(err, ErrNoMatch) {
		t.Errorf("unknown cmd: err = %v, want ErrNoMatch", err)
	}
}

func TestDecodeCIV_ShortAndMalformed(t *testing.T) {
	def := civTestDef()
	for _, line := range [][]byte{
		nil,
		{0xFE},
		{0xFE, 0xFE, 0x00, 0x94},       // no command byte
		{0x01, 0x02, 0x00, 0x94, 0x00}, // no FE FE preamble
	} {
		if _, err := Decode(def, line); !stderr.Is(err, ErrNoMatch) {
			t.Errorf("Decode(% X): err = %v, want ErrNoMatch", line, err)
		}
	}
}

func TestRigModes_CIV(t *testing.T) {
	got := RigModes(civTestDef())
	want := []string{"LSB", "USB", "CW", "USB-D"}
	if len(got) != len(want) {
		t.Fatalf("RigModes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("RigModes[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestValidateCIV_OK(t *testing.T) {
	if err := ValidateRigDefinition(civTestDef()); err != nil {
		t.Fatalf("ValidateRigDefinition(valid CI-V): %v", err)
	}
}

func TestValidateCIV_Rejects(t *testing.T) {
	cases := map[string]func(*RigDefinition){
		"bad civ_address":        func(d *RigDefinition) { d.CivAddress = "ZZ" },
		"empty civ_address":      func(d *RigDefinition) { d.CivAddress = "" },
		"multi-byte civ_address": func(d *RigDefinition) { d.CivAddress = "94E0" },
		"unknown encoding":       func(d *RigDefinition) { d.Commands[0].Encoding = "weird" },
		"bad hex cmd":            func(d *RigDefinition) { d.Commands[0].Cmd = "XY" },
		"empty cmd":              func(d *RigDefinition) { d.Commands[0].Cmd = "" },
		"duplicate command": func(d *RigDefinition) {
			d.Commands = append(d.Commands, Command{Name: "READ", Cmd: "03", Encoding: EncodingNone})
		},
		"exposed tx_on": func(d *RigDefinition) {
			for i := range d.Commands {
				if d.Commands[i].Name == "tx_on" {
					d.Commands[i].Exposed = true
				}
			}
		},
		"mode_seq no entries": func(d *RigDefinition) {
			for i := range d.Commands {
				if d.Commands[i].Name == "set_mode" {
					d.Commands[i].ModeSeq = nil
				}
			}
		},
		"mode_seq duplicate mode": func(d *RigDefinition) {
			for i := range d.Commands {
				if d.Commands[i].Name == "set_mode" {
					d.Commands[i].ModeSeq = append(d.Commands[i].ModeSeq,
						ModeSequence{Mode: "USB", Frames: []string{"0601:01"}})
				}
			}
		},
		"mode_seq bad frame hex": func(d *RigDefinition) {
			for i := range d.Commands {
				if d.Commands[i].Name == "set_mode" {
					d.Commands[i].ModeSeq[0].Frames = []string{"ZZ"}
				}
			}
		},
		"unknown marker kind": func(d *RigDefinition) { d.States[0].Markers[0].Kind = "nope" },
		"bad state prefix":    func(d *RigDefinition) { d.States[0].Prefix = "ZZ" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			def := civTestDef()
			mutate(&def)
			if err := ValidateRigDefinition(def); err == nil {
				t.Errorf("ValidateRigDefinition(%s) = nil, want error", name)
			}
		})
	}
}

// TestEmbeddedIC7300 pins the shipped icom-ic7300.json rigdef end-to-end
// through the public codec surface — the same functions the bridge calls. The
// embedded loader already ran ValidateRigDefinition at init (a fault would
// panic the package), so this asserts the wire bytes are what the bench
// validated, not just that the file parses.
func TestEmbeddedIC7300(t *testing.T) {
	def, ok := Lookup("icom-ic7300")
	if !ok {
		t.Fatal("icom-ic7300 not in the embedded rig DB")
	}
	if def.Protocol != ProtocolIcomCIV {
		t.Fatalf("Protocol = %q, want %q", def.Protocol, ProtocolIcomCIV)
	}
	if def.Ft8Mode != "USB-D" {
		t.Errorf("Ft8Mode = %q, want USB-D", def.Ft8Mode)
	}

	// INIT primes comms with a single freq read; READ snapshots freq + mode.
	initBytes, err := Encode(def, "INIT")
	if err != nil {
		t.Fatalf("Encode INIT: %v", err)
	}
	eqBytes(t, initBytes, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x03, 0xFD})

	read, err := Encode(def, "READ")
	if err != nil {
		t.Fatalf("Encode READ: %v", err)
	}
	eqBytes(t, read, []byte{
		0xFE, 0xFE, 0x94, 0xE0, 0x03, 0xFD,
		0xFE, 0xFE, 0x94, 0xE0, 0x04, 0xFD,
	})

	// FT8 mode set: base USB then data ON (the bench-validated two-frame form).
	usbD, err := EncodeCommand(def, "set_mode", "USB-D")
	if err != nil {
		t.Fatalf("EncodeCommand set_mode USB-D: %v", err)
	}
	eqBytes(t, usbD, []byte{
		0xFE, 0xFE, 0x94, 0xE0, 0x06, 0x01, 0x01, 0xFD,
		0xFE, 0xFE, 0x94, 0xE0, 0x1A, 0x06, 0x01, 0x01, 0xFD,
	})

	// Inbound freq op.
	freq, err := EncodeCommand(def, "set_freq", "14074000")
	if err != nil {
		t.Fatalf("EncodeCommand set_freq: %v", err)
	}
	eqBytes(t, freq, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x05, 0x00, 0x40, 0x07, 0x14, 0x00, 0xFD})

	// Decode a real freq + mode broadcast.
	st, err := Decode(def, []byte{0xFE, 0xFE, 0x00, 0x94, 0x00, 0x00, 0x40, 0x07, 0x14, 0x00})
	if err != nil || st["VFOAFREQ"] != "14074000" {
		t.Errorf("Decode freq broadcast: st=%v err=%v", st, err)
	}
	st, err = Decode(def, []byte{0xFE, 0xFE, 0x00, 0x94, 0x01, 0x03, 0x01})
	if err != nil || st["MAINMODE"] != "CW" {
		t.Errorf("Decode mode broadcast: st=%v err=%v", st, err)
	}

	// TX must not be reachable: the rigdef ships no exposed TX command, and the
	// inbound ops are exactly freq + mode (RX-safe layer; TX is a later step).
	ops := ExposedCommands(def)
	if len(ops) != 2 {
		t.Errorf("ExposedCommands = %v, want exactly [set_freq set_mode]", ops)
	}
	for _, op := range ops {
		if op == "tx_on" || op == "tx_off" || op == "set_power" {
			t.Errorf("ExposedCommands includes %q — must never be exposed", op)
		}
	}

	// Settable modes include the data mode (superset of broadcast base modes).
	modes := RigModes(def)
	var hasUSBD bool
	for _, m := range modes {
		if m == "USB-D" {
			hasUSBD = true
		}
	}
	if !hasUSBD {
		t.Errorf("RigModes = %v, want it to include USB-D", modes)
	}
}

func TestValidate_UnknownProtocol(t *testing.T) {
	def := civTestDef()
	def.Protocol = "martian"
	if err := ValidateRigDefinition(def); err == nil {
		t.Errorf("ValidateRigDefinition(unknown protocol) = nil, want error")
	}
}
