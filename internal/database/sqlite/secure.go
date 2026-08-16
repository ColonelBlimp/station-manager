package sqlite

import (
	"os"
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
// empty paths are skipped. backupDir (when non-empty) has its directory + every *.db it
// holds tightened too — pre-split backups are full QSO history and are visited here rather
// than only at creation. workDir is the canonical resolved working directory. Called by
// EVERY flow that opens these databases (daemon startup, import, restore), not just normal
// startup, so no command can leave a readable database behind.
func SecureDataFiles(workDir, backupDir string, log *logging.Service, dbPaths ...string) error {
	const op errors.Op = "sqlite.SecureDataFiles"

	secure := func(path string, want os.FileMode, kind string) error {
		warn, err := fsperm.SecureApplicationPath(workDir, path, want)
		if err != nil {
			return errors.New(op).WithErr(err).WithMsgf("securing %s", kind)
		}
		if warn != "" {
			log.WarnWith().Str("path", path).Msg("ST-6: " + warn)
		}
		return nil
	}

	securedDirs := make(map[string]struct{})
	for _, db := range dbPaths {
		if db == "" || strings.Contains(db, ":memory:") {
			continue
		}
		dir := filepath.Dir(db)
		if _, done := securedDirs[dir]; !done {
			securedDirs[dir] = struct{}{}
			if err := secure(dir, 0o700, "database directory"); err != nil {
				return err
			}
		}
		// The main DB plus its WAL/SHM sidecars (present only in WAL mode after a write).
		for _, f := range []string{db, db + "-wal", db + "-shm"} {
			if err := secure(f, 0o600, "database file"); err != nil {
				return err
			}
		}
	}

	if backupDir != "" {
		if err := secure(backupDir, 0o700, "backup directory"); err != nil {
			return err
		}
		matches, _ := filepath.Glob(filepath.Join(backupDir, "*.db"))
		for _, f := range matches {
			if err := secure(f, 0o600, "backup database"); err != nil {
				return err
			}
		}
	}
	return nil
}
