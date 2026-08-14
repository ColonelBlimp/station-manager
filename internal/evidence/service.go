package evidence

import (
	"context"
	"database/sql"
	stderrors "errors"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
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
	lossReasonCap    = "cap"
	lossReasonWriter = "writer_error"
	// lossReasonQueueFull (L3): a slot dropped because the WRITER QUEUE was full
	// (backpressure) — no database write was even attempted. Distinct from
	// writer_error (a write that was attempted and failed), the confusable state
	// this class breaks: a slow/backed-up writer vs a failing one.
	lossReasonQueueFull = "evidence_queue_full"
	// lossReasonMeasurement (L2): a slot dropped because a measurement REQUIRED
	// to authorize the write was unknown — the archive could not be measured, so
	// fail-closed. Distinct from cap (measured, genuinely full) and writer_error
	// (the write itself failed): the confusable state this class exists to break.
	lossReasonMeasurement = "measurement_error"
	remoteNeverOffered    = "never_offered"
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
	// statusQueryDelay is a test-only stall that travels WITH Status's
	// database aggregates — proving they run outside s.mu (a status poll
	// must never stall CaptureSlot on the decode goroutine).
	statusQueryDelay time.Duration
	// checkpointHook is a test-only seam that runs WHERE the drop-path
	// TRUNCATE checkpoint runs — proving that checkpoint holds no lock the
	// decode path needs (a reader blocking the truncate must never stall
	// CaptureSlot's stamp on s.mu).
	checkpointHook func()
	// measureFailHook is a test-only seam (L2): when set, each measurement probe
	// calls it with its granular, stable operation name (e.g. "stat_db",
	// "stat_wal", "pragma_freelist_count", "metadata_loss", "checkpoint_purge",
	// "compaction_commit") and, if it returns non-nil, that probe fails with the
	// operation attached — so a test can force fail-closed at exactly one point.
	measureFailHook func(op string) error
)

// measureError tags a measurement/compaction failure with the granular operation
// that produced it, so the fail-closed gate can drive the retention-health tracker
// with a stable operation name (L2). Real probe errors and injected ones both carry
// it; unwrap reaches the underlying cause.
type measureError struct {
	op  string
	err error
}

func (e *measureError) Error() string { return e.err.Error() }
func (e *measureError) Unwrap() error { return e.err }

// measured wraps a probe error with its operation (nil stays nil).
func measured(op string, err error) error {
	if err == nil {
		return nil
	}
	return &measureError{op: op, err: err}
}

// measureFail returns an injected failure for op (test-only), tagged with op.
func measureFail(op string) error {
	if measureFailHook != nil {
		return measured(op, measureFailHook(op))
	}
	return nil
}

// opOf extracts the granular operation name from a measurement error, or a
// generic label if the error is not one.
func opOf(err error) string {
	var me *measureError
	if stderrors.As(err, &me) {
		return me.op
	}
	return "measurement"
}

// migrationSlackBytes pads the v1→v2 migration's projected WAL peak beyond
// the per-page bound: the new profile tables, schema/meta pages dirtied more
// than once, and the 32 B WAL header. An engineering constant like
// headroomBytes, not an operator threshold.
const migrationSlackBytes int64 = 1 << 20

// Config is the evidence block resolved from config.json.
type Config struct {
	Capture  bool
	CapBytes int64
	Path     string
	// Antennas is the §4.2 station-profile declaration (already validated
	// by internal/config). Restart-only by construction: it is read once at
	// Start and reconciled into immutable profile versions.
	Antennas []types.AntennaDecl

	// Sync is §8 consent layer 2, one boolean (operator ruling 2026-08-10).
	// SyncURL/SyncToken are resolved by cmd/smd from the enabled smcloud
	// forwarder's credentials — no second account or token surface, and
	// this package stays free of config/forwarding imports. Validation
	// (sync requires a configured smcloud forwarder) lives in
	// internal/config.
	Sync      bool
	SyncURL   string
	SyncToken string
}

// SlotCapture is one physical slot's evidence as handed over by the decode
// loop: coverage outcome plus the unfiltered rich decode set.
type SlotCapture struct {
	SlotStart   time.Time
	DialMHz     float64
	DialTracked bool
	Outcome     SlotOutcome
	Decodes     []goft8.DecodedMessage

	// The §4.2 profile stamp, resolved by CaptureSlot at emission time and
	// carried WITH the record — the asynchronous writer never re-derives it
	// (O4). Exactly one is set once the service has stamped the slot.
	profileUUID      string
	unprofiledReason string
}

// Status is the local honesty surface (§4.1 amendment: usage and the
// drop-new state are exposed; the unprofiled count is the §5.4 guardrail).
type Status struct {
	Enabled                bool             `json:"enabled"`
	State                  string           `json:"state"`
	CapBytes               int64            `json:"cap_bytes"`
	WatermarkBytes         int64            `json:"watermark_bytes"`
	UsageBytes             *int64           `json:"usage_bytes"` // null when the archive could not be measured (L2)
	Observations           int64            `json:"observations"`
	UnprofiledObservations int64            `json:"unprofiled_observations"`
	DroppedSlots           int64            `json:"dropped_slots"`
	Profiles               *ProfilesStatus  `json:"profiles,omitempty"`
	Sync                   *SyncStatus      `json:"sync,omitempty"`
	Retention              *RetentionStatus `json:"retention,omitempty"`
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

	// retHealth (L2) tracks whether the retention MEASUREMENT layer is failing, so
	// a swallowed measurement/compaction error becomes an observable degraded
	// transition instead of a slot silently blamed on capacity. Created in Start
	// before the writer goroutine; driven ONLY by the write attempt (processSlot),
	// never by a Status poll. Has its own mutex.
	retHealth *retentionHealth

	// queueLoss (L3) logs a bounded record of writer-queue backpressure — a slot
	// dropped in CaptureSlot because the queue was full, distinct from a write that
	// was attempted and failed. Warned at episode totals 1/10/100/…; recovered on
	// the next successful persist. Its own mutex; created in Start.
	queueLoss *logging.EpisodeLoss

	mu       sync.Mutex
	started  bool
	state    string
	dropped  int64
	loss     *lossAccum
	pressure string // retention drop-new cause: "" | cap | metadata (RT9)

	// §4.2 profile resolution state, written once in Start (restart-only
	// activation) and immutable until Stop; reads happen under mu.
	profState  string
	profReason string
	profActive map[string]ProfileActive

	// §5 sync engine state (sync.go). syncCh/syncDone/syncClient are
	// created in Start when sync is enabled and never change after;
	// the remaining fields are guarded by mu. syncCancelBacklog is
	// non-nil exactly while a BACKLOG request is in flight — notifyLive
	// calls it (the ruling's intentional cancellation, no backoff
	// advance).
	syncCh            chan struct{}
	syncDone          chan struct{}
	syncClient        *http.Client
	syncState         string
	syncLastErr       string
	syncLastSuccess   time.Time
	syncCancelBacklog context.CancelFunc
	syncLiveInterrupt bool
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

	// Pragmas ride the DSN (package-review P2, 2026-08-10): busy_timeout
	// and synchronous are CONNECTION-scoped, and the writer, sync loop and
	// status readers each draw pooled connections — a one-time Exec reaches
	// exactly one of them, leaving the rest at busy_timeout 0 and failing
	// concurrent writes with SQLITE_BUSY. The modernc driver applies
	// _pragma parameters to every connection it opens (pinned by
	// TestPragmas_ApplyToEveryPooledConnection).
	db, err := sql.Open("sqlite",
		"file:"+s.cfg.Path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("open evidence.db")
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return errors.New(op).WithErr(err).WithMsg("apply WAL journal mode")
	}
	if err := s.migrateSchema(db); err != nil {
		_ = db.Close()
		return errors.New(op).WithErr(err).WithMsg("create/migrate evidence schema")
	}

	s.db = db
	// §4.2 activation: one transaction, before the writer goroutine exists.
	// Failure is the O5 class-1 posture — capture continues, resolution is
	// globally degraded (rows carry profile_error), the stale prior mapping
	// is never used — and PR7's one default-visible record per transition
	// into degraded is THIS line; nothing else may log the state.
	s.profReason = ""
	if err := s.reconcileProfiles(time.Now()); err != nil {
		s.profState = ProfilesDegraded
		s.profReason = err.Error()
		s.profActive = nil
		s.log.WarnWith().Err(err).
			Msg("evidence: profile activation failed; profile resolution degraded (new rows carry profile_error)")
	}
	s.state = StateCapturing
	s.started = true
	s.retHealth = newRetentionHealth(s.log, s.cfg.Path, retentionMeasurementHeartbeat, time.Now)
	s.queueLoss = logging.NewEpisodeLoss(s.log,
		"evidence: writer queue full; slot dropped (backpressure)",
		"evidence: writer queue recovered",
		lossReasonQueueFull, time.Now)
	s.ch = make(chan SlotCapture, writerQueueSize)
	go s.writerLoop()
	if s.cfg.Sync {
		s.syncCh = make(chan struct{}, 1)
		s.syncDone = make(chan struct{})
		s.syncClient = &http.Client{Timeout: syncHTTPTimeout}
		go s.syncLoop()
	}
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
	if s.syncDone != nil {
		<-s.syncDone
	}
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
	if started {
		// Emission-time stamp (O4): resolved here on the caller's goroutine
		// and carried in the record; the writer never re-derives it.
		s.stampLocked(&sc)
	}
	s.mu.Unlock()
	if !started {
		return
	}
	s.pending.Add(1)
	select {
	case s.ch <- sc:
	default:
		// Writer queue full → backpressure, NOT a write failure (L3): record it as
		// evidence_queue_full so the durable loss record distinguishes a backed-up
		// writer from a failing one. Non-blocking: the episode Warn (spaced at
		// 1/10/100/…) fires outside s.mu.
		s.pending.Add(-1)
		s.mu.Lock()
		s.dropped++
		s.accumulateLocked(sc, lossReasonQueueFull)
		depth, capacity := len(s.ch), cap(s.ch)
		s.mu.Unlock()
		s.queueLoss.Add(1, depth, capacity)
	}
}

// Status reports the capture state. Counts come from the archive itself; a
// disabled or unstarted service reports zeros — and a NIL-counted profiles
// object (O6): the store is deliberately not open, so lineage/version
// counts are unavailable, never zero.
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
	if !started || db == nil {
		st.Profiles = &ProfilesStatus{State: ProfilesDisabled}
		st.Sync = &SyncStatus{Enabled: false}
		s.mu.Unlock()
		return st
	}
	// Snapshot every mu-guarded field, then release: the database
	// aggregates below run OUTSIDE the lock, because CaptureSlot takes
	// s.mu synchronously on the FT8 decode goroutine — a status poll over
	// a cap-sized archive must never stall decoding (package-review P1,
	// 2026-08-10).
	prof := &ProfilesStatus{State: s.profState, Reason: s.profReason}
	if s.profState == ProfilesActive && len(s.profActive) > 0 {
		prof.Active = make(map[string]ProfileActive, len(s.profActive))
		for band, pa := range s.profActive {
			prof.Active[band] = pa
		}
	}
	syn := &SyncStatus{Enabled: s.cfg.Sync}
	if s.cfg.Sync {
		syn.State = s.syncState
		if syn.State == "" {
			syn.State = syncStateIdle
		}
		syn.LastError = s.syncLastErr
		if !s.syncLastSuccess.IsZero() {
			syn.LastSuccess = s.syncLastSuccess.UTC().Format(time.RFC3339)
		}
	}
	ret := &RetentionStatus{Pressure: s.pressure}
	s.mu.Unlock()

	if statusQueryDelay > 0 {
		time.Sleep(statusQueryDelay)
	}
	// Status poll: usage is null when unmeasurable, and it must NOT drive the
	// write-driven tracker (recovery/heartbeat belong to write attempts only).
	if usage, err := s.physicalUsage(); err == nil {
		st.UsageBytes = &usage
	}
	s.fillProfileCounts(prof)
	if s.cfg.Sync {
		s.fillSyncCounts(syn)
	}
	s.fillRetentionCounts(ret)
	st.Profiles, st.Sync, st.Retention = prof, syn, ret
	_ = db.QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&st.Observations)
	_ = db.QueryRow(`SELECT COUNT(*) FROM observations WHERE profile_uuid IS NULL`).
		Scan(&st.UnprofiledObservations)
	return st
}

// migrateSchema brings the archive to schemaVersion: fresh archives get the
// v2 DDL directly; a v1 archive is adopted ADDITIVELY in one transaction
// (PR9 — pre-existing NULL references backfill legacy_unprofiled, nothing
// else is touched). A version newer than this build is refused rather than
// guessed at — the caller's fail-soft leaves evidence idle while decoding
// continues.
func (s *Service) migrateSchema(db *sql.DB) error {
	const op errors.Op = "evidence.Service.migrateSchema"
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (k TEXT PRIMARY KEY, v TEXT NOT NULL)`); err != nil {
		return errors.New(op).WithErr(err).WithMsg("ensure schema_meta")
	}
	var v string
	err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&v)
	switch {
	case err == sql.ErrNoRows, err == nil && v == schemaVersion:
		_, err = db.Exec(schemaSQL)
		if err != nil {
			return errors.New(op).WithErr(err).WithMsg("apply v2 schema")
		}
		return nil
	case err != nil:
		return errors.New(op).WithErr(err).WithMsg("read schema version")
	case v == "1":
		// Cap gate BEFORE writing (codex-P1 ruling 2026-08-10): the adoption
		// backfill dirties ~every observations page (every v1 row matches
		// profile_uuid IS NULL), so this transaction's WAL peak is of the
		// order of the whole db file — far past the writer's fixed headroom —
		// and it cannot be measured in time: dirty pages spill to the -wal
		// DURING the transaction and ROLLBACK does not shrink the file
		// (measured 2026-08-10, modernc.org/sqlite v1.48.1, daemon pragmas:
		// on a 12.4 MB archive the -wal held 10.6 MB before commit, 12.4 MB
		// at peak, and was byte-identical after rollback). Never-exceed
		// therefore needs a projection: one WAL frame per dirtied page
		// (frame = page + 24 B header — WAL file format,
		// https://sqlite.org/walformat.html) bounds growth by the db file
		// size + ~6% + slack for the new tables and re-dirtied meta pages.
		// Refusal writes nothing and surfaces through Start's existing
		// fail-soft: evidence idle, decoding continues, retried at the next
		// restart once the cap or the archive has changed.
		if err := s.migrationBackfillGate(op, "v1→v2"); err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return errors.New(op).WithErr(err).WithMsg("begin v1→v2 migration")
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec(migrate1to2SQL); err != nil {
			return errors.New(op).WithErr(err).WithMsg("apply v1→v2 migration")
		}
		if err := tx.Commit(); err != nil {
			return errors.New(op).WithErr(err).WithMsg("commit v1→v2 migration")
		}
		// Fold the transient WAL peak back before capture starts: TRUNCATE
		// checkpoints and zeroes the log file (an auto-checkpoint only
		// resets it — the file keeps its high-water size), so a large
		// adopted archive does not boot straight into drop_new.
		if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return errors.New(op).WithErr(err).WithMsg("truncate WAL after v1→v2 migration")
		}
		if err := s.migrate2to3(db); err != nil {
			return err
		}
		if err := s.migrate3to4(db); err != nil {
			return err
		}
		return s.migrate4to5(db)
	case v == "2":
		if err := s.migrate2to3(db); err != nil {
			return err
		}
		if err := s.migrate3to4(db); err != nil {
			return err
		}
		return s.migrate4to5(db)
	case v == "3":
		// A GENUINE v3 archive can hold synced rows, so 3→4's
		// legacy_synced backfill dirties ~every synced page — the same
		// whole-archive WAL peak as 1→2, gated the same way. (The chained
		// paths above need no second gate: pre-v3 rows are all synced=0,
		// so their 3→4 backfill dirties nothing.)
		if err := s.migrationBackfillGate(op, "v3→v4"); err != nil {
			return err
		}
		if err := s.migrate3to4(db); err != nil {
			return err
		}
		if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return errors.New(op).WithErr(err).WithMsg("truncate WAL after v3→v4 migration")
		}
		return s.migrate4to5(db)
	case v == "4":
		return s.migrate4to5(db)
	default:
		return errors.New(op).WithMsgf("evidence.db schema version %q is newer than this build understands", v)
	}
}

// migrationBackfillGate refuses a whole-archive backfill migration whose
// transaction cannot fit under the cap: the backfill dirties ~every
// affected page, so the WAL peak is of the order of the db file — far past
// the writer's fixed headroom and unmeasurable in time (pages spill to the
// -wal mid-transaction and rollback does not shrink it; measured
// 2026-08-10, modernc.org/sqlite v1.48.1 — a 12.4 MB archive held 10.6 MB
// of -wal before commit, unchanged after rollback). Projection: one WAL
// frame per dirtied page (frame = page + 24 B, sqlite.org/walformat.html),
// bounded by db size + ~6% + fixed slack. Refusal writes nothing and
// surfaces through Start's fail-soft: evidence idle, decoding continues.
func (s *Service) migrationBackfillGate(op errors.Op, label string) error {
	var dbSize int64
	if fi, err := os.Stat(s.cfg.Path); err == nil {
		dbSize = fi.Size()
	}
	usage, err := s.physicalUsage()
	if err != nil {
		// Fail-closed (Q1): without current usage we cannot bound the projected
		// peak, so refuse — evidence stays idle, decoding continues (Start fail-soft).
		return errors.New(op).WithErr(err).WithMsgf(
			"cannot measure physical usage to bound the %s migration peak; evidence stays idle", label)
	}
	if projected := usage + dbSize + dbSize/16 + migrationSlackBytes; projected > s.cfg.CapBytes {
		return errors.New(op).WithMsgf(
			"%s migration projected peak %d B would exceed the cap %d B; evidence stays idle until the cap is raised or the archive shrinks",
			label, projected, s.cfg.CapBytes)
	}
	return nil
}

// migrate4to5 adds the receipts' dial context (package-review P1-4c) —
// conditional like the profiles halves: chained archives created
// retention_records from the current DDL.
func (s *Service) migrate4to5(db *sql.DB) error {
	const op errors.Op = "evidence.Service.migrate4to5"
	tx, err := db.Begin()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("begin v4→v5 migration")
	}
	defer func() { _ = tx.Rollback() }()
	var hasCol int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('retention_records') WHERE name = 'dial_mhz'`).Scan(&hasCol); err != nil {
		return errors.New(op).WithErr(err).WithMsg("probe retention_records shape")
	}
	stmts := migrate4to5SQL
	if hasCol == 0 {
		stmts = migrateRetention4to5SQL + stmts
	}
	if _, err := tx.Exec(stmts); err != nil {
		return errors.New(op).WithErr(err).WithMsg("apply v4→v5 migration")
	}
	if err := tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("commit v4→v5 migration")
	}
	return nil
}

// migrate3to4 adds the retention columns + table (retention-slice rulings
// 2026-08-10). Like 2→3: ADD COLUMN is schema-page-only, far under the
// reserved headroom, so no cap gate.
func (s *Service) migrate3to4(db *sql.DB) error {
	return s.applyAdditiveMigration(db, "evidence.Service.migrate3to4",
		"sync_outcome", migrateProfiles3to4SQL, migrate3to4SQL)
}

// migrate2to3 adds the v3 sync columns. No cap gate: ADD COLUMN without a
// non-constant default touches the schema page only — no row rewrite
// (https://sqlite.org/lang_altertable.html, ALTER TABLE ADD COLUMN) — so
// the growth is bounded far under the reserved headroom, unlike 1→2's
// every-row backfill.
func (s *Service) migrate2to3(db *sql.DB) error {
	return s.applyAdditiveMigration(db, "evidence.Service.migrate2to3",
		"offered_at", migrateProfiles2to3SQL, migrate2to3SQL)
}

// applyAdditiveMigration runs one additive step whose profiles half is
// CONDITIONAL: a chained archive created that table from the current DDL,
// which already carries the newest columns, so probeColumn decides whether
// profilesSQL applies.
func (s *Service) applyAdditiveMigration(db *sql.DB, op errors.Op, probeColumn, profilesSQL, mainSQL string) error {
	tx, err := db.Begin()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("begin migration")
	}
	defer func() { _ = tx.Rollback() }()
	var hasCols int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('profiles') WHERE name = ?`, probeColumn).Scan(&hasCols); err != nil {
		return errors.New(op).WithErr(err).WithMsg("probe profiles shape")
	}
	stmts := mainSQL
	if hasCols == 0 {
		stmts = profilesSQL + stmts
	}
	if _, err := tx.Exec(stmts); err != nil {
		return errors.New(op).WithErr(err).WithMsg("apply migration")
	}
	if err := tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("commit migration")
	}
	return nil
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

// measurementDrop fails a slot closed (L2 · Q1): a measurement REQUIRED to
// authorize its write was unknown, so the archive could not be measured. Counts
// the slot ONCE as measurement_error and accumulates the loss IN MEMORY — no
// per-slot DB write, because the failure may BE the database; the accumulator
// persists on the first recovered write (§4.1). Decoding continues. Drives the
// tracker's bounded degraded transition, tagged with the granular operation.
func (s *Service) measurementDrop(sc SlotCapture, err error) {
	s.mu.Lock()
	s.dropped++
	s.accumulateLocked(sc, lossReasonMeasurement)
	s.mu.Unlock()
	s.retHealth.dropped()
	s.retHealth.fail(opOf(err), err)
}

// checkpointTruncate folds the WAL, returning any failure tagged with op (a reader
// blocking the truncate is not a failure — busy is not inspected here).
func (s *Service) checkpointTruncate(op string) error {
	if err := measureFail(op); err != nil {
		return err
	}
	var busy, logFrames, checkpointed int64
	if err := s.db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).
		Scan(&busy, &logFrames, &checkpointed); err != nil {
		return measured(op, err)
	}
	return nil
}

func (s *Service) processSlot(sc SlotCapture) {
	if writerDelay > 0 {
		time.Sleep(writerDelay)
	}

	// Authorize chain (L2 · Q1 fail-closed): every measurement REQUIRED to decide
	// write-vs-drop. Any failure → do NOT write; drop this slot ONCE as
	// measurement_error; continue decoding; drive the tracker's bounded transition.
	usage, err := s.physicalUsage()
	if err != nil {
		s.measurementDrop(sc, err)
		return
	}
	watermark := s.cfg.CapBytes - headroomBytes

	// Retention slice (RT1): cap pressure purges instead of dropping when
	// something purgeable and receipt capacity exist — the slot then writes
	// into freed pages and the file stays bounded. Drop-new remains the
	// honest fallback, with Status.Retention.Pressure saying WHY.
	canWrite := usage < watermark
	if !canWrite {
		var ferr error
		if canWrite, ferr = s.tryFreeSpace(); ferr != nil {
			s.measurementDrop(sc, ferr)
			return
		}
	}
	// The authorize chain succeeded — the measurement layer is healthy for THIS
	// write attempt, whether it ends in a write or a genuinely-full cap-drop.

	s.mu.Lock()
	if !canWrite {
		// Genuine cap-drop (measured): decoding continues, only evidence writes
		// stop; the dropped span accumulates and its coalesced interval row is
		// refreshed within the reserved headroom. Counted ONCE as cap here — a
		// post-decision measurement/checkpoint failure below must not reclassify it.
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
		// §4.1: once usage nears the cap, the accumulator extends IN MEMORY —
		// persisting per drop would consume the very reserve the ceiling protects.
		// The deferPersist usage read is POST-decision (the slot is already counted):
		// if it fails, be conservative (defer) and report it via the tracker WITHOUT
		// reclassifying the already-justified cap drop.
		pu, postErr := s.physicalUsage()
		deferPersist := postErr != nil || pu >= s.cfg.CapBytes-writeWalReserveBytes
		if !deferPersist {
			s.refreshLossLocked()
		}
		s.mu.Unlock()
		// The checkpoint runs OUTSIDE s.mu (85f1481a review P1): a reader can block a
		// truncating checkpoint up to the 2 s busy_timeout, and CaptureSlot needs
		// s.mu to stamp — holding it across the checkpoint would stall the decode path.
		if deferPersist {
			if checkpointHook != nil {
				checkpointHook()
			}
			if e := s.checkpointTruncate("checkpoint_drop"); e != nil && postErr == nil {
				postErr = e
			}
		}
		if postErr != nil {
			// Report the post-decision measurement/checkpoint failure — the slot stays
			// counted once as cap; only the tracker's health flips.
			s.retHealth.fail(opOf(postErr), postErr)
		} else {
			s.retHealth.ok()
		}
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
		// Writer/measurement-error recovery: persist the accumulated interval with
		// priority, before any new evidence (§4.1).
		s.closeLossLocked()
	}
	s.mu.Unlock()
	s.retHealth.ok() // authorize chain succeeded → measurement layer healthy

	if err := s.writeSlot(sc); err != nil {
		s.log.WarnWith().Err(err).
			Str("slot", sc.SlotStart.UTC().Format(time.RFC3339)).
			Msg("evidence: slot write failed; dropped and counted")
		s.mu.Lock()
		s.dropped++
		s.accumulateLocked(sc, lossReasonWriter)
		s.mu.Unlock()
		return
	}
	// §5 live lane: the slot just committed — wake the sync loop (SY5).
	s.notifyLive()
	// A successful persist ends any writer-queue backpressure episode (L3 recovery:
	// the next slot after the queue drained). No-op when no episode is active.
	s.queueLoss.Recover()
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
		// The §4.2 stamp travels with the record: exactly one of the pair is
		// non-NULL (CaptureSlot stamped it at emission; O4/O6).
		var pUUID, pReason any
		if sc.profileUUID != "" {
			pUUID = sc.profileUUID
		} else if sc.unprofiledReason != "" {
			pReason = sc.unprofiledReason
		}
		if _, err := tx.Exec(
			`INSERT INTO observations (uuid, slot_start_utc, dial_mhz, dial_tracked,
				freq_hz, dt_sec, snr, payload, parse_status, text,
				prov_algorithm, prov_ap_profile, prov_ap_source,
				metric_sync, metric_hard_sync, metric_costas_geo, metric_costas_min_block,
				metric_blocks, metric_hard_errors, metric_dmin,
				decoder_build, profile_uuid, unprofiled_reason)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			utils.NewUUIDv7At(sc.SlotStart), slotUTC, sc.DialMHz, boolInt(sc.DialTracked),
			m.FreqHz, m.DTSec, m.SNR, m.Payload[:], m.ParseStatus.String(), text,
			string(m.Provenance.Algorithm), m.Provenance.APProfile, m.Provenance.APSource,
			m.Sync, m.HardSync, m.CostasGeo, m.CostasMinBlock,
			m.Blocks, m.HardErrors, m.DMin,
			s.decoderBuild, pUUID, pReason); err != nil {
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
	// sealed tracks the accumulator's lifecycle (RT10): 0 while the row is
	// still being refreshed in place, 1 the moment it closes — only sealed
	// rows are sync-eligible, because an offered UUID's content must never
	// change afterward.
	_, err := s.db.Exec(
		`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz, sealed)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(uuid) DO UPDATE SET
			end_utc = excluded.end_utc,
			slots = excluded.slots,
			observations = excluded.observations,
			dial_mhz = excluded.dial_mhz,
			sealed = excluded.sealed`,
		l.uuid, l.start.Format(time.RFC3339), l.end.Format(time.RFC3339),
		l.slots, l.observations, l.reason, remoteNeverOffered, l.dialMHz, boolInt(clear))
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
func (s *Service) physicalUsage() (int64, error) {
	var total int64
	// The main db exists once the archive is open, so ANY stat failure on it is a
	// real measurement error (fail-closed). WAL/SHM are optional siblings: a MISSING
	// one is a legitimate 0 (Q1), but any other stat error on them is still an error.
	probes := []struct {
		path, op string
		optional bool
	}{
		{s.cfg.Path, "stat_db", false},
		{s.cfg.Path + "-wal", "stat_wal", true},
		{s.cfg.Path + "-shm", "stat_shm", true},
	}
	for _, p := range probes {
		if err := measureFail(p.op); err != nil {
			return 0, err
		}
		fi, err := os.Stat(p.path)
		if err != nil {
			if p.optional && os.IsNotExist(err) {
				continue
			}
			return 0, measured(p.op, err)
		}
		total += fi.Size()
	}
	return total, nil
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
