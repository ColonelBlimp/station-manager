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
	// One update makes the activation final (ADR 0100 §4; W-0029 part 3, A1):
	// the selector and its identity removed, the candidate active, and the
	// activation's identity recorded as the one IN EFFECT. No reader sees some
	// of these without the others. The archive now served is compared with the
	// activation in effect, so that it is known to have been arrived at; the
	// same activation processed again (this write lost, the start repeated)
	// carries the same identity and is recognised.
	dur, err := d.cfgSvc.Update(func(c *config.Config) error {
		if e := c.QsoArchiveByID(id); e != nil {
			e.LastActivationError = ""
		}
		endActivation(c, id)
		c.ActiveQsoArchiveID = id
		// As before this change: nothing stays pending once a candidate is active,
		// whatever the selector named.
		c.PendingQsoArchiveID, c.PendingQsoArchiveActivationID = "", ""
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

// startOperatingSession is the `operating-session` lifecycle node (ADR 0100
// §4; W-0029 part 3, A8): the session's step of every daemon start. It tells the
// archive's database which activation this process serves it under, then runs
// the two tests that close a session nobody asked to close, before any app is
// served:
//
//   - the ARRIVAL: the file records the activation it was last arrived at
//     under. If that is not the activation in effect, the archive has been
//     arrived at, by an activation applied or by a fallback, and a session
//     still open in it is closed with archive_switched;
//   - the DEFAULT-EPOCH test: a session of an earlier default is closed.
//
// The activation in effect is read HERE, after archive-promote and after a
// failed candidate's record: by then it names the activation this start
// processed, whichever way it went, and otherwise the one already in effect.
//
// A failure is logged and the start goes on. Nothing is lost by that: the
// admission's own step runs the same tests and does not admit an app while the
// file cannot be read or written, so an arrival that is owed is never skipped
// by an admission that succeeds. It is not a reason to refuse to serve: the
// daemon has no better archive to offer.
//
// AN ACCEPTED LIMIT (raised by the clean-room review of b0c636ba; accepted by
// the operator, 2026-11-22).
// The arrival is decided from two records, config.json's activation in effect
// and the file's, and holds through any single interruption. It is not proof
// against a chain of lost writes. One such chain: a fallback stamps the archive
// it fell back to with the new activation, durably, while BOTH of config.json's
// writes for that activation (the request's selector and the fallback's record)
// were renamed but not yet confirmed on disk, and the power is then cut.
// config.json comes back as it was before the request, the file's stamp differs
// from the activation in effect, and this step closes the session opened after
// the fallback as archive_switched on a start that processed no switch. The cost
// is one session closed early, shown as closed by a switch: no contact is lost
// or moved, and the next app start opens a new session.
//
// The operator's ruling of 2026-11-21 (question 4, "a") accepts chains beyond a
// single interruption and names one, Home to Contest to Home, whose worst
// result is a session carried on. This chain is of the same kind with a
// different result, and was put to the operator on its own and accepted (the
// dossier, W-0029, rulings, 2026-11-22). What would close it was among the
// choices not taken on question 4: admitting no app after a switch attempt
// until its outcome is durably recorded, or a third durable record of the
// archive last served.
func (d *daemon) startOperatingSession(ctx context.Context) error {
	if d.db == nil {
		return nil
	}
	activation := d.cfgSvc.Snapshot().QsoArchiveActivationID
	d.db.SetServedActivation(activation)
	st, outcome, err := d.db.CheckOperatingSessionAtStartWithContext(ctx, time.Now())
	switch {
	case stderr.Is(err, errors.ErrNotFound):
		return nil // a file with no archive identity has no session to test
	case err != nil:
		d.logger.WarnWith().Err(err).Msg("startup: the operating session could not be tested; the app's admission will test it")
	case outcome == sqlite.SessionApplied:
		d.logger.InfoWith().Int64("generation", st.Generation).
			Msg("startup: a session left open by an earlier visit or an earlier default was closed")
	}
	return nil
}

// endActivation is what ends the pending activation of candidate, in the
// caller's ONE update of config.json: its identity becomes the activation in
// effect, and the selector and the identity are removed together. With nothing
// pending for that candidate it changes nothing: a start that is not processing
// an activation leaves the activation in effect alone.
func endActivation(c *config.Config, candidate string) {
	if c.PendingQsoArchiveID != candidate {
		return
	}
	c.QsoArchiveActivationID = c.PendingActivationIdentity()
	c.PendingQsoArchiveID = ""
	c.PendingQsoArchiveActivationID = ""
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
		// The failed activation is still the one this start processed: the
		// archive fallen back to is served under ITS identity, and a session
		// open there is closed as an arrival closes it (ADR 0100 §4). Memory
		// first, as the rest of this update: when the write fails the selector
		// and its identity are still on disk, and the next start processes the
		// same activation, which the file then already records.
		endActivation(c, candidate.ID)
		return nil
	})
	failure := fmt.Errorf("archive: activation of candidate %s (%q) failed: %w", candidate.ID, candidate.Label, cause)
	if err != nil {
		failure = fmt.Errorf("%w (and recording it in config.json failed: %v)", failure, err)
	}
	return failure
}
