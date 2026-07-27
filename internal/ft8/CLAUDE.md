# internal/ft8 — FT8 subsystem (design detail)

Loaded automatically when working under `internal/ft8/`. This holds the full FT8
design / decision / gotcha detail, migrated out of the root `CLAUDE.md` on
2026-07-22 to keep always-loaded context lean; a high-level pointer remains in
the root file. The single canonical FT8 capture point is `docs/ft8.md`.

## TX + attribution invariants — READ BEFORE CHANGING THE TX OR SLOT PATH

**Never violate these without explicit discussion**, in the sense
`docs/v1-analysis/invariants.md` uses. They were extracted 2026-07-27 after a
nine-round review arc on FT8 dial attribution in which *every* P1 was a violation
of one of them — and in which several fixes broke a rule that was already written
down a few files away. Each is stated in terms an operator or another subsystem
can OBSERVE, deliberately: a logged QSO row, RF keyed or not, a published SSE
status, a spot emitted. Bind tests to these, not to whichever field a mechanism
happens to carry this week — the field-level assertions from that arc were all
deleted within a round or two, while the behavioural ones caught real defects.

1. **A contact the partner has rogered is logged exactly once, ON THE FREQUENCY IT
   HAPPENED ON, whatever happens to our closing rung.** Group A policy
   (`finalrung.go`): the QSO is recorded whether or not the courtesy RR73/73
   reaches the air. Refusing RF, losing the rig, disarming, or any new guard must
   run the rung's completion policy before retiring the session. *Every completion
   callback is generation-guarded, so retiring first makes the callback refuse and
   the contact vanishes silently.* The frequency is the dial the SESSION pinned —
   never a live read at completion time, which differs exactly when a QSY refused
   the closing rung. *This clause was added within an hour of the first draft: the
   original "logged exactly once" was satisfied by a contact filed on the band we
   had just moved to, which is worse than losing it — the wrong-band row is
   forwarded to QRZ and ClubLog (codex P1 on 652821db).*
   Observable: exactly one QSO row + one `ft8-logged` event per rogered contact,
   carrying the session's frequency.

2. **SM never keys unless the daemon can POSITIVELY confirm the rig's frequency —
   and, for a session rung, that it still matches the session's.** This binds
   MANUAL sends too (`TransmitNext`), which do not go through `sessionTxGate` and
   so must repeat the check; skipping them left `/v1/ft8/tx/send` keying with an
   unreadable dial (codex P1 on 652821db). An unknown reading is a refusal, not a pass —
   `bridge.TxReady` checks connection and identity and does NOT require the
   selected VFO to have been decoded, so "ready to key" and "we know where we
   are" are different facts (`Service.dialState`, tracked vs known). With no CAT
   at all the check is inert, because that deployment cannot key. **The check that
   COUNTS is the one adjacent to PTT** (`Service.preKeyDialCheck`, installed via
   `TxController.SetPreKeyCheck`): a manual send is committed up to ~15 s before it
   keys, and the rig's state can change in that window, so a check made when the
   request was accepted proves nothing about the moment of keying. Request-time
   checks are a courtesy — a fast refusal for a send that is already doomed — and
   they go LAST in precedence, after armed / active / in-flight / readiness, or
   they mask those conflicts (codex P1+P2 on 0d180e59).
   Observable: no PTT, and `ErrTxDialUnknown` → 503 `rig_dial_unknown` on a start.

3. **The last published `ft8-qso` status always reflects the live session.** A
   terminal publish happens while the sequencer lock still excludes a replacement
   `Start*`; publishing after the unlock lets a newer session publish ACTIVE first
   and be overwritten by the stale idle, leaving the hub caching idle for a live
   session and the operator without controls. `finalrung.go` documents this; it
   has been found by review FOUR times — the fourth (`NextAnswerer`, codex P2 on
   a9e51f96) inherited the shape by copying `SetSkipIfSilent`, which had it too. A
   comment was demonstrably not enough, so the operator-command entry points now
   carry an executable guard: `publishatomicity_test.go` asserts the sequencer lock
   is HELD at publish time (TryLock succeeding means it was not), and `newTestSeq`
   installs that probe on EVERY test sequencer so the whole suite enforces it on any
   path it drives (`publishguard_test.go` collects violations by source location and
   reports them from TestMain). All 39 sites across the four sequencer files were
   converted on 2026-07-27; the pattern to never reintroduce is
   `s.mu.Unlock()` … `s.publish(...)`. Publishing under the lock is safe because the
   hub's publish takes its own mutex and sends NON-BLOCKING per subscriber (slow
   readers are evicted), and never re-enters the Sequencer. Enforcement is BOTH:
   the runtime probe (paths any test drives) and a source-level AST check
   (`TestSource_NoStatusPublishedAfterUnlock`) that is independent of coverage —
   necessary because 23 of the 39 sites are executed by no test, so the probe alone
   would not catch a regression in them. The AST check understands three forms —
   direct `s.publish`, a local ALIAS of it (this package really uses that, to hand
   the sink to a completion callback), and a call to any Sequencer method that
   publishes transitively — because a guard that knew only the first could be evaded
   by a rename (codex P2 on 603cd026). Methods that take `s.mu` themselves are
   exempt: they own their ordering, which is what `publishCurrent` is for (11 correct
   call sites depend on that exemption). The exemption is STRUCTURAL and now minimal: `s.mu.Lock()`
   must be the method's FIRST statement. Two looser rules each had a hole — "a Lock
   anywhere in the body" passed a conditional or closure-only lock (codex P2 on
   e3a7e605); "before any control-flow statement" passed a bare block hiding an early
   return, and a publish placed BEFORE the lock, since a call is not control flow
   (codex P2 on 30be7fb5). Both attempts enumerated what to REJECT and both
   enumerations were incomplete, so the rule now enumerates what to ACCEPT and the
   accepted set has one member. An unsound exemption is worse than none, because it
   reads as coverage.
   Observable: the final `ft8-qso` frame matches whether a session is running.

4. **No decode is displayed as workable, spotted, or acted on unless its capture
   window is attributable to ONE known frequency.** Every consumer downstream
   resolves a decode against the CURRENT dial, so a window spanning two
   frequencies produces stations rendered as workable where they are not, an
   answer keyed at nobody, and wrong spots published to PSK Reporter. A slot whose
   dial moved is suppressed like a TX slot — the empty `ft8-decode` still fires so
   the slot clock ticks.
   Observable: no decode rows, no spots, no sequencer advance from such a slot.

5. **A session ends only by: operator abandon, disarm, its completion policy, or
   a failed frequency confirmation — INCLUDING one that fires asynchronously.**
   The dial guard's full behaviour is specified as executable tests in
   **`dialguard_test.go`** — read that file before touching anything on this path.
   It was written BEFORE the implementation (2026-07-27), after ten rounds in which
   the rules were inferred one at a time from whatever the last review noticed; each
   inferred rule was wrong in a case the next review found. NO TOLERANCE is an
   operator decision, not an oversight: survivability depends on where the partner
   sits in the passband and which way you moved, so there is no clean edge to pick,
   and every threshold tried before the spec existed was wrong. A dial change now
   also DISARMS TX — including with no session running, because an arm is bound to a
   frequency just as a session is.
   The pre-key gate refuses inside the launched TX goroutine, so its caller cannot
   run the synchronous refuse-then-retire policy; `startTransmission` carries an
   `onDialRefusal` hook for exactly that, invoked strictly AFTER the completion
   callback. Without it a rung with no completion policy (most of them) suppressed
   PTT and left the exchange running (codex P1 on e0207074). Nothing else may retire one, and anything
   that does must be generation-scoped (`AbandonIfCurrent`) so it cannot end a
   session that replaced it. *An unconditional abandon driven by a stale capture
   slot killed a valid session started on the new dial in the meantime.*
   Observable: a session started after the triggering event survives it.
   **And the operator must be able to SEE it end and why** — the terminal
   `ft8-qso` frame carries `end_reason` (`dial_moved` | `dial_unknown`) whenever
   the operator did not cause the end. A safety stop nobody can see is
   indistinguishable from a hang: the first on-air read of a WORKING dial guard was
   "moving the dial does not stop TX", and it took a log dive to establish that it
   had (dogfood 2026-07-27). The terminal frame is the ONLY carrier of that
   reason, so nothing may republish over it: `publishCurrent` returns early when
   the session is idle, because `transmit()` returns as soon as its goroutine
   LAUNCHES and an async refusal can end the session before any post-transmit
   publish runs.

6. **Every completion that ENDS a session performs the SAME session-identity
   transition** — retire the generation, consume any staged teardown reason, clear
   the ladder's state, and publish the terminal status while the lock still
   excludes a replacement start. One primitive, `retireSessionLocked`; doing it by
   hand is how four paths drifted into three different versions, so that a stale
   callback could not be told from a live session and the dial guard's explanation
   vanished when a completion won the race. Call-CQ is deliberately NOT one of
   these — it RESUMES CQ rather than ending, a different transition.
   Observable: after any ending completion the generation has moved on, and the
   terminal frame carries whatever reason was staged.

7. **A control that stops RF is offered only where it can actually stop RF.**
   Skip-if-silent means "if they do not come back, end the session instead of
   repeating this rung" — a property of the RUNG, not of the session mode. The
   code treated it as a mode, so every answer/work mode accepted an arm and the
   status advertised `skip_armed`, while the skip check is only ever reached on
   PRE-FINAL rungs: type-4 work (whose sole rung IS the terminal RR73) and any
   ladder already on its final rung armed a stop that could never fire. A false
   promise on the TX path is worse than no feature — this is the button pressed
   when the operator wants the radio to stop. `rungSkippableLocked` enumerates the
   pre-final rungs POSITIVELY and defaults to false, so a new mode must claim
   skippability deliberately: failing safe costs an unavailable button, failing
   open costs a stop that never comes. Refusing is the operator's decision
   (2026-07-27) over inventing final-rung skip semantics — Abandon already ends a
   contact, and a second meaning on the rung that decides whether a QSO logs is
   not worth the ambiguity. The refusal is DISTINCT on the wire
   (`ft8_rung_not_skippable` vs `ft8_no_active_qso`): "nothing is running" and
   "this rung cannot be skipped" lead to different operator actions.
   Observable: an arm the sequencer refused is never reported as armed, and
   disarm is always accepted.

## Adding a sequencer mode — the coordinated-edit list

Nothing enforces this, and no abstraction should be invented to (the modes differ
in ways that matter — see "Build specific, not generic"). It is a checklist because
that is what it honestly is. Surfaced by the 2026-07-27 package review: seven modes
across four protocol families, each of which had to be taught to every one of these
sites, and skip validation was the one that got missed.

A new mode must be added to: `OnSlot` dispatch · `ActiveCallsign` ·
`rungSkippableLocked` (invariant 7 — omission means NOT skippable, which is the
safe default) · `abandonLocked` · `statusLocked` · `fireOpening` (or a deliberate
decision not to fire an opening, recorded at the start function — see
`StartCallCq`) · the completion snapshot (`completed*QsoLocked`) · and the
Service-side staging in `servicetx.go`.

Two structural rules the modes already obey, worth keeping:

- **An active mode always has exactly its corresponding exchange pointer**
  (`seqAnswering`↔`ex`, `seqWorking`↔`caller`, and so on). The nil checks scattered
  through the switch statements are defensive, not a supported state: a mode set
  without its pointer leaves `mode` and the published status disagreeing, and the
  operator sees a session the sequencer cannot advance.
- **A mode's family decides its final-rung policy, and the Group A/Group B split
  is not symmetric** — it turns on whether the PARTNER already holds a complete
  QSO (Group A: they rogered, so log whether or not our courtesy closer keys;
  Group B: we owe them the roger, so retry and log only on true on-air success).
  Standard answer / FD work / type-4 answer are Group A; standard work / Call-CQ /
  FD answer / type-4 work are Group B. Copying the wrong sibling's policy is an
  easy mistake that either loses a real QSO or logs one the partner never got.

**Corollary that cost three rounds:** if a behaviour test for one of these cannot
be written without inventing a fact the system does not carry, the SYSTEM is
missing that fact — do not settle for a threshold, an age check, or a heuristic in
its place. The occupancy quarantine, the freshness gate and the slot-distance test
were all attempts to infer "was this captured after the QSY?" from data that could
not answer it; the fix was to make the daemon stamp the frequency it measured on.

- **FT8 removed from the SM tree 2026-05-30 — now a separate stream.** The `internal/ft8` subsystem (codec + DSP + decode/ring/scheduler/service), the `research/` clean-room decoder tree (sandbox at strict 129/18 matched), the `captures/` corpus, the `cmd/ft8-*` developer tools, and `internal/audio/capture` (CGO miniaudio) were removed and preserved at tag **`ft8-snapshot-2026-05-30`** — the whole tree is recoverable (`git checkout ft8-snapshot-2026-05-30 -- <paths>`). FT8 work continues out-of-tree as **go-ft8** (`github.com/ColonelBlimp/go-ft8`), a WSJT-X/jt9-derivative library licensed **GPL-3.0-only**; SM links it rather than carrying the decoder. **Decode is wired via the thin `internal/ft8` wrapper** (int16/12 kHz/mono seam, fail-soft, logs each decode — a decode is NOT a QSO; no DB/upload-queue write, so the narrow-daemon-scope invariant holds by import graph). Architecture in ADR 0024. Shipped 2026-05-31: offline `DecodeFile` + `cmd/ft8-decode-file`, and the CGO-free live-pipeline core — int16 `sampleRing` + UTC slot `Scheduler` + `Service` (lifecycle/fail-soft) wiring scheduler → safego decode worker → `DecodeSlot`. **Live capture shipped 2026-06-02 (step 3):** `internal/audio/capture` re-added (miniaudio/malgo, behind `//go:build cgo` + a `!cgo` stub that leaves the subsystem idle on the static build), the int16 capture-source adapter (`source_cgo.go`/`source_nocgo.go`, float32→int16 seam), the exported `ft8.NewService` constructor, `cmd/smd` registration, the top-level `Ft8` config block (`ft8.enabled` / `ft8.device` / `ft8.enable_osd`), and a rewritten `cmd/ft8-capture-probe`. **`ft8.enable_osd` (default on, `*bool` nil→true)** turns on go-ft8's OSD-2/MRB fallback decode — a live A/B vs jt9 -d3 (2026-06-02) showed it recovers ~5 of 7 weak-signal misses for ~1.1–1.7× decode time (well inside the 15 s slot); without it go-ft8 was 100% precision / ~87% recall. FTdx10 live smoke validated the path: 4/4 UTC-aligned slots, 0 drops, 12–16 decodes/slot, decode 0.7–1.6 s. **Live FT8 requires a CGO build** — the static CGO-free default logs "capture unavailable; subsystem idle"; use any `CGO_ENABLED=1` build. **Capture is demand-driven (2026-06-08):** `ft8.enabled=true` no longer grabs the audio device at boot — the daemon acquires the input device when the first `/v1/ft8/events` SSE subscriber connects (operator opens the FT8 view) and releases it after a short linger (`captureLinger`, default 5 s, package var; absorbs reconnect/reload churn) when the last leaves. So an idle daemon holds no microphone until FT8 is actually in use. Sessions never overlap (acquire/release serialised under the Service mutex; release drains the prior scheduler+decoder before re-acquire); fail-soft on acquire failure (logged, subsystem idle); `Service.Subscribe` owns the refcount via an idempotent unsub so hub slow-reader eviction can't unbalance it. **Capture is ALSO gated on CAT being live (2026-06-21):** when the bridge is enabled, `ft8.Service.SetCatGate(bridge.RigConnected)` (wired in `cmd/smd`) means a subscriber alone no longer grabs the mic — the rig must be connected + identity-confirmed too, so the daemon never seizes the audio device with the rig off (e.g. the SPA reopening to FT8 on PC boot). `startCaptureLocked` defers when the gate is false; a reconcile loop (`catReconcileInterval`, 2 s) acquires once CAT comes up with a subscriber present and releases the mic if CAT drops mid-session. `RigConnected` also guards the **passive no-data disconnect** (rig silent but serial port still open — `activeClient`/`identityConfirmed` clear only on pipeline exit) via a consecutive-no-data-timeout strike count (`noDataStrikeLimit`=2): a quiet-but-alive rig recovers on the readLoop INIT+READ probe within one liveness cycle (1 strike → reset) and stays connected, while a genuinely dead rig accrues strikes and reads not-connected so the mic is released. With no bridge (no CAT), no gate is installed and capture stays purely demand-driven (audio-only setups unaffected). **`task deploy:local:dev` now defaults to the CGO PocketFFT build (2026-06-07)** so the dogfood deploy gets live FT8 out of the box; `SM_FFT=gonum task deploy:local:dev` forces the static CGO-free build. `ft8.device` is an integer device index from `ft8-capture-probe -list` (name-matching is a noted follow-up). Capture decision = malgo (multi-backend, fewest host assumptions); CGO contained via build tag (static CGO-free default = offline-only; CGO "live" build = capture + optional pocketfft). **FFT + CGO posture (updated 2026-06-07):** the **dev `task` builds are CGO-on by default** — `run`/`run:smd`/`build`/`build:smd` pin `CGO_ENABLED=1`, so live FT8 capture works without a deploy (gonum FFT, dynamically linked against core glibc; `task run:smd` is the no-RPM FT8 iteration loop — stop the systemd smd first to free the audio/serial port). The **shipped release is CGO + PocketFFT** (live FT8 out of the box), built via **`scripts/release.sh`** inside an **AlmaLinux 8 container** (glibc 2.28) so the glibc-dynamic binary runs on every RPM distro ≥ RHEL 8, including Fedora 43 (decided 2026-06-20, first external-op deploy; Option-1 RPM, tarball/DEB deferred until a non-RPM target appears). `scripts/release-rpm.sh` is the inner builder and **stays CGO-free by default** so a bare non-container run can't accidentally ship a glibc-pinned binary — the container passes `SM_FFT=pocketfft` + `SM_SKIP_SPA=1` (SPA pre-built on the host, so no Node in the container). `task build:smd:static` + CI's `CGO_ENABLED=0` releasability gate keep the static shape green as the no-FT8 / musl fallback. The opt-in **`SM_FFT=pocketfft`** build (`CGO_ENABLED=1 -tags pocketfft`, also `task build:smd:pocketfft` / `rpm:dev:pocketfft`) links go-ft8's CGO PocketFFT for ~2× faster decode (dynamically linked); `task deploy:local:dev` defaults to it. **PocketFFT is the preferred build for live FT8 *transmit*** (decode speed/recall): answering a CQ replies in the current opposite-parity slot on a **synchronised timebase** — symbol 0 at the slot's nominal +0.5s start, and if the decode lands later the controller drops the elapsed head and transmits the synchronised remainder (truncate-don't-shift, **ADR 0032**), so the receiver re-syncs on the Costas arrays (QEX §8 — a reply up to ~5s late, ~8s with AP-mycall, still decodes). The late-window guard (`txLateWindowSec`, ~4.5s into the slot) skips a rung only when too few symbols would survive truncation — relaxing the old full-waveform-fit constraint, so a slower gonum decode (~1.5s) now still transmits (truncated) instead of slipping a +30s cycle; PocketFFT's ~0.72s busy-slot decode just keeps the most symbols + best recall. **Answer-a-CQ first-rung immediate-fire (2026-06-12):** `Sequencer.StartQso` takes `now` and fires the opening call in the click's current TX slot (via `fireOpening`) when it's the opposite parity within `txLateWindowSec`, instead of waiting for the next qualifying `OnSlot` (which lands at a boundary, so a click just after one lost a full ~30s cycle); caller-side Call CQ is unchanged (its CQ goes out next slot by design, ≤~15s). Detail in `docs/ft8.md`. **A CGO build can't be fully *static* with glibc** — miniaudio `dlopen`s ALSA/Pulse/PipeWire at runtime and glibc forbids static `dlopen` — but it dynamically needs only core glibc (`libc`/`libm`/`libresolv`/`ld`) + whatever audio backend it `dlopen`s, so it's portable across glibc distros **provided it's built on an old-enough glibc** — the build-host glibc is the floor, which is why the release builds in the AlmaLinux 8 (glibc 2.28) container. Full static would mean dropping CGO audio for a pure-Go capture lib (a re-architecture, not pursued); the CGO-free static build remains the no-FT8 fallback for musl/Alpine. **`internal/audio`** (CGO-free WAV/FFT) was deliberately RETAINED as a self-contained package for future operator-facing recording/playback. **The former clean-room constraints (implement-from-QEX-paper-only, never WSJT-X source, prefer BSD/MIT FFT to keep the binary unencumbered) are RETIRED as of the 2026-05-31 relicensing — see the licensing note below.** Current licensing rules: ADR 0023 + `docs/licensing.md`. **FT8 transmit decided 2026-06-06 (ADR 0029, Accepted) — RX-safe layers building.** Daemon-owned, layered tones → GFSK audio → audio-output device → PTT → slot timing, reusing the ADR 0027 guaranteed-stop discipline (`tx_on`/`tx_off` stay controller-only, never `exposed`); **manual sequencing** (ADR 0031: the operator picks whom to work + arms TX; the rungs then auto-advance). **Automatic/unattended sequencing (daemon-initiated) is out of scope and unsupported — the QEX FT8 specification forbids automatic operation, and unattended operation is licence-restricted in many jurisdictions. SM is attended-only.** go-ft8's `EncodeStandardMessage` stops at the 79-symbol tone sequence (standard structured messages only); SM owns everything from tones onward, and the encode→modulate chain is offline round-trip-verifiable against the shipped decoder before any RF. **FT8 TX e1–e4 shipped 2026-06-10** (ADR 0029/0030/0031; full detail in `docs/ft8.md`): the daemon TX path + SPA Arm/Call-CQ (e1), the pure next-message resolver (e2), the manual sequencer (e3, answer-a-CQ: click a CQ → auto-advance CQ→73), and QSO logging (e4) — so **the answer-a-CQ flow is complete and logs**. The **"a decode is NOT a QSO" rule evolved to "a completed *exchange* is a QSO"** — a bare decode still writes nothing; a finished exchange does. The QSO is assembled (`ft8.BuildQso`) + submitted by an **injected sink in `cmd/smd`** (`ft8.Service.SetQsoLogger` → `qsoservice.Submit`), so `internal/ft8` does **NOT** import `qsoservice` — narrow-daemon-scope still holds by import graph (dependency injection gives the one-way direction without the import). **Completed FT8 QSOs surface to the SPA's shared session log (2026-06-12):** a one-shot **`ft8-logged` SSE event** (`internal/ft8`: `EventLogged` + `LoggedQso` payload + `NewLoggedQso` mapper; emitted by the e4 sink via `ft8.Service.PublishQsoLogged` after a successful submit, carrying the canonical **UUID** so email-out + edit work for FT8 rows; **not** replay-cached — re-delivery would dup a session row) → the SPA's `ft8-logged` listener adds it to `sessionQsosState` → a new **Session tab** in `Ft8Panel` (reuses `SessionPanel` + the extracted `SessionEmailControls.svelte`, so FT8 + Phone/CW share ONE session list + one email-out, and FT8 QSOs also appear in the Phone/CW Session tab). **FT8 country enrichment landed 2026-06-13:** the e4 sink (`cmd/smd`) calls `enrichOrchestrator.Enrich(theirCall)` before submit — exactly what the SPA does via `/v1/enrich/callsign` for Phone/CW — so the stored QSO carries the real country/DXCC/zones AND the cold-miss `country`-table cache row gets written (previously an FT8 QSO logged with country "Unknown" and created no country record). The enrich+submit runs in a one-shot `safego` goroutine off the FT8 decode loop (a cold-miss country lookup is network I/O; running it inline would stall slot decoding and drop slots) — best-effort, `Enrich` never errors so logging is never blocked, and the on-air grid stays authoritative over any cached locator. `LoggedQso` / the `ft8-logged` SSE now carry `country`, so the Session-tab Country column is populated for FT8 rows too. Band Activity also gained a **per-CQ beam-heading column** (short-path bearing from `my_gridsquare` to the CQ's grid via `lib/utils/bearing.ts` `pathInfo`, SPA-side, to aim the antenna before answering) + a sticky column header; the four FT8 lower tabs (Occupancy/Operate/Session/Settings) carry Heroicons. **Call-CQ caller-side sequencing shipped 2026-06-12 (ADR 0033, `auto_first`):** the Operate-tab Call CQ button starts a *sequenced* session — `POST /v1/ft8/cq/start` → the `Sequencer` caller mode (`internal/ft8/caller.go` `CallerExchange` + `caller_sequencer.go` `onSlotCalling`) calls CQ, auto-works the first answerer through RR73, logs via the e4 sink, and loops the pile-up until Abandon; selection mode is `ft8.tx.caller_answer_mode` (default `auto_first`). Still pending: the **`operator_pick` pile-up stack** (operator chooses the answerer from a queue) + its Settings toggle, and on-air validation. **Automatic/unattended sequencing (daemon-initiated) is explicitly NOT supported and out of scope — the QEX FT8 specification forbids automatic operation; it is not a roadmap item.** TX-frequency selection is a per-slot spectrum **occupancy/clear-offset picker** (data, not a rendered waterfall). Build order is RX-safe-first. **Shipped 2026-06-07:** step (a) per-slot occupancy detector + `ft8-occupancy`/`ft8-decode` SSE + SPA readout; step (b) `internal/ft8/modulate.go` GFSK modulator + offline round-trip; step (c) **`internal/audio/playback`** — a malgo S16/12 kHz/mono audio-OUTPUT device mirroring `internal/audio/capture` (`//go:build cgo`, pure `fillFrame`/`bytesAsInt16` helpers untagged + CGO-free-tested; `New→Init→Play→<done>→Stop/Close`, **caller owns the stop**), the `ft8.tx.device` config field, and `cmd/ft8-tx-probe` (list playback devices + encode-and-play a message, **audio only — no PTT, RF-safe**). Actual RF first enters at step (d). **Step (d) shipped 2026-06-09 (ADR 0030, first real RF):** the PTT seam is `ft8.TxKeyer` (KeyTx/UnkeyTx — same injection pattern as the capture source, so `internal/ft8` keys the rig without importing `internal/bridge`); the bridge implements it (`internal/bridge/ft8tx.go` — `KeyFt8Tx`/`UnkeyFt8Tx`/`TxReady`) by **reusing the tune controller's guaranteed-stop machinery** (hard auto-off `ft8TxMaxDuration` 18s, release-on-disconnect, identity gate, **single-flight shared with tune** so FT8-TX and a tune carrier can never key at once); `tx_on`/`tx_off` stay unexposed; TX power untouched (no tune clamp). `ft8.TxController` (`txcontroller.go`) orchestrates one slot: `EncodeToSlot` → wait for the UTC boundary (key a touch early) → KeyTx (optionally switching to `ft8.tx.mode`) → `Player.Play` → on done UnkeyTx, with PTT dropped on **every** return path (deferred unconditional unkey) + the bridge auto-off backstop. First RF only from the gated **`cmd/ft8-tx-probe -key`** (stands up its own bridge connection — stop the daemon first; no SPA can transmit yet). **Step (e)'s TX-offset picker strip shipped 2026-06-09 (SPA-only, RX-safe):** `Ft8OccupancyStrip.svelte` lays the passband out horizontally (busy bands shaded, daemon-vetted clear offsets as clickable green markers, ★ = top pick); clicking a marker or a **Clear Offsets** chip sets `ft8State.selectedOffset` (the selected chip shows a darker-green border) — **now consumed** by the answer-a-CQ TX flow (e3/e4). The top-row's temporary "TX Frequency" occupied-Hz validation column was replaced by an **Rx Frequency** pane (WSJT-X-style: decodes filtered to the worked station while a QSO is active, else the selected offset ±tol when idle), and the **Operate tab** (renamed from Ladder) renders the **message ladder** — now LIVE + role-aware (the answer-a-CQ exchange and, per ADR 0033, the caller-side Call-CQ session; `<DX>`/`<GRID>`/`<RST>` placeholders, current-slot row highlighted), no longer presentational; full UI detail in `docs/ft8.md`. A second **Spectrum** occupancy view (continuous bar — signals at true positions, **click-anywhere** continuous TX-offset pick, graded clear/near/sharing instead of binary red) is switchable with the channelised strip (2026-06-26, `Ft8OccupancySpectrum.svelte`, `ft8State.occupancyView`); daemon-side no-overlap snap + a rendered scrolling waterfall are still future work. **Band Activity CQ enrichment shipped 2026-06-09 (SPA-only, RX display):** CQ decode lines get a country flag + a worked-before tint (WSJT-X-style — un-worked-on-this-band = attention colour, dupe = muted, colours operator-configurable via daemon `ft8.display` config), reusing existing endpoints (`/v1/enrich/callsign` flag, `/v1/contest-dupe` worked-on-band+mode); progressive + fail-soft, cached per `call|band` for the session, CQ-only (one unambiguous call). No daemon change — `internal/ft8` stays narrow. Lives in `frontend/logging/src/lib/states/ft8Enrich.svelte.ts` + `utils/ft8Message.ts` (`parseCqCall`) + `utils/flag.ts` + `api/contest-dupe.ts`. **ARRL Field Day over FT8 shipped + on-air validated 2026-06-28 (during ARRL FD; ADR 0037), BOTH directions, attended-only:** **answer a `CQ FD`** (search & pounce — `seqAnsweringFd`/`FdExchange`, SPA `isCqFd`) AND **work a caller in FD** (you're the sought-after DX, the dominant path — `seqWorkingFd`/`FdWorkExchange`, SPA `parseDirectedToMeFd`). SM does **NOT** call `CQ FD` (no FD run side). FD is a distinct FT8 message type (class+section, not grid/report) — go-ft8 v0.4.0's packer encodes/decodes it so the modulate seam is unchanged (offline `TestFieldDay_RoundTrip` gate). Operator FD identity = config `ft8.field_day.{class,section}` (class strict in `types`; section via `goft8.ValidARRLFieldDaySection` in `internal/config` — `types` stays stdlib-only); `/v1/ft8/qso/{start,work}` take `mode:"fd"`; QSO logs ADIF `CLASS`/`ARRL_SECT`/`CONTEST_ID=ARRL-FD` via `BuildQso`. The two FD slot handlers are kept ISOLATED from the standard path (parallel, not branched) so standard FT8 is untouched. Pending: FD-aware Operate ladder render, FD pile-up Ctrl-click, config-SPA section dropdown. Validated calls: K7T/W6A (answer), K7IOC (work). The single FT8 capture point is `docs/ft8.md`.
