package main

import (
	"context"
	stderr "errors"
	"fmt"
	"io"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// resolveArchivePaths is the ONE place the daemon, `smd import` and `smd
// restore` turn the config catalogue into the QSO file to open and the global
// stores beside it (ADR 0071; W-0021 slice 2). id names a catalogue entry for
// the commands' --archive flag; empty means the active archive (or, before
// adoption, datastore.path). The commands print what they resolved so an
// operator running one against a stopped daemon can see which file it touches.
func resolveArchivePaths(cfg config.Config, id string, report io.Writer) (archive.Paths, error) {
	p, err := archive.Resolve(cfg, id)
	if err != nil {
		return archive.Paths{}, err
	}
	if report != nil {
		if p.Entry != nil {
			_, _ = fmt.Fprintf(report, "archive: %s (%s, %s) — %s\n", p.Entry.Label, p.Entry.ID, p.Entry.Ownership, p.QSO)
		} else {
			_, _ = fmt.Fprintf(report, "archive: not yet adopted — datastore.path %s\n", p.QSO)
		}
	}
	return p, nil
}

// openArchiveDatabases is the commands' shared open sequence (`smd import`,
// `smd restore`): resolve the archive, run the one-time reference split, open
// and migrate the QSO file and the station-global reference database, and
// apply the ST-6 modes — exactly what the daemon's own startup does, so a
// command can never open a different file than the daemon would. The returned
// func closes both databases; the caller defers it.
func openArchiveDatabases(cfg config.Config, cfgSvc *config.Service, archiveID string,
	dbSvc, refDbSvc *sqlite.Service, loggerSvc *logging.Service, report io.Writer) (archive.Paths, func(), error) {
	const op errors.Op = "smd.openArchiveDatabases"
	paths, err := resolveArchivePaths(cfg, archiveID, report)
	if err != nil {
		return archive.Paths{}, nil, errors.New(op).WithErr(err).WithMsg("resolve archive")
	}
	if err := verifyArchiveIdentity(paths); err != nil {
		return paths, nil, errors.New(op).WithErr(err)
	}
	// Idempotent, backup-first split of an old single-file DB — in case a command
	// runs against it before the daemon has started once. Must precede Open.
	if err := sqlite.BootstrapReferenceSplit(paths.QSO, paths.Reference, paths.Backups, loggerSvc); err != nil {
		return paths, nil, errors.New(op).WithErr(err).WithMsg("bootstrap reference split")
	}
	closeLogged := func(svc *sqlite.Service, what string) {
		if cerr := svc.Close(); cerr != nil {
			loggerSvc.ErrorWith().Err(cerr).Msg(what + " close error")
		}
	}
	dbSvc.SetMigrationSets(sqlite.MigrationSetLog)
	dbSvc.SetDatabasePath(paths.QSO)
	if err := dbSvc.Open(); err != nil {
		return paths, nil, errors.New(op).WithErr(err).WithMsg("open database")
	}
	if err := dbSvc.Migrate(); err != nil {
		closeLogged(dbSvc, "database")
		return paths, nil, errors.New(op).WithErr(err).WithMsg("run migrations")
	}
	refDbSvc.SetMigrationSets(sqlite.MigrationSetReference)
	refDbSvc.SetDatabasePath(paths.Reference)
	if err := refDbSvc.Open(); err != nil {
		closeLogged(dbSvc, "database")
		return paths, nil, errors.New(op).WithErr(err).WithMsg("open reference database")
	}
	closeBoth := func() { closeLogged(refDbSvc, "reference database"); closeLogged(dbSvc, "database") }
	if err := refDbSvc.Migrate(); err != nil {
		closeBoth()
		return paths, nil, errors.New(op).WithErr(err).WithMsg("run reference migrations")
	}
	// ST-6: a command opens/writes the same databases, so it must make them
	// owner-private too (else a permissive-umask run leaves readable QSO data
	// until a later daemon start).
	if err := sqlite.SecureDataFiles(cfgSvc.WorkingDir(), paths.Backups, loggerSvc, dbSvc.DatabaseConfig.Path, paths.Reference); err != nil {
		closeBoth()
		return paths, nil, errors.New(op).WithErr(err).WithMsg("secure database files")
	}
	return paths, closeBoth, nil
}

// verifyArchiveIdentity proves, BEFORE any split, migration or write, that the
// file a catalogue entry names is the archive it claims (review cc1078b7): a
// legacy or external entry records a path, and a path can point at any file.
// The file's own identity row is the authority. A file holding another
// archive's identity is refused naming both ids; a file with NO identity is
// refused too — under the database-first adoption order an entry, the active
// one included, exists only after the file's identity was written and read
// back, so an identity-less file behind any entry is a mis-pointed or replaced
// file. A genuinely pre-adoption install has no entry at all (Entry nil) and is
// left to the daemon's adoption.
func verifyArchiveIdentity(paths archive.Paths) error {
	// One classifier for the daemon's start, the commands' open and the
	// activation preflight (archive.VerifyIdentity): the same file gets the same
	// stable code — a missing file is "missing" on every path.
	return archive.VerifyIdentity(paths)
}

// targetLogbook is the logbook a command writes to: the --logbook value when
// given; otherwise the targeted archive's OWN default (its identity row — the
// file is the authority), or, only for the active or a not-yet-adopted archive,
// config's projection of it. A named inactive archive without a default is
// refused: the active archive's default id would mean another logbook there,
// or none.
func targetLogbook(requested int64, db *sqlite.Service, paths archive.Paths, cfg config.Config) (int64, error) {
	if requested != 0 {
		return requested, nil
	}
	identity, err := db.ArchiveIdentityWithContext(context.Background())
	switch {
	case err == nil && identity.DefaultLogbookID > 0:
		return identity.DefaultLogbookID, nil
	case err != nil && !stderr.Is(err, errors.ErrNotFound):
		return 0, err
	}
	if paths.Entry == nil || paths.Entry.ID == cfg.ActiveQsoArchiveID {
		if cfg.DefaultLogbookID > 0 {
			return cfg.DefaultLogbookID, nil
		}
		return 0, fmt.Errorf("no target logbook (config has no default_logbook_id; pass --logbook)")
	}
	return 0, fmt.Errorf("archive %s (%s) has no default logbook; pass --logbook", paths.Entry.Label, paths.Entry.ID)
}
