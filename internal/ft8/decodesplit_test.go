package ft8

/*
   Design §4 prerequisite 2 (docs/v2-design/spot-network/spot-network-design.md):
   the decode path splits at the RICH go-ft8 result — an evidence branch
   (unfiltered, unprojected; the future evidence.db writer taps it) and the
   curated operational stream (own-TX filter + parse-status filter + the
   four-field projection) feeding the sequencer, Band Activity, the RX decode
   log and PSK Reporter — and the live path adopts ONE stateful goft8.Decoder
   per receiver stream (capture session), gaining cross-slot callsign-hash
   resolution and A7 hints.

   ACCEPTANCE CRITERIA (operator-checked 2026-08-09, this session):

   AC1  A CRC-valid payload whose text is unsupported reaches NO curated
        surface — no Band Activity row, no RX-log line, no sequencer action,
        no PSK spot — distinguishable from "never decoded" because the rich
        result upstream still carries it (payload + parse status).
   AC2  The COMPLETE decode set (all parse statuses, own-TX included,
        provenance/metrics intact) is available at a seam upstream of every
        filter.
   AC3  Own-TX filtering is curated-only: a loopback decode of our own
        transmission reaches no curated consumer but is present in the rich
        result.
   AC4  Statefulness is operator-observable as hash resolution: a nonstandard
        call heard in full in slot N renders RESOLVED (not "<...>") when slot
        N+2 carries it by hash.
   AC5  Decoder state is per capture session: after release + re-acquire, a
        hash learned in the old session does not resolve in the new one.
   AC6  Every skipped TX slot advances decoder state via one zero-slot
        decode — current A7 parity bucket cleared, parity advanced, hash
        table preserved — and the zero decode's output is DISCARDED (no
        curated rows, no evidence); the published empty slot report is
        unchanged. Operator decision 2026-08-09: measured cost 6–8 ms
        clearing a 26–40-hint bucket, 0.09–0.12 ms once empty; replace with
        goft8 Decoder.SkipSlot() when a release provides it.
        AMENDED (review cd1757a7cda2 P1): a DIAL-MOVED slot RESETS the
        decoder instead of advancing it. The receiver context changed, and
        the hash table is band-blind — carrying it across a QSY lets a 10/12-
        bit hash reference on the new band resolve to a call heard on the
        old one, and a collision then renders a valid-looking but WRONG call
        into Band Activity and PSK Reporter. Exactly one slot is flagged per
        QSY (per-batch dial sampling brackets every instant), so reset-on-
        moved covers every band change; a TX slot is the same receiver
        context and must keep its state — that split is the P1 fix.
   AC7  No wire-visible change for parsed-only slots: ft8-decode SSE shape,
        RX-log format, sequencer feed and PSK sink payload are identical —
        frozen by the characterization tests below BEFORE the refactor.
   AC8  (review cd1757a7cda2 P2) Decoder state advances for OMITTED physical
        slots, not only delivered ones: the scheduler skips a boundary
        serviced over two seconds late and drops slots on a full channel
        (sch.Dropped()), so the loop must detect StartUTC gaps and advance
        once per missing slot — an odd-length gap otherwise swaps the two
        parity-indexed A7 buckets for the rest of the session. Advancing
        (not resetting) is deliberate: the receiver context is unchanged, so
        the hash table must survive a gap; per-slot cost after the first two
        skips is ~0.1 ms (bucket already empty), so no gap-length cap is
        needed.
        AMENDED (review 75f40264fe2b P1): "receiver context unchanged" must
        be CHECKED, not assumed — the DialChanged flag rides the slot that
        spans the move and can be dropped with it, so a dial DIFFERENCE
        between consecutive delivered slots resets the decoder exactly as a
        delivered moved slot would; the gap advance runs only when the dial
        held.
        AMENDED (review 68514620 P1+P2): gap inference alone cannot see
        omissions at SESSION BOUNDARIES — before the first delivery there is
        no predecessor, and after the last there is no successor. The
        scheduler therefore tracks its consecutive undelivered run (channel-
        full drops + lateness skips; cold-start boundaries excluded — no
        session audio existed to lose), stamps it on each delivered slot
        (Slot.OmittedBefore, read by the loop ONLY for the first slot), and
        the release path emits the trailing run after the drain. And an
        omitted slot's evidence row claims NO dial context (untracked,
        dial 0) — it was never observed, so inheriting a neighbour's
        tracking state would be an unmeasured assertion.

   TESTABILITY HONESTY (AC6): the parity-ALIGNMENT consequence is observable
   only through A7-assisted recovery of a signal too weak for the normal
   passes — not deterministically synthesizable in CI. The behavioural anchor
   here is hash-survival-across-skip (real audio, real decoder, operator-
   visible text); zero-slot parity semantics are go-ft8's documented
   mechanics (a7.go: history[seq] rewritten per call, seq toggles per call).

   FIXTURE DISCRIMINATION: the unsupported-payload half of AC1/AC2 cannot be
   synthesized as audio (the encoder deliberately refuses non-standard
   families), so it is pinned at the curate seam with hand-built messages
   (TestCurateDecodes) while the audio-level tests pin the own-TX half and
   the loop's routing THROUGH that seam. Composition, stated: the loop
   provably routes rich results through curateDecodes (own-TX fixture), and
   curateDecodes provably drops unparsed rows.
*/

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// mixSlot synthesises one slot carrying several messages at distinct audio
// offsets, each at ~45% amplitude so the sum never clips.
func mixSlot(t *testing.T, msgs map[string]float64) []int16 {
	t.Helper()
	sum := make([]int32, SlotSamples)
	for text, offset := range msgs {
		slot, err := EncodeToSlot(text, offset, 0.5)
		if err != nil {
			t.Fatalf("EncodeToSlot(%q): %v", text, err)
		}
		for i, v := range slot {
			sum[i] += int32(float64(v) * 0.45)
		}
	}
	out := make([]int16, SlotSamples)
	for i, v := range sum {
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
	return out
}

// sinkRecorder captures every DecodeReport handed to the PSK-shaped sink.
type sinkRecorder struct {
	mu      sync.Mutex
	reports []DecodeReport
}

func (r *sinkRecorder) record(rep DecodeReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, rep)
}

func (r *sinkRecorder) all() []DecodeReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]DecodeReport(nil), r.reports...)
}

// drainDecodeEvents non-blockingly drains the subscriber buffer, returning the
// ft8-decode payloads and the count of occupancy events. Call only after
// decodeLoop has returned — publishes are synchronous, so everything is
// buffered by then.
func drainDecodeEvents(ch <-chan hubEvent) (decodes []DecodeReport, occupancy int) {
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			switch e.name {
			case EventDecode:
				decodes = append(decodes, e.payload.(DecodeReport))
			case EventOccupancy:
				occupancy++
			}
		default:
			return
		}
	}
}

// recentEvenSlotStart returns a recent UTC slot boundary with EVEN parity
// ((unix/15)%2 == 0 ⇔ unix%30 == 0), so tests can feed slots whose parity is
// the OPPOSITE of a session started with txParity "odd" — the sequencer
// listens on them and never fires a rung mid-test.
func recentEvenSlotStart() time.Time {
	now := time.Now().UTC()
	return time.Unix(now.Unix()-now.Unix()%30, 0).UTC()
}

// newSplitHarness builds a TX-capable service with a hub subscription, a sink
// recorder and a file-backed RX decode log — every curated surface observable.
func newSplitHarness(t *testing.T) (s *Service, sink *sinkRecorder, events <-chan hubEvent, decLogPath string) {
	t.Helper()
	s = newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	sink = &sinkRecorder{}
	s.SetDecodeSink(sink.record)
	dir := t.TempDir()
	decLogPath = filepath.Join(dir, "all.txt")
	dl := openDecodeLog(decLogPath, dir, logging.Noop())
	if dl == nil {
		t.Fatal("openDecodeLog returned nil")
	}
	s.decLog.Store(dl)
	events, unsub := s.hub.subscribe()
	t.Cleanup(unsub)
	return s, sink, events, decLogPath
}

// readDecodeLog flushes and reads the RX decode log's content.
func readDecodeLog(t *testing.T, s *Service, path string) string {
	t.Helper()
	s.decLog.Load().Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read decode log: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// CHARACTERIZATION (AC7 + the own-TX half of AC1/AC3): frozen CURRENT
// behaviour, written and green BEFORE the split so the refactor cannot drift
// the curated wire surfaces.
// ---------------------------------------------------------------------------

// TestDecodeLoop_CuratedSurfaces_Frozen pins all four curated surfaces for one
// real mixed slot: our own loopback CQ (the session's pinned call transmitting
// off rig audio bleed) plus another station's CQ. The own row must reach NO
// surface; the other station must reach ALL of them with the projected fields.
// The fixture discriminates: an implementation that skipped the own-TX filter
// would show 2 rows everywhere.
func TestDecodeLoop_CuratedSurfaces_Frozen(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, sink, events, logPath := newSplitHarness(t)

	// Arm + start a Call-CQ session so ActiveCallsign() pins our call. txParity
	// "odd" with even fed slots keeps the sequencer listening, never keying.
	if err := s.ArmTx(true); err != nil {
		t.Fatalf("ArmTx: %v", err)
	}
	if err := s.StartCallCq("K1ABC", "FN42", 1500, 14.074, "", "odd", 1); err != nil {
		t.Fatalf("StartCallCq: %v", err)
	}

	samples := mixSlot(t, map[string]float64{
		"CQ K1ABC FN42": 800,  // our own loopback → must be dropped everywhere
		"CQ A61DI LL64": 2200, // another station → must surface everywhere
	})
	ch := make(chan Slot, 1)
	ch <- Slot{StartUTC: recentEvenSlotStart(), Samples: samples, DialTracked: true, DialMHz: 14.074}
	close(ch)
	s.decodeLoop(ch)

	decodes, occupancy := drainDecodeEvents(events)
	if len(decodes) != 1 {
		t.Fatalf("ft8-decode events = %d, want 1", len(decodes))
	}
	rep := decodes[0]
	if len(rep.Decodes) != 1 {
		t.Fatalf("Band Activity rows = %d, want 1 (own CQ filtered, other kept): %+v",
			len(rep.Decodes), rep.Decodes)
	}
	row := rep.Decodes[0]
	if row.Text != "CQ A61DI LL64" {
		t.Errorf("surviving row = %q, want the other station's CQ", row.Text)
	}
	// The projection's four fields, populated and plausible (wire shape AC7).
	if row.FreqHz < 2150 || row.FreqHz > 2250 {
		t.Errorf("row FreqHz = %.1f, want ~2200", row.FreqHz)
	}
	if row.DTSec < 0.1 || row.DTSec > 0.9 {
		t.Errorf("row DTSec = %.2f, want ~0.5", row.DTSec)
	}
	if rep.DialMHz != 14.074 {
		t.Errorf("report DialMHz = %v, want the slot's captured dial 14.074", rep.DialMHz)
	}
	if occupancy != 1 {
		t.Errorf("occupancy events = %d, want 1 (RX slot with a known dial)", occupancy)
	}

	// PSK sink sees the same curated report.
	reports := sink.all()
	if len(reports) != 1 || len(reports[0].Decodes) != 1 || reports[0].Decodes[0].Text != "CQ A61DI LL64" {
		t.Errorf("sink reports = %+v, want exactly the curated report", reports)
	}

	// RX decode log carries one line for the other station, none for our own.
	logText := readDecodeLog(t, s, logPath)
	if !strings.Contains(logText, "~ CQ A61DI LL64") {
		t.Errorf("decode log missing the other station's RX line:\n%s", logText)
	}
	if strings.Contains(logText, "K1ABC") {
		t.Errorf("decode log carries our own loopback:\n%s", logText)
	}
}

// TestDecodeLoop_SkippedSlots_EmptySurfaces_Frozen pins the skip semantics for
// a TX slot and a dial-moved slot, each fed DECODABLE audio: the slot clock
// still ticks (one empty ft8-decode report per slot, sink included), nothing
// reaches the RX log, and no occupancy is published. The decodable audio is
// the discrimination — a loop that decoded anyway would surface the row.
func TestDecodeLoop_SkippedSlots_EmptySurfaces_Frozen(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, sink, events, logPath := newSplitHarness(t)

	audible, err := EncodeToSlot("CQ W1AW FN31", 1500, 0.5)
	if err != nil {
		t.Fatalf("EncodeToSlot: %v", err)
	}

	txStart := recentEvenSlotStart()
	s.markTxSlot(txStart)
	movedStart := txStart.Add(15 * time.Second)

	ch := make(chan Slot, 2)
	ch <- Slot{StartUTC: txStart, Samples: audible, DialTracked: true, DialMHz: 14.074}
	// A moved slot ships DialMHz 0 — the scheduler cannot place a window the
	// dial moved through (Slot doc); occupancy is then suppressed as unplaceable.
	ch <- Slot{StartUTC: movedStart, Samples: audible, DialTracked: true, DialMHz: 0, DialChanged: true}
	close(ch)
	s.decodeLoop(ch)

	decodes, occupancy := drainDecodeEvents(events)
	if len(decodes) != 2 {
		t.Fatalf("ft8-decode events = %d, want 2 (slot clock ticks on skipped slots)", len(decodes))
	}
	for i, rep := range decodes {
		if len(rep.Decodes) != 0 {
			t.Errorf("skipped slot %d published %d rows, want 0: %+v", i, len(rep.Decodes), rep.Decodes)
		}
	}
	if occupancy != 0 {
		t.Errorf("occupancy events = %d, want 0 (TX slot and moved slot both suppress it)", occupancy)
	}
	for i, rep := range sink.all() {
		if len(rep.Decodes) != 0 {
			t.Errorf("sink report %d has %d rows, want 0", i, len(rep.Decodes))
		}
	}
	if logText := readDecodeLog(t, s, logPath); strings.Contains(logText, "W1AW") {
		t.Errorf("skipped slots must write no RX lines, got:\n%s", logText)
	}
}

// ---------------------------------------------------------------------------
// ACCEPTANCE: the split + the stateful decoder. Written RED before the
// implementation.
// ---------------------------------------------------------------------------

// Hash-resolution fixture (AC4/AC5/AC6): "CQ G0XYZ IO91" teaches the decoder
// hash(G0XYZ) — unpackStandard saves every heard second call (go-ft8 v0.8.0
// unpack.go:236). "<G0XYZ> PJ4/K1ABC RR73" carries G0XYZ as a 12-bit hash
// (type 4) and renders "<...>" unless the decoder's hash history resolves it
// (encode.go doc). Both forms encode (verified against v0.8.0, 2026-08-09).
const (
	hashTeachText  = "CQ G0XYZ IO91"
	hashRefText    = "<G0XYZ> PJ4/K1ABC RR73"
	hashUnresolved = "<...> PJ4/K1ABC RR73"
	hashResolved   = "<G0XYZ> PJ4/K1ABC RR73"
)

func encodeSlotOrFatal(t *testing.T, text string, offsetHz float64) []int16 {
	t.Helper()
	slot, err := EncodeToSlot(text, offsetHz, 0.5)
	if err != nil {
		t.Fatalf("EncodeToSlot(%q): %v", text, err)
	}
	return slot
}

func textsOf(msgs []goft8.DecodedMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Text
	}
	return out
}

// TestCurateDecodes pins the curated seam every view consumer sits behind
// (AC1 + AC3): only ParseStatusParsed rows from OTHER stations pass. The
// unsupported-payload rows here are hand-built because the encoder refuses to
// synthesize them as audio — this unit seam plus the loop-level own-TX proof
// (TestDecodeLoop_CuratedSurfaces_Frozen) compose into the full AC1 claim.
func TestCurateDecodes(t *testing.T) {
	msgs := []goft8.DecodedMessage{
		{Text: "CQ A61DI LL64", ParseStatus: goft8.ParseStatusParsed},
		{Text: "CQ K1ABC FN42", ParseStatus: goft8.ParseStatusParsed},         // our own loopback
		{ParseStatus: goft8.ParseStatusUnsupported},                           // CRC-valid, text-less
		{ParseStatus: goft8.ParseStatusInvalid},                               // CRC-valid, bad field
		{Text: "7Q5MLV G0XYZ -10", ParseStatus: goft8.ParseStatusUnknownType}, // never trust text on a non-parsed status
	}

	got := curateDecodes(msgs, "K1ABC")
	if len(got) != 1 || got[0].Text != "CQ A61DI LL64" {
		t.Fatalf("curateDecodes kept %v, want exactly [CQ A61DI LL64]", textsOf(got))
	}

	// No own call → only the parse-status filter applies.
	got = curateDecodes(msgs, "")
	if len(got) != 2 {
		t.Fatalf("curateDecodes with no own call kept %v, want the two parsed rows", textsOf(got))
	}

	if got := curateDecodes(nil, "K1ABC"); len(got) != 0 {
		t.Fatalf("nil in, non-empty out: %v", textsOf(got))
	}
}

// TestCurateDecodes_NeverMutatesInput pins the seam's non-aliasing contract:
// the input is the rich slice the evidence branch taps, so the curated
// filters must copy, never filter in place — an in-place dropUnparsed
// (kept := msgs[:0]) compacts survivors over the rich slice's early elements
// and silently corrupts the evidence stream the moment its writer lands.
func TestCurateDecodes_NeverMutatesInput(t *testing.T) {
	in := []goft8.DecodedMessage{
		{ParseStatus: goft8.ParseStatusUnsupported, SNR: -12},
		{Text: "CQ A61DI LL64", ParseStatus: goft8.ParseStatusParsed},
		{ParseStatus: goft8.ParseStatusInvalid, SNR: -20},
		{Text: "CQ K1ABC FN42", ParseStatus: goft8.ParseStatusParsed},
	}
	want := append([]goft8.DecodedMessage(nil), in...)

	curateDecodes(in, "K1ABC")

	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("curateDecodes mutated its input at [%d]: got %+v, want %+v", i, in[i], want[i])
		}
	}
}

// TestDecodeLoop_SkipAdvancesDecoderOncePerSkippedSlot pins the loop half of
// AC6 at its only observable: the skip advance's debug trace, once per
// skipped TX slot and never for a decoded slot — and, per the P1 amendment,
// a dial-moved slot RESETS instead (its own trace, no advance). The parity
// consequence itself is A7-internal (header note); this guards the CALL
// discipline the zero-slot decision depends on — without it, deleting
// dec.skip() would fail no test at all.
func TestDecodeLoop_SkipAdvancesDecoderOncePerSkippedSlot(t *testing.T) {
	buf := &bytes.Buffer{}
	s := newService(types.Ft8Config{Enabled: true}, logging.NewForWriter(buf), newFakeSource())
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	start := recentEvenSlotStart()
	s.markTxSlot(start.Add(15 * time.Second))

	// Short sample slices keep the decoded slots cheap (rejected before decode
	// work); skip runs regardless of the slot's samples — it feeds zeros.
	ch := make(chan Slot, 3)
	ch <- Slot{StartUTC: start, Samples: make([]int16, 1000)}
	ch <- Slot{StartUTC: start.Add(15 * time.Second), Samples: make([]int16, 1000)}
	ch <- Slot{StartUTC: start.Add(30 * time.Second), Samples: make([]int16, 1000), DialChanged: true, DialTracked: true}
	close(ch)
	s.decodeLoop(ch)

	if got := strings.Count(buf.String(), "decoder state advanced"); got != 1 {
		t.Fatalf("skip advances = %d, want exactly 1 (the TX slot only — a moved slot resets):\n%s",
			got, buf.String())
	}
	if got := strings.Count(buf.String(), "decoder state reset"); got != 1 {
		t.Fatalf("decoder resets = %d, want exactly 1 (the dial-moved slot):\n%s",
			got, buf.String())
	}
}

// TestDecodeLoop_DialMoveResetsDecoderState is the P1 fix's hash observable
// (review cd1757a7cda2): a hash learned BEFORE a dial-moved slot must NOT
// resolve after it — the QSY replaced the receiver context, and a band-blind
// hash table would let a collision render a wrong call as resolved on the
// new band. The moved slot carries decodable audio on purpose: under the
// pre-fix skip() behaviour the hash survives and the reference resolves, so
// the fixture discriminates reset from skip.
func TestDecodeLoop_DialMoveResetsDecoderState(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, _, events, _ := newSplitHarness(t)
	start := recentEvenSlotStart()

	ch := make(chan Slot, 3)
	ch <- Slot{StartUTC: start, Samples: encodeSlotOrFatal(t, hashTeachText, 1500), DialTracked: true, DialMHz: 14.074}
	ch <- Slot{StartUTC: start.Add(15 * time.Second), Samples: encodeSlotOrFatal(t, "CQ W1AW FN31", 1500), DialTracked: true, DialMHz: 0, DialChanged: true}
	ch <- Slot{StartUTC: start.Add(30 * time.Second), Samples: encodeSlotOrFatal(t, hashRefText, 1500), DialTracked: true, DialMHz: 21.074}
	close(ch)
	s.decodeLoop(ch)

	decodes, _ := drainDecodeEvents(events)
	if len(decodes) != 3 {
		t.Fatalf("ft8-decode events = %d, want 3", len(decodes))
	}
	rows := decodes[2].Decodes
	if len(rows) != 1 {
		t.Fatalf("post-QSY rows = %d, want 1: %+v", len(rows), rows)
	}
	if got := rows[0].Text; got != hashUnresolved {
		t.Fatalf("post-QSY decode rendered %q, want %q (a dial move must reset hash state)",
			got, hashUnresolved)
	}
}

// TestDecodeLoop_GapSlotsAdvanceDecoder is AC8's call-discipline guard
// (review cd1757a7cda2 P2), at the same trace observable as the TX-skip
// test: two consecutive deliveries 45 s apart mean two physical slots were
// omitted (scheduler skip or drop), so the decoder must advance exactly
// twice between them. Without gap detection an odd-length gap swaps the A7
// parity buckets for the rest of the session.
func TestDecodeLoop_GapSlotsAdvanceDecoder(t *testing.T) {
	buf := &bytes.Buffer{}
	s := newService(types.Ft8Config{Enabled: true}, logging.NewForWriter(buf), newFakeSource())
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	start := recentEvenSlotStart()
	ch := make(chan Slot, 2)
	ch <- Slot{StartUTC: start, Samples: make([]int16, 1000)}
	ch <- Slot{StartUTC: start.Add(45 * time.Second), Samples: make([]int16, 1000)}
	close(ch)
	s.decodeLoop(ch)

	if got := strings.Count(buf.String(), "decoder state advanced"); got != 2 {
		t.Fatalf("gap advances = %d, want exactly 2 (two omitted physical slots):\n%s",
			got, buf.String())
	}
}

// TestDecodeLoop_DroppedQsySlotStillResets is review 75f40264fe2b P1: the
// DialChanged flag rides the one slot that spans the move, and the scheduler
// can DROP that slot (emitSlot's best-effort send) — the next delivered slot
// is then cleanly attributed to the NEW band with no flag at all. The gap
// advance must not carry the band-blind hash table across such an
// undelivered QSY: a dial DIFFERENCE between consecutive delivered slots
// resets the decoder exactly as a delivered moved slot would. The fixture is
// the reviewer's scenario verbatim: teach on 14.074, omit the QSY slot
// entirely, reference on 21.074 — the hash must NOT resolve.
func TestDecodeLoop_DroppedQsySlotStillResets(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, _, events, _ := newSplitHarness(t)
	start := recentEvenSlotStart()

	ch := make(chan Slot, 2)
	ch <- Slot{StartUTC: start, Samples: encodeSlotOrFatal(t, hashTeachText, 1500), DialTracked: true, DialMHz: 14.074}
	ch <- Slot{StartUTC: start.Add(30 * time.Second), Samples: encodeSlotOrFatal(t, hashRefText, 1500), DialTracked: true, DialMHz: 21.074}
	close(ch)
	s.decodeLoop(ch)

	decodes, _ := drainDecodeEvents(events)
	if len(decodes) != 2 {
		t.Fatalf("ft8-decode events = %d, want 2", len(decodes))
	}
	rows := decodes[1].Decodes
	if len(rows) != 1 {
		t.Fatalf("new-band rows = %d, want 1: %+v", len(rows), rows)
	}
	if got := rows[0].Text; got != hashUnresolved {
		t.Fatalf("post-dropped-QSY decode rendered %q, want %q (a dial difference between delivered slots must reset)",
			got, hashUnresolved)
	}
}

// TestDecodeLoop_GapPreservesHashState pins AC8's deliberate choice of
// ADVANCE over RESET for a gap: the receiver context is unchanged, so a hash
// learned before an omitted slot still resolves after it. This refuses the
// adjacent wrong implementation — reset-per-gap would pass the trace test
// above while throwing away context a lossy channel never invalidated. Like
// AC5's test it is green before and after the fix; its red proof is a
// reversion probe against the reset-per-gap variant.
func TestDecodeLoop_GapPreservesHashState(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, _, events, _ := newSplitHarness(t)
	start := recentEvenSlotStart()

	ch := make(chan Slot, 2)
	ch <- Slot{StartUTC: start, Samples: encodeSlotOrFatal(t, hashTeachText, 1500), DialTracked: true, DialMHz: 14.074}
	ch <- Slot{StartUTC: start.Add(45 * time.Second), Samples: encodeSlotOrFatal(t, hashRefText, 1500), DialTracked: true, DialMHz: 14.074}
	close(ch)
	s.decodeLoop(ch)

	decodes, _ := drainDecodeEvents(events)
	if len(decodes) != 2 {
		t.Fatalf("ft8-decode events = %d, want 2", len(decodes))
	}
	rows := decodes[1].Decodes
	if len(rows) != 1 {
		t.Fatalf("post-gap rows = %d, want 1: %+v", len(rows), rows)
	}
	if got := rows[0].Text; got != hashResolved {
		t.Fatalf("post-gap decode rendered %q, want %q (a gap must advance, not reset)",
			got, hashResolved)
	}
}

// TestSlotDecoder_HashResolvesAcrossSlots is AC4 at the wrapper: one
// slotDecoder carries hash state between slots, so the hash reference resolves
// — where a FRESH decoder (the control, asserted first) renders "<...>". The
// control is what makes a red informative: if IT fails, the fixture is wrong,
// not the statefulness.
func TestSlotDecoder_HashResolvesAcrossSlots(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	teach := encodeSlotOrFatal(t, hashTeachText, 1500)
	ref := encodeSlotOrFatal(t, hashRefText, 1500)

	// Control: statelessly, the hash cannot resolve.
	control := newSlotDecoder(true, logging.Noop())
	if got, _ := control.decode(ref); !containsText(textsOf(got), hashUnresolved) {
		t.Fatalf("fixture control: fresh decode = %v, want %q (fixture broken if absent)",
			textsOf(got), hashUnresolved)
	}

	d := newSlotDecoder(true, logging.Noop())
	if got, _ := d.decode(teach); !containsText(textsOf(got), hashTeachText) {
		t.Fatalf("teach slot decoded %v, want %q", textsOf(got), hashTeachText)
	}
	got, _ := d.decode(ref)
	if !containsText(textsOf(got), hashResolved) {
		t.Fatalf("stateful decode = %v, want %q (hash state must carry across slots)",
			textsOf(got), hashResolved)
	}
	// AC2's field claim at the seam: the rich result reaches the caller with
	// decode provenance intact, not projected away.
	for _, m := range got {
		if m.Text == hashResolved && m.Provenance.Algorithm == "" {
			t.Errorf("rich result lost its provenance: %+v", m)
		}
	}
}

// TestSlotDecoder_SkipPreservesHashState is AC6 at the wrapper: skip()
// advances the decoder across a slot we refuse to decode (own TX, moved dial)
// WITHOUT losing hash state — the reference still resolves on the far side.
// Parity-bucket clearing is go-ft8's zero-slot mechanics (a7.go rewrites
// history[seq] every call) and is not deterministically observable here — see
// the header's testability note.
func TestSlotDecoder_SkipPreservesHashState(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	d := newSlotDecoder(true, logging.Noop())

	// skip on a fresh decoder must be harmless.
	d.skip()

	if got, _ := d.decode(encodeSlotOrFatal(t, hashTeachText, 1500)); !containsText(textsOf(got), hashTeachText) {
		t.Fatalf("teach slot decoded %v, want %q", textsOf(got), hashTeachText)
	}
	d.skip() // the TX slot between them
	msgs, _ := d.decode(encodeSlotOrFatal(t, hashRefText, 1500))
	got := textsOf(msgs)
	if !containsText(got, hashResolved) {
		t.Fatalf("decode after skip = %v, want %q (skip must advance state, not destroy it)",
			got, hashResolved)
	}
}

// TestDecodeLoop_HashStateSpansTxSlots is AC4+AC6 at the operator surface: a
// station heard before our TX slot resolves by hash in the slot after it —
// the Band Activity row carries the resolved call. This is what the stateless
// per-slot API can never do, and the reason the run path (TX every other
// slot) is exactly where statefulness pays.
func TestDecodeLoop_HashStateSpansTxSlots(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, _, events, _ := newSplitHarness(t)

	start := recentEvenSlotStart()
	txStart := start.Add(15 * time.Second)
	s.markTxSlot(txStart)

	ch := make(chan Slot, 3)
	ch <- Slot{StartUTC: start, Samples: encodeSlotOrFatal(t, hashTeachText, 1500), DialTracked: true, DialMHz: 14.074}
	ch <- Slot{StartUTC: txStart, Samples: encodeSlotOrFatal(t, "CQ W1AW FN31", 1500), DialTracked: true, DialMHz: 14.074}
	ch <- Slot{StartUTC: start.Add(30 * time.Second), Samples: encodeSlotOrFatal(t, hashRefText, 1500), DialTracked: true, DialMHz: 14.074}
	close(ch)
	s.decodeLoop(ch)

	decodes, _ := drainDecodeEvents(events)
	if len(decodes) != 3 {
		t.Fatalf("ft8-decode events = %d, want 3", len(decodes))
	}
	last := decodes[2]
	if len(last.Decodes) != 1 {
		t.Fatalf("final slot rows = %d, want 1: %+v", len(last.Decodes), last.Decodes)
	}
	if got := last.Decodes[0].Text; got != hashResolved {
		t.Fatalf("Band Activity rendered %q, want %q (decoder state must span the TX slot)",
			got, hashResolved)
	}
}

// evidenceRecorder captures every EvidenceSlot the loop emits.
type evidenceRecorder struct {
	mu    sync.Mutex
	slots []EvidenceSlot
}

func (r *evidenceRecorder) record(es EvidenceSlot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.slots = append(r.slots, es)
}

func (r *evidenceRecorder) all() []EvidenceSlot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]EvidenceSlot(nil), r.slots...)
}

// TestDecodeLoop_EvidenceSinkReceivesRichUnfiltered is design §4 prereq 2's
// point, now executable end to end: the evidence sink receives the COMPLETE
// decode set — our own loopback included — for the same slot whose curated
// surfaces show only the other station. The same fixture discriminates both
// branches at once.
func TestDecodeLoop_EvidenceSinkReceivesRichUnfiltered(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, _, events, _ := newSplitHarness(t)
	rec := &evidenceRecorder{}
	s.SetEvidenceSink(rec.record)

	if err := s.ArmTx(true); err != nil {
		t.Fatalf("ArmTx: %v", err)
	}
	if err := s.StartCallCq("K1ABC", "FN42", 1500, 14.074, "", "odd", 1); err != nil {
		t.Fatalf("StartCallCq: %v", err)
	}

	samples := mixSlot(t, map[string]float64{
		"CQ K1ABC FN42": 800,
		"CQ A61DI LL64": 2200,
	})
	ch := make(chan Slot, 1)
	ch <- Slot{StartUTC: recentEvenSlotStart(), Samples: samples, DialTracked: true, DialMHz: 14.074}
	close(ch)
	s.decodeLoop(ch)

	decodes, _ := drainDecodeEvents(events)
	if len(decodes) != 1 || len(decodes[0].Decodes) != 1 {
		t.Fatalf("curated rows = %+v, want only the other station", decodes)
	}

	evs := rec.all()
	if len(evs) != 1 {
		t.Fatalf("evidence slots = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Outcome != EvidenceDecoded || ev.DialMHz != 14.074 || !ev.DialTracked {
		t.Fatalf("evidence slot = %+v, want decoded on 14.074 tracked", ev)
	}
	texts := textsOf(ev.Decodes)
	if !containsText(texts, "CQ K1ABC FN42") || !containsText(texts, "CQ A61DI LL64") {
		t.Fatalf("evidence decodes = %v, want BOTH stations (rich, own-TX included)", texts)
	}
}

// TestDecodeLoop_EvidenceOutcomesPerPhysicalSlot pins the coverage vocabulary
// at the seam: one EvidenceSlot per PHYSICAL slot with its true outcome —
// tx and dial-moved slots included, scheduler-omitted slots surfacing as
// capture_dropped, a rejected decode as decoder_error (distinct from a
// silent band), and a decodable slot as decoded. Without the omitted-slot
// rows an empty archive stretch is ambiguous, which is the §4.1 claim.
func TestDecodeLoop_EvidenceOutcomesPerPhysicalSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, _, _, _ := newSplitHarness(t)
	rec := &evidenceRecorder{}
	s.SetEvidenceSink(rec.record)

	start := recentEvenSlotStart()
	s.markTxSlot(start.Add(15 * time.Second))

	ch := make(chan Slot, 5)
	// decoded
	ch <- Slot{StartUTC: start, Samples: encodeSlotOrFatal(t, "CQ A61DI LL64", 1500), DialTracked: true, DialMHz: 14.074}
	// tx (decodable audio deliberately ignored)
	ch <- Slot{StartUTC: start.Add(15 * time.Second), Samples: encodeSlotOrFatal(t, "CQ W1AW FN31", 1500), DialTracked: true, DialMHz: 14.074}
	// dial_changed
	ch <- Slot{StartUTC: start.Add(30 * time.Second), Samples: encodeSlotOrFatal(t, "CQ W1AW FN31", 1500), DialTracked: true, DialMHz: 0, DialChanged: true}
	// gap: slots at +45 s and +60 s never delivered → 2× capture_dropped
	// decoder_error: a malformed short slot is REJECTED by the checked API
	ch <- Slot{StartUTC: start.Add(75 * time.Second), Samples: make([]int16, 1000), DialTracked: true, DialMHz: 14.074}
	// no_decode: a full-length silent slot decodes cleanly to nothing
	ch <- Slot{StartUTC: start.Add(90 * time.Second), Samples: make([]int16, SlotSamples), DialTracked: true, DialMHz: 14.074}
	close(ch)
	s.decodeLoop(ch)

	want := []string{
		EvidenceDecoded,
		EvidenceTx,
		EvidenceDialChanged,
		EvidenceCaptureDropped,
		EvidenceCaptureDropped,
		EvidenceDecoderError,
		EvidenceNoDecode,
	}
	evs := rec.all()
	if len(evs) != len(want) {
		got := make([]string, len(evs))
		for i, e := range evs {
			got[i] = e.Outcome
		}
		t.Fatalf("evidence slots = %v, want %v (one per PHYSICAL slot)", got, want)
	}
	for i, w := range want {
		if evs[i].Outcome != w {
			t.Errorf("slot %d outcome = %s, want %s", i, evs[i].Outcome, w)
		}
	}
	if n := len(evs[0].Decodes); n != 1 {
		t.Errorf("decoded slot carries %d decodes, want 1", n)
	}
	for i, e := range evs[1:] {
		if len(e.Decodes) != 0 {
			t.Errorf("non-decoded slot %d carries decodes: %+v", i+1, e.Decodes)
		}
	}
	// The omitted slots carry their own boundary times.
	if evs[3].Slot.StartUTC != start.Add(45*time.Second).UTC().Format(time.RFC3339) ||
		evs[4].Slot.StartUTC != start.Add(60*time.Second).UTC().Format(time.RFC3339) {
		t.Errorf("capture_dropped slots = %s / %s, want the two omitted boundaries",
			evs[3].Slot.StartUTC, evs[4].Slot.StartUTC)
	}
	// An omitted slot was never OBSERVED, so its dial context claims nothing:
	// inheriting the next slot's DialTracked would assert a tracking state for
	// an interval nobody measured (review 68514620 P2).
	if evs[3].DialTracked || evs[4].DialTracked || evs[3].DialMHz != 0 || evs[4].DialMHz != 0 {
		t.Errorf("capture_dropped rows must claim no dial context, got %+v / %+v", evs[3], evs[4])
	}
}

// TestScheduler_TracksUndeliveredRun is the scheduler half of review 68514620
// P1: a slot the scheduler fails to deliver (channel full) joins a
// consecutive undelivered run; a successful emit stamps that run onto the
// delivered slot as OmittedBefore and resets it, so the tail left when a
// session ends is exactly the run no later slot could ever report.
func TestScheduler_TracksUndeliveredRun(t *testing.T) {
	sch := NewScheduler(nil, logging.Noop())
	ring := newSampleRing(SlotSamples)
	ring.Append(make([]int16, SlotSamples)) // full ring: emitSlot won't cold-start skip

	// Occupy the (capacity 1) channel so emits drop.
	sch.out <- Slot{}

	t1 := time.Date(2026, 8, 10, 12, 0, 15, 0, time.UTC) // boundary; slot start = -15 s
	sch.emitSlot(ring, t1, t1)
	sch.emitSlot(ring, t1.Add(15*time.Second), t1.Add(15*time.Second))

	start, n := sch.UndeliveredTail()
	if n != 2 || !start.Equal(t1.Add(-15*time.Second)) {
		t.Fatalf("tail = (%v, %d), want (%v, 2)", start, n, t1.Add(-15*time.Second))
	}

	<-sch.out // free the channel
	sch.emitSlot(ring, t1.Add(30*time.Second), t1.Add(30*time.Second))
	delivered := <-sch.out
	if delivered.OmittedBefore != 2 {
		t.Fatalf("delivered OmittedBefore = %d, want 2 (the run before it)", delivered.OmittedBefore)
	}
	if _, n := sch.UndeliveredTail(); n != 0 {
		t.Fatalf("tail after delivery = %d, want 0 (run reset)", n)
	}
}

// TestDecodeLoop_FirstSlotOmittedBefore covers the before-first-delivery hole
// (review 68514620 P1): slots dropped before the session's FIRST delivery
// have no predecessor for the gap inference, so the loop reads the delivered
// slot's OmittedBefore stamp instead — coverage rows only, no decoder
// advance (the decoder is fresh at first delivery; there is no parity state
// to keep aligned). Mid-session the stamp is deliberately ignored: the gap
// inference is the one pinned mechanism there, and both count the same run.
func TestDecodeLoop_FirstSlotOmittedBefore(t *testing.T) {
	s, _, _, _ := newSplitHarness(t)
	rec := &evidenceRecorder{}
	s.SetEvidenceSink(rec.record)

	start := recentEvenSlotStart()
	ch := make(chan Slot, 1)
	ch <- Slot{StartUTC: start, Samples: make([]int16, 1000), OmittedBefore: 2}
	close(ch)
	s.decodeLoop(ch)

	evs := rec.all()
	if len(evs) != 3 {
		got := make([]string, len(evs))
		for i, e := range evs {
			got[i] = e.Outcome + "@" + e.Slot.StartUTC
		}
		t.Fatalf("evidence slots = %v, want 2× capture_dropped then the delivered slot", got)
	}
	for i := 0; i < 2; i++ {
		wantStart := start.Add(time.Duration(i-2) * 15 * time.Second).UTC().Format(time.RFC3339)
		if evs[i].Outcome != EvidenceCaptureDropped || evs[i].Slot.StartUTC != wantStart {
			t.Errorf("evidence[%d] = %s@%s, want capture_dropped@%s", i, evs[i].Outcome, evs[i].Slot.StartUTC, wantStart)
		}
	}
}

// TestReleaseCapture_EmitsUndeliveredTail is the teardown half of review
// 68514620 P1: the trailing undelivered run — drops or skips with NO later
// delivery — is invisible to the loop forever, so the release path (after
// the drain proves both session goroutines exited) reads the scheduler's
// tail and emits its capture_dropped coverage. Without this, an incomplete
// capture is indistinguishable from one that stopped cleanly.
func TestReleaseCapture_EmitsUndeliveredTail(t *testing.T) {
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	rec := &evidenceRecorder{}
	s.SetEvidenceSink(rec.record)

	tail := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sch := NewScheduler(nil, logging.Noop())
	sch.undeliveredStart = tail
	sch.undeliveredN = 2

	s.mu.Lock()
	s.capturing = true
	s.curSched = sch
	s.releaseCaptureLocked()
	s.mu.Unlock()

	evs := rec.all()
	if len(evs) != 2 {
		t.Fatalf("evidence slots = %d, want the 2 trailing undelivered", len(evs))
	}
	for i, e := range evs {
		wantStart := tail.Add(time.Duration(i) * 15 * time.Second).UTC().Format(time.RFC3339)
		if e.Outcome != EvidenceCaptureDropped || e.Slot.StartUTC != wantStart || e.DialTracked {
			t.Errorf("evidence[%d] = %+v, want capture_dropped@%s untracked", i, e, wantStart)
		}
	}
}

// TestDecodeLoop_FreshDecoderPerCaptureSession is AC5. Each decodeLoop call is
// one capture session (service.go spawns one per acquire); a hash learned in
// session 1 must NOT resolve in session 2 — cross-session state would carry
// stale hint/hash context across an operator-length gap or a band change.
//
// NOTE this test passes against BOTH the current stateless code and the
// correct per-session implementation; it exists to refuse the ADJACENT WRONG
// implementation (a Service-lifetime decoder). Its red proof is therefore a
// reversion probe against that variant — decoder hoisted to a Service field —
// not a pre-implementation red.
func TestDecodeLoop_FreshDecoderPerCaptureSession(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	s, _, events, _ := newSplitHarness(t)
	start := recentEvenSlotStart()

	ch1 := make(chan Slot, 1)
	ch1 <- Slot{StartUTC: start, Samples: encodeSlotOrFatal(t, hashTeachText, 1500), DialTracked: true, DialMHz: 14.074}
	close(ch1)
	s.decodeLoop(ch1)

	ch2 := make(chan Slot, 1)
	ch2 <- Slot{StartUTC: start.Add(30 * time.Second), Samples: encodeSlotOrFatal(t, hashRefText, 1500), DialTracked: true, DialMHz: 14.074}
	close(ch2)
	s.decodeLoop(ch2)

	decodes, _ := drainDecodeEvents(events)
	if len(decodes) != 2 {
		t.Fatalf("ft8-decode events = %d, want 2", len(decodes))
	}
	rows := decodes[1].Decodes
	if len(rows) != 1 {
		t.Fatalf("session-2 rows = %d, want 1: %+v", len(rows), rows)
	}
	if got := rows[0].Text; got != hashUnresolved {
		t.Fatalf("session 2 rendered %q, want %q (a fresh session must not inherit hash state)",
			got, hashUnresolved)
	}
}
