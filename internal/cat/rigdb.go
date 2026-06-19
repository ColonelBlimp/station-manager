package cat

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

//go:embed rigs/*.json
var embeddedRigs embed.FS

// rigDB holds every known rig definition keyed by its ID. Populated from
// the embedded rigs/*.json files at init time; future external-dir
// registration (see RegisterExternalDir) will extend or override it.
var rigDB map[string]RigDefinition

// externalDirs records paths supplied via RegisterExternalDir. Currently
// only stored; loading is deferred (docs/v2-design/cat-serial-reuse.md §7.8).
var externalDirs []string

func init() {
	rigDB = make(map[string]RigDefinition)

	entries, err := embeddedRigs.ReadDir("rigs")
	if err != nil {
		panic(fmt.Sprintf("cat: embedded rigs directory unreadable: %v", err))
	}

	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := embeddedRigs.ReadFile("rigs/" + e.Name())
		if err != nil {
			panic(fmt.Sprintf("cat: failed to read embedded rig %s: %v", e.Name(), err))
		}
		var def RigDefinition
		if err := json.Unmarshal(data, &def); err != nil {
			panic(fmt.Sprintf("cat: failed to parse embedded rig %s: %v", e.Name(), err))
		}
		if def.ID == "" {
			panic(fmt.Sprintf("cat: embedded rig %s has empty id", e.Name()))
		}
		if _, dup := rigDB[def.ID]; dup {
			panic(fmt.Sprintf("cat: embedded rig id %q duplicated in %s", def.ID, e.Name()))
		}
		// Reject structurally-invalid rigdefs at load (review 2026-06-05 M2):
		// a template/value-map typo would otherwise become silent garbage on
		// the CAT write path. Panic matches the other load-time invariants here.
		if err := ValidateRigDefinition(def); err != nil {
			panic(fmt.Sprintf("cat: invalid embedded rig %s: %v", e.Name(), err))
		}
		rigDB[def.ID] = def
	}
}

// Lookup returns the rig definition registered under id, if any. The
// boolean is false when no rig with that id is known. Once
// RegisterExternalDir is implemented, external entries will take
// precedence over embedded builtins (lookup order: external → embedded
// → not-found).
func Lookup(id string) (RigDefinition, bool) {
	def, ok := rigDB[id]
	return def, ok
}

// CloneRigDefinition returns a deep copy of def whose slices, maps, and pointers
// share no backing storage with the original. Lookup returns a rigdef by value,
// but its Commands/States/ModeMappings (and the nested ModeSeq/Frames/Markers/
// ValueMappings) still point at the process-global rigDB entry — a caller that
// mutated them in place would corrupt every later lookup of that id (review
// 2026-06-19 L2). All current callers are read-only, so Lookup itself stays
// zero-cost (no clone on the per-command lookup path); a caller that needs to
// MODIFY a looked-up rigdef (e.g. a future operator-override merge) must clone
// first via this helper. That keeps the foot-gun opt-in rather than taxing every
// read.
func CloneRigDefinition(def RigDefinition) RigDefinition {
	clone := def // copies scalar fields + the all-scalar RigSerial/RigTiming structs

	// RigSerial.RTS/DTR are *bool tri-state pointers; copy the pointees so a
	// caller toggling them can't reach back into the shared definition.
	if def.Serial.RTS != nil {
		v := *def.Serial.RTS
		clone.Serial.RTS = &v
	}
	if def.Serial.DTR != nil {
		v := *def.Serial.DTR
		clone.Serial.DTR = &v
	}

	if def.Commands != nil {
		clone.Commands = make([]Command, len(def.Commands))
		for i, c := range def.Commands {
			c.Frames = cloneStringSlice(c.Frames)
			if c.ModeSeq != nil {
				ms := make([]ModeSequence, len(c.ModeSeq))
				for j, m := range c.ModeSeq {
					m.Frames = cloneStringSlice(m.Frames)
					ms[j] = m
				}
				c.ModeSeq = ms
			}
			clone.Commands[i] = c
		}
	}

	if def.States != nil {
		clone.States = make([]State, len(def.States))
		for i, s := range def.States {
			if s.Markers != nil {
				mk := make([]Marker, len(s.Markers))
				for j, m := range s.Markers {
					if m.ValueMappings != nil {
						vm := make([]ValueMapping, len(m.ValueMappings))
						copy(vm, m.ValueMappings) // ValueMapping is all-scalar
						m.ValueMappings = vm
					}
					mk[j] = m
				}
				s.Markers = mk
			}
			clone.States[i] = s
		}
	}

	if def.ModeMappings != nil {
		mm := make(map[string]types.ModeMapping, len(def.ModeMappings))
		for k, v := range def.ModeMappings {
			mm[k] = v // types.ModeMapping is all-scalar
		}
		clone.ModeMappings = mm
	}

	return clone
}

// cloneStringSlice returns a copy of s sharing no backing array, or nil when s
// is nil (preserving the nil/empty distinction so a round-trip is exact).
func cloneStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// List returns the ids of all known rigs, sorted alphabetically.
func List() []string {
	ids := make([]string, 0, len(rigDB))
	for id := range rigDB {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// RegisterExternalDir registers a directory of operator-supplied rig JSON
// files that extend or override the embedded database. Once the loader is
// implemented, lookup order becomes external → embedded → not-found, so
// operators can override a shipped rig by placing a file with the same id
// in the external directory.
//
// STUB: currently records the directory but does no loading. See
// docs/v2-design/cat-serial-reuse.md §7.8 for the deferred loader design.
func RegisterExternalDir(dir string) error {
	externalDirs = append(externalDirs, dir)
	return nil
}
