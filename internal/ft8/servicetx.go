package ft8

import (
	"context"
	stderrors "errors"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

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
	// ErrTxBadMessage: the message is not an encodable standard FT8 message.
	ErrTxBadMessage = stderrors.New("ft8: not an encodable standard message")
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

	player, err := s.newPlayer(s.txDeviceIndex())
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
	s.txArmed = true
	s.txLastErr = ""
	s.txMu.Unlock()

	s.log.InfoWith().Str("mode", s.txMode()).Msg("ft8 tx: armed")
	s.publishTxState()
	return nil
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

// TransmitNext queues one standard FT8 message to transmit on the next UTC
// slot. Refused unless armed, idle (no transmission in flight), and the message
// encodes. Returns immediately — the transmission runs in a tracked goroutine
// (it blocks up to a slot waiting for the boundary, then ~12.6 s of audio), and
// its progress/outcome rides the ft8-tx SSE event. PTT is guaranteed down on
// every path (controller deferred unkey + bridge auto-off).
func (s *Service) TransmitNext(message string, offsetHz float64) error {
	const op errors.Op = "ft8.Service.TransmitNext"

	// Validate encodability synchronously so a bad message is an immediate
	// error, not an async failure after the (up to 15 s) slot wait.
	if _, err := EncodeToSlot(message, offsetHz, txNominalDtSec); err != nil {
		return errors.New(op).WithErr(ErrTxBadMessage).WithMsg(err.Error())
	}
	// Boundary-aligned: TransmitSlot waits for the next UTC slot and starts at the
	// nominal +0.5 s — right for a manually-initiated CQ (we pick our own slot/
	// parity, so we start on time with no truncation).
	return s.startTransmission(message, offsetHz, func(ctx context.Context, ctrl *TxController) error {
		return ctrl.TransmitSlot(ctx, message, offsetHz)
	}, nil)
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
func (s *Service) seqTransmit(message string, offsetHz float64, onDone func(ok bool)) error {
	const op errors.Op = "ft8.Service.seqTransmit"
	if _, err := EncodeWaveform(message, offsetHz); err != nil {
		return errors.New(op).WithErr(ErrTxBadMessage).WithMsg(err.Error())
	}
	return s.startTransmission(message, offsetHz, func(ctx context.Context, ctrl *TxController) error {
		return ctrl.TransmitCurrentSlot(ctx, message, offsetHz)
	}, onDone)
}

// startTransmission runs one transmission through the armed controller under the
// single-flight guard, in a tracked goroutine. fn is the controller call —
// next-slot TransmitSlot for a manual CQ, or current-slot TransmitCurrentSlot for
// a sequencer rung. Returns synchronously with ErrTxNotArmed / ErrTxInFlight if it
// can't start; the transmission's progress/outcome rides the ft8-tx SSE. PTT is
// guaranteed down on every path (controller deferred-unkey + bridge auto-off).
func (s *Service) startTransmission(
	message string,
	offsetHz float64,
	fn func(ctx context.Context, ctrl *TxController) error,
	onDone func(ok bool),
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
	ctrl := s.txCtrl
	txCtx, cancel := context.WithCancel(base)
	s.txCancel = cancel
	s.txInFlight = true
	s.txMessage = message
	s.txOffsetHz = offsetHz
	s.txLastErr = ""
	s.txMu.Unlock()

	s.publishTxState() // transmitting

	safego.GoTracked(txCtx, "ft8.tx", s.onPanic, func() {
		defer cancel() // release the ctx on normal completion
		err := fn(txCtx, ctrl)

		// A cancel (disarm / daemon stop) is a normal stop, not a failure.
		failed := err != nil && !stderrors.Is(err, context.Canceled)

		s.txMu.Lock()
		s.txInFlight = false
		s.txMessage = ""
		s.txOffsetHz = 0
		s.txCancel = nil
		if failed {
			s.txLastErr = "ft8_tx_failed"
		}
		s.txMu.Unlock()

		if failed {
			s.log.WarnWith().Err(err).Msg("ft8 tx: transmission failed")
		}
		s.publishTxState() // done (or failed)

		// Completion callback (final-rung QSO logging, review H1): ok only on a
		// clean success — a cancel (disarm/stop) or any error means the final
		// transmission did NOT complete on air, so the QSO must not be logged.
		if onDone != nil {
			onDone(err == nil)
		}
	}, false, &s.txWg)

	return nil
}

// StartQso begins a manual answer-a-CQ exchange (ADR 0031): the operator picked
// the worked station (theirCall/theirGrid, from a CQ heard in the slot at
// theirSlotUTC) and a clear offset. Requires TX **armed** — the sequencer keys
// through the armed controller. ourCall/ourGrid are the station identity the api
// layer resolved from config.
func (s *Service) StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC string, offsetHz, dialFreqMHz float64) error {
	const op errors.Op = "ft8.Service.StartQso"
	// seqGate: armed-check + sequencer commit are atomic w.r.t. disarm (M3).
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	s.txMu.Lock()
	armed := s.txArmed
	s.txMu.Unlock()
	if !armed {
		return errors.New(op).WithErr(ErrTxNotArmed)
	}
	return s.seq.StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC, offsetHz, dialFreqMHz, time.Now().UTC())
}

// StartCallCq begins a sequenced Call-CQ session (ADR 0033): we call CQ in our slot
// parity and work the stations that answer, one at a time, looping until AbandonQso.
// Requires TX **armed** — the sequencer keys through the armed controller. The
// answerer-selection mode is read from ft8.tx.caller_answer_mode (default auto_first).
// ourCall/ourGrid are the station identity the api layer resolved from config;
// offsetHz is our TX offset; dialFreqMHz is the rig dial for the logged QSO frequency.
func (s *Service) StartCallCq(ourCall, ourGrid string, offsetHz, dialFreqMHz float64) error {
	const op errors.Op = "ft8.Service.StartCallCq"
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
	s.txMu.Lock()
	armed := s.txArmed
	s.txMu.Unlock()
	if !armed {
		return errors.New(op).WithErr(ErrTxNotArmed)
	}
	return s.seq.StartCallCq(ourCall, ourGrid, offsetHz, dialFreqMHz, mode, time.Now().UTC())
}

// SetQsoLogger injects the sink that logs a completed FT8 exchange (ADR 0029
// step e4) — the daemon (cmd/smd) wires it to qsoservice. Called once during
// wiring, before Start. A nil logger (e.g. tests) means completed exchanges are
// not logged, only emitted on the SSE. internal/ft8 stays free of qsoservice /
// config / adif: the assembly + submit live in the injected sink.
func (s *Service) SetQsoLogger(fn func(ctx context.Context, c CompletedQso)) {
	s.qsoLogger = fn
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

// txDeviceIndex resolves the output device from ft8.tx.device: an integer index
// (as listed by `ft8-tx-probe -list`), else -1 (system default). Mirrors the
// capture-side device resolution.
func (s *Service) txDeviceIndex() int {
	if s.cfg.TX != nil && s.cfg.TX.Device != "" {
		if n, err := strconv.Atoi(s.cfg.TX.Device); err == nil {
			return n
		}
		s.log.WarnWith().Str("device", s.cfg.TX.Device).
			Msg("ft8: tx.device is not an integer index; using system default")
	}
	return -1
}

// txMode is the rig data-mode literal the controller switches to before keying
// (ft8.tx.mode); empty leaves the rig's current mode untouched.
func (s *Service) txMode() string {
	if s.cfg.TX != nil {
		return s.cfg.TX.Mode
	}
	return ""
}
