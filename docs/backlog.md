# Backlog — the definitive ranked worklist

**Single purpose:** the one authoritative, priority-ordered list of every
triaged, not-yet-done piece of cross-cutting work. If it's real work and it
isn't being done *right now*, it lives here — ranked. This is where "what's
next, and in what order" is answered.

**How it relates to the other docs** (full map: `docs/README.md`):
- Raw, un-triaged notes jotted mid-operation live in `docs/dogfood-inbox.md`
  and **graduate here** once triaged (or are struck there as non-issues).
- The 1–3 items in flight **right now** live in `docs/session-handoff.md`'s
  active-cycle list, pulled from the top of this file — that doc does NOT
  re-rank the queue; **this file owns the ranking.**
- FT8-internal implementation mechanics live in `docs/ft8.md`; only
  *cross-cutting* FT8 work lands here.
- Shipped / resolved / ruled-out items are **moved** to `docs/backlog-archive.md`
  (NOT read at session start) so this file stays lean. Open the archive only when
  you need an item's history.

**Conventions:**
- The **Worklist index** below is the ranked table of contents — one terse line
  per item. The **detail** for each lives in its full entry further down; lead
  each entry with the surface (file/subsystem) so its title is greppable.
- When an item ships, **move its detail block to `backlog-archive.md`** and drop
  its index line — do NOT strike it in place. A `~~struck~~` top-level item left
  in this file is a bug: relocate it. (Struck *sub-bullets* inside a still-open
  item are fine — they show which sub-steps of live work are done.)
- Tiers: **P0** correctness/safety · **P1** finish in-flight · **P2** next
  features (by workstream) · **P3** deferred/large/needs-trigger · **Designed
  workstreams** (built on go-ahead) · **Parked** (not work).

## Worklist index (ranked) — the definitive "what's next"

> **▶ Active cycle (set 2026-07-04): _Get to the next shippable state for 7Q8AC._**
> The goal is a release the external operator (7Q8AC, Malawi, offline-first) can run;
> "stabilise & finish in-flight" is the means. Clear P0 + the P1 finish/validate items
> — they ARE the ship gate — before opening any new P2 workstream. Theming (P2 ·
> _UI cohesion_) is deliberately **not now**. The live active-cycle checklist is in
> `docs/session-handoff.md`; this index is the full ranked queue.

**P0 — correctness / safety (do first)**
- _None open — the TX-safety batches shipped and were swept to `backlog-archive.md` (2026-07-24). One code-complete item still needs ON-AIR validation: the reduced type-4 ladder (P2). The tx-alarm self-clear validated on air 2026-08-08 (closed entry in P1)._

**P1 — finish in-flight / validate (small; closes open arcs)**
- ~~**VALIDATE (on-air): tx-alarm self-clear.**~~ **VALIDATED on air 2026-08-08 — CLOSED (operator's word, same day).** The fault fired naturally during the 200-QSO 17m run: at 15:31:46 a closing RR73's unkey came back unconfirmed (tx-status `2`, uncertain) → TX ALARM raised (`tx_unconfirmed`) + ft8 mode restore correctly refused → within the same second the re-probe confirmed idle (`tx state confirmed idle {how: tx-status 0}`) and `tx alarm cleared (transmitter confirmed idle)`; the QSO (BD8TE) logged normally and the run picked up its next caller 14 s later. No operator action, no stuck RF, rich log trail — exactly the self-clear-within-a-probe-interval criterion, met by a natural occurrence rather than a provoked one. _(Struck rather than deleted so a stale copy of this list can't resurrect the item.)_
- Behavioural retest of shipped daemon changes on the dogfood daemon (session 192/193 batch). _Sweep note (2026-07-18): the batch = DXCC numeric-match (`HasQsoForDxcc` on `json_extract('$.dxcc')`), the `ft8_decode_log` config toggle, and the `tx_parity` CQ-sequencer path — shipped 2026-06-25/26. ~3.5 weeks of daily FT8 dogfooding since has plausibly exercised DXCC-match (new-entity markers) and tx-parity implicitly; decode-log only matters if enabled. Operator's call to close._
- **`internal/pskreporter/service.go:141` — stable IPFIX observation-domain ID across daemon restarts. HIGH PRIORITY (operator-flagged 2026-08-11).** Today `s.id = rand.Uint32()` is regenerated on every `Start`, so each daemon boot presents a NEW observation-domain identifier (and resets the IPFIX sequence to 0 + re-sends templates for the first 3 packets). Over the current dogfood log that is **~150 restarts = ~150 distinct "sessions"** for one station at the PSK Reporter collector. Surfaced by the 2026-08-11 PSK Reporter investigation (7Q5MLV, public IP `105.234.176.191`): the collector's maintainer reported "a new UDP source port every transmission" plus service disruption from an odd-IP reporter starting Fri/Sat. The source-port hopping is **Airtel Malawi CGNAT** — measured sub-30 s UDP idle timeout (STUN test, one fixed local port 49068, external port changed after every idle gap down to 30 s), carrier-side and unfixable at our end (raised with the maintainer as a collector-side correlation matter: key on the payload identifier, not source IP:port). **But the restart-churn half IS ours to fix.** _Change:_ make the id stable — derive it deterministically from the receiver callsign (hash → uint32), or persist a random id in the data dir — so restarts stop looking like new sessions. Still spec-legal (WSJT-X uses a random per-run id; a stable per-station id is also permitted). _Acceptance (operator-observable):_ the observation-domain ID field in the emitted IPFIX datagram (`ipfix.go:206`, `encodeDatagram`'s `id`) is identical across daemon restarts for the same receiver callsign, differs for a different callsign, and is distinguishable from today's change-every-boot behaviour — assert on the wire, not internals. TDD, RED first. _OPEN DECISION (operator's call, settle before building):_ derive-from-callsign (stateless, simplest) vs persist-random (survives a callsign change without colliding prior history). Does NOT touch the live TX path. _(Context: this session's draft reply to the maintainer.)_

**P2 — next features (open one workstream per active focus)**
- **RECONCILIATION 2026-08-11 — the five SHIP-GATE logging-audit entries below are BADLY STALE; read this before picking any of them.** A full per-package cross-check of every finding against current code ran 2026-08-11: **32 of 67 findings were already shipped** at that point (with pinning tests), including nearly every "top item" the entries still call open (api A7, qsoservice Q1/Q7, ft8 1-8/11-14, forwarding F1/F4/F6/F9, bridge B1/B2/B3). **Current open sets (each `reviews/*-logging-gaps.md` file now carries a per-file status header — trust it over the mega-bullets below):** _bridge_ — **10/14 DONE** (B1-B4, B6-B11; commits `568742ed`/`df1db6ab`/`1408edb1`, 2026-08-11), OPEN **B5** (CIV mid-batch), **B13** (tune-restore encode drops), **B14** (freq/power parse) — cheap, need test scaffolding — plus **B12** (needs-decision). _api_ — **9/12 DONE** (was 7; **A11 both halves + A6 FIXED 2026-08-12, working tree**: `mergeForwarders` preserves an undecodable stored-creds blob instead of dropping it; `credentialKeysSet`/`noteForwarderCredCorruption` warn once per corrupt forwarder at GET; `recoverPanic` flags a committed/truncated response and skips the doomed envelope — all TDD + reversion-proved), OPEN **A5/A10/A12 (needs-decision)**. _qsoservice_ — **Q4/Q5/Q6 FIXED 2026-08-12 (working tree)**: stamp-sync re-enqueue promoted to Info; `forwarded_to` destinations on QSO stored/soft-deleted; all 14 `_ = tx.Rollback()` sites now log a Warn on rollback failure via `rollbackTx` — all TDD + reversion-proved. OPEN **Q8/Q9/Q10** (all clean cheap). _ft8_ — 12/14 DONE, OPEN #9, #10 (cheap). _forwarding_ — 4/17 DONE, OPEN F5/F7/F8/F15/F16 + F3-age (cheap) + F2/F10/F11/F12/F13/F14/F17 (needs-decision). One cross-cutting DECISION gates several: "what does a keyed transmission record" (api A10 / bridge B12 / ft8 #6 already shipped its half) · A5/A12 log-levels · a `forwarding.Result` disposition field (F11/F14). **Do NOT re-audit from scratch — build from these open sets.**
- **CLOUD logging audit — the SIXTH package area, NEW 2026-08-11 ([`reviews/cloud-logging-gaps.md`](reviews/cloud-logging-gaps.md), C1–C6).** `cmd/smcloud` + `internal/cloud` were never part of the 2026-08-01 five-package sweep; a codex/cloud logging review surfaced six gaps (transcribed from `.codex-reviews/20260811-001.txt`, file:line spot-checked). **Two are High false-success-on-the-wire bugs:** **C1** — evidence rejects/tombstones/digest-conflicts/missing-profiles return HTTP 200 with NO server log (`evidence.go:44` logs only batch failures) and the client quarantines silently (`sync.go:607`); **C2** — an export is logged success (`server.go:476`) BEFORE the deferred gzip `Close()` flush (`gzip.go:37`), so a truncated response coexists with a success record. C3 startup/migration audit trail · C4 no request-ID correlation (LAN deploys have no access log) · C5 missing tenant context on auth failures · C6 stdlib HTTP diags bypass slog. F11 (forwarding) is the client-side cross-ref, not a new item. Verify file:line before building. Fold in + delete the file once shipped.
- **codex `1408edb1` review (2 × P2) — FIXED in the working tree 2026-08-11, awaiting operator commit.** The clean-room review of the B4/B6/B7 commit found both delivered half their intent: **B4** logged only the no-waiter ACK drop, not the buffer-full/duplicate drop (`command.go` default branch, now a distinct Debug line); **B6** re-logged "subscriber count changed" on every idempotent unsubscribe re-call at an unchanged count (`publishClientCount` now edge-triggered, `service.go`). Full TDD (2 new tests in `diaglog_test.go`, reversion-proved); `-race`+vet+gofmt green. `.codex-reviews/1408edb148bd.md` stays until committed.
- **SHIP GATE — log coverage: four things that happen and leave NO durable trace (operator-directed 2026-07-31: "we do need to plug these before shipping anything").** One entry, not four, because they are one defect shape: an event occurs, the operator or a future admin can never learn it did. **Pre-ship because shipping to 7Q8AC means the person diagnosing a fault is not the operator, and none of the four is recoverable retrospectively.** All four verified against the code + the live `smd.log` on 2026-07-31 (14.36 MB, 81,978 lines, 2026-07-16→07-31). **(a) Config saves are not logged.** `handler_config.go` logs only validation WARNINGS (`:670`, `:754`); a successful change — `ft8_max_repeats`, forwarder enable/disable, rig selection — writes nothing. Combined with the daemon rewriting `config.json` at startup, "when did this setting change, and to what?" is unanswerable today. ~~**(b) QSO deletes write no log line.** `qsoservice.Delete` (`delete.go:37`) has no logger call.~~ **(b) IS FALSE — STRUCK 2026-08-01, do not build it.** `qsoservice.Delete` DOES log: `delete.go:85-90`, Info, with `qso_id` + `call` + `qso_date` + `time_on`. `git log -L` puts that line in the tree since **`d516d816`, 2026-05-17 — two and a half months BEFORE this entry was written** — and it was enhanced since (the original carried only `qso_id`). So the claim was wrong when written, not stale from a later fix; `:37` is the function's signature line, consistent with it having been read from the signature rather than the body. Third instance of the verify-backlog-before-building failure. The delete path also appends its `qso_history` row (ADR 0016), so provenance AND the log line both exist. _(Found by the `internal/qsoservice` logging review — `reviews/qsoservice-logging-gaps.md`, Correction 1.)_ **(c) The whole notification category has no daemon record.** Toasts are client-side (`lib/ui/toasts.svelte.ts`, ~50 call sites); several have no daemon counterpart at all (FT8 gate refusals, client-side ADIF export failures). Close the tab and they never existed. This is the substrate the notification-history rail needs and there is nothing behind it. ~~**(d) Log lines carry no build version.**~~ **(d) SHIPPED 2026-08-01 (Diff C).** Every record now carries `version` from the base logger context — including the pre-logger `logStartupFailure` writer, which hand-writes its JSON and bypasses `logging.Service` entirely (it is the message most likely to be read on a failed fresh deploy). **`internal/buildinfo.Version` is the single carrier: `cmd/smd`'s `main.Version` was REMOVED, not aliased**, and all five smd Taskfile lines + `dev-rpm.sh` + `release-rpm.sh` retargeted (`cmd/smcloud` keeps its own, out of scope). **TRAP for anyone copying an old build command: `-X` on a symbol that no longer exists EXITS 0 AND STAMPS NOTHING**, so a stale `-X main.Version=` silently yields a `dev` build. Decision taken (operator, 2026-08-01): **full version string**, decided together with the forwarding F1 restructure that made it affordable — **+6% vs today** (29.4 MiB at 30 days) instead of **+23%** (34.0 MiB) had it shipped alone, on a corrected 15.51-day divisor. Note the honest limit: an unstamped build logs `version:"dev"`, so the FIELD is always present but "dev" says only that the build was unstamped, not which build wrote the record. The original finding, for the record: `"version"` appeared ONCE per start on `smd starting`, so attribution meant replaying the file forward from a marker — broken by rotation (a `(pre-first-startup)` bucket already exists in the current log) and lost entirely by any `grep`, across **58 DISTINCT BUILDS in 15.1 days**. **Do NOT "fix" this by mirroring `smd.log` into a table** — 99.5% of lines are `info` (81,557 info / 454 warn / 57 error) and three message types are 65% of bytes (`forwarding: success` 24%, `http request` 23%, `forwarding: submit` 18%); see the logging ADR line for the selective operator-facing event store instead.
- **SHIP GATE — `internal/ft8` logging gaps (14 findings, own file: [`reviews/ft8-logging-gaps.md`](reviews/ft8-logging-gaps.md)).** The same audit as the entry above, run against `internal/ft8` on 2026-08-01 (review only, no code changed). Same defect shape — an event occurs and the operator can never learn it did — so it is pre-ship for the same reason: the person diagnosing a fault at 7Q8AC is not the operator. Headline: **a hub slow-reader eviction (`hub.go:72`) silently ends a live QSO** via eviction → unsub → linger → `disarmTx` → PTT down + abandon, and is indistinguishable from the operator closing the browser; `ft8 seq: session abandoned` carries **no callsign and a blank reason** on all four common paths; `disarmTx` logs one message for five causes including the only enforced presence check; **dial-moved slots suppress decode + sequencer + occupancy silently** (same class as the 2026-07-27 "moving the dial does not stop TX" log dive, whose session-end half got `end_reason` and whose per-slot half got nothing); the stalled-caller cool-off is unlogged both when set and when it skips, while the unencodable-caller skip eleven lines away IS logged; `qsolog.go` has zero log calls and four silent degradations on data forwarded to QRZ/ClubLog. **ONE OPERATOR DECISION, do not pre-empt it** (finding 6): a successful transmission logs nothing at the Service layer — 6 log calls in 1415 lines of `servicetx.go` — which is exactly the 2026-07-28 incident shape (`idleinhibit.go:8`: kept keying, decode log kept saying "Transmitting", no audio reached the rig for 24 min / 48 CQ calls); what counts as evidence a transmission actually happened is the operator's call. **Second pass added 11-14 the same day** (verified before filing): late-slot transmit deferrals are silent at **8 sites** across every sequencer family plus `fireOpening`, folding two different causes (`dt < 0` = clock/slot fault vs `dt > txLateWindowSec` = slow decode) into one silent branch, with a sibling `lastTxSlot` dedup skip silent at 7 more — so three distinct reasons a slot passes without RF all look identical, and `txLateWindowSec = 4.5` is unmeasurable on a live station · decode-log line drops are counted but only WARNED at `Close()`, so a long capture loses lines for hours unnoticed · the deferred final flush/close (and every per-line write) discard their errors, three lines below a normal-path flush that logs its own · TX teardown discards `dev.Stop()`/`dev.Close()` then logs `disarmed` unconditionally, and `ArmTx(false)` returns nil so the HTTP 202 agrees. The file also records **what is NOT a gap** (the ~30 TX refusal sentinels are already on the access line via `middleware.go:308`; the four sequencer files are well covered; per-decode detail is deliberately Debug) — read that section before re-auditing, and note it carries one **struck + corrected** bullet where finding 12 overturned a first-pass conclusion about `decodelog.go`. Fold into this backlog and delete the file once shipped.
- **SHIP GATE — `internal/bridge` logging gaps (14 findings, own file: [`reviews/bridge-logging-gaps.md`](reviews/bridge-logging-gaps.md)).** Same audit as the two entries above, run against `internal/bridge` on 2026-08-01 (6,294 non-test lines, **66 log sites** — review only, no code changed). **The headline is a negative result worth keeping:** the TX-safety files are WELL logged (28 sites across `txconfirm`/`ft8tx`/`tune`/`txrecheck` — they got attention after real on-air incidents), and the gaps cluster in code that decides NOT to act. **B1 is the one that matters:** `armDriveWatch` has two silent early returns leaving a transmission unmonitored, and the `!meterSeenSinceTx` branch (`drivealarm.go:120`) is reported **nowhere** — not the SSE wire, not the log — so the SPA keeps showing whatever the last meter selection implied while the detector has declined to arm. That is the exact shape `driveMonitorFor`'s own comment (`:87-90`) calls "the worst shape available", and it runs against the standing operator instruction quoted at `events.go:118`: *"being silently unprotected is worse than being told"* — which was answered on the SSE wire only, so `smd.log` still cannot say whether drive monitoring ran for a given transmission (also feeds ADR 0060, which is parked on alarm data). **B2 is the SAME defect as the ft8 entry's finding 1** (`hub.go:157`, 260 lines / 0 log calls) — fix both hubs in one commit; note the bridge case is milder, a standing tx-alarm survives eviction via the `lastTxAlarm` cache. Also: a failed key whose defensive `tx_off` SUCCEEDS logs `tx state confirmed idle` — the same line a normal transmission produces (B3); and the accepted CI-V late-ACK race (`command.go:305`, "narrow and bounded", accepted-not-fixed) discards at `:325` the one observable that would show whether it fires (B4). **Second pass added B9-B14 the same day** (verified before filing; a 7th item was dropped as a dup). **B10 is now the top item and outranks B1:** the identity paths log NOTHING — and `readLoop:825`'s mismatch returns `exitPermanent`, which `runSupervisor:552` handles with a bare `return`, so **the bridge dies for the process lifetime with not one line in `smd.log`**. Every other `exitPermanent` site logs first (`:182`, `:191`, `:217`, `:227`); this is the only silent one, and it fires on a wrong `bridge.cat.driver` — a first-run mistake, on a deployment where the person reading the log is not the operator. The unrecognised-ID case (`:814`) blocks every write path indefinitely while state display keeps working, also unlogged. Also: the no-data re-probe write failure (`:675`) discards `werr` while the sibling branch eight lines below logs its cause **and explains why it must** ("the SPA renders `serial_port_error` from the code, ignoring `details.error`") — same code published, one diagnosable, one not · passive liveness loss/recovery has no durable transition, so the bridge-side cause of an FT8 mic release cannot be joined to its logged effect · **normal tune key/unkey is silent** while every abnormal tune path logs — a real RF carrier into an amp with no trace, the same shape as the ft8 file's finding 6, and the two should be settled with ONE decision about what a keyed transmission records · tune/FT8 restore ENCODE failures are dropped before any write so the existing write-failure logs never fire, and tune's independent power+mode appends make a **partial restore** (left on tune power or in RTTY) report success · malformed freq/power parse errors are discarded, which leaves `CurrentDialMHz` **stale rather than unknown** — worse, because ft8's pre-key gate compares against the pinned value and a stale reading matches. The file records **what is NOT a gap** — `SendCommands`' five refusals have exactly ONE caller and land on the access log; the supervisor's SSE dedup is correct because the log line is unconditional and precedes the deduped publish; `events.go` is declarations only — read that section before re-auditing, and note it carries one **struck + corrected** bullet where B10 overturned a first-pass "pipeline.go is best-covered / covers identity handling" claim (call-site count is not coverage). Fold into this backlog and delete the file once shipped.
- **SHIP GATE — `internal/api` logging gaps (12 findings, **2 SHIPPED 2026-08-01**, own file: [`reviews/api-logging-gaps.md`](reviews/api-logging-gaps.md)).** ~~A1~~ and ~~A9~~ are **DONE** (`b1c50913` tests + `0265f04a` implementation) — do not re-pick them; they are marked ✅ in the file with the shipped detail. **10 remain**, A7 still the top item. Third of the three logging audits (2026-08-01; 5,639 non-test lines, 27 log sites — review only, no code changed). **FEWEST findings of the three, and that is the result:** this package has a structurally correct access log (`logRequests` outermost at `server.go:332`, carrying `code`/`error`/`op` on every failure), which already covers the refusal surfaces that needed individual findings in ft8 and bridge — so `handler_ft8_tx.go`, `handler_rig_command.go`, `handler_qso.go` etc. need nothing despite having zero log calls. The gaps are only what happens OUTSIDE a request and what the access log deliberately omits. **A1:** `HTTP server listening` (`server.go:438`) has no closing bracket — `StopAccepting` and `Shutdown` log nothing, so a shutdown shows only a burst of 503 `shutting_down` lines plus one giant-duration line per SSE stream, with no line saying when draining began; a clean exit is indistinguishable from a crash, which matters at 58 builds/15 days with `smd` deliberately not auto-start. **A2:** the two endpoints that act on OUTBOUND data — forwarder-upload enqueue and smcloud reconcile — return their result summary to the browser and log nothing, so "the operator pressed it" is durable and "it enqueued 47 rows to ClubLog" is not. **A3:** CSRF rejections (the API's only security control, guarding the whole mutating surface) use static messages, so the offending `Host`/`Origin` — the entire diagnostic content — is discarded; a rebinding attempt, a stale bookmark, and a `0.0.0.0`-bound LAN deployment refusing legitimate traffic are one identical line. Also: config logging is failure-only so **every config line in `smd.log` is a rejection** (A4 — this IS ship-gate item (a), cross-referenced not re-filed), and SSE subscribers are invisible at the default level until they disconnect (A5 — resolve with the two hub-eviction fixes). **The NOT-gaps section is longer than the findings and is the load-bearing half:** it records that `r.URL.Path` (not `RawQuery`) and never-the-body are DELIBERATE per the 2026-07-25 credential leaks, so any fix must log derived facts only; that path-carried subjects are already attributed (partially mitigating ship-gate item (b) at this layer); and that the three 503 causes already have distinct codes. **Second pass added A7-A12 the same day** (verified before filing; three folded into A2/A3/A4 rather than re-filed). **A7 is now the top item across all three logging files:** `currentStationIdentity` (`handler_ft8_qso.go:30-43`) discards every non-not-found DB error and returns an empty callsign — failing closed is CORRECT and deliberate, but three FT8 handlers then emit **400 `no_station_callsign`**, so a failing database is reported to the operator as unset configuration. The two demand OPPOSITE actions: it sends them to Settings to fix a field that is already right while the real fault goes uninvestigated, and being a 400 it never reaches `writeServerError`, the one mechanism that would have logged the cause. **A8:** a config PUT can return **HTTP 500 with the change already committed** (`handler_config.go:778` — `buildConfigResponse` fails after `s.cfg.Update` at `:704`), so the record that exists is actively WRONG, not merely missing; a commit line right after the update resolves A8 and A4 together. Also: `http.Server.ErrorLog` is unset (`server.go:331`) so Go's accept/transport diagnostics and any panic escaping `recoverPanic` go to stderr/journal instead of `smd.log` — a split along the worst line, since `smd.log` is the file an admin gets (A9) · successful rig commands log no ops, so a QSY, mode change and power change are one identical 202 — **needs a VOLUME decision first, key-repeat frequency stepping would swamp the log, do not add a bare Info line** (A10, settle with bridge B12) · malformed stored forwarder credentials report as "none set" on GET and, worse, the merge at `:1234` rebuilds from an empty base and **silently drops them — a data-loss bug alongside the logging gap** (A11) · health-check Ping errors lose the cause, log transitions only or a probe loop floods the log (A12). **One second-pass item CORRECTED a recommendation in the first pass:** A3 originally advised logging the rejected `Origin` as "a hostname, not a credential" — wrong, since a non-browser client can send `https://user:pass@host`; the amendment requires parsed `u.Scheme`/`u.Host`, which `originAllowed` already computes. Implement the amendment, not the original wording. Fold into this backlog and delete the file once shipped.
- **SHIP GATE — `internal/qsoservice` logging gaps (10 findings + 2 CORRECTIONS, own file: [`reviews/qsoservice-logging-gaps.md`](reviews/qsoservice-logging-gaps.md)).** Fourth logging audit (2026-08-01; 2,018 non-test lines, 8 log sites — review only, no code changed). **Its most valuable output is a correction, not a finding: it proved SHIP GATE item (b) above FALSE** (see the strike there) — and separately corrected a claim I had accepted into the `internal/api` review. **The CRUD path here is the best-logged in the daemon** and should be the template for the other packages' missing success-path lines: submit/update/delete each emit a full Info line after commit (qso_id, call, date, time, +freq/band/mode). The gaps are the paths either side. **Q1:** `restore.go` is **89 lines with zero log calls** — the only silent CRUD verb, and it is the ADR 0040 S5 **disaster-recovery** path (the workstream that exists because the dogfood DB was lost ~2026-07-16); an idempotent re-run and a real recovery produce identical silence, which is the exact question during a recovery. **Q2:** the enqueue log carries 2 of 5 outcome fields and omits **`SkippedNoHistory`** — the ClubLog `realtime.php` refusal SM confirmed back to them 2026-07-19 and whose violation blocks the API key; so SM withholds QSOs to honour a written third-party commitment and keeps no evidence it did (fix with `api-logging-gaps.md` A2 or the count still exists only in the HTTP response). **Q3:** a `duplicate` submit is silent while a `stored` one logs — backwards, since the operator saw the rejection and will be the one asking; also means the data that would inform the FT8 duplicate-detection backlog item is not being collected. Also: `stamp_sync.go:69` logs at **Debug**, so the built fix for the measured smcloud bandwidth churn (~650 KB per drifted hour on a Malawi link) is unobservable in production and its failure mode is silent non-firing (Q4) · a stored QSO's forwarder fan-out is unrecorded and two silent branches decide it, so "why did this QSO never reach ClubLog?" cannot distinguish never-queued from queued-and-failed from pending (Q5) · 14 `_ = tx.Rollback()` discards in the package that owns **one-fails-all-fail**, the only sites that could observe the invariant being violated (Q6). **Second pass added Q7-Q10 the same day** (verified before filing; two folded into Q2/Q5) and **overturned one of my own corrections** — the enqueue zero-work case really IS silent (`:181`/`:292` return before the tx AND before the log), so an all-refused ClubLog selection writes NOTHING, not a partial line; my rebuttal was true about the post-commit path and irrelevant because commit never happens. **Q7:** `Restore`'s existence probe (`restore.go:60`) handles only `err == nil`, so a real DB fault is treated as a genuine miss and it inserts anyway — if the insert succeeds the error vanishes entirely, and the idempotence guarantee the probe exists to provide is silently not in force. **This is the same idiom as `api-logging-gaps.md` A7 in a second package** (`if err == nil` / `Is(err, ErrNotFound)` with an implicit else swallowing every other DB error) — **grep for a third rather than fixing two in isolation**; note A7 fails closed (safe) while Q7 fails OPEN (writes a row). **Q8:** the import batch fallback (`submit_batch.go:180`,`:188`) discards the very error that triggered it, so an import silently degrading to per-record inserts on every batch is indistinguishable from a clean one. Also: a **forced** dedupe bypass is unrecorded (`submit.go:373` skips the check entirely, `:482` carries no `forced` flag) and the access log deliberately omits query params, so a deliberate duplicate and a first-time contact are byte-identical — with Q3 that means NEITHER side of the duplicate question is being recorded while the FT8 duplicate-detection item stays open (Q9) · import/restore have no durable completion summary, terminal totals go to stdout only, so `smd.log` cannot tell a completed bulk run from an interrupted one — **aggregate at the command/batch boundary, not per QSO** (Q10, refines Q1). NOT-gaps records that validation/pre-commit errors correctly have no lines (they reach `writeServerError`/the access log) and that QSO payloads and `before_image` blobs must never be logged — `qso_history` already holds the pre-image inside the DB, `smd.log` is `0644`. Fold into this backlog and delete the file once shipped.
- **SHIP GATE — `internal/forwarding` logging gaps (17 findings, **F1 (BOTH halves), F4 and F6 SHIPPED 2026-08-01** — the attempt-record rewrite (`f31738bc`/`191ac370`) and `qso_upload.origin` (`59542a8a`/`ed0f0657`); **F17 newly filed**; **14 of 17 remain**, own file: [`reviews/forwarding-logging-gaps.md`](reviews/forwarding-logging-gaps.md)).** Fifth and last logging audit (2026-08-01; 3,558 non-test lines, 21 log sites — review only, no code changed). **Fewest findings and the problem is INVERTED:** the other four packages were too quiet; this one is **43.3% of `smd.log` by bytes** from two message types. **Re-measured on the LIVE log today** (84,151 lines / 14.04 MB, `SM_WORKING_DIR`) — independently reproduces the 2026-07-31 figures: `forwarding: success` 23.5% (17,143 lines, avg 202 B) + `forwarding: submit` 18.0% (17,433, avg 151 B) = **34,576 lines, 41% of every line the daemon has written**, and `success` repeats forwarder/qso_id/action/call from `submit` adding only `upstream_id`. **NB a stale copy at `~/pCloudDrive/.../smd.log` (2026-07-03) gives 2.0% not 23.5% — measure the `SM_WORKING_DIR` file.** **F1: neither line records WHY the row was queued** — live logging, manual backfill, stamp-sync re-enqueue and reconcile repair are identical. Worked example, and its resolution IS the evidence: smcloud shows 15,271 submits vs qrz 1,263 / clublog 903 (12.8 per QSO), which reads like a re-enqueue loop but is **NOT a defect** — 12,037 of them landed on 2026-07-18, the one-off initial backfill. Establishing that took bucketing by day and cross-referencing a different message type from a different package; one `origin` field answers it directly. **DECIDE THIS WITH SHIP GATE ITEM (d)** — (d) adds ~22% to every line and F1 is where 43% of the lines are; deciding separately means paying the version cost on lines that shouldn't exist in that form. Three restructuring options are set out in the file with their trade-offs (the `submit` line's in-flight breadcrumb has real value if the daemon hangs mid-upload — don't just delete it). Also: the stamp-sync chain is invisible at BOTH ends (`worker.go:572` fires `OnQsoStamped` silently, `qsoservice/stamp_sync.go:69` is Debug — fix as one change with qsoservice Q4) (F2) · an `OutcomeUnreachable` row retries forever by design (ADR 0038) with no retry-age and no queue depth, so "one row retried 86 times over 3 days" and "86 rows each failed once" are indistinguishable (F3 — do the age, the depth needs a new query and may not be worth it). **NOT-gaps is load-bearing here:** **NO CREDENTIAL LEAK, verified two ways** (QRZ's key goes in the POST body not the URL, and a keyword scan of every `forwarding:*` error field in the live log returns 0 matches) — the 895–1,101 B error lines are Go error chains, **do not "sanitise" them**; the four forwarder implementations having ZERO log calls is CORRECT architecture (typed `Result` → `worker.persistOutcome` logs centrally); `registry.go` panics on programmer error and `Build` failures fail startup deliberately; worker startup is logged at the `cmd/smd` caller; and the 401-is-terminal problem is a BEHAVIOUR bug already filed below, not a logging gap. **Second pass added F4-F16 the same day — all 13 verified real, none duplicating F1-F3. The two passes found OPPOSITE HALVES of one defect:** the first says the two huge lines carry no provenance, the second says the interesting transitions carry no line at all — so decide F1's restructuring with F5/F6/F7 in view, they are the lines that SHOULD exist. **F4 is the cheapest and should go first:** `registry.go:63` documents, as the justification for its no-credentials-in-errors rule, that constructor errors are "logged as a startup fatal by spawnForwarderWorkers" — **they are not**; `cmd/smd/main.go:1294` only returns, and `main.go:132` prints to **stderr**, so a credential/config fault that stops the daemon shows `smd starting` → `smd stopped` with the cause absent from `smd.log`. That is the **third route** by which "why did the daemon stop?" bypasses the file an admin reads (with api A1 and A9) — and fixing it makes an existing security comment true rather than aspirational (keep the rule; its stated reason is what's wrong). **F5:** `markTransientInternal` has ZERO log calls, so a DATABASE-caused transient — the more serious cause — logs neither its retry nor its exhaustion, while the forwarder-caused one logs both; and it self-erases, since `last_error` is the only record and a later success clears it. **F6:** `forwarding: success` is logged BEFORE `markSuccess` persists, and the re-arm case is Debug-only — so an upstream-accepted row that is still queued and WILL be sent again looks exactly like a completed upload (compounds with F1: the duplicate that follows has no provenance either). Also: soft-delete/missing-QSO terminal transitions call `markFailed` with no line (F7, the other end of qsoservice Q5) · a reconcile can commit upserts then fail on deletes and log only "run failed", discarding the partial summary (F8) · on-demand reconcile has no summary log — the forwarding half of api A2 (F9) · reconcile records `Truncated=true` but not how much remains, and drops the same classifications a second time at the consumer, **so fixing qsoservice Q2 alone will NOT surface them** (F10) · smcloud's `applied=0` ("cloud has newer") logs as ordinary success while reconcile WARNS on the same condition — the two paths disagree (F11) · ctx cancellation at shutdown is classified as `host unreachable` and logs an outage-shaped line plus a persistence Error, while the claim path suppresses cancellation correctly — this is what makes the 86 unreachable lines hard to read (F12) · a ClubLog build with no injected API key constructs an unusable forwarder with no startup warning, then emits **"host unreachable"** forever for a path that makes NO network request (F13, a concrete instance of F3). Tier 3: lost upstream dispositions (F14), retry policy absent from the startup line (F15), `res.Err` omitted from the unrecognised-outcome warning (F16). Fold into this backlog and delete the file once shipped.
- **VALIDATE (on-air): FT8 reduced type-4 hashed ladder (ADR 0048).** Code shipped 2026-07-16 (`type4.go`/`type4_sequencer.go`, `mode:"type4"`, SPA answer path; offline `TestType4_RoundTrip` green). Work a real nonstandard station (`/D`, prefix-compound) on air → flip ADR 0048 Proposed→Accepted. _(Detail: `docs/ft8.md` + git.)_
- _SPA architecture (post-ship · ADR 0044):_ consolidate logging+config+logbook into **one app shell** (dashboard + Operate[Phone/CW+FT8] + Logbook + Settings; manual stays zero-JS — 3→1). **RETIREMENT DIRECTION DECIDED 2026-07-20 (operator):** retire the three legacy SPAs in favour of `frontend/app` — remove routes AND embeds together (dead embeds bloat the binary + CI), KEEP the source dirs for reference (real deletion later gets a preservation tag, the ft8-snapshot pattern). Parity audit run (detail: session-handoff S228): order = **logbook first** (one gap — port the 2026-07-19 ClubLog amber-retry/`skipped_no_history` surface, legacy-SPA-only today) → **logging** (port the 112-line `en.ts` i18n error catalogue — app deliberately shows raw codes, `rig.svelte.ts` ~658; `/` then redirects to `/app/`) → **config last** (blocked on building the app Settings view, which also absorbs the FT8 Settings tab + MyStation, both app-less today; app Settings + Dashboard are placeholders). `api-endpoints.md` updates in the same commit as any route removal. Sequenced behind SMC milestone 1. **Subsumes** the _UI cohesion_ cluster below (theme built into the shell from the first commit). Gated behind the 7Q8AC ship.
- _UI cohesion:_ shared theme layer (token convergence) → UI themes + dark mode → FT8 Spectrum colour revision · version-in-tab-title
- _FT8 — **PARKED 2026-07-31 (operator): "none of these 6 are pressing or something I recognise as needing" — do NOT present this cluster as a session target list. Revisit only when the operator names a specific item.** The verified detail below is accurate as of 2026-07-31 and is kept because it cost a full re-verification pass — but it is a snapshot, not a standing sweep, so re-verify before building if one is picked up much later. **RE-VERIFIED AGAINST THE CODE 2026-07-31** (the 2026-07-18 sweep had expired: `frontend/logging` retired 2026-07-21, three days later, killing three sub-items). Realistic target list is 6 buildable + 1 decision, not 11._ **OPEN, buildable now:** type-4 free-text messages (`modulate.go:142` still calls `goft8.EncodeStandardMessage` — structured only; no entry UX) · type-4 work-a-caller trigger (daemon `StartWorkCallerT4` exists at `type4_sequencer.go:217` but is UNREACHABLE — `handler_ft8_qso.go:113,167` accepts `mode:"type4"` only on the ANSWER path and `startFt8WorkCaller` takes no mode arg; still deferred on hashed-us ambiguity, so DECIDE before building) · callsign ignore list (no implementation, daemon or SPA) · Call-CQ waiting feedback (partial — `Ft8Operate.svelte:90` renders `· sent ×N`; nothing says the run is unanswered) · Call-CQ layer-2 recency pool (layer 1 shipped 2026-07-17; still no candidate pool in `Sequencer`, only the same-slot rescan) · work-path opening: prefer a clean next-slot start over the truncated immediate fire (`work_sequencer.go:74` and `:329` still call `fireOpening`). **BLOCKED:** attempt-limit control — everything exists EXCEPT the UI (`handler_config.go:100` `ft8_max_repeats` + range validation, `:774` pushes it live via `SetMaxRepeats`); gated on the app Settings view, which is still a placeholder. **DECISION, not a build:** offset-picker snap — the design moved underneath it. `Ft8OccupancyStrip.svelte:63` already snaps the daemon ★ pick to its cell and `Ft8OccupancySpectrum.svelte:7` deliberately does NOT snap ("click ANYWHERE"); the open question is whether an arbitrary-click snap is still wanted at all. **DEAD — do not pick these up:** ~~footer info-strip rehome~~ (`rxCaption` exists ONLY in `frontend/logging/src/lib/ui/panels/Ft8Panel.svelte`, a retired SPA) · ~~shift+ctrl freq-step parity~~ (`RigKeys.svelte` is shell-global in `frontend/app` and covers FT8; the gap was logging's `QsoPanel`) · ~~accumulate-mode slot-grouped Rx pane~~ (`rxDecodes` does not exist in `frontend/app` and there is no standalone Rx-pane component — almost certainly logging-era too, but CONFIRM what the app's Rx view renders before deleting this entry rather than assuming). **Also FT8 and also not on-air, filed elsewhere:** auto-work pile-up ADR · FT8 session-reconnect reconcile (see the smcloud line) · FT8 duplicate-QSO detection + merge (line below; overlaps the logging workstream).
- _Forwarding / data (verified in the 2026-07-18 sweep):_ clear queued-upload backlog for a forwarder (**half built**: `DiscardQueuedUploadsForForwarderWithContext` exists and auto-purges DISABLED forwarders at daemon startup — `cmd/smd/main.go:464`; the operator-triggered endpoint + UI are the open half) · configurable session-email subject/body (open — still hardcoded in `handler_session_email.go`, with in-code comments pointing here) · **ClubLog putlogs.php bulk-backfill path (logged 2026-07-19, from the API-key helpdesk exchange):** ClubLog's grant condition is that realtime.php never carries catch-up batches of pre-existing QSOs (anti-pattern → key blocked; confirmed back to them 2026-07-19) — but SM's Logbook backfill rides the worker → realtime.php one QSO at a time, so pointing it at ClubLog for a historical set would break the promise. Interim rule (documented in the inbox note): ClubLog history = manual ADIF upload on clublog.org. **The refusal is ENFORCED as of 2026-07-19 (refined same day per review — retry-aware):** `forwarding.RegisterNoBulkBackfill` (clublog registers in init) → `qsoservice.EnqueueUploads` distinguishes PER ROW via queue history: a QSO with prior clublog queue rows was a live upload, so re-arming it is legitimate realtime usage (the 403-era Terminal rows' recovery path — review caught that the first blanket block severed it); a history-less row is backfill → refused into `skipped_no_history` (no queue row written). The logbook SPA shows an amber "Retry failed uploads to clublog" button for such destinations (tooltip + notice explain what was skipped; the "Not on clublog" gap-browse stays — it's how the export set is assembled). Deletes (delete.php) and live logging-time enqueues are unaffected. Same review round also fixed the gzip layer: `gzipResponseWriter.Unwrap()` (without it `http.ResponseController` couldn't extend the export write deadline past the server-wide 2 min — every default Go client accepts gzip, so slow restores would truncate mid-JSON) + proper Accept-Encoding negotiation (q-values/case/wildcard; `gzip;q=0` no longer served gzip) + `Vary` on identity responses too. The proper fix: the clublog forwarder gains a putlogs.php batch route and the backfill path routes ClubLog-bound sets through it (batch ADIF POST) instead of per-QSO realtime rows — then 7Q8AC-style operators get in-app backfill without the manual step. Design notes: (a) putlogs semantics differ (whole-log ADIF upload, server-side dedupe), so the backfill result reporting (enqueued/skipped counts) needs rethinking for that route; (b) **durable retry provenance** (review round 2 #2, accepted limitation for now): the retry-only gate keys on qso_upload rows, which are working state — the ADR 0039 startup purge deletes a disabled forwarder's non-uploaded rows, so disabling ClubLog mid-403 loses the failed rows' retry eligibility (degraded path = the ADIF manual upload, which is ClubLog's blessed route anyway — documented at the gate in `enqueue.go`). The putlogs route dissolves this: once bulk is legal in-app, the history distinction stops mattering for recovery.
- _Infra (all verified OPEN in the 2026-07-18 sweep — three carry in-code "future work" comments):_ SPA SSE consolidation (one multiplexed stream; daemon still serves 3 separate SSE endpoints) · `/v1/hardware` audio availability + enum caching (single `Available` bool + explicit "no cache" comment) · CI-V `sets_state` value-compat validation (marker-exists check only) · `internal/iocdi` contract hardening (M1/M3/M4 all still present) · multi-tab operating-lock (ownership + take-over; awareness banner already shipped — `events.go`/`hub.go` comments mark the lock as future work)
- **smcloud: a 401 is TERMINAL, so a token rotation strands every in-flight upload (found 2026-07-26, planning the ADR 0040 rotation).** `classifyHTTPStatus` marks 401 terminal → `markFailed`, no retry. Correct for QRZ/ClubLog (revoked account), wrong for a destination the operator owns and whose credential they are deliberately changing. The hourly reconciler heals the DATA; the upload rows stay failed. Detail in Bugs below.
- **FT8 duplicate-QSO detection + merge (log level) — evidence-backed 2026-07-26.** _(Half done: a DELIBERATE repeat now stores correctly — `allow_duplicate` threads operator intent from the SPA click through to `Submit`'s `force`, shipped 2026-07-26. What remains is detecting and resolving the ACCIDENTAL pairs already in the log.)_ A station that never copies our closing `RR73` restarts and gets worked and logged a SECOND time; four such rows exist in the dogfood log and were uploaded to QRZ, ClubLog and SM Cloud (QRZ accepted both copies — it does not dedupe). The `confirmHold` mitigation shipped 2026-07-26 narrows the window but deliberately does not close it. Detection + an operator resolve surface (keep / merge / delete) is the open half. Detail in Bugs below.
- _Data / SM-Cloud prep (do before S3):_ `internal/database` review lows (cold-insert retry, bootstrap stale-table detection, + 5 nits) — verified still open 2026-07-18 (no unique-error catch on the cold insert; bootstrap split-check still keys on `country` alone)
- _Code-review lows (2026-07-05 `internal/api` review):_ credential-clear asymmetry (forwarder clears on blank, SMTP/lookup keep) — verified open 2026-07-18 (`mergeForwarders` overlays blanks; `mergeSmtp`/`mergeLookupProvider` keep; each only locally documented)
- _Code-review nits (2026-07-05 `internal/qsoservice` review):_ best-effort `contacted_station` cache warm-up uses the request ctx (a detached short-timeout ctx would make it client-independent, like the dedupe refetch) — verified open, still open
- _Code-review enhancements (2026-07-21 `internal/qsoservice` review — operator-deferred as enhancements, NOT regressions):_ **(#4) empty-report fabrication is FT8-only** — `submit.go`'s `if mode != "FT8"` defaults RST to `59` (and `update.go`'s `if Mode != "FT8"` requires non-empty RST), so an imported **FT4 / other SNR-reporting** record gets a bogus `59` and a legit empty-report record can't be edited; widen the exemption from the FT8 literal to the SNR-mode set when FT4 (or another SNR mode) actually matters. FT8-only is deliberate today and SM doesn't run FT4, so no live impact. · **(#7) SUBMODE not validated against MODE** — `submit.go:111` consults the submode catalogue only when MODE is ABSENT, so `MODE=SSB, SUBMODE=DMR` (DMR is a DIGITALVOICE submode) and unknown submodes store + forward unchecked (Update just trims the value); add SUBMODE↔MODE consistency validation. Data-quality enhancement, no current consumer forcing it.
- _Per-logbook operating identity (decided 2026-07-22, deferred from the `internal/api` review #1 — operator chose "patch now, feature next"):_ **make the operating callsign follow the SELECTED logbook instead of the global My Station config field.** Today `STATION_CALLSIGN` on a QSO is sourced from `config.LoggingStation.StationCallsign`, and the submit gate (`qsoservice/submit.go:145`) requires it to equal the logbook's callsign; FT8 TX identity reads the same config field (`handler_ft8_qso.go:97`). So operating under a second call (7Q5MLV ↔ 7Q8AC) is impossible without changing the global callsign — which the review #1 patch now **rejects** (409 `callsign_locked_to_logbook`) to stop it silently orphaning the default logbook. The feature: demote `station_callsign` to a set-once *home/default* (seeds the first logbook), and source the operating callsign (QSO stamp + FT8 TX identity) from the **active logbook**, so switching call = selecting a logbook. Touches daemon (submit gate, FT8 TX identity, setup/seed) + both SPAs (logbook selector drives identity; My Station dissolved). **SPECIFIED in ADR 0055 (2026-07-22, Accepted)** — the refined model: `STATION_CALLSIGN` + `OWNER_CALLSIGN` → logbook; `MY_*` location/equipment → physical-station config; `OPERATOR` (+ `MY_NAME`) → config **operator roster** + transient current-op (multi-op / contest-group case). Retires the callsign_mismatch gate AND the shipped 409 guards (config PUT + logbook DELETE + setup reuse) — those only fenced the footgun this model removes. Design fully converged; no code yet.
- _Daemon / data (dogfood triage 2026-07-08):_ downgrade client-abort enrichment WARN→debug when the cause is request-ctx cancellation (verified open 2026-07-18 — `orchestrator.go` `warn` is unconditional, no `ctx.Err()` check)
- **FT8 RX propagation overlay on the map — "what can I hear right now" (2026-07-26).** Plot stations decoded in the last N minutes as an overlay on the existing contacts map. **Entirely first-party**: the decode stream already carries call/SNR/freq/time and the map engine already draws great-circle arcs from the QTH — no external service, no rate limit, no bandwidth, refreshed every slot. The "who can hear ME" half is a SEPARATE and much harder problem (needs an aggregator) — deliberately not this item. Detail in Bugs below.
- _Rig / bands (dogfood triage 2026-07-14):_ **contact view (working panel) re-organise** (frontend/app Operate UI)
- _Map (dogfood triage 2026-07-18):_ ~~**zoom/pan + station hover tooltip**~~ **BUILT 2026-07-18** (committed; needs a dogfood eyeball after redeploy) · ~~**background-tab staleness**~~ **BUILT same day** (`visibilitychange` → immediate catch-up refetch in `mapData`, listener detached on teardown; repro confirmed by the operator first) — both → archive once dogfood-validated
- _smcloud hardening — **pre-Phase-2 gate** (operator-flagged 2026-07-18):_ ~~**`cmd/smcloud` needs rate limiting before anything internet-facing.**~~ **RATE LIMITING BUILT 2026-07-19**, per the decided two-layer design (operator, 2026-07-18 — each layer a distinct job): (1) **per-IP at the reverse proxy** — `deploy/smcloud/Caddyfile.example` gained the `rate_limit` block (60/min/IP; NB stock Caddy ships NO rate-limit handler — needs `xcaddy build --with github.com/mholt/caddy-ratelimit`, documented there + runbook §4; the proxy sees real client IPs, the binary never does) · (2) **the global in-process concurrency limit** — `internal/cloud/server/limit.go` `limitMiddleware`, bounded-semaphore try-acquire OUTERMOST in the handler chain (a rejected request costs one 503 write — no gzip writer, no pool wait, no goroutine pile-up); default 16 ≈ 3× the 5-conn pool, `-max-concurrent`/`SMCLOUD_MAX_CONCURRENT` (boot-validated ≥ 1, fail-loud on junk); over-limit → immediate `503 {"code":"overloaded"}` + `Retry-After: 2`. Tests: saturation → 503 with parked requests unaffected, slot release, zero-value default fallback, parse table. **Review round (3 findings, ALL real — 7th consecutive clean round) FIXED 2026-07-19:** (1) the handler-level semaphore alone couldn't bound a connection/slow-header flood (net/http spawns a goroutine per accepted conn BEFORE any handler) → accept-time connection cap added (`netutil.LimitListener`, 4× the request cap — x/net was already a direct dep) + claims narrowed in the comments; LimitListener semantics pinned by test · (2) runbook §4 enabled STOCK Caddy before the plugin note, and `xcaddy build` doesn't touch the systemd binary → §4 rewritten: install custom binary over `/usr/bin/caddy` → `caddy list-modules | grep rate_limit` → `caddy validate` → only then enable; package-upgrade clobber warning (fail-loud: stock caddy refuses the unknown directive) · (3) ADR 0052 overclaimed "reconcile surfaces" single-writer violations — same-second same-revision edits produce identical version tuples (`>=` tie guard + payload-less reconcile hash → silent divergence); ADR corrected + device/writer-id tie-breaker made a PRECONDITION of the bidirectional-reconcile sync leg. **Round 2 (3 findings, ALL real — 8th consecutive) FIXED 2026-07-19:** (1) runbook step 5 `enable --now` doesn't restart the apt-auto-started stock Caddy → explicit `restart` (both families; upgrade note now says repeat 3 + restart) · (2) ADR: payload-digest-in-hash is NOT equivalent to a writer-id tie-breaker (summary would flag mismatch forever while the revision+modified_at manifest diff finds nothing → non-convergent); reworded — writer id is the ordering fix, digest at most a detection aid needing manifest propagation + a conflict branch · (3) `connCap` ×4 could wrap negative on an extreme `SMCLOUD_MAX_CONCURRENT` and panic LimitListener at boot → parse ceiling 4096 (`maxMaxConcurrent`) + boundary/overflow tests. (Original mechanism, for the record: the bounded DB pool protects Postgres, not the process — `/v1/health` pings Postgres unauthenticated, and excess requests piled up as handler goroutines waiting on the pool.) **Remaining gate: the ADR 0040 security assessment + rotating the leaked dogfood token** before anything internet-facing. · ~~**Streaming/paginated export (review 3 #4)**~~ **BUILT 2026-07-20 (external review re-found it — the fix-don't-defer trigger):** `ExportSnapshot` now streams rows to callbacks from inside the repeatable-read tx and `handleExport` writes each row straight onto the wire (identical wire format; peak memory one row); accepted trade documented in the handler — the tx/pool conn stays open while a slow client drains, bounded by `exportWriteDeadline` + the request semaphore. Landed in the 2026-07-20 four-finding batch (padded-UUID raw gate + restore canonicalise · `(tenant_id, uuid)` composite key migration 0004 + `ON CONFLICT` rescope · this · gzip relative q-value preference). **FT8 session reconnect reconcile** — `ft8-logged` has no backlog, so a QSO completed during an SSE gap is durable in the logbook but missing from the session list / export / worked markers. **Design note (the trap):** do NOT naively replay-cache the event daemon-side — a fresh page load hours later would receive the stale event into an empty session list, and the uuid dedup can't catch that (the new session doesn't hold the row). Correct shapes: SPA reconnect reconcile (on re-open, fetch QSOs since session start, merge by uuid) or a time-bounded replay the SPA can age-filter. · **Write idempotency for QSO submit + session email (daemon)** — a timed-out write is ambiguous (may have committed / SMTP may have accepted); the SPA now *says* "outcome unknown" (shipped), but resolving the ambiguity needs a daemon-side idempotency token (client-generated key the daemon dedups on, with a retrievable outcome) so a retry is safe instead of merely warned about. Email is the sharper case (a real duplicate lands in a real inbox).
- _smcloud stamp-drift → reconcile bandwidth churn (found 2026-07-19, dogfood log):_ **post-upload stamp writes make `in_sync:false` the routine state and force the expensive reconcile path every operating hour.** Mechanism (verified in the dogfood DB — every post-0050 row sits at revision 2): the QRZ success stamp (`MarkUploadSuccessWithAdifStampWithContext`) and the session-email stamp (`MarkSessionEmailedWithContext`) both `UPDATE qso` AFTER the smcloud worker has already pushed the row → the combined trigger bumps `revision` → local ≠ cloud until the hourly reconcile heals it (observed: 7/39/34-row heals through an evening session, 94 after the email batch). Cost is NOT the upserts — it's that any hash mismatch drops `RunOnce` to the **full cloud-manifest GET: O(total logbook), ~110 B/row ≈ ~650 KB uncompressed at 5.7k rows and growing forever, per client per drifted hour** (`writeJSON` sends plain JSON; no gzip anywhere on the path) — precious bandwidth on a Malawi link, and linear in client count for Phase 2. **Fix shapes, in order:** ~~(1) the two stamp paths enqueue an smcloud upsert for the stamped row(s)~~ **BUILT 2026-07-19:** `forwarding.RegisterRowMirror` flag (smcloud registers; the re-enqueue targets mirror types only, so a QRZ stamp can never re-upload to QRZ) → `qsoservice.EnqueueStampSync` (update-action rows via the existing UPSERT re-arm, idempotent across the QRZ-then-email double stamp) → worker `Config.OnQsoStamped` hook fired ONLY on the stamping branch (smcloud stamps nothing → no loop) wired in `spawnForwarderWorkers`, + the session-email handler enqueues after its stamp commit; all best-effort (stamp never rolls back for a mirror miss; reconcile stays the backstop). Steady state returns to the cheap hash-only check, restoring `in_sync:false` as a real alarm. Tests: worker hook fire/no-fire + 5 qsoservice integration cases; full `-race` green. ~~(2) gzip the manifest + export responses~~ **BUILT 2026-07-19:** `gzipMiddleware` wraps the smcloud handler chain (`internal/cloud/server/gzip.go`) — Content-Encoding negotiation, Vary header, streaming writer; 3 tests incl. a stock-Go-client transparent round-trip (no daemon change needed — the transport already sends Accept-Encoding and auto-decompresses; skew-safe both directions). **Takes effect at the next F44 smcloud RPM rebuild + deploy** (`task rpm:smcloud`). Open half: (3) Phase-2 scale option, capture-don't-build: bucketed range hashes (Merkle-lite) so a mismatch pulls only drifted buckets instead of the whole manifest.
- _Dogfood triage 2026-07-23 (frontend/app · all verified in code, detail in Bugs):_ **Rig Control VFO-A/B surface** — swap-vs-select semantics (needs a decision), label not a click target, VFO-B sometimes unrendered (needs on-rig observation).
- _Onboarding:_ install / first-run friction for non-Linux operators
- _Diagnostics:_ operator log viewer (DB-manager tab) — **now subsumed by ADR 0061 (next line); that entry's "a tab in the DB-manager SPA" shape predates ADR 0044 and is probably stale.**
- _Consolidated logging (**ADR 0061, status Proposed**):_ an operator-facing **EVENT store fed from published events** — explicitly NOT a mirror of `smd.log` — in `station-manager.db`, categorised, JSON detail column, **build version on every row**; `smd.log` retained unchanged as the diagnostic sink of last resort. The `qso` category **already exists and works** (`qso_history`, ADR 0016) — missing only the way OUT (no HTTP route, no SPA surface, `FetchQsoHistoryByUUIDWithContext` called only from tests), so that half is a SURFACING job. smcloud gets the same split, tenant-scoped. **ALARMS ARE THE PILOT SLICE** — the feed already exists (`EventTxAlarm`/`EventDriveAlarm` via the bridge hub, only the sink is missing), volume is trivial (9 in 15 days), and they exercise every hard part (build stamping, retention, SPA surface, acknowledgement round-trip) at a scale where getting it wrong is cheap; they also unblock ADR 0060, which is parked on alarm data that does not exist in usable form. Note the alarm gap is **not** "unlogged" — raises/clears/codes are all in `smd.log`; what is missing is build attribution, queryability, and **the operator's half** (`dismissTxAlarm`/`dismissDriveAlarm` set a client-side boolean and send nothing, so nothing records whether an alarm was ever seen). Prerequisite: the SHIP GATE entry above (those four are events that do not yet exist to be recorded). **Open questions are the operator's call and listed unfilled in the ADR** — the gating one: **is the smcloud admin surface internet-facing or behind WireGuard/Tailscale?** (raised 2026-07-20, still unanswered; Phase 2 already blocked on the ADR 0040 security assessment).
- _Alert surfaces (**ADR 0060, status Proposed — DO NOT BUILD YET**):_ the three shell alerts (`TxAlarmBanner`/`DriveAlarmBanner`/`DriveMonitorNotice`) render **in document flow** in `App.svelte` and push `<main>` down — up to three rows, and the drive alarm raises mid-slot with ~9 s of a 12.6 s FT8 slot left, so content jumps while the operator is reading it. Audit (2026-07-31) found the event-vs-state tiering already sound; only placement is wrong. Proposed direction: nothing in flow · header centre (permanently-reserved chrome, zero shift) hosts the calm states · **`tx_still_keyed` alone** gets a blocking emergency overlay, the other four TX codes demote to the header. **BLOCKED ON OBSERVATION, deliberately** — operator saw some of these live 2026-07-31 and wants several more runs before committing; the ADR is a record, not a mandate. **Carries a daemon dependency:** `raiseTxAlarm` publishes only on the false→true edge (`if !already`), so an alarm that escalates `tx_unconfirmed` → `tx_still_keyed` on a later probe is **suppressed** — harmless today (all five codes render one banner), load-bearing the moment the code becomes a tier selector. Open questions are the operator's call and are listed unfilled in the ADR. Incidental: ADR 0008 specifies toasts at `top-4 right-4`, `Toasts.svelte` resolves to bottom-centre — one is out of date.
- _Code-review lows (2026-07-05 SPA review; re-verified in the 2026-07-18 sweep):_ 13 verified low-severity fixes (the fetch-timeout standout was promoted to P1 and SHIPPED 2026-07-05 → archive) — state-reset gaps (**mostly done**: tabCount + freqKnown resets confirmed in `bridge.svelte.ts`; stale-decodes / enrich-zombies unverified — check those two when picked up) · FT8 UI nits (**partially done**: bearing-360° + canAnswer TX-guard confirmed built; drain-abort / FD tooltip / isWorking split unverified)
- _Bridge/TX hardening (2026-07-05 review):_ ~~generation counter · identity poisoning~~ **PROMOTED to the P0 TX-safety companion batch (2026-07-18 review re-found both with sharper mechanisms)** · `bridge.New` nil-checks (NB the item's premise drifted: New takes an `openClient` closure, not Serial/Cat fields — `Initialize` checks only the logger; re-scope to "validate injected closure + logger deps" when picked up)
- _FT8 audio levels (dogfood 2026-08-06, triaged same day):_ ~~RX level bar (capture RMS, cheapest)~~ **BUILT 2026-08-06** (`AudioLevelCard`, the RX-meter arc) · ~~TX drive indicator from the ALC/PO metering~~ **BUILT 2026-08-07** (ADR 0064 continuous polling + `TxDriveChip`; awaiting on-hardware acceptance + `alc_red` calibration) · **WSJT-X-style per-band Pwr control via `txAmplitude`** (waveform attenuation, NOT mixer control) — the operator's live pain: hand-adjusting PC volume when the linear amp's indicated power looks wrong. This half also closes the 2026-07-27 drive-collapse follow-up (a) `ft8.tx.amplitude`. Detail below.
- _Dogfood triage 2026-08-07 (small, frontend/app + daemon):_ **Rigs tab: expose the daemon's ACTIVE rig on the wire** (bridge-open rig vs `default_rig_id`; "restart to apply" indicator when they diverge — inbox 2026-08-02) · **SSE clients revive on `visibilitychange`, only-when-dead** — one fix at the SSE layer for all surfaces; a healthy `/v1/ft8/events` stream must NEVER be torn down (reopen-fail would disarm TX via the linger); needs a client-side liveness signal (inbox 2026-07-28/08-02) · **Phone/CW comment paste-list** — port `commentHistory.svelte.ts` (bounded MRU + dropdown) from the retired logging SPA (inbox 2026-08-07).

**P3 — deferred / large / needs a trigger**
- CAT poll mode (ADR 0034) · FT8 semi-auto watch-list (SET ASIDE) · spot-submitter registry (on 2nd destination) · operator / user profiles (contesting lens: bundle op-identity + contest params, swap mid-event — dogfood 2026-07-06) · outbound UDP telemetry (WSJT-X-compatible) · FT8 occupancy waterfall render · POTA fields · config hot-reload · settings help tooltips + beginner/expert mode · FT8 Monitor/Listen toggle (DISCUSSION) · download-site install page · `PUT /v1/config` `default_logbook.id` wiring (no consumer yet)
- _Never-captured gaps (2026-07-20 "what's left" survey — in NO doc until now):_ **LoTW forwarder** (the most-expected destination after QRZ for a general audience; a different integration shape — TQSL signing/certificates, not REST) · **eQSL** · **awards tracking** (DXCC/WAS/WAZ worked/confirmed per band-mode — the natural consumer of the ADR 0052 QSL-confirmation layer if that's ever decided) · **logbook statistics/analytics** (QSOs by band/mode/year, rate charts — a logbook staple) · **inbound DX cluster client** (spot CONSUMPTION for phone/CW hunting; only outbound spot-submission was captured). **inbound DX cluster client is now ADR 0053 (Accepted 2026-07-20)** — a new `internal/dxcluster` telnet subsystem — enrichment (DXCC) + exact-callsign contest-dupe + a NEW needed-entity logbook aggregation (contest-dupe is exact-callsign, can't answer needed-DXCC) for the wanted decision; SSE spot feed + watch-list alerts; FREQUENCY-first click-to-QSY (spots carry no mode; mode best-effort inferred → rig literal, omitted if unknown since SendCommands is atomic) via the bridge seam; P2/post-ship. Also from the survey: logbook management (multi-logbook create/switch — `default_logbook.id` deliberately unwired), DB management (no operator-facing backup/restore/integrity surface), and contesting (serials, Cabrillo, contest definitions/score — design-first; only FT8 FD + contest-dupe exist) are design-first workstreams whose UI homes belong in the app shell, not the legacy SPAs. **Two more surfaced by the 2026-07-20 qso-director.com feature scan:** (a) **POTA / activation management as a first-class workflow** — SM has the ADIF fields (`MY_SIG`/`MY_IOTA`/`MY_WWFF_REF`) but no activation workflow (park/summit lookup, activation-mode logging, a LIVE POTA-spot feed); POTA is hugely popular and composes with the ADR 0053 cluster/spot subsystem — strongest genuinely-new candidate, possibly its own ADR. (b) **"Call Sense"-style predictive callsign assistance** — super-check-partial completion as you type, from your own log + a master callsign DB, with previous-QSO recall; SM only enriches AFTER a full call is typed, so assisted/predictive entry is a real logging-UX win for DX/contest. Both P2/post-ship. (The scan otherwise VALIDATED existing direction: DX cluster+alerts=ADR 0053, WSJT-X/GridTracker UDP interop already backlogged, contest scoring + dockable layout already noted; SDR spectrum scope + external-WSJT-X integration judged out of SM's lane.)
- _Review-arc finding 2026-08-05:_ **`frontend/app/src/lib/operate/rig.svelte` — one owner for "where is the rig, including commands in flight".** Four mechanisms answer the same question today and none of them talk to each other; the mode-restore arc took five clean-room review rounds converging on it. Detail below.
- _Dogfood triage 2026-07-23:_ **QTH on the Phone/CW card** — WAI today (QTH lives in the ContactDialog by design); build only if the operator confirms they want it promoted into the Contact-details disclosure.
- _Dogfood triage 2026-07-25 (note only, no code yet):_ **rig Time-Out Timer (TOT) surfaced / set via CAT.** The FTdx10/FT-710 has a menu TOT (operator's is 3 min); on 2026-07-25 it cut two long SSB transmissions with a triple-beep — diagnosed as **rig-side, NOT SM** (SM wrote nothing at the drops; two TX were both exactly 180.0s — see `session-handoff.md`). SM could read the current TOT via CAT and surface it, so a "long TX will be cut in Ns" warning is possible before the rig times you out, and optionally set it — but per **ADR 0057** the TOT is a REQUIRED TX-safety prerequisite (SM's dead-wire backstop, the only stop that survives a dead CAT link mid-tune/FT8-TX), so any CAT-set MUST keep it enabled and bounded, NEVER OFF. **Weigh against narrow-daemon scope first** — this is operating UX (rig control), not log/forward. NB SM's own tune (≤20s) + FT8-TX (≤18s) sit well under any TOT, so this is purely for manual phone/CW ragchews, not an SM-TX blocker. **Extension (stuck-TX follow-up (a), 2026-07-28, folded in 2026-08-07):** mode-scoped clamp — snapshot the operator's TOT, clamp to the rig's 1-min floor for the life of an FT8 session, restore on exit (the ADR 0027 snapshot/clamp/restore shape, so not a new safety mechanism); addresses the one failure class no CAT logic can (a radio that stopped listening — it would have ended the 07-28 carrier ~4 min earlier). Needs `EX` menu-command support in the rigdef, which doesn't exist today.
- _Dogfood triage 2026-08-07 (UI, each needs an operator decision first — detail in the struck inbox entries):_ **world-time widget vs map solar-time overlay** (one decision covers both; overlay recommendation = (A) solar) · **map band filter** (band-list source: data-window vs `operating_bands` vs both) · **session-panel column resize + sort** (resize must keep `table-fixed` binding widths; persistence + sort model open) · **notification history rail + sticky-toast conversion** (decide together with ADR 0060 placement + ADR 0061 event store — the rail's substrate; never serve `smd.log`).
- _Meter/TX-incident follow-ups (triage 2026-08-07, from the struck 07-27→07-30 inbox entries):_ **FT-710 rigdef `RM`/`MS`/`METERPOLL` selectors unverified against its own CAT manual** (verify before any FT-710 deploy; nothing meter-related in `yaesu-ft710.json` today) · **meters `last` field still raw key-down tail** (onset-style treatment or removal — `meters.go` logs `_last` unchanged) · **arm-time log of the output sink's name + volume** (one grep instead of an hour at the next collapse) · **playback reopen-on-collapse** (needs-trigger: the disarm/re-arm recovery is UNVERIFIED — try it at the next occurrence before building the deadsource.go analogue) · **TX0-ignored sweep + persistent-`2` escalation re-look** (parked on thin data — 4 usable samples; escalation needs an operator duration threshold). `MY_RIG` follow the CAT-identified rig when connected (config = fallback) · single-source the freq→band table + regional band-plan design (three hand-synced copies today) · FT8 tune-carrier occupancy-skip (pending HW check on whether the RTTY tune tone bleeds into RX audio) · whole-log **Dashboard map** (time-window contacts map SHIPPED 2026-07-16 → archive; the dashboard reuses its engine — needs the `GET /v1/logbook/{id}/map` aggregate) · FT8 auto band-hop / "run the bands" · voice keyer + phone/CW auto-CQ + QSO copilot (crosses the v1 "no phone/CW PTT-for-operating" line — post-ship) · movable / dockable nav · propagation / conditions panel (external online data source — dogfood 2026-07-09) · 2nd callsign-enrichment provider (HamQTH/QRZCQ/qrz.digital candidates as a fallback link in the lookup chain, catches QRZ-absent calls — dogfood 2026-07-13/24) · smcloud "am I being heard?" pile-up status site (community-phase, capture-don't-build — dogfood 2026-07-11)
- _smcloud deploy-model note (2026-08-11) — needs-a-trigger = Phase 2 / multi-instance:_ **`cmd/smcloud/main.go:433` auto-applies the embedded migrations (`store.Migrate`) on every boot** — correct and convenient for the single dogfood instance (an upgrade is `rpm -Uvh` → `systemctl restart` → self-migrates via the golang-migrate `schema_migrations` table, idempotent; no separate migrate step). Two things to add BEFORE anything internet-facing / multi-instance: (1) a **`-migrate`-only mode** (apply pending migrations then exit) plus a serve-only path, so a Phase-2 deploy can gate schema DDL behind an explicit step in a maintenance window **with a backup taken first**, rather than applying it the instant a new binary boots (an upgrade whose migration is long-locking or wrong would otherwise land unattended); (2) confirm the **concurrent-boot story** — golang-migrate's postgres driver is understood to take an advisory lock so two instances shouldn't race the migration, but VERIFY that before relying on it at multi-instance. Deliberately NOT a build for the single instance today. Ties to the smcloud pre-Phase-2 gate (the ADR 0040 security assessment + token rotation, P2 above).

**Designed workstreams — built on go-ahead (not queued)**
- SM Cloud P1 (ADR 0040 + `docs/v2-design/sm-cloud-p1.md`) · DB-manager SPA (4th SPA — incl. a data-validation / DXCC-consistency-checker surface mirroring `scripts/qso-audit.py`, dogfood 2026-07-13)

**Parked — blocked or out of scope (do not pick up now)**
- _Blocked on external event:_ **FT8 Field Day UI** (FD-aware Operate ladder render · pile-up Ctrl-click · config-SPA section dropdown) + any further FD on-air validation — the FD path can only be exercised **during a Field Day contest**, so it waits for the next one. Flows already shipped + on-air-validated 2026-06-28. NOT a 7Q8AC ship concern (ARRL/RAC-only; a Malawi op doesn't run FD).
- _Out of scope (never):_ FT8 **daemon-initiated** sequencing — the FT8 spec forbids automatic operation; every SM session starts from an operator action. NB "operator-initiated", NOT "attended-only" — SM does not check that the operator stays. See the scope note.
- _Future thinking:_ "design our own sequencing / timing".

## Bugs (detail)

- **P3 · `frontend/app/src/lib/operate/rig.svelte` — no single owner for "where is the
  rig, including commands in flight".** _(Surfaced 2026-08-05 by the operating-mode
  restore arc: five clean-room review rounds, each finding a real defect in the
  previous round's fix — `c52e9b80` → `809f7890`. Every one was the same mistake at a
  finer grain, and they converge on a concept the codebase does not have.)_

  **The question nobody owns.** Between issuing a CAT command and the rig's confirming
  push, `rig.vfoA` / `rig.vfoB` / `rig.modeLiteral` still read where the rig is
  *leaving*. Any code that must know the rig's position in that gap has to answer
  "what have we commanded that has not come back yet?" — and today four different
  mechanisms answer it, independently and incompatibly:

  1. **`pendingFreqHz`** (`rig.svelte`) — per-VFO commanded frequency, seeded by
     `setFreq` / `nudgeFreq` / `seedFreqTarget`. Frequency only. Never cleared by a
     report; instead `nudgeFreq` decides it is stale via a **350 ms time window**
     (`FREQ_REPEAT_WINDOW_MS`), which is a key-repeat heuristic, not a CAT round-trip
     guarantee.
  2. **Optimistic write + rollback** — `setMode` writes `rig.modeLiteral` + `rig.mode`
     immediately and reverts on rejection; `swapVfoLive` does the same for `rig.vfoB`.
     Correct, but only for those two fields, and it mutates the DISPLAY state.
  3. **`held` + `rigReports`** (`modeRestore.svelte`) — per-field commanded value dated
     with that field's rig-report counter, superseded the moment the rig reports that
     field. This is the most correct of the four and the only one with no threshold in
     it — but it is private to mode-restore.
  4. **Nothing at all** — `selectBand` and `bandUp`/`bandDown` (`set_band`,
     `band_up`/`band_down`) deliberately have no optimistic write and wait for the
     push. Reasonable in isolation; invisible to (1) and (3).

  **The live defect this leaves.** `modeRestore`'s holds only know about the commands
  `modeRestore` itself issued. A frequency moved by `setFreq`, `nudgeFreq`,
  `selectBand`, `bandUp`/`bandDown` or `ft8SelectBand` leaves no trace it can see, so
  a Phone/CW ↔ FT8 switch **within one CAT round-trip of any of those** snapshots the
  pre-command frequency and can later return the rig to it. Narrow (sub-second) and
  not the same bug as any of the five that were fixed — but it is the same shape, and
  it is the one the current design cannot close from inside `modeRestore`.

  **The refactor.** Lift the concept into `rig.svelte` as the module's own answer —
  roughly: every command path records `{field, value, reportSeq}` as it issues, every
  rig-state report supersedes the matching field, and one exported accessor returns
  the rig's effective position. `modeRestore.held` / `effective()` then delete
  outright; `pendingFreqHz` folds into it (losing the 350 ms window, which the
  report counters make unnecessary); `setMode` / `swapVfoLive` keep their display
  optimism but stop being the only record.

  **Blast radius — this is why it is P3 and not P2.** It touches EVERY path that moves
  the radio: `setFreq`, `nudgeFreq` (+ Coarse/Fine/Jump), `selectBand`, `bandUp`,
  `bandDown`, `setMode`, `selectVfo`/`swapVfo`, `ft8SelectBand`, and mode-restore.
  That is the TX-adjacent surface, so it wants characterization tests over the
  existing behaviour FIRST (`rig.svelte.test.ts` is already substantial), then the
  lift — per the "characterization tests before refactoring" lesson. It does **not**
  touch `tx_on`/`tx_off`: those are never `exposed` (ADR 0026/0030) and no accessor
  here can reach them.

  **Do not "simplify" it with a timeout.** The first three attempts in the arc each
  reached for a tolerance and each was wrong. "Has the rig reported this field since
  we commanded it" is a fact the system can carry exactly, via the per-field counters
  already in `rig.svelte` (`rigReports`, bumped in `onRigState` where each field is
  written). Keep that property.

  **Prior art to read before starting:** `modeRestore.svelte.ts` (the converged
  version) and `modeRestore.svelte.test.ts` — its header carries acceptance criteria
  A1–A19 and the reasoning for each rule, including the four failed models. The five
  review rounds are in the git log between `c52e9b80` and `809f7890`.

- **P2 · `internal/ft8` + FT8 view — audio level indicators + a WSJT-X-style
  per-band Pwr control.** _(Dogfood 2026-08-06: "add audio in and out controls for
  ft8 with an indicator level bar green==good, red==too high, orange==too low";
  assessed same day — this entry IS the triage, mechanism-corrected after the
  operator pointed at WSJT-X/JTDX precedent.)_

  **The operator's live pain is TX-side (stated 2026-08-06):** "I have recently
  been adjusting the volume on the PC when the indicated pwr output on the linear
  amp is not looking correct" — i.e. TX drive is corrected by hand at the OS
  mixer, with the AMP's meter as the only feedback. That is the same behaviour
  the 2026-08-01 50-QSO run recorded as step-shaped power dips, and hand-mixer
  changes are the class that produced the 2026-07-30 muted-into-live-QSO
  incident. The feature's value centre is removing that loop: set drive in-app,
  read the RIG's own meters on-screen.

  **Staged, cheapest-first:**
  1. ~~**RX level bar**~~ **BUILT 2026-08-06** (same day): daemon
     `internal/ft8/audiolevel.go` (peak/RMS dBFS per 250 ms window, tee between
     source and scheduler, `ft8-audio-level` SSE ~4 Hz, not replay-cached) ·
     config `ft8.audio` low/high dBFS window (resolved on GET as `ft8_audio`,
     read-only over PUT, validated on the RESOLVED pair) · SPA
     `audioLevel.svelte.ts` classifier (clip = fixed −1 dBFS peak; staleness
     2 s = dead-capture state, distinct from the −120 silence floor which
     truthfully reads 'low') + `AudioLevelCard.svelte` bottom-left chip/card
     (state-coloured icon collapsed; TX stand-down). Defaults −60/−10 dBFS
     AWAIT HARDWARE CALIBRATION against the PCM2903C — config.json edit +
     restart to tune.
  2. **TX drive indicator — now ADR 0064 (Proposed, 2026-08-06): CONTINUOUS
     `RM4;RM5;` polling while an FT8 capture session is live** (250 ms
     cadence / 100 ms answer timeout, both operator-ratified same day; the
     timeout, not the cadence, is the load-bearing safety number). The
     earlier premise here was WRONG twice: the bridge does NOT stream ALC
     (only the selected meter is pushed — METERSEL stays PO for the drive
     watch), and the "can't query during TX" objection died on the wire —
     TWO catcli experiments (2026-08-06, one operator-keyed) proved queries
     answer DURING transmission with live values, and that nothing
     subscribes continuing answers. Most of the mechanism exists (ADR 0035
     poll loop + rigdef RM4/RM5 decode + meterTags slots). Open: Tune
     coverage, SPA red threshold (first datum: ALC 026 at normal voice
     drive).
  3. **Per-band Pwr control** — WSJT-X precedent CITED (user guide: the Pwr
     slider at the main window's right edge, dB on hover, remembered per band
     and separately for Tune; JTDX inherits the layout — inference). Mechanism
     is NOT mixer control: it attenuates the GENERATED waveform, and our hook
     already exists — `internal/ft8/modulate.go` `txAmplitude = 0.5`, the
     constant that scales the normalised waveform into int16. Make it an
     adjustable dB attenuation: no PipeWire volume API, no session-scoped sink
     IDs, no second-writer-against-the-mixer problem. STILL TX-PATH-SENSITIVE —
     design questions before build: behaviour if moved mid-transmission;
     per-band memory (that is what makes it replace the mixer habit); and how
     deliberately-lowered drive interacts with the drive-watch's PO
     expectations so turning it down does not read as the collapse signature.
     Live-TX-path rollout discipline applies (on-hardware acceptance procedure
     written first, passive checks before keyed ones).

  **Input CONTROL (as opposed to the bar): capture-side gain via the OS —**
  deliberately not staged; WSJT-X ships meter-only input (recollection, not
  cited) and the RX bar likely dissolves the need. Revisit only on a new
  operator ask.

- **P2 · `frontend/app/src/lib/map` + the FT8 decode stream — RX propagation overlay
  ("what can I hear right now").** _(From a 2026-07-26 discussion about PSK Reporter
  being slow. The key realisation: what people want from a PSKReporter map is two
  different problems, and SM already owns one of them outright.)_

  **The split.** "Who can I HEAR" is first-party — SM decodes the band every 15 s and
  each decode carries callsign, SNR, frequency and time. "Who can hear ME" is
  unknowable locally and is the only thing an aggregator uniquely provides. **This
  item is the RX half only**; the TX half is separate and deferred (see below).

  **Why it is attractive.** Zero external dependency, zero rate limit, **zero
  bandwidth** — which matters more here than anywhere: 7Q8AC is Malawi/offline-first
  and the whole smcloud stamp-drift arc was about not burning the link. It also
  refreshes every slot, and for your OWN station it is more accurate than PSK
  Reporter, which is aggregated and delayed.

  **What already exists** (so this is assembly, not new machinery): `WorldMap.svelte`
  draws great-circle arcs from an origin with per-endpoint hover tooltips and
  zoom/pan; `MapArc` already carries `from`/`to`/`key`/`label`/`color`;
  `ft8Enrich.svelte.ts` already resolves country/DXCC/worked-before per callsign; the
  `ft8-decode` SSE already streams every decode to the SPA.

  **The real limitation, worth knowing before it looks like a bug:** a grid only
  appears in CQ and ANSWER messages (`CQ W1ABC FN42`, `7Q5MLV DL9UW JO41`). A plain
  in-progress exchange (`K1ABC W9XYZ -07`) carries no grid, so only stations whose
  grid has been seen — or can be enriched — are plottable. Expect a partial picture,
  biased toward stations calling CQ.

  **Design points when picked up.** (a) A propagation overlay and the contacts map
  tell DIFFERENT stories — QSOs made vs paths open now — so they want a layer toggle
  rather than one merged surface. (b) Time window (rolling N minutes) and what decays:
  age-fade, or drop. (c) Colour by band or by SNR. (d) Fail-soft per the
  enrichment-never-blocks invariant: a missing grid or a failed lookup drops that
  station, never the overlay or the map.

  **The TX half, explicitly NOT this item.** "Who is hearing me" needs an external
  source. Options, best-first for this station: PSK Reporter's **query API**
  (`retrieve.pskreporter.info`, plain HTTP so no new dependency, but aggressively
  rate-limited and 503s under load — the observed slowness); a PSK Reporter **MQTT
  feed**, which would push filtered reports instead of polling and would be far
  cheaper on the link — **UNVERIFIED, confirm it exists before designing around it**,
  and it costs an MQTT client dependency; **WSPRnet** (good API, WSPR only, says
  nothing about your FT8 signal); **RBN** (CW/RTTY only). Whatever is chosen it should
  be **on-demand** — a "who's hearing me?" button doing one fetch — not a background
  poll, for both the rate limit and the bandwidth. Note `internal/pskreporter` is
  **upload-only** today (IPFIX over UDP); nothing in SM currently reads from them.

  **Related items already in this file:** the P3 "propagation / conditions panel
  (external online data source)" is the TX-half/space-weather cousin; the
  spot-submitter registry generalises the OUTBOUND side; ADR 0053's inbound DX cluster
  shares the "external live feed onto the map" plumbing but is a different data shape
  (human spots, not automated reception reports); and the smcloud "am I being heard?"
  pile-up status idea is the long game — if SM Cloud ever has several operators
  uploading decodes, SM has its own reception network that nobody else can copy.

- **P2 · `internal/forwarding/smcloud` (+ `qrz`) — a 401 is classified TERMINAL, so
  rotating the smcloud bearer token strands every upload attempted during the
  cutover.** _(Found 2026-07-26 while working out the impact of the ADR 0040 token
  rotation — the pre-Phase-2 gate. Not yet hit in anger; the rotation hasn't been
  done.)_

  **Mechanism, traced end to end.** `smcloud.classifyHTTPStatus` retries only
  408/429/5xx; every other 4xx — "notably 401 (bad token) and 400 (malformed)" — is
  `forwarding.OutcomeTerminal`, and the worker's `OutcomeTerminal` branch calls
  `markFailed`. A failed row does NOT retry. `qrz.classifyHTTPStatus` uses the same
  matrix (the smcloud comment says so, and the code matches).

  **Why it bites on rotation.** The token lives in two places that must change
  together — `SMCLOUD_TOKEN` in `/etc/smcloud/smcloud.env` (read at boot) and the
  daemon's `forwarders.smcloud.credentials` (forwarders are built once in
  `spawnForwarderWorkers`) — so BOTH need a restart and there is unavoidably a window
  where one side has the new token and the other doesn't. Every QSO logged in that
  window 401s and is marked permanently failed. **An overlap is impossible:**
  `SMCLOUD_TOKEN_N` supports 32 tenants but `collectTenantPairs` refuses boot on
  duplicate tokens AND duplicate callsigns, so old+new for one tenant cannot coexist.

  **What already saves it, and what doesn't.** The hourly reconciler treats local as
  authoritative and re-enqueues cloud-missing rows (`EnqueuedUpserts`), with the ADR
  0038 forever-retry posture applying to heal traffic — so the DATA reaches the cloud
  within the hour unaided. What it does not repair is the `qso_upload` row: that stays
  `failed`, so the logbook SPA shows red for QSOs that are actually safe. Manual
  recovery is `POST /v1/forwarder/{name}/uploads` (the retry surface built for the
  ClubLog 403-era Terminal rows).

  **Operational workaround (no code):** rotate while NOT operating. No QSOs logged =
  no uploads attempted = no terminal rows, and the window stops mattering.

  **The open question — is terminal right here at all?** A bad token is an
  operator-fixable condition, not bad data. For QRZ/ClubLog terminal is correct: a 401
  means a revoked account at a third party, and hammering it is antisocial. For
  smcloud the operator owns both ends, the destination is on their own LAN/VPS, and a
  401 during rotation is a routine, self-resolving state. Options: (a) make 401
  transient for smcloud only — but then a genuinely wrong token retries forever under
  ADR 0038, which is cheap against your own host but noisy; (b) a bounded
  retry-with-backoff for 401 specifically, terminal only after N attempts; (c) leave
  it and rely on the reconciler, documenting the "rotate while idle" rule. Whichever
  is chosen, the classification wants to be **per-forwarder** rather than the shared
  matrix it is now — a self-hosted destination and a third-party API genuinely differ
  here.

- **P2 · `internal/qsoservice` + logbook SPA — FT8 duplicate-QSO detection and merge.**
  _(Diagnosed 2026-07-26 from the dogfood log + decode log; the partial mitigation
  shipped the same day.)_

  **The mechanism, proven on air.** A Call-CQ contact logs the moment its closing
  `RR73` transmits. If the partner does not copy that `RR73` they repeat their
  `R-report` — and once the contact is cleared, `pickAnswererLocked` only accepts a
  GRID answer, so those repeats are invisible. They give up, restart from the top
  with a grid answer, and are worked and logged AGAIN. Caught red-handed with the
  decode log on: **XE1GM repeated `7Q5MLV XE1GM R-07` eleven times** at −9..−13 while
  the sequencer, having logged and moved on, ignored every one.

  **Damage already in the log** (all 40m/30m/20m FT8, same day, minutes apart):
  AC8MR (5995/5998), KI2Y (5990/5992), KE4IHI (5982/5989) — plus XE1GM (6015), whose
  contact the other operator very likely does NOT hold. All were forwarded; QRZ
  issued two distinct record ids for AC8MR, so **QRZ does not dedupe these**. Across
  the whole log, 20 FT8 pairs match "same call+band+mode, same day, ≤30 min apart".

  **What is already done.** `confirmHold` (`internal/ft8/caller_sequencer.go`, shipped
  2026-07-26) keeps a completed Call-CQ contact listenable for one of the answerers'
  slots and re-sends the `RR73` to a partner still asking, bounded by
  `confirmResendLimit`. It NARROWS the window. It deliberately does not close it:
  once the budget is spent, a partner who heard none of our `RR73`s and restarts is
  worked again — by then they genuinely never received the roger, so on air that is
  CORRECT, and it is the only way they get their contact (operator-ratified
  2026-07-26; rationale at the code site).

  **So the residual defect is the second ROW, not the second QSO** — which is why the
  fix belongs at log level, not in the sequencer. Suppressing the re-work would deny
  a station its contact to keep our log tidy; the same recoverability argument
  retired the SPA's blocking dupe guard (now advisory) and settled the Group A/B
  split in `internal/ft8/finalrung.go`.

  **Since shipped (2026-07-26):** the confirm-hold above, plus `allow_duplicate` — the
  operator's explicit "work this station again" intent, pinned at arm time like the
  logbook, stamped on the CompletedQso and passed to `Submit` as `force`. Without it a
  deliberate repeat inside one minute hashed to the first contact's dedupe key and was
  silently discarded. So a repeat the operator ASKS for is now stored; the remaining
  gap is the accidental pairs the sequencer creates on its own.

  **Not every repeat is a defect — detection must not assume it is.** A naive "same
  call+band+mode twice" query flags three different things, and only ONE is a problem:
  (a) the accidental pair this item is about — the partner missed our roger, restarted,
  and was worked again; (b) a repeat the OPERATOR asked for (`allow_duplicate` is set —
  recorded intent, exclude it); (c) a repeat the PARTNER chose, by answering our CQ
  again later. (c) is ordinary operating, not a fault: they decided to call us. Time
  separation is a signal, not a rule — minutes apart reads accidental, hours apart
  usually does not.

  **The open work:** (1) DETECT the pair (same call+band+mode, same session, minutes
  apart — note `qsoservice`'s existing dedupe key is call+band+mode+freq+date+HH**MM**
  and correctly does NOT catch these; it exists to catch a re-submitted identical
  contact); (2) SURFACE it as a review list, not a modal — "these two look like one
  contact, here is the evidence"; (3) RESOLVE: keep both / merge into one / delete the
  orphan, with the delete propagating upstream via the existing
  `enums/upload/action` `Delete` (ClubLog `delete.php` is already wired).

  **Design points when picked up.** (a) ADIF `QSO_COMPLETE` already exists on
  `types.Qso` (`qso_details.go:30`, values Y/N/NIL/`?`) and is **never written
  anywhere** — the natural, exportable home for "we sent the roger, we don't know it
  landed". But marking it is INFERENCE from a later event, so prefer recording the
  observed link and letting the operator set the field, or a genuine second contact
  gets silently stamped as doubtful. (b) There is no back-channel in FT8 — you cannot
  ask the other operator anything — so the real reconciliation is passive and already
  exists: an unmatched row simply stays unconfirmed in LoTW/ClubLog/QRZ. SM's job is
  local visibility plus the delete path, not a protocol. (c) If the operator does want
  to ask, give them the two rows copy-pasteable; that is an affordance, not a feature.

- **P2 · Rig Control VFO-A/B surface — swap semantics · label click target · VFO-B
  refresh.** _(dogfood 2026-07-21 ×3, triaged as one item 2026-07-23 — same surface, one
  fix pass.)_ (a) **Swap semantics — needs a decision before building.** Clicking the
  non-selected VFO box runs `selectVfo` → `swapVfoLive` → `swap_vfo` (`SV;`), which
  exchanges A↔B contents rather than selecting a VFO; `rig.svelte.ts` documents the
  reason (the FTdx10 has no CAT command that moves the operating frequency onto a named
  VFO — `VS` toggles a flag only), so "select the other" was equated with "swap". The
  operator reads the result as "VFO-B retains the swapped freq, not the actual
  frequency". Either add a `VS`-based select op to the rigdef (and decide what "selected"
  then means for the per-VFO freq-step ops) or relabel the boxes so the swap is explicit.
  (b) **Label not clickable** — trivial: `RigPanel.svelte` puts the `VFO-{v}` label in a
  `<span>` outside the `<button>`, so only the frequency box is a target; make the
  label+box one control (one button, one accessible name — no nested interactives).
  (c) **VFO-B sometimes not rendered** — NOT reproducible from the code (both VFOs render
  from a `{#each}`, `FB;` is in the rigdef `READ` set, and `swapVfoLive` optimistically
  mirrors `vfoB` before the command), so it points at a refresh/`null`-reset path rather
  than the render. Needs on-rig observation: which VFO blanked, after exactly what
  action, and whether it self-recovered.

- **`PUT /v1/config` — wire `default_logbook.id` on PUT (P3 residual).** The
  omitted-blocks-zeroed data-loss half (M5) shipped 2026-07-04 (see backlog-archive);
  this is the leftover: `default_logbook.id` is still never copied on a PUT. Deliberately
  left — there is no logbook-switching consumer yet and it is NOT a data-loss path (an
  omitted id just isn't applied). Wire it (with active-row validation) if/when a
  logbook-switch UI lands. See `docs/reviews/internal-api-2026-06-14.md`.

- **FT8 accumulate-mode "duplicate rows" → slot-grouped display (NOT dedup).**
  Reframed 2026-07-02 (was "Rx Frequency shows duplicate rows when feed ≠ single").
  The apparent duplicates in `accumulate` feed mode are *legitimate* — the same
  station decoded across multiple 15 s slots — so the fix is **presentation (group
  by slot)**, not dedup (dedup would hide the useful "still calling / SNR-trend"
  signal). **Discovery 2026-07-02:** Band Activity **already has a slot divider** —
  `slotSeparator` (`Ft8Panel.svelte` ~758) draws one on each slot change showing the
  slot's UTC **time + band** (e.g. `14:30:15 · 20m`), gated on
  `feedMode === 'accumulate' && !cqToTop`. So it's largely done for the
  accumulate + non-cqToTop case (the time is already there). **Decided (operator,
  remote): keep the Band Activity divider; Rx pane left OPEN pending dogfood.**
  **Todo next:**
  1. ~~Add **parity** (even/odd) to the `slotSeparator` label.~~ **DONE 2026-07-03.**
     The divider now reads `14:30:15 · 20m · even` (`slotParity(utc)`), and took a
     styling pass at the operator's request the same day: filled `bg-gray-400` bar,
     `text-gray-700`, left pad `pl-2`, top border removed. `Ft8Panel.svelte`
     `slotSeparator`.
  2. Confirm whether the "duplicate rows" were seen under **`cqToTop`** — that
     ordering **suppresses the divider** (decodes get reordered, so slot-grouping
     can't apply). Decide: WAI, or should grouping still apply under cqToTop?
  3. **Dogfood** the divider (with parity added) to get a better story, THEN decide
     whether the **Rx Frequency pane** needs its own grouping/time or is fine as-is
     (no dedup today; `rxDecodes` ~520, filters by worked-call / offset±tol). Rx
     stays open until then.

- **Multi-tab operating-lock — ownership + take-over (P2; awareness banner already shipped).**
  Filed 2026-06-27 (operator flagged as a real risk). The SPA is multi-tab: every tab
  subscribes to `/v1/rig/events` and any tab can `POST /v1/rig/command` — no "which tab
  owns the rig" concept. The *dangerous* cases are already prevented daemon-side: writes
  serialise at `cmdMu`; TX is single-flight (`keyMu`/`ErrTxActive`, shared by tune + FT8-TX)
  so two tabs can't double-key or steal the mic. The only residual is the **soft** hazard —
  a freq/band/mode change in one tab moves the one radio another tab is operating on.
  **Advisory awareness shipped 2026-07-04** (see backlog-archive): the daemon emits a
  `rig-clients {count}` SSE event on multi-tab transitions and the logging SPA shows a
  passive banner when >1 tab is open — enough for the single-op 7Q8AC ship. **Remaining
  (the real lock, P2):** a daemon-tracked owner so a non-owner's write is *rejected*
  (read-only), with explicit take-over. Design facts from the 2026-07-04 dig: there is NO
  client/tab identity on any rig endpoint today (all anonymous; `EventSource` can't send
  headers), so it needs a per-tab id (body/query on commands + a correlating handshake on
  the SSE), a new `ErrNotRigOwner` gate in `SendCommands` (mirroring the `ErrTxActive`
  check + 409 mapping), a `POST /v1/rig/control` acquire/release/take-over endpoint, an
  owner-broadcast SSE event, and lock UI in the logging SPA (only SPA that controls the
  rig). Only worth it when multi-tab / multi-op is real (e.g. alongside a 2nd operator or
  smcloud). Related dogfood notes (same root): "Next during TX moves on mid-transmit",
  "currently-worked station still clickable in Band Activity".

- **`internal/iocdi` contract hardening (concurrency + build-time validation).**
  Filed from the `internal/iocdi` review (2026-06-19, M1 + M3 + M4); deferred
  because the daemon registers single-threaded then builds, so none is exercised
  today — the fail-fast wins (M2 reject duplicate/invalid registration) + L1/L2
  shipped 2026-06-19. **M1:** registration isn't transactional — dependency
  discovery + the `built` check happen before `regMu` is taken, so a concurrent
  `Register`/`Build` can race the dependency map or accept a post-build bean. Fix:
  do the whole register transaction (check `built` → discover deps → validate →
  mutate both maps) under one lock; add a race test. **M3:** `requiredDependency`
  stores one type per bean ID, so two fields sharing a tag ID with different types
  lose one; and unexported/incompatible tagged fields are silently skipped, so
  `Build` can succeed with a nil `di.inject` dependency that fails at first use.
  Fix: store dependency edges per-field and validate EVERY tagged field at build
  (settable, bean exists, assignable) — treat tagged-but-unset as a build error.
  **NB before doing M3: confirm the current cmd/smd wiring has no latent
  unsatisfied tag, or it will turn into a startup failure.** **M4:** `Build` runs
  user `Initialize()` while holding `regMu` (an initializer that resolves/registers
  would deadlock), and a failed initializer leaves `built=false` so a retry re-runs
  the already-succeeded ones. Fix: release the registry lock before invoking
  initializers, add a `building` state, and document/enforce initializer
  idempotence. See `docs/reviews/internal-iocdi-2026-06-19.md`.

- **`/v1/hardware` per-direction audio availability + enumeration caching.**
  Filed from the `internal/hardware` review (2026-06-19, M2 + M3), deferred to the
  **config-SPA workstream** (the unbuilt consumer). **M2:** `AudioDevices` returns
  `(capture, playback, err)` but errors on EITHER direction's failure, and the
  handler collapses that to `audio.available=false` with both lists empty — so a
  playback-enumeration failure hides a working capture list (and vice versa).
  Fix: surface per-direction availability (e.g. `capture_available` /
  `playback_available` on the `/v1/hardware` response, or populate the successful
  direction + log which failed). It's a Tier-1 `api-endpoints.md` wire change, so
  shape it WITH the config SPA that consumes it. **M3:** `/v1/hardware` does
  uncached live OS/audio enumeration on every request (a fresh malgo
  context per call), with only the default 128-wide request limiter — a buggy SPA
  tab / retry loop can spin up many audio contexts. Fix: a short-TTL cache or
  singleflight around enumeration (+ maybe a lower per-route cap), with a refresh
  path if the picker needs immediate hot-plug detection. Both are hardening for
  the config SPA; the M1 wrong-codec safety guard + L2 labels shipped 2026-06-19.
  See `docs/reviews/internal-hardware-2026-06-19.md`.

- **CI-V `sets_state` value-compatibility validation.** Filed from the
  `internal/cat` review (2026-06-19, L1). `ValidateRigDefinition` rejects a CI-V
  `sets_state` that names no State marker, but it does NOT verify the command's
  *encoding* can populate that marker — so a future CI-V rigdef could pass
  validation with a mismatched pair (a `bcd_freq` command setting a mode tag, a
  `bcd_power` command setting `MAINMODE`, or a valueless command declaring
  `sets_state`), and the wait-for-ACK path (ADR 0034) would then synthesize the
  wrong/empty state push after a successful ACK. NOT an active bug — the shipped
  IC-7300 rigdef's `sets_state` pairs are all correct (pinned by
  `TestCommandSetsState`). Deferred until external/operator rigdef loading is real
  (`RegisterExternalDir` is still a stub), since that's when an unvetted rigdef
  could actually reach this gap. Fix: extend CI-V validation so `sets_state` is
  encoding-compatible (`bcd_freq`→BCD-freq marker, `bcd_power`→BCD-level marker,
  `mode_seq`→marker whose mapped values include the mode literals; valueless
  commands can't declare `sets_state`), with negative tests per incompatible pair
  + a positive test per shipped IC-7300 `sets_state` command. See
  `docs/reviews/internal-cat-2026-06-19.md`.

- **Configurable session-email subject + body (formatting tags).** Flagged
  2026-06-17 as multi-operator interest grows — a QSL manager receiving logs from
  several operators benefits from operator-tailored, distinguishable mail. Today the
  subject (logbook-callsign-prefixed default) and body ("ADIF for this session
  attached." / "Contains N QSOs." / "Generated by …") are **hardcoded** in
  `sessionEmailSubject` / `sessionEmailBody` (`internal/api/handler_session_email.go`).
  Let the operator configure both via **templates with substitution tags** — e.g.
  `{callsign}`, `{count}`, `{date}`, `{logbook}`, `{to}` — stored in `config.json`
  (durable per-operator setting, per the settings-in-config rule; natural home is
  alongside the SMTP block) and edited from the SPA email/Settings surface. Keep the
  current hardcoded strings as the **defaults**. Design points when picked up: the tag
  set + a *small, safe* substitution (no general templating engine — just `{tag}`
  replacement); and whether a **per-send free-text note** (a transient SPA input,
  distinct from the durable template) is also wanted. The callsign-prefix + QSO-count
  shipped 2026-06-17 as the hardcoded first step toward this.

- **CAT poll mode (rigdef-configurable) — deferred (ADR 0034).** The bridge read
  model (ADR 0019) is push-only: the rig broadcasts on change (Yaesu/Kenwood AI,
  Icom CI-V Transceive). Designed but deferred: an optional rigdef `poll` block
  (interval + read command) that sends `READ` on a timer and flips liveness so a
  *missed poll* — not silence — is the disconnect signal. For Icom we instead
  document **CI-V Transceive ON** as an operator prerequisite. Add the `poll`
  block (additive; push-only rigs unaffected) if an operator can't keep
  Transceive on, the bus contends with other software, or state Transceive
  doesn't broadcast surfaces. Decide one-missed-poll vs N-strikes then.

- **FT8 offset picker — daemon-side no-overlap snap + click-anywhere.**
  `Ft8OccupancyStrip` offers daemon-vetted clear offsets as discrete markers
  today; clicking arbitrary spectrum (with a daemon-side snap to the nearest
  no-overlap slot) is future work.

- **FT8 semi-auto response to a session watch-list — SET ASIDE 2026-06-12 in favour
  of the caller-side work-stack; hunter/auto-fire variant stays UNDER CONSIDERATION
  (grayline, NOT decided).** The 2026-06-12 stack discussion concluded the
  **caller-side pile-up work-stack** (feature above) is the path: it delivers the
  "curate calls + work a queue" benefit while staying attended — the operator pops
  each contact, no auto-fire. The watch-list's *only* unique value was the **hunter**
  case (auto-respond to a wanted CQ inside the one-slot reply window, faster than a
  human can click), and that auto-initiation is exactly what crosses into
  daemon-initiated operation. So it is parked, not dropped; the original idea is
  preserved below. Idea: the operator manually selects a set of callsigns into a
  **session-bound** list and clicks **'Go'** to arm it; when one of those calls
  then appears as a CQ in a decode, the daemon responds in the **immediate next
  slot** — using the ADR 0032 synchronised/truncated send to hit the tight
  end-of-decode → next-sequence window a human can't reliably click within.
  Technically a small delta on ADR 0032: the only new piece is swapping the
  per-QSO click for a watch-list match as the initiation trigger; sequencer,
  timing, and off-ramps already exist.
  **Attended framing + guardrails (the operator's design):** the list is
  session-bound (cleared on session end — never persistent, never unmanned), the
  operator manually picks the targets and gives an explicit 'Go', is present and
  supervising, can abort instantly (Abandon / Disarm), and there is no auto-CQ
  cycle — the human supplies the operating *intent* (the selection + 'Go'); the
  software only covers human reaction time in the reply window.
  **Open regulatory question (acknowledged grayline, still being thought through):**
  does pre-authorising a batch + auto-responding count as *attended* (the operator
  initiated the intent, analogous to WSJT-X "Call 1st") or does it cross into
  *daemon-initiated* operation (QEX §9 forbids robotic/unattended; SM's
  operator-initiated stance)? Not resolved — recorded to keep thinking. **If ever
  built, it must be framed as attended-assisted; public docs must never present
  it as automatic operation.** Note this item is the one place where the
  operator-initiated line is genuinely at issue: a watch-list match, not an
  operator click, would be the thing that STARTS a contact — which is why it sits
  differently from ADR 0059 auto-work (that continues a run the operator already
  started). See the scope note at the end of this file, CLAUDE.md's FT8 bullet,
  and `docs/ft8.md`.

- **FT8 callsign ignore list.** An operator-maintained list of callsigns to
  suppress in the FT8 view — already worked, not being sought, known nuisance, etc.
  Listed calls should be hidden (or clearly de-emphasised) in Band Activity and not
  offered as answerable CQ rows. Distinct from the existing *automatic*
  worked-before tint (`ft8Enrich`): this is a **manual** list with mixed reasons,
  so keep it separate from worked-detection. Open design points: (1) storage — a
  non-session setting → daemon `config.json` (per the settings-in-config rule), with
  an add/remove UX in the FT8 view; (2) behaviour — hide entirely vs grey-out vs
  just non-clickable (lean: hide, with a toggle to reveal); (3) match semantics —
  exact callsign vs prefix/wildcard. Whether it also feeds AP-hint *de*-prioritising
  (ADR 0025) is a later question, not v1.

- **FT8 — work type-4 compound calls + free-text messages.**
  **▶ PRIORITISED NEXT (2026-07-14, operator directive):** build the **reduced type-4
  hashed QSO ladder** so SM can complete a contact with any nonstandard call (`/D`,
  `/M`, prefix-compound `PJ4/NA2AA`). Empirically confirmed 2026-07-14 (probe against
  go-ft8 v0.7.0): a `/D` suffix packs **type-4**, identical to a prefix-compound —
  `CQ` / `RR73` / `73` encode (partner hashed to `<...>`) but the `CQ`+grid /
  opening-call+grid / report / rogered-report rungs are **rejected** ("unsupported
  standard message" — no type-4 grid/report form), so SM's standard grid→report→73
  ladder can't be walked and the sequencer fails soft (`ErrTxBadMessage`). `/P` is the
  exception — it packs *standard*, carries grid+report, and already works end-to-end.
  **Design + alternatives recorded in ADR 0048** (spelled-partner matching · degraded
  FD-style logging · FD-clone isolated path · answer+work-only v1). The build shape is
  in the type-4 sub-bullet below.
  The answer-a-CQ,
  work-a-caller, and Call-CQ paths work any station whose exchange encodes as a
  **standard structured FT8 message**; the sequencer defensively **skips** anything
  else (the dynamic "reply does not encode" guard — every site tries
  `goft8.EncodeStandardMessage` and treats an error as `ErrTxBadMessage`, in
  `caller_sequencer.go` / `sequencer.go` / `work_sequencer.go` / `EncodeWaveform`).
  - ~~**Standard `/P` variant.**~~ **SHIPPED 2026-06-18** with the **go-ft8 v0.3.4→v0.3.5**
    bump — `EncodeStandardMessage` now accepts the standard `/P` variant, so the dynamic
    guards pass it through with **no SM code change** (the encode-check seam was designed
    for exactly this). Proven offline: `internal/ft8/modulate_test.go`
    (`TestEncodeStandardMessage_Portable` + `TestModulate_RoundTrip_Portable`).
  - **type-4 compound / nonstandard calls (`PJ4/NA2AA`, `K1ABC/4`, …) — go-ft8 v0.5.0
    LANDED the encode/decode (2026-07-05), so this is now buildable, not blocked.** State
    after the v0.5.0 bump (session 199): **RX works** — a type-4 CQ (`CQ PJ4/NA2AA`) and
    directed type-4 (`PJ4/NA2AA <...> RR73`) now decode and reach Band Activity (round-trip
    tested, `TestCompoundCQ_Decodes`). **TX does NOT yet run a full QSO:** type-4 carries
    only `CQ`/`RR73`/`73` with the partner call HASHED to `<...>` — there is **no type-4
    grid/report form** — so SM's standard grid→report→R-report→73 ladder can't be walked
    with a prefix-compound partner, and `StartQso`/`StartQsoFd` correctly fail soft
    (`ErrTxBadMessage`). **BUILT 2026-07-16 (ADR 0048):** a distinct reduced
    `bare-calls→RR73→73` ladder (`type4.go` / `type4_sequencer.go`). **No 22-bit hash
    table** — the earlier "resolve `<...>` back to the real call" idea was dropped: go-ft8
    exposes no decoded-hash integer to match against, and the partner always spells itself,
    so matching is on the **spelled** partner (ADR 0048 weighed and rejected a persistent
    decoder / SM-side `hashCall`). Only on-air validation remains.
    `TestPrefixCompound_EncoderBoundary` pins the encoder boundary and will
    flip if a later go-ft8 adds the grid/report forms. **`/R` suffix:** encodes in v0.5.0 but
    go-ft8 does NOT yet DECODE it ("RTTY Roundup … not yet unpacked"), so it fails the
    round-trip gate — do not transmit `/R` until decode lands. **Free text** (71-bit) encode +
    an entry UX is still separate work. Capture point: `docs/ft8.md`; see ADR 0029 (the
    `EncodeStandardMessage` seam).

- **FT8 caller-side sequencing — BOTH flows SHIPPED; only on-air validation remains.**
  `auto_first` (Call CQ, ADR 0033) shipped 2026-06-12 and the operator-pick experience
  shipped 2026-06-17 as the SPA-owned pile-up stack (+ up-arrow reorder, session 192) —
  which **supersedes** the never-built daemon `operator_pick` Call-CQ mode (501-rejected,
  not needed). The one true remainder is **on-air validation** of the caller side + the
  session-tab logging (unit-tested + offline-encode-verified only). Detail below; this was
  the gap on 2026-06-12 when a real 7Q pile-up (DK8IF / DL9UW / …) was unworkable.
  - **SHIPPED — `auto_first` (the WSJT-X "Auto Seq" mode):** the Operate-tab **Call CQ**
    button starts a sequenced session (`POST /v1/ft8/cq/start` → `Service.StartCallCq` →
    the `Sequencer` caller mode) that calls CQ, **auto-works the first answerer** through
    report → RR73, **logs it via the e4 sink**, then resumes CQ — looping until Abandon.
    Caller ladder: `internal/ft8/caller.go` (`CallerExchange`); driver:
    `caller_sequencer.go` (`onSlotCalling`); live role-aware ladder + "Calling CQ…":
    `Ft8MsgPanel`. Config: `ft8.tx.caller_answer_mode` (default `auto_first`). **Needs
    on-air validation** — unit-tested + offline-encode-verified only so far.
  - **SHIPPED 2026-06-28 — `auto_strongest` answerer selection + Settings-tab knob.**
    `onSlotCalling` now picks the **highest-SNR** encodable answerer in the slot (clear
    the loud ones first) when `caller_answer_mode == auto_strongest`, else the first by
    decode order (`auto_first`). Surfaced over `/v1/config` as `ft8_caller_answer_mode`
    (presence-aware; only the two auto answer modes accepted, `operator_pick`/junk →
    400) and editable from the logging SPA's **FT8 Settings tab → Call CQ → Answer**
    (First answerer / Strongest signal). Pile-up drain stays **FIFO** — the knob governs
    only the hands-off auto-answerer. Tests: `caller_sequencer_test.go`
    (auto_strongest-picks-highest / auto_first-picks-first), `handler_config_ft8_test.go`,
    `types/ft8_test.go`. **Needs on-air validation** like the rest of the caller side.
  - **SHIPPED 2026-06-17 — pile-up callsign stacking (the operator-pick experience, as
    an SPA-owned FIFO).** Realised the "pick which caller to work" need via a different
    (operator-chosen) shape than the original daemon `operator_pick` Call-CQ mode:
    **Ctrl/Cmd+click** a calling-you decode in Band Activity to push it onto an in-memory
    **FIFO** (`ft8PileupStack.svelte.ts`), worked **oldest-first**; the Operate view
    **drains** it via the existing work-a-caller path (`StartWorkCaller`) whenever the rig
    is armed+idle, advancing as each contact completes, while the operator keeps adding.
    Capture (Ctrl+click) is available in **any state** (mid-QSO, disarmed — pure capture,
    no TX), which is the whole point: callers are only visible in your RX parity and the
    work-now click is gated on armed+idle, so you grab them when you see them and the SPA
    works them when it can. Drawer (`Ft8PileupDrawer.svelte`) in the Operate tab + a depth
    badge on the tab; **Abandon pauses** the drain (queue kept, Resume on the drawer);
    Clear-all + per-entry remove. **SPA-only** — daemon untouched (reuses work-a-caller +
    the `ft8-qso` idle signal). In-memory (erased on tab/browser close), mirroring
    `callsignStack`. This **supersedes the daemon `caller_answer_mode: operator_pick`
    Call-CQ mode** (still `501`-rejected at `StartCallCq`; not needed — the stack gives
    operator-chosen working for *anyone* calling you, whether or not you called CQ).
    `auto_first` Call-CQ stays as the hands-off "call CQ + auto-work answerers" loop.
  - **Operator-initiated either way:** the operator starts it by calling CQ, and Abandon
    stops it instantly; **no auto-CQ cycle, no auto-fire-on-watch-match** — which is why
    this **supersedes the auto-responder framing** of the watch-list item above. (What it
    does NOT claim: that the operator stays. A Call-CQ run keeps working answerers until
    Abandon or until the FT8 view closes — see the scope note.)

- **FT8 Call-CQ: on contact abandon, work a live answerer instead of resuming CQ
  (dogfood 2026-07-17, P2). LAYER 1 BUILT 2026-07-17; layer 2 (answerer pool) open.**
  When the worked answerer goes silent and the contact hits `max_repeats`,
  `onSlotCalling` used to drop it and transmit CQ — other stations answering us were
  lost because the answerer scan (phase 1) runs only when `s.caller == nil` at the TOP
  of a slot. (The RR73 completion path was always fine — next slot's phase-1 scan picks
  a new answerer without an extra CQ.) **Layer 1 (shipped):** the max-repeats drop now
  re-runs the answerer pick (extracted `pickAnswererLocked`, honours the answer-mode
  knob + the M2 encodability skip) over the CURRENT slot's decodes and replies in the
  same slot; CQ only when nobody else is calling. Test
  `TestCallerSequencer_AbandonWorksLiveAnswererSameSlot`; needs on-air validation with
  the rest of the caller side. **Layer 2 (open):** callers heard DURING the failed
  contact's retry cycles (~2.5 min at 5 repeats) still leave no memory — accumulate
  grid-answer decodes from non-worked stations during phase 2 into a recency-bounded
  candidate pool (last K their-slots; prefer most-recent/strongest; validate
  encodable), picked on abandon and after RR73 (tail-enders). The pool would also feed
  the parked "Call-CQ waiting feedback" SPA item. Surface: `internal/ft8/caller_sequencer.go`.

- **Spot-submitter registry — generalize when a 2nd destination lands (e.g. DX cluster).**
  PSK Reporter is the first "submit what I heard" destination. A **DX cluster spot submit**
  (telnet/TCP to a cluster node, e.g. announce a worked/heard DX) would be a second — at
  which point it's a natural fit to extract a **spot-submitter interface + registry**,
  mirroring `internal/forwarding` (Forwarder interface + `init()`-registered destinations)
  and the lookup-provider chain: one decode-sink fans out to N registered submitters, each
  with its own config/enable/transport. **Deliberately NOT done now** ("build specific, not
  generic" — the v1 `internal/adapters` cautionary tale): one destination doesn't justify a
  framework, and the cluster transport/semantics (TCP, often selective/manual announce) differ
  enough that the abstraction should be designed against **two** real implementations, not
  one. The current `pskreporter.Service` (AddSpot/Flush/Start/Stop) is shaped so the extraction
  is clean when the DX-cluster submit actually arrives. Trigger: that second destination.

- **FT8 main-panel footer → an info strip (rehome the offset readout there).** The
  bottom-of-main-panel footer now holds "Next slot in Ns · even/odd" (added with the
  countdown move). Grow it into a small **info / status strip** and relocate the
  **"Offset N Hz ±tol"** readout into it — today that's the `rxCaption` under the Rx
  Frequency column (`Ft8Panel.svelte` ~line 282). **Pairs with the Rx-pane redesign
  above:** that pane becomes the worked-station enrichment card / blank empty-state, so
  its caption ("Offset N Hz ±tol" / "No offset selected") needs a new home — this strip
  is it. (The caption's "Following <call>" variant is subsumed by the enrichment card,
  so really only the idle offset readout moves.) Net: one tidy status strip along the
  panel bottom — next-slot countdown · parity · selected TX offset — leaving the three
  top panes (Main Freq / Band Activity / Rx-now-enrichment) uncluttered.

- **Install + first-run onboarding is too high-friction for non-Linux operators.**
  Filed from the 2026-06-23 clean-DB dogfood deploy. `docs/install.md` walks the
  operator through detailed manual rig configuration; for a non-experienced Linux
  user that's too much. North-star (KISS): the daemon discovers serial/audio
  devices and offers friendly labels, the SPA picks — the operator never
  hand-edits `config.json` or types hardware ids (extends the rig-profiles
  direction, ADR 0028). Scope is the whole onboarding arc — install, first-run
  rig setup, identity — not just the doc. Design initiative; pairs with the
  fresh-install config-defaults bug above.

- **Download-site install page (derive from `docs/install.md`).** Filed
  2026-06-23. The operator manual deliberately omits install/uninstall (ADR
  0036 arc starts at First Run; the embedded manual is unreachable pre-install
  anyway), so the public download site needs its own install page. Make it a
  lightly-edited operator-friendly rendering of `docs/install.md` (§1–3 install
  + enable, §10 uninstall) so the two don't drift — install.md stays the single
  canonical source. External/website work, out of this repo.

- **Clear the queued-upload backlog for a forwarder (esp. a disabled one).**
  Filed 2026-06-23. The shadow side of ADR 0022's enqueue-by-presence: a
  configured-but-disabled forwarder silently accumulates `pending` `qso_upload`
  rows, so *enabling it later flushes the whole backlog*. The operator needs a
  deliberate way to **discard that queue** ("don't upload the backlog, start
  sending from now"). Design points:
  - Daemon op: purge `pending`/`failed` `qso_upload` rows for a named forwarder;
    **never touch `uploaded`/`success` rows** (those are real upload history).
    Per-forwarder scope (not global). Safe for a disabled forwarder (worker idle,
    no race); for an enabled one, coordinate with the in-flight batch.
  - UX home: forwarder management in the **config SPA** — show "N queued for
    upload · [Clear queue]" next to each forwarder, with confirmation. Pairs with
    the enable/disable toggle (consider offering "clear queue?" when disabling).
  - Consider recording the purge in the audit trail (`qso_history`) so a cleared
    backlog is explainable later.
  - **Document the underlying behaviour for operators** (manual forwarding
    chapter, currently a stub): adding a forwarder queues *future* QSOs; disabling
    doesn't stop the queue growing, only the sending; this is the lever to empty
    it. The inverse (bulk-forward an existing log to a newly-enabled service) is
    the separate backfill feature.

- **Operator / user profiles — selectable op-identity bundles.**
  Filed 2026-06-24. Surfaced during the config-SPA Station-tab split: the
  **operational op-identity pair** (`Operator` callsign + `Operator Name`, and
  possibly `Owner's Callsign`) stays in the logging SPA precisely because it
  swaps per session — in a contest or multi-op you change operator mid-event.
  The future enhancement: turn that pair into **named, selectable profiles** the
  operator picks at session start instead of retyping (e.g. a dropdown of saved
  operators on the logging My Station / QSO surface). Daemon owns the profile
  list (config.json), SPA picks. Keep the current single-pair fields working;
  profiles layer on top without foreclosing today's split. Not scoped/started —
  a direction, not a commitment. Depends on the config-SPA workstream existing
  first (that's where profile CRUD would live).

- **2nd callsign-enrichment provider (HamQTH fallback link).** Filed from dogfood
  2026-07-13 when the live re-enrich flow was validated but couldn't name-repair
  **RG6S** — the callsign isn't on QRZ.com, and QRZ is the only callsign-class
  provider configured (not a flow bug; no source had the data, and the country layer
  still repaired country/dxcc/zones via hamnut). The enrichment orchestrator already
  runs a provider **chain** (`o.Chain`) with QRZ as its only link, so a second
  callsign provider (e.g. HamQTH, free tier) as a fallback would catch some
  QRZ-absent calls (Russian/CIS calls are a common QRZ gap). Needs: a provider client
  + chain config + the ADR 0017 cache semantics it already gets for free. Flaky-link /
  Malawi-relevant (more sources = more complete offline records). Untriaged detail was
  in the inbox 2026-07-13.
  **Candidate providers + API docs (dogfood 2026-07-24, capture for evaluation — NOT a
  build commitment):** HamQTH (`hamqth.com/developers.php` — callsign/callbook lookup is
  **XML-only** (name/QTH/grid via an authenticated session key); its JSON endpoint returns
  DXCC data only, which the chain discards — so the enrichment link must speak XML. The
  named free-tier candidate.) · QRZCQ (`qrzcq.com/page/developers`) · QRZ.digital
  (`qrz.digital/api/swagger-ui/index.html` — REST/OpenAPI). When picked up, evaluate each
  for coverage of QRZ-absent calls (CIS especially), licence, and rate-limit terms. Note
  some (HamQTH, QRZCQ) also host **logbooks**, so a provider could double as a forwarder
  destination — decide the role (enrichment lookup vs upload target) per service, which is
  itself part of the deferred investigation.

- **smcloud "am I being heard?" pile-up status site (P3 · community phase, capture-don't-build).**
  Filed 2026-07-11, refined across that session. When running a pile-up, SM (local)
  publishes to a PUBLIC website; a caller opens the page, types their callsign, and
  sees their **status** — no SM install needed caller-side. **Publish STATUS, not
  queue rank** (the critical reframe): data source = the DECODE FEED (everyone SM
  decoded calling the op this session), NOT the operator's curated Ctrl-click stack
  (most callers aren't in it → stack lookup returns "not found" for the common case).
  States: **worked ✓** / **heard — not yet worked** (decoded this session) / **not
  heard**. Avoid a "#N position" — FT8 pile-ups aren't ordered queues, so a rank
  promises a fairness the op won't honour. **Unique niche:** ClubLog Live Stream shows
  the DX's LOG (worked-✓ half); PSK Reporter shows where MONITORS heard you; NEITHER
  shows "the DX's own receiver is hearing you" — that middle state is the gap SM can
  own (it has the decode feed + session log locally). Also show **on-air + frequency**
  now ("7Q8AC is on-air: 14.074 MHz (20m FT8)") so the page is discovery, not just
  status — data is already local (CAT dial freq + FT8 band/mode). **Cost:** local side
  mostly exists; new work = smcloud (a small endpoint taking per-slot snapshots
  `{dx_call, on_air, freq, band, mode, decoded[], worked[]}` + a lookup page
  `?dx=7Q8AC&me=G4XYZ`) — ~weekend MVP, not a platform. **Caveats:** best-effort
  publish (enrichment-never-blocks discipline — a failed push never touches the QSO);
  FLAKY-LINK staleness is the real risk (a stale freq/decode misleads worse than
  nothing → a prominent "updated Ns ago / STALE" stamp is mandatory, and an explicit
  active-vs-idle concept: idle shows "last operated Xh ago"). **Distribution:** embed
  as a QRZ.com bio `<iframe>` (callers reflexively open qrz.com/db/<dx>) — MAKE-OR-BREAK
  UNKNOWN = whether QRZ permits iframes in bios AND whether HTML-bio editing needs paid
  QRZ XP; fallback = a prominent link/button. **Notifications guardrail:** do NOT
  auto-email callers (spam — GDPR/PECR + CAN-SPAM + QRZ ToS + deliverability; link-only
  email doesn't fix the unsolicited SEND, and the QRZ-embed already puts the link where
  callers go). The only legitimate shape = CALLER-initiated opt-in ON smcloud ("notify
  me when 7Q8AC works me", 1:1, transactional-after-a-real-QSO, with unsubscribe).
  Sits with the SM Cloud P1 designed workstream (ADR 0040) + the P4 community bucket;
  orthogonal to the frontend/app daily-driver work. Full note: `docs/dogfood-inbox.md`
  2026-07-11.

- **Contacts map — background-tab staleness (P2 · frontend/app; dogfood
  2026-07-18; BUILT same day — `visibilitychange` → immediate `refresh()` in
  `mapData.svelte.ts` (hidden→visible edge only, listener removed on
  teardown, MapView test drives the edge in jsdom); operator confirmed the
  repro before the fix landed. Needs a dogfood re-check after redeploy).**
  A map tab left open but HIDDEN (same-window background tab)
  does not update, and shows stale data when re-activated (observed: 6 QSOs /
  4 80m arcs frozen). Verified gaps: no `visibilitychange` handling in
  `mapData.svelte.ts` or `api/log-events.ts`; the 300 ms `scheduleRefresh`
  debounce runs on a timer browsers throttle in hidden tabs; SSE may also die
  silently in a long-hidden tab (browser-native reconnect only). Fix
  direction: on `visibilitychange` → visible, run an immediate `refresh()`
  (idempotent, cheap — the same head-refetch events trigger) and let it
  double as the liveness check; optionally skip refetch work entirely while
  hidden. NB the map's PRIMARY posture — own window on a second monitor — is
  unaffected (throttling keys on visibility, not focus), which is why this is
  P2 polish, not a P1 break of the feature's main use.

- **Contacts map — zoom/pan + station hover tooltip (P2 · frontend/app; dogfood
  2026-07-17, triage 2026-07-18; BUILT 2026-07-18 — uncommitted, needs a
  dogfood eyeball after redeploy: `lib/map/zoom.ts` pure transform/hit math +
  WorldMap wheel-zoom 1–16×/drag-pan/dblclick-reset/Reset-view button/stacked
  tooltip, 618-test suite green).** Two interactivity gaps on the shipped
  time-window map (`lib/map/engine.ts` + `WorldMap.svelte` + `MapView.svelte`),
  built as ONE item because they share coordinate machinery:
  (a) **Zoom/pan** — wheel/pinch zoom + drag pan. d3-geo already renders; either a
  `d3-zoom` transform on the render group or projection `scale`/`translate` updates
  with an engine re-render. All layers (land, grey line, arcs, legend markers) must
  ride the same transform; clamp the zoom range; provide a reset (double-click or a
  ⌂ button). (b) **Hover tooltip on a remote station** — hovering an arc endpoint
  shows the contact's details: callsign, band/mode, time, grid, distance + bearing
  (`lib/utils/bearing.ts` `pathInfo` already computes distance/bearing; the rest is
  in the plotted QSO set). Needs pointermove hit-testing — nearest endpoint within a
  px radius — plus a small positioned tooltip; overlapping endpoints at low zoom can
  list N contacts or show "N QSOs" (zoom disambiguates, another reason they pair).
  Build zoom first or together: hit-testing must run in the transformed screen
  space, so a tooltip built against the static projection gets reworked by zoom.
  Shared engine ⇒ the interactivity lands for the whole-log Dashboard map below too.

- **Whole-log Dashboard map (P3 · frontend/app; follow-on to the shipped time-window map).**
  The time-window contacts map SHIPPED 2026-07-16 (engine `lib/map/engine.ts` + reusable
  `WorldMap.svelte`; route `/map` — `MapView.svelte` + `mapData.svelte.ts` windowed fetch +
  `qso.*` live refresh; "Open map ↗" in the shared SessionPanel — full design trail in
  `backlog-archive.md`). The dashboard route (`App.svelte` `{:else}`) is still an empty
  placeholder wanting a first tenant; the whole-log map reuses the SAME render engine — the
  only new piece is the data source: a small aggregate endpoint **`GET /v1/logbook/{id}/map`**
  returning dedup'd plot coords (`[{grid,lat,lon,dxcc,cont,bands[],modes[],count}]`; 5.4k QSOs
  → a few hundred unique grids / ~150 DXCC → one tiny offline-friendly request), rather than
  paging the cursor API over a flaky link. NB the bespoke aggregate slightly tensions with the
  ADR 0043/0044 "compose existing + subscribe, resist aggregates" guidance — justified because
  a coordinate projection is a genuinely different shape than paginated rows and is the
  flaky-link-correct choice; **record the exception if built**. The per-QSO-origin refinement
  (roving/multi-site: per-QSO `my_gridsquare` origins instead of the single fixed `myGrid`)
  stays deferred with it.

- **SPA consolidation — one app shell (ADR 0044, post-ship).** Merge the three
  Svelte SPAs (`frontend/{logging,config,logbook}`) into one Vite + Svelte 5 app
  (`frontend/app/`) with a persistent shell — dashboard/status home, **Operate**
  (Phone/CW + FT8 as sibling modes over the shared session log), **Logbook**,
  **Settings** (the config surface; route stays `/config`), and a link out to the
  zero-JS **manual** (which stays separate per
  ADR 0036 — this is **3→1, not 4→1**). Drivers: the FT8/logging seam is wrong
  (FT8 uses logging but isn't logging — they're siblings over one session log);
  plumbing is triplicated and drifting (three `_helpers.ts`; the session-198
  fetch-timeout fix reached only logging's copy); theming/dark-mode is re-authored
  per app. It is the **client-side mirror of ADR 0043**'s per-surface `internal/api`
  split. Design is settled in the ADR; three sub-decisions **endorsed 2026-07-06**
  (History-API real-path routing [provisional] · lean status-home dashboard ·
  config-as-route) — with one open finer point: the **default landing view is an
  operator preference** (a `startup_view` config setting — dashboard / operate→FT8
  / operate→phone / last-used; dashboard stays the default), settled at build. Key
  constraints baked into the ADR: **per-route code-splitting
  is a requirement, not an optimisation** (one bundle now spans all surfaces — the
  7Q8AC link); the **theme system is built first** from logging's tokens as the
  baseline (utility *nomenclature* open to a rationalisation pass during the merge);
  **API endpoint count is unchanged** — usage simplifies (one hydration, one stream
  lifecycle, a natural first consumer for the deferred 0043 `qso.*` events spine) —
  and **resist a bespoke `GET /v1/dashboard` aggregate** (compose existing +
  subscribe, per 0043). **Subsumes** the three _UI cohesion_ items below (shared
  theme layer · UI themes + dark mode · version-in-tab-title): they become work
  *inside* the shell build, not separate cross-SPA passes. **Post-ship — gated
  behind the 7Q8AC release; do NOT open before the ship gate clears.** See
  `docs/decisions/0044-consolidate-operator-spas-into-one-shell.md`.

- **UI consistency across SPAs — shared theme layer.** _(Reframed 2026-07-06 by
  ADR 0044 — see "SPA consolidation" above: once the three SPAs become one app,
  the "lift logging's `@theme` into a file all three import" step is **absorbed**
  and the token-convergence sweep happens *inside* the single-shell migration. The
  measurements + safety-rail below stay accurate and load-bearing for that sweep.)_
  THIS IS THE LOAD-BEARING
  WORK — theming (dark mode / selectable palettes, see "UI themes + dark mode")
  is the *carrot*, not the task; converging on one token layer is what actually
  tidies the CSS, and it pays off even if a theme picker never ships.
  Filed 2026-06-24; sized + sequenced 2026-07-04. The logging SPA already has a
  genuinely good token layer in `frontend/logging/src/styles/app.css` — a Tailwind
  v4 `@theme` block of semantic colours (`surface`, `ink`, `line`, `focus`,
  `invalid`, `vfo-*`) plus a `@layer components` of shared classes (`.btn`,
  `.input-base`, `.toast-*`, `.tab-item`). The mess is that it's **half-adopted**:
  measured 2026-07-04, config = 275 raw palette colour classes / **0** tokens,
  logbook = 98 / **0**, and even logging still bypasses its own tokens ~217 times
  (79 token uses). So a dev moving between SPAs juggles two mental models — that
  inconsistency is most of the "Tailwind feels cumbersome" pain.
  **Safety rail that makes this mechanical:** logging's tokens were deliberately
  defined EQUAL to their raw Tailwind values (`--color-surface: #fff`, `--color-focus`
  = indigo-600, each mapping noted in a comment), so converting `text-indigo-600`
  → `text-focus` is a **visual no-op** — the whole sweep is diff-reviewable, not a
  redesign. **Order (de-risked):** (1) lift logging's `@theme` + `@layer components`
  into a CSS file all three SPAs import (one source of truth); (2) convert config
  onto it (biggest win — 275 raw / 0 tokens, visual no-op); (3) finish logging's
  own conversion (retire its ~217 raw classes) + do logbook. Do NOT big-bang all
  three mid-other-work, and don't lead with a picker UI. Steps 1–3 ARE the tidy-up;
  the theme toggle (see "UI themes + dark mode") is a thin follow-on once colours
  route through variables. Not now — flagged for a dedicated pass.

- **Operator email address — config Station tab field (needs a daemon home).**
  Filed 2026-06-24. Surfaced while building the config-SPA Station tab: there's
  no operator/station email field anywhere today. It **can't ride
  `logging_station`** — that block strictly follows ADIF, and ADIF has no
  `MY_EMAIL`. So a working field needs a small daemon-side home; the leading
  option is a new SM config string **`operator_email`** (served on `/v1/config`
  GET, set via PUT, echoed like the rest), with the input dropped into the
  Station tab's Postal-address section. Rejected reusing `mailer.default_recipient`
  (that's "where session-log emails go," not the operator's contact address —
  don't overload). Related context (operator): **QRZ.com exposes an email address
  and uses it to populate the ADIF `EMAIL` field** — note that ADIF `EMAIL` is the
  *contacted station's* address (already modelled at `types.ContactedStation.Email`
  + filled by QRZ enrichment), distinct from the operator's *own* email this item
  is about. Worth checking, when this lands, whether the operator email should
  also flow anywhere outbound (e.g. forwarder profiles) or stays purely local
  contact info. Deferred — no concrete consumer yet.

- **Outbound UDP telemetry stream (WSJT-X-protocol-compatible).**
  Filed 2026-06-24, prompted by an external query about feeding WSJT-X data into
  Prometheus/Loki/Grafana. Idea: SM **emits** a UDP datagram stream of its FT8
  decodes, QSO-logged events, and rig status, the way WSJT-X's UDP Message
  Protocol does — so the existing ham tooling ecosystem (GridTracker, JTAlert,
  Grafana/Prometheus exporters, the operator-observability stacks people build)
  works against SM as the FT8 engine for free. Turns SM from a walled garden into
  a first-class citizen of the UDP-consuming tooling world; a real interop
  differentiator.
  - **Building blocks already exist:** `internal/events` (the hub) + the SSE
    surface (`/v1/rig/events`, `/v1/ft8/events`, `ft8-logged`, `rig-state`)
    already produce + fan out exactly this data — a UDP emitter is just another
    hub sink. `internal/pskreporter` already proves the outbound-UDP,
    never-block-the-decode-loop pattern (fire-and-forget, I/O off the decode
    goroutine). Architecturally a clean opt-in egress subsystem beside
    bridge/ft8/pskreporter; config-gated, default off.
  - **The decision (wants an ADR when it lands):** emit the **WSJT-X UDP protocol**
    (Qt QDataStream, big-endian, the Status/Decode/QSOLogged schema) for instant
    ecosystem compatibility — vs an SM-native JSON-over-UDP that's trivial to
    build but nothing consumes. Lean WSJT-X-compatible; the whole value is riding
    existing tooling. Cost: implement + MAINTAIN QDataStream encoding against a
    schema that's WSJT-X's to change (a maintenance tail — hence the ADR).
  - **Hard constraints to bake in:** (1) **emit-only** — expose only the OUTBOUND
    subset; the WSJT-X protocol's inbound control side (Reply / HaltTx / Replay)
    is a remote-rig-control surface that collides with the operator-initiated FT8
    invariant (a remote Reply would start a contact with no operator action here)
    and with the existing daemon-owned command path. Telemetry out, never
    control in. (2) **never block** decode/TX — same discipline as PSK Reporter
    (send off the decode path, drop on a full buffer, fail-soft).
  - Meaty subsystem; **later**, after SM is releasable. Closes the loop with the
    external WSJT-X→Grafana request (they could point existing WSJT-X-consuming
    tools straight at SM).

- **UI themes + dark mode (all SPAs).** From dogfood-inbox 2026-06-24; operator
  reaffirmed the selectable-theme angle 2026-07-04. Wants a theme system: a dark
  mode **and** an operator-selectable theme (the operator picks a named palette for
  the UIs). **This entry is the thin follow-on, NOT the work** — the cost lives in
  the token-convergence pass under "UI consistency across SPAs — shared theme
  layer" (colours are inline in every component today: measured 275 raw classes in
  config, 98 in logbook, ~217 still-raw in logging). Once that pass routes colours
  through the shared `@theme` variables, a theme is cheap: a `data-theme` attribute
  plus a second set of variable values (dark mode = one such theme), and the
  operator's selectable theme is just N such sets. Do NOT start here — start with
  the convergence; wiring a toggle before the tokens exist just paints over the
  problem. The theme **choice** is a durable setting → daemon `config.json`, not
  localStorage (per the settings-in-config rule); the FT8 highlight colours already
  live there, so a `display`/`theme` config block is the natural home. The theme
  **picker** belongs on the config SPA's new **`General` tab** (see "Config SPA — a
  `General` tab" below), alongside the other cross-cutting preferences.

- **FT8 occupancy — multiple switchable spectral views (channelised strip +
  waterfall).** Filed from dogfood-inbox 2026-06-25; rationale + direction settled
  2026-06-26. **Decision: provide BOTH the current channelised strip AND a scrolling
  waterfall, switchable** — not one or the other. Today TX-offset selection is a
  per-slot **data** view: `Ft8OccupancyStrip` lays the passband out horizontally with
  busy bands shaded + daemon-vetted clear offsets as clickable markers (deliberately
  *not* a render — CLAUDE.md).

  **Why the waterfall matters (the load-bearing rationale — operator's insight):**
  SM is, as far as we know, the **only FT8 app that channelises the offset**. On the
  air FT8 is *continuous and overlap-tolerant* — stations sit at arbitrary audio
  offsets (6.25 Hz tone spacing, ~50 Hz wide), the decoder processes the whole
  passband, and two close/partially-overlapping signals routinely BOTH decode (strong
  LDPC FEC + Costas sync; a real collision needs them within ~a signal-width *and*
  comparable strength). Channelising a continuous space has two costs: (a) the band
  **looks fuller than it is** (a neighbour that merely touches your nominal window
  shades the channel), and (b) the binary **red "occupied"** manufactures operator
  *guilt* — "why did they come onto my channel" / "am I transmitting on top of
  someone" — when in FT8 sharing offsets is normal, low-stakes, and usually fine. A
  waterfall shows the **continuous truth** and lets the operator exercise *judgment* —
  see real gaps, **straddle** between signals, pick a spot that's "clear enough." That
  is a genuine capability the channel markers can't give (channelising stays the right
  frictionless default for the common "click a green one" case; the waterfall is the
  complementary expert view).

  **Two distinct strands (weigh separately):**
  1. ~~**Soften the occupancy semantics.**~~ **SHIPPED 2026-06-26 as the switchable
     "Spectrum" view.** Rather than alter the channelised strip in place, this landed
     as a **second, switchable presentation** (Channels | Spectrum toggle in
     `Ft8OccupancyPanel`, operating state `ft8State.occupancyView`): the Spectrum view
     (`Ft8OccupancySpectrum.svelte`) shows signals as soft shading at their **true
     continuous positions** (no cells), the daemon clear offsets as ▾/★ ticks at real
     positions (aligned with the Clear Offsets list), **click-anywhere** continuous
     offset pick, and a **graded clear / near / sharing** status (neutral status words,
     no advice — the operator judges) instead of binary red — directly killing the
     false-full + TX-guilt. Pure logic in
     `lib/utils/ft8Spectrum.ts` (`signalProximity`/`offsetFromFraction`, tested). Both
     views write the one `selectedOffset`. The grading is **position-only** (`Ft8Band`
     has no strength — loud-vs-weak needs the waterfall's FFT magnitudes). Docs: `ft8.md`,
     CLAUDE.md FT8 bullet.
  2. **The waterfall itself (rich — the continuous view).** Feasibility assessed
     2026-06-26: **the browser render is NOT the bottleneck** and the "JS redraw is
     slow vs C/Go" worry is misplaced *if* done right — Canvas 2D self-blit scroll
     (`drawImage(canvas,0,1)` + one `putImageData` row), sub-ms/frame, proven by every
     web SDR (WebSDR/KiwiSDR/OpenWebRX) at far higher data rates than FT8's ~3 kHz /
     ~10 fps. **DOM-per-cell would be catastrophic — never do that.** The FFT stays in
     Go (the browser does zero numeric work — it rasterises pre-computed magnitude
     rows). **The real work/cost is daemon-side:** a sub-slot FFT cadence (~10 fps vs
     today's once-per-15s — ~150× more FFTs, still cheap absolute, but the exact trigger
     to revisit PocketFFT for the occupancy/waterfall FFT — memory
     `project_sm_realfft_stays_pure_go`), a streaming push channel (~8 KB/s of quantised
     rows — binary WebSocket cleaner than text SSE), demand-driven (only while the view
     is open), plus scaling/contrast (AGC) + slot-time gridlines for readability.
     **De-risk by spiking the Canvas render with synthetic rows first** (an afternoon,
     no daemon work) before building the FFT-streaming pipeline.
     - **FFT choice — gonum vs PocketFFT (noted 2026-07-09, operator raised).** Don't
       assume CGO/PocketFFT is *needed*: the waterfall's is a **lightweight display FFT**
       (a few-thousand-point real FFT for magnitude bins, ~10–20 fps → tens of µs each,
       <1 ms/s of CPU in gonum) — a different, far lighter workload than the heavy
       *decode* FFT (jt9-style demod that must finish inside the 15 s slot, which is why
       decode already uses PocketFFT). The scary "~150×" is 150× a once-per-15s baseline
       = still only ~10–20 small FFTs/s. **But it's a cheap, reversible call because CGO
       is already in the picture:** the shipped build is CGO + PocketFFT, and **live FT8
       already REQUIRES CGO** (audio capture), so the waterfall is *de-facto CGO-gated*
       already — using PocketFFT for its bins costs nothing new and loses no static-build
       capability (that build has no live FT8 anyway). **Approach:** build the waterfall
       FFT behind the existing `gonum`-default / `pocketfft`-opt-in seam (same as decode),
       **measure at target fps + resolution, switch only if gonum shows in the profile.**
       Don't pre-optimise; the PocketFFT door is open and free if measurement says so.

- **FT8 Spectrum view — colour revision.** Filed 2026-06-26. The Spectrum occupancy
  view (`Ft8OccupancySpectrum.svelte`) shipped with first-pass colours: soft slate
  shading for signals, green/amber/orange-red footprint by proximity (clear/near/
  sharing), indigo/amber ▾/★ offset ticks. Operator wants these revised (palette TBD)
  — likely tighten the proximity ramp + the signal-vs-pick contrast, and reconcile with
  the eventual shared theme layer / dark-mode work (the FT8 highlight colours are
  already operator-configurable daemon config `ft8.display`; consider whether the
  Spectrum palette should join that or stay component-level). Cosmetic; no logic change.
  **Light-mode half (dogfood 2026-07-14, P2):** the **frontend/app** Occupancy pane
  (`Ft8OccupancyStrip` / `Ft8OccupancySpectrum`) colours only read correctly in dark
  mode — the busy/clear cell fills + amber recommendation markers wash out or look
  wrong on the light surface (red-500 / green-700 opacities tuned for the dark canvas).
  Needs a light-mode pass on the cell fills + spectrum tints; fold into this colour
  revision so both SPAs' occupancy palettes are reconciled at once.

- **LSPA → My Station → Location: future POTA fields.** Filed from dogfood-inbox
  2026-06-25, alongside the Location-tab field trim (the trim itself is the
  active phase-2 LSPA cleanup, not backlog). Once the Location tab is reduced to
  Grid Square / Altitude / Lat / Lon, the future addition is **POTA fields**
  (park references — `MY_SIG`/`MY_SIG_INFO` ADIF, or POTA park id). Sibling to
  the already-deferred IOTA/POTA/SOTA bucket (memory
  `project_sm_adif_my_star_buckets`). Not scoped — a placeholder so the "future
  add POTA" intent isn't lost when the trim lands.

- **Config hot-reload — apply changes without a daemon restart (whole-thing review).**
  Filed 2026-06-26. Today most config is **restart-only**: a subsystem binds its config
  at boot, so changing it needs a full `smd` restart (the config SPA shows a
  "restart required" banner). The friction the operator hit: **add a rig, then switch the
  active rig to it** — `rigs` + `default_rig_id` are restart-bound (ADR 0028 Phase 1: rig
  switch = edit `default_rig_id` + restart), so a perfectly routine "I just added my new
  rig, make it active" is a restart. Same for the bridge, forwarders, FT8/PSK/decode-log,
  SMTP. Meanwhile *some* config already applies **live** (My Station identity, `ft8_display`
  prefs — re-read by the SPA after a `/v1/config` PUT). So the ask is a **whole-thing
  review of what can/should hot-reload vs stay restart-only**, then make the high-friction
  ones live — **rig add + active-rig switch first** (re-open the bridge against the new
  active rig without dropping the process; the demand-driven FT8 capture + the bridge
  supervisor already tear down / reopen, so a "reconfigure + re-acquire" path is plausible).
  Context: this is `config.md §11` (hot-reload, previously deferred *to* the config-SPA
  workstream) made concrete by real dogfooding. Open design points: which blocks are
  safe to swap live (rig/audio device re-acquire, serial reopen) vs genuinely need a
  restart (e.g. server listen address); how the SPA signals "applied live" vs "needs
  restart" per block; and the daemon mechanism (a reload/reconfigure entry point per
  subsystem vs a coarse re-init). **Not now — recorded as a whole-area initiative.**

- **Version display in a more permanent place (e.g. tab title) across all SPAs.**
  Filed 2026-06-26 (the second half of the About→config inbox note). The build version
  now lives on the config SPA's General-tab About section, but the operator wants it
  more *ambient* — surfaced consistently across all three SPAs without opening a tab,
  e.g. the browser **tab title** (`<title>Station Manager — Config (2.0.0-…)`), a footer,
  or a header chip. Small per-SPA; do it with the cross-SPA shared-shell / nav work so
  the three stay consistent. Source is `GET /v1/version` (`daemon` field).

- **Logbook SPA — the management surface (beyond browse).** Filed 2026-06-26. The
  logbook SPA's QSO **browse** (selector + cursor-paged read-only table) shipped
  2026-06-26; the richer logbook-management UX — present in the operator's v1 reference
  app (`7Q-Station-Manager.20250823/logbook-app`) and flagged by the logging-vs-logbook
  scope memory — is deferred. Each is its own pass (design the UX first, per the
  design-SPA-UX-before-building rule):
  - ~~**Per-row QSO edit**~~ **SHIPPED 2026-06-27** — an Edit button per row opens a
    modal seeded from the row; Save PATCHes `/v1/qso/{uuid}` and replaces the row in
    place. Form covers the editable fields (date/times, call, freq+band, mode/submode,
    RST sent/rcvd, country, name, grid, comment); the daemon restores immutables,
    re-derives band from freq, and re-validates (a bad edit returns a message, modal
    stays open). ESC cancels, Ctrl/Cmd+Enter saves. `EditQsoModal.svelte` +
    `api/qso.ts` (`patchQso`) + edit orchestration in `logbook.svelte.ts`.
  - ~~**Multi-select (selection mechanism)**~~ **SHIPPED 2026-06-26/27** — first-column
    row checkboxes + a header select-all (indeterminate when partial), selection
    persists across pages (keyed on QSO id), cleared on logbook switch; a "N selected ·
    Clear" indicator. The **bulk ACTIONS** it feeds are still deferred:
    - **Export (selected / all) as ADIF** — reuses the session email-out's server-side
      ADIF rebuild from `{uuids[]}`, or a dedicated export endpoint / download.
    - **Send selected by email** — the session email-out endpoint already takes
      `{to, uuids[]}`; generalise it to an arbitrary selection.
    - **Upload selected to online services** — bulk-enqueue chosen QSOs to forwarders.
      This is the operator-driven **backfill** lever (ADR 0022 enqueues *future* QSOs by
      presence; retrospective upload of an existing log is explicitly this app's job —
      see the forwarder-enqueue memory + the "clear queued-upload backlog" item, its
      inverse).
  - **Per-row edit — more fields (from dogfood-inbox 2026-06-29).** Extend the edit
    form beyond today's set with **notes** (distinct from `comment`) and
    **long-path/short-path (LP/SP)** propagation info.
  - **"Emailed" column (from dogfood-inbox 2026-06-29).** Surface per-QSO whether it's
    been sent via session email-out — mirror the SessionPanel "Emailed" column
    (`SmFwrdByEmail*` in `additional_data`) in the logbook table. (Sent-flag *edits*
    are already noted as future logbook work — memory `project_sm_session_email_sent_status`.)
  - **Bulk email / export as a dialog overlay (from dogfood-inbox 2026-06-29).** When
    the export-selected / email-selected bulk actions (above) land, present them in a
    dialog overlay rather than inline — operator UX preference.
  - **Search / filter** — by callsign / date range / band / mode / country. Needs new
    daemon query params on the QSO-list endpoint (today it's cursor paging only, no
    filters) — a wire + validation + test change, so design it WITH the SPA.
  - **QSL-awaiting view** — filter to QSOs flagged for QSL (e.g. `app_sm_request_qsl` /
    QSL status) for card/label workflows.
  - **Edit-history viewer** — surface the `qso_history` append-only audit table for a
    QSO (who/what/when changed) — read-only forensics.
  - **Logbook management** — create / rename / delete logbooks from the UI (daemon
    endpoints exist: POST / PATCH / DELETE `/v1/logbook`; DELETE refuses a non-empty one).
  Build order for what remains: search/filter → bulk actions (export/email/upload) →
  QSL-awaiting → edit-history → logbook CRUD (edit + the selection mechanism are done).
  The reference app is a UX guide, not a port (it's Wails + page-number paging; SM is
  HTTP + cursor paging + its own utils/tokens).

- **FT8 Call CQ — no operator feedback while waiting for a chosen slot parity.** Filed
  2026-06-28. When **CQ slot** is set to **Even** or **Odd** (not the default **Next**),
  `StartCallCq` forces our CQ parity and deliberately does **not** immediate-fire — the
  first CQ is held until the next slot of the chosen parity (`caller_sequencer.go:63-88`),
  which can be up to ~one extra slot (~15–30 s) after the click. Correct behaviour (it's
  the point of choosing a parity), but the UI gives no sign it's waiting: the button flips
  to *Calling CQ…* immediately while the rig stays silent, so a non-default parity can read
  as "it didn't fire." Enhancement: a transient **"waiting for even/odd slot…"** indicator
  (or a countdown on the Call CQ control) until the first CQ actually keys, then drop to the
  normal calling state. SPA-only — the daemon already publishes the chosen `cq_period`
  (`QsoStatus`) and the first `tx-state {transmitting:true}` marks the real start, so the
  SPA can show "waiting" between StartCallCq and that first TX edge. Surfaces: `Ft8MsgPanel`
  (the Call CQ control), `ft8.svelte.ts`. Low effort; pure clarity. Default **Next** is
  unaffected (it fires on the very next boundary, so there's nothing to wait through).

- **FT8 Monitor/Listen on-off toggle — DISCUSSION POINT (not a committed build).** Filed
  2026-06-27. Today FT8 audio capture is tied to the **FT8 view being open**: `Ft8Panel`'s
  `onMount` calls `startFt8` → subscribes `/v1/ft8/events` → the daemon acquires the mic
  (demand-driven, refcounted, ~5 s linger, CAT-gated). So as long as a tab sits on the FT8
  operating mode — even backgrounded — the daemon holds the microphone and decodes every
  slot. That's correct and WSJT-X-like (its receiver runs while the window is up), and a
  2026-06-27 "why is the mic held?" turned out to be exactly this (an FT8 browser tab open
  alongside a Phone/CW one — benign). The discussion: should capture instead be gated on an
  explicit **Monitor/Listen** control the operator toggles, so the mic engages only when
  they deliberately start listening, not merely because the view is on screen? Points to
  weigh: (a) does Monitor *replace* the view-open trigger or *augment* it (view open but not
  monitoring → no mic, no Band Activity)? (b) interaction with the existing demand-driven
  refcount + CAT gate (Monitor would become the primary gate, subscriber-count secondary);
  (c) what the FT8 view shows when not monitoring (empty Band Activity + a "Listening off"
  state); (d) is Monitor per-tab UI state or daemon-owned (ties into the multi-tab
  operating-lock item — two tabs, who's monitoring?); (e) is the current behaviour actually
  a problem, or fine once the operator understands it (the trigger was a misunderstanding,
  not a fault)? Decide the model before any build. Surfaces if pursued: `Ft8Panel` (gate
  `startFt8`/subscription on a Monitor toggle), `ft8.svelte.ts`, possibly daemon capture
  gating. Related: the multi-tab rig-lock item (Bugs) shares the "which surface is doing
  what" question.

- **FT8 same-session dupe rule — extend to the daemon auto-workers.** Filed 2026-06-27.
  The SPA now blocks working/queuing a **same-session dupe** (a call already logged on the
  current band this session — contest-dupe style) from **Band Activity clicks**
  (`Ft8Panel` `workedThisSession`, gating `answerCq`/`workCaller`/enqueue; greyed rows).
  But the rule only covers operator clicks — the **daemon auto-workers don't honour it**:
  Call-CQ `auto_first` (`caller_sequencer.go` answerer-pick) and the pile-up drain could
  still work a station already logged this session. To make the "no same-session dupes"
  rule airtight, the daemon needs a session-dupe notion too — which is awkward because
  **the session is an SPA concept** (`sessionQsosState`, per-tab), not a daemon one (the
  daemon has no session_id, by design — see the session-scope memory). Options to weigh:
  (a) the SPA passes a "skip these calls" set / a since-timestamp to the daemon on
  `cq/start`; (b) the daemon dedupes against *today's* logbook rows on this band+mode (a
  proxy for "this session" — simpler, but not exactly session-scoped); (c) leave the
  auto-workers as-is and accept that hands-off modes may re-work a session dupe (document
  it). Decide the model before building. Until then, the SPA-click guard is the protection
  and the auto-workers are a known gap (noted in `docs/ft8.md`).

- **Operator log viewer (daemon diagnostics) — DB-manager tab to start.** Surfaced
  2026-06-30 while specifying ADR 0039's "loud startup log line" for the
  disabled-forwarder queue discard. The realisation: a loud `smd.log` line is
  worthless to an operator who won't `tail` a file — and external ops (7Q8AC etc.)
  have *no* window into daemon activity/errors. Distinct from the **live
  operational toasts** that already exist (rig-disconnected, upload-failed, bridge
  errors via SSE in the logging SPA) — this is a viewer for the **structured log
  history + admin-class events that never become toasts** (forwarder discards,
  migrations, the reference.db bootstrap, startup warnings). Decided shape (2026-06-30):
  **a diagnostics surface inside the DB-manager**, not a 5th standalone SPA — same
  admin/troubleshooting audience + cadence as queue-health/DB-health; promote to its
  own SPA later only if it grows teeth (live streaming, heavy filtering, multi-source).
  Daemon side stays narrow: a recent-log endpoint (`GET` over a ring buffer of the
  last N structured lines, or a bounded `smd.log` read), optionally an SSE tail for
  live — no coupling into log/forward internals. Build alongside the DB-manager SPA
  workstream.

- **SPA SSE consolidation — one multiplexed event stream (all SPAs).** The logging
  SPA opens 2-3 long-lived **SSE** streams per tab (`/v1/rig/events`,
  `/v1/ft8/events`, and the `/v1/events` firehose). Browsers cap **~6 connections
  per host** over HTTP/1.1, so several SM tabs each holding these **starve the
  browser** — new connections (a fresh tab, or even the SPA's own fetches) queue
  forever → "Connecting…" / frozen tab. Surfaced 2026-07-02: the (then new-tab)
  cross-SPA nav accumulated a tab per click and hung Firefox after 5-7 clicks.
  **Immediate fix shipped:** cross-SPA nav now navigates **same-tab** (only Manual
  opens a new tab), so only one SPA's SSE set is live at a time — removes the
  auto-accumulation. **Residual risk:** manually opening ~3+ logging tabs can still
  brush the 6-connection limit. **Durable fix:** collapse the per-topic SSE into ONE
  multiplexed stream (e.g. `GET /v1/stream` carrying rig/ft8/qso/bridge events tagged
  by type, SPA fans out client-side) so a tab holds ONE SSE regardless of how many
  event topics it watches — the events hub already multiplexes internally, so this
  is mostly a new combined endpoint + a client demultiplexer. NOT urgent (same-tab
  nav covers normal use); revisit if tab-starvation recurs or before a wider release.

- **FT8: operator-adjustable attempt limit before Next.** **Daemon side SHIPPED
  2026-07-03** — `ft8.tx.max_repeats` is surfaced on `/v1/config` as `ft8_max_repeats`
  ([1–10], resolved default 6) and **applied live** to the running sequencer via
  `Service.SetMaxRepeats`/`Sequencer.SetMaxRepeats` (mutex-guarded, affects an in-flight
  contact on its next slot — no restart). Decided: **logging FT8 Settings tab, live**
  (not the config SPA). Tests: sequencer clamp + service forward/nil-safe (`ft8`),
  GET/PUT/400/omit round-trip (`api`); docs: api-endpoints.md + config.md §11.5 (the one
  live-applied config field). **SPA field still pending.** Filed 2026-06-27 (dogfood),
  triaged 2026-07-03. In a big pile-up, if a station stops hearing you the sequencer
  works the full rung count (daemon-set `maxRepeats`) before the operator's Next can
  advance — wasting slots on a non-responder. The "N calls left" readout is display-only
  today (`Ft8Panel` Working banner, from `ft8State.qso.maxRepeats`, set by the daemon per
  rung — not SPA-editable). Add an operator control to **cap the attempts before
  auto-advancing** (a small numeric field beside Next, or a session default in the FT8
  Settings tab) so a dead contact is dropped sooner. Design points: SPA-only nudge of an
  early-Next threshold vs a daemon `max_repeats` override on `/v1/ft8/qso/*`; per-session
  vs per-QSO. Surfaces: `Ft8MsgPanel` (Next control), `ft8.svelte.ts`, possibly the
  sequencer rung count.
  **Config default + INFINITE option (2026-07-04):** resolved default is **6**, current
  clamp **[1–10]**. Add an **infinite / keep-going** option — never auto-abandon a contact,
  chase until answered or Abandoned (for a rare/weak/fading station worth staying on).
  *Encoding wrinkle:* `0`/absent already means "use default 6", so infinite needs its own
  value (e.g. `-1`, or a `max_repeats: "infinite"` literal) — don't overload `0`. *Caveat:*
  infinite chases a *silent* contact forever, blocking the pile-up on one non-responder — a
  deliberate choice; the operator still has Next/Abandon. NB this is the **contact** off-ramp;
  **CQ itself is already uncapped** by design (`caller_sequencer.go`: "calling CQ is the
  operator's standing intent — keep calling until answered or Abandoned").

- **FT8 Field Day — FD-aware Operate ladder render (+ remaining FD UI). PARKED — blocked until the next Field Day contest.**
  Parked 2026-07-04: the FD UI can only be meaningfully exercised on-air during a Field
  Day contest, so it waits for the next one; ARRL/RAC-only, so it is not a 7Q8AC
  ship-gate item. Filed
  2026-06-28 (dogfood "correct the ladder display for the ARRL FD"), triaged 2026-07-03.
  FD-over-FT8 shipped + on-air-validated 2026-06-28 (ADR 0037, both directions), but the
  **Operate-tab message ladder still renders the standard exchange placeholders**
  (`<DX>`/`<GRID>`/`<RST>`), not the FD class+section exchange — so the ladder is wrong
  for an FD QSO. This is the documented FD remainder set (CLAUDE.md + memory
  `project_sm_ft8_field_day`): (1) the **FD-aware ladder render**; (2) **FD pile-up
  Ctrl-click** (enqueue an FD caller); and (3) the **config-SPA section dropdown**
  (`ft8.field_day.section`, validated by `goft8.ValidARRLFieldDaySection`). SPA-side for
  (1)/(2); config-SPA for (3). The daemon FD path is done — this is presentation/entry.

- **Settings help tooltips + beginner/expert mode (all SPAs).** Filed 2026-07-02
  (dogfood), triaged 2026-07-03. Many FT8 (and other) settings knobs are terse; add
  larger explanatory tooltips/help text, with an operator toggle to switch them off
  (**beginner ↔ expert** mode) so an experienced op isn't nagged. Cross-cutting UI: the
  beginner/expert flag is a durable pref → daemon `config.json` (settings-in-config
  rule), natural home the config-SPA General tab; the tooltip copy lives per component.
  Start with the FT8 Settings tab (densest), extend as friction surfaces. Pairs with the
  shared-theme / cross-SPA-shell work.

- **`actions/rigControl` — shift+ctrl freq-step key parity in FT8 (match phone/CW).**
  From dogfood-inbox 2026-07-03, graduated 2026-07-04. In phone/CW the Shift+Ctrl arrow
  cluster tunes the rig (±100 Hz / ±10 Hz / ±5 kHz band-hop, per CLAUDE.md rig-control);
  the operator wants the same freq-step keys live while an FT8 view is focused. Today those
  bindings are wired for the logging (phone/CW) surface only. Scope: decide whether FT8
  reuses the same `actions/rigControl` handler (routing set_freq/set_freq_b by selected VFO
  as today) and which FT8 focus contexts should capture the keys without clashing with FT8's
  own Shift+Ctrl shortcuts. Small, but needs a keymap-collision check against the FT8 panel.

- **SPA code-review low-severity batch (2026-07-05 review).** The verified LOW
  findings from the same review whose highs/mediums (findings 1–7) shipped
  2026-07-05. Each was confirmed by the slice reviewers; none is ship-gate. The
  one standout (SPA fetch timeouts) was promoted to P1 and SHIPPED 2026-07-05 (see
  backlog-archive). The rest are grouped as the reviewer grouped them; each line
  leads with its surface
  so it's greppable. Batch them in a dedicated cleanup pass (mostly one-liners);
  pull an individual item forward if a related file is already open.

  **Standout still in the batch:**
  - **`TX_PWR` in (0, 0.5) W rounds to the `0` sentinel while passing the `> 0`
    omit-guard (`adif.ts:240`).** Durable outbound ADIF data — a fractional
    QRP power emits a wrong `0`. Fix: round before the omit gate, not after.

  **States (`lib/states/`):**
  - `bridge.svelte.ts:182–193` — `tabCount` not reset on SSE error, so the
    "another tab is operating" banner stays stuck after a daemon restart.
  - `bridge.svelte.ts` (~330) — `closeSource()` doesn't clear
    `catState.freqKnown`, unlike both involuntary-disconnect paths (inconsistent
    reset → a stale freq can read as known after a voluntary close).
  - `ft8.svelte.ts:454` — single-feed mode keeps the prior slot's decodes on a
    silent slot (should clear to reflect "nothing this slot").
  - `ft8Enrich` — `ft8EnrichState.clear()` doesn't invalidate in-flight lookups,
    so a lookup resolving after view-close re-inserts a zombie cache row.

  **FT8 UI:**
  - `Ft8PileupDrawer` — drawer-close can't abort an in-flight drain start; the
    `AbortSignal` param exists but is unused.
  - FD callers advertise "Ctrl+click to add to pile-up" in the tooltip, but
    `enqueueCaller` parses with `parseDirectedToMe` only → silent no-op (FD
    pile-up is listed pending in CLAUDE.md / the parked FD-UI item). Hide the
    affordance until FD pile-up Ctrl-click actually lands.
  - `bearing.ts:105` — bearing rounds *after* normalisation, so 359.97° renders
    `360` instead of `000`. Normalise after rounding.
  - `Ft8Panel.svelte:698` — `isWorking` splits on `' '` where sibling parsers use
    `/\s+/` (inconsistent whitespace handling).
  - `canAnswer` omits `!tx.transmitting` unlike `canSend` (a TX-in-progress guard
    the sibling has).

  **QSO UI:**
  - Edit-overlay mode dropdown renders blank for stored modes outside its static
    9-entry list (QsoPanel's own `modeList` appends the stored value; the overlay
    doesn't). Mirror the append so an out-of-list mode still shows.
  - Session-row fields re-read from live state *after* the `await`, so a CAT push
    mid-POST can skew the Session-tab display. Snapshot before the await.
  - F3 "Stop" re-snaps Time On to now — documented design, but a real data trap;
    worth a confirm or a relabel so it can't silently overwrite a set Time On.

  This whole batch, plus the two standouts, came from the 2026-07-05 SPA review;
  the "Verified sound" list from that review (ADIF byte-length prefixes,
  enrichment-never-blocks invariant, midnight rollover, i18n catalogue, EventSource
  lifecycles, mode.ts submode table) was checked hard and should NOT be re-flagged.

- **Bridge / TX-safety hardening batch (2026-07-05 `internal/bridge` review).** The
  three verified LOW findings from the bridge-subsystem review; its MEDIUM-HIGH (#1,
  stranded-key backstop on a failed key write) + LOW-MED (#2, defensive/teardown
  unkey bypassing cmdMu) + doc nits (#6) shipped the same day. All three below are
  fail-safe-directioned (they fail toward unkey / write-block, never toward a stuck
  carrier), which is why they're LOW despite touching TX code. Verify each still
  applies before fixing — the #1/#2 fixes moved nearby code.
  - **Auto-off retry re-arm can clobber a fresh key's timer** (`tune.go` ~292–302 /
    `ft8tx.go` ~236–246). `tuneAutoOff`/`ft8TxAutoOff` release `keyMu` inside
    `releaseTune`/`releaseFt8Tx`, then take `mu` to re-arm the retry. Interleaving:
    release fails → operator StopTune succeeds → operator StartTune keys anew with a
    fresh timer → the old callback resumes, sees `active=true`, overwrites the timer
    with a 1 s retry → the new tune is cut at ~1 s and the orphaned original timer
    fires a redundant release later. Fail-safe (unkeys early, never strands). Fix: a
    per-key **generation counter** captured at key time and re-checked in the callback
    before re-arming.
  - **A garbled first IDENTITY permanently write-blocks the pipeline instance**
    (`pipeline.go` ~660–696). `identityVerified` latches on the FIRST IDENTITY
    response; if startup serial noise corrupts the ID digits it decodes to "" →
    `identityUnrecognised` → toast + write paths blocked, and because the latch is set
    a later clean matching IDENTITY can never upgrade to confirmed. Recovery needs a
    pipeline restart that may never come on a healthy link. Fail-closed is the right
    default; the fix is to let a later **exact-match** IDENTITY confirm while KEEPING
    the mismatch-halts semantics (only the unrecognised→confirmed upgrade opens up).
    Shipped rigdefs can't produce a false `identityMismatch`, so only the unrecognised
    path is exposed.
  - **`bridge.New` trusts `Serial`/`Cat` non-nil** (`pipeline.go` ~174 derefs
    `*s.cfg.Serial`; every write path derefs `s.cfg.Cat`). Safe today because
    `config.ActiveBridge()` always populates both, but nothing in the package enforces
    it — a future caller/test passing `Enabled: true` with raw config panics inside the
    supervisor goroutine and kills the daemon. Fix: a two-line nil check in
    `Initialize()` (which today only checks the logger) to make the invariant local.
  - Also noted (not a fix, just a documented window): a slow/wedged tab evicted by the
    hub keeps the other tabs' `rig-clients` count stale until its handler goroutine
    hits the 10 s SSE write deadline and its deferred unsub broadcasts — bounded and
    advisory.

- **migration 0004: timestamp `localtime`→UTC + normalise pre-fix debris rows — AUTHORED
  2026-07-05, ONE STAGED-REVIEW BUG CAUGHT + FIXED, awaiting operator review + deploy.** Files:
  `migrations/log/0004_utc_timestamps.{up,down}.sql`; test `TestMigrate0004_NormalisesTimestampsToUTC`
  (seeds all FOUR formats → asserts canonical UTC). Full `internal/database` suite green under
  `-race` (incl. the down path via `TestMigrate_DownRestoresRSTLengthConstraint`, step count bumped
  −2→−3). **HIGH bug caught in staged review (2026-07-05) BEFORE deploy — the reason the staged
  gate exists:** the normalisation CASE had only two arms (`… +00:00` keep / else −2h), but
  **PRE-fix** sqlboiler stored UTC `created_at`/`deleted_at` via Go's `time.Time.String()` as
  `'… +0000 UTC'` (UTC-correct, but not `+00:00`), so the −2h arm would have shifted every pre-fix
  `created_at` (≈ every QSO since April) 2 h WRONG. Fixed with a **third CASE arm**
  `WHEN v LIKE '% +0000 UTC%' THEN datetime(substr(v,1,19))` (reformat, no shift — first 19 chars
  are already UTC wall time), applied to all six normalised columns + a `seed(4)` regression case.
  The down path stays coherent (post-up rows are naive-UTC; its `+2 hours` arm round-trips).
  **Empirical scoping:** `boil` defaults to **UTC**, so sqlboiler (`created_at`/`deleted_at`) and
  the Go writers (`modified_at`) store UTC POST-fix; only the SQL **DEFAULTs**
  (`qso_upload.created_at`, `qso_history.at`) + the two **triggers** stamped local, and PRE-fix
  sqlboiler UTC wore the `+0000 UTC` skin (the caught bug). The migration rebuilds the three tables
  with `datetime('now')` (UTC) defaults + UTC triggers, normalising every value during the copy.
  **Still pending YOUR go-ahead to deploy** — eyeball the SQL against the live DB first; it
  auto-runs on the next `smd` start once committed. **Deploy safety (reviewer advice):** golang-migrate
  gives no rollback net, so **back up the DB first** (a `VACUUM INTO` like the bootstrap split does)
  and **spot-check a known QSO's `created_at` against its `qso_date`/`time_on` after** it runs.
  Original staged spec follows for reference. The **fix-forward half shipped 2026-07-05** (`_time_format=sqlite` on `getDsn`/`bootstrapDSN` + `time.Now().UTC()`
  on the 10 `null.Time` DATETIME writers → every *Go-written* stamp is now SQLite-canonical UTC;
  `TestModifiedAt_StoredCanonicalUTC` locks it). This migration is the **staged half** — deliberately
  separate so it can be eyeballed against the live dogfood DB before it runs. Do **before SM Cloud
  S3** (reconcile diffs `qso.modified_at` across hosts; the Postgres store canonicalises to µs UTC).
  Empirically verified against modernc v1.48.1 (scratch probes):
  - **Trigger/default change:** `datetime('now','localtime')` → `datetime('now')` (UTC) on
    `qso.created_at` + `qso_history.at` defaults and the `trg_qso_set_modified_at` /
    `trg_qso_upload_set_updated_at` trigger bodies. SQLite can't alter a default/trigger in place →
    **table rebuild**, same pattern migrations 0002/0003 already use (FK re-point, trigger + index
    recreate, `qso_history` append-only guards).
  - **Normalise existing rows (per-format — single-TZ CAT assumption):**
    - *Naive-local strings* (`YYYY-MM-DD HH:MM:SS`, no offset/`T`/`Z` — trigger/default writes):
      written as CAT local, scanned back as UTC → **2 h off**. Shift to UTC (`datetime(col,'-2 hours')`
      for the fixed +02:00 CAT), format-gated so only these rows are touched.
    - *Go monotonic-debris strings* (`… +0200 CAT m=+…` — pre-fix `time.Time.String()`): offset-aware
      so **correct-instant** but `datetime()`-unparseable; strip to canonical UTC (offset embedded, so
      no shift — just reformat). Pure-SQL string surgery is fiddly; a Go-side migration step may be
      cleaner. Bounded set (only pre-fix updates).
    - *Already-canonical* (`…+00:00`, post-fix): leave.
  - **Decision to confirm before authoring:** the −2 h shift assumes ALL historical naive-local rows
    were written in CAT — correct for the single-operator dogfood DB; **confirm that's the only DB with
    pre-fix data** (7Q8AC hasn't onboarded). If any row was written elsewhere the blanket shift is wrong.
  - **Test:** seed a table with one row of each format, run the migration, assert all rows end
    canonical UTC + correct instant (extend `review_findings_test.go` / a migration test).
  - NB after the fix-forward but before this migration, `created_at` + trigger-written `modified_at`
    stay naive-local (2 h off) while Go-written `modified_at` is clean UTC — a known, harmless-today
    interim (ordering works on the prefix; nothing external reads these yet).

- **`internal/database` review low-severity batch (2026-07-05).** Verified LOW findings from the
  same review; none ship-gate, none touch the reconcile invariant (that's finding 1, above).
  - **Cold-insert race → retry instead of erroring** (`api_context.go:1311-1368`, `writeContactedStation`):
    two concurrent enrichment writes for the same callsign can both miss the fetch and both insert; the
    loser hits the UNIQUE index and the error propagates. Enrichment is fail-soft so logging is never
    blocked, but `IsUniqueConstraintError` exists exactly for this — catch it and retry as the
    update-merge branch (turns a spurious warning into correct behaviour). Read-merge-write is also
    last-write-wins under concurrency — acceptable for a cache, noted.
  - **Bootstrap split-detection can leave a stale table** (`bootstrap.go:56-61`): the "already split?"
    check keys on the `country` table alone, but the drops run `country` then `contacted_station`; a
    crash between them leaves a stale `contacted_station` in the log DB forever (data safe — already
    copied to reference.db). Detect on *either* table.
  - **Nits:** `FetchAllLogbooksWithContext` fails the whole list on one bad adapter row while every QSO
    list path warns-and-skips — pick one policy · `MarkSessionEmailedWithContext` builds an unbounded
    `IN (?)` list (fine at session scale; guard-comment before anything import-sized feeds it) ·
    `getDsn`/`bootstrapDSN` interpolate the file path raw into `file:%s?…` — a path with `?`/`#` breaks
    the DSN, one `url.PathEscape` away · `DeleteLogbookByIDWithContext` check-then-act window (concurrent
    QSO submit between Exists and soft-delete orphans a QSO under a deleted logbook — negligible single-op)
    · `Ping` sleeps 25 ms once more after its final failed attempt (cosmetic).

- **`internal/api` review low-severity batch (2026-07-05).** Verified LOW findings; the
  MEDIUM (PUT /v1/config lost-update race between the two SPAs) shipped 2026-07-05 (see
  backlog-archive). None below is ship-gate.
  - ~~**Disabled-subsystem routes return misleading statuses** (`server.go` / `spaHandler`).~~
    **FIXED 2026-07-05.** A `/v1/*` path reaching the SPA catch-all (disabled bridge/FT8, or a
    typo) now returns an honest 404 instead of 200-HTML/405; and a real directory (`/assets/`)
    SPA-falls-through to index.html instead of an `http.FileServer` listing (the disclosure nit,
    closed by the same change). `spaHandler` `/v1/`-guard + `f.Stat().IsDir()` rewrite; tests
    `TestSpaHandler_ApiPathReturns404` + `TestSpaHandler_DirectoryServesIndexNotListing`.
  - **Nits:**
    - Negative server-limit config panics at startup: `Normalize` defaults only `== 0`, so
      `max_concurrent_requests: -1` reaches `make(chan struct{}, -1)` in `newLoadLimiter` → panic
      (`limits.go:47`). A validation error would be kinder than a loud crash.
    - Credential-clear semantics differ across masked surfaces: **forwarder** creds treat an
      empty-string value as overwrite-with-empty (clearable), while **SMTP/lookup** passwords
      treat blank as keep (not clearable via the API). Each is locally documented, but the
      asymmetry invites a confused bug report — unify, or document side-by-side in api.md.
    - ~~`spaHandler` serves **directory listings** for real embed-FS dirs~~ **FIXED 2026-07-05**
      (same change — a directory hit now SPA-falls-through to index.html).
    - Stale comment: `middleware.go:206-216` (Unwrap) still says the bridge SSE handler "clears
      its write deadline at stream open" — it arms per-write deadlines now. Doc drift only.

- **`internal/qsoservice` review nits (2026-07-05).** The review's two LOW findings +
  the #3 pinning test were FIXED 2026-07-05 (audit `before_image` now marshalled before
  the merge so a `contact_history` body can't taint it via the shared `ContactHistory`
  backing array — protects the SM Cloud sync input, ADR 0016; `EnqueueUploads` doc
  corrected to describe the actual per-TYPE ADIF-stamp check; `TestUpdate_RestoresAllForwarderStamps`
  reflects over `types.Qso` stamp tags and pins the immutable-restore list against drift).
  None ship-gating. Residual nits:
  - **`uuid_conflict` classification unreachable under `force`** (`submit.go:322`): the guard
    is `IsUniqueConstraintError(err) && !force`, so a forced re-import of an exported log (UUID
    collision, random dedupe key) aborts with a generic error instead of the per-record
    `uuid_conflict` report. Unreachable today (batch path hardwires `force=false`,
    `SubmitImport` has no non-test caller) but a trap for whoever wires a `--force` import.
    Dropping `&& !force` is safe.
  - **`importBatchFallback` publishes Hub events** (via `s.submit`), contradicting
    `SubmitImportBatch`'s "does NOT publish" doc — harmless (import runs daemon-offline) but the
    doc should flag the fallback as the exception.
  - **Best-effort `contacted_station` cache warm-up uses the request ctx** — a client that
    disconnects right after commit skips the cache write. Deliberately best-effort; a detached
    short-timeout ctx (like the dedupe refetch already uses) would make it client-independent.

- **FT8 work-a-caller / Resume opening — prefer a clean next-slot start over a truncated
  immediate fire.** From dogfood-inbox 2026-07-05 ("abandon while TX → Resume immediately
  starts TX — too late into the slot?"), triaged + **confirmed WAI (not a bug)**: the immediate
  TX is gated by `fireOpening` (`sequencer.go:805-816`) — it keys immediately ONLY within the
  first `txLateWindowSec` (4.5 s) of an our-parity slot, where ADR 0032 truncate-don't-shift keeps
  the signal decodable; past 4.5 s (or in the partner's parity) it defers to the next slot. So it
  never actually fires "too late." **The enhancement:** `fireOpening` is SHARED by the answer-a-CQ
  path (which legitimately NEEDS the truncated immediate fire — a DXpedition moves on if you're a
  slot late) and the **work-a-caller / pile-up-drain** path (`StartWorkCaller` → `fireOpening`,
  reached on Resume). But a station you're *working* is calling YOU and will keep calling — there
  is no tight reply window — so a truncated opening (up to ~36 % of symbols dropped at 4.5 s) is
  arguably worse than just waiting one slot for a clean, full-length start. Consider gating the
  **opening rung on the work path** (`seqWorking`, and maybe the caller-side too) to skip the
  immediate-truncate and wait for the next clean our-parity `OnSlot`, while leaving the answer-a-CQ
  opening's immediate fire unchanged. `fireOpening` is per-mode already (the `switch s.mode`), so
  the differentiation is local; decide whether the caller-side Call-CQ opening wants the same
  treatment. Purely a TX-quality nicety — the current behaviour is correct and on-air-validated,
  just not optimal for the no-time-pressure work case.

- **Re-enrich a logged QSO — in the logbook SPA (BUMPED 2026-07-05; next session).**
  From dogfood-inbox 2026-07-04, **bumped out of P2/P3 parking** after a second flaky-link
  occurrence: **there is no UI way to re-run enrichment on an already-logged QSO**, so a name
  dropped by a QRZ timeout at log time stays dropped with no backfill path. **Justification
  (why it's more than a nicety):** this is the recurring 7Q8AC/Malawi operating condition —
  on a flaky link, QRZ lookups intermittently time out during logging, and while the
  "enrichment never blocks logging" invariant holds (the QSO logs fine) + the QRZ resilience
  recovers per-lookup (no permanent disable, session-198 work), a few QSOs slip through
  nameless each bad-internet session. Measured 2026-07-05: **3/52 nameless** (RG6S, R2BNC,
  SP9SOF) during the 13:00–14:00 UTC timeout window; a 128-QSO FT8 pile-up earlier lost more.
  Hand-editing each is the only fix today. **Decision (operator, 2026-07-05): implement in the
  LOGBOOK SPA** (the QSO management surface — where you go to fix up historical rows), not just
  the logging SPA's session-tab edit overlay. **Approach:** in the logbook SPA's per-row edit
  modal (`EditQsoModal` already exists), add a **"Re-enrich"** action that calls
  `/v1/enrich/callsign` for the row's call and merges the result (name, QTH, country, grid,
  DXCC/zones) into the editable fields, which the operator then saves via the existing
  `patchQso` PATCH. Progressive + fail-soft (a re-enrich that times out changes nothing).
  Reuses the exact endpoint the new-QSO `Callsign` component uses; no daemon change. Consider
  a bulk "re-enrich selected" once the logbook multi-select bulk-actions land (folds into
  "Logbook SPA — the management surface"). **Companion doc task (below).**

- **Manual FAQ: "Why is a QSO logged without a name, and how do I fix it?" (operator, 2026-07-05).**
  Document the name-missing cause + remedy in the operator manual (Hugo, ADR 0036) as an FAQ /
  troubleshooting entry. **Cause:** on a poor internet link the QRZ callsign lookup can time out
  at the moment you log — SM deliberately logs the QSO anyway (enrichment must never block
  logging) rather than making you wait or lose the contact, so the name/QTH just aren't filled
  in for that one. It is NOT a lost QSO and NOT a credentials problem; the lookup service
  recovers on its own for the next contacts. **Remedy:** re-run enrichment on the row once the
  link is back (the logbook-SPA "Re-enrich" action above), or hand-edit the name. Pairs with the
  re-enrich feature — write the FAQ so its "remedy" references that button once it ships (until
  then: hand-edit). Keep it operator-plain (no "QRZ session key / i/o timeout" jargon).

## Scope notes (NOT backlog — recorded so they aren't mistaken for it)

- **FT8 daemon-initiated sequencing is OUT OF SCOPE and unsupported** — the QEX
  FT8 specification forbids automatic operation and unattended operation is
  licence-restricted in many jurisdictions. Not a roadmap item.
  **State the claim precisely** (the wording matters, and this doc got it wrong
  until 2026-07-30): SM is **operator-initiated** — every session begins with an
  operator action (the click plus arming TX) and the daemon never starts one.
  That is enforced. What is NOT enforced is **attendance**: once a run is started
  it continues until Abandon — a Call-CQ run works answerers, and under ADR 0059
  an auto-work run works callers — with nobody at the desk. Remaining present is
  the operator's obligation under their own licence.
  **One presence check IS enforced, and it is narrower than attendance: the FT8
  view must stay OPEN.** When the last `/v1/ft8/events` subscriber goes away past
  the linger window, `Service.onLingerExpired` calls `disarmTx` — dropping PTT and
  abandoning any active QSO — before releasing the capture device
  (`internal/ft8/service.go:398–429`, which calls this the "attended-only
  guarantee"). So: walking away with the browser open does not stop a run; closing
  the browser does. Say that, rather than either "attended-only" (overstates —
  nothing checks for a human) or "runs unattended" (understates — it dies with the
  tab).

- **"Design our own sequencing/timing" — future thinking (flagged 2026-06-12).**
  Operator wants to revisit, later, whether SM grows its own sequencing/timing
  design rather than mirroring WSJT-X's. Hard constraint to carry into that
  conversation: anything on the air as *FT8* must stay protocol-interoperable —
  the Costas sync, 15 s cadence, and 0.5 s nominal start are protocol, not SM
  choices; a genuinely new mode would need its own Costas arrays (per the QEX
  licence restriction on non-conforming streaming) and would not be "FT8". So the
  open design space is SM's own sequencer *architecture / policy / UX*, not the
  on-air timing of standard FT8. No action now — recorded so it isn't lost.
