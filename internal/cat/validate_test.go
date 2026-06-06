package cat

import "testing"

// TestEmbeddedRigDefinitionsValidate asserts every shipped rigdef passes
// ValidateRigDefinition. (init() already panics on an invalid embedded rig, so
// this is also a guard that the validation contract isn't quietly loosened.)
func TestEmbeddedRigDefinitionsValidate(t *testing.T) {
	for _, id := range List() {
		def, ok := Lookup(id)
		if !ok {
			t.Fatalf("Lookup(%q) not found", id)
		}
		if err := ValidateRigDefinition(def); err != nil {
			t.Errorf("embedded rig %q failed validation: %v", id, err)
		}
	}
}

// goodRigDef returns a minimal structurally-valid definition: one valueless,
// one padded, and one value-mapped command, plus the marker the value_map
// references. Rebuilt fresh per call so mutation cases don't alias.
func goodRigDef() RigDefinition {
	return RigDefinition{
		ID: "test",
		Commands: []Command{
			{Name: "INIT", Cmd: "AI1;"},
			{Name: "set_freq", Cmd: "FA%s;", Pad: 9, Exposed: true},
			{Name: "set_mode", Cmd: "MD0%s;", ValueMap: "MAINMODE", Exposed: true},
		},
		States: []State{
			{Prefix: "MD0", Markers: []Marker{
				{Tag: "MAINMODE", Index: 0, Length: 1, ValueMappings: []ValueMapping{
					{Key: "1", Value: "LSB"},
					{Key: "2", Value: "USB"},
				}},
			}},
		},
	}
}

func TestValidateRigDefinition(t *testing.T) {
	if err := ValidateRigDefinition(goodRigDef()); err != nil {
		t.Fatalf("good def failed validation: %v", err)
	}

	// Non-exposed TX/playback commands are the correct, supported shape (the
	// tune controller reaches them via the low-level Encode) — only EXPOSED
	// ones are rejected. Guards against the M2 check over-restricting.
	withSafeTx := goodRigDef()
	withSafeTx.Commands = append(withSafeTx.Commands,
		Command{Name: "tx_on", Cmd: "TX1;"},
		Command{Name: "tx_off", Cmd: "TX0;"},
	)
	if err := ValidateRigDefinition(withSafeTx); err != nil {
		t.Fatalf("def with non-exposed tx_on/tx_off failed validation: %v", err)
	}

	cases := []struct {
		name string
		mod  func(*RigDefinition)
	}{
		{"empty command name", func(d *RigDefinition) { d.Commands[0].Name = "" }},
		{"empty command template", func(d *RigDefinition) { d.Commands[0].Cmd = "" }},
		{"duplicate command name", func(d *RigDefinition) { d.Commands[2].Name = "set_freq" }},
		// M2: TX/playback commands must not be Exposed — the schema, not just
		// TestTxCommands_NotExposed, has to reject this for future rigdefs.
		{"exposed tx_on", func(d *RigDefinition) {
			d.Commands = append(d.Commands, Command{Name: "tx_on", Cmd: "TX1;", Exposed: true})
		}},
		{"exposed tx_off", func(d *RigDefinition) {
			d.Commands = append(d.Commands, Command{Name: "tx_off", Cmd: "TX0;", Exposed: true})
		}},
		{"exposed PLAYBACK", func(d *RigDefinition) {
			d.Commands = append(d.Commands, Command{Name: "PLAYBACK", Cmd: "PB0%s;", Exposed: true})
		}},
		{"negative pad", func(d *RigDefinition) { d.Commands[1].Pad = -1 }},
		{"padded missing %s", func(d *RigDefinition) { d.Commands[1].Cmd = "FA;" }},
		{"extra format verb", func(d *RigDefinition) { d.Commands[1].Cmd = "FA%s%d;" }},
		{"two %s verbs", func(d *RigDefinition) { d.Commands[1].Cmd = "FA%s%s;" }},
		{"value-bearing wrong verb only", func(d *RigDefinition) {
			d.Commands = append(d.Commands, Command{Name: "BAD", Cmd: "X%d;"})
		}},
		{"value_map unknown tag", func(d *RigDefinition) { d.Commands[2].ValueMap = "NOPE" }},
		{"value_map non-injective", func(d *RigDefinition) {
			d.States[0].Markers[0].ValueMappings[1].Value = "LSB" // duplicate of [0]
		}},
		{"marker zero length", func(d *RigDefinition) { d.States[0].Markers[0].Length = 0 }},
		{"marker negative index", func(d *RigDefinition) { d.States[0].Markers[0].Index = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := goodRigDef()
			tc.mod(&d)
			if err := ValidateRigDefinition(d); err == nil {
				t.Errorf("%s: expected validation error, got nil", tc.name)
			}
		})
	}
}
