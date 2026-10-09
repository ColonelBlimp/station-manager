// Package adoption runs Home's SM Cloud adoption in the background (ADR 0090,
// ADR 0091, W-0021 5F.3): at start and hourly, it judges whether Home's default
// SM Cloud binding may claim its legacy cloud logbook, reserves the name
// durably, asks the server to adopt it, and records the confirmation under the
// station account it was made with. It sits beside smcloud rather than inside
// it because it drives the archive manager, whose tests import smcloud.
package adoption

import (
	"context"
	stderr "errors"
	"fmt"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Interval is the time between checks (ADR 0090, T2). The first runs at once
// (ruling A1): safety comes from the judgement and the durable reservation,
// not from how long the daemon has been up.
const Interval = time.Hour

// The states the adopter reports (ADR 0090 T4; ADR 0091 Q2, A3, A4). The
// archive derives the adopted states and "needs confirmation" itself.
const (
	StateChecking        = "checking"
	StateConfirming      = "confirming"
	StateUnsupported     = "unsupported"
	StateUnreachable     = "unreachable"
	StateUncertain       = "uncertain"
	StateUnauthorized    = "unauthorized"
	StateUnreadable      = "unreadable"
	StateRefused         = "refused"
	StateConflict        = "conflict"
	StateUnsafe          = "unsafe"
	StateBlocked         = "blocked"
	StateLocalUnreadable = "local_unreadable"
	StateRecordFailed    = "record_failed"

	// stateAdopted is logged, never held: the archive shows a confirmation.
	stateAdopted = "adopted"
)

var messages = map[string]string{
	StateChecking:        "Checking whether the legacy cloud logbook can be adopted.",
	StateConfirming:      "Confirming the adoption for the current station account.",
	StateUnsupported:     "Not yet: the server does not support archive identity.",
	StateUnreachable:     "Not yet: the server could not be reached (retrying).",
	StateUncertain:       "Adoption outcome uncertain: no confirmation was received from the server. Retrying.",
	StateUnauthorized:    "Not adopted: the token was refused.",
	StateUnreadable:      "Not adopted: the server's answer could not be read.",
	StateUnsafe:          "Not adopted: the legacy cloud logbook cannot be matched safely to Home's default logbook; manual recovery is required.",
	StateBlocked:         "Adoption confirmation blocked: the legacy cloud logbook can no longer be matched safely to Home's default logbook; manual recovery is required.",
	StateLocalUnreadable: "Not yet: the local archive could not be read (retrying).",
	StateRecordFailed:    "Cloud adoption succeeded; local confirmation could not be saved. Retrying.",
	stateAdopted:         "Adopted; applies after a restart.",
}

// transient states prove nothing about an earlier adoption request, so they
// never replace a sticky one of the same subject (ruling A4); nor does a later
// refusal, which publish shows beside it (ruling A5).
var (
	transient = map[string]bool{StateChecking: true, StateConfirming: true, StateUnreachable: true, StateLocalUnreadable: true}
	sticky    = map[string]bool{StateUncertain: true, StateRecordFailed: true}
)

// Archive is what the adopter needs from the archive manager.
type Archive interface {
	ReserveAdoption(ctx context.Context, pin archive.AdoptionPin, judge func(context.Context) (string, error)) (archive.ReserveOutcome, string, error)
	RecordAdoption(ctx context.Context, pin archive.AdoptionPin) (bool, error)
}

// Config reads the saved configuration.
type Config interface {
	Snapshot() config.Config
}

// Adopter runs the checks. Construct with New; drive with Run.
type Adopter struct {
	cfg     Config
	archive Archive
	db      *sqlite.Service
	running []types.LogbookDestination
	log     *logging.Service
	after   func(time.Duration) <-chan time.Time

	// Owned by the checking goroutine.
	suppressed *archive.AdoptionSubject // a terminal outcome's subject (ruling Q4)
	unresolved archive.AdoptionStatus   // an unresolved outcome and its subject (ruling A5)
	logged     archive.AdoptionStatus   // the last outcome logged

	mu     sync.Mutex
	status archive.AdoptionStatus
}

// New builds the adopter. running is the bindings the daemon started with;
// the enabled ones are the routes its workers upload under until the restart.
func New(cfg Config, arch Archive, db *sqlite.Service, running []types.LogbookDestination, log *logging.Service) *Adopter {
	var routes []types.LogbookDestination
	for _, b := range running {
		if b.Enabled {
			routes = append(routes, b)
		}
	}
	return &Adopter{cfg: cfg, archive: arch, db: db, running: routes, log: log, after: time.After}
}

// Run checks at once, then every Interval, until ctx is cancelled. It keeps
// running after a success and while nothing is eligible, so a later account
// change or a newly enabled binding is noticed (ruling Q3); a save never
// starts an attempt itself.
func (a *Adopter) Run(ctx context.Context) {
	for {
		a.check(ctx)
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-a.after(Interval):
		}
	}
}

// Status is the latest status and the subject it describes.
func (a *Adopter) Status() archive.AdoptionStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// target is what one attempt acts for, read from the saved state.
type target struct {
	subject archive.AdoptionSubject
	binding types.LogbookDestination
	label   string
	logbook types.Logbook
	client  *smcloud.AdoptionClient
}

// check is one check: read the saved state and, when an attempt is due, make
// it. A terminal outcome waits until its subject changes (ruling Q4); a
// binding confirmed under the current account needs nothing.
func (a *Adopter) check(ctx context.Context) {
	t, ok, err := a.target(ctx)
	if err != nil {
		// Not evidence of ineligibility: the suppression and the status stay,
		// and the next check reads again.
		a.log.WarnWith().Err(err).Msg("smcloud adoption: the archive could not be read; the next check retries")
		return
	}
	if !ok {
		a.suppressed, a.logged = nil, archive.AdoptionStatus{}
		a.set(archive.AdoptionStatus{})
		return
	}
	if archive.AdoptionConfirmed(t.binding, t.subject.Account) {
		a.suppressed = nil
		a.set(archive.AdoptionStatus{})
		return
	}
	if a.suppressed != nil && *a.suppressed == t.subject {
		return
	}
	a.suppressed = nil
	a.attempt(ctx, t)
}

// target reads Home's default SM Cloud binding, enabled on a live logbook,
// with a complete station account; false when there is nothing eligible. An
// error is a failed read, which says nothing about eligibility.
func (a *Adopter) target(ctx context.Context) (target, bool, error) {
	cfg := a.cfg.Snapshot()
	entry := cfg.QsoArchiveByID(cfg.ActiveQsoArchiveID)
	if entry == nil || entry.Ownership != types.QsoArchiveOwnershipLegacy {
		return target{}, false, nil
	}
	bindings, err := a.db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		return target{}, false, err
	}
	logbooks, err := a.db.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return target{}, false, err
	}
	t := target{label: entry.Label}
	for _, lb := range logbooks {
		if lb.ID == cfg.DefaultLogbookID && lb.UUID != "" {
			t.logbook = lb
		}
	}
	for _, b := range bindings {
		if b.Destination == smcloud.Type && b.LogbookID == cfg.DefaultLogbookID && b.Enabled {
			t.binding = b
		}
	}
	if t.logbook.ID == 0 || t.binding.ForwarderName == "" {
		return target{}, false, nil
	}
	if t.subject, err = archive.SubjectOf(cfg, t.binding); err != nil {
		return target{}, false, nil
	}
	var account types.ForwarderConfig
	for _, fc := range cfg.Forwarders {
		if fc.Type == smcloud.Type {
			account = fc
		}
	}
	fc, err := forwarding.BindingConfig(t.binding, account)
	if err == nil {
		t.client, err = smcloud.NewAdoptionClient(fc)
	}
	if err != nil {
		return target{}, false, nil
	}
	return t, true, nil
}

// attempt is one adoption attempt (ADR 0091): the version, the cloud
// manifest, the judgement and durable reservation, the request, and the
// confirmation. A cancelled attempt (shutdown) reports nothing more.
func (a *Adopter) attempt(ctx context.Context, t target) {
	sub := t.subject
	if t.binding.RemoteAdoptedAt != nil {
		a.report(sub, StateConfirming, "")
	} else {
		a.report(sub, StateChecking, "")
	}
	protocol, err := t.client.IdentityProtocol(ctx)
	if err != nil {
		a.failed(ctx, sub, err, false)
		return
	}
	if protocol < 1 {
		a.terminal(sub, StateUnsupported)
		return
	}
	uuids, err := t.client.CloudUUIDs(ctx, sub.Name)
	if err != nil {
		a.failed(ctx, sub, err, false)
		return
	}
	pin := archive.AdoptionPin{ArchiveID: sub.ArchiveID, Binding: t.binding, Account: sub.Account}
	outcome, reason, err := a.archive.ReserveAdoption(ctx, pin, a.judge(t.binding.LogbookID, uuids))
	switch {
	case ctx.Err() != nil:
		return
	case err != nil:
		a.report(sub, StateLocalUnreadable, "", err)
		return
	case outcome == archive.ReserveChanged:
		// The saved state moved under the attempt; the next check re-reads it.
		a.set(archive.AdoptionStatus{})
		return
	case outcome == archive.ReserveUnsafe && t.binding.AdoptionReservedAt != nil:
		a.report(sub, StateBlocked, reason)
		return
	case outcome == archive.ReserveUnsafe:
		a.report(sub, StateUnsafe, reason)
		return
	}
	err = t.client.Adopt(ctx, smcloud.AdoptRequest{
		LegacyName: sub.Name, ArchiveUUID: sub.ArchiveID, ArchiveLabel: t.label,
		LogbookUUID: t.logbook.UUID, LogbookName: t.logbook.Name, Callsign: t.logbook.Callsign,
	})
	if err != nil {
		a.failed(ctx, sub, err, true)
		return
	}
	recorded, err := a.archive.RecordAdoption(ctx, pin)
	switch {
	case err != nil:
		a.report(sub, StateRecordFailed, "", err)
	case !recorded:
		// The account, default or binding changed since the attempt read
		// them: the completion is discarded and its status is not carried
		// to the new subject (ruling R3).
		a.log.InfoWith().Str("forwarder", sub.ForwarderName).Int64("logbook_id", sub.LogbookID).
			Msg("smcloud adoption: the cloud confirmed, but the binding or the station account changed meanwhile; the confirmation was discarded and the next check judges again")
		a.set(archive.AdoptionStatus{})
	default:
		a.report(sub, stateAdopted, "")
	}
}

// judge is the T3 judgement over the local evidence and the cloud UUIDs, run
// by ReserveAdoption under its locks: it reads only the archive.
func (a *Adopter) judge(defaultLogbookID int64, cloud []string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		evidence, err := smcloud.LocalAdoptionEvidence(ctx, a.db, defaultLogbookID, a.running)
		if err != nil {
			return "", err
		}
		evidence.CloudUUIDs = cloud
		verdict, err := smcloud.JudgeAdoption(evidence)
		switch {
		case err != nil:
			return "", err
		case verdict.Safe:
			return "", nil
		case verdict.Reason == "":
			return "the evidence is not safe", nil
		}
		return verdict.Reason, nil
	}
}

// failed reports a failed request. A request that may have reached the server
// and drew no answer leaves the adoption's outcome uncertain. A 409 to the
// adoption request with a recognised code (the client keeps only the three
// adoption conflicts) resolves an earlier uncertain outcome of the same
// subject: the store never clears a mapping, and a replay of one in effect
// answers 200 (internal/cloud/store/identity.go, adoptArchive and
// adoptLogbook). Any other 409 resolves nothing.
func (a *Adopter) failed(ctx context.Context, sub archive.AdoptionSubject, err error, sent bool) {
	if ctx.Err() != nil {
		return
	}
	var ae *smcloud.AdoptionError
	if !stderr.As(err, &ae) {
		a.report(sub, StateUnreachable, "", err)
		return
	}
	switch ae.Failure {
	case smcloud.AdoptionUnreachable:
		if sent {
			a.report(sub, StateUncertain, "", err)
		} else {
			a.report(sub, StateUnreachable, "", err)
		}
	case smcloud.AdoptionUnauthorized:
		a.terminal(sub, StateUnauthorized, err)
	case smcloud.AdoptionUnreadable:
		// After the adoption request the server may have committed it.
		if sent && a.unresolved.Subject != sub {
			a.unresolved = archive.AdoptionStatus{Subject: sub, State: StateUncertain}
		}
		a.terminal(sub, StateUnreadable, err)
	case smcloud.AdoptionConflict:
		if sent && ae.Code != "" && a.unresolved.Subject == sub && a.unresolved.State == StateUncertain {
			a.unresolved = archive.AdoptionStatus{}
		}
		blocker := "the adoption conflicts"
		if ae.Code != "" {
			blocker = fmt.Sprintf("the adoption conflicts (%s)", ae.Code)
		}
		a.suppressed = &sub
		a.publish(sub, StateConflict, "Not adopted: "+blocker+".", blocker, "", err)
	default:
		blocker := fmt.Sprintf("the server returned HTTP %d", ae.Status)
		a.suppressed = &sub
		a.publish(sub, StateRefused, "Adoption could not be confirmed: "+blocker+".", blocker, "", err)
	}
}

// blockers name what stops a confirmation, for a status that keeps an earlier
// unresolved outcome (ruling A5).
var blockers = map[string]string{
	StateUnauthorized: "the server rejected authentication (HTTP 401)",
	StateUnreadable:   "the server's answer could not be read",
	StateUnsupported:  "the server does not support archive identity",
	StateUnsafe:       "the legacy cloud logbook can no longer be matched safely to Home's default logbook; manual recovery is required",
	StateBlocked:      "the legacy cloud logbook can no longer be matched safely to Home's default logbook; manual recovery is required",
}

// kept opens the message of a status that keeps an unresolved outcome while
// naming the current blocker.
var kept = map[string]string{
	StateUncertain:    "Adoption outcome remains uncertain.",
	StateRecordFailed: "Cloud adoption succeeded; local confirmation could not be saved.",
}

// terminal reports an outcome that waits for a relevant change (ruling Q4).
func (a *Adopter) terminal(sub archive.AdoptionSubject, state string, errs ...error) {
	a.suppressed = &sub
	a.publish(sub, state, messages[state], blockers[state], "", errs...)
}

// report publishes a state with its standard message; reason is the
// judgement's, for the log only.
func (a *Adopter) report(sub archive.AdoptionSubject, state, reason string, errs ...error) {
	a.publish(sub, state, messages[state], blockers[state], reason, errs...)
}

// publish sets the status and logs the transition once. An unresolved outcome
// of the same subject (uncertain, or a cloud success not recorded) is kept
// until evidence resolves it (ruling A5): a transient state shows it as it
// is, and any other names the current blocker beside it. "adopted" clears the
// status, since the archive shows the confirmation.
func (a *Adopter) publish(sub archive.AdoptionSubject, state, message, blocker, reason string, errs ...error) {
	u := a.unresolved
	if u.Subject != sub {
		u = archive.AdoptionStatus{}
	}
	shown := archive.AdoptionStatus{Subject: sub, State: state, Message: message}
	switch {
	case state == stateAdopted:
		a.unresolved, shown = archive.AdoptionStatus{}, archive.AdoptionStatus{}
	case u.State == StateRecordFailed && state == StateUncertain, u.State != "" && transient[state]:
		shown = archive.AdoptionStatus{Subject: sub, State: u.State, Message: messages[u.State]}
	case sticky[state]:
		a.unresolved = archive.AdoptionStatus{Subject: sub, State: state}
	case u.State != "":
		shown = archive.AdoptionStatus{Subject: sub, State: u.State, Message: kept[u.State] + " Confirmation is blocked: " + blocker + "."}
	}
	a.set(shown)
	if transient[state] && shown.State != state {
		return
	}
	first := a.logged.Subject != sub
	switch {
	case state == StateChecking || state == StateConfirming:
		if !first {
			return
		}
	case !first && a.logged.State == shown.State && a.logged.Message == shown.Message:
		return
	}
	a.logged = shown
	ev := a.log.InfoWith()
	if !transient[state] && state != stateAdopted {
		ev = a.log.WarnWith()
	}
	ev = ev.Str("state", state).Str("forwarder", sub.ForwarderName).Int64("logbook_id", sub.LogbookID).Str("cloud_name", sub.Name)
	if reason != "" {
		ev = ev.Str("reason", reason)
	}
	for _, err := range errs {
		ev = ev.Err(err)
	}
	if state == stateAdopted {
		message = messages[stateAdopted]
	} else {
		message = shown.Message
	}
	ev.Msg("smcloud adoption: " + message)
}

func (a *Adopter) set(s archive.AdoptionStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = s
}
