package main

import (
	"context"
	stderr "errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
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
func adoptArchive(ctx context.Context, db *sqlite.Service, cfgSvc *config.Service, logger *logging.Service, paths archive.Paths) error {
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

	// The EFFECTIVE selection (2D): the entry this start opened — the active
	// archive, or the pending candidate being proved. The file must hold that
	// archive; a pre-adoption install (no entry) is compared with the active id.
	expected := snap.ActiveQsoArchiveID
	if paths.Entry != nil {
		expected = paths.Entry.ID
	}
	if expected != "" && expected != identity.ArchiveUUID {
		return errors.New(op).WithMsgf("config.json selects archive %s but the file at %s holds archive %s; "+
			"refusing to start rather than serve the wrong file (fix the catalogue or the datastore path)",
			expected, canonical, identity.ArchiveUUID)
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
		// The active selector is set only for a pre-adoption install; a pending
		// candidate is promoted by the archive-promote node once the graph is up.
		if paths.Entry == nil && c.ActiveQsoArchiveID != identity.ArchiveUUID {
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

// startArchivePromote is the `archive-promote` lifecycle node (ADR 0071, 2D):
// when this start opened a PENDING candidate and every database-dependent node
// came up on it, promote it — active = candidate, pending cleared, the entry's
// last failure cleared — with config.Service.Update (file first). A write
// failure fails this node, which fails the generation, which run() answers by
// rolling back and starting again on the last-known-good archive. It runs
// before http, so no request sees a daemon serving one archive while the
// catalogue names another. A no-op when nothing is pending.
func (d *daemon) startArchivePromote(context.Context) error {
	const op errors.Op = "smd.startArchivePromote"
	if !d.paths.Candidate || d.paths.Entry == nil {
		return nil
	}
	id := d.paths.Entry.ID
	dur, err := d.cfgSvc.Update(func(c *config.Config) error {
		if e := c.QsoArchiveByID(id); e != nil {
			e.LastActivationError = ""
		}
		c.ActiveQsoArchiveID = id
		c.PendingQsoArchiveID = ""
		return nil
	})
	if err != nil {
		// Classified so the fallback generation records the stable code.
		return &archive.ActivationFailure{Code: archive.FailPromotionPersist, Err: errors.New(op).WithErr(err).WithMsgf("promote archive %s to active", id)}
	}
	d.cfg = d.cfgSvc.Snapshot()
	d.paths.Candidate = false // from here on this is the active archive
	// The success event — only now, after the promotion write, and into this
	// (newly active) archive's own file. Best-effort: the recorder's enqueue
	// never blocks or fails the node.
	if d.events != nil {
		d.events.ArchiveActivated(id, d.paths.Entry.Label, time.Now())
	}
	ev := d.logger.InfoWith().Str("archive_id", id).Str("label", d.paths.Entry.Label).Str("path", d.paths.QSO)
	if dur == config.DurabilityUncertain {
		ev = ev.Bool("durability_uncertain", true)
	}
	ev.Msg("archive: candidate activated (pending → active)")
	return nil
}

// activationFailure names the pending candidate a start could not activate;
// the last-known-good generation logs it once its logger is open.
type activationFailure struct {
	Candidate types.QsoArchiveConfig
	Err       error
	// At is when the fallback was decided — the occurrence time the Station
	// Event carries once the last-known-good generation's events node is up.
	At time.Time
}

// recordActivationFailure is startGenerations' answer to a candidate that did
// not come up: the failure goes on the candidate's entry and pending is
// cleared, memory first — the daemon must serve the last-known-good archive
// this session even when config.json cannot be written — so the next start is
// an ordinary one. It returns the failure to report, naming the candidate and
// a persist failure when there was one.
func recordActivationFailure(cfgSvc *config.Service, candidate types.QsoArchiveConfig, cause error) error {
	_, err := cfgSvc.UpdateInMemoryThenPersist(func(c *config.Config) error {
		if e := c.QsoArchiveByID(candidate.ID); e != nil {
			e.LastActivationError = archive.FailureCode(cause) // the code only; the chain is logged below
		}
		if c.PendingQsoArchiveID == candidate.ID {
			c.PendingQsoArchiveID = ""
		}
		return nil
	})
	failure := fmt.Errorf("archive: activation of candidate %s (%q) failed: %w", candidate.ID, candidate.Label, cause)
	if err != nil {
		failure = fmt.Errorf("%w (and recording it in config.json failed: %v)", failure, err)
	}
	return failure
}
