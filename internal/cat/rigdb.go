package cat

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
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
