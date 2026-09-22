// Package archive resolves the station's QSO archives (ADR 0071): which file
// the daemon opens for the active archive, where a named archive lives, and
// where the station-global stores are. Every entry point that opens a QSO
// database — the daemon, `smd import`, `smd restore` — resolves through here,
// so none of them can fall back to a different rule.
package archive

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const (
	managedDirName      = "qso-archives"
	ReferenceDBFilename = "reference.db"
	EvidenceDBFilename  = "evidence.db"
	backupsDirName      = "backups"
)

// Paths is one resolution: the QSO file to open and the global stores beside it.
type Paths struct {
	// QSO is the archive file. Entry is its catalogue entry, nil before adoption
	// (an existing installation that no identity-aware daemon has started yet),
	// when QSO is simply datastore.path.
	QSO   string
	Entry *types.QsoArchiveConfig
	// Reference and Evidence are station-global and do not switch with the
	// archive: <data_dir>/db/<name>, or — an old external layout — a file already
	// observed beside datastore.path, frozen there (ADR 0071 "upgrade without
	// movement") whichever archive is selected.
	Reference string
	Evidence  string
	// Backups holds pre-split QSO backups: archive-scoped once an archive exists,
	// so equal basenames from two archives cannot collide.
	Backups string
}

// GlobalDir is where station-global databases live: <data_dir>/db.
func GlobalDir(cfg config.Config) string { return filepath.Join(cfg.DataDir, "db") }

// ManagedDir holds daemon-created archives: <data_dir>/db/qso-archives.
func ManagedDir(cfg config.Config) string { return filepath.Join(GlobalDir(cfg), managedDirName) }

// PathFor is an entry's file: derived from the id for a managed archive (never
// stored), the recorded absolute path for a legacy or external one.
func PathFor(cfg config.Config, e types.QsoArchiveConfig) string {
	if e.Ownership == types.QsoArchiveOwnershipManaged {
		return filepath.Join(ManagedDir(cfg), e.ID+".db")
	}
	return e.Path
}

// Resolve returns the paths for archive `id`, or for the ACTIVE archive when id
// is empty. Before adoption (empty catalogue, no active id) the QSO file is
// datastore.path; a catalogue with entries but no active archive, an active id
// outside the catalogue, or an unknown id are refused by name — the daemon must
// never guess which file to serve.
func Resolve(cfg config.Config, id string) (Paths, error) {
	var entry *types.QsoArchiveConfig
	switch {
	case id != "":
		if entry = cfg.QsoArchiveByID(id); entry == nil {
			return Paths{}, fmt.Errorf("archive %s is not in the catalogue", id)
		}
	case cfg.ActiveQsoArchiveID != "":
		if entry = cfg.QsoArchiveByID(cfg.ActiveQsoArchiveID); entry == nil {
			return Paths{}, fmt.Errorf("active archive %s is not in the catalogue", cfg.ActiveQsoArchiveID)
		}
	case len(cfg.QsoArchives) > 0:
		return Paths{}, fmt.Errorf("the catalogue lists %d archive(s) but none is active", len(cfg.QsoArchives))
	}

	p := Paths{Entry: entry}
	if entry != nil {
		e := *entry
		p.Entry = &e
		p.QSO = PathFor(cfg, e)
	} else {
		p.QSO = cfg.Datastore.Path
	}

	// The freeze rule keys on the STATION's pre-archive layout — the directory
	// of datastore.path, the only place an old external layout could have put a
	// global file — never on the selected archive, so A → B → A resolves the same
	// evidence.db every time (ADR 0071 AC 5).
	global := GlobalDir(cfg)
	legacyDir := filepath.Dir(cfg.Datastore.Path)
	p.Reference = globalStore(global, legacyDir, ReferenceDBFilename)
	p.Evidence = globalStore(global, legacyDir, EvidenceDBFilename)
	p.Backups = filepath.Join(global, backupsDirName)
	if entry != nil {
		p.Backups = filepath.Join(p.Backups, entry.ID)
	}
	return p, nil
}

// globalStore applies the freeze rule: the global location wins whenever its
// file exists or nothing was ever created beside the QSO file; a file that an
// old external layout already put beside the QSO file stays where it is.
func globalStore(globalDir, legacyDir, name string) string {
	global := filepath.Join(globalDir, name)
	if legacyDir == globalDir {
		return global
	}
	if _, err := os.Stat(global); err == nil {
		return global
	}
	legacy := filepath.Join(legacyDir, name)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return global
}
