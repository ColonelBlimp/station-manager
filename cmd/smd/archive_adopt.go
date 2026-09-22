package main

import (
	"context"
	stderr "errors"
	"path/filepath"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// legacyArchiveLabel names the adopted installation in the catalogue; the
// operator may rename it later (labels never rename files).
const legacyArchiveLabel = "Home"

// adoptArchive registers the open QSO file as the station's archive (ADR 0071,
// W-0021 slice 1; ruling (a): the existing file is adopted IN PLACE with
// ownership `legacy`). Database-first and idempotent, in this order:
//
//  1. the file's identity is ensured — archive UUID (minted only if absent),
//     default logbook pointer, a UUID on every logbook row — in one transaction
//     and read back;
//  2. the catalogue is reconciled against THAT uuid: an empty catalogue gains
//     the legacy entry and the active selector; an entry with the same uuid is
//     left as is (its recorded path refreshed to the canonical one if the file
//     moved); an ACTIVE entry naming a different uuid fails closed — the wrong
//     file sits at a registered path, and starting would serve it;
//  3. default_logbook_id is projected: the file's value wins when it names a
//     row, config's value is written into a file that has none.
//
// Runs after ensureDefaultLogbook, so config's default names a real row by the
// time the file learns it. A catalogue persist failure is logged and the daemon
// continues on the in-memory catalogue: the identity is already in the file, so
// the next start with a writable config registers the same uuid, never a new one.
func adoptArchive(ctx context.Context, db *sqlite.Service, cfgSvc *config.Service, logger *logging.Service) error {
	const op errors.Op = "smd.adoptArchive"
	snap := cfgSvc.Snapshot()

	identity, err := db.EnsureArchiveIdentityWithContext(ctx, snap.DefaultLogbookID)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("ensure archive identity")
	}

	canonical, err := canonicalPath(db.DatabaseConfig.Path)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve datastore path")
	}

	if snap.ActiveQsoArchiveID != "" && snap.ActiveQsoArchiveID != identity.ArchiveUUID {
		return errors.New(op).WithMsgf("config.json names active archive %s but the file at %s holds archive %s; "+
			"refusing to start rather than serve the wrong file (fix the catalogue or the datastore path)",
			snap.ActiveQsoArchiveID, canonical, identity.ArchiveUUID)
	}

	changed := false
	dur, err := cfgSvc.UpdateInMemoryThenPersist(func(c *config.Config) error {
		if e := c.QsoArchiveByID(identity.ArchiveUUID); e == nil {
			c.QsoArchives = append(c.QsoArchives, types.QsoArchiveConfig{
				ID: identity.ArchiveUUID, Label: legacyArchiveLabel,
				Ownership: types.QsoArchiveOwnershipLegacy, Path: canonical,
			})
			changed = true
		} else if e.Ownership != types.QsoArchiveOwnershipManaged && e.Path != canonical {
			e.Path = canonical
			changed = true
		}
		if c.ActiveQsoArchiveID != identity.ArchiveUUID {
			c.ActiveQsoArchiveID = identity.ArchiveUUID
			changed = true
		}
		if identity.DefaultLogbookID != 0 && c.DefaultLogbookID != identity.DefaultLogbookID {
			c.DefaultLogbookID = identity.DefaultLogbookID
			changed = true
		}
		return nil
	})
	if err != nil {
		logger.WarnWith().Err(err).Str("archive_id", identity.ArchiveUUID).
			Msg("startup: archive adopted in memory but the catalogue could not be persisted to config.json; the identity is in the file and the next start will register it")
	} else if changed {
		ev := logger.InfoWith().Str("archive_id", identity.ArchiveUUID).Str("ownership", string(types.QsoArchiveOwnershipLegacy)).Str("path", canonical)
		if dur == config.DurabilityUncertain {
			ev = ev.Bool("durability_uncertain", true)
		}
		ev.Msg("startup: archive catalogue written (existing installation adopted in place)")
	}

	// The file learns config's default when it has none of its own.
	after := cfgSvc.Snapshot()
	if identity.DefaultLogbookID == 0 && after.DefaultLogbookID > 0 {
		if err := db.SetArchiveDefaultLogbookWithContext(ctx, after.DefaultLogbookID); err != nil {
			if !stderr.Is(err, errors.ErrNotFound) {
				logger.WarnWith().Err(err).Int64("default_logbook_id", after.DefaultLogbookID).
					Msg("startup: could not record the default logbook in the archive")
			}
		}
	}
	return nil
}

// canonicalPath is the absolute, symlink-resolved form the catalogue records,
// so the same file is never registered twice under two spellings.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real, nil
	}
	return abs, nil // a not-yet-created file has no symlinks to resolve
}
