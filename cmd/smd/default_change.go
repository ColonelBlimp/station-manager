package main

import (
	"context"
	stderr "errors"
	"strings"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The default-change states a start can leave (ADR 0097 §1).
const (
	defaultChangeEffective          = "effective"
	defaultChangeCleanupPending     = "applied_cleanup_pending"
	defaultChangeStillPending       = "not_applied_intent_pending"
	defaultChangeAbandoned          = "not_applied_abandoned"
	defaultChangeRemovalUnconfirmed = "withdrawal_unconfirmed"
)

// Why a start abandoned a pending change (recorded on its tombstone).
const (
	defaultChangeWrongArchive     = "default_change_wrong_archive"
	defaultChangeTargetMissing    = "default_change_target_missing"
	defaultChangeTargetIneligible = "default_change_target_ineligible"
)

// defaultPointer is what the reconciliation needs from the open archive: its
// identity and default pointer, a durable write of that pointer, and the live
// logbooks.
type defaultPointer interface {
	ArchiveIdentityWithContext(ctx context.Context) (sqlite.ArchiveIdentity, error)
	SetArchiveDefaultLogbookDurablyWithContext(ctx context.Context, logbookID int64) error
	FetchAllLogbooksWithContext(ctx context.Context) ([]types.Logbook, error)
}

// newDefaultPointer is the reconciliation's store; tests replace it to inject
// faults.
var newDefaultPointer = func(db *sqlite.Service) defaultPointer { return db }

// defaultChangeReport is what a start found and did with a default change.
// Unconfirmed is derived from the live config when read (defaultChangeState).
type defaultChangeReport struct {
	State       string // "" when there is nothing to report
	RequestID   string
	LogbookUUID string
	FailureCode string
	// Unconfirmed: config still holds the record — a selector, or a tombstone
	// whose removal is not yet confirmed durable — so it blocks activation and
	// protects its target.
	Unconfirmed bool
	// settled is the state once config no longer holds the record: a later
	// durable write confirmed the removal this start could not.
	settled string
}

// defaultChangeState holds the generation's report and derives, on each read,
// whether config still holds the record: a confirming save made later (any
// durable config write holding the tombstone) settles it, so no flag taken at
// start can go stale.
type defaultChangeState struct {
	mu     sync.Mutex
	cfgSvc *config.Service
	report defaultChangeReport
}

func (s *defaultChangeState) set(cfgSvc *config.Service, r defaultChangeReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfgSvc, s.report = cfgSvc, r
}

func (s *defaultChangeState) get() defaultChangeReport {
	s.mu.Lock()
	r, cfgSvc := s.report, s.cfgSvc
	s.mu.Unlock()
	if cfgSvc == nil || r.RequestID == "" {
		return defaultChangeReport{State: r.State, FailureCode: r.FailureCode}
	}
	if c := cfgSvc.Snapshot().DefaultLogbookChange; c != nil && c.RequestID == r.RequestID {
		r.Unconfirmed = true
		return r
	}
	r.State, r.Unconfirmed = r.settled, false
	return r
}

// reconcileDefaultChange settles config's default-change record at start,
// before anything reads the default (ADR 0097 §1):
//   - a tombstone, found on disk, is unconfirmed for this process: config is
//     rewritten durably, which confirms it (config.Service drops it);
//   - a pending selector is applied by a durable write of the archive's
//     pointer, read back; abandoned when its target is refused; or kept when
//     the write failed with the old default verified. The start fails rather
//     than guess whenever the pointer cannot be proven: unreadable, unexpected
//     (NULL, not live, neither the old default nor the target), or visible on
//     the target without a durable commit that wrote it.
//
// The effective default then follows the verified file, in memory, even when
// config cannot be persisted.
func reconcileDefaultChange(ctx context.Context, p defaultPointer, cfgSvc *config.Service, logger *logging.Service) (defaultChangeReport, error) {
	rec := cfgSvc.Snapshot().DefaultLogbookChange
	if rec == nil {
		return defaultChangeReport{}, nil
	}
	if rec.Removed != "" {
		return reconfirmTombstone(cfgSvc, logger, *rec), nil
	}
	return applyDefaultChange(ctx, p, cfgSvc, logger, *rec)
}

// reconfirmTombstone rewrites config durably so the found tombstone is
// confirmed; an uncertain or failed rewrite leaves it unconfirmed.
func reconfirmTombstone(cfgSvc *config.Service, logger *logging.Service, rec config.DefaultLogbookChange) defaultChangeReport {
	r := defaultChangeReport{State: defaultChangeRemovalUnconfirmed, RequestID: rec.RequestID, LogbookUUID: rec.LogbookUUID, FailureCode: rec.FailureCode}
	if rec.Removed == config.RemovedApplied {
		r.State, r.settled = defaultChangeCleanupPending, defaultChangeEffective
	}
	if _, dur, err := cfgSvc.UpdateIfChanged(true, func(*config.Config) error { return nil }); err != nil || dur != config.Durable {
		logger.WarnWith().Err(err).Str("request_id", rec.RequestID).Str("removed", rec.Removed).
			Msg("startup: a default logbook change's removal could not be confirmed on disk; it stays unconfirmed")
	}
	return r
}

func applyDefaultChange(ctx context.Context, p defaultPointer, cfgSvc *config.Service, logger *logging.Service, rec config.DefaultLogbookChange) (defaultChangeReport, error) {
	const op errors.Op = "smd.applyDefaultChange"
	r := defaultChangeReport{RequestID: rec.RequestID, LogbookUUID: rec.LogbookUUID}
	identity, err := p.ArchiveIdentityWithContext(ctx)
	switch {
	case stderr.Is(err, errors.ErrNotFound):
		return abandonDefaultChange(cfgSvc, logger, rec, defaultChangeWrongArchive), nil
	case err != nil:
		return r, errors.New(op).WithErr(err).WithMsg("read the archive's default pointer")
	case identity.ArchiveUUID != rec.ArchiveID:
		return abandonDefaultChange(cfgSvc, logger, rec, defaultChangeWrongArchive), nil
	}
	logbooks, err := p.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return r, errors.New(op).WithErr(err).WithMsg("list the live logbooks")
	}
	byID := make(map[int64]types.Logbook, len(logbooks))
	var target types.Logbook
	for _, lb := range logbooks {
		byID[lb.ID] = lb
		if lb.UUID != "" && lb.UUID == rec.LogbookUUID {
			target = lb
		}
	}
	current, live := byID[identity.DefaultLogbookID]
	if identity.DefaultLogbookID == 0 || !live {
		return r, errors.New(op).WithMsgf("the archive's default pointer (%d) names no live logbook while a default change is pending", identity.DefaultLogbookID)
	}

	// Crash recovery: the file already names the target, visibly but not
	// provably durably. A durable rewrite establishes it.
	if target.ID != 0 && current.ID == target.ID {
		if err := commitPointer(ctx, p, target.ID); err != nil {
			return r, errors.New(op).WithErr(err).WithMsg("re-establish the default pointer durably")
		}
		return projectApplied(cfgSvc, logger, rec, target.ID), nil
	}

	switch {
	case target.ID == 0:
		return abandonDefaultChange(cfgSvc, logger, rec, defaultChangeTargetMissing), nil
	case !strings.EqualFold(strings.TrimSpace(target.Callsign), strings.TrimSpace(current.Callsign)):
		return abandonDefaultChange(cfgSvc, logger, rec, defaultChangeTargetIneligible), nil
	}

	writeErr := p.SetArchiveDefaultLogbookDurablyWithContext(ctx, target.ID)
	after, err := p.ArchiveIdentityWithContext(ctx)
	if err != nil {
		return r, errors.New(op).WithErr(err).WithMsg("read back the default pointer")
	}
	switch {
	case after.DefaultLogbookID == target.ID && writeErr == nil:
		return projectApplied(cfgSvc, logger, rec, target.ID), nil
	case after.DefaultLogbookID == target.ID:
		// Visible, but its commit was not reported: establish durability.
		if err := commitPointer(ctx, p, target.ID); err != nil {
			return r, errors.New(op).WithErr(err).WithMsg("establish the default pointer durably")
		}
		return projectApplied(cfgSvc, logger, rec, target.ID), nil
	case after.DefaultLogbookID == current.ID && writeErr != nil:
		// Not applied, with the old default verified: the change stays pending
		// and is tried again at the next start.
		logger.WarnWith().Err(writeErr).Str("request_id", rec.RequestID).
			Msg("startup: the default logbook change could not be applied; it stays pending and the start continues on the current default")
		r.State = defaultChangeStillPending
		return r, nil
	case after.DefaultLogbookID == current.ID:
		return r, errors.New(op).WithMsg("the default pointer's write was reported committed, yet the old default reads back")
	}
	return r, errors.New(op).WithMsgf("the default pointer reads back %d, neither the old default nor the target", after.DefaultLogbookID)
}

// commitPointer writes the pointer durably and proves it by reading it back.
func commitPointer(ctx context.Context, p defaultPointer, id int64) error {
	if err := p.SetArchiveDefaultLogbookDurablyWithContext(ctx, id); err != nil {
		return err
	}
	after, err := p.ArchiveIdentityWithContext(ctx)
	if err != nil {
		return err
	}
	if after.DefaultLogbookID != id {
		return stderr.New("the durable write was reported committed, yet the pointer reads another logbook")
	}
	return nil
}

// projectApplied makes the applied default effective in memory whatever config's
// persistence does, and replaces the selector with its "applied" tombstone in
// the same write. A durable write confirms that removal at once.
func projectApplied(cfgSvc *config.Service, logger *logging.Service, rec config.DefaultLogbookChange, target int64) defaultChangeReport {
	tomb := rec
	tomb.Removed = config.RemovedApplied
	dur, err := cfgSvc.UpdateInMemoryThenPersist(func(c *config.Config) error {
		c.DefaultLogbookID = target
		c.DefaultLogbookChange = &tomb
		return nil
	})
	r := defaultChangeReport{State: defaultChangeCleanupPending, RequestID: rec.RequestID, LogbookUUID: rec.LogbookUUID, settled: defaultChangeEffective}
	ev := logger.InfoWith()
	if err != nil || dur != config.Durable {
		ev = logger.WarnWith().Err(err).Bool("cleanup_pending", true)
	}
	ev.Str("request_id", rec.RequestID).Int64("default_logbook_id", target).Msg("startup: the default logbook change was applied")
	if cfgSvc.Snapshot().DefaultLogbookChange == nil {
		r.State = defaultChangeEffective
	}
	return r
}

// abandonDefaultChange replaces a refused selector with its "abandoned"
// tombstone. The file stays the truth: a failed write leaves the selector, and
// the change is judged again at the next start.
func abandonDefaultChange(cfgSvc *config.Service, logger *logging.Service, rec config.DefaultLogbookChange, code string) defaultChangeReport {
	tomb := rec
	tomb.Removed, tomb.FailureCode = config.RemovedAbandoned, code
	dur, err := cfgSvc.Update(func(c *config.Config) error {
		c.DefaultLogbookChange = &tomb
		return nil
	})
	r := defaultChangeReport{RequestID: rec.RequestID, LogbookUUID: rec.LogbookUUID, FailureCode: code, settled: defaultChangeAbandoned}
	switch {
	case err != nil:
		r.State = defaultChangeStillPending
	case dur != config.Durable:
		r.State = defaultChangeRemovalUnconfirmed
	default:
		r.State = defaultChangeAbandoned
	}
	logger.WarnWith().Err(err).Str("request_id", rec.RequestID).Str("code", code).Str("state", r.State).
		Msg("startup: the default logbook change was not applied")
	return r
}
