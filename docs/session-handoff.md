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

## Now (as of 2026-08-07)


<!-- THE ONLY SECTION THE SessionStart HOOK INJECTS. Keep it under ~25 lines.
     It is ORIENTATION, not the record — "where are we, what's next, what must
     I not do". Detail belongs in Current state below, which is NOT injected. -->

- **END OF DAY 2026-08-07: tree CLEAN at `d217ed73`, every review triaged +
  deleted** (9 review rounds today — 8 codex + 1 pasted external — 6 real
  findings, all fixed same-day RED-first). **DEPLOYED `1152-g80db5a92` at
  13:55 and went ON AIR**; the deploy now trails THREE runtime commits —
  `c3d67c82` (ALC amber band → chip reads green at healthy drive),
  `5c22fdd9` (TX-path fixes: PSK spot attribution + a PTT panic guard),
  `d217ed73` (logging: duplicate-refusal + enqueue outcome lines) —
  **redeploy before the next on-air session**.
- **First live ADR 0064 data:** healthy FT8 drive = **ALC 15–18 every slot,
  PO flat** — broke the chip's zero-only green; **colour bands ratified +
  built** same afternoon (`alc_amber` 30 RATIFIED; `alc_red` 50 still
  PROVISIONAL, needs the §4 iii deliberate-overdrive datum).
- **ADR 0065 is deployed but UNEXERCISED on air** (afternoon ran
  `auto_first`, no arm gesture). Open checks: pill on a work-caller arm · FD
  click leaves toggle lit, no toast · a pick run
  (`ft8.tx.caller_answer_mode: "operator_pick"` in config.json — stop smd,
  edit, start).
- **Evening: pasted external review, 3 REAL TX-path defects fixed** (detail
  in Current state): PTT panic guard now registers the line after KeyTx ·
  decode reports stamp their CAPTURE dial (`ft8-decode` `dial_mhz`; PSK sink
  + SPA attribute from it, never live state) · an arm re-checks the dial at
  commit (stale pre-lock pin).
- **Late evening — the logging round** (detail in Current state): enrichment
  ctx-cancel warns → Debug (one rule in the shared helper, all ten sites) ·
  **qsoservice Tier 1 CLOSED** (Q1 restore outcomes, Q2 enqueue all-paths
  all-five-counts, Q3 duplicate-refusal record — spec `logginggaps_test.go`) ·
  log rotation VERIFIED ALREADY BUILT (lumberjack, 100 MB/5/30 d/compress in
  config.json — the "untracked gap" claim was wrong; nothing filed). Logging
  backlog's remaining Tier 1: api A2/A3 only.
- **Earlier today** (detail in Current state): ADR 0065 end to end (per-click
  arming + operator_pick, 10 ratified forks) · ADR 0064 full build · inbox
  fully triaged · toast split · session search · morning-log diagnosis.
- **NEXT:** redeploy (`d217ed73`) → on-air: §4 iii deliberate overdrive →
  ratify `alc_red` → flip 0064 Accepted · the 0065 sanity checks above ·
  api A2/A3 (last Tier-1 logging) · findings 9/10 · paste-list port ·
  ctrl+click-on-CQ gesture (0065, undecided) · Tune-coverage question.

## Current state (as of 2026-08-07)

### 2026-08-07 (late evening) — the logging round: two quick wins and Tier 1 closed

Directed as "the two honest quick wins + the qsoservice Tier 1 block". One of
the quick wins dissolved on verification; the rest built TDD (all rules
RED-first, six reversion probes, full `go test ./...` green, codex clean).

- **Enrichment ctx-cancel → Debug:** the demotion lives in the orchestrator's
  shared `warn` helper so all TEN call sites follow one rule — an error rooted
  in `context.Canceled` is the CALLER hanging up (aborted SPA fetch,
  connection death, shutdown), not a provider fault, and it was logging full
  Warn error-chains in bursts (9 of the morning session's 24 warnings).
  `DeadlineExceeded` deliberately stays Warn: our own timeout catching a slow
  provider. Three-way spec: canceled→debug (message intact) / real fault→warn
  / deadline→warn.
- **Log rotation: nothing to build — the claim was wrong.** Verification
  found lumberjack fully wired with config.json values 100 MB / 5 backups /
  30 days / compress; the 39.5 MB file just hasn't hit the threshold. No
  backlog entry filed (an entry for something that works is the stale-entry
  trap). The "untracked gap" statement in the outstanding-items answer was an
  inference from file size, made without the one grep that would have
  settled it — same failure shape as [[verify-backlog-before-building]].
- **qsoservice Tier 1 (Q1–Q3) CLOSED**, spec
  `internal/qsoservice/logginggaps_test.go` with each finding's confusable
  pair in the header: Q1 — `Restore` logs per-call Debug with distinguishable
  `outcome` (real recovery vs idempotent re-run was identical silence) + a
  durable per-run Info summary in `cmd/smd/restore.go` (that one main-package
  line is the single untested piece, mirroring the adjacent stdout prints);
  Q2 — the enqueue outcome line fires on EVERY return path incl. the
  zero-enqueue early return (all-refused ClubLog-compliance vs never-invoked
  were the same silence), carrying `requested` + all five counts, lengths
  only; delete-repair path got the sibling + its missing `not_found`;
  Q3 — a refused duplicate logs at Info naming the submission and the
  colliding row, one helper on both the pre-check and unique-index race
  paths. Gaps doc annotated ✅; the package's remaining tail is Q4–Q10
  (Tier 2/3), and the audit's remaining Tier 1 anywhere is api A2/A3.

### 2026-08-07 (evening) — pasted external review: three real FT8 TX-path defects

All three verified against the code, fixed RED-first with reversion probes,
gates green under CGO + CGO-free + -race; the fix commit's codex round was
clean. Nothing here changes behaviour the operator chose — all three are
correctness holes found by inspection.

- **P1 — PTT panic window** (`txcontroller.go`): `onKeyed` ran between a
  successful `KeyTx` and the unkey defer's registration, so a panic in the
  callback escaped with PTT asserted (bridge 18 s auto-off as the only net) —
  on exactly the path the function's contract says is covered. The guard now
  registers the line after `KeyTx`; a second idempotent registration after the
  evidence defer preserves the LIFO order that makes `keyed_ms` the true
  key-to-unkey span, and an onKeyed panic writes no false "transmitted" line.
- **P1 — decode reports lose their capture dial**: the PSK Reporter sink
  re-read the LIVE dial at publish time; publication lags capture by the
  decode (~0.7–1.6 s), so a QSY in that gap spotted a whole slot on the wrong
  band — the moved-slot suppression can't catch a move that POSTDATES the
  window. `DecodeReport` now stamps `dial_mhz` from `Slot.DialMHz` (same rule
  as occupancy), the PSK sink spots from the stamp (skips unattributable
  slots), and the SPA withholds rows whose capture band differs from the
  view's band (late-arrival tail after the band-change feed clear; no stamp =
  fail-open display). Daemon test pins the captured value AGAINST a live
  source already reading elsewhere.
- **P2 — stale arm pin** (`servicetx.go`): armTx reads the dial before txMu
  (lock order) and committed that entry reading; a QSY inside the window
  pinned the old frequency, and onDialMoved's `!armed` early-return never
  repaired it — with no capture running there is NO move callback at all, so
  the armed-but-every-key-refused state was indefinite. Fix: post-commit
  compare-and-repair under a fresh hold (interim fails closed); an unknown
  re-read keeps the entry pin deliberately (refuse-and-re-arm covers the
  QSY+CAT-blink double fault) — reasoning at the code site. The test also
  pins the repaired VALUE, so a fix-by-zero-pin (7c2e66ad hazard) fails.

### 2026-08-07 (late afternoon) — first live meter data flips the ALC colour grammar

Deployed `1152-g80db5a92` at 13:55 and operated FT8 under the new meter poll —
the first live ADR 0064 data. Healthy drive measured **ALC 15–18 (min 15) every
slot, PO flat at 155–157**, low-power slots earlier read 7–12 — and ALC is
**never zero while keyed** on this rig, which falsified the chip's original
grammar (green = ALC exactly 0): every correct transmission rendered amber, and
amber reads "act to make this green" — the operator asked exactly that from the
rig, which was the finding. Ratified (two forks, recommended options): green =
the healthy band below a new `ft8.meter.alc_amber` floor (default **30**,
covering every healthy datum: FT8 15–18, low-power 7–12, voice 26); amber =
floor..red−1 relabeled "ALC elevated — approaching the red line"; red ≥
`alc_red` unchanged (50, still PROVISIONAL — no overdrive datum exists; §4 iii
produces it). Built TDD both halves (`ResolveFt8Meter` amber floor with
clamp-down-to-red for a floor above the line; `txDriveStatus` band flip pinned
by T2's "ALC 18 must be green"; `setTxDriveConfig` became named-options so
three positional numbers can't transpose). Review clean. Also closed two doc
gaps found on the way: `GET /v1/config`'s response doc listed neither
`ft8_audio` nor `ft8_meter`; config.md had no `ft8.meter` entry.

Log facts from the session worth keeping: the run was `answer_mode:auto_first`
with no arm gesture — **ADR 0065 remains unexercised on air**; AP2TN worked +
logged 14:47 on the new build; zero drive alarms.

### 2026-08-07 (second session) — ADR 0065 designed, ratified and built in one day

The directed work was "triage the inbox → diagnose the morning's on-air logs →
design auto-work/cq-answer/operator_pick", with a stated focus of clearing FT8
features and niggles. All three parts landed, and the design became a full build.

- **Inbox triage:** all 17 untriaged entries dispatched — 13 dispositions
  (backlog moves committed as `213cd1`), 4 fixed same-day, 2 grew into
  ADR 0065. Non-FT8 items (map filter, world clock, session-panel chrome,
  paste-list port, notification rail) went to the backlog per the focus.
- **Log diagnosis (morning session):** VK5GR worked 07:15, no reply, abandoned,
  re-clicked 07:20 → FALSE "already worked" toast, then completed + logged
  (qso 7096) — became fork 4's evidence. Two Abandon-debts (06:01:17,
  07:26:06) — became the ADR's core evidence. 9/24 warnings were idle-disarm
  noise (fixed: Debug when the device was idle). 3 QSOs logged, zero alarms.
- **ADR 0065 (Accepted, both phases built):** per-click auto-work intent
  staged like the logbook pin; gate-refused arms never block the contact (the
  frame's `auto_work_armed:false` is the verdict — SPA toasts once);
  plain start CLEARS an armed run (W12 precedent); FD/type-4 never arm;
  ctrl+shift+click = the ONE modifier chord that starts TX; standing toggle is
  one-shot + in-memory. operator_pick: sequencer lists answerers
  (`answer_mode`+`answerers` on caller frames), `POST /v1/ft8/cq/pick`
  commits, three DISTINCT refusals (no-pick-run 409 / not-listed 404 /
  contact-in-flight 409), parks neither auto-pick nor blacklist,
  stalled/cool-off deliberately do NOT filter the list (auto-re-lock guards,
  not operator guards). Build forks all ratified: 3-min staleness (= SPA
  STALE_MS), refuse mid-contact pops, badge-only discovery, config.json-only.
- **Review rounds:** `b8867450` clean · `7de6708e` P1 (arm ordered AFTER the
  work-caller commit publish → granted arm's first frame read unarmed; moved
  before, pinned G8) + P2 (FD/type-4 clicks leaked the intent → false refusal
  toast ate the toggle; call-site + wrapper guard, pinned AW5) · `883663bb`
  clean · `6b1cf93b` P2 (stale pop refused WITHOUT delisting/publishing — the
  drawer kept offering a gone station; pop now sweeps + publishes on refusal,
  shared `expireAnswerersLocked`) · `80db5a92` clean (incl. -race).
- **Also fixed:** pre-existing lint red at `883663bb` (`onRowClick` complexity
  22>20 — the prior round's "lint clean" claim was wrong; split into
  `answerCqRow`/`workCallerRow`) · engaged-vs-logged toast wording ·
  session-panel callsign search (SR1-SR4) · toast z-60 over dialogs ·
  `ErrNotPlaying` disarm warn → Debug (sentinels moved to untagged
  `internal/audio/playback/errors.go`).

### 2026-08-06 (later) — the A25 FT8 first-entry seed

Operator report from the deployed build: phone→FT8 left the rig on 14.225 USB
("should move to DATA and 14,074"). Root cause was DESIGNED behaviour — the
restore's "no snapshot → nothing" rule (old A3/A4), which after a reload also
poisoned FT8's snapshot with the phone position on exit. The retired SPA never
auto-tuned on FT8 entry either (its watering-hole jump was the band BUTTON), so
this was a new criterion, not a port gap: **A25**, amending A3/A4 to the phone
direction + unconfigured FT8 only. Operator settled the frequency source
mid-build: "use the band the rig is on, just move to an appropriate frequency."

Mechanism: `seedFt8()` in modeRestore — the ft8SelectBand order (dial first,
then mode; a data mode on a phone frequency is the guarded-against outcome)
issued with modeRestore's own per-command holds. Review `c0df1c8a` filed two
real P1s the same afternoon: the seed drove VFO A regardless of selection
(rig.band derives from the SELECTED VFO; setFreq routes by it — the hand-copy
missed that), and the retry marker counted the seed's own dial confirmation as
operator evidence, making a half-seeded {FT8 dial, phone mode} snapshot
permanent. Both fixed in `9c012341` (S13/S13b/S14, red-first against the
committed code); the marker now watches only OUTSTANDING fields. 16 S rules
total; probes A–E proved the constraint rules' teeth (probe E caught S3b's own
decorative fixture — rig parked exactly on the watering hole distinguishes
nothing). CAT-off-seeds-nothing is RATIFIED with the correct rationale:
"Cat-off - ft8 cannot work" — not my drafted display-context reading.

### 2026-08-06 — power cut mid-task: the revive-on-'online' fix

**Morning session (operating day).** Operator committed yesterday's pile-up
drawer work as `67ba8a66` (inert on both drawers); its codex review had no
findings and is deleted. The daemon was deployed at 05:06 from the then-dirty
tree (`2.0.0-alpha.1-1100-g510cb4fa-dirty`) — functionally identical to
`67ba8a66`, so the deploy is NOT behind in behaviour, only in version string.

**"CAT link lost" investigated and root-caused (all passive, nothing keyed).**
The operator swapped internet routers. Timeline, cited:

- NetworkManager journal: `enp10s0 activated → unavailable (carrier-changed)`
  05:23:33; DISCONNECTED 05:23:34; carrier back 05:23:39; new DHCP lease
  (same IP 192.168.1.147) 05:24:17; CONNECTED_GLOBAL 05:24:18.
- smd.log: `GET /v1/rig/events` completed 05:23:39 after 999,623 ms — the
  browser's rig SSE stream, open since page load, died at the carrier drop.
  (`http request` lines log at request COMPLETION — an open SSE shows nothing.)
- After that: ZERO reconnect attempts in smd.log and zero established sockets
  to :8080 (`ss`). The tab's EventSource was dead and not retrying.
- Bridge/serial/daemon healthy throughout: pipeline up since 05:06:56, meters
  live, a rig command at 05:21:39 fine; `GET /app/` answered 200 in 0.4 ms
  during the incident. The ONLY daemon-side symptoms were external-network
  errors (pskreporter UDP unreachable 05:17:02, QRZ TLS timeout 05:12:20).
- Not a server timeout: historical rig-events streams lived 3.5+ h, and the
  SSE handlers use per-write deadlines (`internal/api/handler_events.go`).
- Mechanism (labelled inference): the browser reset its connections on the OS
  network-change signal — loopback included, which no router can break at TCP
  level. `rig.svelte.ts:719` maps the transport error to `cat = 'lost'`.
- Gap: `sse-reviving.ts` revives dead streams ONLY on visibilitychange →
  visible (built for the 2026-07-18 hidden-tab case). Tab visible the whole
  time ⇒ never fired. Workaround used: F5.

**Fix in flight (operator: "Go ahead with the online revive fix - TDD").**
Plan agreed with myself before the cut, not yet executed past the header:

1. O-series rules in `sse-reviving.test.ts` (mirror V-series): O1 revive a
   CLOSED stream on window `online` while visible (the incident); O2 healthy
   stream untouched (FT8-linger safety — also under SPURIOUS online events,
   navigator.onLine is unreliable); O3 first-connect untouched; O4 revive
   stuck-CONNECTING-after-error, and assert the old stream was close()d first
   (retry-timer race); O5 online while HIDDEN revives nothing (JUDGEMENT CALL,
   flagged in the header for the operator — return-to-visible already covers
   it, and a hidden FT8 tab must not re-grab audio capture); O6 teardown is
   final (window listener removed + `src === null` guard, the V6 pair); O7 the
   revived stream is rewired via the caller's WireFn.
2. Fixture trap to avoid, already noted: O1 must NOT dispatch visibilitychange
   after killing the stream (that trigger would revive and make the online
   assertion vacuous). Needs a silent visibilityState setter split out of
   setVisibility(), plus an afterEach reset to 'visible'.
3. RED: O1/O4/O7 fail on their own assertions against current code. O2/O3/O5/O6
   pass vacuously — their teeth get a wrong-implementation probe (unconditional
   recreate on online ⇒ O2/O3/O5 must each go red) before the real
   implementation is kept.
4. Implementation sketch: extract `reviveIfVisibleAndDead()` (visible check +
   isDead() + close/reset/create) and register the SAME handler for document
   visibilitychange AND window `online`; teardown removes both.
5. Reversion proof (remove only the online registration; O1/O4/O7 red for the
   right reason, V-series green), full SPA gate, then refresh the stale "adds
   the one case" comments in `rig-sse.ts` (line ~7), `log-events.ts` (line ~9)
   and `sse-reviving.ts`'s own WHY header — there are now TWO revive cases.

**Also for the operator:** a network bounce during an FT8 run will still
abandon the QSO — stream death starts the 5 s capture linger and the DHCP
outage was 44 s; the revive fix does not (and should not) change that.

### 2026-08-05 — stopped mid-session for a POWER CUT

Tree clean at `4d131720`; every codex review triaged and deleted. **Deploy is
10 commits behind** — the QSO-row coordinate reconcile is the only part that
matters live.

**Shipped.** Settings → **FT8** section (the config-SPA port; **General** is the
last tab before that SPA retires) — no colour pickers, because the three
`ft8_display.highlight_*` keys are vestigial (stored, round-tripped, read by
nothing; Band Activity uses a theme-aware palette). Map **arc fan-out**: repeat
contacts with one station resolved to the same point and drew byte-identical
paths, so 6 QSOs showed 5 arcs and the hidden one was always the older band.
**QSO-row coordinate reconcile**: `ReconcileStationCoords` moved to `adapters`
and applied in `QsoTypeToModel`, the choke point all five QSO write paths share
— the cache had been guarded since 08-04, but the QSO ROW is what ADIF export
and the forwarding worker read. FT8 **band buttons now assert the rig data
mode**; that was an unfinished implementation rather than a design gap (the
daemon already served `ft8_mode` and its field comment said the band buttons
drive `set_mode` with it). SSB only looks automatic because `set_band` triggers
the RIG's band-stack recall; FT8 uses `set_freq`, which triggers none.

**Found, not fixed, undecided.** A full scan of 6,975 QSOs: **121 rows across
104 callsigns** carry coordinates outside their own grid, and the cause is
systematic — 48 distinct pairs, led by 39× the Ukraine centroid and 16× Moscow,
i.e. QRZ returning a country centroid while the grid came from the on-air
exchange. Separately, **5,640 rows (82%)** still hold ADIF-format coordinates
(`"N034 44.378"`) predating the ingress normalisation; they export fine but the
map plots them at grid resolution.

**The arc worth remembering.** The FT8 save-timeout reconcile took SEVEN review
rounds, six of them fixing a defect the previous fix had introduced. It
converged clean, and the current behaviour is safe. The cause is the contract,
not the code: a whole-block `PUT /v1/config` with no revision cannot tell "my
write landed" from "someone else's did", so every remedy is an inference over
four mutable snapshots. **If it draws another finding, add `If-Match`/revision
to the endpoint instead of another inference** — operator's call, not started.


> **2026-08-04 (evening), LAST — THE `internal/ft8` LOGGING SHIP GATE, STARTED.
> 5 of 14 findings closed, 9 to go. STOPPED MID-FINDING-4 at the operator's
> instruction; nothing is half-written, the tree is clean at `4665b5a9`.**
>
> - **Read `docs/reviews/ft8-logging-gaps.md`, not this summary.** It now carries
>   a ✅ note per closed finding (what shipped, in which commit, and where the
>   review's own suggestion was WRONG) plus a Progress block naming what is left
>   and where to resume. **Do not delete the file until all 14 ship** — it is the
>   only place the confusable-state statements live, and those are the behaviour
>   specs the tests are written from.
> - **The operator's decision on finding 6, which was the gate's only open one:**
>   a keyed transmission records **two independent witnesses** — wall `keyed_ms`
>   and `samples` submitted. Each covers the other's blind spot. **It does NOT
>   prove RF left the rig**, and `txcontroller.go` says so in as many words: that
>   belongs to the drive alarm watching the rig's PO meter, and this record is
>   what the alarm gets correlated against. Written down precisely so a later
>   reader does not upgrade it into a claim it cannot support.
> - **Two places the review was wrong, both found by building it.** Finding 2
>   suggested `ActiveCallsign()` for the abandon line — that returns OUR call (the
>   TX identity), not the partner's, so it needed a new `partnerCallLocked()`
>   reading the exchange BEFORE `abandonLocked` clears the pointers. Finding 3
>   said five disarm causes; there are SIX (the retune path was missed).
> - **Shape worth copying for the remaining nine:** the review's own instruction
>   is that "a test asserting only 'a line was emitted' is weaker than the rule —
>   assert that the two confusable states produce DISTINGUISHABLE output". R21
>   therefore drives operator-disarm AND unattended-linger and compares them,
>   rather than checking either alone; R23 guards the enumeration against two
>   causes collapsing to the same string, which would restore the defect while
>   every individual rule still passed.
> - **MY PROCESS FAILURES, twice each, both repeats from earlier the same day.**
>   (1) A reversion proof broke the BUILD (unused variable) instead of failing a
>   test — inconclusive, and it looks like a pass if you only read the exit code.
>   (2) I used `git checkout <file>` to undo a probe and **wiped uncommitted work**,
>   because HEAD does not have work that is not committed. That cost a reapply
>   both times. **Back up to the scratchpad before every reversion probe; never
>   `git checkout` a file with uncommitted changes in it.**

> **2026-08-04 (afternoon) — THE COORDINATE ARC. Four commits, six
> reviews, five P1s, every one mine — plus the map band filter. Started as "what
> map items are left?" and became the whole coordinate perimeter.**
>
> - **What was actually wrong.** A whole-logbook scan found FIVE rows across four
>   stations carrying a correct grid beside South Pole coordinates, all dated
>   27 Jul – 1 Aug — i.e. still being produced, because the 2026-07-30 fix was
>   DISPLAY-ONLY (`rowPoint()`) and the cause was untouched. Root traced to
>   R9LAU's QRZ profile: **Grid Square `AA00aa`, Geo Source "From Grid"**, so QRZ
>   derives the polar coordinates itself and the pair AGREES on arrival. The
>   contradiction appears later, when the real grid arrives from the on-air
>   exchange and the field-wise merge replaces the grid alone.
> - **The architecture that came out of it** (operator's framing): decimal
>   degrees is the canonical internal form and every perimeter converts. Provider
>   ingress normalises FORMAT; the storage merge ARBITRATES grid vs coordinates,
>   because that contradiction spans two writes from two sources and no single
>   adapter can see it; ADIF converts both directions; SM Cloud needs nothing,
>   since it mirrors the canonical form. **The grid ARBITRATES rather than
>   supplies** — precise coordinates survive while it vouches for them. `my_lat`
>   followed: the grid now SUGGESTS, a contradiction is REFUSED BY NAME (a human
>   typed it and is present to be told, unlike third-party data), and legacy
>   ADIF-format configs are MIGRATED — without which the new validation would
>   have refused every existing install at startup.
> - **Four predicates now live once in `utils`** (`CoordsValid` /
>   `CoordsReadable` / `CoordsInsideGrid` / `IsPlaceholderGrid`) so the four
>   layers cannot drift on what "contradicts" means. The naming carries the
>   distinction that cost most: boundaries that ADMIT a value ask `CoordsValid`;
>   the merge, which only COMPARES, asks `CoordsReadable`.
> - **THE REVIEW LESSONS — all five P1s mine, worth reading as a set.**
>   (1) I applied the `AA00` sentinel to the MERGED record, so a refresh carrying
>   only a placeholder ERASED a grid and precise coordinates we already held
>   (verified live before fixing). It is a property of ONE INPUT, and I put it on
>   the wrong side of a line I had drawn an hour earlier. (2)/(3) I wrote
>   `ConvertFromXDDDMMM` as the inverse of a function that validates axis and
>   range, and dropped the `isLat` parameter — an inverse weaker than its
>   forward, in the very change where I cited the asymmetric-round-trip lesson.
>   (4) `canonicalCoord` treated any successful `ParseFloat` as a coordinate
>   (`NaN`, `±Inf`, latitude 91). (5) I then bound-checked the PROVIDER ingress
>   and not the CONFIG one **in the same commit** — a boundary promise is only as
>   good as its least-guarded door, and I guarded the one I was looking at.
> - **Also mine, caught by the reversion discipline rather than by review:** two
>   proofs were inconclusive because the revert failed to BUILD (unused import,
>   unused variable) rather than failing a test — the trap already recorded that
>   morning, hit twice more. And R7b initially passed BY CONSTRUCTION, because I
>   wrote its fixture in post-ingress shape so it could not reproduce the defect
>   it was named for.
> - **A rule DROPPED rather than implemented.** An early `my_lat` criterion
>   distinguished a DERIVED position from an operator-SET one when the grid is
>   cleared. That needs provenance the system does not carry, and inventing a
>   marker is the trap CLAUDE.md names — so the rule is uniform (no grid →
>   coordinates stand) and the reasoning sits in the test file.
> - **Operator decisions, not to be re-litigated:** the five polar rows are NOT
>   repaired or re-uploaded — QRZ shows the CORRECT position for them (its
>   logbook page for the 2026-07-27 R9LAU QSO reads 57.173356 N / 65.559720 E),
>   so the bad coordinates never landed there. ClubLog unchecked.
> - **Provider formats: survey abandoned in favour of the boundary.** QRZ (our
>   code), HamQTH and QRZCQ (both API docs) all return DECIMAL; HamCall could not
>   be checked at all (site unreachable). My earlier "formats vary by source"
>   claim was **wrong** — built on reading a rendered QRZCQ page as an API
>   contract. The durable conclusion is the operator's: you cannot know what a
>   provider sends until you implement it, so the boundary must not assume.
> - **Map band filter shipped** (dogfood-inbox 2026-08-01): a select defaulting
>   to All, options from `station.operating_bands` — the STATION's bands, not the
>   window's contents, which would flicker as QSOs age out. Deliberately NOT
>   persisted: the grey-line toggle beside it ADDS an overlay, this REMOVES
>   contacts, so a filter surviving into the next session opens on an empty world
>   with nothing to explain why.
>
> - **A DOC-LOSS INCIDENT WORTH THE ENTRY.** Updating `## Now` earlier, I sliced
>   from a marker to END OF FILE and replaced it with two bullets — deleting
>   `## Current state` (all three arcs) and `## Active cycle`. The file went
>   317 → 109 lines and rode into a commit unnoticed, because I checked the HOOK
>   OUTPUT (which only reads `## Now`, and looked fine) instead of the file.
>   Recovered from `0dfb24de`. **The rule: an edit anchored on `s[index:]` has no
>   right-hand boundary — anchor on both ends, and after editing a structured
>   doc, check its SECTION LIST, not the part you were looking at.** Exactly the
>   least-guarded-door shape as review finding (5) above, in a different medium.

> **2026-08-04 (morning) — TWO SHIPS AND ONE DECISION REAFFIRMED. Both ships were
> caught wrong by review or by the operator before they were right.**
>
> - **Settings navigation guard (ADR 0063).** Confirm-on-leave naming the dirty
>   sections; `beforeunload`; a confirmed discard happens THERE AND THEN, so the
>   app stops holding edits it has reported as gone. **Three exits, not one** —
>   `navigate()`, `popstate` (which bypasses it, and needs the URL pushed BACK
>   because it fires after the address bar moved), and `setMode()` from the
>   always-visible OperateNav. Rigs needed a new `anyDirty`: `dirty` answers only
>   for the SELECTED rig. **Two review findings, both mine.** P1: I exempted Rigs
>   because `#applyFetched` preserves dirty drafts — and `load()` wipes drafts
>   and baselines outright two statements later, so the exemption was built on
>   half a sequence and rig edits were lost silently. P2: the confirm promised a
>   discard while a PUT was already on the wire, which the daemon would then
>   persist. Both fixed; `R3b` is the characterisation test whose absence let the
>   first one ship. MDN settled `beforeunload` — `preventDefault()` **and**
>   `returnValue`, quoted in the code.
> - **Session ends now say WHY** (three commits, three reviews — `3531e1ed`,
>   `ea0c91a5`, `d13fcb22`, the last clean). Dogfooding a 50-QSO run showed seven
>   of eight `session abandoned` records carrying `reason: ""`. Not one missing
>   label: `Abandon()` is reached from TWELVE places and only the two dial paths
>   staged a reason, so a session that DIED read exactly like one the operator
>   stopped. Three families now: operator Abandon and TX disarm are named in the
>   LOG only (`operator` / `tx_disarmed`) — the frame stays silent because the
>   operator caused them and a toast would narrate their own click; the eight
>   terminal-TX sites carry `tx_not_armed` / `tx_bad_message`, which DO reach the
>   frame per invariant 5. Repeat-cap ends log `no_answer`; their frame half is
>   ACCEPTED with the rationale at the code site (the countdown already
>   telegraphs it, so a toast per unanswered call is noise).
>   **The trap found on the way:** the SPA's unknown-code fallback said "the rig
>   frequency could not be verified" — safe only while every code was
>   frequency-related, and a lie the moment a `tx_*` code existed. Now
>   cause-agnostic. `api-endpoints.md` corrected too (it listed two codes and had
>   never gained `band_change`).
>   **THEN TWO ROUNDS OF MY OWN FIXES BEING THE DEFECT.** Round 2: I staged the
>   reason under one lock hold and abandoned under a second, so a teardown landing
>   between them consumed it — and because the dial guard stages into that SAME
>   slot, a rung failure could OVERWRITE its explanation and report a safety stop
>   as a transmit failure. Fixed by passing the cause as an argument through one
>   lock hold. Round 3 (**P1**): the teardown was still unconditional, so a stale
>   rung could end the session that REPLACED its own — invariant 5's named hazard,
>   reached down a new path. **I had declined exactly this in round 2**, saying the
>   generation was hidden inside `transmitLocked`'s closure and gen-scoping was "a
>   larger change". It was five lines: make the function return the generation it
>   had already bound. "Not in scope" was a fact about the signature, which I
>   treated as a fact about the cost. See [[review-findings-fix-dont-defer]].
> - **FT8 dupe guard: asked for, then correctly NOT built.** The 50-QSO audit
>   found KK2A logged twice. `caller_sequencer.go` carries an operator-ratified
>   2026-07-26 note saying **do not** suppress the re-work; the log showed KK2A
>   asked twice and we re-sent twice, so they never copied the RR73 and the
>   second contact is the only one they got. Operator reaffirmed: no way to know
>   whether they logged it, and the dupe costs little. **The defect is the extra
>   ROW, not the QSO.**
> - **The rest of the run was clean:** 50 stored, 50 forwarded to QRZ + ClubLog +
>   smcloud, zero errors, no drive alarms, `meter_po_max` never zero across 246
>   keyings. The power dips were the operator adjusting volume — confirmed by
>   step-shaped transitions (109→95 between consecutive slots, not a ramp), which
>   also validated the drive-alarm instrumentation in the field: a deliberate 13%
>   drop for 25 minutes raised nothing.

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
