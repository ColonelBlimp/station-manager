package ft8

import (
	"context"
	stderrors "errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADIF ANT_PATH values the FT8 logging path uses (QsoDetails.AntPath notes only
// S and L are used). The operator's short/long radio maps onto these.
const (
	antPathShort = "S"
	antPathLong  = "L"
)

// normalizeAntPath maps an operator/SPA path value onto an ADIF ANT_PATH code.
// Accepts "L"/"long" (case-insensitive) for long path; anything else — including
// "S"/"short" and empty — is short. Lenient by design: an unrecognised value can
// only ever fall back to short, never an invalid ADIF code.
func normalizeAntPath(p string) string {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case antPathLong, "LONG":
		return antPathLong
	default:
		return antPathShort
	}
}

// FT8 transmit wiring (ADR 0030 step e1) — the daemon-reachable TX path.
//
// This is the plumbing + safety gate on top of the bench-validated step-(d)
// TxController: an explicit arm/disarm (the operator gate before any FT8 RF)
// plus a "transmit this message on the next slot" entry the SPA drives. There
// is no sequencing here — the message and offset are supplied by the caller;
// step e3 pre-fills them from the pure resolver (sequence.go).
//
// The guaranteed stop is unchanged from step (d): the controller drops PTT on
// every return path and the bridge keyer owns the hard auto-off +
// release-on-disconnect + single-flight (shared with the tune carrier). Arming
// only adds the operator-consent gate and the output-device lifecycle.

// TX error sentinels. The api package maps these to HTTP status + i18n codes
// (handler_ft8_tx.go); internal/errors chains Unwrap, so a sentinel wrapped via
// .WithErr is matchable with errors.Is up the stack.
var (
	// ErrTxUnavailable: no keyer is wired (bridge disabled), or this build has
	// no audio output (CGO-free). Transmit cannot be armed.
	ErrTxUnavailable = stderrors.New("ft8: transmit unavailable")
	// ErrTxNotReady: the rig is not connected / identity-unverified right now.
	// Retryable — the rig may be powering up or reconnecting.
	ErrTxNotReady = stderrors.New("ft8: rig not ready to transmit")
	// ErrTxNotArmed: a send was requested while disarmed.
	ErrTxNotArmed = stderrors.New("ft8: transmit not armed")
	// ErrTxInFlight: a send was requested while a transmission is already running.
	ErrTxInFlight = stderrors.New("ft8: a transmission is already in flight")
	// ErrTxSuperseded: a sequencer rung reached the transmission commit after its
	// session was abandoned or replaced (the rung is decided under the sequencer's
	// lock but committed under txMu — an Abandon in that gap finds no txCancel to
	// cancel, so the commit itself must refuse; review 2026-07-20 #1). Expected
	// during an operator Abandon — callers drop the rung quietly.
	ErrTxSuperseded = stderrors.New("ft8: transmission superseded by session change")
	// ErrTxDialUnknown: a dial source is configured but cannot report the rig's
	// frequency right now (the selected VFO has not been decoded this session).
	// The rig may still report TxReady — that checks connection and identity, not
	// frequency knowledge — so this is its own refusal: SM will not key, or log a
	// contact, on a frequency it cannot corroborate. Retryable; it clears as soon
	// as the bridge decodes the VFO.
	ErrTxDialUnknown = stderrors.New("ft8: rig frequency unknown; cannot verify the transmit frequency")
	// ErrTxBadMessage: the message is not an encodable standard FT8 message.
	ErrTxBadMessage = stderrors.New("ft8: not an encodable standard message")
	// ErrTxBadOffset: the requested TX audio offset is non-finite or sits outside
	// the usable passband (a signal at offset..offset+signalWidth must fit inside
	// the configured passband, which is itself kept <= Nyquist). The daemon owns
	// this gate because the endpoints drive real RF — it must never key a tone
	// the SPA mis-supplied (review 2026-06-19 M1). Distinct from ErrNoOffset
	// (offset 0 = "operator hasn't picked one yet").
	ErrTxBadOffset = stderrors.New("ft8: TX offset outside the usable passband")
	// ErrCallerAnswerModeUnsupported: a Call-CQ session was requested with an
	// answerer-selection mode that is configured but not yet implemented
	// (operator_pick — the pile-up stack is future work). Rejected at start so a
	// config'd operator_pick fails loudly rather than silently auto-picking the
	// first answerer (review H2 / ADR 0033).
	ErrCallerAnswerModeUnsupported = stderrors.New("ft8: caller answer mode not supported")
)

// txPlayer is the daemon's owned FT8 transmit output device. It extends the
// controller's slotPlayer (Play/Stop) with the Init/Close lifecycle the Service
// drives on arm/disarm. The real malgo playback.Player satisfies it (CGO); the
// CGO-free build has no implementation, so newTxPlayer returns ErrTxUnavailable.
type txPlayer interface {
	slotPlayer
	Init() error
	Close() error
}

// TxState is the ft8-tx SSE payload: the transmit arm/disarm + in-flight
// status the SPA renders (and the gate it reflects). Cached by the hub for
// late-subscriber replay.
type TxState struct {
	Armed        bool    `json:"armed"`
	Transmitting bool    `json:"transmitting"`
	Message      string  `json:"message,omitempty"`
	OffsetHz     float64 `json:"offset_hz,omitempty"`
	// Error is an i18n code for the last failed transmission ("" = none), so a
	// reconnecting SPA can surface it; cleared when the next transmission starts.
	Error string `json:"error,omitempty"`
}

// SetTxKeyer injects the PTT keyer (the bridge, in cmd/smd). Called once during
// daemon wiring before Start; a nil/unset keyer leaves TX unavailable.
func (s *Service) SetTxKeyer(k TxKeyer) {
	s.txMu.Lock()
	s.keyer = k
	s.txMu.Unlock()
}

// ArmTx arms (true) or disarms (false) the FT8 transmit path. Idempotent.
// Arming requires a wired, ready keyer and an available output device; it
// acquires the device and builds the slot controller. Disarming aborts any
// in-flight transmission (PTT drops) and releases the device. Disarmed at
// construction — nothing can transmit until the operator arms.
func (s *Service) ArmTx(armed bool) error {
	if armed {
		return s.armTx()
	}
	s.disarmTx(false)
	return nil
}

func (s *Service) armTx() error {
	const op errors.Op = "ft8.Service.ArmTx"

	s.txMu.Lock()
	switch {
	case s.txClosed:
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxUnavailable).WithMsg("subsystem stopped")
	case s.txArmed:
		s.txMu.Unlock()
		return nil // idempotent
	case s.keyer == nil:
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxUnavailable).WithMsg("no keyer wired")
	case !s.keyer.TxReady():
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxNotReady)
	}

	player, err := s.newPlayer(s.txDeviceSpec())
	if err != nil {
		s.txMu.Unlock()
		// newPlayer already wraps ErrTxUnavailable on the CGO-free build.
		return errors.New(op).WithErr(err).WithMsg("create tx player")
	}
	if err := player.Init(); err != nil {
		_ = player.Close()
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxUnavailable).WithMsg("init tx player: " + err.Error())
	}

	s.txDevice = player
	s.txCtrl = NewTxController(s.keyer, player, s.txMode(), s.log)
	s.txCtrl.SetPreKeyCheck(s.preKeyDialCheck)
	// Record each keyed slot so decodeLoop skips occupancy for it (the slot's
	// captured audio is our own TX — see markTxSlot / the self-decode filter),
	// and write the decode-log TX line — HERE, not at commit, so the log records
	// only transmissions whose PTT actually keyed (review 2026-07-20 #6).
	s.txCtrl.onTransmit = func(boundary time.Time) {
		s.markTxSlot(boundary)
		s.writeTxLogLine()
	}
	s.txArmed = true
	s.txLastErr = ""
	s.txMu.Unlock()

	s.log.InfoWith().Str("mode", s.txMode()).Msg("ft8 tx: armed")
	s.publishTxState()
	return nil
}

// markTxSlot records a slot boundary the TX controller just keyed (wired via
// txCtrl.onTransmit, invoked only AFTER PTT engages — a failed key must not mark
// the slot, or decodeLoop would skip the occupancy of a genuine RX slot).
// decodeLoop consults wasTxSlot to skip decode + occupancy for the slot: its
// captured audio is our own signal. A ring so a second TX keyed close behind
// can't evict the first before decodeLoop processes it.
func (s *Service) markTxSlot(boundary time.Time) {
	utc := SlotRefFromTime(boundary).StartUTC
	s.txSlotMu.Lock()
	s.txSlots[s.txSlotIx] = utc
	s.txSlotIx = (s.txSlotIx + 1) % len(s.txSlots)
	s.txSlotMu.Unlock()
}

// writeTxLogLine emits the JTDX ALL.TXT Transmitting line for the in-flight
// transmission. Wired into txCtrl.onTransmit, so it runs only once PTT has
// actually keyed (review 2026-07-20 #6) — a cancelled wait or failed key writes
// nothing, and the timestamp is the real key time rather than commit time
// (up to ~15 s early for a manual next-slot CQ). Runs on the TX goroutine with
// txMu free. nil-safe no-op when the decode log is off.
func (s *Service) writeTxLogLine() {
	s.txMu.Lock()
	msg, offset, dial := s.txMessage, s.txOffsetHz, s.txDialMHz
	s.txMu.Unlock()
	s.decLog.Load().WriteTx(time.Now().UTC(), dial, offset, msg)
}

// wasTxSlot reports whether startUTC is one of the recent slots we transmitted in.
func (s *Service) wasTxSlot(startUTC string) bool {
	if startUTC == "" {
		return false
	}
	s.txSlotMu.Lock()
	defer s.txSlotMu.Unlock()
	for _, u := range s.txSlots {
		if u == startUTC {
			return true
		}
	}
	return false
}

// disarmTx tears down the TX path: aborts any in-flight transmission, drains
// the TX goroutine, and closes the output device. closing=true also latches the
// subsystem so it can never be re-armed (used by Stop). Idempotent. Also abandons
// any active sequenced QSO (ADR 0031 off-ramp: disarm aborts the contact).
func (s *Service) disarmTx(closing bool) {
	// seqGate: clear the armed state AND abandon the sequencer atomically w.r.t.
	// a concurrent StartQso/StartCallCq (review M3), so a start can't slip an
	// active session in between. Order seqGate → txMu.
	s.seqGate.Lock()
	defer s.seqGate.Unlock()

	s.txMu.Lock()
	if closing {
		s.txClosed = true
	}
	if !s.txArmed && s.txCancel == nil {
		s.txMu.Unlock() // idle: nothing to tear down (txClosed already latched above)
		// Still abandon under the gate: a session could be active from a start
		// that raced an earlier disarm; clear it now that armed is false.
		if s.seq != nil {
			s.seq.Abandon()
		}
		return
	}
	wasArmed := s.txArmed
	s.txArmed = false
	if s.txCancel != nil {
		s.txCancel() // abort in-flight; controller drops PTT on the cancel path
	}
	dev := s.txDevice
	s.txDevice = nil
	s.txCtrl = nil
	s.txMu.Unlock()

	// Armed state is now false (under the gate); abandon the sequencer. A start
	// blocked on seqGate will observe txArmed=false and refuse to commit.
	if s.seq != nil {
		s.seq.Abandon()
	}

	// Wait for the in-flight transmission (if any) to return BEFORE closing the
	// device — outside txMu so the TX goroutine can clear its own state. The
	// bridge auto-off backstop guarantees PTT is down regardless.
	s.txWg.Wait()

	if dev != nil {
		_ = dev.Stop()
		_ = dev.Close()
	}
	if wasArmed {
		s.log.InfoWith().Msg("ft8 tx: disarmed")
	}
	s.publishTxState()
}

// validateTxOffset is the daemon-owned guard on the TX audio offset, used by
// every endpoint that can key the rig (TransmitNext + the three sequenced
// starts) — the SPA's localStorage value is NOT a sufficient guard for a
// hardware-facing API (review 2026-06-19 M1). offset 0 stays ErrNoOffset
// ("operator hasn't picked one"); a non-finite value or one whose signal
// (offset..offset+signalWidthHz) doesn't fit inside the resolved occupancy
// passband is ErrTxBadOffset. The passband is config-validated to <= Nyquist
// (M3), so fitting inside it also keeps the modulator below the alias limit.
func (s *Service) validateTxOffset(op errors.Op, offsetHz float64) error {
	if math.IsNaN(offsetHz) || math.IsInf(offsetHz, 0) {
		return errors.New(op).WithErr(ErrTxBadOffset).WithMsg("offset_hz is not a finite number")
	}
	if offsetHz <= 0 {
		return errors.New(op).WithErr(ErrNoOffset)
	}
	var occCfg *types.Ft8OccupancyConfig
	if s.cfg.TX != nil {
		occCfg = s.cfg.TX.Occupancy
	}
	occ := resolveOccupancyConfig(occCfg)
	low, high := float64(occ.PassbandLowHz), float64(occ.PassbandHighHz)
	if offsetHz < low || offsetHz+float64(signalWidthHz) > high {
		return errors.New(op).WithErr(ErrTxBadOffset).WithMsgf(
			"offset_hz %.0f outside usable passband [%d, %d] (signal width %d Hz)",
			offsetHz, occ.PassbandLowHz, occ.PassbandHighHz, signalWidthHz)
	}
	return nil
}

// TransmitNext queues one standard FT8 message to transmit on the next UTC
// slot. Refused unless armed, idle (no transmission in flight), and the message
// encodes. Returns immediately — the transmission runs in a tracked goroutine
// (it blocks up to a slot waiting for the boundary, then ~12.6 s of audio), and
// its progress/outcome rides the ft8-tx SSE event. PTT is guaranteed down on
// every path (controller deferred unkey + bridge auto-off).
func (s *Service) TransmitNext(message string, offsetHz float64) error {
	const op errors.Op = "ft8.Service.TransmitNext"

	if err := s.validateTxOffset(op, offsetHz); err != nil {
		return err
	}
	// Validate encodability synchronously so a bad message is an immediate
	// error, not an async failure after the (up to 15 s) slot wait.
	if _, err := EncodeToSlot(message, offsetHz, txNominalDtSec); err != nil {
		return errors.New(op).WithErr(ErrTxBadMessage).WithMsg(err.Error())
	}
	// A manual send and a sequenced session must be mutually exclusive. They share
	// only the single-flight (txInFlight) guard, which is false BETWEEN a session's
	// rungs — so without this a manual message could key mid-exchange, and the
	// reverse (a session started while this is queued) would burn its opening rung
	// on ErrTxInFlight while the manual message still went out. seqGate makes the
	// Active() check atomic w.r.t. the session-start paths (which hold seqGate and
	// refuse symmetrically on txInFlight via sessionTxGate); it is held across
	// startTransmission so the no-session decision and the txInFlight commit can't
	// be split by a concurrent StartQso. (StartQso already drives startTransmission
	// under seqGate via fireOpening, so this nesting is the established order.)
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	if s.seq.Active() {
		return errors.New(op).WithErr(ErrQsoInProgress)
	}
	// Boundary-aligned: TransmitSlot waits for the next UTC slot and starts at the
	// nominal +0.5 s — right for a manually-initiated CQ (we pick our own slot/
	// parity, so we start on time with no truncation). dialMHz 0 — a manual transmit
	// has no session dial, so the decode-log TX line omits the band.
	// nil commitOK — a manual transmit has no sequencer session to validate, and
	// nil onDialRefusal for the same reason: there is no session to retire, and a
	// stale one from an earlier exchange must not be touched.
	return s.startTransmission(message, offsetHz, 0, nil, func(ctx context.Context, ctrl *TxController) error {
		return ctrl.TransmitSlot(ctx, message, offsetHz)
	}, nil, nil)
}

// seqTransmit transmits a sequencer rung in the CURRENT slot on the synchronised
// timebase (ADR 0032) — the reply lands in the slot opposite the worked station,
// head-truncated if the decode landed past the slot's nominal +0.5 s start. Used
// only by the Sequencer; the late-window guard is the sequencer's. Shares the arm
// gate + single-flight + ft8-tx status with TransmitNext.
// onDone (optional) fires from the transmit goroutine once the transmission
// finishes, with ok=true only on actual success (a cancel from disarm/stop is
// ok=false). The sequencer uses it to log a completed QSO only after the final
// rung truly transmitted — never on "queued" alone (review H1).
func (s *Service) seqTransmit(message string, offsetHz, dialMHz float64, gen uint64, onDone func(ok bool)) error {
	const op errors.Op = "ft8.Service.seqTransmit"
	if _, err := EncodeWaveform(message, offsetHz); err != nil {
		return errors.New(op).WithErr(ErrTxBadMessage).WithMsg(err.Error())
	}
	// THE INVARIANT: an FT8 exchange lives on one dial frequency. The session
	// pinned one when it started; if the rig has moved off it, the partner is no
	// longer in our passband and the contact is over. Keying anyway transmits at a
	// station that is not there, and the QSO would be logged on the frequency we
	// left (the sequencer carries the pinned dial into the completed contact).
	//
	// Checked HERE — the single funnel every rung passes through — rather than by
	// reacting to an observed dial transition somewhere upstream. Reacting is what
	// made this whack-a-mole: a rule that fired on a moved capture slot ended
	// whatever session was active when that slot was PROCESSED, killing a valid
	// session started on the new dial in between (codex P1 on c6b8a15d), and any
	// missed or mis-timed transition left a hole. A session pinned to the new dial
	// simply matches here, so there is nothing left to get wrong.
	//
	// Both sides come from the SAME reader, so exact comparison is correct; the
	// client-supplied dialMHz carried for logging is NOT comparable. Refuse only
	// when both readings are known — an unreadable dial is the keyer's business
	// (TxReady shares that precondition), not a new way to block TX.
	s.txMu.Lock()
	pinned := s.sessionDialMHz
	s.txMu.Unlock()
	cur, tracked, known := s.dialState()
	// With a source installed the rung must be POSITIVELY validated: an unknown
	// reading on either side is a refusal, not a pass. Untracked (no CAT) stays
	// inert — that deployment cannot key at all.
	if tracked && (!known || pinned == 0 || cur != pinned) {
		// Refusing RF must never un-make a contact that already happened. Run the
		// rung's completion policy FIRST: a Group A final rung records the QSO on
		// either outcome and retires the session itself, and every callback is
		// generation-guarded — so abandoning first would bump the generation and
		// the callback would refuse, silently discarding a completed contact
		// (codex P1 on a76f1f61).
		if onDone != nil {
			onDone(false)
		}
		// Then end the session if it is still live. The generation check makes
		// this self-selecting: after a Group A completion the generation has moved
		// on and this is a no-op, while a Group B callback (which does not log or
		// retire on failure) leaves it to fire here. It also guarantees a rung can
		// never end a session that replaced it.
		s.seq.AbandonIfCurrent(gen, "rig dial no longer matches the session's frequency")
		// Callers already treat this sentinel as "session gone; idle already
		// published" and return without re-firing onDone, so no new error code
		// reaches the SPA and no completion runs twice.
		return errors.New(op).WithErr(ErrTxSuperseded)
	}
	// commitOK re-validates the rung's session generation under txMu, closing the
	// unlock→commit gap an Abandon can land in (review 2026-07-20 #1; see
	// ErrTxSuperseded and the startTransmission commit section).
	commitOK := func() bool { return s.seq.isCurrent(gen) }
	return s.startTransmission(message, offsetHz, dialMHz, commitOK, func(ctx context.Context, ctrl *TxController) error {
		return ctrl.TransmitCurrentSlot(ctx, message, offsetHz)
	}, onDone, func() {
		// Generation-scoped so a refusal belonging to this rung can never end a
		// session that replaced it.
		s.seq.AbandonIfCurrent(gen, "rig dial no longer confirmed at keying time")
	})
}

// startTransmission runs one transmission through the armed controller under the
// single-flight guard, in a tracked goroutine. fn is the controller call —
// next-slot TransmitSlot for a manual CQ, or current-slot TransmitCurrentSlot for
// a sequencer rung. Returns synchronously with ErrTxNotArmed / ErrTxInFlight if it
// can't start; the transmission's progress/outcome rides the ft8-tx SSE. PTT is
// guaranteed down on every path (controller deferred-unkey + bridge auto-off).
func (s *Service) startTransmission(
	message string,
	offsetHz, dialMHz float64,
	commitOK func() bool,
	fn func(ctx context.Context, ctrl *TxController) error,
	onDone func(ok bool),
	onDialRefusal func(),
) error {
	const op errors.Op = "ft8.Service.startTransmission"

	base := s.base() // daemon lifecycle ctx (taken before txMu — no lock nesting)

	s.txMu.Lock()
	if !s.txArmed {
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxNotArmed)
	}
	if s.txInFlight {
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxInFlight)
	}
	// Re-check LIVE rig readiness, not just the sticky armed flag (review M1):
	// the rig can disconnect or lose identity verification between ArmTx and now,
	// in which case the bridge would refuse to key anyway. Fail fast here so we
	// don't launch a TX goroutine (and, for a sequencer rung, keep a session
	// alive) against an unready rig.
	if s.keyer == nil || !s.keyer.TxReady() {
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxNotReady)
	}
	// Dial knownness goes LAST, after armed / in-flight / readiness — the same
	// precedence sessionTxGate uses. Checking it earlier (it was at the top of
	// TransmitNext) meant a disarmed send with an unreadable dial reported
	// rig_dial_unknown instead of the documented not-armed conflict, and masked
	// in-flight conflicts too (codex P2 on 0d180e59). This is the fast synchronous
	// refusal; the guarantee is preKeyDialCheck, which re-runs at the moment of
	// keying — this one only avoids accepting a send that is already doomed.
	if _, tracked, known := s.dialState(); tracked && !known {
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxDialUnknown)
	}
	// Session-generation commit gate (review 2026-07-20 #1), checked WHILE
	// HOLDING txMu so it is atomic with the txCancel registration below:
	// AbandonQso bumps the generation first and reads txCancel under txMu
	// second, so a stale rung either sees the bumped generation here (refused)
	// or registered txCancel before Abandon's read (cancelled) — there is no
	// third interleaving. Lock order txMu→seq.mu; nothing takes them reversed.
	if commitOK != nil && !commitOK() {
		s.txMu.Unlock()
		return errors.New(op).WithErr(ErrTxSuperseded)
	}
	ctrl := s.txCtrl
	txCtx, cancel := context.WithCancel(base)
	s.txCancel = cancel
	s.txInFlight = true
	s.txMessage = message
	s.txOffsetHz = offsetHz
	// dialMHz is the accepted session's dial, passed in by the caller (0 for a
	// manual transmit) — stashed for the decode-log TX line, which is written
	// from onTransmit only once PTT actually keys (review 2026-07-20 #6: at
	// commit, a manual CQ hasn't keyed for up to ~15 s, and a cancelled or
	// failed key would still have logged a Transmitting line).
	s.txDialMHz = dialMHz
	s.txLastErr = ""

	// Launch UNDER txMu (review 2026-06-19 M1): GoTracked does txWg.Add(1)
	// synchronously, so doing it while we still hold txMu means a concurrent
	// disarmTx — which reads txCancel/txInFlight under txMu, then txWg.Wait
	// outside it — cannot pass Wait with a zero counter in the window before the
	// goroutine is counted. The goroutine's cleanup re-takes txMu, so it parks
	// until we Unlock below; fn itself needs no lock. (Mirrors the
	// hold-the-lock-across-Add pattern in internal/lookup/refresher.)
	safego.GoTracked(txCtx, "ft8.tx", s.onPanic, func() {
		defer cancel() // release the ctx on every exit (runs last)

		var txErr error
		normal := false
		// Cleanup runs on EVERY exit, INCLUDING a panic that safego recovers
		// (normal stays false then). safego recovers panics outside fn's body, so
		// without this defer the post-fn cleanup would be skipped on a panic,
		// leaving txInFlight/txCancel set and the TX path wedged in-flight until a
		// daemon restart (review 2026-06-19 H1).
		defer func() {
			// A cancel (disarm / daemon stop) is a normal stop, not a failure; a
			// panic (normal=false) or any non-cancel error is.
			failed := !normal || (txErr != nil && !stderrors.Is(txErr, context.Canceled))
			s.txMu.Lock()
			s.txInFlight = false
			s.txMessage = ""
			s.txOffsetHz = 0
			s.txDialMHz = 0
			s.txCancel = nil
			if failed {
				s.txLastErr = "ft8_tx_failed"
			}
			s.txMu.Unlock()

			switch {
			case !normal:
				s.log.ErrorWith().Msg("ft8 tx: transmission panicked (recovered); in-flight state cleared")
			case failed:
				s.log.WarnWith().Err(txErr).Msg("ft8 tx: transmission failed")
			}
			s.publishTxState() // done / failed

			// Completion callback (final-rung QSO logging): ok only on a clean
			// success — a cancel, error, or panic means the final transmission did
			// NOT complete on air, so the QSO must not be logged.
			if onDone != nil {
				onDone(normal && txErr == nil)
			}
			// The pre-key gate refuses INSIDE this goroutine, long after
			// seqTransmit returned, so its caller never sees the error and the
			// synchronous "refuse, then retire the session" policy cannot run. A
			// failed frequency confirmation must still end the session (invariant
			// 5): otherwise the exchange lingers — consuming slots, blocking a new
			// session, and resuming if the dial happens to come back — until the
			// NEXT rung's synchronous check catches it (codex P1 on e0207074).
			//
			// Ordering is the whole point: strictly AFTER onDone. Every completion
			// callback is generation-guarded, so retiring first makes a Group A
			// contact's callback refuse and the QSO vanish — the same trap as
			// a76f1f61. The retirement itself is generation-scoped by the caller.
			if onDialRefusal != nil && isDialRefusal(txErr) {
				onDialRefusal()
			}
		}()

		txErr = fn(txCtx, ctrl)
		normal = true
	}, false, &s.txWg)

	s.txMu.Unlock()
	s.publishTxState() // transmitting
	return nil
}

// sessionTxGate verifies the shared preconditions for STARTING a sequenced
// session. Callers hold seqGate — which makes this atomic w.r.t. a manual
// TransmitNext (that path gates on seqGate + the sequencer's Active() state) —
// and must NOT already hold txMu.
//
// Precedence — armed → active session → in-flight → ready — and the ORDER is
// load-bearing. The classification checks (active session, in-flight) MUST come
// BEFORE the ready check, because the production keyer reports TxReady()==false
// while it is keying (bridge.Service.TxReady folds in tuneActive/ft8TxActive/
// txUncertain — single-flight holds the rig). If the ready check ran first, a
// duplicate start during the keyed portion of a rung — most of a live slot —
// would surface ErrTxNotReady (503) instead of the correct conflict code:
//   - armed: the operator-consent gate before any FT8 RF.
//   - active session: a session already owns the TX path, so a duplicate start is
//     ErrQsoInProgress — the same code the sequencer's own mode guard returns,
//     classified here so it is returned whether or not a rung is currently keyed.
//   - in-flight (no session): a manual TransmitNext, or a just-finished session's
//     draining tail. txInFlight is shared by manual sends and rungs and is false
//     BETWEEN a session's rungs, so without this a session could start under a
//     queued manual send and its opening rung would collide (ErrTxInFlight) and
//     burn a repeat/slot while the manual message still keyed. Either case is live
//     RF a new session must not start atop; ErrTxInFlight is accurate for both.
//   - ready: LIVE rig readiness, not just the sticky armed flag (review M1) —
//     with nothing keying, refuse to commit (and publish) a session the rig can no
//     longer key rather than returning 202 and letting the sequencer spin.
//
// Returns a wrapped sentinel on refusal, nil to proceed.
func (s *Service) sessionTxGate(op errors.Op) error {
	s.txMu.Lock()
	armed := s.txArmed
	inFlight := s.txInFlight
	ready := s.keyer != nil && s.keyer.TxReady()
	s.txMu.Unlock()
	if !armed {
		return errors.New(op).WithErr(ErrTxNotArmed)
	}
	// Active session / in-flight classified before the ready check: the keyer is
	// deliberately not-ready while keying, so ready-first would mask these as
	// ErrTxNotReady during a live rung/send (review: duplicate-during-keyed-rung).
	if s.seq.Active() {
		return errors.New(op).WithErr(ErrQsoInProgress)
	}
	if inFlight {
		return errors.New(op).WithErr(ErrTxInFlight)
	}
	if !ready {
		return errors.New(op).WithErr(ErrTxNotReady)
	}
	// Every session start funnels through here, under seqGate — so this is the one
	// place to pin the dial the DAEMON itself reads. Read before taking txMu so no
	// new lock nesting appears (dialSource reaches into the bridge). A start that
	// is subsequently rejected leaves the value stale and unread: the guard in
	// seqTransmit only consults it while a session is active, and the next start
	// overwrites it — the same reasoning setPendingLogbook already relies on.
	dial, tracked, known := s.dialState()
	if tracked && !known {
		// Refuse up front rather than committing a session that could never
		// validate a rung — the operator gets one clear reason instead of a
		// silent session that never transmits.
		return errors.New(op).WithErr(ErrTxDialUnknown)
	}
	s.txMu.Lock()
	s.sessionDialMHz = dial
	s.txMu.Unlock()
	return nil
}

// dialState reports the daemon's OWN view of the rig dial. Deliberately distinct
// from the dialFreqMHz the client supplies for LOGGING: that value took a
// different path and the two must never be compared against each other.
//
// tracked and known are SEPARATE facts and conflating them disables the safety
// invariant exactly when it is needed (codex P1 on a76f1f61). "No dial source"
// means no CAT, and FT8 cannot key without a writable rig, so there is nothing to
// protect. "Source installed but the reading is unavailable" is different: the
// bridge reports TxReady on connection + identity, which does NOT require the
// selected VFO's frequency to have been decoded — so the rig can be ready to key
// while the daemon cannot say what it is tuned to. That must not authorise RF.
// Same distinction as Slot.DialTracked on the receive side.
func (s *Service) dialState() (mhz float64, tracked, known bool) {
	s.mu.Lock()
	src := s.dialSource
	s.mu.Unlock()
	if src == nil {
		return 0, false, false
	}
	m, ok := src()
	return m, true, ok
}

// StartQso begins a manual answer-a-CQ exchange (ADR 0031): the operator picked
// the worked station (theirCall/theirGrid, from a CQ heard in the slot at
// theirSlotUTC) and a clear offset. Requires TX **armed** — the sequencer keys
// through the armed controller. ourCall/ourGrid are the station identity the api
// layer resolved from config.
func (s *Service) StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	const op errors.Op = "ft8.Service.StartQso"
	if err := s.validateTxOffset(op, offsetHz); err != nil {
		return err
	}
	// seqGate: armed-check + sequencer commit are atomic w.r.t. disarm (M3).
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	if err := s.sessionTxGate(op); err != nil {
		return err
	}
	// The decode-log TX dial comes from the sequencer's accepted session (threaded
	// through seqTransmit), so a rejected start here can't relabel an active rung.
	// Antenna path: consume BEFORE the start, restore on rejection (review
	// 2026-07-20 round 12 #3). seq.StartQso publishes the active session and can
	// fire the opening before it returns, so a post-accept reset raced any path
	// selection made for the brand-new exchange — consuming first means the path
	// is at its default before the session is ever visible. A rejected start
	// (double-start, operator error) restores the active exchange's choice; see
	// restoreExchangePath for the accepted residual on that rare path.
	prevPath, prevGen := s.consumeExchangePath()
	// Stage the arm-time logbook to the session BEFORE the start (ADR 0055): each
	// seq.Start* consumes it into s.logbookID under s.mu, ATOMICALLY with mode
	// activation, and stamps it onto the CompletedQso. Staging-before-activation is
	// the fix for the terminal-first-rung race — a post-start bind left a gap in which
	// StartWorkCallerT4's sole RR73 could complete and snapshot a stale/zero logbook.
	// A rejected start (ErrQsoInProgress) leaves the staged value unconsumed; the next
	// start overwrites it (all serialised by seqGate), so no restore is needed here.
	s.seq.setPendingLogbook(logbookID)
	s.seq.setPendingAllowDuplicate(allowDuplicate)
	if err := s.seq.StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC, offsetHz, dialFreqMHz, time.Now().UTC()); err != nil {
		s.restoreExchangePath(prevPath, prevGen)
		return err
	}
	return nil
}

// StartQsoFd begins a manual answer-a-CQ-FD exchange (ARRL Field Day, search &
// pounce): the operator picked a station calling CQ FD. Our Field Day identity
// (class + section) is daemon config (ft8.field_day), not client-supplied — mirroring
// how StartCallCq reads the answer mode — and a missing identity is refused up front.
// Requires TX armed, same as StartQso.
func (s *Service) StartQsoFd(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	const op errors.Op = "ft8.Service.StartQsoFd"
	if err := s.validateTxOffset(op, offsetHz); err != nil {
		return err
	}
	var class, section string
	if s.cfg.FieldDay != nil {
		class, section = s.cfg.FieldDay.Class, s.cfg.FieldDay.Section
	}
	if strings.TrimSpace(class) == "" || strings.TrimSpace(section) == "" {
		return errors.New(op).WithErr(ErrFdIdentityUnset)
	}
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	if err := s.sessionTxGate(op); err != nil {
		return err
	}
	// Antenna path: consume before the start, restore on rejection — see StartQso.
	prevPath, prevGen := s.consumeExchangePath()
	// Stage the arm-time logbook before the start (ADR 0055) — see StartQso.
	s.seq.setPendingLogbook(logbookID)
	s.seq.setPendingAllowDuplicate(allowDuplicate)
	if err := s.seq.StartQsoFd(ourCall, class, section, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, time.Now().UTC()); err != nil {
		s.restoreExchangePath(prevPath, prevGen)
		return err
	}
	return nil
}

// StartCallCq begins a sequenced Call-CQ session (ADR 0033): we call CQ in our slot
// parity and work the stations that answer, one at a time, looping until AbandonQso.
// Requires TX **armed** — the sequencer keys through the armed controller. The
// answerer-selection mode is read from ft8.tx.caller_answer_mode (default auto_first).
// ourCall/ourGrid are the station identity the api layer resolved from config;
// offsetHz is our TX offset; dialFreqMHz is the rig dial for the logged QSO frequency.
func (s *Service) StartCallCq(ourCall, ourGrid string, offsetHz, dialFreqMHz float64, txParity string, logbookID int64) error {
	const op errors.Op = "ft8.Service.StartCallCq"
	if err := s.validateTxOffset(op, offsetHz); err != nil {
		return err
	}
	// Reject operator_pick until the pile-up stack exists (review H2): the
	// sequencer would otherwise silently auto-pick the first answerer, which is
	// NOT what operator_pick promises. Fail loudly so a config typo is visible.
	mode := types.ResolveFt8CallerAnswerMode(s.cfg.TX)
	if mode == types.Ft8CallerAnswerOperatorPick {
		return errors.New(op).WithErr(ErrCallerAnswerModeUnsupported).
			WithMsg("operator_pick answerer selection is not yet implemented; use auto_first")
	}
	// seqGate: armed-check + sequencer commit are atomic w.r.t. disarm (M3).
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	if err := s.sessionTxGate(op); err != nil {
		return err
	}
	// Antenna path: consume before the start, restore on rejection — see StartQso.
	prevPath, prevGen := s.consumeExchangePath()
	// Stage the arm-time logbook before the start (ADR 0055) — see StartQso.
	s.seq.setPendingLogbook(logbookID)
	// A Call-CQ run works whoever answers, so there is no per-station repeat
	// intent to express — stage FALSE explicitly so a flag left over from a
	// previous per-station start cannot leak into this session.
	s.seq.setPendingAllowDuplicate(false)
	if err := s.seq.StartCallCq(ourCall, ourGrid, offsetHz, dialFreqMHz, mode, txParity, time.Now().UTC()); err != nil {
		s.restoreExchangePath(prevPath, prevGen)
		return err
	}
	return nil
}

// StartWorkCaller begins working a station that is calling us (ADR 0033 "work a
// caller"): the operator picked theirCall/theirGrid from a decode directed at our
// call ("<ourCall> <theirCall> <grid>"), heard in the slot at theirSlotUTC; theirSnr
// is our SNR of that signal (the report we send back). Requires TX **armed** — the
// sequencer keys through the armed controller. ourCall is the station identity the
// api layer resolved from config.
func (s *Service) StartWorkCaller(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	const op errors.Op = "ft8.Service.StartWorkCaller"
	if err := s.validateTxOffset(op, offsetHz); err != nil {
		return err
	}
	// seqGate: armed-check + sequencer commit are atomic w.r.t. disarm (M3).
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	if err := s.sessionTxGate(op); err != nil {
		return err
	}
	// Antenna path: consume before the start, restore on rejection — see StartQso.
	prevPath, prevGen := s.consumeExchangePath()
	// Stage the arm-time logbook before the start (ADR 0055) — see StartQso.
	s.seq.setPendingLogbook(logbookID)
	s.seq.setPendingAllowDuplicate(allowDuplicate)
	if err := s.seq.StartWorkCaller(ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, time.Now().UTC()); err != nil {
		s.restoreExchangePath(prevPath, prevGen)
		return err
	}
	return nil
}

// StartWorkCallerFd begins working a station that called us with a Field Day exchange
// (the FD twin of StartWorkCaller): the operator picked "<ourCall> <theirCall> <class>
// <section>". theirClass/theirSection are parsed by the api layer from that decode; OUR
// class/section come from ft8.field_day config (not client-supplied). Requires TX armed.
func (s *Service) StartWorkCallerFd(ourCall, theirCall, theirGrid, theirClass, theirSection string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	const op errors.Op = "ft8.Service.StartWorkCallerFd"
	if err := s.validateTxOffset(op, offsetHz); err != nil {
		return err
	}
	var class, section string
	if s.cfg.FieldDay != nil {
		class, section = s.cfg.FieldDay.Class, s.cfg.FieldDay.Section
	}
	if strings.TrimSpace(class) == "" || strings.TrimSpace(section) == "" {
		return errors.New(op).WithErr(ErrFdIdentityUnset)
	}
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	if err := s.sessionTxGate(op); err != nil {
		return err
	}
	// Antenna path: consume before the start, restore on rejection — see StartQso.
	prevPath, prevGen := s.consumeExchangePath()
	// Stage the arm-time logbook before the start (ADR 0055) — see StartQso.
	s.seq.setPendingLogbook(logbookID)
	s.seq.setPendingAllowDuplicate(allowDuplicate)
	if err := s.seq.StartWorkCallerFd(ourCall, class, section, theirCall, theirGrid, theirClass, theirSection,
		theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, time.Now().UTC()); err != nil {
		s.restoreExchangePath(prevPath, prevGen)
		return err
	}
	return nil
}

// StartQsoT4 begins a reduced type-4 answer-a-CQ exchange (ADR 0048): the operator picked
// a station with a NONSTANDARD/compound call (e.g. "CQ PJ4/NA2AA"), which cannot walk the
// standard grid/report ladder. theirSnr is our SNR of their CQ (logged as RST_SENT, since
// type-4 exchanges no report on the air). Needs no config identity — our own call is
// standard. Requires TX armed, same gating as StartQso.
func (s *Service) StartQsoT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	const op errors.Op = "ft8.Service.StartQsoT4"
	if err := s.validateTxOffset(op, offsetHz); err != nil {
		return err
	}
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	if err := s.sessionTxGate(op); err != nil {
		return err
	}
	// Antenna path: consume before the start, restore on rejection — see StartQso.
	prevPath, prevGen := s.consumeExchangePath()
	// Stage the arm-time logbook before the start (ADR 0055) — see StartQso.
	s.seq.setPendingLogbook(logbookID)
	s.seq.setPendingAllowDuplicate(allowDuplicate)
	if err := s.seq.StartQsoT4(ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, time.Now().UTC()); err != nil {
		s.restoreExchangePath(prevPath, prevGen)
		return err
	}
	return nil
}

// StartWorkCallerT4 begins working a NONSTANDARD/compound station that called us (the
// type-4 twin of StartWorkCaller, ADR 0048): the operator picked a bare directed call
// ("<ourCall> <theirCall>") whose sender's call is nonstandard. theirSnr is our SNR of it
// (RST_SENT). Needs no config identity. Requires TX armed.
func (s *Service) StartWorkCallerT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	const op errors.Op = "ft8.Service.StartWorkCallerT4"
	if err := s.validateTxOffset(op, offsetHz); err != nil {
		return err
	}
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	if err := s.sessionTxGate(op); err != nil {
		return err
	}
	// Antenna path: consume before the start, restore on rejection — see StartQso.
	prevPath, prevGen := s.consumeExchangePath()
	// Stage the arm-time logbook before the start (ADR 0055) — see StartQso.
	s.seq.setPendingLogbook(logbookID)
	s.seq.setPendingAllowDuplicate(allowDuplicate)
	if err := s.seq.StartWorkCallerT4(ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, time.Now().UTC()); err != nil {
		s.restoreExchangePath(prevPath, prevGen)
		return err
	}
	return nil
}

// SetQsoLogger injects the sink that logs a completed FT8 exchange (ADR 0029
// step e4) — the daemon (cmd/smd) wires it to qsoservice. Called once during
// wiring, before Start. A nil logger (e.g. tests) means completed exchanges are
// not logged, only emitted on the SSE. internal/ft8 stays free of qsoservice /
// config / adif: the assembly + submit live in the injected sink.
func (s *Service) SetQsoLogger(fn func(ctx context.Context, c CompletedQso)) {
	s.qsoLogger = fn
}

// SetDecodeSink injects an observer called once per slot with that slot's decodes
// (the PSK Reporter uploader). Set once during wiring, before Start. Like
// SetQsoLogger, it keeps internal/ft8 free of the consumer (one-way import via DI).
func (s *Service) SetDecodeSink(fn func(DecodeReport)) {
	s.decodeSink = fn
}

// SetExchangePath records the operator's antenna-path choice for the active
// exchange — "S"/"short" or "L"/"long" (case-insensitive). Logging-only: it
// annotates the QSO the exchange logs (ADIF ANT_PATH + the short/long
// bearing+distance, stamped in BuildQso) and never touches the on-air signal.
// Settable any time during a contact (the SPA's short/long radio POSTs here);
// read once when the exchange completes. Defaults to short and resets to short
// at the start of each new exchange, so a prior contact's "long" never carries
// over.
func (s *Service) SetExchangePath(path string) {
	p := normalizeAntPath(path)
	s.txMu.Lock()
	s.exchPath = p
	s.exchPathGen++ // any explicit selection invalidates a pending rejected-start restore
	s.txMu.Unlock()
}

// SetMaxRepeats retunes the live unanswered-rung repeat cap (ft8.tx.max_repeats),
// applied immediately to the running sequencer so the operator can dial it down
// mid-pile-up without a restart (wired from the /v1/config PUT). nil-safe: a no-op
// when FT8 is disabled (the api Server holds a nil *Service) or before the sequencer
// exists.
func (s *Service) SetMaxRepeats(n int) {
	if s == nil || s.seq == nil {
		return
	}
	s.seq.SetMaxRepeats(n)
}

// exchangePath returns the active exchange's antenna path, "S" or "L"
// (short when unset).
func (s *Service) exchangePath() string {
	s.txMu.Lock()
	defer s.txMu.Unlock()
	if s.exchPath == "" {
		return antPathShort
	}
	return s.exchPath
}

// stampCompletionPath captures the active exchange's antenna path on the
// per-attempt CompletedQso after its terminal rung succeeds, before the sequencer
// releases the completed state. Per-QSO storage avoids a singleton snapshot that
// another completion can overwrite. The generation lets onComplete clear the live
// selection only if no newer choice has landed. Superseded attempts never stamp.
// A FAILED attempt stamps only on a GROUP A final rung, which logs whether or not
// its closing message keyed (see finalrung.go) — without the stamp that QSO would
// silently lose the operator's antenna choice. Group B retries instead of logging,
// and each retry captures the latest selection.
// preKeyDialCheck is the final gate before PTT (TxController.SetPreKeyCheck),
// enforcing invariant 2 at the moment it matters rather than when the send was
// accepted. Two rules:
//
//   - We must know where the rig is. An unreadable dial refuses: SM does not key
//     on a frequency it cannot corroborate, and that reading is also what labels
//     the decode log's TX line.
//   - If a SESSION is active, the rig must still be on the dial that session
//     pinned. Compared against sessionDialMHz, not the caller's dialMHz: the
//     latter is the CLIENT-supplied value carried for logging and took a
//     different path, so comparing it would produce spurious refusals. A manual
//     send has no active session, so only the first rule applies to it — a stale
//     pin from a previous session must never block one.
//
// Refusing here aborts the transmission without keying; startTransmission's
// failure path still runs the completion callback, so a Group A contact is
// recorded on its pinned frequency rather than lost (invariant 1).
// isDialRefusal reports whether a transmission failed because the daemon could no
// longer confirm the rig's frequency — the two sentinels preKeyDialCheck returns.
// Deliberately narrow: a key or play failure is transient and each ladder already
// decides whether to retry it, so only a frequency refusal retires the session.
func isDialRefusal(err error) bool {
	return stderrors.Is(err, ErrTxDialUnknown) || stderrors.Is(err, ErrTxSuperseded)
}

func (s *Service) preKeyDialCheck() error {
	const op errors.Op = "ft8.Service.preKeyDialCheck"
	cur, tracked, known := s.dialState()
	if !tracked {
		return nil // no CAT: nothing to corroborate against, and no session either
	}
	if !known {
		return errors.New(op).WithErr(ErrTxDialUnknown)
	}
	if !s.seq.Active() {
		return nil
	}
	s.txMu.Lock()
	pinned := s.sessionDialMHz
	s.txMu.Unlock()
	if pinned != 0 && cur != pinned {
		return errors.New(op).WithErr(ErrTxSuperseded)
	}
	return nil
}

func (s *Service) stampCompletionPath(c *CompletedQso) {
	s.txMu.Lock()
	p := s.exchPath
	if p == "" {
		p = antPathShort
	}
	c.AntPath = p
	c.antPathGen = s.exchPathGen
	c.antPathStamped = true
	// Stamp the frequency the contact actually happened on: the dial this SESSION
	// pinned, read from the rig at start. A contact is logged on the frequency it
	// was made on, not on wherever the rig is when the completion lands — and
	// those differ exactly when a QSY refused the closing rung, which is the case
	// that made us preserve the QSO in the first place. Storing it on the new band
	// would be worse than losing it: the wrong-band row is forwarded to QRZ and
	// ClubLog and has to be chased down by hand (codex P1 on 652821db).
	//
	// This supersedes the sink's old live-dial read, which existed because the
	// CLIENT-supplied dial went stale across a Call-CQ pile-up. The pin has neither
	// problem: it comes from the bridge, a start is refused outright while the dial
	// is unreadable, and a QSY during the session ends the session.
	if s.sessionDialMHz != 0 {
		c.DialFreqMHz = s.sessionDialMHz
	}
	s.txMu.Unlock()
}

// resetCompletionPath clears the completed exchange's live selection when it is
// still the value represented by c. If SetExchangePath has advanced the generation
// since the stamp, that newer selection belongs to the next/current exchange and
// must survive this delayed completion callback.
func (s *Service) resetCompletionPath(c CompletedQso) {
	if !c.antPathStamped {
		return
	}
	s.txMu.Lock()
	if s.exchPathGen == c.antPathGen {
		s.exchPath = ""
	}
	s.txMu.Unlock()
}

// consumeExchangePath returns the active exchange's path ("S"/"L", short when
// unset) AND clears it back to the default, in one txMu hold — completion
// previously read and reset via two separate acquisitions, so an operator
// SetExchangePath landing between them was silently swallowed (review
// 2026-07-20 round 12 #3). Session starts consume too (before the sequencer
// commit), pairing with restoreExchangePath on rejection. The returned
// generation token makes that restore conditional: it applies only if no
// SetExchangePath landed in between, so the operator's LATEST selection
// always wins (codex review of the first consume/restore shape — an
// unconditional restore was a lost update on the rejected-start path).
func (s *Service) consumeExchangePath() (string, uint64) {
	s.txMu.Lock()
	p := s.exchPath
	s.exchPath = ""
	gen := s.exchPathGen
	s.txMu.Unlock()
	if p == "" {
		return antPathShort, gen
	}
	return p, gen
}

// restoreExchangePath puts a consumed path back after a REJECTED session start,
// so the still-active exchange keeps its choice — UNLESS a SetExchangePath
// landed since the consume (the generation moved): the operator's newer
// selection then stands and the stale value is dropped. Restoring the default
// is always a no-op.
func (s *Service) restoreExchangePath(p string, gen uint64) {
	if p != antPathLong {
		return
	}
	s.txMu.Lock()
	if s.exchPathGen == gen {
		s.exchPath = p
	}
	s.txMu.Unlock()
}

// SetQsoSkip arms/disarms skip-if-silent on the active sequenced session (the
// deferred Next, daemon-side): armed, a silent cycle ends the session instead
// of keying the repeat. Nil sequencer → ErrNoActiveQso (nothing to arm).
func (s *Service) SetQsoSkip(armed bool) error {
	if s.seq == nil {
		return ErrNoActiveQso
	}
	return s.seq.SetSkipIfSilent(armed)
}

// AbandonQso drops any active sequenced QSO (operator action). Idempotent.
// Abandon is the operator's immediate off-ramp: besides stopping the sequencer
// (no further rungs), it cancels any in-flight transmission NOW — dropping PTT
// and stopping audio mid-slot rather than letting the current ~13 s cycle play
// out. TX stays ARMED (the device isn't torn down): only the contact ends, so
// the operator can call CQ or answer again. (disarmTx is the harder stop that
// also closes the device.) The cancelled transmission's goroutine clears
// txCancel/txInFlight and publishes the final ft8-tx state on its own path.
func (s *Service) AbandonQso() {
	// seqGate: the abandonment + cancellation must be atomic w.r.t. a concurrent
	// Start* — which commits its session, and can fire the opening transmission,
	// under this SAME gate (see StartQso, where seq.StartQso publishes and may
	// fire before returning). Without the gate, Abandon could slip into a start
	// AFTER its armed check but BEFORE the session commits: it would find an idle
	// sequencer and a nil txCancel, do nothing, and the start would then commit a
	// FRESH-generation session and transmit AFTER Abandon returned — the rung
	// commit-gate can't refuse a session created after the bump. Matches disarmTx's
	// seqGate protocol. Order seqGate → txMu.
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	// Bump the generation FIRST, then read txCancel under txMu second — the order
	// the rung commit-gate in startTransmission relies on (review 2026-07-20 #1).
	if s.seq != nil {
		s.seq.Abandon()
	}
	s.txMu.Lock()
	cancel := s.txCancel
	s.txMu.Unlock()
	if cancel != nil {
		cancel() // abort in-flight; the controller drops PTT on the cancel path
	}
}

// publishTxState snapshots the current TX state under txMu and fans it out on
// the ft8-tx SSE event. Call without holding txMu.
func (s *Service) publishTxState() {
	s.txMu.Lock()
	st := TxState{
		Armed:        s.txArmed,
		Transmitting: s.txInFlight,
		Message:      s.txMessage,
		OffsetHz:     s.txOffsetHz,
		Error:        s.txLastErr,
	}
	s.txMu.Unlock()
	s.hub.publish(hubEvent{name: EventTx, payload: st})
}

// PublishQsoLogged fans a just-logged FT8 QSO out on the ft8-logged SSE event so
// the SPA can add it to its session list (ADR 0029 step e4). Called by the e4
// sink (cmd/smd) after a successful qsoservice submit — the daemon owns assembly
// + storage there, so internal/ft8 receives only the SPA-ready payload and stays
// free of the storage path. One-shot (not cached for replay; see hub.publish).
func (s *Service) PublishQsoLogged(l LoggedQso) {
	s.hub.publish(hubEvent{name: EventLogged, payload: l})
}

// base returns the daemon-lifecycle context bound at Start (parent of every
// transmission, so daemon shutdown aborts an in-flight TX and drops PTT). Read
// under s.mu since Start writes parentCtx there.
func (s *Service) base() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parentCtx == nil {
		return context.Background()
	}
	return s.parentCtx
}

// txDeviceSpec resolves the output device from ft8.tx.device (projected from the
// active rig's audio.tx by ActiveFt8): a non-numeric value is a device NAME
// resolved to a live index at acquire time by the playback layer; an integer
// string is honoured as a raw index for any un-migrated config; empty → system
// default (name "", index -1). Mirrors the capture-side resolveAudioDevice.
func (s *Service) txDeviceSpec() (name string, index int) {
	if s.cfg.TX != nil {
		return resolveAudioDevice(s.cfg.TX.Device)
	}
	return "", -1
}

// resolveAudioDevice maps a configured audio-device string to either a device
// name (the per-rig RigConfig.Audio.{RX,TX} model) or a raw index (a legacy
// integer-string config). A value that parses as an integer is the index; any
// other non-empty value is a name; empty is the system default.
func resolveAudioDevice(s string) (name string, index int) {
	if s == "" {
		return "", -1
	}
	if n, err := strconv.Atoi(s); err == nil {
		return "", n
	}
	return s, -1
}

// txMode is the rig data-mode literal the controller switches to before keying
// (ft8.tx.mode); empty leaves the rig's current mode untouched.
func (s *Service) txMode() string {
	if s.cfg.TX != nil {
		return s.cfg.TX.Mode
	}
	return ""
}
