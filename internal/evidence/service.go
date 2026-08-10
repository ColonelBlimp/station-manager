package evidence

import (
	"database/sql"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	_ "modernc.org/sqlite" // pure-Go driver; works in the CGO-free build
)

// ServiceName is the iocdi bean id for the evidence writer.
const ServiceName = "evidence-service"

// The operator-facing cap floor is types.EvidenceMinCapBytes (32 MiB),
// validated by internal/config — it lives in types because config cannot
// import this package (evidence → logging → config cycle). Initialize's own
// check below is the weaker PHYSICAL bound (cap must exceed the headroom
// var) so tests may dial headroom down without fighting the operator floor.

// Capture states surfaced by Status.
const (
	StateDisabled  = "disabled"
	StateCapturing = "capturing"
	StateDropNew   = "drop_new"
)

// SlotOutcome is the §4.1 coverage vocabulary.
type SlotOutcome string

const (
	OutcomeDecoded        SlotOutcome = "decoded"
	OutcomeNoDecode       SlotOutcome = "no_decode"
	OutcomeTx             SlotOutcome = "tx"
	OutcomeDialChanged    SlotOutcome = "dial_changed"
	OutcomeDecoderError   SlotOutcome = "decoder_error"
	OutcomeCaptureDropped SlotOutcome = "capture_dropped"
)

// Loss reasons and the slice's only remote status (§4.1 three-valued
// taxonomy: everything dropped locally in this slice was never offered).
const (
	lossReasonCap      = "cap"
	lossReasonWriter   = "writer_error"
	remoteNeverOffered = "never_offered"
)

const slotDuration = 15 * time.Second

// Tunables, package vars so tests can dial them (the captureLinger pattern).
var (
	// headroomBytes is reserved below the hard cap: the watermark is
	// CapBytes − headroomBytes, and capture drops once physical usage
	// crosses it. The watermark check runs BEFORE each write, so the cap
	// guarantee holds only while headroom exceeds the 32 KiB shared-memory
	// file plus one slot transaction's WAL growth (~tens of KiB) plus
	// checkpoint churn (auto-checkpoint moves up to ~4 MiB of WAL at
	// default settings) — an engineering constant, not an operator
	// threshold; tests that dial it down must stay above the per-write
	// growth or they create a configuration the design forbids.
	headroomBytes int64 = 16 << 20
	// writerQueueSize bounds the enqueue between the decode loop and the
	// writer goroutine: 64 slots ≈ 16 minutes of backlog, after which
	// CaptureSlot drops (and counts) rather than ever blocking the caller.
	writerQueueSize = 64
	// writerDelay is a test-only stall for the writer goroutine.
	writerDelay time.Duration
)

// Config is the evidence block resolved from config.json.
type Config struct {
	Capture  bool
	CapBytes int64
	Path     string
}

// SlotCapture is one physical slot's evidence as handed over by the decode
// loop: coverage outcome plus the unfiltered rich decode set.
type SlotCapture struct {
	SlotStart   time.Time
	DialMHz     float64
	DialTracked bool
	Outcome     SlotOutcome
	Decodes     []goft8.DecodedMessage
}

// Status is the local honesty surface (§4.1 amendment: usage and the
// drop-new state are exposed; the unprofiled count is the §5.4 guardrail).
type Status struct {
	Enabled                bool   `json:"enabled"`
	State                  string `json:"state"`
	CapBytes               int64  `json:"cap_bytes"`
	WatermarkBytes         int64  `json:"watermark_bytes"`
	UsageBytes             int64  `json:"usage_bytes"`
	Observations           int64  `json:"observations"`
	UnprofiledObservations int64  `json:"unprofiled_observations"`
	DroppedSlots           int64  `json:"dropped_slots"`
}

// lossAccum is the reserved in-memory accumulator: the record of dropping
// must survive the failure it reports, so it lives here and is persisted
// with priority at the first moment the writer can write (§4.1). Honest
// crash-time limit: an accumulator not yet persisted at a hard crash is
// gone.
type lossAccum struct {
	uuid         string
	start, end   time.Time
	slots        int64
	observations int64
	reason       string
	dialMHz      float64
	dialMixed    bool
}

// Service owns evidence.db and its single bounded writer goroutine.
type Service struct {
	cfg          Config
	log          logging.Logger
	decoderBuild string

	db      *sql.DB
	ch      chan SlotCapture
	quit    chan struct{}
	done    chan struct{}
	closed  atomic.Bool
	pending atomic.Int64 // enqueued-but-unprocessed slots; drain observability

	mu      sync.Mutex
	started bool
	state   string
	dropped int64
	loss    *lossAccum
}

func New(cfg Config, log logging.Logger) *Service {
	if log == nil {
		log = logging.Noop()
	}
	return &Service{
		cfg:          cfg,
		log:          log,
		decoderBuild: goft8Build(),
		state:        StateDisabled,
		quit:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// goft8Build resolves the linked go-ft8 module version — the "decoder build"
// every observation carries, because a decode's evidentiary weight depends
// on which decoder produced it.
func goft8Build() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/ColonelBlimp/go-ft8" {
				return d.Version
			}
		}
	}
	return "unknown"
}

// Initialize validates config; a disabled service validates nothing (its
// config is inert).
func (s *Service) Initialize() error {
	const op errors.Op = "evidence.Service.Initialize"
	if !s.cfg.Capture {
		return nil
	}
	if s.cfg.Path == "" {
		return errors.New(op).WithMsg("evidence capture enabled but no db path resolved")
	}
	if s.cfg.CapBytes <= headroomBytes {
		return errors.New(op).WithMsgf(
			"evidence cap %d B must exceed the reserved headroom %d B (the watermark would be ≤ 0)",
			s.cfg.CapBytes, headroomBytes)
	}
	return nil
}

// Start opens the archive and spawns the writer — only when capture is
// enabled: a disabled service creates no file at all (EV1; §8 consent).
func (s *Service) Start() error {
	const op errors.Op = "evidence.Service.Start"
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfg.Capture || s.started {
		return nil
	}

	db, err := sql.Open("sqlite", s.cfg.Path)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("open evidence.db")
	}
	// One writer goroutine owns all writes; a second connection would only
	// serve Status reads, which the same handle covers.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=2000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return errors.New(op).WithErr(err).WithMsgf("apply %s", pragma)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return errors.New(op).WithErr(err).WithMsg("create evidence schema")
	}

	s.db = db
	s.state = StateCapturing
	s.started = true
	s.ch = make(chan SlotCapture, writerQueueSize)
	go s.writerLoop()
	s.log.InfoWith().
		Str("path", s.cfg.Path).
		Int64("cap_bytes", s.cfg.CapBytes).
		Int64("watermark_bytes", s.cfg.CapBytes-headroomBytes).
		Str("decoder_build", s.decoderBuild).
		Msg("evidence: capture started")
	return nil
}

// Stop drains the writer, persists any accumulated loss best-effort, and
// closes the archive. Idempotent.
func (s *Service) Stop() {
	if s.closed.Swap(true) {
		return
	}
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return
	}
	close(s.quit)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLossLocked()
	_ = s.db.Close()
	s.state = StateDisabled
	s.started = false
}

// CaptureSlot enqueues one slot's evidence. NEVER blocks: a full queue (or a
// stopped/disabled service) drops the slot, and drops under a running writer
// are counted into the loss accumulator.
func (s *Service) CaptureSlot(sc SlotCapture) {
	if s.closed.Load() {
		return
	}
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return
	}
	s.pending.Add(1)
	select {
	case s.ch <- sc:
	default:
		s.pending.Add(-1)
		s.mu.Lock()
		s.dropped++
		s.accumulateLocked(sc, lossReasonWriter)
		s.mu.Unlock()
	}
}

// Status reports the capture state. Counts come from the archive itself;
// a disabled or unstarted service reports zeros.
func (s *Service) Status() Status {
	s.mu.Lock()
	st := Status{
		Enabled:        s.cfg.Capture,
		State:          s.state,
		CapBytes:       s.cfg.CapBytes,
		WatermarkBytes: s.cfg.CapBytes - headroomBytes,
		DroppedSlots:   s.dropped,
	}
	db := s.db
	started := s.started
	s.mu.Unlock()
	if !started || db == nil {
		return st
	}
	st.UsageBytes = s.physicalUsage()
	_ = db.QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&st.Observations)
	_ = db.QueryRow(`SELECT COUNT(*) FROM observations WHERE profile_uuid IS NULL`).
		Scan(&st.UnprofiledObservations)
	return st
}

func (s *Service) writerLoop() {
	defer close(s.done)
	for {
		select {
		case sc := <-s.ch:
			s.processSlot(sc)
			s.pending.Add(-1)
		case <-s.quit:
			for {
				select {
				case sc := <-s.ch:
					s.processSlot(sc)
					s.pending.Add(-1)
				default:
					return
				}
			}
		}
	}
}

func (s *Service) processSlot(sc SlotCapture) {
	if writerDelay > 0 {
		time.Sleep(writerDelay)
	}

	usage := s.physicalUsage()
	watermark := s.cfg.CapBytes - headroomBytes

	s.mu.Lock()
	if usage >= watermark {
		// Drop BEFORE the cap (operator amendment 2026-08-10): decoding
		// continues, only evidence writes stop; the dropped span accumulates
		// and its coalesced interval row is refreshed within the reserved
		// headroom — the record of dropping must not itself be dropped.
		if s.state != StateDropNew {
			s.state = StateDropNew
			s.log.InfoWith().
				Int64("usage_bytes", usage).
				Int64("watermark_bytes", watermark).
				Int64("cap_bytes", s.cfg.CapBytes).
				Msg("evidence: capture paused at watermark (drop-new)")
		}
		s.dropped++
		s.accumulateLocked(sc, lossReasonCap)
		s.refreshLossLocked()
		s.mu.Unlock()
		return
	}
	if s.state == StateDropNew {
		s.state = StateCapturing
		s.closeLossLocked()
		s.log.InfoWith().
			Int64("usage_bytes", usage).
			Int64("watermark_bytes", watermark).
			Msg("evidence: capture resumed (capacity available)")
	} else if s.loss != nil {
		// Writer-error recovery: persist the accumulated interval with
		// priority, before any new evidence (§4.1).
		s.closeLossLocked()
	}
	s.mu.Unlock()

	if err := s.writeSlot(sc); err != nil {
		s.log.WarnWith().Err(err).
			Str("slot", sc.SlotStart.UTC().Format(time.RFC3339)).
			Msg("evidence: slot write failed; dropped and counted")
		s.mu.Lock()
		s.dropped++
		s.accumulateLocked(sc, lossReasonWriter)
		s.mu.Unlock()
	}
}

// writeSlot commits one slot — its coverage row and every observation — as
// ONE transaction (EV4): the archive never shows observations for a slot
// without that slot's coverage row, whatever happens mid-write.
func (s *Service) writeSlot(sc SlotCapture) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	slotUTC := sc.SlotStart.UTC().Format(time.RFC3339)
	if _, err := tx.Exec(
		`INSERT INTO coverage (uuid, slot_start_utc, outcome, dial_mhz, dial_tracked, decode_count)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		utils.NewUUIDv7At(sc.SlotStart), slotUTC, string(sc.Outcome),
		sc.DialMHz, boolInt(sc.DialTracked), len(sc.Decodes)); err != nil {
		return err
	}
	for _, m := range sc.Decodes {
		// NULL text when the payload has no canonical rendering — "" would
		// claim an empty message was transmitted.
		var text any
		if m.ParseStatus == goft8.ParseStatusParsed {
			text = m.Text
		}
		if _, err := tx.Exec(
			`INSERT INTO observations (uuid, slot_start_utc, dial_mhz, dial_tracked,
				freq_hz, dt_sec, snr, payload, parse_status, text,
				prov_algorithm, prov_ap_profile, prov_ap_source,
				metric_sync, metric_hard_sync, metric_costas_geo, metric_costas_min_block,
				metric_blocks, metric_hard_errors, metric_dmin,
				decoder_build, profile_uuid)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
			utils.NewUUIDv7At(sc.SlotStart), slotUTC, sc.DialMHz, boolInt(sc.DialTracked),
			m.FreqHz, m.DTSec, m.SNR, m.Payload[:], m.ParseStatus.String(), text,
			string(m.Provenance.Algorithm), m.Provenance.APProfile, m.Provenance.APSource,
			m.Sync, m.HardSync, m.CostasGeo, m.CostasMinBlock,
			m.Blocks, m.HardErrors, m.DMin,
			s.decoderBuild); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// accumulateLocked extends the reserved loss accumulator with one dropped
// slot. One coalesced interval: if the reason changes and the old interval
// cannot be persisted (the writer is the thing that failed), the span widens
// under the FIRST reason — a documented imprecision under double failure,
// preferred over losing the count.
func (s *Service) accumulateLocked(sc SlotCapture, reason string) {
	if s.loss != nil && s.loss.reason != reason {
		s.persistLossLocked()
		if s.loss != nil {
			// Persist failed; fold rather than lose.
			reason = s.loss.reason
		}
	}
	if s.loss == nil {
		s.loss = &lossAccum{
			uuid:    utils.NewUUIDv7At(sc.SlotStart),
			start:   sc.SlotStart.UTC(),
			reason:  reason,
			dialMHz: sc.DialMHz,
		}
	}
	l := s.loss
	l.end = sc.SlotStart.UTC().Add(slotDuration)
	l.slots++
	l.observations += int64(len(sc.Decodes))
	if sc.DialMHz != l.dialMHz && !l.dialMixed {
		l.dialMixed = true
		l.dialMHz = 0 // spans more than one dial context; unattributed is honest
	}
}

// refreshLossLocked upserts the open interval's row, keeping the
// accumulator (the interval is still growing — the cap path).
func (s *Service) refreshLossLocked() { s.upsertLossLocked(false) }

// closeLossLocked persists and clears the accumulator (resume, recovery,
// Stop).
func (s *Service) closeLossLocked() { s.upsertLossLocked(true) }

// persistLossLocked tries to close the interval; on failure the accumulator
// survives for the next opportunity.
func (s *Service) persistLossLocked() { s.upsertLossLocked(true) }

func (s *Service) upsertLossLocked(clear bool) {
	l := s.loss
	if l == nil || s.db == nil {
		return
	}
	_, err := s.db.Exec(
		`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(uuid) DO UPDATE SET
			end_utc = excluded.end_utc,
			slots = excluded.slots,
			observations = excluded.observations,
			dial_mhz = excluded.dial_mhz`,
		l.uuid, l.start.Format(time.RFC3339), l.end.Format(time.RFC3339),
		l.slots, l.observations, l.reason, remoteNeverOffered, l.dialMHz)
	if err != nil {
		s.log.WarnWith().Err(err).Msg("evidence: loss interval persist failed; accumulator retained")
		return
	}
	if clear {
		s.loss = nil
	}
}

// physicalUsage is the §4.1 physical definition: the db file plus its WAL
// and shared-memory siblings — a logical row target cannot bound what WAL
// growth does to the disk.
func (s *Service) physicalUsage() int64 {
	var total int64
	for _, p := range []string{s.cfg.Path, s.cfg.Path + "-wal", s.cfg.Path + "-shm"} {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	return total
}

// queueEmpty reports a fully drained writer (tests' drain helper).
func (s *Service) queueEmpty() bool { return s.pending.Load() == 0 }

// compactForTest frees genuine space (rows + VACUUM + WAL truncate) so the
// resume path can be exercised against real file sizes.
func (s *Service) compactForTest() error {
	for _, q := range []string{
		`DELETE FROM observations`,
		`DELETE FROM coverage`,
		`VACUUM`,
		`PRAGMA wal_checkpoint(TRUNCATE)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
