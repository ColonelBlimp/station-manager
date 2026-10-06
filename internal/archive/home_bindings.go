package archive

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// HomeFile is the adopted Home archive's file: the catalogue's legacy entry,
// or datastore.path while nothing has been adopted. Home is the one archive
// whose bindings were seeded from config.json (ADR 0082 part 4), so it is the
// archive the v5 shape can be rebuilt from (W-0021 5C).
func HomeFile(cfg config.Config) (string, *types.QsoArchiveConfig, error) {
	if len(cfg.QsoArchives) == 0 {
		return cfg.Datastore.Path, nil, nil
	}
	for i := range cfg.QsoArchives {
		if e := cfg.QsoArchives[i]; e.Ownership == types.QsoArchiveOwnershipLegacy {
			return PathFor(cfg, e), &e, nil
		}
	}
	return "", nil, fmt.Errorf("the catalogue lists %d archive(s) but no adopted Home archive", len(cfg.QsoArchives))
}

// ReadHomeBindings reads Home's bindings from its CLOSED file, read-only, for
// the offline commands, and confirms the file IS Home by its archive identity:
// a catalogue entry names a path, and a path can hold another file.
func ReadHomeBindings(cfg config.Config) (sqlite.PeekedBindings, error) {
	path, entry, err := HomeFile(cfg)
	if err != nil {
		return sqlite.PeekedBindings{}, err
	}
	peek, err := sqlite.PeekDestinationBindings(path)
	if err != nil {
		return sqlite.PeekedBindings{}, fmt.Errorf("read Home's archive file %s: %w", path, err)
	}
	if entry != nil && (!peek.HasIdentity || peek.Identity.ArchiveUUID != entry.ID) {
		return sqlite.PeekedBindings{}, fmt.Errorf("the file at %s is not the Home archive %q by identity", path, entry.Label)
	}
	return peek, nil
}
