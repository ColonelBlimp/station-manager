package ft8

import (
	stderrors "errors"
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Operating ownership (W-0027 C4a; ADR 0102 ruling 6; ADR 0100 §3, App lifetimes).
//
// FT8 is a scope: an operation in which the daemon transmits by itself once the operator
// has started it. With the operating-client check on, such a scope is operated only by
// the desktop app's current lifetime: the identity its main process made at start and was
// admitted with (internal/api holds the admission; this package is handed the outcome).
//
// THE RULE. Every command that can start, resume or alter operation is accepted only
// from the current lifetime, and the question is asked under seqGate, held until the
// command has taken effect. A replacement takes the same gate. So a command of a
// replaced app that was already on its way cannot act under the new app's arm (codex-125,
// finding 2): there is no moment between "allowed" and "done" for a replacement to fit in.
//
// TWO COMMANDS ARE NOT GATED.
//   - A disarm, from anyone, so that a stop is never refused (operator ruling 2026-11-17,
//     question 11). Its cost is stated there: a replaced app's late disarm ends what the
//     current app is doing.
//   - The profile claim. It changes the profile only on an idle daemon (no subscriber, no
//     arm, no session, nothing in flight: ClaimProfile), so it cannot start, resume or
//     alter operation; and the FT8 view sends it by itself when a stream comes back, which
//     after a daemon restart can be before the app has been admitted again.
//
// NOT REQUIRED means the check is off (server.require_desktop_app): no command asks who
// is calling, and a command that took no gate before C4 takes none now.
//
// The per-scope presence rule (which streams hold an arm, and the grace) is C4b.

// ErrLifetimeNotCurrent refuses an operating command whose caller is not the current app
// lifetime: another lifetime, none at all, or any lifetime while none is current.
var ErrLifetimeNotCurrent = stderrors.New("ft8: the caller is not the current app lifetime")

// ErrNoAppLifetime is ErrLifetimeNotCurrent for a caller that named no lifetime at all:
// a tool, or a desktop app that was started before C4 and is still running. It is its own
// error so that such a caller can be told what to do about it (restart the app).
var ErrNoAppLifetime = fmt.Errorf("%w: the request names none", ErrLifetimeNotCurrent)

// RequireOperatingLifetime turns the rule on. Called once at wiring, before Start, when
// the daemon serves only the desktop app.
func (s *Service) RequireOperatingLifetime() { s.ownershipRequired.Store(true) }

// RetireOperating is the first two steps of a replacement, as one hold of the gate:
// seal, so that no lifetime can operate, the outgoing one included; then stop, by the
// path every other stop takes, with the cause the daemon uses when nobody is attending.
// It returns when the transmission in flight has ended. A contact already earned is
// still logged (invariant 1). PublishOperating is the third step.
func (s *Service) RetireOperating() {
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	s.operatingLifetime = ""
	s.disarmTxLocked(disarmUnattended)
}

// PublishOperating makes lifetime the one that may operate. Between RetireOperating and
// this, none may.
func (s *Service) PublishOperating(lifetime string) {
	s.seqGate.Lock()
	s.operatingLifetime = lifetime
	s.seqGate.Unlock()
}

// operatorCheckLocked is the rule, for a command that already holds seqGate.
func (s *Service) operatorCheckLocked(op errors.Op, who string) error {
	if !s.ownershipRequired.Load() {
		return nil
	}
	if who == "" {
		return errors.New(op).WithErr(ErrNoAppLifetime)
	}
	if who != s.operatingLifetime {
		return errors.New(op).WithErr(ErrLifetimeNotCurrent)
	}
	return nil
}

// operatorGate is the rule for a command that takes no gate of its own: it takes
// seqGate, asks, and hands back the release, to be deferred so the gate is held until
// the command has taken effect. When ownership is not required it takes nothing, so
// those commands run exactly as they did before C4.
func (s *Service) operatorGate(op errors.Op, who string) (release func(), err error) {
	if !s.ownershipRequired.Load() {
		return func() {}, nil
	}
	s.seqGate.Lock()
	if err := s.operatorCheckLocked(op, who); err != nil {
		s.seqGate.Unlock()
		return nil, err
	}
	return s.seqGate.Unlock, nil
}

// Operating is the FT8 command surface as one caller. internal/api uses it for every FT8
// route, with the lifetime the request carried ("" for none).
type Operating struct {
	s   *Service
	who string
}

// OperatingAs returns the command surface for a caller's app lifetime.
func (s *Service) OperatingAs(lifetime string) Operating { return Operating{s: s, who: lifetime} }

// ClaimProfile is not gated (see the package rule above).
func (o Operating) ClaimProfile(name string) (Profile, error) { return o.s.ClaimProfile(name) }

func (o Operating) ArmTx(armed bool) error { return o.s.armTxAs(o.who, armed) }

func (o Operating) TransmitNext(message string, offsetHz float64) error {
	return o.s.transmitNextAs(o.who, message, offsetHz)
}

func (o Operating) StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool, answerMode string) error {
	return o.s.startQsoAs(o.who, ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate, answerMode)
}

func (o Operating) StartQsoFd(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return o.s.startQsoFdAs(o.who, ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (o Operating) StartQsoT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return o.s.startQsoT4As(o.who, ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (o Operating) StartCallCq(ourCall, ourGrid string, offsetHz, dialFreqMHz float64, answerMode, txParity string, logbookID int64, cqModifier string) error {
	return o.s.startCallCqAs(o.who, ourCall, ourGrid, offsetHz, dialFreqMHz, answerMode, txParity, logbookID, cqModifier)
}

func (o Operating) StartWorkCaller(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool, answerMode string) error {
	return o.s.startWorkCallerAs(o.who, ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate, answerMode)
}

func (o Operating) StartWorkCallerFd(ourCall, theirCall, theirGrid, theirClass, theirSection string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return o.s.startWorkCallerFdAs(o.who, ourCall, theirCall, theirGrid, theirClass, theirSection, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (o Operating) StartWorkCallerT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return o.s.startWorkCallerT4As(o.who, ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (o Operating) SetExchangePath(path string) error { return o.s.setExchangePathAs(o.who, path) }
func (o Operating) AbandonQso() error                 { return o.s.abandonQsoAs(o.who) }
func (o Operating) StopAutoWorkRun() error            { return o.s.stopAutoWorkRunAs(o.who) }
func (o Operating) SetQsoSkip(armed bool) error       { return o.s.setQsoSkipAs(o.who, armed) }
func (o Operating) NextAnswerer() error               { return o.s.nextAnswererAs(o.who) }
func (o Operating) PickAnswerer(call string) error    { return o.s.pickAnswererAs(o.who, call) }
func (o Operating) BagAnswerer(call string) error     { return o.s.bagAnswererAs(o.who, call) }
func (o Operating) UnbagAnswerer(call string) error   { return o.s.unbagAnswererAs(o.who, call) }
func (o Operating) ResumeDrain() error                { return o.s.resumeDrainAs(o.who) }

// The commands as they were before C4, for a caller with no lifetime: every existing
// test, and anything in the daemon that is not a request from the app. With ownership
// required such a caller is refused, as a tool is.

func (s *Service) TransmitNext(message string, offsetHz float64) error {
	return s.transmitNextAs("", message, offsetHz)
}

func (s *Service) StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool, answerMode string) error {
	return s.startQsoAs("", ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate, answerMode)
}

func (s *Service) StartQsoFd(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return s.startQsoFdAs("", ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (s *Service) StartQsoT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return s.startQsoT4As("", ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (s *Service) StartCallCq(ourCall, ourGrid string, offsetHz, dialFreqMHz float64, answerMode, txParity string, logbookID int64, cqModifier string) error {
	return s.startCallCqAs("", ourCall, ourGrid, offsetHz, dialFreqMHz, answerMode, txParity, logbookID, cqModifier)
}

func (s *Service) StartWorkCaller(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool, answerMode string) error {
	return s.startWorkCallerAs("", ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate, answerMode)
}

func (s *Service) StartWorkCallerFd(ourCall, theirCall, theirGrid, theirClass, theirSection string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return s.startWorkCallerFdAs("", ourCall, theirCall, theirGrid, theirClass, theirSection, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (s *Service) StartWorkCallerT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return s.startWorkCallerT4As("", ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (s *Service) SetExchangePath(path string)     { _ = s.setExchangePathAs("", path) }
func (s *Service) AbandonQso()                     { _ = s.abandonQsoAs("") }
func (s *Service) StopAutoWorkRun()                { _ = s.stopAutoWorkRunAs("") }
func (s *Service) SetQsoSkip(armed bool) error     { return s.setQsoSkipAs("", armed) }
func (s *Service) NextAnswerer() error             { return s.nextAnswererAs("") }
func (s *Service) PickAnswerer(call string) error  { return s.pickAnswererAs("", call) }
func (s *Service) BagAnswerer(call string) error   { return s.bagAnswererAs("", call) }
func (s *Service) UnbagAnswerer(call string) error { return s.unbagAnswererAs("", call) }
func (s *Service) ResumeDrain() error              { return s.resumeDrainAs("") }
