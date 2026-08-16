package sqlite

import (
	"path/filepath"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/fsperm"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// SecureDataFiles enforces the ST-6 private-state policy on the given database files (the
// log DB and the reference DB): each file, its parent directory, and any EXISTING -wal/-shm
// sidecars are tightened to owner-only when application-owned (0600 files, 0700 dirs). A
// group/world-accessible operator-supplied path OUTSIDE the working directory is never
// mutated — it may be a deliberately-shared location — but earns a high-signal warning.
//
// A failure to establish the required mode on an application-owned file is FATAL: the
// operator's QSO data must not be served from a group/other-readable file. In-memory and
// empty paths are skipped. workDir is the canonical resolved working directory.
func SecureDataFiles(workDir string, log *logging.Service, dbPaths ...string) error {
	const op errors.Op = "sqlite.SecureDataFiles"

	securedDirs := make(map[string]struct{})
	for _, db := range dbPaths {
		if db == "" || strings.Contains(db, ":memory:") {
			continue
		}
		dir := filepath.Dir(db)
		if _, done := securedDirs[dir]; !done {
			securedDirs[dir] = struct{}{}
			if warn, err := fsperm.SecureApplicationPath(workDir, dir, 0o700); err != nil {
				return errors.New(op).WithErr(err).WithMsg("securing database directory")
			} else if warn != "" {
				log.WarnWith().Str("path", dir).Msg("ST-6: " + warn)
			}
		}
		// The main DB plus its WAL/SHM sidecars (present only in WAL mode after a write).
		for _, f := range []string{db, db + "-wal", db + "-shm"} {
			if warn, err := fsperm.SecureApplicationPath(workDir, f, 0o600); err != nil {
				return errors.New(op).WithErr(err).WithMsg("securing database file")
			} else if warn != "" {
				log.WarnWith().Str("path", f).Msg("ST-6: " + warn)
			}
		}
	}
	return nil
}
