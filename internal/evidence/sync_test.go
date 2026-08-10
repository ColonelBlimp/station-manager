package evidence

/*
   §5 sync-slice acceptance criteria SY1–SY9 (spot-network §5.1/§5.2/§5.4 +
   the 2026-08-10 amendments and ratified mechanism contracts; the SMC
   ingest half E1–E8 lives in internal/cloud/store/evidence_test.go and the
   HTTP half H1–H4 in internal/cloud/server/evidence_test.go). Each
   criterion names its nearest confusable state. The fake SMC here is an
   httptest server speaking the real evidencewire contract — the HTTP
   boundary IS the client's boundary; archives are real SQLite files.

   SY1  Consent is a wall: capture on + sync off ⇒ no EVIDENCE request or
        data ever leaves for SMC and every row stays unsynced —
        distinguishable from "sync on, SMC unreachable" (attempted, in
        backoff, visible in status). QSO traffic on its own independent
        path is unaffected (structural: SY7). The validation half (sync
        requires a configured smcloud forwarder) is pinned in
        internal/config.
   SY2  One contract, four kinds, quarantine: a permanent_reject row is
        quarantined locally (reason recorded, visible in status, NEVER
        re-offered, never deleted) without blocking batch-mates —
        distinguishable from a transient failure, which retries and never
        quarantines.
   SY3  already_present is terminal-benign: the row marks synced exactly
        like accepted (the restored-backup duplicate offer).
   SY4  Profile ordering self-heals BY SELECTION, not envelope order
        (operator ruling): retryable_missing_profile re-offers the
        referenced LOCAL profile even if it was previously marked synced —
        it heals an SMC-side restore — and the observation lands on a
        later round. A NULL-profile observation is accepted as explicitly
        unprofiled, never retried; "no sync-side retry state is ever
        created for a null-profile observation" (§5.4 amendment, repeated
        here verbatim as required; legacy_unprofiled is indistinguishable
        from any other NULL for sync).
   SY5  Prompt while live, lazy for backlog — the HEALTHY-CHANNEL
        guarantee: with SMC reachable, a fresh slot's rows reach SMC
        within one debounce window (scaled in tests; one 15 s slot cycle
        in production). A live signal CANCELS an in-flight backlog
        request, and that intentional cancellation does not advance
        backoff — otherwise "a fresh decode never queues behind backlog"
        would be false under drain.
   SY6  The archive explains its gaps remotely too: loss intervals and
        coverage sync as first-class rows (their acceptance is asserted
        through the same envelope; the SMC-side query surface is the
        store's E1).
   SY7  The QSO path is untouchable, structurally: internal/evidence
        imports neither internal/forwarding nor internal/qsoservice —
        no shared queue, worker, or credentials type. (The behavioural
        half — evidence endpoint down while QSO sync stays green — is a
        daemon-level property of that separation.)
   SY8  At-least-once, marked-once: rows mark synced only on a terminal
        outcome. An invalid or incomplete response (wrong outcome count,
        mismatched uuids, non-2xx) consumes NO rows — they re-offer next
        round — distinguishable from already_present, which is terminal.
   SY9  offered_at is conservative durable send-intent (COALESCE before
        dispatch): set before the first dispatch attempt, PRESERVED at its
        first value across re-offers, NULL only on never-offered rows.
        "Possibly offered, unacknowledged" — a crash can occur before
        bytes leave. The retention slice's never_offered vs
        offered_unacknowledged distinction consumes exactly this column.

   SY10 (codex-P1 fix, 2026-08-10) Profile-first is a PRIORITY, not a cap
        exemption: no envelope ever exceeds the batch cap even when
        unsynced profiles alone exceed it — leftover profiles ride later
        rounds. The confusable design (profiles outside the cap) wedges
        permanently once accumulated profiles pass the server's row limit:
        every request 400s, consumes nothing, and retries the same
        oversized set forever, taking live observations down with it.

   Constants under test are the ratified ones (1 s / 500 rows / 10 s /
   30 s→15 min / 30 s HTTP timeout), dialed down per test via the package
   vars (captureLinger pattern).
*/

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// fakeSMC is a scripted SMC speaking the real wire contract.
type fakeSMC struct {
	mu       sync.Mutex
	batches  [][]evidencewire.Record
	times    []time.Time
	canceled int // stalled requests that ended by client cancellation
	// script decides each record's outcome; nil = accept everything.
	script func(rec evidencewire.Record) evidencewire.RowOutcome
	// respond overrides the whole response; nil = per-record script.
	respond func(recs []evidencewire.Record) (int, any)
	stall   bool // stall the NEXT request until its context dies
	ts      *httptest.Server
}

func newFakeSMC(t *testing.T) *fakeSMC {
	t.Helper()
	f := &fakeSMC{}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req evidencewire.PutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.batches = append(f.batches, req.Records)
		f.times = append(f.times, time.Now())
		stall := f.stall
		f.stall = false
		respond := f.respond
		script := f.script
		f.mu.Unlock()

		if stall {
			<-r.Context().Done()
			f.mu.Lock()
			f.canceled++
			f.mu.Unlock()
			return
		}
		if respond != nil {
			status, body := respond(req.Records)
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		resp := evidencewire.PutResponse{}
		for _, rec := range req.Records {
			o := evidencewire.RowOutcome{UUID: rec.UUID, Outcome: evidencewire.OutcomeAccepted}
			if script != nil {
				o = script(rec)
				o.UUID = rec.UUID
			}
			resp.Outcomes = append(resp.Outcomes, o)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeSMC) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

// sawUUID reports whether any recorded batch carried the uuid, and how often.
func (f *fakeSMC) sawUUID(uuid string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.batches {
		for _, r := range b {
			if r.UUID == uuid {
				n++
			}
		}
	}
	return n
}

// dialSync shrinks the ratified constants for test cadence and restores them.
func dialSync(t *testing.T, debounce, backlog, backoffMin, backoffMax time.Duration) {
	t.Helper()
	oldD, oldB, oldMin, oldMax := syncLiveDebounce, syncBacklogInterval, syncBackoffMin, syncBackoffMax
	syncLiveDebounce, syncBacklogInterval, syncBackoffMin, syncBackoffMax = debounce, backlog, backoffMin, backoffMax
	t.Cleanup(func() {
		syncLiveDebounce, syncBacklogInterval, syncBackoffMin, syncBackoffMax = oldD, oldB, oldMin, oldMax
	})
}

func syncedConfig(t *testing.T, url string) Config {
	t.Helper()
	cfg := testConfig(t, true)
	cfg.Sync = true
	cfg.SyncURL = url
	cfg.SyncToken = "test-token"
	return cfg
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func syncMark(t *testing.T, path, table, uuid string) (synced int, offeredAt, quarantine *string) {
	t.Helper()
	db := openRaw(t, path)
	var off, q *string
	if err := db.QueryRow(
		`SELECT synced, offered_at, quarantine_reason FROM `+table+` WHERE uuid = ?`, uuid).
		Scan(&synced, &off, &q); err != nil {
		t.Fatalf("read sync mark %s/%s: %v", table, uuid, err)
	}
	return synced, off, q
}

func obsUUID(t *testing.T, path string, slot time.Time) string {
	t.Helper()
	db := openRaw(t, path)
	var uuid string
	if err := db.QueryRow(`SELECT uuid FROM observations WHERE slot_start_utc = ?`,
		slot.UTC().Format(time.RFC3339)).Scan(&uuid); err != nil {
		t.Fatalf("observation uuid for %s: %v", slot, err)
	}
	return uuid
}

// SY1 — capture on, sync off: nothing leaves, nothing marks.
func TestSY1_ConsentWall(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	smc := newFakeSMC(t)
	// Consent off with the destination KNOWN: the wall must be the Sync
	// boolean itself, not an unconfigured URL — a gateless build with
	// credentials in hand is exactly the guarded-against implementation.
	cfg := testConfig(t, true)
	cfg.SyncURL = smc.ts.URL
	cfg.SyncToken = "test-token"
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)
	time.Sleep(100 * time.Millisecond) // several would-be sync periods

	if n := smc.requestCount(); n != 0 {
		t.Fatalf("SY1: %d evidence requests left the machine with sync disabled, want 0", n)
	}
	st := s.Status()
	if st.Sync == nil || st.Sync.Enabled {
		t.Fatalf("SY1: Status.Sync = %+v, want present with enabled=false — distinguishable from unreachable-and-retrying", st.Sync)
	}
	s.Stop()
	if n := countRows(t, openRaw(t, cfg.Path), `SELECT COUNT(*) FROM observations WHERE synced != 0`); n != 0 {
		t.Fatalf("SY1: %d rows marked synced with sync disabled", n)
	}
}

// SY2 — quarantine on permanent_reject: visible, never re-offered, batch-mates land.
func TestSY2_QuarantineDoesNotBlockBatchmates(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	smc := newFakeSMC(t)
	var poisoned string
	var pmu sync.Mutex
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		pmu.Lock()
		defer pmu.Unlock()
		if rec.Kind == evidencewire.KindObservation && poisoned == "" {
			poisoned = rec.UUID
		}
		if rec.UUID == poisoned {
			return evidencewire.RowOutcome{Outcome: evidencewire.OutcomePermanentReject, Reason: "digest_conflict"}
		}
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}
	}

	cfg := syncedConfig(t, smc.ts.URL)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)

	waitFor(t, "quarantine + accepted batch-mate", func() bool {
		pmu.Lock()
		p := poisoned
		pmu.Unlock()
		if p == "" {
			return false
		}
		synced, _, q := syncMark(t, cfg.Path, "observations", p)
		return synced == 0 && q != nil && *q == "digest_conflict"
	})
	// The coverage batch-mate must have landed regardless.
	waitFor(t, "coverage batch-mate synced", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE synced = 1`) == 1
	})

	// More sync rounds happen; the quarantined row must never be offered again.
	pmu.Lock()
	p := poisoned
	pmu.Unlock()
	before := smc.sawUUID(p)
	s.CaptureSlot(obsSlot(slotAt(15), 14.074, true))
	drain(t, s)
	waitFor(t, "second slot synced", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE synced = 1`) == 2
	})
	if after := smc.sawUUID(p); after != before {
		t.Fatalf("SY2: quarantined row re-offered (%d → %d offers) — quarantine must be terminal locally", before, after)
	}
	st := s.Status()
	if st.Sync == nil || st.Sync.Quarantined != 1 {
		t.Fatalf("SY2: Status.Sync.Quarantined = %+v, want 1 — the operator must see the quarantine", st.Sync)
	}
}

// SY3 — already_present marks synced exactly like accepted.
func TestSY3_AlreadyPresentMarksSynced(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	smc := newFakeSMC(t)
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAlreadyPresent}
	}
	cfg := syncedConfig(t, smc.ts.URL)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)
	waitFor(t, "already_present marks synced", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations WHERE synced = 1`) == 1
	})
}

// SY4 — retryable_missing_profile re-offers the LOCAL profile even though it
// was already marked synced (selection priority; heals an SMC-side restore),
// and the observation lands on a later round.
func TestSY4_MissingProfileReoffersSyncedProfile(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	smc := newFakeSMC(t)
	var mu sync.Mutex
	phase := 1 // 1: healthy · 2: SMC "restored from backup, lost the profile" · 3: healed
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case rec.Kind == evidencewire.KindProfile:
			if phase == 2 {
				phase = 3 // the re-offered profile restores SMC's copy
			}
			return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}
		case rec.Kind == evidencewire.KindObservation && phase == 2:
			return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeRetryableMissingProfile}
		}
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}
	}

	cfg := syncedConfig(t, smc.ts.URL)
	cfg.Antennas = []types.AntennaDecl{dxCommander()}
	s := newRunning(t, cfg)

	// Phase 1: the minted profile syncs and is marked SYNCED locally.
	waitFor(t, "profile synced in phase 1", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM profiles WHERE synced = 1`) == 1
	})
	db := openRaw(t, cfg.Path)
	var pUUID string
	if err := db.QueryRow(`SELECT uuid FROM profiles`).Scan(&pUUID); err != nil {
		t.Fatal(err)
	}
	offersBefore := smc.sawUUID(pUUID)

	// Phase 2: SMC has "lost" the profile; a new observation references it.
	mu.Lock()
	phase = 2
	mu.Unlock()
	s.CaptureSlot(obsSlot(slotAt(0), 3.573, true)) // 80m → stamps the profile
	drain(t, s)

	waitFor(t, "observation healed after profile re-offer", func() bool {
		dbw := openRaw(t, cfg.Path)
		return countRows(t, dbw, `SELECT COUNT(*) FROM observations WHERE synced = 1`) == 1
	})
	if after := smc.sawUUID(pUUID); after <= offersBefore {
		t.Fatalf("SY4: the LOCAL profile was never re-offered (%d → %d offers) — selection priority must override its synced=1", offersBefore, after)
	}
	if synced, _, _ := syncMark(t, cfg.Path, "profiles", pUUID); synced != 1 {
		t.Fatalf("SY4: the re-offered profile must end synced=1 again, got %d", synced)
	}
}

// SY5 — the healthy-channel live guarantee, scaled: a captured slot's rows
// reach SMC within a debounce window, not a backlog tick.
func TestSY5_LiveWithinOneCycle(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 10*time.Second, 50*time.Millisecond, time.Second)
	smc := newFakeSMC(t)
	cfg := syncedConfig(t, smc.ts.URL)
	s := newRunning(t, cfg)

	start := time.Now()
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)
	waitFor(t, "live push", func() bool { return smc.requestCount() > 0 })
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("SY5: live rows took %v to leave — that is backlog cadence, not the live lane", elapsed)
	}
	uuid := obsUUID(t, cfg.Path, slotAt(0))
	waitFor(t, "live observation at SMC", func() bool { return smc.sawUUID(uuid) > 0 })
}

// SY5 — a live signal cancels an in-flight backlog request, and the
// intentional cancellation does not advance backoff.
func TestSY5_LiveCancelsBacklogWithoutBackoffPenalty(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 3*time.Second, 10*time.Second)
	smc := newFakeSMC(t)

	// Boot 1, sync OFF: accumulate a backlog.
	cfg := testConfig(t, true)
	s1 := newRunning(t, cfg)
	for i := 0; i < 4; i++ {
		s1.CaptureSlot(obsSlot(slotAt(i*15), 14.074, true))
	}
	drain(t, s1)
	s1.Stop()

	// Boot 2, sync ON, first (backlog) request stalls until canceled.
	smc.mu.Lock()
	smc.stall = true
	smc.mu.Unlock()
	cfg2 := cfg
	cfg2.Sync, cfg2.SyncURL, cfg2.SyncToken = true, smc.ts.URL, "test-token"
	s2 := newRunning(t, cfg2)
	waitFor(t, "backlog request in flight", func() bool { return smc.requestCount() == 1 })

	liveAt := time.Now()
	s2.CaptureSlot(obsSlot(slotAt(300), 14.074, true)) // the live signal
	drain(t, s2)

	waitFor(t, "stalled backlog request canceled", func() bool {
		smc.mu.Lock()
		defer smc.mu.Unlock()
		return smc.canceled == 1
	})
	waitFor(t, "post-cancel request", func() bool { return smc.requestCount() >= 2 })
	smc.mu.Lock()
	second := smc.times[1]
	smc.mu.Unlock()
	if gap := second.Sub(liveAt); gap > time.Second {
		t.Fatalf("SY5: post-cancel request came %v after the live signal — a backoff-scale delay means the intentional cancellation advanced backoff", gap)
	}
	liveUUID := obsUUID(t, cfg.Path, slotAt(300))
	waitFor(t, "live row at SMC ahead of drained backlog", func() bool { return smc.sawUUID(liveUUID) > 0 })
}

// SY8 — an invalid or incomplete response consumes no rows.
func TestSY8_InvalidResponseConsumesNothing(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 30*time.Millisecond, 200*time.Millisecond)
	smc := newFakeSMC(t)
	var mu sync.Mutex
	invalid := true
	smc.respond = func(recs []evidencewire.Record) (int, any) {
		mu.Lock()
		defer mu.Unlock()
		if invalid {
			// One outcome short: a response that does not answer every row.
			resp := evidencewire.PutResponse{}
			for _, r := range recs[:len(recs)-1] {
				resp.Outcomes = append(resp.Outcomes, evidencewire.RowOutcome{UUID: r.UUID, Outcome: evidencewire.OutcomeAccepted})
			}
			return http.StatusOK, resp
		}
		resp := evidencewire.PutResponse{}
		for _, r := range recs {
			resp.Outcomes = append(resp.Outcomes, evidencewire.RowOutcome{UUID: r.UUID, Outcome: evidencewire.OutcomeAccepted})
		}
		return http.StatusOK, resp
	}

	cfg := syncedConfig(t, smc.ts.URL)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)
	waitFor(t, "first (invalid) response processed", func() bool { return smc.requestCount() >= 1 })
	time.Sleep(50 * time.Millisecond)
	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM observations WHERE synced = 1`) +
		countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE synced = 1`); n != 0 {
		t.Fatalf("SY8: %d rows consumed by an incomplete response, want 0", n)
	}
	mu.Lock()
	invalid = false
	mu.Unlock()
	waitFor(t, "re-offer under a valid response", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations WHERE synced = 1`) == 1
	})
}

// SY9 — offered_at: set before dispatch, preserved across re-offers, NULL
// only when never offered.
func TestSY9_OfferedAtIsPreservedSendIntent(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 30*time.Millisecond, 100*time.Millisecond)
	smc := newFakeSMC(t)
	var mu sync.Mutex
	fail := true
	smc.respond = func(recs []evidencewire.Record) (int, any) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return http.StatusInternalServerError, errorBody{}
		}
		resp := evidencewire.PutResponse{}
		for _, r := range recs {
			resp.Outcomes = append(resp.Outcomes, evidencewire.RowOutcome{UUID: r.UUID, Outcome: evidencewire.OutcomeAccepted})
		}
		return http.StatusOK, resp
	}

	cfg := syncedConfig(t, smc.ts.URL)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)
	uuid := obsUUID(t, cfg.Path, slotAt(0))

	var first string
	waitFor(t, "send-intent recorded before any success", func() bool {
		synced, off, _ := syncMark(t, cfg.Path, "observations", uuid)
		if synced != 0 || off == nil {
			return false
		}
		first = *off
		return true
	})
	waitFor(t, "a second (failing) attempt", func() bool { return smc.requestCount() >= 2 })
	if _, off, _ := syncMark(t, cfg.Path, "observations", uuid); off == nil || *off != first {
		t.Fatalf("SY9: offered_at changed across re-offers (%q → %v) — COALESCE must preserve the FIRST send-intent", first, off)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	waitFor(t, "eventual success", func() bool {
		synced, off, _ := syncMark(t, cfg.Path, "observations", uuid)
		return synced == 1 && off != nil && *off == first
	})
}

// SY10 — the batch cap bounds every kind, profiles included.
func TestSY10_BatchCapBoundsProfilesToo(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldBatch := syncBacklogBatch
	syncBacklogBatch = 3
	defer func() { syncBacklogBatch = oldBatch }()
	smc := newFakeSMC(t)

	// Boot 1, sync OFF: the archive accumulates more profile versions than
	// one batch may carry (mass-injected — minting 7 real activations would
	// be 7 boots for the same bytes).
	cfg := testConfig(t, true)
	s1 := newRunning(t, cfg)
	s1.Stop()
	db := openRaw(t, cfg.Path)
	for i := 0; i < 7; i++ {
		if _, err := db.Exec(
			`INSERT INTO profiles (uuid, lineage, version, valid_from, name, bands)
			 VALUES (?, ?, 1, '2026-08-10T00:00:00Z', ?, '80m')`,
			// Distinct lineages so UNIQUE(lineage, version) holds.
			fmt.Sprintf("junk-prof-%02d", i), fmt.Sprintf("L%02d", i), fmt.Sprintf("L%02d", i)); err != nil {
			t.Fatal(err)
		}
	}

	cfg2 := cfg
	cfg2.Sync, cfg2.SyncURL, cfg2.SyncToken = true, smc.ts.URL, "test-token"
	s2 := newRunning(t, cfg2)
	waitFor(t, "all profiles synced across bounded batches", func() bool {
		dbw := openRaw(t, cfg.Path)
		return countRows(t, dbw, `SELECT COUNT(*) FROM profiles WHERE synced = 1`) == 7
	})
	s2.Stop()

	smc.mu.Lock()
	defer smc.mu.Unlock()
	for i, b := range smc.batches {
		if len(b) > 3 {
			t.Fatalf("SY10: batch %d carried %d records over the cap of 3 — an over-cap envelope 400s and wedges sync", i, len(b))
		}
	}
	if len(smc.batches) < 3 {
		t.Fatalf("SY10 fixture: %d batches for 7 profiles under cap 3 — the cap was not exercised", len(smc.batches))
	}
}

type errorBody struct{}

// SY7 — structural: the evidence package shares nothing with the QSO
// forwarding path (no queue, worker, or credentials type to contend on).
func TestSY7_NoForwardingImports(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, banned := range []string{
				"internal/forwarding", "internal/qsoservice", "internal/config", "internal/adif",
			} {
				if strings.Contains(p, banned) {
					t.Fatalf("SY7: %s imports %s — evidence must stay independent of the QSO path", e.Name(), p)
				}
			}
		}
	}
}
