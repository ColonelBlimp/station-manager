package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0071 / W-0021 slice 2: ONE resolver turns the config catalogue into the
// paths the daemon, `smd import` and `smd restore` open. The QSO file comes
// from the active (or a named) catalogue entry; reference.db and evidence.db
// are station-global under <data_dir>/db and never follow the QSO file —
// except a file an old external layout already created beside the QSO file,
// which stays frozen where it was observed.

const idA = "019fd5c5-efcc-7193-be4f-1fee532ee315"
const idB = "019fd5c5-efcc-7193-be4f-1fee532ee316"

func baseCfg(t *testing.T) config.Config {
	t.Helper()
	return config.DefaultConfig(t.TempDir())
}

func TestResolve_BeforeAdoptionUsesDatastorePathAndGlobalStores(t *testing.T) {
	cfg := baseCfg(t)
	p, err := Resolve(cfg, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.QSO != cfg.Datastore.Path || p.Entry != nil {
		t.Fatalf("pre-adoption QSO = %q entry %v; want datastore.path %q and no entry", p.QSO, p.Entry, cfg.Datastore.Path)
	}
	global := filepath.Join(cfg.DataDir, "db")
	if p.Reference != filepath.Join(global, "reference.db") || p.Evidence != filepath.Join(global, "evidence.db") || p.Backups != filepath.Join(global, "backups") {
		t.Fatalf("global stores = %q %q %q; want under %s", p.Reference, p.Evidence, p.Backups, global)
	}
}

func TestResolve_ActiveLegacyAndManagedEntries(t *testing.T) {
	cfg := baseCfg(t)
	legacyPath := filepath.Join(cfg.DataDir, "db", "station-manager.db")
	cfg.QsoArchives = []types.QsoArchiveConfig{
		{ID: idA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: legacyPath},
		{ID: idB, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged},
	}
	cfg.ActiveQsoArchiveID = idA

	p, err := Resolve(cfg, "")
	if err != nil {
		t.Fatalf("resolve active: %v", err)
	}
	if p.QSO != legacyPath || p.Entry == nil || p.Entry.ID != idA {
		t.Fatalf("active = %q entry %+v; want the legacy path", p.QSO, p.Entry)
	}
	// Backups are archive-scoped once an archive exists (equal basenames cannot collide).
	if want := filepath.Join(cfg.DataDir, "db", "backups", idA); p.Backups != want {
		t.Fatalf("backups = %q, want %q", p.Backups, want)
	}
	pb, err := Resolve(cfg, idB)
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}
	if want := filepath.Join(ManagedDir(cfg), idB+".db"); pb.QSO != want {
		t.Fatalf("managed path = %q, want %q (derived from the id, never stored)", pb.QSO, want)
	}
	// Global stores do not follow the QSO file.
	if pb.Reference != p.Reference || pb.Evidence != p.Evidence {
		t.Fatalf("global stores moved with the archive: %q vs %q", pb.Reference, p.Reference)
	}
}

func TestResolve_Refusals(t *testing.T) {
	cfg := baseCfg(t)
	cfg.QsoArchives = []types.QsoArchiveConfig{{ID: idA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/data/db/x.db"}}
	if _, err := Resolve(cfg, idB); err == nil || !strings.Contains(err.Error(), idB) {
		t.Fatalf("unknown id accepted or unnamed: %v", err)
	}
	cfg.ActiveQsoArchiveID = idB
	if _, err := Resolve(cfg, ""); err == nil || !strings.Contains(err.Error(), idB) {
		t.Fatalf("active id outside the catalogue accepted or unnamed: %v", err)
	}
	cfg.ActiveQsoArchiveID = ""
	if _, err := Resolve(cfg, ""); err == nil {
		t.Fatal("a catalogue with entries but no active archive resolved to datastore.path; that is the pre-adoption fallback only")
	}
}

// An old external layout put reference.db / evidence.db beside an external QSO
// file. Such a file is frozen where it was observed; a global file, when it also
// exists, wins; a fresh layout derives from the working directory.
func TestResolve_FreezesObservedExternalGlobalStores(t *testing.T) {
	cfg := baseCfg(t)
	external := t.TempDir()
	cfg.Datastore.Path = filepath.Join(external, "log.db")
	touch := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	touch(filepath.Join(external, "reference.db"))
	p, err := Resolve(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Reference != filepath.Join(external, "reference.db") {
		t.Fatalf("reference = %q, want the observed external file frozen in place", p.Reference)
	}
	if want := filepath.Join(cfg.DataDir, "db", "evidence.db"); p.Evidence != want {
		t.Fatalf("evidence = %q, want the global %q (none observed beside the QSO file)", p.Evidence, want)
	}
	// The global file wins once it exists.
	touch(filepath.Join(cfg.DataDir, "db", "reference.db"))
	p2, _ := Resolve(cfg, "")
	if p2.Reference != filepath.Join(cfg.DataDir, "db", "reference.db") {
		t.Fatalf("reference = %q, want the global file once present", p2.Reference)
	}
}

// AC 5 (global-store continuity, review of 2A): the freeze rule keys on the
// STATION's pre-archive layout — the directory of datastore.path, the only
// place an old external file could have been created — never on the selected
// archive. Switching A → B → A must resolve the same evidence.db every time.
func TestResolve_FrozenGlobalStoreIsStationWideNotPerArchive(t *testing.T) {
	cfg := baseCfg(t)
	external := t.TempDir()
	cfg.Datastore.Path = filepath.Join(external, "log.db")
	if err := os.WriteFile(filepath.Join(external, "evidence.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.QsoArchives = []types.QsoArchiveConfig{
		{ID: idA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: cfg.Datastore.Path},
		{ID: idB, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged},
	}
	frozen := filepath.Join(external, "evidence.db")
	for i, id := range []string{idA, idB, idA} {
		p, err := Resolve(cfg, id)
		if err != nil {
			t.Fatal(err)
		}
		if p.Evidence != frozen {
			t.Fatalf("step %d (%s): evidence = %q, want the station's frozen %q", i, id, p.Evidence, frozen)
		}
	}
}
