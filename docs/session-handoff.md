# Station Manager — Session Handoff

**Purpose:** rolling handoff document across Claude sessions. Captures what was
done in the previous session, where the repo currently is, and **what the next
session should pick up**. Read this first when starting a session — it exists
precisely so we don't re-derive state or redo finished work.

**How to use this document:**

**Structure (reworked 2026-08-02 — orientation and the record are now separate):**

- **`## Now`** — the ONLY section the SessionStart hook injects. Under ~25
  lines: where we are, what's next, what must not be started. Read it first.
- **`## Current state`** — the rolling detailed record, newest arc first. NOT
  injected. Read it when `## Now` isn't enough.
- **`## Active cycle`** — the 1–3 things in flight, newest block first.
- **[`session-handoff-archive.md`](session-handoff-archive.md)** — everything
  rolled off. Grep it; don't read it.

**Why the split.** Until 2026-08-02 the hook sliced `## Current state` at a
prose marker. The marker had been removed from the doc and nothing noticed, so
the hook emitted the file from that heading to EOF — 231 KB. The harness caps
injected output, so each session got `Output too large` plus a 2 KB preview, and
the RECONCILE staleness warning printed underneath was **never delivered at
all**. A section that grows without limit cannot be the thing that gets
injected; `## Now` is bounded by editorial rule and is what the hook reads.

- **At session end:** update **`## Now`** and bump its `(as of YYYY-MM-DD)` —
  the staleness guard keys off that date. Add an arc to `## Current state` if the
  session did something a future reader would otherwise have to re-derive.
- **Rolling window (enforced 2026-08-02):** keep **3 arcs** in `## Current
  state` and **1 block** in `## Active cycle`; roll the rest into the archive
  (newest-first, verbatim). The previous policy said ~12 sessions and, before
  that, 2–3; neither was enforced and the doc reached 3,005 lines / 233 KB. If
  the hook ever prints its `TRUNCATED` notice, `## Now` has outgrown its budget
  — trim it, don't raise the cap.
- **Durable facts go in memory files,** not here. This document is for
  transitory session-to-session state. If something is stable across all
  future sessions (a project invariant, a user preference, a design rule),
  capture it in a memory file under `~/.claude/projects/.../memory/`.

---

## Now (as of 2026-08-10)


<!-- THE ONLY SECTION THE SessionStart HOOK INJECTS. Keep it under ~25 lines.
     It is ORIENTATION, not the record — "where are we, what's next, what must
     I not do". Detail belongs in Current state below, which is NOT injected. -->

- **Spot-network arc, position 2026-08-10 evening.** §4.1 + §4.2 PUSHED
  through `1100cff3`; CI GREEN (`31368590522`); all codex reviews triaged
  clean + deleted.
- **§5 SYNC SLICE BUILT 2026-08-10 — in tree, UNCOMMITTED (operator
  commits).** Criteria-first; four operator rulings taken + dated
  amendments in §4.1/§5.1/§5.3: retention/purge SPLIT OUT as the named
  next slice (this slice deletes nothing, preserves offered-vs-never
  metadata); occurrences/canonicalisation DEFERRED to the page slice
  (replay-complete rows, no identity semantics); `evidence.sync` ONE
  boolean reusing smcloud forwarder creds (validation refuses without);
  ratified constants 1 s · 500 rows · 10 s · 30 s→15 min · 30 s HTTP.
  Built: shared wire contract `internal/cloud/evidencewire` (6 outcomes,
  canonical digest v1 — lexeme-preserving); SMC `PUT /v1/evidence` +
  Postgres migration 0005 (digest identity (tenant,kind,uuid),
  missing-profile probe, row faults never block batch-mates); evidence
  schema v3 (offered_at send-intent COALESCE + quarantine_reason, 1→2→3
  chain); sync engine (live/backlog lanes, live CANCELS in-flight backlog
  without backoff advance, newest-first selection, profile re-offer heals
  SMC restore, invalid response consumes nothing); status `sync` object.
  Specs: SY1–SY9 (`internal/evidence/sync_test.go`) · E1–E8 (store) ·
  H1–H4 (server) · D1–D6 (digest) · V3 (schema). RED-first; 9 reversion
  proofs bit (2 caught real fixture gaps: RFC3339Nano offered_at,
  SY1 known-URL wall); race green (evidence+cloud, real Postgres), tree
  -short green, lint 0 issues; config.md + api-endpoints.md + cloud
  doc.go same-change.
- **§5 arc PUSHED through `bdb3699b`** (`c8df1e9d` slice · `6f4486e3` 3
  P1 fixes + main.go wiring · `bdb3699b` 0005 history note — that note is
  the REFUTATION of 6f4486e3's review P1, refuted on deployment fact: no
  DB ever applied v5-JSONB). All four review rounds triaged, all docs
  deleted. CI run `31376763384` was IN PROGRESS at handoff — confirm
  green (`gh run list -L1`) before building on top.
- **RETENTION SLICE IN FLIGHT (operator rulings TAKEN 2026-08-10, dated
  amendment in §4.1 — read it, don't re-derive): full drop order (current
  capture beats old unacked; quarantined drops = `rejected`, known
  absent); constants 500-max/64 MiB-reusable-pages-clamped-half-watermark/
  256-trigger/64-direct + 4 MiB logical metadata budget (over-budget =
  metadata-pressure drop-new, NO invisible purge); schema v4 persists
  per-row terminal sync_outcome (purge class = accepted|already_present
  ONLY) + loss SEALING (open accumulator never sync-eligible — fixes a
  latent §5 defect); SMC supersession = tombstone-then-delete one txn
  (activates `tombstoned`), summary re-compactable only after its own
  accept; DELETE doesn't shrink SQLite — measure freelist, bounded
  checkpoints, no live VACUUM, queued slots take priority between
  chunks. Criteria RT1–RT10 ruled; RED suite next.**
- **RETENTION SLICE BUILT — in tree, UNCOMMITTED (operator commits).**
  All RED-first: schema v4 (sync_outcome all kinds · loss
  sealed+supersedes · retention_records · 1→2→3→4 chain, factored
  applyAdditiveMigration) · RT10 sealing (latent §5 fix) · SMC 0006
  (retention kind + evidence_tombstones; supersession
  tombstone-then-delete; tombstone gates every kind; E11–E13) · client
  retention sync (syncTables order profiles→retention→loss→obs→cov,
  supersedes on wire) · purge engine (tryFreeSpace at watermark: ≤1 chunk
  ≤500 rows per slot, write-through on ≥1 free page — freelist physics
  MEASURED and cited in retention.go; cloud-present first, then unsynced
  oldest by class never_offered/offered_unacknowledged/rejected, receipt
  SAME txn; TRUNCATE checkpoints, no VACUUM) · compaction (adjacent-
  agreeing runs, ≤64 direct preds, exact totals; summaries re-compact
  only after own accept) · 4 MiB budget → metadata-pressure drop-new
  (state + Retention.Pressure says why) · status retention object.
  Spec RT1–RT10 in retention_test.go (+E11–E13 store side); 10 retention
  proofs bit (incl. 2 fixture gaps the proofs caught: RT3 needed
  cov-acked/obs-pending, RT10 needed budget-0 drops); race green
  (evidence+cloud real PG), tree -short green, lint 0 (dupl factored),
  evidence suite ×3 stable. Docs same-change: api-endpoints retention
  object · cloud doc.go · SY2 header amended (sync never deletes;
  cap purge may, as `rejected`). Old drop-new fixtures dial
  metadataBudgetBytes=0 (ruled full-§4.1 change, comments say so).
- **Retention arc LANDED: pushed through `38a2a4fa`, CI GREEN
  (`31381987118`, `completed success` verified from output — see
  verify-ci-verdicts-from-output memory for the piped-watch false-green
  it corrects). Whole §4/§5 pipeline on main: capture → profiles → sync →
  retention, every review round fixed or refuted.**
- **DOGFOOD CONSENT APPLIED (operator-directed 2026-08-10): live
  config.json now carries evidence {capture:true, sync:true, antennas:
  DX Commander 80/40/30 h=0 coax · VHQ Hex beam 20/17/15/12/10/6 h=12;
  locator OMITTED deliberately — the KG49dj fixture value was untrusted
  vs station grid KH78an}. Backup: config.json.pre-evidence. smd was
  STOPPED; deployed build still `1192-ga9151fea` (pre-evidence).**
- **DEPLOYED + VERIFIED 2026-08-10 13:24 local (`1225-g38a2a4fa-dirty`):
  capture textbook — profiles ACTIVE (2 lineages, 9 bands mapped, minted
  11:24:03Z), retention zeros, no unprofiled rows, no drops. Sync in
  HONEST BACKOFF: `SMC answered 404` — the production SMC
  (192.168.1.200:8091) runs the pre-§5 server without PUT /v1/evidence;
  2 profiles wait unsynced; one warn, no spam — exactly the SY1/SY8
  posture.**
- **OPERATOR-run PACKAGE review of internal/evidence (on 38a2a4fa): 4 P1
  + 3 P2, ALL judged REAL. Fix progress (RED-first, in tree):
  ✅P1-2 Status held s.mu through db aggregates → CaptureSlot stall
  (snapshot-then-query restructure; statusQueryDelay seam travels with
  queries; fillProfileCounts/fillSyncCounts/fillRetentionCounts unlocked).
  ✅P1-1 activation gate now accepts watermark-with-freelist (post-purge
  file never shrinks; manufactured-state fixture — organic ones dodge via
  tail truncation). ✅P1-3 hard-cap ceiling: freelist never authorizes a
  write at/past cap (tryFreeSpace re-check; checkpoint result inspected,
  busy → Debug; activation gate same ceiling). Fixture sweep: dialed
  headroom 4096→64K, cap margins 48K (shm 32K made old margins a
  forbidden config the ceiling exposed).
  ✅P1-4 metadata honesty: receipt end = maxSlot+15s; compaction requires
  TEMPORAL adjacency (prev.end==next.start); retention receipts carry
  dial_mhz — SCHEMA v5 (proper migrate4to5, conditional column; v4 was
  deployed so no in-place edit; frozen-DDL V4→V5 test). ✅P2-1 pragmas
  ride the DSN (_pragma params reach EVERY pooled conn — modernc
  verified by test; the one-time Exec left pool conns at busy_timeout 0).
  ✅P2-2 unsynced purge now GLOBAL oldest-first, selected set split into
  per-class receipts (rejected/offered_unacked/never_offered) in one txn.
  ✅P2-3 200-responses decode through a 1 MiB LimitReader; quarantine
  reasons truncate at 256 runes. ALL SEVEN FIXED; 11 reversion proofs
  bit; fixture physics lessons: tail-truncation collapses small-archive
  pressure (manufactured states + direct-seam tests where organic
  fixtures dissolve); dialed headroom must absorb shm 32K (sweep to 64K/
  48K margins). Race green evidence+cloud+smcloud; tree -short green;
  lint 0; suite ×3 stable.**
- **Dogfood NOTE: smd RUNS the pre-fix build — all findings latent there
  (archive young, far from cap); REDEPLOY after commit (evidence.db will
  migrate v4→v5 at start). SMC deploy still pending (sync in honest 404
  backoff).**
- **Package-review fixes committed `ab9868cc` (UNPUSHED); ITS auto-review
  raised 2 more REAL findings, FIXED in tree: P1 the cap ceiling now
  RESERVES one write's WAL growth (writeWalReserveBytes=64K — a write one
  byte under the cap still appends frames past it; same reserve on the
  activation gate) · P2 the metadata gate reserves for ALL up-to-three
  per-class receipts (receiptReserveBytes=3×256). Both RED-first + proofs
  bit. Fixture band re-tuned: headroom 320K / cap margin 256K (the
  margin−reserve gap must host boot-2's ~60-90K working churn — measured
  via instrumented fixtures). Race/lint/tree green; ×3 stable.**
- **`8efbb2fe` (ceiling-reserve pair) committed; ITS review raised 1 more
  REAL P1, FIXED in tree: sustained drop-new consumed the reserve — each
  dropped slot refreshed the loss row's WAL, crossing the cap in ~13
  drops. Fix: near the ceiling the accumulator extends IN MEMORY (§4.1's
  documented deferral) and a bounded TRUNCATE folds the WAL; the record
  persists with priority at Stop. RED-first + proof bit. This is the
  4th consecutive review round on the same cap-ceiling code — the
  [[review-fixes-need-full-scrutiny]] cluster; the physics (DELETE +
  freelist + WAL fold on tiny archives) keeps outrunning fixtures, so
  the manufactured-state tests now VACUUM+checkpoint before measuring and
  the sustained-drops invariant is cap-not-exceeded (oscillation is
  legitimate), not all-30-drop. TestCap_DropsNewBeforeTheLimit amended
  same reason (fold may resume capture past the watermark). Race/lint/
  tree green, ×3 stable.**
- **`85f1481a` review (ROUND 5 on the cap path) raised 1 more REAL P1,
  FIXED in tree: my own round-4 checkpoint ran UNDER s.mu → a reader
  blocking the TRUNCATE stalls CaptureSlot up to the 2s busy_timeout
  (same class as the P1-2 Status fix 3 rounds back). Moved the checkpoint
  outside s.mu (decision made locked, I/O unlocked); checkpointHook seam
  travels with it; RED-first + proof bit. All gates green.**
- **PATTERN CALL (operator's "watch closely"): 5 consecutive review
  rounds on the SAME cap/drop path, each fix revealing the next — the
  textbook [[review-fixes-need-full-scrutiny]] cluster. Root cause named:
  the §4.1 writer holds s.mu across DB I/O while CaptureSlot needs s.mu
  to stamp. The checkpoint is now out; the LOSS UPSERT
  (`upsertLossLocked`, service.go:839) still runs under s.mu in the
  refresh/close paths — PRE-EXISTING §4.1 design, tiny single-row write,
  NOT flagged by the per-commit reviews, but SAME class (could wait on
  the sync loop's WAL write up to busy_timeout). NOT fixed reactively:
  moving all loss persistence off s.mu is a real §4.1 refactor deserving
  its own criteria, not a round-6 patch. Flag for a deliberate decision.**
- **`340c5aac` (checkpoint-outside-mu) review CLEAN — the cap/drop review
  cluster is CLOSED at round 6 with no new regression.** The full
  internal/evidence package-review arc (ab9868cc → 8efbb2fe → 85f1481a →
  340c5aac, 4 fix commits over the original 7 findings + 4 follow-ons):
  all fixed or noted, all reviews clean or triaged. STILL OPEN, flagged
  not fixed: `upsertLossLocked` writes under s.mu (pre-existing §4.1,
  bounded, same class as the checkpoint) — a deliberate s.mu-off-DB-IO
  refactor if it ever recurs, criteria-first.
- **Evidence-fix stack PUSHED (origin caught up through 340c5aac); redeploy
  smd (v4→v5 migrates at start) + smcloud deploy still pending.**
- **internal/ft8 PACKAGE REVIEW COMPLETE (operator-run, 3 P1 + 3 P2 — all 6
  CONFIRMED REAL + FIXED in tree, UNCOMMITTED; daemon STOPPED so no live-TX
  risk during edits):**
  - ✅P1-flaky-test: `decodesplit_test.go` `Contains(log,"K1ABC")` matched a
    valid TX line (`Transmitting … CQ K1ABC`) as an RX loopback → wall-clock
    flaky RED. Now RX-specific (`~ CQ K1ABC`); **70/70 across phases**.
  - ✅P2-race: `onSlotIdleArmed` read `s.autoWork.selectMode` after unlock →
    captured into a local pre-unlock (`-race` clean).
  - ✅P2-double-publish: removed the redundant re-publish (commit already
    published under the lock, invariant 3) at work_sequencer queued-drain +
    caller_sequencer idlePop; ADR 0067 + publishguard structural checks cover.
  - ✅P1-allowDuplicate-leak: `consumePendingAllowDuplicate()` one-shot clears
    at all 7 commit sites; RED-first reversion proof bit.
  - ✅P2-subscriber-before-Start: Start reconciles subCount (calls
    startCaptureLocked, gate-aware, when enabled && subCount>0); RED-first
    proof bit.
  - ✅P1-starved-windows: `Slot.Starved` off the scheduler's per-boundary
    delta (reused EXISTING minLiveWindowSamples — no invented threshold;
    testable `boundaryStarved` helper); decodeLoop suppresses decode + skips
    (keeps hash) + no sequencer drive + no occupancy + `capture_dropped`
    evidence + empty clock tick (invariant 4). RED-first; 3 reversion proofs
    bit. maintidx exemption for decodeLoop (MI 19, feature-growth class of
    onSlotWorking/runPipeline).
  **Gates: race clean, lint 0, gofmt clean, ft8 suite ×2 green, tree -short
  green, all cmd/ build.** Fix-first (no dedicated pinning test) only on
  P2-race (validated by -race) + P2-double-publish (structural coverage).
  ft8 fixes committed `bf07a552` (UNPUSHED). Its review raised 1 REAL P2 in
  my P1-starved fix — after a lateness SKIP the resync boundary's short
  remainder delta falsely flagged the next (full-window) slot as starved.
  FIXED in tree: `starveResync` re-primes the baseline for exactly the
  boundary after a skip; RED-first + reversion proof. Gates green.
  resync fix `dd751b28`; ITS review raised 1 more REAL P2 — my
  reprime-to-not-starved over-corrected (a source that STALLED during the
  gap → stale ring → decoded as current). Resolved CONSERVATIVELY: the
  post-lateness window is UNVERIFIABLE (filled sampled at service not target
  time; fresh-sample position unknowable), so it's recorded as capture loss
  — one lost slot per rare >2s stall, never a stale decode. RED-first +
  reversion proof (return-false lets a stalled window decode); gates green.
  3rd round on the starved-window code (fix-cluster) — the conservative
  suppression is the cluster-ending SAFE direction; if a later review
  objects it's an operator design call, not another inference.
- **NEXT: OPERATOR commits the unverifiable-resync fix (watch auto-review;
  sm-pg running) → push → CI → redeploy smd (evidence.db v4→v5 at start;
  picks up ft8 fixes too) → smcloud deploy → FT8 soak.** on-air 0067 ·
  Settings defaults dropdown · max_repeats-to-session.
  Dogfood: enabling capture + the antennas block in the live config is an
  OPERATOR action (consent default off; MY_ANTENNA still carries the
  two-antenna string — coupling deferred). SM6MUY remedy pick still open.
  Flake watch: FT8 decode-log (×2) — passed clean all runs today; bridge
  handler 1s waits.

## Current state (as of 2026-08-10)

### 2026-08-10 — decoder-state review arc closed; evidence writer built

**The decoder-state arc (prereq 2's reviews) converged in three rounds, each
fix red-first with probes:** `75f40264` — dial-moved slots RESET the decoder
(`slotDecoder.reset()`; the band-blind hash table must not cross a QSY; TX
slots keep the state-preserving zero-slot skip) and delivery gaps ADVANCE
once per omitted physical slot (StartUTC ÷ SlotDuration; hash survives a
lossy channel; empty-bucket skips ~0.1 ms so no cap). `d8c11a0a` — the
composition hole its own review found: the scheduler can DROP the slot
carrying DialChanged (emitSlot best-effort send), so a dial DIFFERENCE
between consecutive delivered slots now resets exactly like a delivered
moved slot, and the gap advance runs only when the dial held. Final review:
NO FINDINGS. CI GREEN on the push (`31349738895`). Spec trail: AC6/AC8
dated amendments in `internal/ft8/decodesplit_test.go`.

**The §4.1 evidence writer (first capture slice) was built the same day**,
criteria-first with three operator rulings taken before mechanism (each also
folded into the design doc as dated amendments): (1) observations ship with
a NULLABLE profile ref now — NULL means "explicitly unprofiled", never
"pending"; profiles are their own slice BEFORE sync, and sync will treat
null-profile rows as accepted, `retryable_missing_profile` only for non-null
UUIDs absent remotely; (2) the cap is enforced as drop-new at a soft
WATERMARK below the hard physical limit (headroom for WAL/checkpoint + one
coalesced loss interval; decode continues, only evidence writes stop;
resume when capacity returns; purge/acked-first machinery lands with sync);
(3) default cap 500 MiB EXACT (524,288,000 bytes). Build: new
`internal/evidence` package (own evidence.db via modernc — WAL, versioned
schema with synced flags pre-provisioned for sync; bounded non-blocking
writer; one-transaction slot commits; coalesced never_offered loss
intervals via the reserved in-memory accumulator; physical usage measured
over db+WAL+shm; boundary test pins it never imports ft8/sqlite/bridge);
ft8-side `SetEvidenceSink` emitting one EvidenceSlot per PHYSICAL slot —
rich decodes with true outcomes, capture_dropped rows for scheduler-omitted
slots, and decode-failure made a distinct fact (`decoder_error` vs a silent
band); `evidence` config block (capture default OFF per §8 consent;
`types.EvidenceMinCapBytes` floor — in types because config→evidence would
cycle через logging); `GET /v1/evidence/status` (api's ADR 0043 ratchet
entry added with intent); cmd/smd wiring with evidence Stop AFTER ft8 Stop.
Spec EV1–EV9 in `internal/evidence/evidence_test.go`; 5 reversion probes
bit (watermark, gap emission, Validate wiring, defaults fill, non-blocking
enqueue); the one implementation failure caught en route was the cap test's
unphysical 4 KiB headroom fixture — the headroom's sizing rule (must absorb
shm + one slot's WAL growth) is now documented on the var. Also this
morning: the SM6MUY worked-indication bug captured to the dogfood inbox
(untriaged).

### 2026-08-09 — Draft 3 convergence (9 review rounds), run identity, go-ft8 v0.8.0

**Morning: 5 codex bridge findings FIXED red-first** (steady-dedup
disconnect, meter-poll config validation, Stop vs confirm timer, answer-loss
poll counter, count-before-write race) — committed (`2653e859`, `004f547b`),
pushed, CI GREEN. The flaky handler test passed untouched, supporting the
flake assessment (see the flake entry under 2026-08-08 below).

**The spot-network doc REWRITTEN as Draft 3 — the operator's vertical
slice** (docs/v2-design/spot-network/spot-network-design.md; title now
"FT8 Evidence Capture and Live Station Presence"). Build = SQLite evidence
capture (decoded-message records + dt + flags + versioned profiles, size
cap, loss counters) → sync over the EXISTING SMD↔SMC channel (prompt while
live, lazy backlog) → separate latest-state presence endpoint (incarnation
token + snapshotRev, all the ordering rules kept) → ONE public per-call
lookup page (not heard / heard / queued / worked / in-log ladder) →
time-boxed pilot with pre-registered go/no-go. NO
SBE/QUIC/TimescaleDB/MQTT/research-network — all moved to a short "future
directions" section, gated on a researcher asking or an implementer
committing; the full protocol design survives in this file's git history.
MQTT question RESOLVED: heard comes from our own synced decodes
(first-hand, fresher); MQTT noted only as a someday option for non-SM
stations.

**Draft 3 review rounds, all findings verified then folded:**

- **Round 1 (8 findings):** sync identity now UUIDv7-per-observation
  (ADR 0016 pattern — DELETES the generation/sequence machinery; drops
  leave honest gaps); explicit state model offline/monitoring/on_air +
  transmitting flag (sources: ft8 lifecycle / sequencer / bridge;
  heartbeats span subsystem life, not run life); "logged" ladder rung
  scoped to THE RUN via runId (unscoped log lookup inverts the ladder);
  profile sync §5.4; two named pipeline prerequisites (evidence-grade
  go-ft8 results; decode-sink fan-out, single slot now PSK-occupied);
  lookup-as-queue-oracle ACCEPTED explicitly + rate limits; RBN/WSJT-X-UDP
  adapter recorded in §11 (also in dogfood inbox); dt claim narrowed to
  timing diagnosis.
- **Round 2 (7 findings):** deletion TOMBSTONES by UUID (else backup
  restore resurrects deleted rows) + deletion-vs-ongoing-opt-out policy
  split + local-copy boundary stated; RUN IDENTITY named as NEW
  cross-cutting state (mint at run start, survives Stop/Resume +
  reconnection, ends at Abandon/run-end; threads QsoStatus → CompletedQso
  → local QSO → cloud sync; prerequisite 3); immediate snapshot on EVERY
  transition (heartbeat = backstop only) + transmitting sources
  FT8-specific keyed state, NOT TxActive() (tune conflation); normative
  lookup rules (heard TTL 10min provisional, stale DOWNGRADES to "last
  known as of", run-id grace period); loss COUNTERS → loss INTERVALS
  (synced); pilot metric restated as decoded-repeats PROXY; 77-bit payload
  vs 174-bit codeword terminology.
- **Round 3 (6 findings):** evidence gets its OWN evidence.db (WAL
  single-writer + 5s busy_timeout would let purges stall a QSO commit —
  invariant by structure not discipline); presence authority transfers on
  FIRST SNAPSHOT not POST (crashed token-holder must not lock out the live
  client); page delivery SPECIFIED = 5s short polling of a cached status
  endpoint (budgeted apart from oracle limits); cursor language deleted
  (synced flag = scheduling optimisation; manifest endpoint the named
  future remedy); monitoring visibility = two COHERENT modes (hidden label
  + fresh heard answers = privacy in name only); run-grace precedence
  (current run always wins) + criterion 5 (logged-this-run within 1min
  provisional).
- **Round 4 (5 findings):** superseded client YIELDS on 409
  (recreate-on-409 = endless takeover loop; re-acquire only on authority
  staleness or operator takeover); per-call ladder refresh via
  callsign-bound TOKEN (probe rate-limited, refresh cheap); ladder
  run-rungs scoped to Call-CQ runs ONLY (on_air stays honest for
  answer/one-off sessions); on-air-only mode requires observed_at >=
  runStartedAt (else run start exposes the hidden monitoring period);
  heard TTL runs on VALIDATED observation time (archive verbatim, public
  view skew-checked).
- **Round 5 (6 findings):** takeover staleness checked ATOMICALLY by SMC
  at first-snapshot acceptance (sampled age let two healthy clients
  alternate); snapshots carry lastEndedRunId+runEndedAt until grace
  expires (grace from runEndedAt never reconnect); heard validation
  ONE-SIDED (future-beyond-skew excluded; ingest−observed distance is
  DELAY, never grounds); per-row terminal outcomes (accepted /
  already_present / tombstoned / suppressed / retryable_missing_profile /
  permanent_reject→local quarantine); ingest = ONE transaction with
  suppression checked first + suppressed UUIDs tombstoned; lookup tokens
  STATELESS signed (HMAC), cache by canonical (station,callsign) with
  per-station+global caps.
- **Round 6 (7 findings):** go-ft8 prerequisite expanded to 4
  evidence-grade requirements (payload-even-if-unsupported-text, per-msg
  AP provenance, NO evidence-level filtering, stateful per-stream
  Decoder); presence authority state made EXPLICIT (authorityToken +
  per-authority rev + receipt time; "newest wins" language deleted); heard
  BAND-SCOPED during runs (pinned dial + >= runStartedAt in EVERY privacy
  mode; deterministic winner rule); ended-run grace persisted locally +
  SMC retains newest grace record; per-slot COVERAGE records (decoded /
  no_decode / tx / dial_changed / decoder_error / capture_dropped) + loss
  reporter survives its own failure path; finite refresh budget +
  single-flight cache population; opt-out spans ALL public answer sources.
- **Round 7 (6 findings + 1 wording):** decode split at the RICH go-ft8
  result (evidence branch unfiltered/unprojected; curated branch =
  sequencer/PSK/UDP — SetDecodeSink sits after filter+projection,
  service.go:898 verified); destructive requests need a VERIFIED workflow
  (identity evidence at pilot scale, audit record, idempotent, revocable);
  presence authority = a PostgreSQL station-presence ROW under
  row-lock/CAS (a process mutex splits brain across replicas); synced
  observations carry decode-time interpretation (nullable text, parse
  status, decoder build) + canonical-base-call identity rule (/P //R match
  base, display exact); stateless-token scope clarified + per-station /
  global query semaphore; coverage records = first-class synced rows with
  retention coupled to their interval; graceful stop sends a TERMINAL
  offline snapshot (distinguishable from stale).
- **Round 8 (3 P1s):** terminal snapshot RELEASES authority atomically
  (else a polite shutdown locks the restarted daemon out; duplicate
  terminal PUTs idempotent); size cap = PHYSICAL bytes over
  evidence.db+WAL+temp with headroom + short reader transactions (WAL
  outgrows logical caps under long readers; a full filesystem would break
  the QSO path the separate file exists to protect); every cap purge
  recorded — loss INTERVALS for unsynced (gone everywhere) vs
  local-RETENTION records for acked (lives on SMC), both synced.
- **Round 9 (3 P1s):** loss taxonomy THREE-valued (never_offered /
  offered_unacknowledged=remote-UNKNOWN / acked — "unacknowledged" ≠
  "absent from SMC", the lost-ack case); ONE tagged sync envelope — every
  record kind (obs/coverage/loss/retention/profiles) gets UUIDv7 + digest
  + per-row outcome, purge+audit-record in ONE SQLite transaction;
  callsign canonicalisation = ONE VERSIONED function applied at every
  comparison point (exact forms stored, current version applied at query
  time). Reviewer standard: implementation-ready once P1s close — closed.

**go-ft8 v0.8.0 landed the §4 prerequisite-1 evidence-grade features**
(verified payload incl. unsupported families, AP/decoder provenance,
unfiltered results, stateful per-stream Decoder) — bumped in `1df6d94d`.
Its review's P2: v0.8.0 surfaces CRC-valid unsupported/reserved/invalid
payloads as TEXT-LESS messages (v0.7.1 rejected them), so `dropUnparsed`
at DecodeSlot's return now filters curated consumers to ParseStatusParsed;
the comment marks this as THE curated-branch filter with the evidence
branch tapping upstream (design §4 prereq 2 arriving early).

**RUN IDENTITY (prereq 3) BUILT** — while the operator ran a 95+ pile-up
(mid-run health sweep clean; first natural tx_still_keyed SELF-RECOVERY at
14:43:43, the re-unkey path validated live). Spec
`internal/ft8/runidentity_test.go` RI1–RI9 (header carries the criteria):
UUIDv7 minted at StartCallCq + armAutoWorkLocked (doc §6.2 amended —
mode-only arming means ANY arming start births a run), Sequencer-level
runID/runStartedAt (CQ shape runs with armed=false, so NOT in
autoWorkState; outlives contacts, so NOT contactFlags), carriage in
applyRunStateLocked (terminals included, B12 rule), ends at
abandonLocked/auto-Stop/invalid-arm, stamped via completedCallerQsoLocked
→ BuildQso → types.Qso.AppSmRunID (app_sm_run_id; additional_data + cloud
payload ride free) + adif round-trip + spec classification. Its review's
2 P1s produced the **pin-at-commit model** (`5011dc83`):
contactFlags.runID pinned at all 5 contact-commit sites (3 factored into
commitCqContactLocked); completions read the PIN, never live s.runID —
answer-seed contacts carry their run (RI8), a Stop mid-contact no longer
strips the in-flight contact's association (RI9). All probes bit exactly;
ft8/adif/api/qsoservice green, race green, gofmt/vet clean. Review of
`5011dc83`: NO FINDINGS.

**Session ended by power cut**; all five commits were pushed beforehand.
On resume: CI confirmed GREEN, `.codex-reviews/` empty.

### 2026-08-08 (evening) — ADR 0067 built A–D in one day

**The one-rule run model went from ratified to fully built.** Slice A
replaced the 0065 intent grammar with mode-only arming (`armAutoWorkLocked`
reads `pendingAnswerMode` alone; pick arms a LISTING run that transmits
nothing unpicked). Slice B added the daemon bag-and-drain queue
(`cq/bag`/`unbag`/`resume`; drain order = bag order with 3-min staleness
expiry at drain; Stop pauses/Resume continues; its review found a P1 —
re-heard bagged callers now REFRESH their queue entry instead of relisting,
B11 — and a P2 lock race, both fixed). Slice C rebuilt the SPA: ONE run
surface in the old checkbox/chip slot (Answer-mode selector relocated +
locked under a run, ratified state strings verbatim in
`RunSurface.svelte.test.ts`, Stop/Resume), the drawer rewritten around the
daemon's two lists (Work/Bag per listed row; × unbags; footer Resume),
ctrl/cmd+click bags daemon-side, and the whole SPA drain machinery deleted
(`ft8Pileup.svelte.ts` + drain `$effect` + parity lock + UtilRail/badge
reads). Its review found a real P2: terminal frames carried only
`AutoWorkArmed`, so a completed pick contact blanked the drawer until the
next slot — fixed by factoring `applyRunStateLocked` onto BOTH statusLocked
and terminalStatusLocked (B12 pins it). Slice D (in tree) retired the
config key/GET seed/wire fields and swept the docs; ADRs 0059/0065/0066
carry dated supersession notes. Six slice-C reversion probes all red on
their own assertions — one first attempt was INVALID (called an unimported
symbol, test stayed green) and was caught by reading the failure, redone.


### 2026-08-08 (afternoon) — ADR 0066 designed+built in hours; the drive alarm's poll witness

**ADR 0066 — "the way this works is too confusing" became a shipped design
the same afternoon.** The flip's first on-air contact ("auto-work next
contact is not working") turned out to be an explicit `operator_pick` in
config (the 7c prep edit — my "key absent" grep had missed the space after
the colon, corrected in-flight), and the operator ruled the config-knob
model itself the defect: **all run knobs session-based, config.json holds
only defaults**. Designed criteria-first (layout forks ratified in
conversation: one row → far-ends justified → "Answer mode" label → TX-offset
readout removed into the Call CQ title), Accepted try-and-adjust, built both
halves TDD: `answer_mode` rides cq/start + the auto-work intent, the arming
gate consumes staged session facts (`SetAutoWorkCallers`/`autoWorkPolicy`
DELETED — the knob is only the toggle's GET-served boot seed), "I pick"
drops the intent at the source with the toggle disabled-and-explained, the
config PUT accepts all three literals as defaults. Re-derived en route: W2,
W10 (inverted to config-DEFAULT-through-the-service), V3, G2, the
refused-sink and AW fixtures; the sessionend AST guard rejected the first
run-mode field name (`autoWork.mode` → `selectMode`) exactly as designed.
Specs R1–R6 + SP1–SP4b, all reversion-probed. Reviews: d7fbf935 P1
(selector editable while idle-and-armed → lock widened, SP4b) · three clean
rounds. ALC chip trimmed to label+dot on request (value on the card).

**CI: a silently-failed push, a lint flake, then green.** The 0066 push
never reached origin (found via `ahead 2` — the run everyone assumed was
running didn't exist); the re-push failed on 3
`no-unnecessary-type-assertion` errors a flaky local pass had hidden
(getByTestId returns `any` here; casts→annotations, deterministic both ends
now). Run `31252713891` then went green — 14m03s, full pipeline.

**The drive alarm cried "no RF output" on every transmission (12:29) with
full output measured in the same log lines.** The pushed RM0 stream had
collapsed to 6–8 frames/13 s (the rig pushes on CHANGE; the PO envelope was
dead flat, po_min=po_max=105 — which also corrects the morning's gap
dismissal: those gaps tracked STEADY PO, not hands on the rig) while the
ADR 0064 poll answered 53×/slot at 104–108. Fix: the alarm consults the
poll as a second witness — polled PO **> 0** inside the silence window
withholds it (zero is the alarm's own claim; a measurement beat it), polls
at zero or stale still alarm, recovery untouched (keys off the pushed gap),
P4 intact. Spec DP1–DP3; new watch state `poll_output`; pushed as
`773cc0b9`. Its review found a real P1: the full-window re-arm trusted one
positive poll for ~2× the freshness bound and could defer a real collapse's
alarm past unkey (timer cancelled, nothing fires) — DP4 pins the timeline,
the re-arm now uses the witness's REMAINING lifetime; in tree awaiting
commit. Mid-session triage note: DL6LG stalling at the reporting rung was
propagation (−19 at their end), not the station.

### 2026-08-08 (midday) — antenna repro clean; the operator_pick default flip; the CI gate found red and fixed

**Stuck-TX: the antenna experiment closed the deliberate-repro programme.**
Pre-registered in the inbox before the run (per ATDD), executed by the
operator at 08:30: three 2 s tunes into the DX Commander on 20 m, every off
confirmed idle within ~2 s, zero alarms/re-unkeys/auto-offs in the whole
hour. With duration and FT8-residue refuted on 2026-07-24 and ingress now
unconfirmed, no hypothesis survives with evidence — the fault is filed
intermittent, deliberate keying stops, and the next natural occurrence on a
current build self-recovers (the txrecheck re-unkey retry) and logs richly.
Two corrections recorded on the way: tune power is the ADR 0027 code-const
20 W (the incident narrative's "30 W" was the SSB operating power, so power
fidelity held automatically), and the 07:23 `tune auto-off fired` lines were
the overdrive session's deliberate full-length carriers, not the 05:10
anomaly class. The backlog P1 "tx-alarm self-clear" validation stays open
but passive — it needs a real stick, and provoking one is no longer
justified.

**The operator_pick default flip (built TDD, dated notes in ADR 0065 +
0033).** Asking where the Settings knob for `caller_answer_mode` lived
surfaced two things: the knob is a PORT GAP (the dropdown existed only in
the retired logging SPA — inbox entry filed, `ft8_max_repeats` is the
natural same-card sibling since its "blocked on placeholder Settings"
reason expired 2026-08-05), and the DEFAULT contradicted the operator's
licensing intent. Ratified same session: `DefaultFt8CallerAnswerMode` =
`operator_pick`, scoped as the RESOLVE FALLBACK (option A — absent AND
invalid keys fail toward the non-automatic mode, everywhere, not just
fresh installs). Spec: `TestResolveFt8CallerAnswerMode` (hardcoded
literals), `TestResolveFt8AutoWorkCallers_AbsentModeDoesNotArm` (the
sharpest consequence: knob-on + no mode arms NOTHING), the api GET default
test; reversion probe put all five failures on their own assertions. Five
ft8 run-semantics tests had their fixtures opt into automation explicitly.
Docs swept (config.md, ft8.md, api-endpoints.md, ft8/CLAUDE.md). The codex
P1 on the follow-up ("fresh installs can't save — the frontend echoes the
resolved pick back and PUT 400s") was REFUTED with rationale in
handler_config.go: no served client echoes the field (app sends
display/psk/decode_log only; config SPA never touches it; the one client
that did was retired + un-embedded 2026-07-21). The kernel it contained —
a future hydrate-echo UI would 400 — is the read-only-render constraint
now recorded at the validation site and the inbox port-gap entry.

**The CI gate had been red ~2 days and nobody noticed.** Every push since
mid-2026-08-07 failed at golangci-lint: `runPipeline` and `onSlotWorking`
crossed the maintidx floor (both MI 19 vs 20) by GROWING with ratified
work — the ADR 0064 meter-poll lifecycle and the ADR 0065 arming
refinement. Not drift (2.11.3 pinned both ends, reproduced locally
byte-for-byte). Fix: exemptions-with-rationale in `.golangci.yml`, same
class as their already-exempted siblings readLoop/onSlotCalling
(dispatch-shaped, comment-dense, TX-path, invariant-covered) — "exempted,
not blessed", they join the refactor backlog the list is. Because every
red run truncated at lint, the tail (race, five build shapes, FT8 decode
test) had run dark for days — de-risked locally before the push (race
suite, static + pocketfft builds all green), then the push produced the
first fully-green run in 40+: **14m02s is the honest cost of the full
gate**. Habit fix folded into the codex-reviews memory: `gh run list -L1`
in the post-commit loop. Cosmetic follow-up noted: the workflow's
checkout/setup actions target deprecated Node 20 — a version-bump commit
someday, own-commit rule.

**Also:** the PSK Reporter IPFIX announcement was wordsmithed, its
test-collector detail verified (tree + external sources agree: production
`report.pskreporter.info:4739`, test listener 14739, analyse-don't-write),
and SENT to the PSK Reporter Google group — collector-side feedback, if
any, lands in the inbox. Incidental: our ipfix.go cites RFC 5101; the
current IPFIX RFC is 7011 (same wire version 0x000A) — cosmetic.

### 2026-08-08 (morning) — hardware-acceptance day: everything validated, ADR 0064 Accepted

Operator deployed at 05:59 (`1171-gf9444aac`, = HEAD at the time) and ran
the itemised acceptance list top to bottom. Commits: `eaba629b`
(ratification records) + `02733fa3` (the fold); both reviews clean.

**Passive batch — four for four, first try.** Deploy confirmed from the log
version field. VFO select validated exactly as pre-registered: VFO-B on a
deliberately different frequency, one click, the operating dial moved to
B's own content — VS is a true select, the manual legend holds, and archive
S~2527's "flag only" observation was the equal-contents confound (citations
written at verification time into `rig.svelte.ts` selectVfo + the inbox
entry). Selection-restore round trip confirmed on hardware. A26 boot-capture
demonstrated and RATIFIED (park on phone → boot the browser on
/app/operate/ft8 → rig untouched, confirming boot-is-not-a-switch → FT8
band click moves it → switch to phone returns it): the A4 amendment is no
longer drafted-only; the criterion header records the ratifying demo.

**The overdrive run — the datum was about the INSTRUMENT.** First attempt
"couldn't reach red": sink pushed to the 1.0 digital ceiling, RM ALC
answered max 30. Operator then reported the front-panel needle +20 dB OVER
the zone — and the log agreed something was badly hot: in-band PO collapsed
from the healthy 109–121 to ~35 on both PO witnesses across three slots
while ALC answered 29–30. Conclusion, measured: **the RM ALC answer
saturates at ~30 of 255** — it cannot distinguish zone-edge drive from
gross overdrive, §4 (iii)'s meter-face agreement fails in the over-region,
and no ALC-only threshold above ~30 can ever fire (even the 3900% mixer
typo would read 30). Measurement recorded in `internal/bridge/meters.go`'s
measured-on-hardware lineage. Meter-frame gaps of 2.0–2.5 s appeared ONLY
on hands-on slots (meter-face flip, mixer slide; hands-off ≤ 400 ms) —
dismissed on that correlation, re-check trigger: a 2 s+ gap hands-off.

**Red folded into amber (operator-ratified) — built TDD both halves.**
Daemon: `ft8.meter` is amber-only (`Ft8MeterConfig`/`Ft8MeterLevels`,
`DefaultFt8AlcRed` gone, no cross-clamp — 999 clamps to 255 now); served
shape pinned (`{"alc_amber":30}`); a legacy `alc_red` key is tolerated and
ignored (pinned; the dogfood config.json has no meter block at all —
verified). SPA: `red` state removed, amber terminal with the action label
("ALC high — reduce the audio level"), card marker moved to the amber
floor. Informative RED both halves, reversion probes both halves (each spec
failed on its own fold assertion against the restored old code), full gates
green. ADR 0064 flipped **Accepted** with a §4 results block (including the
qualified (iii) outcome and the gap dismissal); the §4 "iii"→"(ii)"
cross-reference fixed; config.md + api-endpoints.md match. The paired
ALC+PO overdrive detector (ALC-at-ceiling AND PO-collapsed — the run's
two-witness signature) is captured in the inbox with its open judgement
calls (PO floor, chip-vs-alarm, persistence), explicitly NOT built.

**Deploy note:** the fold is not live until the next redeploy — the running
daemon serves the old two-threshold shape, the SPA falls back fine either
way. Nothing rig-critical.

## Active cycle (the 1–3 things in flight now)

> **▶ RE-UPDATED 2026-08-02. Nothing is mid-flight — SHIP GATE (a) closed clean
> and the tree is committed, deployed and at HEAD. These are the next picks, in
> the order the operator and I last discussed them.**
>
> - **1. SHIP GATE (c) — notification records. THE LAST GATE ITEM.** The whole
>   notification category has no daemon record: toasts are client-side, several
>   with no daemon counterpart at all, so closing the tab erases them. This is
>   what still blocks "ship anything". (a) and (d) are done; (b) QSO deletes
>   remains open but is partly covered — the `qso_history` row lands, so
>   provenance survives; it is the admin-readable file that misses it.
> - **2. The config UI port — five tabs.** The operator's framing when picking
>   (a): *"(a) also points us toward completing the config implementation for the
>   UI."* The standalone config SPA is STILL SERVED at `/config/`
>   (`internal/api/server.go:309`), so nothing is unreachable — this is
>   consolidation debt under ADR 0044, not a functional gap. App-shell Settings
>   has **Station + Rigs**; still to port: **General (174 ln), FT8 (219),
>   Email (158), Enrichment (128), Forwarding (114)**. Budget realistically:
>   Station's 178-line tab became 413 lines with its state module and tests, and
>   Rigs' 273 became 1,273 — so ~2,500–3,500 lines over five increments, not one
>   sitting. **Now cheaper to verify:** every save the new tabs make is logged
>   with a field-level delta, so a ported tab is checkable against `smd.log`
>   rather than by eyeballing `config.json`.
> - **3. SSE reconnect on `visibilitychange`.** From the 2026-08-01 inbox triage:
>   the 07-28 "Cannot reach the daemon" report and the 07-18 map-staleness report
>   share ONE root cause — nothing recreates a dead `EventSource` when a tab is
>   restored. `mapData.svelte.ts:310` heals map DATA but not the stream. Fix once
>   at the SSE layer and both reports close.
> - **PARKED, operator-flagged "come back to this shortly":** `operator_pick` /
>   Call-CQ auto_off. A Call-CQ run ALWAYS auto-works answerers; the one manual
>   mode is accepted by config validation and REJECTED at runtime as
>   unimplemented. Detail in the `ft8-cq-answerer-selection` memory. **Not scoped
>   — do not start without the operator.**
> - **EVIDENCE NOW ACCRUING (no action, just don't lose it):** the three hub
>   eviction logs have been live since the 08-01 17:01 deploy. The operator's
>   standing instruction is **DO NOT TUNE THE BUFFERS** (8 ft8 / 64 bridge/events)
>   until those records show healthy clients actually being evicted. Zero
>   evictions in the 15 days before the feature existed.
