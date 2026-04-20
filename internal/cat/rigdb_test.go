package cat

import (
	"testing"
)

func TestListReturnsEmbeddedRigs(t *testing.T) {
	ids := List()
	if len(ids) == 0 {
		t.Fatalf("List returned no rigs; expected at least the embedded builtins")
	}
	want := map[string]bool{
		"yaesu-ftdx10": false,
		"yaesu-ft710":  false,
	}
	for _, id := range ids {
		if _, ok := want[id]; ok {
			want[id] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("expected %q in List, got %v", id, ids)
		}
	}
}

func TestLookupExistingRig(t *testing.T) {
	def, ok := Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatalf("Lookup(yaesu-ftdx10) not found")
	}
	if def.ID != "yaesu-ftdx10" {
		t.Errorf("ID = %q, want yaesu-ftdx10", def.ID)
	}
	if def.Family != "yaesu" {
		t.Errorf("Family = %q, want yaesu", def.Family)
	}
	if def.Terminator != ";" {
		t.Errorf("Terminator = %q, want ;", def.Terminator)
	}
	if def.Serial.BaudRate != 38400 {
		t.Errorf("Serial.BaudRate = %d, want 38400", def.Serial.BaudRate)
	}
	if def.Serial.LineDelimiter != ";" {
		t.Errorf("Serial.LineDelimiter = %q, want ;", def.Serial.LineDelimiter)
	}
	if len(def.Commands) == 0 {
		t.Error("Commands is empty")
	}
	if len(def.States) == 0 {
		t.Error("States is empty")
	}
}

func TestLookupUnknownRig(t *testing.T) {
	_, ok := Lookup("does-not-exist")
	if ok {
		t.Error("Lookup of unknown id returned ok=true")
	}
}

func TestRegisterExternalDirStub(t *testing.T) {
	// Stub must accept any path and return nil without side effects the
	// caller can observe through Lookup/List.
	before := len(List())
	if err := RegisterExternalDir("/nonexistent/path/that/will/not/be/walked"); err != nil {
		t.Fatalf("RegisterExternalDir(stub) returned error: %v", err)
	}
	after := len(List())
	if before != after {
		t.Errorf("List size changed after RegisterExternalDir stub: %d -> %d", before, after)
	}
}

func TestLookupFindsFreqMarker(t *testing.T) {
	// Sanity-check the state-parser shape: FA prefix should have a single
	// 9-byte frequency marker at index 0. If the JSON schema drifts this
	// test will flag it.
	def, ok := Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal("Lookup(yaesu-ftdx10) not found")
	}
	var fa *State
	for i := range def.States {
		if def.States[i].Prefix == "FA" {
			fa = &def.States[i]
			break
		}
	}
	if fa == nil {
		t.Fatal("no FA state in rig definition")
	}
	if len(fa.Markers) != 1 {
		t.Fatalf("FA markers = %d, want 1", len(fa.Markers))
	}
	m := fa.Markers[0]
	if m.Index != 0 || m.Length != 9 {
		t.Errorf("FA marker = {Index:%d Length:%d}, want {0, 9}", m.Index, m.Length)
	}
	if m.Tag == "" {
		t.Error("FA marker Tag is empty")
	}
}
