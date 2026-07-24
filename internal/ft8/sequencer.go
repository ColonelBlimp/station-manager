package ft8

import (
	stderrors "errors"
	"strings"
	"sync"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// FT8 manual sequencer (ADR 0031): operator-initiated, auto-advancing. The
// operator clicks a CQ to answer (StartQso) with TX armed; this walks the CQ→73
// ladder via the e2 Exchange — transmitting each rung in the slot opposite the
// worked station, advancing on the decode directed to us — and finishes when the
// 73 is sent (e4 will log it). The operator intervenes only to Abandon.
//
// Timing (ADR 0032): we answer in the slot immediately AFTER the worked station's,
// which we only learn after decoding their slot (~0.7–1.6 s into our slot). The
// rung is sent on the synchronised timebase (seqTransmit → TransmitCurrentSlot),
// in the parity opposite theirs — NOT at the next boundary, which would be their
// parity and collide. Because the decode lands after the slot's nominal +0.5 s
// start, the controller drops the elapsed head and transmits the synchronised
// remainder (truncate-don't-shift); the receiver re-syncs on the Costas arrays.
// OnSlot is the single trigger: the decode loop calls it once per completed slot.

// defaultSeqMaxRepeats caps consecutive unanswered transmissions of a rung before
// the QSO is abandoned (ADR 0031 off-ramp). ~6 of our slots ≈ 90 s of calling. The
// operator can override it via ft8.tx.max_repeats (clamped ≤ Ft8MaxRepeatsCeiling);
// this is the fallback when unset.
const defaultSeqMaxRepeats = types.DefaultFt8MaxRepeats

// txLateWindowSec is the latest into our slot we will START a rung. Past this,
// too few symbols / Costas sync words survive the head-truncation (ADR 0032) for
// a reliable decode, so we skip and retry on our next cycle. The truncation
// itself lives in the TxController. Package var so tests can dial it.
var txLateWindowSec = 4.5

// Sequencer sentinels (mapped to HTTP by the api layer).
var (
	// ErrQsoInProgress: a StartQso/StartCallCq while a session is already active.
	ErrQsoInProgress = stderrors.New("ft8: a QSO is already in progress")
	// ErrNoOffset: started without a TX offset.
	ErrNoOffset = stderrors.New("ft8: no TX offset selected")
	// ErrNoCall: StartCallCq without an operator callsign.
	ErrNoCall = stderrors.New("ft8: no operator callsign configured")
	// ErrFdIdentityUnset: StartQsoFd without the operator's Field Day class+section
	// (ft8.field_day) — we can't transmit an FD exchange without our own identity.
	ErrFdIdentityUnset = stderrors.New("ft8: Field Day class/section not configured")

	// ErrNoActiveQso: SetSkipIfSilent when no skippable session is active (idle, or
	// a Call-CQ run — whose Next is an immediate takeover, not a deferred skip).
	ErrNoActiveQso = stderrors.New("ft8: no active QSO to arm a skip on")
)

// seqMode is the active sequencer session: idle, answering a CQ (ex drives), or
// calling CQ (caller drives, ADR 0033). Only one session is active at a time.
type seqMode int

const (
	seqIdle seqMode = iota
	seqAnswering
	seqCalling
	// seqWorking: working a station that is calling US (it sent a grid directed to
	// our call), picked by the operator from the pile-up (ADR 0033 "work a caller").
	// Reuses the caller ladder (we report first), but unlike seqCalling it has no
	// CQ phase and on completion/off-ramp it goes idle rather than resuming CQ.
	seqWorking
	// seqAnsweringFd: answering a station's CQ FD (ARRL Field Day, search & pounce).
	// The FD twin of seqAnswering — fdEx drives it; we send our class+section then
	// RR73 and log their exchange. No FD caller side (we never call CQ FD).
	seqAnsweringFd
	// seqWorkingFd: working a station that called US with a Field Day exchange (the FD
	// twin of seqWorking). fdWork drives it; we reply R+our class/section, RR73, and log
	// their class/section. This is the dominant path for a sought-after DX station.
	seqWorkingFd
	// seqAnsweringT4: answering a NONSTANDARD/compound station's CQ with the reduced
	// type-4 ladder (ADR 0048) — t4Ex drives it. No grid/report is exchanged (the
	// protocol has no wire form for one with a hashed call); we open bare-calls, they
	// roger, we 73. The type-4 twin of seqAnswering, isolated so the standard path is
	// untouched.
	seqAnsweringT4
	// seqWorkingT4: working a nonstandard station that called US, reduced type-4 ladder
	// (ADR 0048) — t4Work drives it. A single RR73 rung (no report), logged after it.
	seqWorkingT4
)

// QsoStatus roles (the `ft8-qso` SSE Role field) — which side of the contact we
// are, so the SPA renders the matching ladder.
const (
	roleAnswerer = "answerer"
	roleCaller   = "caller"
	// roleWorker: working a station that called US, picked from the pile-up (ADR 0033
	// "work a caller"). Caller-style exchange (we report first) but with NO CQ phase,
	// so the SPA renders a dedicated ladder (no leading CQ row) — distinct from
	// roleCaller, whose ladder opens with our CQ.
	roleWorker = "worker"
)

// QsoStatus is the `ft8-qso` SSE payload + the sequencer's exposed state: the role
// (answerer/caller), the active contact's rung, the worked call, the message we'll
// send next, and the unanswered-repeat count. Active=false means idle (no session).
type QsoStatus struct {
	Active bool   `json:"active"`
	Role   string `json:"role,omitempty"` // "answerer" | "caller" | "worker"; empty when idle
	// Fd marks an ARRL Field Day answer-a-CQ session, so the SPA renders the FD ladder
	// (class/section rungs) instead of the standard grid/report one.
	Fd bool `json:"fd,omitempty"`
	// Type4 marks a reduced type-4 (nonstandard/compound call) session (ADR 0048), so
	// the SPA renders the reduced bare-calls→RR73→73 ladder (no grid/report rungs).
	Type4     bool   `json:"type4,omitempty"`
	TheirCall string `json:"their_call,omitempty"`
	// TheirGrid — the worked station's 4-char grid, for the ladder's opening row (so it
	// shows the real grid, not a "<GRID>" placeholder). Empty until known.
	TheirGrid string `json:"their_grid,omitempty"`
	// State — answerer: calling|reporting|confirming; caller: calling-cq|reporting|rogering.
	State       string `json:"state,omitempty"`
	NextMessage string `json:"next_message,omitempty"`
	Repeats     int    `json:"repeats,omitempty"`
	// SkipArmed — the operator armed skip-if-silent on this session (deferred
	// Next): a silent cycle ends the session instead of keying the repeat.
	SkipArmed bool `json:"skip_armed,omitempty"`
	// MaxRepeats — the unanswered-rung repeat cap, set ONLY while the current rung is
	// actually subject to it (an answerer pre-73, or a caller working an answerer
	// pre-RR73). Zero (omitted) on the uncapped rungs (calling CQ) and the one-shot
	// final rungs (73/RR73), so the SPA shows the "calls left" countdown iff this is
	// >0 without re-deriving the cap-vs-one-shot rule. Remaining = MaxRepeats-Repeats.
	MaxRepeats int `json:"max_repeats,omitempty"`
	// OurReport / TheirReport — the signal reports exchanged, formatted exactly as
	// they appear on the air (e.g. "-12", "+04"); empty until known. OurReport is
	// the report WE send (our SNR of their signal); TheirReport is the one THEY sent
	// us. The SPA fills the ladder's <RST> placeholders from these.
	OurReport   string `json:"our_report,omitempty"`
	TheirReport string `json:"their_report,omitempty"`
	// FD exchange (Fd sessions only) — our + their ARRL Field Day class/section, for the
	// SPA's FD ladder. Ours are known from config at start; theirs are empty until known
	// (an answerer learns them in their R-exchange rung; a worker has them from the
	// picked call). The SPA fills the FD ladder's <CLS>/<SEC> placeholders from these.
	OurClass     string `json:"our_class,omitempty"`
	OurSection   string `json:"our_section,omitempty"`
	TheirClass   string `json:"their_class,omitempty"`
	TheirSection string `json:"their_section,omitempty"`
	// TheirPeriod — the FT8 slot parity ("even"|"odd") of the slots we PROCESS, i.e.
	// the operator's RX parity (the worked/answered station transmits in these slots).
	// The SPA uses it as the single workable parity for the pile-up queue: a station
	// can only be worked next if it shares this parity, so wrong-parity decodes are
	// blocked from the queue. Empty when idle.
	TheirPeriod string `json:"their_period,omitempty"`
}

// CompletedQso is captured when an exchange finishes (73 sent) — the data e4 maps
// to a types.Qso (BuildQso) and submits via qsoservice.
type CompletedQso struct {
	// LogbookID is the logbook PINNED when the exchange was armed (ADR 0055) —
	// the QSO is logged there, not to whatever the current default logbook is at
	// completion, so a mid-exchange logbook switch can't relabel or misroute it.
	// Stamped by the Service's onComplete from the arm-time value. 0 = unpinned
	// (defensive; the sink falls back to the current default).
	LogbookID      int64
	TheirCall      string
	TheirGrid      string
	OurReport      int // the report WE sent (our SNR of their signal)
	HasOurReport   bool
	TheirReport    int // the report THEY sent us
	HasTheirReport bool
	// StartedAt is when this contact began — the operator's click for answer-a-CQ /
	// work-a-caller, or the slot we picked the answerer for a Call-CQ pile-up. It is
	// the QSO's TIME_ON; the completion instant passed to BuildQso is TIME_OFF. Zero
	// if a path failed to stamp it (BuildQso falls back to the completion instant).
	StartedAt time.Time
	OffsetHz  float64
	// DialFreqMHz is the rig's dial frequency at QSO start (from the SPA, which
	// reads it from the live rig state). The logged QSO frequency IS this dial
	// (FT8 convention — see BuildQso); the band is derived from the dial, and
	// OffsetHz is TX audio placement only, never folded into FREQ.
	DialFreqMHz float64
	// AntPath is the operator's antenna-path choice for this contact, "S" (short)
	// or "L" (long), used only to annotate the logged QSO (ADIF ANT_PATH + the
	// short/long bearing+distance). It is NOT part of the on-air exchange — FT8
	// messages carry no path info. The Service stamps the current exchange path
	// onto the per-attempt CompletedQso when the final rung succeeds.
	// Empty = unset (defaults to short).
	AntPath string
	// antPathGen ties AntPath to the Service path generation captured for this
	// completion attempt. antPathStamped distinguishes a valid generation zero
	// from a synthetic/direct completion. Both are internal concurrency metadata.
	antPathGen     uint64
	antPathStamped bool

	// Class / Section are the WORKED station's ARRL Field Day exchange (their class
	// + ARRL/RAC section), set only for an answer-a-CQ-FD contact. Empty for a
	// standard QSO. BuildQso maps them to ADIF CLASS / ARRL_SECT (+ CONTEST_ID).
	Class   string
	Section string
}

// Sequencer owns the (single) active session — answering a CQ or calling CQ, never
// both. Its dependencies are injected so it stays decoupled from the Service and
// unit-testable: transmit sends a rung (Service.seqTransmit — current-slot late-dt),
// publish fans out QsoStatus on the ft8-qso SSE, and onComplete (optional, e4)
// consumes a finished QSO. The caller-side flow (ADR 0033) lives in
// caller_sequencer.go but shares this struct's deps + the OnSlot entry point.
type Sequencer struct {
	mu   sync.Mutex
	mode seqMode

	// Answering a CQ (seqAnswering): ex is the active exchange; theirGrid is the
	// worked station's grid from the CQ we answered.
	ex        *Exchange
	theirGrid string

	// Answering a CQ FD (seqAnsweringFd): fdEx is the active Field Day exchange.
	fdEx *FdExchange

	// Working a caller in FD (seqWorkingFd): fdWork is the active Field Day work exchange.
	fdWork *FdWorkExchange

	// Reduced type-4 ladders (ADR 0048): t4Ex answers a nonstandard station's CQ
	// (seqAnsweringT4); t4Work works a nonstandard caller (seqWorkingT4).
	t4Ex   *T4Exchange
	t4Work *T4WorkExchange

	// Calling CQ (seqCalling, ADR 0033): caller is nil while still calling (phase 1),
	// set once an answerer is chosen (phase 2). cqMessage is repeated each of our
	// slots until answered; answerMode is auto_first | operator_pick.
	caller     *CallerExchange
	ourCall    string
	ourGrid    string
	cqMessage  string
	answerMode string
	// stalledCalls accumulates the answerers abandoned at the repeat cap since the
	// current CQ round began. pickAnswererLocked skips them, so a handful of stations
	// that keep repeating their grid can't be re-selected in rotation and starve the
	// rest of the pile-up (113e14b8 review P2: a single-cycle exclusion let two
	// preferred stallers ping-pong forever). Reset when a fresh CQ round starts — a
	// completed contact, the rescan exhausting the live answerers, and StartCallCq.
	stalledCalls []string

	// Shared by both modes. theirPeriod is the parity of the slots we PROCESS (the
	// worked station's when answering, or — when calling CQ — the answerers', i.e.
	// opposite our CQ parity); we transmit in the opposite (current) slot.
	theirPeriod string
	offsetHz    float64
	dialFreqMHz float64 // rig dial freq at start, for the logged QSO frequency
	// logbookID is the logbook PINNED to the ACCEPTED session (ADR 0055), consumed
	// from pendingLogbookID atomically with mode activation and stamped onto the
	// CompletedQso. pendingLogbookID is staged by the Service before a start.
	logbookID        int64
	pendingLogbookID int64
	startedAt        time.Time // contact start, stamped as the logged QSO's TIME_ON
	repeats          int
	// skipIfSilent — operator-armed "drop this contact instead of repeating an
	// unanswered rung" (the SPA's deferred Next, moved daemon-side 2026-07-13).
	// Checked at the silent-repeat sites BEFORE the repeat keys, so a skip never
	// transmits at a station we've decided to drop (the SPA-side version could
	// only abandon a repeat already on the air — an audible PTT tick and wasted
	// RF). Cleared when the partner advances us (they came back), on every
	// session start, and on Abandon.
	skipIfSilent bool
	maxRepeats   int

	// sessionGen is bumped on every session-identity change (StartQso /
	// StartCallCq commit, Abandon). A final-rung onDone closure captures the gen
	// at queue time and acts (logs, mutates state, publishes) ONLY if the gen
	// still matches — so an Abandon/disarm (or any future superseding path) that
	// fires while the final 73/RR73 is still in flight can't let a stale callback
	// log a QSO or publish idle over a newer session (review follow-up M1).
	sessionGen uint64

	// lastTxSlot is the start of the TX slot the most recent rung fired in.
	// StartQso's immediate fireOpening and the SAME slot's pending OnSlot can
	// otherwise both drive one slot (review 2026-07-20 #2): the opening fires,
	// then OnSlot consumes another repeat for the identical slot — with
	// max_repeats=1 that abandons the session while the opening is still
	// playing. OnSlot's transmit sections skip a slot already fired.
	lastTxSlot time.Time

	// transmit sends a rung. dialMHz is the active session's dial (from this
	// sequencer's own accepted state), passed through so the decode-log TX line
	// records the correct band without the Service holding mutable per-session dial
	// state (review: a rejected concurrent Start* must not relabel an active rung).
	// gen is the sessionGen captured at rung-decision time — the Service re-checks
	// it at transmission COMMIT (under its own txMu, via isCurrent), because the
	// rung sites call transmit after dropping s.mu: an Abandon landing in that gap
	// found no txCancel to cancel yet, and the stale rung keyed RF after abandon
	// returned (review 2026-07-20 #1). onDone (optional) fires from the transmit
	// goroutine once the transmission finishes — ok=true only on a clean on-air
	// success. The final rung passes a completion closure so the QSO is logged
	// ONLY after the 73/RR73 actually transmitted, never on "queued" (review H1).
	transmit func(message string, offsetHz, dialMHz float64, gen uint64, onDone func(ok bool)) error
	// Completion transitions publish while s.mu is held so an immediate
	// replacement session cannot publish active before the old idle event. The
	// sink must therefore be non-reentrant; production only appends to the hub.
	publish func(QsoStatus)
	// prepareComplete stamps Service-owned, logging-only metadata onto the
	// per-attempt QSO after the final rung succeeds and before the completion
	// state is released. It must not call back into the Sequencer; completion
	// callbacks invoke it with s.mu released.
	prepareComplete func(*CompletedQso)
	onComplete      func(CompletedQso)
	log             logging.Logger
}

// transmitLocked binds the CURRENT session generation into a rung-shaped
// transmit closure. Rung sites capture it under s.mu (in place of s.transmit)
// so the commit-time isCurrent check compares against the generation the rung
// was decided for — not whatever is current when the closure finally runs.
func (s *Sequencer) transmitLocked() func(message string, offsetHz, dialMHz float64, onDone func(ok bool)) error {
	gen := s.sessionGen
	tx := s.transmit
	return func(message string, offsetHz, dialMHz float64, onDone func(ok bool)) error {
		return tx(message, offsetHz, dialMHz, gen, onDone)
	}
}

// isCurrent reports whether gen is still the live session generation. The
// Service calls this while holding its txMu at transmission commit — lock
// order txMu→s.mu, safe because no sequencer path holds s.mu while calling
// Service transmit/onComplete. Terminal status publication may hold s.mu, but
// the production publisher only appends to the SSE hub and never takes txMu.
func (s *Sequencer) isCurrent(gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionGen == gen
}

func newSequencer(transmit func(string, float64, float64, uint64, func(ok bool)) error, publish func(QsoStatus), maxRepeats int, log logging.Logger) *Sequencer {
	if log == nil {
		log = logging.Noop()
	}
	if maxRepeats <= 0 {
		maxRepeats = defaultSeqMaxRepeats
	}
	return &Sequencer{
		maxRepeats: maxRepeats,
		transmit:   transmit,
		publish:    publish,
		log:        log,
	}
}

// SetMaxRepeats retunes the unanswered-rung repeat cap while the sequencer runs
// (ft8.tx.max_repeats, edited from the FT8 Settings tab). Takes s.mu because
// maxRepeats is read on the slot goroutine; the next OnSlot rung check uses the new
// value, so LOWERING it mid-pile-up drops a dead contact sooner (and raising it lets
// it call longer). Clamped via the shared config resolver ([1, Ft8MaxRepeatsCeiling];
// ≤0 → default) so there's one clamp truth. The change is picked up on the next slot;
// the fresh cap is republished then, so no forced re-publish here.
func (s *Sequencer) SetMaxRepeats(n int) {
	resolved := types.ResolveFt8MaxRepeats(&types.Ft8TXConfig{MaxRepeats: n})
	s.mu.Lock()
	s.maxRepeats = resolved
	s.mu.Unlock()
}

// StartQso begins answering a CQ. theirSlotUTC is the RFC3339 start of the slot
// the CQ was heard in (it fixes the worked station's parity — we transmit in the
// opposite one). offsetHz is the operator-picked clear offset. Idempotency: only
// one QSO at a time (ErrQsoInProgress).
func (s *Sequencer) StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC string, offsetHz, dialFreqMHz float64, now time.Time) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	t, err := time.Parse(time.RFC3339, theirSlotUTC)
	if err != nil {
		return err
	}

	ex := NewExchange(ourCall, ourGrid, theirCall)
	// Validate the opening message encodes BEFORE committing the session (review
	// M1): the CQ parser accepts compound/portable calls (with `/`) the standard
	// encoder rejects, so an unanswerable call would otherwise publish an active
	// ladder that can never produce RF. Fail up front with a clear error.
	if msg, ok := ex.TxMessage(); ok {
		if _, err := goft8.EncodeStandardMessage(msg); err != nil {
			return ErrTxBadMessage
		}
	}

	s.mu.Lock()
	if s.mode != seqIdle {
		s.mu.Unlock()
		return ErrQsoInProgress
	}
	s.mode = seqAnswering
	s.skipIfSilent = false
	s.sessionGen++
	s.logbookID = s.pendingLogbookID // pin the staged logbook atomically with activation
	s.ex = &ex
	s.theirPeriod = SlotRefFromTime(t).Period
	s.theirGrid = theirGrid
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.startedAt = now.UTC()
	s.repeats = 0
	st := s.statusLocked()
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", theirCall).Str("their_period", s.theirPeriod).
		Float64("offset_hz", offsetHz).Msg("ft8 seq: answering CQ")
	s.publish(st)
	// Send the opening call this slot if we're already in our TX window — else the
	// first rung waits for the next qualifying OnSlot (up to a full cycle late).
	s.fireOpening(now)
	return nil
}

// StartQsoFd begins answering a CQ FD (ARRL Field Day, search & pounce). It is the
// FD twin of StartQso: ourClass/ourSection are the operator's configured Field Day
// identity (the reply carries them), theirGrid comes from their CQ FD (logged, not
// exchanged). Same one-session-at-a-time + opening-encode-validation discipline.
func (s *Sequencer) StartQsoFd(ourCall, ourClass, ourSection, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, now time.Time) error {
	if offsetHz <= 0 {
		return ErrNoOffset
	}
	if strings.TrimSpace(ourClass) == "" || strings.TrimSpace(ourSection) == "" {
		return ErrFdIdentityUnset
	}
	t, err := time.Parse(time.RFC3339, theirSlotUTC)
	if err != nil {
		return err
	}

	ex := NewFdExchange(ourCall, ourClass, ourSection, theirCall, theirGrid, theirSnr)
	// Validate our opening exchange encodes before committing (mirrors StartQso): a
	// compound/portable call or a class/section the packer rejects would otherwise
	// publish a ladder that can never produce RF.
	if msg, ok := ex.TxMessage(); ok {
		if _, err := goft8.EncodeStandardMessage(msg); err != nil {
			return ErrTxBadMessage
		}
	}

	s.mu.Lock()
	if s.mode != seqIdle {
		s.mu.Unlock()
		return ErrQsoInProgress
	}
	s.mode = seqAnsweringFd
	s.skipIfSilent = false
	s.sessionGen++
	s.logbookID = s.pendingLogbookID // pin the staged logbook atomically with activation
	s.fdEx = &ex
	s.theirPeriod = SlotRefFromTime(t).Period
	s.offsetHz = offsetHz
	s.dialFreqMHz = dialFreqMHz
	s.startedAt = now.UTC()
	s.repeats = 0
	st := s.statusLocked()
	s.mu.Unlock()

	s.log.InfoWith().Str("their_call", theirCall).Str("their_period", s.theirPeriod).
		Float64("offset_hz", offsetHz).Str("our_class", ourClass).Str("our_section", ourSection).
		Msg("ft8 seq: answering CQ FD")
	s.publish(st)
	s.fireOpening(now)
	return nil
}

// Abandon drops the active session — answering or calling CQ (operator action,
// disarm, or off-ramp). Idempotent.
func (s *Sequencer) Abandon() {
	s.mu.Lock()
	was := s.mode != seqIdle
	s.mode = seqIdle
	s.sessionGen++ // supersede any in-flight final-rung callback (review follow-up M1)
	s.ex = nil
	s.fdEx = nil
	s.fdWork = nil
	s.t4Ex = nil
	s.t4Work = nil
	s.caller = nil
	s.repeats = 0
	s.skipIfSilent = false
	s.mu.Unlock()
	if was {
		s.log.InfoWith().Msg("ft8 seq: session abandoned")
		s.publish(QsoStatus{Active: false})
	}
}

// SetSkipIfSilent arms (or disarms) the skip-if-silent intent on the active
// session: when armed, a silent cycle on an already-transmitted rung ends the
// session INSTEAD of keying the repeat. Arming requires a skippable session
// (answering / working, standard or FD) — a Call-CQ run's Next is an immediate
// takeover, and idle has nothing to skip (ErrNoActiveQso). Disarming is always
// accepted (idempotent — the session may have ended between the operator's
// click and the request). Publishes the updated status (confirm-by-push).
func (s *Sequencer) SetSkipIfSilent(armed bool) error {
	s.mu.Lock()
	skippable := s.mode == seqAnswering || s.mode == seqAnsweringFd ||
		s.mode == seqWorking || s.mode == seqWorkingFd ||
		s.mode == seqAnsweringT4 || s.mode == seqWorkingT4
	if armed && !skippable {
		s.mu.Unlock()
		return ErrNoActiveQso
	}
	// Reaching here: either a valid arm (skippable) or a disarm (always ok).
	s.skipIfSilent = armed
	st := s.statusLocked()
	s.mu.Unlock()
	s.publish(st)
	return nil
}

// Active reports whether a session (answering or calling CQ) is in progress.
func (s *Sequencer) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode != seqIdle
}

// ActiveCallsign returns our own callsign for the CURRENTLY-ACTIVE session — the
// call pinned when the exchange was armed (ADR 0055, pin-at-arm). It is the single
// source for self-decode filtering: only an active session keys TX, so an idle
// sequencer returns "" and nothing is filtered (nothing of ours is on the air).
// No DB lookup, no fallback — the call was resolved once at arm and carried here.
func (s *Sequencer) ActiveCallsign() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.mode {
	case seqAnswering:
		if s.ex != nil {
			return s.ex.OurCall
		}
	case seqAnsweringFd:
		if s.fdEx != nil {
			return s.fdEx.OurCall
		}
	case seqWorkingFd:
		if s.fdWork != nil {
			return s.fdWork.OurCall
		}
	case seqAnsweringT4:
		if s.t4Ex != nil {
			return s.t4Ex.OurCall
		}
	case seqWorkingT4:
		if s.t4Work != nil {
			return s.t4Work.OurCall
		}
	case seqCalling, seqWorking:
		return s.ourCall
	default:
		// seqIdle — no active session; nothing is keyed, so there's no own-call to
		// filter. A new TX mode MUST add its case above, or self-decode filtering
		// silently degrades for it (its TX would leak back into the decode feed).
	}
	// Reached only if an active mode's exchange pointer is unexpectedly nil (an
	// invariant violation) — treat as no active call rather than dereference.
	return ""
}

// setPendingLogbook stages the arm-time logbook for the NEXT accepted start (ADR
// 0055). The Service calls it (under seqGate) BEFORE seq.Start*; each Start* then
// consumes it into s.logbookID under s.mu ATOMICALLY with mode activation, so the
// pin lands with the session and no completion can observe an active session before
// its logbook is set. Staging-BEFORE-activation is the fix for the terminal-first-rung
// race: a post-activation bind left a gap in which StartWorkCallerT4's sole RR73 could
// complete and snapshot a stale (or zero, first-session) logbook. A rejected start
// (ErrQsoInProgress) leaves the staged value unconsumed and the active session's pin
// untouched; the next start overwrites it — all starts are serialised by seqGate.
func (s *Sequencer) setPendingLogbook(id int64) {
	s.mu.Lock()
	s.pendingLogbookID = id
	s.mu.Unlock()
}

// OnSlot is the per-slot driver, called by the decode loop once each completed slot.
// ref is the slot just decoded; msgs are its decodes; now is wall-clock UTC. It
// dispatches to the active session's handler (answering a CQ, or calling CQ); idle
// is a no-op. Each handler re-validates its mode under s.mu, so the brief unlocked
// read here is safe (a concurrent Abandon at worst makes a handler no-op).
func (s *Sequencer) OnSlot(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	s.mu.Lock()
	mode := s.mode
	s.mu.Unlock()
	switch mode {
	case seqAnswering:
		s.onSlotAnswering(ref, msgs, now)
	case seqAnsweringFd:
		s.onSlotAnsweringFd(ref, msgs, now)
	case seqCalling:
		s.onSlotCalling(ref, msgs, now)
	case seqWorking:
		s.onSlotWorking(ref, msgs, now)
	case seqWorkingFd:
		s.onSlotWorkingFd(ref, msgs, now)
	case seqAnsweringT4:
		s.onSlotAnsweringT4(ref, msgs, now)
	case seqWorkingT4:
		s.onSlotWorkingT4(ref, msgs, now)
	default:
		// seqIdle — no active session; nothing to drive this slot.
	}
}

// onSlotAnswering drives an answer-a-CQ exchange: feed the worked station's decodes
// to Advance, then transmit our rung in the (current) opposite-parity slot, late-dt.
func (s *Sequencer) onSlotAnswering(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	// Decide everything under the lock, then run side effects (transmit, publish,
	// complete) after releasing it — transmit takes the Service's txMu.
	s.mu.Lock()
	if s.ex == nil {
		s.mu.Unlock()
		return
	}
	// The slot we can still transmit in is ref+1 (the current one), whose parity
	// is opposite ref's. Only the worked station's slots set up our reply; a slot
	// of our own parity just decoded our own transmission — nothing to do.
	if ref.Period != s.theirPeriod {
		s.mu.Unlock()
		return
	}

	// Advance on their decode directed to us (records their report + our SNR).
	// Capture what the worked station said to us this slot and whether it advanced
	// us, so a stalled exchange is diagnosable: did their roger arrive and we fail
	// to parse it, or did they send nothing we decoded?
	var heard string
	advanced := false
	for _, m := range msgs {
		if pm := parseMessage(m.Text); pm.to == s.ex.OurCall && pm.from == s.ex.TheirCall {
			heard = m.Text
		}
		if next, ok := s.ex.Advance(m.Text, m.SNR); ok {
			*s.ex = next
			s.repeats = 0
			advanced = true
			break
		}
	}
	if heard != "" {
		s.log.InfoWith().
			Str("from_worked", heard).
			Bool("advanced", advanced).
			Str("now_rung", s.ex.State.label()).
			Msg("ft8 seq: decode from worked station")
	}
	if advanced {
		s.skipIfSilent = false // they came back — an armed skip no longer applies
	}

	msg, ok := s.ex.TxMessage()
	if !ok { // exchange already done — shouldn't happen here; clear defensively.
		s.ex = nil
		s.mode = seqIdle
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	rung := s.ex.State.label()

	// Late-window guard: the current (our) slot started at ref.start + SlotDuration.
	// Too late into it and too few symbols survive head-truncation to decode — skip
	// and retry next cycle (ADR 0032).
	curStart, perr := time.Parse(time.RFC3339, ref.StartUTC)
	if perr != nil {
		s.mu.Unlock()
		return
	}
	dt := now.Sub(curStart.Add(SlotDuration)).Seconds()
	if dt < 0 || dt > txLateWindowSec {
		st := s.statusLocked()
		s.mu.Unlock()
		s.publish(st)
		return
	}
	// A rung already went out in this TX slot (a Start*'s immediate fireOpening
	// racing this same slot's pending OnSlot — review 2026-07-20 #2): don't
	// consume a repeat or key again; the next cycle drives the exchange. The
	// dedup is per physical slot, not per session — two transmissions in one
	// slot are impossible regardless of session identity.
	if s.lastTxSlot.Equal(curStart.Add(SlotDuration)) {
		st := s.statusLocked()
		s.mu.Unlock()
		s.publish(st)
		return
	}

	confirming := s.ex.State == txConfirming
	if !confirming {
		// Operator-armed skip: a full silent cycle on an already-sent rung
		// (repeats > 0 — never before the opening has even transmitted) ends
		// the session INSTEAD of keying the repeat — no RF at a no-show.
		if s.skipIfSilent && !advanced && s.repeats > 0 {
			s.ex = nil
			s.mode = seqIdle
			s.skipIfSilent = false
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: skip-if-silent — no reply; ending without repeat")
			s.publish(QsoStatus{Active: false})
			return
		}
		// Calling / Reporting wait for the partner; cap the repeats (off-ramp).
		if s.repeats >= s.maxRepeats {
			s.ex = nil
			s.mode = seqIdle
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: no answer after max repeats; abandoning")
			s.publish(QsoStatus{Active: false})
			return
		}
		s.repeats++
	}

	s.lastTxSlot = curStart.Add(SlotDuration)
	transmit, offset, dial := s.transmitLocked(), s.offsetHz, s.dialFreqMHz
	repeats := s.repeats
	var completed *CompletedQso
	if confirming {
		// Capture the QSO data for logging, but DO NOT clear the exchange or go
		// idle yet (review follow-up M1): the session stays in txConfirming until
		// the 73 actually transmits, so a synchronous ErrTxInFlight can retry next
		// slot, and the operator can't start a new QSO (ErrQsoInProgress) while we
		// are still keying the 73. The report fields are final at txConfirming —
		// Sent() only flips State — so reading the live exchange is correct.
		c := s.completedQsoLocked()
		completed = &c
	}
	gen := s.sessionGen
	st := s.statusLocked()
	prepareComplete, onComplete, publish := s.prepareComplete, s.onComplete, s.publish
	s.mu.Unlock()

	// Side effects outside the lock. Log every rung transmit (msg, rung, and how
	// late into our slot it fired) so an on-air failure is diagnosable from the
	// log — a successful send was previously silent.
	s.log.InfoWith().
		Str("msg", msg).
		Str("rung", rung).
		Float64("offset_hz", offset).
		Float64("dt_s", dt).
		Int("repeats", repeats).
		Msg("ft8 seq: transmitting rung")

	// Final-rung completion (review H1 + follow-up M1): the QSO logs ONLY after
	// the 73 truly transmits, and the gen guard means an Abandon/disarm that
	// superseded this session while the 73 was in flight neither logs nor
	// publishes. On success → log + idle + publish idle. On RF failure → leave
	// the exchange in txConfirming so the next slot retries the 73 (don't drop
	// the contact).
	var onDone func(ok bool)
	if completed != nil {
		c := *completed
		onDone = func(ok bool) {
			if ok && prepareComplete != nil {
				prepareComplete(&c)
			}
			s.mu.Lock()
			if s.sessionGen != gen { // superseded (abandon/disarm) — stale callback
				s.mu.Unlock()
				return
			}
			if !ok {
				s.mu.Unlock()
				s.log.WarnWith().Str("their_call", c.TheirCall).
					Msg("ft8 seq: final 73 did not transmit; will retry next slot")
				return
			}
			s.ex = nil
			s.mode = seqIdle
			// Publish the terminal state while the state lock still excludes a
			// replacement Start*. Otherwise that start can publish active first
			// and this delayed completion can overwrite it with stale idle.
			publish(QsoStatus{Active: false})
			s.mu.Unlock()
			s.log.InfoWith().Str("their_call", c.TheirCall).Msg("ft8 seq: QSO complete (73 sent)")
			if onComplete != nil {
				onComplete(c)
			}
		}
	}

	if err := transmit(msg, offset, dial, onDone); err != nil {
		// A superseded commit means Abandon (or a new session) won the race while
		// this rung was between s.mu and the Service's txMu — the session is gone
		// and idle was already published; republishing st would flash it active.
		if stderrors.Is(err, ErrTxSuperseded) {
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: rung transmit failed")
		// onDone never fired (the goroutine didn't start). ErrTxNotArmed (TX gone)
		// and ErrTxBadMessage (will never encode — review M1) are terminal. Anything
		// else (e.g. ErrTxInFlight) is transient: the state is untouched (a final
		// rung is still txConfirming), so the next slot retries.
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon()
			return
		}
		publish(st)
		return
	}
	publish(st)
	// Completion (confirming) is handled by onDone from the transmit goroutine.
}

// onSlotAnsweringFd drives an answer-a-CQ-FD exchange — the FD twin of
// onSlotAnswering (kept separate so the live standard path is untouched). The final
// rung is fdRogering (we send RR73); the QSO logs from the transmit goroutine's
// onDone only after RR73 truly transmits, with the same sessionGen guard against a
// superseding Abandon/disarm.
func (s *Sequencer) onSlotAnsweringFd(ref SlotRef, msgs []goft8.DecodedMessage, now time.Time) {
	s.mu.Lock()
	if s.fdEx == nil {
		s.mu.Unlock()
		return
	}
	if ref.Period != s.theirPeriod {
		s.mu.Unlock()
		return
	}

	var heard string
	advanced := false
	for _, m := range msgs {
		if pm := parseMessage(m.Text); pm.to == s.fdEx.OurCall && pm.from == s.fdEx.TheirCall {
			heard = m.Text
		}
		if next, ok := s.fdEx.Advance(m.Text); ok {
			*s.fdEx = next
			s.repeats = 0
			advanced = true
			break
		}
	}
	if heard != "" {
		s.log.InfoWith().Str("from_worked", heard).Bool("advanced", advanced).
			Str("now_rung", s.fdEx.State.label()).Msg("ft8 seq: FD decode from worked station")
	}
	if advanced {
		s.skipIfSilent = false // they came back — an armed skip no longer applies
	}

	msg, ok := s.fdEx.TxMessage()
	if !ok { // already done — clear defensively.
		s.fdEx = nil
		s.mode = seqIdle
		s.mu.Unlock()
		s.publish(QsoStatus{Active: false})
		return
	}
	rung := s.fdEx.State.label()

	curStart, perr := time.Parse(time.RFC3339, ref.StartUTC)
	if perr != nil {
		s.mu.Unlock()
		return
	}
	dt := now.Sub(curStart.Add(SlotDuration)).Seconds()
	if dt < 0 || dt > txLateWindowSec {
		st := s.statusLocked()
		s.mu.Unlock()
		s.publish(st)
		return
	}
	// Slot already fired (immediate fireOpening vs this slot's pending OnSlot —
	// review 2026-07-20 #2); see onSlotAnswering.
	if s.lastTxSlot.Equal(curStart.Add(SlotDuration)) {
		st := s.statusLocked()
		s.mu.Unlock()
		s.publish(st)
		return
	}

	confirming := s.fdEx.State == fdRogering
	if !confirming {
		// Operator-armed skip — see onSlotAnswering; same semantics.
		if s.skipIfSilent && !advanced && s.repeats > 0 {
			s.fdEx = nil
			s.mode = seqIdle
			s.skipIfSilent = false
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: FD skip-if-silent — no reply; ending without repeat")
			s.publish(QsoStatus{Active: false})
			return
		}
		if s.repeats >= s.maxRepeats {
			s.fdEx = nil
			s.mode = seqIdle
			s.mu.Unlock()
			s.log.InfoWith().Msg("ft8 seq: FD no answer after max repeats; abandoning")
			s.publish(QsoStatus{Active: false})
			return
		}
		s.repeats++
	}

	s.lastTxSlot = curStart.Add(SlotDuration)
	transmit, offset, dial := s.transmitLocked(), s.offsetHz, s.dialFreqMHz
	repeats := s.repeats
	var completed *CompletedQso
	if confirming {
		c := s.completedQsoFdLocked()
		completed = &c
	}
	gen := s.sessionGen
	st := s.statusLocked()
	prepareComplete, onComplete, publish := s.prepareComplete, s.onComplete, s.publish
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Int("repeats", repeats).Msg("ft8 seq: transmitting FD rung")

	var onDone func(ok bool)
	if completed != nil {
		c := *completed
		onDone = func(ok bool) {
			if ok && prepareComplete != nil {
				prepareComplete(&c)
			}
			s.mu.Lock()
			if s.sessionGen != gen { // superseded — stale callback
				s.mu.Unlock()
				return
			}
			if !ok {
				s.mu.Unlock()
				s.log.WarnWith().Str("their_call", c.TheirCall).
					Msg("ft8 seq: FD final RR73 did not transmit; will retry next slot")
				return
			}
			s.fdEx = nil
			s.mode = seqIdle
			publish(QsoStatus{Active: false}) // ordered before any replacement start
			s.mu.Unlock()
			s.log.InfoWith().Str("their_call", c.TheirCall).Msg("ft8 seq: FD QSO complete (RR73 sent)")
			if onComplete != nil {
				onComplete(c)
			}
		}
	}

	if err := transmit(msg, offset, dial, onDone); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: FD rung superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: FD rung transmit failed")
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon()
			return
		}
		publish(st)
		return
	}
	publish(st)
}

// completedQsoFdLocked snapshots an answer-a-CQ-FD contact for logging. No report
// fields — FD exchanges class+section, not an SNR report. Caller holds s.mu.
func (s *Sequencer) completedQsoFdLocked() CompletedQso {
	return CompletedQso{
		LogbookID:    s.logbookID,
		TheirCall:    s.fdEx.TheirCall,
		TheirGrid:    s.fdEx.TheirGrid,
		Class:        s.fdEx.TheirClass,
		Section:      s.fdEx.TheirSection,
		OurReport:    s.fdEx.SendSnr,    // our SNR of them → RST_SENT (FD exchanges no report)
		HasOurReport: s.fdEx.HasSendSnr, // RST_RCVD is the config default, applied at log time
		StartedAt:    s.startedAt,
		OffsetHz:     s.offsetHz,
		DialFreqMHz:  s.dialFreqMHz,
	}
}

// completedFdWorkQsoLocked snapshots a work-a-caller-FD contact for logging — their
// class/section came from the call we picked. Caller holds s.mu.
func (s *Sequencer) completedFdWorkQsoLocked() CompletedQso {
	return CompletedQso{
		LogbookID:    s.logbookID,
		TheirCall:    s.fdWork.TheirCall,
		TheirGrid:    s.fdWork.TheirGrid,
		Class:        s.fdWork.TheirClass,
		Section:      s.fdWork.TheirSection,
		OurReport:    s.fdWork.SendSnr,    // our SNR of them → RST_SENT (FD exchanges no report)
		HasOurReport: s.fdWork.HasSendSnr, // RST_RCVD is the config default, applied at log time
		StartedAt:    s.startedAt,
		OffsetHz:     s.offsetHz,
		DialFreqMHz:  s.dialFreqMHz,
	}
}

// slotStart returns the UTC 15-second slot boundary containing t, epoch-aligned to
// match SlotRefFromTime's parity convention (:00/:15/:30/:45).
func slotStart(t time.Time) time.Time {
	u := t.Unix()
	return time.Unix(u-u%slotSeconds, 0).UTC()
}

// fireOpening transmits the opening rung IMMEDIATELY if, at start time, we are
// already inside our own TX slot (parity opposite the partner's) and early enough
// that head-truncation still leaves a decodable signal (ADR 0032). Without this,
// the opening rung waits for the next qualifying OnSlot — which lands at a slot
// boundary, so a session started just after one misses the current TX slot and
// stalls a full ~30 s cycle. A no-op (the OnSlot path then drives normally) when
// we're in the partner's slot or past the late window. Called once, right after
// StartQso / StartCallCq set up state. The opening rung is always non-terminal
// (answer: "calling"; call-CQ: the CQ), so there is no completion path here.
func (s *Sequencer) fireOpening(now time.Time) {
	s.mu.Lock()
	// Resolve the opening message for the active mode.
	var msg, rung string
	switch s.mode {
	case seqAnswering:
		if s.ex == nil {
			s.mu.Unlock()
			return
		}
		m, ok := s.ex.TxMessage()
		if !ok {
			s.mu.Unlock()
			return
		}
		msg, rung = m, s.ex.State.label()
	case seqAnsweringFd:
		if s.fdEx == nil {
			s.mu.Unlock()
			return
		}
		m, ok := s.fdEx.TxMessage()
		if !ok {
			s.mu.Unlock()
			return
		}
		msg, rung = m, s.fdEx.State.label()
	case seqAnsweringT4:
		// Opening rung is t4Calling (bare calls) — always non-terminal, so the
		// no-completion contract here holds. (seqWorkingT4 is deliberately NOT fired
		// here: its sole rung is the terminal RR73, which needs an onDone completion
		// path fireOpening does not provide — it is fired by fireWorkT4 instead.)
		if s.t4Ex == nil {
			s.mu.Unlock()
			return
		}
		m, ok := s.t4Ex.TxMessage()
		if !ok {
			s.mu.Unlock()
			return
		}
		msg, rung = m, s.t4Ex.State.label()
	case seqWorkingFd:
		if s.fdWork == nil {
			s.mu.Unlock()
			return
		}
		m, ok := s.fdWork.TxMessage()
		if !ok {
			s.mu.Unlock()
			return
		}
		msg, rung = m, s.fdWork.State.label()
	case seqCalling:
		if s.caller != nil { // a contact already started — not an opening
			s.mu.Unlock()
			return
		}
		msg, rung = s.cqMessage, "calling-cq"
	case seqWorking:
		// Opening rung is our report (cqReporting) — always non-terminal, so the
		// no-completion contract here holds (RR73 is never an opening).
		if s.caller == nil {
			s.mu.Unlock()
			return
		}
		m, ok := s.caller.TxMessage()
		if !ok {
			s.mu.Unlock()
			return
		}
		msg, rung = m, s.caller.State.label()
	default:
		s.mu.Unlock()
		return
	}

	// Only the current slot of OUR parity (opposite the partner's) is transmittable,
	// and only within the late window (else too few symbols survive truncation).
	curStart := slotStart(now)
	if SlotRefFromTime(curStart).Period == s.theirPeriod {
		s.mu.Unlock()
		return // current slot is the partner's parity — leave it to OnSlot
	}
	dt := now.Sub(curStart).Seconds()
	if dt < 0 || dt > txLateWindowSec {
		s.mu.Unlock()
		return
	}

	s.repeats++
	// Mark this TX slot consumed BEFORE dropping the lock: the same slot's
	// OnSlot may already be pending (its decode was published just before this
	// session started), and without the mark it would consume a second repeat —
	// with max_repeats=1, abandoning the session while the opening is still
	// playing (review 2026-07-20 #2).
	s.lastTxSlot = curStart
	transmit, offset, dial, repeats := s.transmitLocked(), s.offsetHz, s.dialFreqMHz, s.repeats
	st := s.statusLocked()
	s.mu.Unlock()

	s.log.InfoWith().Str("msg", msg).Str("rung", rung).Float64("offset_hz", offset).
		Float64("dt_s", dt).Int("repeats", repeats).Bool("immediate", true).
		Msg("ft8 seq: transmitting rung")
	// The opening rung is always non-terminal (no completion), so no onDone.
	if err := transmit(msg, offset, dial, nil); err != nil {
		if stderrors.Is(err, ErrTxSuperseded) { // session gone; idle already published
			s.log.InfoWith().Str("msg", msg).Msg("ft8 seq: opening superseded before commit; dropped")
			return
		}
		s.log.WarnWith().Err(err).Str("msg", msg).Msg("ft8 seq: rung transmit failed")
		if stderrors.Is(err, ErrTxNotArmed) || stderrors.Is(err, ErrTxBadMessage) {
			s.Abandon() // TX gone / message can't encode — can't continue.
			return
		}
	}
	s.publish(st)
}

// statusLocked builds the QsoStatus snapshot for the active session. Caller holds s.mu.
// statusLocked decorates the per-mode status with the session-wide flags.
func (s *Sequencer) statusLocked() QsoStatus {
	st := s.statusModeLocked()
	if st.Active {
		st.SkipArmed = s.skipIfSilent
	}
	return st
}

func (s *Sequencer) statusModeLocked() QsoStatus {
	switch s.mode {
	case seqAnswering:
		if s.ex == nil {
			return QsoStatus{Active: false}
		}
		msg, _ := s.ex.TxMessage()
		st := QsoStatus{
			Active:      true,
			Role:        roleAnswerer,
			TheirCall:   s.ex.TheirCall,
			TheirGrid:   s.theirGrid,
			State:       s.ex.State.label(),
			NextMessage: msg,
			Repeats:     s.repeats,
			TheirPeriod: s.theirPeriod,
		}
		// Advertise the cap only on the rungs it governs (everything but the one-shot
		// 73), so the SPA's countdown shows iff max_repeats>0.
		if s.ex.State != txConfirming {
			st.MaxRepeats = s.maxRepeats
		}
		if s.ex.HasSendSnr {
			st.OurReport = formatReport(s.ex.SendSnr)
		}
		if s.ex.HasRcvdReport {
			st.TheirReport = formatReport(s.ex.RcvdReport)
		}
		return st
	case seqAnsweringFd:
		if s.fdEx == nil {
			return QsoStatus{Active: false}
		}
		msg, _ := s.fdEx.TxMessage()
		st := QsoStatus{
			Active:       true,
			Role:         roleAnswerer,
			Fd:           true,
			TheirCall:    s.fdEx.TheirCall,
			TheirGrid:    s.fdEx.TheirGrid,
			State:        s.fdEx.State.label(),
			NextMessage:  msg,
			Repeats:      s.repeats,
			TheirPeriod:  s.theirPeriod,
			OurClass:     s.fdEx.OurClass,
			OurSection:   s.fdEx.OurSection,
			TheirClass:   s.fdEx.TheirClass,
			TheirSection: s.fdEx.TheirSection,
		}
		// Cap governs the pre-RR73 rung only (the one-shot RR73 is uncapped).
		if s.fdEx.State != fdRogering {
			st.MaxRepeats = s.maxRepeats
		}
		return st
	case seqWorkingFd:
		if s.fdWork == nil {
			return QsoStatus{Active: false}
		}
		msg, _ := s.fdWork.TxMessage()
		st := QsoStatus{
			Active:       true,
			Role:         roleWorker,
			Fd:           true,
			TheirCall:    s.fdWork.TheirCall,
			TheirGrid:    s.fdWork.TheirGrid,
			State:        s.fdWork.State.label(),
			NextMessage:  msg,
			Repeats:      s.repeats,
			TheirPeriod:  s.theirPeriod,
			OurClass:     s.fdWork.OurClass,
			OurSection:   s.fdWork.OurSection,
			TheirClass:   s.fdWork.TheirClass,
			TheirSection: s.fdWork.TheirSection,
		}
		if s.fdWork.State != fdwRogering {
			st.MaxRepeats = s.maxRepeats
		}
		return st
	case seqAnsweringT4:
		if s.t4Ex == nil {
			return QsoStatus{Active: false}
		}
		msg, _ := s.t4Ex.TxMessage()
		st := QsoStatus{
			Active:      true,
			Role:        roleAnswerer,
			Type4:       true,
			TheirCall:   s.t4Ex.TheirCall,
			TheirGrid:   s.t4Ex.TheirGrid,
			State:       s.t4Ex.State.label(),
			NextMessage: msg,
			Repeats:     s.repeats,
			TheirPeriod: s.theirPeriod,
		}
		// Cap governs the pre-73 rung only (the one-shot 73 is uncapped).
		if s.t4Ex.State != t4Confirming {
			st.MaxRepeats = s.maxRepeats
		}
		// No report is exchanged on the wire, but OurReport carries the SNR we log as
		// RST_SENT (informational; the reduced ladder renders no report rung).
		if s.t4Ex.HasSendSnr {
			st.OurReport = formatReport(s.t4Ex.SendSnr)
		}
		return st
	case seqWorkingT4:
		if s.t4Work == nil {
			return QsoStatus{Active: false}
		}
		msg, _ := s.t4Work.TxMessage()
		st := QsoStatus{
			Active:      true,
			Role:        roleWorker,
			Type4:       true,
			TheirCall:   s.t4Work.TheirCall,
			TheirGrid:   s.t4Work.TheirGrid,
			State:       s.t4Work.State.label(),
			NextMessage: msg,
			Repeats:     s.repeats,
			TheirPeriod: s.theirPeriod,
		}
		// The sole rung (RR73) is a one-shot terminal, so it is uncapped (MaxRepeats
		// stays 0).
		if s.t4Work.HasSendSnr {
			st.OurReport = formatReport(s.t4Work.SendSnr)
		}
		return st
	case seqCalling:
		// Calling CQ: active from the first CQ. Until a station is being worked
		// (caller != nil) the rung is "calling-cq" and the next message is the CQ.
		st := QsoStatus{Active: true, Role: roleCaller, Repeats: s.repeats, TheirPeriod: s.theirPeriod}
		if s.caller != nil {
			msg, _ := s.caller.TxMessage()
			st.TheirCall = s.caller.TheirCall
			st.TheirGrid = s.caller.TheirGrid
			st.State = s.caller.State.label()
			st.NextMessage = msg
			// Cap governs the working-an-answerer rungs but not the one-shot RR73; on
			// plain calling-cq (caller==nil) it's uncapped, so MaxRepeats stays 0.
			if s.caller.State != cqRogering {
				st.MaxRepeats = s.maxRepeats
			}
			if s.caller.HasSendSnr {
				st.OurReport = formatReport(s.caller.SendSnr)
			}
			if s.caller.HasRcvdReport {
				st.TheirReport = formatReport(s.caller.RcvdReport)
			}
		} else {
			st.State = "calling-cq"
			st.NextMessage = s.cqMessage
		}
		return st
	case seqWorking:
		// Working a station that called us: caller-role ladder (report → RR73), but
		// no calling-cq phase — s.caller is always set. Role "caller" so the SPA
		// renders the same ladder as Call-CQ; the cap governs the pre-RR73 rung.
		if s.caller == nil {
			return QsoStatus{Active: false}
		}
		msg, _ := s.caller.TxMessage()
		st := QsoStatus{
			Active:      true,
			Role:        roleWorker,
			TheirCall:   s.caller.TheirCall,
			TheirGrid:   s.caller.TheirGrid,
			State:       s.caller.State.label(),
			NextMessage: msg,
			Repeats:     s.repeats,
			TheirPeriod: s.theirPeriod,
		}
		if s.caller.State != cqRogering {
			st.MaxRepeats = s.maxRepeats
		}
		if s.caller.HasSendSnr {
			st.OurReport = formatReport(s.caller.SendSnr)
		}
		if s.caller.HasRcvdReport {
			st.TheirReport = formatReport(s.caller.RcvdReport)
		}
		return st
	default:
		return QsoStatus{Active: false}
	}
}

// completedQsoLocked captures the finished exchange for logging. Caller holds s.mu.
func (s *Sequencer) completedQsoLocked() CompletedQso {
	return CompletedQso{
		LogbookID:      s.logbookID,
		TheirCall:      s.ex.TheirCall,
		TheirGrid:      s.theirGrid,
		OurReport:      s.ex.SendSnr,
		HasOurReport:   s.ex.HasSendSnr,
		TheirReport:    s.ex.RcvdReport,
		HasTheirReport: s.ex.HasRcvdReport,
		StartedAt:      s.startedAt,
		OffsetHz:       s.offsetHz,
		DialFreqMHz:    s.dialFreqMHz,
	}
}
