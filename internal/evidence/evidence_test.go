package evidence

/*
   Acceptance criteria for the §4.1 evidence writer (first capture slice;
   docs/v2-design/spot-network/spot-network-design.md §4/§4.1/§8, operator
   decisions 2026-08-10 — null profiles, watermark drop-new, 500 MiB default):

   EV1  Capture is a default-off consent layer: a disabled service creates NO
        evidence.db, writes nothing, and says so in Status — distinguishable
        from a broken writer, which would have been enabled and logged.
   EV2  Observation fidelity: every decode in a captured slot — parsed,
        unsupported-payload and own-loopback alike, the evidence stream is
        UNFILTERED — lands as one row carrying UUIDv7 identity, slot time,
        dial context, offset/dt/SNR, the verified 77-bit payload, parse
        status, text (NULL when the payload has no canonical text), AP/decoder
        provenance, decode metrics and the decoder build. profile_uuid is
        NULL, meaning "no profile was recorded" — never "pending" (§5.4
        amendment).
   EV3  Every captured physical slot gets exactly one coverage row with its
        true outcome (decoded / no_decode / tx / dial_changed / decoder_error
        / capture_dropped); non-decoded outcomes carry no observations.
   EV4  A slot's coverage row and its observations are one transaction: the
        archive never shows observations for a slot without that slot's
        coverage row.
   EV5  Capture never blocks the caller: CaptureSlot is a bounded non-blocking
        enqueue; under writer pressure slots DROP and are counted, the caller
        never waits.
   EV6  Bounded disk, drop BEFORE the cap: past the soft watermark (cap minus
        reserved headroom) evidence writes stop while decoding continues; the
        dropped span is a coalesced loss interval (reason, slots,
        observations, remote_status never_offered) written within the
        reserved headroom; Status exposes the drop_new state and usage.
   EV7  Capture RESUMES when capacity returns (rows freed / cap raised):
        the accumulated interval is closed and new slots write again.
   EV8  Disabling capture stops new writes and deletes nothing: an existing
        archive opened by a disabled service keeps its rows and gains none.
   EV9  The local honesty surfaces exist: Status carries usage bytes,
        cap/watermark, state, observation and unprofiled-observation counts
        (the §5.4 missing-profile guardrail), and dropped-slot counts.

   Failure injection is real where possible: the cap path is exercised with a
   genuinely small cap via the headroom package var (captureLinger pattern),
   resume by genuinely freeing file space, writer pressure via the queue-size
   and writer-delay package vars. No mocks, real SQLite files (house style).
*/

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func testConfig(t *testing.T, capture bool) Config {
	t.Helper()
	return Config{
		Capture:  capture,
		CapBytes: 524288000,
		Path:     filepath.Join(t.TempDir(), "evidence.db"),
	}
}

func newRunning(t *testing.T, cfg Config) *Service {
	t.Helper()
	s := New(cfg, logging.Noop())
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(s.Stop)
	return s
}

// openRaw opens a read-only second connection to the archive for assertions.
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return n
}

// drain waits until the writer has consumed everything enqueued so far.
func drain(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.queueEmpty() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("writer did not drain in time")
}

func slotAt(sec int) time.Time {
	return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC).Add(time.Duration(sec) * time.Second)
}

// richSlot is the EV2 fixture: a parsed decode, a CRC-valid unsupported
// payload (text-less), and our own loopback CQ — all three must be stored,
// which is what distinguishes evidence capture from curated capture.
func richSlot(start time.Time) SlotCapture {
	return SlotCapture{
		SlotStart:   start,
		DialMHz:     14.074,
		DialTracked: true,
		Outcome:     OutcomeDecoded,
		Decodes: []goft8.DecodedMessage{
			{
				Text:        "CQ A61DI LL64",
				Payload:     goft8.Payload77{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a},
				ParseStatus: goft8.ParseStatusParsed,
				Provenance:  goft8.DecodeProvenance{Algorithm: goft8.DecodeAlgorithmBP},
				SNR:         -8, FreqHz: 1204.5, DTSec: 0.3,
				Sync: 1.9, HardSync: 5, CostasGeo: 0.8, CostasMinBlock: 0.5,
				Blocks: 3, HardErrors: 2, DMin: 12.5,
			},
			{
				Payload:     goft8.Payload77{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22, 0x33, 0x44},
				ParseStatus: goft8.ParseStatusUnsupported,
				Provenance:  goft8.DecodeProvenance{Algorithm: goft8.DecodeAlgorithmOSD},
				SNR:         -19, FreqHz: 2410.0, DTSec: -0.1,
			},
			{
				Text:        "CQ K1ABC FN42",
				Payload:     goft8.Payload77{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xa0},
				ParseStatus: goft8.ParseStatusParsed,
				Provenance:  goft8.DecodeProvenance{Algorithm: goft8.DecodeAlgorithmBP},
				SNR:         30, FreqHz: 800.0, DTSec: 0.5,
			},
		},
	}
}

// EV1 — a disabled service is inert and says so.
func TestDisabledService_CreatesNothing(t *testing.T) {
	cfg := testConfig(t, false)
	s := newRunning(t, cfg)

	s.CaptureSlot(richSlot(slotAt(0)))

	if _, err := os.Stat(cfg.Path); !os.IsNotExist(err) {
		t.Fatalf("disabled service must create no evidence.db; stat err = %v", err)
	}
	st := s.Status()
	if st.Enabled || st.State != StateDisabled {
		t.Fatalf("Status = %+v, want disabled", st)
	}
}

// EV2 — full observation fidelity for the unfiltered stream.
func TestCaptureSlot_StoresRichObservations(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	s.CaptureSlot(richSlot(slotAt(0)))
	drain(t, s)

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM observations`); n != 3 {
		t.Fatalf("observations = %d, want 3 (parsed + unsupported + own loopback all stored)", n)
	}

	// The parsed row, in full.
	var (
		uuid, slotUTC, status, alg, build string
		text, profile                     sql.NullString
		dial, freq, dt                    float64
		tracked, snr                      int
		payload                           []byte
		mSync                             float64
		mBlocks                           int
	)
	err := db.QueryRow(`SELECT uuid, slot_start_utc, dial_mhz, dial_tracked, freq_hz, dt_sec, snr,
			payload, parse_status, text, prov_algorithm, metric_sync, metric_blocks,
			decoder_build, profile_uuid
		FROM observations WHERE parse_status = 'parsed' AND snr = -8`).
		Scan(&uuid, &slotUTC, &dial, &tracked, &freq, &dt, &snr,
			&payload, &status, &text, &alg, &mSync, &mBlocks, &build, &profile)
	if err != nil {
		t.Fatalf("parsed row: %v", err)
	}
	if len(uuid) != 36 {
		t.Errorf("uuid = %q, want UUIDv7 shape", uuid)
	}
	if slotUTC != "2026-08-10T12:00:00Z" || dial != 14.074 || tracked != 1 {
		t.Errorf("slot context = %s/%v/%d, want 2026-08-10T12:00:00Z/14.074/1", slotUTC, dial, tracked)
	}
	if freq != 1204.5 || dt != 0.3 || snr != -8 {
		t.Errorf("signal fields = %v/%v/%d", freq, dt, snr)
	}
	if len(payload) != 10 || payload[0] != 0x01 || payload[9] != 0x0a {
		t.Errorf("payload = %x, want the 10 verified bytes", payload)
	}
	if !text.Valid || text.String != "CQ A61DI LL64" {
		t.Errorf("text = %+v, want the canonical text", text)
	}
	if alg != "bp" || mSync != 1.9 || mBlocks != 3 {
		t.Errorf("provenance/metrics = %s/%v/%d", alg, mSync, mBlocks)
	}
	if build == "" {
		t.Error("decoder_build must not be empty")
	}
	if profile.Valid {
		t.Errorf("profile_uuid = %q, want NULL (explicitly unprofiled)", profile.String)
	}

	// The unsupported payload stores with NULL text.
	var unsupText sql.NullString
	if err := db.QueryRow(`SELECT text FROM observations WHERE parse_status = 'unsupported'`).
		Scan(&unsupText); err != nil {
		t.Fatalf("unsupported row: %v", err)
	}
	if unsupText.Valid {
		t.Errorf("unsupported text = %q, want NULL (no canonical text exists)", unsupText.String)
	}

	// One coverage row for the slot, outcome decoded, counting its decodes.
	var outcome string
	var decodes int
	if err := db.QueryRow(`SELECT outcome, decode_count FROM coverage WHERE slot_start_utc = ?`,
		"2026-08-10T12:00:00Z").Scan(&outcome, &decodes); err != nil {
		t.Fatalf("coverage row: %v", err)
	}
	if outcome != "decoded" || decodes != 3 {
		t.Errorf("coverage = %s/%d, want decoded/3", outcome, decodes)
	}
}

// EV3 — non-decoded outcomes write coverage only.
func TestCaptureSlot_CoverageOnlyOutcomes(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	outcomes := []SlotOutcome{OutcomeNoDecode, OutcomeTx, OutcomeDialChanged, OutcomeDecoderError, OutcomeCaptureDropped}
	for i, o := range outcomes {
		s.CaptureSlot(SlotCapture{SlotStart: slotAt(15 * i), DialMHz: 14.074, DialTracked: true, Outcome: o})
	}
	drain(t, s)

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM coverage`); n != len(outcomes) {
		t.Fatalf("coverage rows = %d, want %d", n, len(outcomes))
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM observations`); n != 0 {
		t.Fatalf("observations = %d, want 0", n)
	}
	for _, o := range outcomes {
		if n := countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE outcome = ?`, string(o)); n != 1 {
			t.Errorf("outcome %s rows = %d, want 1", o, n)
		}
	}
}

// EV4 — no slot ever shows observations without its coverage row.
func TestArchive_ObservationsAlwaysCovered(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	for i := 0; i < 6; i++ {
		if i%2 == 0 {
			s.CaptureSlot(richSlot(slotAt(15 * i)))
		} else {
			s.CaptureSlot(SlotCapture{SlotStart: slotAt(15 * i), Outcome: OutcomeTx})
		}
	}
	drain(t, s)

	db := openRaw(t, cfg.Path)
	orphans := countRows(t, db,
		`SELECT COUNT(*) FROM observations o
		 WHERE NOT EXISTS (SELECT 1 FROM coverage c WHERE c.slot_start_utc = o.slot_start_utc)`)
	if orphans != 0 {
		t.Fatalf("%d observations without a coverage row for their slot", orphans)
	}
}

// EV5 — the caller never blocks; overflow drops are counted.
func TestCaptureSlot_NeverBlocksUnderPressure(t *testing.T) {
	oldQ, oldD := writerQueueSize, writerDelay
	writerQueueSize, writerDelay = 1, 50*time.Millisecond
	defer func() { writerQueueSize, writerDelay = oldQ, oldD }()

	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	start := time.Now()
	const fed = 10
	for i := 0; i < fed; i++ {
		s.CaptureSlot(SlotCapture{SlotStart: slotAt(15 * i), Outcome: OutcomeNoDecode})
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("CaptureSlot blocked: %d calls took %v", fed, took)
	}
	drain(t, s)
	st := s.Status()
	if st.DroppedSlots == 0 {
		t.Fatal("a stalled writer must count its drops")
	}
	if st.DroppedSlots >= fed {
		t.Fatalf("dropped %d of %d — writer consumed nothing", st.DroppedSlots, fed)
	}
}

// EV6 — past the watermark: drop-new state, loss interval, decoding untouched.
func TestCap_DropsNewBeforeTheLimit(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 64 * 1024 // must absorb shm (32 KiB) + one slot txn of WAL growth — see headroomBytes
	defer func() { headroomBytes = oldHeadroom }()
	// Retention-slice amendment (2026-08-10): at cap pressure the writer now
	// PURGES unsynced history before it ever drops (full §4.1), so this
	// test's drop-new boundary needs purging refused — a zero metadata
	// budget forbids the receipt, and an unreceipted purge must never
	// happen (RT6). The cap boundary this test pins is unchanged.
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 0
	defer func() { metadataBudgetBytes = oldBudget }()

	cfg := testConfig(t, true)
	cfg.CapBytes = 256 * 1024 // watermark ≈ 252 KiB — trips via db+WAL growth within ~tens of slots
	s := newRunning(t, cfg)

	// Feed rich slots until the watermark trips, bounded well above need.
	tripped := false
	for i := 0; i < 400; i++ {
		s.CaptureSlot(richSlot(slotAt(15 * i)))
		drain(t, s)
		if s.Status().State == StateDropNew {
			tripped = true
			break
		}
	}
	if !tripped {
		t.Fatal("cap never tripped — watermark not enforced")
	}

	// Retention amendment (2026-08-10): past the watermark, the in-band WAL
	// fold may legitimately REVEAL capacity and resume capture (the
	// documented resume-on-capacity), so "nothing writes after the trip" is
	// no longer the criterion. What must hold: usage NEVER exceeds the cap,
	// and every drop is recorded as never_offered cap loss.
	for i := 400; i < 404; i++ {
		s.CaptureSlot(richSlot(slotAt(15 * i)))
		drain(t, s)
		if u := s.physicalUsage(); u > cfg.CapBytes {
			t.Fatalf("usage %d exceeded the cap %d past the watermark", u, cfg.CapBytes)
		}
	}
	st := s.Status()
	if st.DroppedSlots == 0 {
		t.Error("the watermark trip recorded no dropped slots")
	}
	if st.UsageBytes == 0 || st.UsageBytes > cfg.CapBytes {
		t.Errorf("Status usage = %d, want within the cap %d", st.UsageBytes, cfg.CapBytes)
	}
	s.Stop() // persists the accumulator with priority, whatever the band did

	db := openRaw(t, cfg.Path)
	var slots, obs int
	var reason, remote string
	if err := db.QueryRow(`SELECT slots, observations, reason, remote_status FROM loss_intervals ORDER BY uuid ASC LIMIT 1`).
		Scan(&slots, &obs, &reason, &remote); err != nil {
		t.Fatalf("loss interval: %v", err)
	}
	if slots < 1 || obs < 3 {
		t.Errorf("loss interval spans %d slots / %d observations, want ≥ the tripping rich slot", slots, obs)
	}
	if reason != "cap" || remote != "never_offered" {
		t.Errorf("loss = %s/%s, want cap/never_offered", reason, remote)
	}
}

// EV7 — freeing capacity resumes capture and closes the interval.
func TestCap_ResumesWhenCapacityReturns(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 64 * 1024 // must absorb shm (32 KiB) + one slot txn of WAL growth — see headroomBytes
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true)
	cfg.CapBytes = 256 * 1024
	s := newRunning(t, cfg)

	for i := 0; i < 400 && s.Status().State != StateDropNew; i++ {
		s.CaptureSlot(richSlot(slotAt(15 * i)))
		drain(t, s)
	}
	if s.Status().State != StateDropNew {
		t.Fatal("setup: cap never tripped")
	}
	s.CaptureSlot(richSlot(slotAt(15 * 500))) // one dropped slot in the interval
	drain(t, s)

	// Free genuine space through the service's own writer connection, then
	// checkpoint so the physical files shrink.
	if err := s.compactForTest(); err != nil {
		t.Fatalf("free space: %v", err)
	}

	resumeStart := slotAt(15 * 600)
	s.CaptureSlot(richSlot(resumeStart))
	drain(t, s)

	st := s.Status()
	if st.State != StateCapturing {
		t.Fatalf("state = %s, want capturing after capacity returned", st.State)
	}
	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE slot_start_utc = ?`,
		resumeStart.UTC().Format(time.RFC3339)); n != 1 {
		t.Fatalf("post-resume slot rows = %d, want 1 (capture resumed)", n)
	}
}

// EV8 — a disabled service leaves an existing archive intact and untouched.
func TestDisable_KeepsRowsAddsNone(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	s.CaptureSlot(richSlot(slotAt(0)))
	drain(t, s)
	s.Stop()

	db := openRaw(t, cfg.Path)
	kept := countRows(t, db, `SELECT COUNT(*) FROM observations`)
	if kept == 0 {
		t.Fatal("setup: no rows to keep")
	}

	cfg.Capture = false
	s2 := newRunning(t, cfg)
	s2.CaptureSlot(richSlot(slotAt(150)))
	if n := countRows(t, db, `SELECT COUNT(*) FROM observations`); n != kept {
		t.Fatalf("disabled service changed the archive: %d → %d", kept, n)
	}
}

// EV9 — the honesty surfaces: usage, counts, unprofiled tally.
func TestStatus_SurfacesUsageAndUnprofiledCount(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	s.CaptureSlot(richSlot(slotAt(0)))
	s.CaptureSlot(richSlot(slotAt(15)))
	drain(t, s)

	st := s.Status()
	if !st.Enabled || st.State != StateCapturing {
		t.Fatalf("Status = %+v, want enabled/capturing", st)
	}
	if st.UsageBytes == 0 {
		t.Error("usage bytes = 0, want the physical file sizes")
	}
	if st.CapBytes != cfg.CapBytes || st.WatermarkBytes != cfg.CapBytes-headroomBytes {
		t.Errorf("cap/watermark = %d/%d", st.CapBytes, st.WatermarkBytes)
	}
	if st.Observations != 6 || st.UnprofiledObservations != 6 {
		t.Errorf("observations/unprofiled = %d/%d, want 6/6 (all rows NULL-profiled this slice)",
			st.Observations, st.UnprofiledObservations)
	}
}
