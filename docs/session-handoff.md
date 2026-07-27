# Station Manager — Session Handoff

**Purpose:** rolling handoff document across Claude sessions. Captures what was
done in the previous session, where the repo currently is, and **what the next
session should pick up**. Read this first when starting a session — it exists
precisely so we don't re-derive state or redo finished work.

**How to use this document:**

- **At session start:** read top-to-bottom. The "Current state" section tells
  you where the repo is. The "Next steps" section tells you what to do. If the
  next session's goals have already been set, work from them.
- **At session end:** the assistant updates this document before stopping.
  Move anything in "Next steps" that was completed into "What happened this
  session" with a date. Leave anything unfinished in "Next steps" and add new
  items discovered during the session.
- **Rolling window (enforced):** keep about the **last ~12 sessions** of
  `### Session N` entries live. When the list grows past **~15**, move the
  oldest block down into [`session-handoff-archive.md`](session-handoff-archive.md)
  (newest-first, verbatim) so this doc — read top-to-bottom every session start —
  stays lean. The archive is the grep-able convenience copy; the authoritative
  long-form record is git history + the v1-analysis docs + the memory files.
  (Prior policy said "2–3 sessions" but was never enforced — the doc reached 197
  entries / ~1 MB before the first roll-off on 2026-06-14. ~12 keeps the current
  multi-session arcs intact without the bloat.)
- **Durable facts go in memory files,** not here. This document is for
  transitory session-to-session state. If something is stable across all
  future sessions (a project invariant, a user preference, a design rule),
  capture it in a memory file under `~/.claude/projects/.../memory/`.

---

## Current state (as of 2026-07-27)

> **2026-07-27 — the occupancy/attribution arc reached the TX path (round 7,
> uncommitted).** Round 6 stopped publishing occupancy for a slot whose dial moved,
> and round 7's first half stopped publishing its DECODES too (they reach the
> sequencer, Band Activity and the **PSK Reporter** sink, all of which resolve a
> decode against the CURRENT dial — an A→B→A slot would render stations heard
> elsewhere as workable here and spot wrong frequencies to a public network).
>
> **That was not enough, and the gap was on the TX path.** Empty decodes are NOT a
> sequencer no-op: `onSlotAnswering` reads them as "they said nothing", repeats the
> rung and KEYS in the next slot. So a QSY during a receive window would transmit at
> a station no longer in our passband and log the contact on the frequency we left
> (the session pins its dial at start). Suppressing only the moved slot delays that
> by exactly one slot, because every settled slot on the new frequency is silence
> too. **A dial move now ENDS the active session** (`s.seq.Abandon()`), and a moved
> slot is not fed to the sequencer at all — silence has to be observed, not assumed.
> Nothing loggable is lost: a contact already complete for us was logged and retired
> at its final rung, and an incomplete one has nothing to log.
>
> **Round 8 replaced that with the INVARIANT, and that ended the whack-a-mole.**
> The abandon-on-move was both racy and wrong-targeted: it ended whichever session
> was active when a moved slot was PROCESSED, not the one live during the window it
> described — so QSY-then-Call-CQ inside one slot had its brand-new valid session
> killed at the next boundary — and it bypassed `AbandonQso`'s `seqGate` and
> in-flight cancellation. Deleted.
>
> TX safety is now one check at the single transmit funnel (`seqTransmit`): **the
> rig must still be on the dial the session pinned**, else the rung is refused
> (`ErrTxSuperseded`, which existing callers already drop quietly) and the session
> is ended via the new generation-scoped `Sequencer.AbandonIfCurrent` — so a rung
> can never kill a session that replaced it. The pin is the DAEMON's own dial read,
> taken in `sessionTxGate` (the shared preamble of all seven `Start*`), never the
> client-supplied dial carried for logging: same reader on both sides, so exact
> comparison is right. No CAT → guard inert; the keyer owns readiness.
>
> **On-air behaviour change: touching the VFO mid-exchange ends that exchange**
> (`ft8-qso` pushes `active:false`). That is what the radio was already doing
> physically. A session STARTED on the new dial is unaffected.
>
> **Round 9 (uncommitted) fixed three P1s on that guard — all of them "the new path
> did not follow a protocol this codebase already had", and each fixed by copying
> the existing pattern rather than inventing one.**
> 1. **A completed QSO could be dropped.** Group A records the contact whether or
>    not the closing message reaches the air, but the guard abandoned FIRST and every
>    completion callback is generation-guarded, so the bumped generation made it
>    refuse. Now the rung's completion policy runs first, then `AbandonIfCurrent(gen)`
>    — which self-selects: after a Group A completion the generation has moved on so
>    it no-ops, while Group B (no log, no retire on failure) leaves it to fire.
> 2. **An unknown dial disabled the guard.** `TxReady` needs connection + identity;
>    `CurrentDialMHz` additionally needs the selected VFO decoded — so the rig can be
>    ready to key while the daemon cannot say where it is. `dialState()` now separates
>    TRACKED from KNOWN (the same distinction as `Slot.DialTracked` on the RX side):
>    with a source installed the rung must be POSITIVELY validated, and a start whose
>    dial is unreadable is refused up front with the new `ErrTxDialUnknown` →
>    503 `rig_dial_unknown`. No CAT → inert.
> 3. **The terminal publish escaped the lock.** `AbandonIfCurrent` published idle
>    after unlocking, so a concurrent `Start*` could commit and publish ACTIVE first
>    and be overwritten by the stale idle — the hub then caches idle for a live
>    session. `finalrung.go` already documents this exact hazard (from `3c1ee047` /
>    `a301d350`) and publishes under `s.mu`; now this does too.
>
> **Round 10 (uncommitted): five TX/attribution INVARIANTS written down**, in
> `internal/ft8/CLAUDE.md` so they auto-load in the package where they were being
> missed (pointer from `docs/ft8.md`; the canonical list is not duplicated). Every
> P1 in this arc violated one of them. They are stated in operator-observable terms
> — a logged row, RF keyed or not, a published status, a spot emitted — because the
> field-level assertions from this arc were all deleted within a round or two while
> the behavioural ones caught real defects.
>
> Round 10 also fixed two more P1s, and the first is a direct vindication of that
> shift: preserving the QSO across a refused final rung filed it **on the band we
> moved to** — the sink preferred a LIVE dial read over the session's — so a
> wrong-band row would have been forwarded to QRZ and ClubLog. Worse than losing it.
> The session's pinned dial is now stamped onto the completion
> (`stampCompletionPath`, the seam already used for Service-owned completion facts)
> and the sink prefers it, via an extracted, unit-tested `resolveQsoDialMHz`. The
> old live-read preference existed because the CLIENT dial went stale across a
> Call-CQ pile-up; the pin has neither problem. Second P1: `TransmitNext` never went
> through `sessionTxGate`, so manual `/v1/ft8/tx/send` kept keying with an
> unreadable dial — now gated, which also makes the `ErrTxDialUnknown` mapping in
> `handler_ft8_tx.go` reachable.
>
> **The review caught a hole in the invariants within an hour of writing them:**
> "logged exactly once" was satisfied by a wrong-band row. Invariant 1 now says
> "…ON THE FREQUENCY IT HAPPENED ON", and invariant 2 now binds manual sends. And
> the first version of the preservation test only COUNTED callbacks — it passed
> against the reverted code. Rewritten to assert the logged frequency, with a
> deliberately-wrong client dial so it exercises the stamp.
>
> **Round 11 (uncommitted): the dial check moved to the moment of KEYING.** The
> request-time check proved nothing — `TransmitSlot` waits up to ~15 s for the
> boundary, and the rig's state can change in that window (select an undecoded VFO
> and `CurrentDialMHz` goes unknown). `TxController.SetPreKeyCheck` now runs
> `Service.preKeyDialCheck` immediately before `KeyTx`, so every path to PTT —
> manual send and sequencer rung — is gated at the moment it matters. Refusing
> there aborts without keying and the normal failure path still runs the completion
> callback, so invariant 1 holds. Its rules differ by caller: unknown dial always
> refuses; the pinned-dial comparison applies only while a SESSION is active, so a
> stale pin cannot block a manual send. Also fixed: the request-time check now goes
> LAST (after armed / in-flight / readiness) — placed first, it reported
> `rig_dial_unknown` for a disarmed send and masked in-flight conflicts.
>
> **Round 12 (uncommitted): the pre-key refusal now retires the session.** The gate
> fires inside the launched TX goroutine, so `seqTransmit` never sees the error and
> its synchronous refuse-then-retire policy cannot run — a rung with no completion
> callback (most of them) suppressed PTT and left the exchange running.
> `startTransmission` gained an `onDialRefusal` hook, invoked strictly AFTER
> `onDone` (retiring first bumps the generation and a Group A contact's callback
> refuses — the a76f1f61 trap) and generation-scoped by the caller. Narrow by
> design: only a frequency refusal retires; a key/play failure stays transient.
>
> **Severity note:** the finding read as an indefinite leak. It is bounded — the
> NEXT rung's synchronous check catches the same condition, so the session
> self-retired within a slot or two. The fix matters because invariant 5 should not
> depend on a later rung happening to run.
>
> Rounds 4-8 were whack-a-mole because every fix reacted to an observed dial
> TRANSITION; transitions can be missed, mis-timed, or attributed to the wrong
> session. The invariant cannot be. **A codex P1 claiming a boundary QSY escapes
> detection was REFUTED** — per-batch sampling means consecutive samples bracket
> every instant, so exactly one slot is always flagged (verified by walking a change
> through every position across a boundary). It was believed because it cited a
> STALE round-5 comment of ours as evidence. That comment, and three more from the
> arc, are corrected.
>
> Also corrected: the `missing_from_unsupported` message claimed any stampless
> destination "keeps a full copy". Only true of a row mirror — the dev stub
> registers no ADIF prefix and mirrors nothing — so both the daemon message and the
> SPA copy now state only what a missing prefix proves.
>
> Proven by reversion; gate green (gofmt, vet, `go test ./internal/ft8
> ./internal/api -race`, prettier/eslint/svelte-check, **747 frontend tests**).

> **2026-07-27 — the occupancy panel, rounds 4 to 6. Round 5 is the one that
> matters: the fix moved to the SOURCE, and the client-side guesswork is gone.**
>
> Round 4 (`f6ea7ce2`, committed) added a post-band-change quarantine keyed on the
> daemon slot clock. Round 5 deletes it. The review of `f6ea7ce2` showed the anchor
> can lag the capture clock by TWO slots, because a slot is published only after it
> decodes: with the last published slot at 11:59:45 and the rig moving just after
> 12:00:15, the straddling 12:00:15 capture sits exactly 30 s past the anchor and
> was admitted by the very test meant to reject it. No threshold repairs that — a
> delayed-but-steady pipeline is indistinguishable from a live one from inside the
> browser, so **the client cannot establish that a capture happened after the QSY.**
> Three rounds of client-side approximation failed for that one reason.
>
> **The daemon now says which frequency it measured on.** `Slot.DialMHz` /
> `Slot.DialChanged` are stamped by the scheduler from a dial read taken once per
> boundary — that instant is both the END of the slot being emitted and the START
> of the next, so a slot is attributable exactly when the two readings bracketing it
> agree. A slot whose dial moved mid-window is skipped like a TX slot (it describes
> two bands and belongs to neither); `OccupancyReport.DialMHz` carries the rest to
> the SPA, which stamps the band from the REPORT instead of from wherever the rig is
> when the report lands. Wired with `ft8Svc.SetDialSource(bridgeSvc.CurrentDialMHz)`
> — the same injection shape as `SetCatGate`, so `internal/ft8` still never imports
> `internal/bridge`. `api-endpoints.md` updated; the `ft8-qso` event already carried
> this exact lesson for contacts (`dial_freq_mhz`), so occupancy now matches it.
>
> **Round 6 hardened the same idea in two places.** (1) Endpoint sampling missed an
> A→B→A excursion inside one slot — band-stack recall returns to exactly the
> frequency you left, so a wrong band button corrected within 15 s read as stable
> while most of the window was captured elsewhere. The dial is now sampled on every
> audio batch (~43 ms), so any move inside the window marks the slot unplaceable.
> (2) A CAT-attached daemon no longer emits reports it cannot place at all: `dial_mhz`
> unknown is now as disqualifying as dial-moved. `Slot.DialTracked` separates "no CAT,
> nothing to attribute with" from "CAT present but this slot could not be placed" —
> the bridge only reports a dial once the selected VFO has been decoded, so the second
> is reachable early in a session, and there the operator CAN transmit.
>
> So `dial_mhz` is now absent **only** in the audio-only deployment, where the SPA
> falls back to the arrival stamp and keeps the pre-existing one-slot ambiguity after
> a manual band change. That is display-only and cannot steer anything: FT8 keying
> needs a writable rig, and `TxReady` shares the `rigWritableLocked` precondition with
> the dial read, so no-CAT means no transmit. With CAT it is exact, and there is no
> post-QSY blank window — which is what round 4's P2 was complaining about.
>
> Proven by reversion at every step: neutering the SPA attribution re-validates the
> old band's picture; reverting the daemon halves drops the stamp and publishes the
> straddled slot; reverting to endpoint-only sampling attributes the A→B→A slot to the
> frequency it started on; and reverting the placeability gate publishes a report from
> a CAT-attached session with no dial. Gate green: gofmt, `go vet`, `go test ./internal/ft8 -race`,
> all `cmd/...`, prettier / eslint / svelte-check 0 errors / **743 frontend tests**.
> **This round carries a DAEMON change** on top of the already-undeployed tail.

> **2026-07-26 — the FT8 final-rung session. ~19 commits, all reviewed clean. The
> daemon-side work IS deployed and was validated on air across ~148 QSOs with zero
> duplicates; the frontend polish at the end is NOT (running build `c7f88cbc`,
> HEAD is `6088b931` + uncommitted).**
>
> **1. The final-rung retry cap (the day's main arc).** Our last rung in a QSO
> could repeat unboundedly: the partner has everything they need, so from THEIR
> side the contact is complete, and we would keep calling into a station that has
> moved on. The seven handlers split into two groups by whether the QSO is ours to
> log:
> - **Group A — the contact IS complete for us** (we sent the final RR73/73 after
>   receiving their report). Log it, send ONCE, move on. Shared helper
>   `finalRungDoneLocked` in the new `internal/ft8/finalrung.go`, which also holds
>   the policy write-up. It bumps `sessionGen` so a late callback cannot double-log.
> - **Group B — the contact is NOT complete** (we are still owed something). Cap
>   the retries, then ABANDON: no log, clear the session, back to CQ.
>
> **2. `confirmHold` — the duplicate-QSO fix, and the day's best result.** Live-log
> diagnosis (AC8MR, KI2Y, KE4IHI — 3 dupes in 21) found the mechanism: the partner
> misses our RR73, restarts the exchange, and gets logged a SECOND time. XE1GM was
> caught repeating `R-07` eleven times into our silence. Call-CQ now keeps a
> completed contact *listenable* for a bounded window (`confirmResendLimit` 2,
> `confirmHoldSlotLimit` 5) and re-sends RR73 only to a partner still asking.
> **On air: 3 genuine repairs (SQ2LXX, VK6WTF, HL3KPJ), 12/12 same-slot releases,
> zero throughput lost, zero duplicates in 148+ QSOs.** Two false positives
> (EW8DU, PA3GSM) traced to treating `RRR` and `RR73` as one token — fixed at the
> parser with `message.rogerSignsOff` (`920807a9`), since only `RR73` signs off.
>
> **3. Deliberate duplicates are now expressible.** The SPA's same-session dupe
> guard blocked the operator from re-working a station on purpose. Intent is
> threaded end to end as `allow_duplicate` → `CompletedQso.AllowDuplicate` →
> `qsoservice.Submit(..., force)`; the engaged-call set is keyed `CALL|BAND` off
> the new `dial_freq_mhz` and survives a reload via sessionStorage.
>
> **4. Logging hygiene.** `ft8-all.txt` now rotates (lumberjack, 10 MB × 5 gzipped)
> and is created **0600**; a legacy 0644 file is tightened on open. The survey also
> caught `cmd/smd/startuplog.go` creating `smd.log` 0644 — also now 0600. SM Cloud
> gained a Caddy access log with `Authorization`/`Cookie` explicitly deleted.
>
> **5. README rewritten for the public** (35 → 108 lines) with the
> station-manager.org link and two screenshots. **All attended/unattended language
> was removed** — SM *does* run unattended (a CQ run continues as long as you leave
> it), so the old claim was simply untrue.
>
> **6. Occupancy blank-panel fix** (`6088b931` + follow-up). Two independent bugs:
> the panel locks to the parity we TRANSMIT in and the daemon skips occupancy for a
> slot we transmitted in, so during a CQ run it can NEVER fill — yet it said
> "Waiting for slot…", which implied imminent data and cost the operator a live
> session's worth of confusion. It now names the cause and the action. Separately,
> snapshots were never invalidated on a **band change**, so a QSY rendered the old
> band's picture as current.
>
> ### Open loose ends from this session — READ BEFORE PICKING WORK
>
> - **UNDEPLOYED — the whole tail: `4e223176` … `b0025985`.** The running daemon is
>   `c7f88cbc`. Deploy before the next session or the occupancy panel still lies.
>   What is in that tail:
>   - Count badge right-anchored in the narrow rail (`d66a54f1`). The `999+` cap
>     alone did NOT fix the clipping: `999+` is exactly as wide as `1000`, and a
>     clipped `999+` is WORSE — it reads as an exact `999` (codex P2 on `4e223176`).
>   - **Two P1s on the occupancy fix itself** (`b0025985`), both reachable and both
>     landing on the TX path, since `effectiveOffset` falls back to `suggested[0]`
>     when the operator has not pinned an offset: (1) one shared band tag over two
>     INDEPENDENT per-parity snapshots let a fresh parity revalidate the other
>     parity's old-band data — and during a CQ run the TX parity is exactly the one
>     that never refreshes; (2) the hub replays the last occupancy to a
>     freshly-connected tab and the payload carries NO band, so a QSY plus a browser
>     refresh stamped a pre-QSY snapshot as current. Fixed with per-parity band tags
>     and a three-slot freshness gate.
>   - **Round 3 killed the residual at the source.** The client-side age gate was
>     wrong twice over: reports publish ~16 s after their slot and capture lingers
>     5 s past the last unsubscribe, so QSY-then-refresh sat INSIDE any threshold
>     loose enough for decode latency; and `start_utc` is the DAEMON's clock while
>     `Date.now()` is the browser's, so a skewed host would silently discard every
>     live report and leave the panel permanently empty (codex P1+P2 on `b0025985`).
>     Gate deleted. **The daemon now simply does not replay occupancy to a late
>     subscriber** (`internal/ft8/hub.go`) — decode/tx/qso still replay, because a
>     stale decode list is cosmetic and does not steer where we transmit. The cost
>     is what `handler.go`'s own comment already accepted as the fallback: the next
>     slot is ≤15 s away. No clocks, no window, no residual.
>   - **Rounds 4 and 5 (2026-07-27) closed the QSY door properly** — see the block
>     at the top of Current state. "No residual" above was wrong: replay was one of
>     three doors, and the plain band change was the widest. Round 5 stopped
>     approximating and made the daemon stamp the frequency it measured on.
>   - Worked panel `w-full` and the Session-tile Map removal (both inbox items,
>     closed `b0025985`).
> - **`5ea0ff60` HAS A WRONG COMMIT MESSAGE.** It claims to stop 401 being
>   classified as terminal; it is **docs-only** (backlog.md, +46). The code still
>   treats 401 as terminal (`internal/forwarding/smcloud/smcloud.go:330`), so a
>   token rotation still strands the queue. Do not read git log and assume it is
>   fixed — it is a backlog item.
> - **STILL UNSEEN ON AIR** (the paths the next session should exercise): a repair
>   needing a SECOND re-send, a confirm-hold LIFETIME EXPIRY, and a Group B
>   final-rung CAP. Operator plans 80m/40m/30m to hunt them.
> - **Stuck-TX investigation is parked on an operator experiment**, unchanged from
>   2026-07-24: 2 s tune into the ANTENNA on 20m. Both leading hypotheses (tune
>   duration, FT8 residue) were refuted on a dummy load; RF ingress is the last
>   lead.
> - **Dogfood inbox, untriaged:** add a world-time widget. (The worked-panel and
>   session-panel Map items were done in `b0025985` — the worked-panel fix was
>   `w-full`, NOT `table-fixed`: that was already present, and per CSS 2.1 §17.5.2 a
>   table with `width: auto` ignores `table-layout: fixed` entirely.)

> **LATER 2026-07-25 — the "reasonably robust for public use" pass ran, plus a
> long external-review arc on FT8 TX and on config/forwarder credential handling.
> ~20 commits, every one reviewed to a clean bill. NONE of it is deployed.**
>
> **Robustness pass (operator-staged: fix identified items step-by-step → sweep →
> public-ready). Steps 1, 2, 5 DONE; 3 PARKED; 4 STRUCK AS STALE.**
> - **Step 1 — SUBMODE↔MODE validation** (`fcd45c45` + `35da0b91`). `Submit`
>   rejected an inconsistent pair but `Update` bypassed it, so a `PATCH
>   {"mode":"CW"}` onto an SSB/USB QSO persisted and FORWARDED `CW`/`USB`. Now one
>   shared `validateSubmodeMatchesMode` in `qsoservice/validation.go` called by
>   BOTH paths; submode also normalised on store in both (Submit validated the
>   uppercased value but stored the raw one).
> - **Step 2 — config credential handling. This grew into SEVEN commits and is the
>   most consequential change of the day.** The durable rules, all now enforced
>   daemon-side rather than by the browser:
>   - A **blank credential on a PUT KEEPS the stored value** (`mergeForwarders`
>     used to clear on blank). The safety of every stored secret previously
>     depended on the config SPA stripping empties before the PUT — nothing else
>     had to. The app's forwarders Settings section is unbuilt, so this was settled
>     before it gets written against the old behaviour.
>   - The exception is **`CredentialField.Clearable`**, a per-field marker meaning
>     "empty is a meaningful value the constructor defaults". Only `smcloud.logbook`
>     and the dev `stub.mode` carry it. **NOT inferred from `Kind == "text"`** —
>     most text credentials are REQUIRED (ClubLog email/callsign, SM Cloud url) and
>     blanking one produced a 200 followed by a daemon that would not restart.
>     `Kind` is presentation only; `Clearable` is write policy. Changing a field's
>     Kind no longer changes its write semantics.
>   - A clearable blank is stored as the **canonical `""`**, not the whitespace as
>     sent — the merge classified with `TrimSpace` but persisted verbatim, and
>     `stub.New` compares `mode == ""` exactly, so a stored `" "` bricked startup.
>   - **`PUT /v1/config` now probes every ENABLED forwarder with
>     `forwarding.Build`** — the same call `spawnForwarderWorkers` makes — so the
>     endpoint accepts exactly what the daemon can start with (400
>     `forwarder_unusable`). Disabled entries are skipped, matching startup, so a
>     destination stays saveable while half-configured. The probe runs only when
>     the body carried `forwarders`, so one bad stored destination cannot block
>     unrelated saves. It ALSO runs in the first-run dry run, before
>     `seedDefaultLogbook` writes — otherwise a rejected setup PUT left an orphaned
>     logbook that failed 409 on the retry.
>   - **Constructor errors must not echo credential values.** `smcloud.New` quoted
>     `credentials.url`, which can carry userinfo — and that error is logged as a
>     startup fatal AND was surfaced by the new probe. Fixed at source (the
>     constructor now names the fault, not the value), which also closed a
>     pre-existing leak into the startup log. Contract recorded on
>     `forwarding.Build`.
> - **Step 3 — `bridge.New` dependency validation: PARKED.** `internal/bridge` is
>   being worked by someone else (CI-V TX confirmation / arm fallback). Pick up
>   when that lands.
> - **Step 4 — STRUCK AS STALE (4th stale backlog item).** Both halves were already
>   fixed by the 2026-07-23 sqlite batch: `bootstrap.go`'s `splitState` covers all
>   four pieces of split state, and the cold-insert race is gone (contacted_station
>   is a single upsert; the country cache uses `ON CONFLICT`). The one bare
>   `Insert` — `InsertCountryWithContext` — has **zero production callers**.
> - **Step 5 — cache-warm context** (`3b2a04f5`). The post-commit
>   `contacted_station` warm used the REQUEST ctx, so a client disconnect skipped
>   warming the cache for the callsign just worked. Now a detached 2 s ctx,
>   mirroring the dedupe refetch. Deliberately NOT applied to `submit_batch.go`
>   (CLI-only, where a cancel is the operator's Ctrl-C and stopping is correct).
>   **No direct regression test** — the cancel window isn't deterministically
>   reachable without a seam; the sibling dedupe fix landed the same way.
>
> **FT8 TX decodability guards (4 review rounds → clean, `d66a0bb4` → `e2124231`,
> plus `4730c58a`). Safety-adjacent, ALL UNDEPLOYED AND UNVALIDATED ON AIR.**
> `transmit()` now has two independent checks, because a transmission that cannot
> be decoded must be reported FAILED, not short — success is what LOGS the QSO, so
> a delayed final 73/RR73 could book and forward a contact the other station never
> heard:
> - **Head-loss floor** before `Play`: fail if truncation reached FT8's middle
>   Costas array (`maxDecodableSkip`, ~5.92 s of the 12.96 s waveform; arrays at
>   tone idx 0-6/36-42/72-78, receiver needs 2 of 3). Deliberately looser than the
>   sequencer's implied 4.0 s so it never tightens the working late window.
> - **Slot-overrun check** AFTER `Play` returns: `Play` returns once the device is
>   RUNNING and does device enumeration + `malgo.InitDevice` + `device.Start`
>   inline, unbounded. That delay is UNCOMPENSATED shift (unlike CAT keying
>   latency, which the truncation absorbs), so require
>   `elapsed + audio + txPlayTail ≤ txAudioBudget` (14.5 s). **The `txPlayTail`
>   reserve is load-bearing** — the player's `done` means samples REACHED the
>   device, not that they were emitted. Overrun preserves `ctx.Err()` so a disarm
>   during a slow start stays a normal stop, not `ft8_tx_failed`. This is the ONLY
>   guard covering an UNTRUNCATED rung (a next-slot CQ drops no head, so no
>   head-loss test can see a slow device start).
> - **`RST_SENT` logged the raw SNR, not the clamped on-air report** (`4730c58a`):
>   SNR 99 transmitted `+49` and logged 99, outbound to QRZ/ClubLog. `clampReport`
>   now applies where the report is RECORDED, not just formatted. Reachable because
>   `their_snr` arrives UNVALIDATED from the client (`work_sequencer.go:46`). NOT
>   applied to type-4/FD (they send no report on air, so their SNR is a logged
>   measurement) and `RcvdReport` stays verbatim (that token IS what was on the air).
>
> **config.json diagnostics (`814724ae` → `19ccd71a`, 4 commits).** A malformed
> config was a cryptic pre-logging fatal (`migrating config: parsing config
> document: invalid character '}'…`) — no file, no line, and type errors leaked Go
> struct names. Now: the path, line/column, the offending line carated, and a named
> fix. Covers trailing comma (the caret points at the COMMA, not the brace json
> blames), truncated file, empty file, wrong type, and top-level array. The
> trailing-comma hint is derived from the DOCUMENT, not from matching
> encoding/json's message prose. Line/column is suppressed when a config migration
> re-marshalled the document (offsets would point at bytes the operator can't see).
> **The snippet redacts, and took three rounds to get right** — blacklist →
> allowlist by character → **allowlist by POSITION** (string-aware, inheriting
> multi-line string state). config.json is 0600 *because* it holds SMTP/lookup
> passwords; this message reaches stderr → journal and smd.log at **0644**. Only
> structure and provable keys survive. Residual, accepted: encoding/json's own text
> quotes the single offending character.

> **Session(s) since 232 (2026-07-24 → 07-25) — CI RED RESOLVED, the on-air
> CAT-identity bug fixed, and the "PTT drops mid-SSB" mystery closed. Several items
> below are COMMITTED LOCALLY BUT NOT YET PUSHED/DEPLOYED — see each.**
> - **CI red RESOLVED (pending a push + one green run to confirm).** The `go test
>   (race detector, -short)` red was THREE independent flaky tests, not one — pulled
>   the CI log via `gh` (now installed + authed): (a) `internal/bridge`
>   `TestStillKeyed_StopsOnceTheRigObeys` counted ALL writes (a re-unkey PLUS its
>   follow-up status query) against a tolerance of one → count re-unkeys (`TX0;`)
>   only, matching the sibling `StopsWhenTheClientGoesAway` already fixed 2026-07-23
>   but missed here; (b) **DOMINANT (4/6 reds)** `internal/api`
>   `TestStress_20Clients_50QSOs` blew the 5m per-package `-timeout` under -race →
>   raised the `-race -short` timeout 5m→10m AND scaled the stress down under `-short`
>   (kept both CW+SSB mode cohorts, per a P2 review); (c) `internal/forwarding/worker`
>   `TestWorker_DoesNotClaimOtherForwardersRows` → `no such table` from a shared-cache
>   `:memory:` DB destroyed when its sole pooled conn drops under -race → temp-FILE DB.
>   Commits `16b42897` / `e41c5645` / `68232b1d`, **LOCAL, NOT PUSHED** (CI only runs
>   on push). Verified locally under -race (bridge 200×+40×, api green under -short,
>   worker green). Session-232 active-cycle item 1 is fix-complete; watch the first
>   post-push CI.
> - **On-air CAT bug FIXED: "connected rig's identity is unverified" on a band change
>   while CAT shows green.** Identity confirms only on a decoded IDENTITY frame, which
>   comes ONLY from the READ (`ID;`) — and READ re-fired only on a full 5s liveness
>   silence, which a chatty AI-mode rig never hits. So a lost connect-time ID reply
>   left the rig write-blocked all session (green CAT, every command 409, FT8 "capture
>   deferred"). Fix: an active identity re-probe in `readLoop` — while unconfirmed,
>   re-issue READ on a bounded 1s cadence on ANY decoded frame (READ is queries only,
>   no TX; H2 untouched). Commits `bb3af343` + `2a890373` (2nd = review fix: re-probe
>   BEFORE decode so unparsed keep-alive frames drive it too). **NOT DEPLOYED** —
>   needs `task deploy:local:dev` + an on-air boot→green→band-change eyeball.
> - **"PTT drops mid-SSB" — EXPLAINED 2026-07-25 = the rig's own 3-min Time-Out Timer,
>   NOT SM.** New occurrence (long ragchew w/ an NA station + a fast triple-beep). The
>   log settles it: the two longest transmissions of the session were BOTH exactly
>   180.0s (`06:14:46`, `06:32:09`), NOTHING exceeded 185s (a hard 3:00 cap), and at
>   both drops SM logged ONLY the passive `tx-status 2→0` observation — no `TX0;`, no
>   defensive tx_off, no reconnect. The triple-beep is the Yaesu TOT alarm. The
>   session-232 "daemon wrote NOTHING 06:39–07:16" pair is the SAME thing → the
>   defensive-tx_off-on-reconnect candidate is RULED OUT. Fix is on the radio (FUNC →
>   OPERATION SETTING → GENERAL → TIME OUT TIMER — RAISE it, keep it ENABLED; per ADR
>   0057 the TOT is SM's dead-wire TX backstop, so never disable it). Backlog P3 note:
>   surface/set TOT via CAT + warn before a long TX is cut (idea only, no code).
> - **FT8 sequencing audit — CLEAN.** Swept the whole dogfood log (465 completed QSOs,
>   2017 TX rungs): every rung addressed to the right station, no rung regressions, 0
>   QSOs logged without the full 3-way exchange, TX timing p99=0.25s into slot, 0
>   mixed-parity, 465 completes = 465 logged. The apparent "anomalies" were all the
>   ADR 0033 work-next-live-answerer pileup hops behaving correctly. Nothing to fix.
> - **Also shipped since 232 (2026-07-24, already pushed):** server-side "Not emailed
>   only" logbook filter + session-email not-yet-emailed delta (`60c80460`,
>   `aa5986ee`, `94997cbf`), backlog archive sweep (`f735bc2a`), DecodeFile now
>   rejects wrong-duration WAVs (`d06dc6d2`).
>
> **Session 232 (2026-07-23, later) — ROOT CAUSE CONFIRMED ON AIR. The RTS
> diagnosis from session 231 is now proven by a clean CAT trace, and the fix
> exposed a second Yaesu rig setting that has to change before CAT works at all.**
> - **CONFIRMED — SM was holding data-mode PTT down via RTS.** Deployed
>   `3f1a047a`, operator tuned and stopped it by hand; the trace settles it:
>   `16:12:32 status=1 uncertain=false` (tune keyed **by CAT**) → `16:12:34
>   status=2 uncertain=true` (unkey in flight) → `16:12:35 status=0
>   uncertain=false` (settled in RX). **Rest state now reads `0`**; before the
>   rigdef fix it read `2` — 975 times. The stuck-TX investigation is CLOSED:
>   cause was `"rts": true` in the Yaesu rigdefs against a rig on
>   `RPTT SELECT = RTS`.
> - **The post-unkey `tx-status 2` is the TX→RX tail, NOT a stuck carrier.** It
>   resolves to `0` within ~1 s. This is what made the morning's readings
>   ambiguous — two different conditions produce `2` (benign transition vs a
>   control line holding PTT), and the old log could not tell them apart.
> - **NEW FINDING — `CAT RTS` must be DISABLED on Yaesu.** De-asserting RTS
>   **broke CAT entirely**: on FTdx10/FT-710, `RADIO SETTING → GENERAL → CAT RTS`
>   (factory default **ENABLE**) makes the rig withhold *all* CAT replies unless
>   the PC asserts RTS. RTS therefore has two unrelated jobs and the menu picks
>   one. Symptom is silent — port opens, commands go out, **no error anywhere**,
>   rig just never answers ("capture deferred — rig/CAT not live"). Documented in
>   `cat.md` (settings table + blockquote) and a new `troubleshooting.md` section.
>   **Shipping `rts: false` breaks CAT out of the box for any Yaesu on factory
>   defaults** — a deliberate trade (a dead CAT link is obvious and harmless; a
>   stuck transmitter is neither), but it is a trade, and it is the operator's to
>   revisit.
> - **Observability gap CLOSED (`observeTxStatus`).** Status answers were
>   discarded whenever `txUncertain` was false — i.e. most of the time, and
>   exactly the window in which a control line keying the rig would show. Now
>   logs `bridge: rig tx-status changed {status, uncertain}` on every
>   **transition** (not every frame — TXSTATUS also arrives on AUTO-mode pushes).
>   Log-only; nothing keys, unkeys, alarms or confirms differently.
>   `TestObserveTxStatus_IdleObservationIsInert` pins it.
> - **CI HAS BEEN RED FOR DAYS — TWO SEPARATE CAUSES, ONE STILL OPEN.**
>   (a) **Manual build** — session 231's `cat.md` used a `{{% notice %}}`
>   shortcode from a theme this manual does not have (it has no theme and no
>   `layouts/shortcodes/`), so Hugo aborted the whole site build; a `relref` in
>   `troubleshooting.md` would also have produced a dead link, since the manual is
>   a single page with `#`-anchors (ADR 0036). **Both fixed.** Note Hugo **is**
>   already gated in CI (`ci.yml:164–176`) — the gate fired correctly and was not
>   being watched. (b) **`go test (race detector, -short)` — STILL OPEN.** Red on
>   `1979a3ec`, `072c0a74`, `ddbe46be`, with greens interleaved → **flaky, not
>   broken**. Does NOT reproduce locally: full suite green (2m17s/24 cores), green
>   at `GOMAXPROCS=4`, and green with the Postgres-gated `internal/cloud/store`
>   tests actually running (they skip locally, so they were the prime suspect —
>   ruled out). Two of the three red steps finished in 3:26/3:55, **under** the 5m
>   timeout, so it is a real failure not a timeout. **BLOCKED ON THE CI LOG:** no
>   `gh` installed and the Actions logs API is 403 unauthenticated.
> - **Minor:** `internal/forwarding`'s registry is not idempotent across
>   `-count>1` (`TestRegister_And_Build` panics on re-run in the same binary).
>   Harmless under CI's `-count=1`. Noted, not fixed.
>
> **Session 231 (2026-07-23) — DB REVIEW BATCH SHIPPED, DEPLOYED, THEN THREE
> STUCK-TX INCIDENTS THAT ENDED IN A ROOT CAUSE. The session's real output is the
> last item: SM was holding the operator's data-mode PTT down via RTS.**
> - **ROOT CAUSE FOUND — the Yaesu rigdefs asserted RTS/DTR.** `yaesu-ftdx10` /
>   `yaesu-ft710` shipped `"rts": true, "dtr": true` (the Icom def already set both
>   false, with a comment warning about exactly this). The dogfood FTdx10 runs
>   **PSK/DATA → RPTT SELECT = RTS**, so SM opening the CAT port held the
>   **data-mode PTT down for the life of every connection** — and no CAT `TX0;` can
>   release a PTT a wire is holding. Evidence: **975 of 985** post-unkey status
>   reads answered `tx-status 2` ("TX by OTHER means"); only 10 ever answered `0`.
>   That also explains the 06:36 stuck tune (tune switches to RTTY = a data mode →
>   RTS-as-PTT live) and why **SSB was never affected** (SSB is on DAKY).
>   **FIXED:** both Yaesu defs de-asserted; `RigOverrides` gained tri-state
>   `rts`/`dtr` (there was NO operator escape hatch — it could not be corrected
>   from config.json at all); manual gained a CAT section + a troubleshooting
>   entry. Operator set the rig to DAKY. **CONFIRMED ON AIR the same day — see
>   session 232 above.**
> - **THREE stuck-TX incidents, three different mechanisms** (2026-07-18 USB
>   write-endpoint stall; 2026-07-21 30m, clean kernel log, RFI suspected;
>   2026-07-23 20m/30W, CAT healthy throughout — the rig answered `TX1;`). The
>   third ran ~2 minutes and ended when the operator switched the radio off.
>   **What held:** the restore-skip (no full-power write into a keyed rig) and the
>   TxReady interlock (refused 3 re-tunes). **What didn't:** nothing re-asked the
>   rig, so the alarm sat there.
> - **ADR 0057 — TX-safety scope, the session's other durable output.** Operator
>   called the accumulated protection "slop"; the numbers agreed (28 state fields,
>   ~1,700 lines, 12 commits in 7 days, **4 review rounds in one day each finding a
>   real defect in the previous fix**). Decision: **CAT confirmation is best-effort
>   DETECTION; the rig's TOT is the guarantee.** Kept the three proven guards plus
>   two simple additions (bounded alarm re-probe loop — the alarm could previously
>   latch itself out of every clear path; and a re-unkey retry on a positive "still
>   keyed" answer). **REVERTED** per-cycle reply counting (assumed a 1:1
>   query↔reply correspondence the protocol lacks; blocked TX on a healthy rig
>   after reconnect) and **REMOVED** the half-built marker barrier. Standing rule:
>   **no new TX-safety mechanism without an observed failure.** ADR 0057 is also
>   the standing answer to the anonymous-reply finding clean-room reviews re-raise
>   every round.
> - **`internal/database/sqlite` review batch (5 findings) — ALL FIXED**, each with
>   a regression test verified to fail against the pre-fix code. Reference migration
>   `0003_widen_contacted_station_call` (cache `call` 20→32; **rehearsed on a copy of
>   the live 5,471-row reference.db**); atomic conditional logbook delete +
>   `requireLiveLogbook` guard (FK `ON DELETE RESTRICT` never fires on a soft
>   delete); `bootstrap.go` `splitState` (interrupted split resumed instead of
>   reading as done) + unconditional single-char-prefix purge; contacted-station
>   `INSERT … ON CONFLICT … DO UPDATE`. **Follow-on from review:** `InsertQsoTx`
>   statement ORDER is load-bearing — the guard read as the tx's first statement
>   caused `SQLITE_BUSY_SNAPSHOT` on ANY concurrent commit (two ordinary submits
>   were enough); insert-first + FK→`ErrNotFound` mapping fixed it.
> - **DEPLOYED `2.0.0-alpha.1-787-g00921ce9`** (PocketFFT/CGO). **ClubLog forwarder
>   confirmed LIVE on air** — 3 successful realtime.php uploads. Closes the
>   long-standing "enable at next on-air test" item.
> - **Dogfood inbox TRIAGED** (15 open items, all verified against the code):
>   5 already done, 2 resolved elsewhere, 5 → backlog, 3 closed WAI/no-action.
> - **FT8 session-panel time** now `HH:MM:SS` (the daemon was truncating the SSE
>   payload; Phone/CW was full precision, so FT8 was the odd one out).
>
> **Session 230 (2026-07-21) — REVIEW-DRIVEN HARDENING + LOGGING-SPA RETIREMENT.
> A long reactive session: finished the CSRF arc, worked a whole `internal/qsoservice`
> review batch, and retired the legacy logging SPA. Every commit reviewed + converged
> at ZERO open codex findings.**
> - **CSRF middleware — hardening arc converged (`internal/api/csrf.go`
>   `requireSameOrigin`).** API-wide same-origin guard over the whole mutating surface.
>   **Rebinding-proof** for loopback + specific-IP binds (Host allowlist from OUR config,
>   not the attacker-controlled Host). **Fail-closed (loopback-only)** for wildcard TCP
>   binds AND Unix sockets — incl. a Unix socket behind a reverse proxy that forwards a
>   rebound Host+Origin. Trust model: `r.Host` is the Origin comparison basis ONLY for
>   tcp (where `hostAllowed` validated it); non-browser no-Origin clients (`curl
>   --unix-socket`, CLI) always pass. **Deferred (Option B):** a `server.allowed_hosts`
>   config for a real wildcard-LAN / proxied-hostname deployment (YAGNI until one exists).
> - **2026-07-21 `internal/qsoservice` review batch (7 findings, operator-pasted; all
>   verified real).** #1–#3, #5, #6 FIXED; #4, #7 logged as enhancements (`backlog.md`
>   line 59: FT4/SNR empty-report policy; SUBMODE↔MODE validation).
>   - **#1 mode/call length → migration `0006_widen_mode_call` (schema now v6).** mode
>     CHECK ≤10→**≤20** (DIGITALVOICE is 12 + DSTAR/DMR resolve to it), call BETWEEN
>     1..20→**1..32** to match `IsValidCallsign` — was ABORTING a QRZ-ADIF import on any
>     such QSO (`importBatchFallback` treats a CHECK violation as a hard error). Plus
>     `modes.MaxModeLen=20` catalogue bound (over-length main-modes + submode-parents
>     dropped at load; an invalid override DELETES the key not skips; deterministic
>     sorted resolution — valid parent always beats over-length sibling; `slices.Sort`).
>   - **#2 band/freq coherence.** `submit` now derives BAND from FREQ like `update`
>     (freq authoritative), and **rejects an out-of-band freq** (strict — operator's
>     call). To keep strict safe, the freq→band table (daemon `utils/frequency.go` +
>     **app SPA**, not logging) was widened to ADIF: 60m 5.25→**5.06**, 4m 70.5→**71.0**
>     (else 60m/5.100 false-rejected). Symmetric submit/update.
>   - **#3 TIME_OFF** — reject a present-but-malformed value; absent/empty/whitespace all
>     default to TIME_ON (indistinguishable after `adif.Parse` right-trim; correct).
>   - **#5/#6 error propagation** — batch import ABORTS on a dedupe-lookup infra fault
>     (not per-record); `EnqueueUploads` tombstone probe + `EnqueueDeleteUploads`
>     live-fetch propagate a non-ErrNotFound error instead of bucketing as NotFound /
>     falling through (which could enqueue a DELETE against a live row).
> - **Logging SPA RETIRED (`frontend/logging`).** Embed (`frontend/embed.go`
>   `LoggingFS`/`loggingSPA`) + `GET /` route + CI/Taskfile/release-script build steps
>   all removed; **`GET /` now 302-redirects to `/app/`** (exact `/{$}`, NO root
>   catch-all → any unmatched GET, incl. `/debug/pprof/*` when off, is a clean 404).
>   **Source dir KEPT** for reference (backlog retirement pattern — real delete later
>   gets a preservation tag). The app SPA at `/app/` is now the sole browser client;
>   `ci-local.sh` repointed to `frontend/app`. **Known parity gap** (accepted): the app
>   shows raw error codes, not the logging SPA's 112-line `en.ts` i18n catalogue.
> - **Latent CI red fixed:** `handler_version_test.go` hardcoded schema v5 — migration
>   0006 made it v6 (I missed it at the 0006 commit; caught during the SPA work).
>
> **Session 229 (2026-07-20, later) — CONFIG UI: RIGS SECTION BUILT +
> hardened; CLUBLOG API KEY moved to BUILD-TIME INJECTION (ADR 0054); ADR 0053
> (inbound DX cluster) written; POTA + Call Sense added to backlog. Everything
> at ZERO open codex findings after a multi-round convergent review arc.**
> - **Rigs section** (`lib/config/RigsSection.svelte` + `rigs.svelte.ts` +
>   `api/rigs.ts` + `api/hardware.ts`) — master-detail: rig list (rigdef
>   friendly name + "default" badge; device removed per operator) → read-only
>   identity/FT8-mode/serial-defaults → **editable Connection** (serial port +
>   audio RX/TX pickers from `/v1/hardware`, keeping a stored-but-absent device
>   as "(not detected)"; degrades to read-only text on a static/CGO-free daemon
>   where audio can't be enumerated). "Restart the daemon to reconnect" note.
> - **Concurrency-safe save model (took ~4 codex rounds, every finding real):**
>   a rig save WHOLE-REPLACES the catalogue daemon-side, so save RE-FETCHES,
>   then merges only the connection FIELDS the operator changed — **port, audio
>   RX, audio TX diffed INDEPENDENTLY** — against an **immutable per-draft
>   baseline** (not the live `this.rigs`, which drifts on refresh), and PUTs
>   WITHOUT `default_rig_id` (presence-aware; never clobbers the active-rig
>   pick). Pickers disabled mid-save; `#applyFetched` re-baselines PRISTINE
>   retained drafts; Cancel adopts the current server value. Accepted limitation
>   (documented in code): the re-fetch→PUT isn't atomic — a truly-concurrent
>   second writer is last-writer-wins; closing it needs server-side optimistic
>   concurrency, disproportionate for a single-operator daemon. Tests in
>   `rigs.svelte.test.ts` pin each fix (rx/tx independence, baseline rebase,
>   Cancel-adopts-fresh). Lesson reinforced: [[review-fixes-need-full-scrutiny]].
> - **ClubLog application API key → BUILD-INJECTED (ADR 0054, Accepted).**
>   ClubLog's terms forbid publishing the key in source; the operator also wants
>   it out of `config.json`. So it's stamped into the binary via `-ldflags`
>   (`-X …/clublog.InjectedAPIKey`) from the gitignored `.env`
>   (**`CLUBLOG_API_KEY`** — already set), the same `-X` channel as
>   `main.Version`. Wired into all 5 `cmd/smd` build lines + `dev-rpm.sh` /
>   `release-rpm.sh` / `release.sh` (host `.env` sourced; container gets `-e`).
>   ClubLog `credentials` now hold only `email`/`password`/`callsign` (the SPA
>   add-forwarder descriptor dropped the `api` field). **Startup scrubs a legacy
>   `api` out of `config.json`** (`stripCredentialKey`) — GUARDED on a non-empty
>   baked key so a keyless build can't delete the only copy. **Keyless build =
>   fail-SOFT:** `clublog.New` still constructs (a `Build()` error would abort
>   the WHOLE daemon via `spawnForwarderWorkers`), and every `Submit`
>   short-circuits to **Unreachable** (no network) so a queued backlog is
>   preserved and auto-ships once a keyed build deploys — NOT Terminal (would
>   fail rows permanently). ⚠️ **`build/config.json` HAD the key in plaintext**
>   (line ~59) — the startup scrub removes it on the next daemon start with the
>   new binary; the key value was also surfaced in-session, so **weigh
>   rotation**. 4 codex rounds → CLEAN.
> - **ADR 0053 (inbound DX-cluster spot alerts) — Accepted + written**
>   (review-corrected): new `internal/dxcluster` telnet subsystem; FREQUENCY-
>   first click-to-QSY (spots carry no mode → best-effort inferred → rig literal,
>   omitted if unknown since `SendCommands` is atomic); reuse enrichment (DXCC) +
>   exact-callsign contest-dupe, plus a NEW needed-entity logbook aggregation
>   (contest-dupe can't answer "needed DXCC"). Not built — P2/post-ship.
> - **Backlog:** POTA / activation-management + "Call Sense" predictive-callsign
>   assistance added (2026-07-20 qso-director.com scan). Both P2/post-ship.
> - **Process:** per-commit clean-room codex reviews are now a PERMANENT git hook
>   (operator confirmed) — do NOT propose trimming/batching them
>   ([[codex-commit-reviews]]).
> - **NEXT (config UI):** the next Settings section — **forwarders** (recommended:
>   removes the hand-edit-JSON pain; the ClubLog descriptor is now clean) OR
>   logbooks / DB management — or build the **DX-cluster subsystem** (ADR 0053),
>   or start **SPA retirement** (drop logging/config/logbook routes + embeds, keep
>   source). ClubLog: enable at the next on-air test.

> **Session 229 (2026-07-20, afternoon) — CONFIG UI STARTED: the app Settings
> view's first real section (Station) BUILT + iterated + committed, through a
> long automatic-codex-review arc. Also: SMC milestone 1 DEPLOYED earlier the
> same day (see S228), `smcloudctl`/`smctl` control scripts added.**
> - **Config UI = the app's Settings view** (`frontend/app`, ADR 0044 — the
>   keystone that lets the standalone config SPA retire). Empty landing panel
>   first (`lib/config/Settings.svelte`), then the first real section:
> - **Station section** (`lib/config/StationSection.svelte` + `station.svelte.ts`
>   state + `api/config.ts`) — the ADIF `logging_station` identity block:
>   identity (callsign/operator/owner/name + activity SIG), location & zones
>   (grid/country/DXCC/CQ/ITU/altitude), postal address, equipment, CW. Loads +
>   saves via `/v1/config`. Operator-directed UI polish: grid moved to
>   Location, SIG added (blank by default), hints → tooltips, two-row zones
>   layout, Street/Antenna `w-full max-w-[38rem]`, ADIF-accurate callsign
>   tooltips. **station_callsign is READ-ONLY** (operator decision): the daemon
>   binds each logbook to STATION_CALLSIGN (`submit.go:136` — a live QSO needs
>   STATION_CALLSIGN == the logbook's callsign, else `callsign_mismatch`), and
>   PUT only seeds it at first-run, so editing it post-setup would break
>   logging. operator/owner stay editable (they don't touch the logbook bind).
> - **Data-safety contracts (verified against the daemon, not just comments):**
>   `logging_station` PUT is a WHOLE-BLOCK replace (`overlayConfig`), so the
>   save round-trips the full block (incl. daemon-derived my_lat/my_lon the form
>   never renders) or omitted fields zero. The operational `station` block is
>   NOT sent (presence-aware → omit leaves it untouched; echoing a load-time
>   copy would clobber a concurrent amp/power/band change). After save, the
>   shared app station context is refreshed from the PUT RESPONSE (not a second
>   GET) via the shared `applyStationIdentity` helper in `main.ts`.
> - **FIVE codex review rounds on this section (all findings verified real,
>   all fixed) — a cautionary arc worth reading:** (1) 3 findings — callsign
>   editability (→ read-only), stale shared identity after save (→ refresh
>   hook), operational-block clobber (→ omit it), + a docs P2 (broken
>   `git diff … A..HEAD` range → put the range BEFORE `--`; "byte-identical"
>   binary was false, main.Version moved 712→716). (2) my refresh hook applied
>   `fetchStationContext()`'s configOk:false sentinel unconditionally → wiped
>   the good context on a transient GET failure. (3) fixing that by keeping the
>   last-good context then LOGGED STALE operator/grid → the real fix was
>   dropping the second GET entirely and refreshing from the PUT response. (4)
>   that surgical update missed `setMyGrid` (one of three identity setters
>   `applyStationContext` ran) → displayed bearing desynced from logged →
>   extracted the shared `applyStationIdentity` helper so the boot + save paths
>   can't drift. (5) CLEAN. **Rounds 2–5 were all follow-on defects in my OWN
>   fixes** — see the new memory [[review-fixes-need-full-scrutiny]].
> - Tests: `api/config.test.ts` (round-trip data-safety) + `station.svelte.test.ts`
>   (load/dirty/reset/save, save-latch serialization, no-second-GET). App suite
>   650 green; lint + check clean throughout.
> - **Also this session (dev-loop niggle):** `build/config.json` (the dev
>   working-dir config, NOT the repo) had an invalid-JSON trailing comma from a
>   hand-edited smcloud forwarder entry → `task run:smd` crashed at parse;
>   fixed the comma. This is exactly the hand-edit-JSON pain the config UI's
>   forwarder section will remove.
> - **NEXT (config UI):** QSL sub-section (separate `qsl` block, presence-aware)
>   OR the next top-level section — forwarders (recommended: the hand-edit-JSON
>   surface) or rigs. The app Settings view is now unblocked as the config-SPA
>   retirement keystone.

> **Session 228 (2026-07-20) — review batch #9 (4 findings) BUILT + COMMITTED
> (`internal/cloud` at a CLEAN BILL, incl. the multi-tenancy prerequisite
> migration 0004), the FIX-DON'T-DEFER policy adopted, milestone-1 design
> APPROVED (build PAUSED by operator — do NOT start it unprompted), the
> SPA-retirement direction decided with the parity audit run — then ft8 review
> batch #10 (6 findings, 6/6 real) BUILT + COMMITTED, follow-up round #11 on
> the smcloud batch (2/2 real) BUILT + COMMITTED, round #12 (3/3 real, incl. a
> HIGH regression of my own inside the #10 capture fix) BUILT + COMMITTED in
> three operator-directed steps — and the operator switched to AUTOMATIC
> per-commit clean-room reviews (codex, `.codex-reviews/`). Then SMC milestone
> 1 (multi-tenancy) BUILT → DEPLOYED + verified live on F44 (caught a
> stale-binary trap during the deploy), and `smcloudctl`/`smctl` control
> scripts added — codex catching 3 real reliability bugs in each. Nothing
> deferred anywhere; the whole tree ends the session at zero open review
> findings.**
> - **External review (4 findings) → all four verified → ALL BUILT (the
>   fix-don't-defer trigger session):** (1) padded UUID: server validated a
>   TRIMMED uuid but stored the payload verbatim → 200-accepted rows failed
>   SQLite's 36-char CHECK at restore. Fixed both ends: `server.go` validates
>   the RAW `q.UUID` (padding → 400) and `qsoservice/restore.go` canonicalises
>   (TrimSpace before validate/insert) so pre-fix padded backups restore.
>   (2) global-unique uuid + the ON CONFLICT tenant guard turned a
>   cross-tenant uuid collision into applied:0 reported as SUCCESS (forwarder
>   treats applied:0 as stale-push success) → the later tenant's row was
>   permanently unbackable. Fixed structurally: **migration 0004** rebuilds
>   the qsos PK as `(tenant_id, uuid)`, `Upsert` conflicts on the composite
>   key (tenant guard clause dropped — redundant), `Store.Get` gained a
>   tenant param (uuid alone stopped being a key). The old
>   `TestUpsert_TenantScope` pinned the BROKEN semantics — rewritten as
>   `TestUpsert_TenantScopedUUID` (B's push of A's uuid lands as B's own row;
>   A untouched; exports isolated). Migrate-over-live-data test now asserts
>   the 2-column PK — exactly what the F44 upgrade will run. (3) export
>   buffering — a RE-FIND of our own deferred review-3 #4, which triggered
>   the policy discussion below → pulled forward and BUILT: `ExportSnapshot`
>   streams rows to callbacks from inside the repeatable-read tx (books
>   first, then row-at-a-time; tx never escapes the store), `handleExport`
>   writes rows straight to the wire (byte-identical format — key order,
>   escaping, `[]`, trailing newline all verified; e2e suite runs the real
>   HTTP path). Mid-stream failure after the 200 = truncated invalid JSON
>   the restore client rejects as corrupt. Accepted trade documented IN the
>   handler: the pool conn stays open while a slow client drains, bounded by
>   `exportWriteDeadline` + the request semaphore. (4) gzip negotiation
>   honoured q=0 refusals but not RELATIVE weights → effective-weight compare
>   (explicit beats wildcard; unnamed identity = 0.001, the smallest
>   expressible qvalue, so it's acceptable-but-least-preferred and
>   `gzip;q=0.8` still gets gzip; ties → gzip). **Self-review catches before
>   presenting:** first weight impl defaulted unnamed identity to 1.0 (would
>   have flipped `gzip;q=0.8` to identity — caught against the existing test
>   table); the old tenant-scope test + migrate version pin encoded pre-fix
>   behaviour (updated deliberately); the tx-lifetime trade written into the
>   code so the next clean-room round prices it instead of re-finding it.
>   All suites green `-race` against the dev Postgres (podman `sm-pg`
>   container — was stopped; `podman start sm-pg`, since `task db:pg:up`
>   errors when the container exists). COMMITTED by the operator.
> - **FIX-DON'T-DEFER policy adopted (operator, memory
>   `review-findings-fix-dont-defer`):** paid reviews are deliberately
>   CLEAN-ROOM (ignore prior reviews) so deferred findings re-bill every
>   round — that's the intended goading, proven by #3 above. Every finding on
>   a reviewed package now reaches a terminal state: FIXED (default) or
>   refuted/accepted with the rationale AT THE CODE SITE (the store/doc.go
>   sqlboiler note is the house pattern — it answered the operator's own
>   "why no sqlboiler models?" question this session unaided). "Real but
>   parked in the backlog" is not a valid state for review findings. Goal:
>   production-ready from the outset, per-package clean bills so future
>   review hits are pure signal about new code.
> - **SMC milestone 1 (multi-tenancy) design walk-through DONE + APPROVED —
>   build NOT started (operator: "Don't build yet"):** numbered env pairs
>   `SMCLOUD_CALLSIGN_N`/`SMCLOUD_TOKEN_N` (N from 2), legacy unnumbered
>   pair stays valid as tenant 1 (zero migration on the live box); all in
>   `/etc/smcloud/smcloud.env` (root 0600, systemd EnvironmentFile). Agreed
>   fail-loud boot rules: no silent gaps (scan the whole index range / the
>   environ, so an orphaned `_3` can't be silently skipped; both-or-neither
>   per index), duplicate TOKEN across pairs rejected (the map would collapse
>   two tenants — auth "works" while writing into the wrong tenant), duplicate
>   CALLSIGN rejected (two tokens → one tenant is the device-tokens feature
>   arriving unmanaged). `validateToken`/`normalizeCallsign` per pair;
>   startup log per tenant, never tokens. Rotation today = edit env + restart
>   both ends (401s in the window are fail-soft). **Env→DB credential move is
>   NOT a tenant-count trigger — it's the device-tokens milestone** (second
>   device per tenant, i.e. the POTA laptop after bidirectional reconcile:
>   per-device revocation is the forcing function), gated on the ADR 0040
>   assessment; tokens table would hold hashes, issuance via a CLI subcommand
>   not an admin endpoint. If no second device ever appears, env pairs are
>   the permanent right answer.
> - **Security-audit concern (operator) discussed:** assessment checklist =
>   request-body limits (verify MaxBytesReader exists!), the unauthenticated
>   `/v1/health` DB ping decision, token lifecycle + compromise runbook,
>   systemd unit hardening directives, Postgres least-privilege, log/error
>   secret hygiene. Proposed format: structured self-audit doc → pushed
>   through the external review channel as the second net. **Open option
>   that shrinks the problem: WireGuard/Tailscale overlay instead of public
>   HTTPS** — milestone 1 needs "reachable by two known operators", not "on
>   the internet"; overlay-first defers most of the checklist until the
>   phone/laptop roadmap genuinely needs public HTTPS. Decision not taken.
> - **SPA retirement direction decided (operator):** retire the logging,
>   config and logbook SPAs in favour of `frontend/app` — remove routes AND
>   embeds (keep source dirs for reference; deletion later gets a
>   preservation tag, the ft8-snapshot pattern). Parity audit RUN:
>   `frontend/app` shows RAW bridge-error codes by design (`rig.svelte.ts`
>   ~658 — no i18n catalogue; logging's 112-line `en.ts` + the ny/tum seam
>   need porting); app Logbook HAS backfill/gap-browse/bulk-re-enrich/edit
>   modal but MISSES the 2026-07-19 ClubLog amber retry
>   (`skipped_no_history` handling is legacy-logbook-SPA-only); QSL-awaiting
>   + edit-history were always "(future)" — never built anywhere, nothing to
>   lose; FT8 Settings tab + MyStation have NO app counterpart (both fold
>   into the app Settings-view build); app Settings + Dashboard views are
>   PLACEHOLDERS (`App.svelte` dashed box). **Retirement order: logbook
>   (port the ClubLog retry) → logging (port the i18n catalogue) → config
>   (blocked on building the app Settings view, which also absorbs FT8
>   settings + My Station).** When logging retires, `/` redirects to
>   `/app/`. Not yet directed to build.
> - Also this session: project-wide "what's left" survey (logbook/config/db
>   management + contesting are design-first workstreams; LoTW/eQSL, awards
>   tracking, stats, inbound DX cluster were in NO doc — now captured in the
>   backlog); smcloud sqlboiler question answered (deliberate hand-written
>   SQL, `store/doc.go`); streaming-export backlog item struck as BUILT.
> - **ft8 review batch #10 (6 findings on `internal/ft8`, all verified real,
>   ALL BUILT — first batch fully under fix-don't-defer, zero deferred):**
>   (1) **HIGH, TX-correctness: Abandon → unintended rung.** Rung sites call
>   the injected transmit AFTER dropping seq.mu; an Abandon in that gap found
>   no txCancel yet, so the stale rung keyed RF and published Transmitting
>   after abandon returned. Fix: `transmitLocked()` binds the sessionGen at
>   rung-decision time; `startTransmission` gained a `commitOK` gate checked
>   WHILE HOLDING txMu before txCancel registers (new `ErrTxSuperseded`,
>   dropped quietly by rung callers — no stale-status republish). Abandon
>   bumps gen then reads txCancel under txMu → every interleaving is
>   refuse-or-cancel; argument written at the commit site. Lock order
>   seqGate→txMu→seq.mu, no reversal (all 8 rung sites + 3 onComplete sites
>   call out only after unlock; disarm stays safe via the txArmed re-check
>   under the same txMu hold). (2) **same slot driven twice:** decodeLoop now
>   runs seq.OnSlot BEFORE publishing the actionable decode (also spends the
>   late window on the rung, not occupancy math) + the sequencer records
>   `lastTxSlot` — fireOpening marks, all 8 OnSlot transmit sections skip a
>   fired slot (per PHYSICAL slot; marking pre-transmit is deliberate — every
>   failure mode makes a same-slot retry moot). Was: with max_repeats=1 the
>   pending OnSlot self-abandoned the session mid-opening. (3) **KeyTx
>   latency off the ADR 0032 timebase:** head-truncation moved POST-key —
>   transmitAligned keeps only a pre-key feasibility estimate (don't key PTT
>   for an empty remainder); `transmit` truncates against the actual clock
>   after KeyTx+settle right before Play, so CAT/mode-switch latency shortens
>   the head instead of shifting every symbol's DT. (4) **crashed capture
>   loop leaked the mic:** onCaptureLoopExit now src.Stop()s +
>   hub.clearActivity() (capturing=false had made releaseCaptureLocked no-op
>   forever — device held by a dead session, next acquire overwrote the
>   un-Closed CGO capture); malgoSource.Stop nils m.cap after Close so a
>   double loop-exit can't double-Close. Recovery stays "re-open the FT8
>   view" (0→1 subscriber transition). (5) **antenna-path boundaries:** all 7
>   Start* reset the path only on an ACCEPTED start (a rejected duplicate no
>   longer flips the active QSO to short); onComplete consumes-then-resets so
>   a Call-CQ run's next answerer doesn't inherit long-path. Accepted residue
>   documented at the exchPath field (failed mid-exchange caller answerer
>   leaves the choice in place). (6) **ALL.TXT logged attempts:** WriteTx
>   moved from commit into the onTransmit callback — only once PTT actually
>   keys, real key timestamp, dial via the new txDialMHz in-flight field.
>   7 new pinning tests; validation matrix = full ft8 suite + `-race -short`
>   + `CGO_ENABLED=0 -short` + vet, all green; nothing outside internal/ft8
>   changed. Self-review catch: the capture-test recovery assertion first
>   assumed a 2nd subscriber re-acquires — it doesn't (0→1 by design).
> - **Review round #11 (2 findings on the streaming-export batch, both real,
>   BUILT):** (1) **export pool exhaustion** — the streaming fix's held-tx
>   trade was under-priced: pool 5 vs request semaphore 16 vs 15-min export
>   deadline meant five slow authenticated exports drained the ENTIRE pool
>   (health/uploads/reconcile starved); my own comment's "the request
>   semaphore caps how many" was another soft guarantee claim (16 > 5 caps
>   nothing). Fix: `maxConcurrentExports = 2` try-acquire gate at the top of
>   `handleExport`, BEFORE any store access — over-limit → 503 + Retry-After
>   60; deferred release on every path. (NB the restore client does NOT
>   auto-retry — a gated export surfaces as a failure the operator re-runs;
>   the round-12 review corrected this exact overclaim in the code comment,
>   and the codex review of the docs commit caught it here too.)
>   2 of 5 conns worst-case leaves 3 for the short-lived routes. Pinned by a
>   no-DB test (nil store — proves rejection precedes store access) + the
>   PG-backed export/e2e tests exercise acquire/release. (2) **doc drift** —
>   `sm-cloud-p1.md` + ADR 0050 still described `ON CONFLICT (uuid)` + the
>   tenant guard. Per docs tier rules (historical, append-only): DATED
>   POINTER NOTES, not rewrites — sm-cloud-p1 got a "superseded detail"
>   block; ADR 0050 got a scoped note on the migration-0003 bullet stating
>   explicitly that the revision guard / hash formula / dual-deploy rule
>   STAND and only the conflict-target detail is superseded by 0004.
>   Validation: build + gofmt + vet + fresh `-race` cloud/e2e suites vs dev
>   PG, all green. smcloud-only, rides the milestone-1 F44 rebuild.
> - **Round #12 (3 findings, 3/3 real) — built in three operator-directed
>   steps:** (1) LOW: my round-11 comment claimed the restore client "treats
>   5xx as transient and retries" — FALSE (push-path worker retries;
>   `FetchExport` is one-shot and ignores Retry-After). Comment corrected
>   (server.go `maxConcurrentExports` doc); the same false claim in the
>   handoff prose was independently re-found by the first codex review and
>   corrected there too. (2) HIGH, my regression inside the #10 capture fix
>   (the worst category again): `malgoSource.Stop` nilled `m.cap` BEFORE
>   `<-m.done` while the pump dereferenced `m.cap.Samples()` per iteration —
>   race/nil-panic on a buffered batch. Fixed with both belts: the pump takes
>   the samples channel as an ARGUMENT (never touches the pointer again —
>   race impossible by construction) + the nil-out moved after the drain.
>   Codex-reviewed clean. (3) MEDIUM: path-reset atomicity — completion
>   read+reset via two txMu holds (a set landing between was swallowed) and
>   an accepted start published active before the reset. Fixed:
>   `consumeExchangePath()` (atomic read+clear), Start* inverted to
>   consume-BEFORE + restore-on-reject. The first shape's restore was
>   unconditional — the codex review caught the lost update (a selection
>   landing mid-window overwritten by a stale restore) → **`exchPathGen`
>   generation token:** SetExchangePath bumps it; restore applies only if the
>   generation hasn't moved, so the operator's latest selection always wins.
>   Re-reviewed clean. Tests: consume/restore semantics incl. the exact
>   lost-update scenario; full ft8 + `-race -short` + CGO-free + build green.
> - **NEW STANDING PROCESS — automatic codex commit reviews (operator,
>   2026-07-20):** every commit gets a clean-room review from another AI,
>   landing as `.codex-reviews/<12-hex>.md` (UNCOMMITTED, transient).
>   Workflow: check after commits + at session start → verify findings
>   against code → implement valid ones → DELETE the review doc (deletion =
>   processed; no status editing). Memory `codex-commit-reviews` records it.
>   The reviewer's sandbox cannot run `go test` (read-only /tmp) — cover its
>   blocked verification locally when triaging.
> - **SMC MILESTONE 1 — MULTI-TENANCY PROVISIONING BUILT + COMMITTED
>   (operator-directed, ADR 0052):** `cmd/smcloud` `collectTenantPairs` —
>   legacy `SMCLOUD_CALLSIGN`/`SMCLOUD_TOKEN` pair = tenant 1 (unchanged, so
>   the live F44 env is drop-in), numbered `SMCLOUD_CALLSIGN_N`/
>   `SMCLOUD_TOKEN_N` (N 2..32) add tenants; `run()` loops `EnsureTenant` per
>   pair into the N-entry token→tenant map (`server.New` needed no change).
>   Fail-loud: orphaned halves, unparseable/non-canonical/out-of-range
>   suffixes, duplicate tokens, duplicate callsigns all refuse boot; one
>   `tenant provisioned` log line per tenant (callsign+id, never the token).
>   `smcloud.env.example` + runbook §1.4 gained the add-a-pair→restart
>   procedure. **Three codex rounds on it:** (a) `_02`/`_+2` alternate
>   spellings of one index could cross-combine halves → canonical-suffix
>   rejection (Itoa round-trip); (b) my follow-up set-twice guard was DEAD
>   CODE — systemd EnvironmentFile resolves a repeated key last-wins before
>   exec, so `os.Environ()` never shows it twice; removed the guard + its
>   fabricated test, documented the reality (the canonical-suffix check is
>   the real, reachable protection); (c) clean. Tenant isolation was already
>   structural (migration 0004) + pinned by the existing two-tenant e2e
>   tests.
> - **MILESTONE 1 DEPLOYED + VERIFIED LIVE on F44** (`task rpm:smcloud` →
>   install → restart). Verification caught a real trap first: the initial
>   `enable --now` left the OLD 2026-07-19 binary running (a running unit
>   isn't restarted by enable --now — the same trap the runbook documents for
>   Caddy); a `systemctl restart smcloud` swapped it, then `/v1/version` read
>   `712-g97b6e1da` (HEAD) and health `db:ok` = migration 0004 applied cleanly
>   over the live ~5,772 rows, reconcile `in_sync:true`. **7Q8AC not yet
>   onboarded** (add the env pair + restart when ready).
> - **Control scripts `smcloudctl` + `smctl` (operator-requested parity with
>   smd):** `scripts/smcloudctl.sh` (NEW, packaged into the smcloud RPM at
>   `/usr/bin/smcloudctl`) — start/stop/restart/status/enable/disable for the
>   SYSTEM smcloud unit (auto-sudo; no `import` — smcloud has no local DB).
>   On-crash restart already exists in the unit (`Restart=on-failure`), so
>   "auto restart" = boot autostart (`enable`/`disable`). **Codex found 3 real
>   reliability bugs, all fixed** (then ported the same 3 to the pre-existing
>   `smctl`): (1) `stop` gated on `is-active`, which exits NON-ZERO in
>   `activating(auto-restart)` — so it couldn't stop a crash loop → `is_stopped`
>   state check; (2) `Type=simple` reports active on fork BEFORE the 5s
>   Postgres ping/migrations, so `sleep 1; is-active` gave false success →
>   `stays_active` watches the unit HOLDS active for 8s (>1 restart cycle);
>   (3) `status` did `systemctl status || true` → always exit 0 (dead service
>   reads healthy) → honest exit from the real state. Also added a runbook
>   "updating the binary → `smcloudctl restart`" note (closes the stale-binary
>   gap that bit the deploy). Both control-script commits reviewed CLEAN.
> - **Deploy-smc-again? decided NO (for now):** the smcloud RUNTIME SOURCE is
>   unchanged since the live deploy (`git diff 97b6e1da..HEAD -- cmd/smcloud
>   internal/cloud` reports nothing — note the range goes BEFORE `--`, else git
>   reads it as a path and exits 128; the binary itself isn't byte-identical
>   because the build stamps main.Version, which moved 712→716). Only packaging
>   changed (smcloudctl + runbook), so a redeploy is cosmetic. Hold it until
>   `smcloudctl` is actually wanted on the box OR 7Q8AC onboards (bundle the
>   wrapper deploy with a restart that
>   carries real value).
> - **On journald (F44 ops):** smcloud logs to the systemd JOURNAL, not a file
>   (`os.Stderr` → journald; the unit hardening forbids file writes anyway) —
>   `journalctl -u smcloud`. Operator made journald PERSISTENT on the F44 box
>   (`/var/log/journal` + `SystemMaxUse=500M` drop-in) so smcloud history
>   survives reboots. (smd, by contrast, writes a real file at
>   `~/.local/share/station-manager/log/smd.log`.)
> - **Codex-review timing note:** reviews can LAG a commit — after the
>   smcloudctl-fix commit I was pivoted straight to smctl and skipped its
>   review, which then sat in `.codex-reviews/` until the next check; and the
>   smd review landed a beat after its commit. So on resume/after commits,
>   expect the pending review to be for a PRIOR commit, and confirm which
>   commit a review targets (subject + verified paths) before assuming it
>   covers HEAD.
> - **NEXT (operator-set): (1)** smcloud packaging redeploy (ships
>   `smcloudctl`) — OPTIONAL, no binary change; bundle with 7Q8AC onboarding
>   when ready. **(2)** ClubLog enable at the next on-air test (checklist in
>   `docs/dogfood-inbox.md`). **(3)** stamp-drift steady-state eyeball
>   (`grep reconcile …/smd.log | tail -3` → `in_sync:true`). **(4)** on-air
>   FT8 eyeball of the TX-path changes (commit gate, post-key truncation,
>   keyed-time ALL.TXT) — normal QSO flow + an operator Abandon mid-exchange
>   + ALL.TXT lines matching real key times; rides `task deploy:local:dev`.
>   **(5)** standing: dogfood validations, backlog; SPA retirement + app
>   Settings build queued unless re-prioritised. Phase-2 security gate
>   (ADR 0040 + token rotation) before anything internet-facing.

> **Session 227 (2026-07-19, mid-morning) — FT8 band-change clear BUILT, then
> BOTH S226 NEXT deploys DONE + VERIFIED. Everything through the sixth batch
> is now live on both ends.**
> - **FT8 band-change Band Activity clear (dogfood niggle → built + committed
>   `acfc9bfe`):** a frontend/app port gap — the logging SPA's Ft8Panel already
>   cleared decodes on band change, but frontend/app's `clearDecodes()` was
>   orphaned. Fix: `ft8State.noteOperatingBand(band)` with a `lastSeenBand`
>   field that DELIBERATELY survives view close (so a band change while the
>   FT8 view is closed still clears the persistent pile-up singleton on
>   reopen; reset only in `resetFt8ForTests`) + a one-line `$effect` in
>   `Ft8View.svelte` feeding it `rig.band`. A genuine band-to-band change also
>   clears the pile-up queue; first-band seed doesn't. 4 state tests; app
>   suite 639 green.
> - **`task deploy:local:dev` DONE — daemon restarted 10:02 on
>   `2.0.0-alpha.1-689-gacfc9bfe` (the latest commit):** stamp-drift fix,
>   TX-safety round 4, ClubLog retry-only guard (incl. the force fix), and the
>   band-change clear are all LIVE. Clean boot; reconciler + workers started.
> - **F44 smcloud RPM rebuilt + installed — gzip VERIFIED live:** probe against
>   `http://192.168.1.200:8091/v1/health` shows `Content-Encoding: gzip` +
>   `Vary: Accept-Encoding` when accepting, identity on a plain request.
> - **Reconcile expectation (so the first reading isn't misread):** the FIRST
>   post-deploy hourly reconcile will still show `in_sync:false` + a few
>   upserts — rows stamped by the OLD daemon after the 07:24 run (e.g.
>   SQ3PMX/IT9LCC, QRZ-stamped 07:42) carry residual pre-fix drift; that run
>   heals them. The reconcile AFTER it is the verdict: steady state is
>   `in_sync:true` even mid-pile-up, and any later `in_sync:false` is a REAL
>   drift signal again (and the manifest pull behind it is now gzipped ~10×).
> - **Afternoon arc — SMC production-readiness → rate limiting BUILT → ADR
>   0052 (SMC identity) → review round (3 findings, ALL real — 7th
>   consecutive clean round) FIXED. Committed per-batch by the operator.**
> - **SMC production-readiness answer:** yes for the LAN/backup job it does
>   (drills + audit + hardening all passed), no for the internet — and the
>   rate-limiting gate got BUILT the same afternoon: two-level in-process
>   bound (accept-time connection cap via `netutil.LimitListener` at 4× the
>   request cap — net/http spawns a goroutine per accepted conn BEFORE any
>   handler, so the handler semaphore alone can't stop a connection flood —
>   plus `internal/cloud/server/limit.go` request semaphore, default 16,
>   `SMCLOUD_MAX_CONCURRENT`, over-limit → 503 + Retry-After) + the per-IP
>   Caddy layer (`Caddyfile.example` rate_limit block; runbook §4 rewritten
>   with the plugin install order: xcaddy build → install over
>   `/usr/bin/caddy` → `list-modules` proof → validate → only then enable;
>   stock Caddy fails loud on the directive, so a package-upgrade clobber
>   can't run silently unlimited). **Remaining Phase-2 gate: ADR 0040
>   security assessment + rotating the leaked dogfood token.** Limiter goes
>   live on a box at its next `task rpm:smcloud` rebuild (no urgency on LAN).
> - **ADR 0052 (Accepted): SM Cloud identity** — first and foremost a
>   no-data-loss backup; permanent rules: passive revision-guarded store
>   (no merge logic ever), single writer per QSO (operator is the mutex;
>   guard makes violations safe, not impossible), forwarding-by-origin
>   (pulled rows never forward — also keeps the ClubLog promise), everything
>   richer is a layer over the store. **First milestone (operator): multiple
>   tenancy — provision 7Q8AC as a second hand-provisioned tenant** (map
>   supports N; the work is boot provisioning — today exactly one
>   callsign/token pair). Then: bidirectional reconcile (completes the POTA
>   laptop loop at the `CloudOnly` seam) → device tokens → delta pull →
>   query API; qrz-core/QSL captured as explored-not-decided.
> - **Review round on the batch (3/3 real):** (1) conn-flood gap → the
>   accept-time cap above + narrowed comments + LimitListener semantics
>   pinned by test · (2) runbook §4 had enabled STOCK Caddy before the
>   plugin existed → rewritten (see above) · (3) ADR 0052 overclaimed
>   "reconcile surfaces" single-writer violations — same-second
>   same-revision edits make identical version tuples (`>=` tie guard +
>   payload-less hash → silent divergence); ADR corrected + device/writer-id
>   tie-breaker made a PRECONDITION of the bidirectional-reconcile leg.
> - **Name check (operator question):** "Station Manager" vs Triton Digital's
>   streaming encoder of the same name — no registered mark found, crowded
>   descriptive term, different market; keep the name. Closest real
>   neighbour is 4O3A's ham "Station Manager" console (discoverability, not
>   legal).
> - **Review round 2 on the batch (3 findings, ALL real — 8th consecutive)
>   FIXED:** runbook `restart` not `enable --now` (apt auto-starts stock
>   Caddy; a running unit never swaps binaries on enable --now) ·
>   payload-digest-in-hash is NOT equivalent to the writer-id tie-breaker
>   (summary would flag mismatch forever while the revision+modified_at
>   manifest diff finds nothing → non-convergent; ADR reworded — writer id
>   is the ordering fix) · `connCap` ×4 overflow → parse ceiling 4096 +
>   boundary/overflow tests.
> - **Operator feedback → new standing memory
>   (`verify-guarantee-claims-before-presenting`):** the review rounds have
>   been catching unverified guarantee claims in my own work; from now on
>   every safety/behaviour claim in a batch is adversarially verified + the
>   diff self-reviewed BEFORE presenting — external review is the second
>   net. The operator is trialling model choice on whether the find-rate
>   drops; the milestone-1 build is the test case.
> - **ClubLog API key ARRIVED (operator, 2026-07-19 evening)** — will be
>   enabled for the next on-air test. Full enable checklist + grant-condition
>   facts in `docs/dogfood-inbox.md` (the 2026-07-18/19 ClubLog note): config
>   SPA creds + enable + restart; first `forwarding: success` = proven; 403 =
>   breaker → fix creds + restart; historical catch-up = ONE manual ADIF on
>   clublog.org; distribution decision (embed vs paste-in) still open — check
>   the grant email for privacy conditions first.
> - **SMC deploy decision: LET IT RIDE** — the rate limiter is dormant
>   defense on the LAN box (no protocol change, skew-safe); it deploys as a
>   passenger on the milestone-1 `task rpm:smcloud` rebuild.
> - **NEXT (operator-set): (1) build SMC milestone 1 — multi-tenancy
>   provisioning — NEXT SESSION:** multi-pair boot provisioning in
>   `cmd/smcloud` (today exactly one SMCLOUD_CALLSIGN/SMCLOUD_TOKEN pair;
>   the server's token→tenant map already takes N) to provision 7Q8AC as the
>   second hand-provisioned tenant, per ADR 0052; then ONE F44 rebuild
>   carries multi-tenancy + the limiter. Build under the new
>   verify-guarantee-claims discipline. **(2)** ClubLog enable at the next
>   on-air test (checklist above; verify afterwards: log grep + queue
>   check). **(3)** stamp-drift steady-state eyeball
>   (`grep reconcile …/smd.log | tail -3` → `in_sync:true`). **(4)**
>   standing: dogfood validations, backlog.

> **Session 226 (2026-07-19, morning) — DEPLOY DAY + SIX BUILT BATCHES:
> ADR 0051 went LIVE (first real confirmations observed), the smcloud
> stamp-drift fix + gzip landed, a FOURTH TX-safety review round (5 findings)
> was verified + built, the ClubLog realtime.php promise got ENFORCED in
> code, and TWO review rounds on the gzip/ClubLog batch (3+3 findings, 6/6
> real — the 5th and 6th consecutive clean rounds) were absorbed same
> morning. Committed per-batch by the operator.**
> - **The 04:22 deploy took everything through S225 live.** First live ADR 0051
>   evidence in the log within minutes: `bridge: tx state confirmed idle` on
>   real unkeys during an FT8 pile-up.
> - **Reconcile check → stamp-drift root cause (evidence-verified in the DB):**
>   every post-0050 QSO sits at revision 2 — the QRZ upload stamp
>   (`MarkUploadSuccessWithAdifStampWithContext`) and the session-email stamp
>   both bump `revision` AFTER the smcloud worker already pushed the row, so
>   `in_sync:false` + upserts (7/39/34, then a 94-row heal at 04:24) was the
>   ROUTINE state during operating hours. The real cost is bandwidth, not the
>   upserts: any hash mismatch drops reconcile to the full cloud-manifest GET —
>   O(total logbook), ~650 KB uncompressed at 5.7k rows, no gzip anywhere on
>   the path — per client per drifted hour (the operator's concern exactly).
> - **Stamp-drift fix BUILT (committed):** `forwarding.RegisterRowMirror` flag
>   (smcloud self-registers; re-enqueue targets mirror types ONLY, so a QRZ
>   stamp can never re-upload to QRZ) → `qsoservice.EnqueueStampSync`
>   (update-action rows via the existing UPSERT re-arm, idempotent across the
>   double stamp) → worker `Config.OnQsoStamped` hook fired ONLY on the
>   stamping branch (smcloud stamps nothing → no loop) wired in
>   `spawnForwarderWorkers` + the session-email handler enqueues after its
>   stamp commit. All best-effort; reconcile stays the backstop. Steady state
>   returns to the ~1 KB hash-only check — `in_sync:false` is a real alarm
>   again. 2 worker tests + 5 qsoservice integration tests. **Gzip half stays
>   open in the backlog** (server-side only — Go clients already send
>   Accept-Encoding and auto-decompress, so no lockstep deploy; needs an F44
>   smcloud RPM rebuild).
> - **TX-safety round 4 (operator-pasted review, 5 findings — ALL verified
>   real, 4th consecutive clean round) BUILT P1s-first:** (P1) **restore gated
>   on positive RX confirmation** — new `txConfirmDone` per-cycle channel +
>   `waitTxConfirm`; the tune power/mode restore and FT8 mode restore are
>   SKIPPED when unconfirmed/alarmed (a fixed 150 ms settle previously wrote
>   `PC` full power even if the rig missed `TX0;` — amp-damage territory;
>   clamped-power RTTY + the standing banner is the safe state) · (P1)
>   **cause-aware teardown** — `unkeyOnTeardown(…, fault)` where fault =
>   error-shaped pipeline exit (`ctx.Err()==nil`): a faulted pipe's
>   write-accepted unkey now ALARMS (the incident shape; the supervisor may
>   never reconnect), healthy shutdowns keep the ADR 0051 quiet-uncertain
>   trade · (P2) **per-VFO dial knownness** — `dialKnown` deleted;
>   `CurrentDialMHz` refuses when the SELECTED VFO is undecoded (wrong-band
>   FT8 logs) · (P3) **def-floor tune-power validation** — New + `ResolveTune`
>   dry-run the configured watts against the rigdef (PC floor 5 W on both
>   Yaesu defs; 1 W previously advertised tune:true while every StartTune
>   failed); found + fixed a latent nil-deref (`cfg.Cat` is a nil-able
>   pointer) · (P3) **multi-tab count race** — `clientsMu` orders join/leave +
>   count + publish (a stale 2 could be the LAST event, sticking a lone tab's
>   banner). New tests pin every finding (unconfirmed-skip tune/FT8/TX1-answer,
>   fault-teardown alarm, per-VFO unknown, def-floor); `answerTxStatusQueries`
>   fixture helper keeps healthy-rig tests fast (suite 37 s → 4 s). Bridge 4×
>   `-race` stable; full tree `-race` green.
> - **ClubLog helpdesk exchange (API key still pending):** they conditioned the
>   grant on realtime.php NEVER carrying catch-up batches of pre-existing QSOs
>   (anti-pattern → key blocked; bulk = putlogs.php only). Drafted the
>   plain-text confirmation reply (behaviour claims verified against
>   clublog.go first: realtime insert-only-at-logging-time, single-QSO retries,
>   403 breaker = fix credentials + restart, no runtime reset — 403-era rows
>   go Terminal and need re-send). Revised the inbox note's backfill plan:
>   history (3 failed rows + gap + 5.6k) = ONE manual ADIF upload on
>   clublog.org; a putlogs.php bulk route logged as the longer-term backlog
>   item.
> - **ClubLog backfill ENFORCED off (operator-directed, so the promise can't
>   be broken by a misclick):** `forwarding.RegisterNoBulkBackfill` (clublog
>   registers in init, same idiom as RegisterRowMirror) →
>   `qsoservice.EnqueueUploads` refuses with typed `bulk_backfill_unsupported`
>   BEFORE any queue row is written; the logbook SPA withholds the Upload
>   button for clublog-type destinations (amber "use an ADIF export" note;
>   the "Not on clublog" gap-browse stays — it assembles the export set).
>   Deletes + live logging-time enqueues untouched. Daemon guard test + 3 SPA
>   state tests (block keys on TYPE, not name).
> - **smcloud gzip BUILT (operator-directed, the stamp-drift bandwidth half):**
>   `gzipMiddleware` wraps the whole handler chain
>   (`internal/cloud/server/gzip.go`) — negotiated Content-Encoding + Vary,
>   streaming writer; ~10× on the repetitive manifest/export JSON. 3 tests
>   without Postgres, incl. a stock-Go-client transparent round-trip proving
>   ZERO daemon-side change + no lockstep (skew-safe both directions).
> - **Gzip/ClubLog review round (3 findings, ALL real — 5th consecutive clean
>   round) FIXED:** (P1) `gzipResponseWriter.Unwrap()` — `ResponseController`
>   walks Unwrap chains, so without it handleExport's 15-min write-deadline
>   extension silently failed and every gzip-accepting export (= every default
>   Go client, incl. restore) ran under the server-wide 2-min timeout →
>   slow-link restores would truncate mid-JSON; pinned by a
>   deadline-through-the-wrapper test · (P1) **the blanket ClubLog guard
>   severed the 403-era rows' recovery path** (this endpoint re-arms failed
>   live uploads) → replaced with PER-ROW queue-history distinction: history =
>   live upload → retry allowed (legitimate realtime usage); no history =
>   backfill → refused into new `skipped_no_history` bucket; the SPA button
>   became amber **"Retry failed uploads to clublog"** (tooltip + skip-count
>   notice) · (P2) proper Accept-Encoding negotiation (`acceptsGzip`:
>   q-values/case/x-gzip/wildcard, explicit-beats-wildcard; `gzip;q=0` no
>   longer served gzip) + `Vary` on identity responses too. 14-case parser
>   table + 4 new e2e/integration tests.
> - **Gzip/ClubLog review round 2 (3 findings, ALL real) — built 1+3,
>   documented 2:** (P1) **force bypassed the retry gate** — `force=true`
>   skipped the stamp check, after which an UPLOADED clublog row counted as
>   history → a direct API caller could force-re-arm up to 5,000 delivered
>   QSOs into realtime.php. Fixed both halves: force is REFUSED outright for
>   retry-only destinations (typed `force_unsupported`, fail-loud) AND history
>   narrowed to unfinished insert rows (`action=insert && status != uploaded`
>   — an uploaded row is a delivered QSO, not retry provenance; closes the
>   stampless-uploaded no-force path too) · (P2) **all-refused negotiation →
>   406** — `acceptsGzip` became tri-state `negotiateEncoding`
>   (gzip/identity/not-acceptable; identity keeps its RFC default-acceptable
>   status unless refused explicitly or via `*;q=0`); refusing everything now
>   gets 406 + Vary (behaviour change: bare `*;q=0` now 406s, it refuses
>   identity too). 18-case table + e2e 406 test · (P1 #2, **documented not
>   built**): queue rows aren't durable retry provenance (the ADR 0039
>   startup purge erases a disabled forwarder's failed rows → retry
>   eligibility lost). Accepted: the degraded path IS ClubLog's blessed route
>   (manual ADIF), a KNOWN LIMITATION block sits at the gate in `enqueue.go`,
>   and "durable provenance" is folded into the putlogs.php backlog item —
>   which dissolves the problem (bulk legal in-app = the history distinction
>   stops mattering for recovery).
> - **NEXT (both DONE in S227): (1) `task deploy:local:dev`** — the stamp-drift fix, round-4
>   TX-safety batch, and ClubLog retry-only guard (incl. the force fix) are
>   NOT live (the 04:22 deploy predates them); after deploy, watch the hourly
>   reconcile go quiet (`in_sync:true` even during operating hours).
>   **(2) F44 smcloud RPM rebuild** (`task rpm:smcloud` + install on the F44
>   box) to activate gzip — independent of (1); cut it from the LATEST commit
>   (Unwrap protects restore; round 2 adds the 406 negotiation).

> **Session 225 (2026-07-18, evening) — THE TX-SAFETY MEGA-ARC: the stuck-TX
> incident's root fix, end to end — ADR 0051 designed, built, and hardened
> through TWO adversarial review rounds, all in one evening. Committed
> per-step by the operator.**
> - **Warm-up (post-S224):** header CAT chip → Rig Control panel TOGGLE
>   (route-aware; + the "Rig → Rig Control" rename, both hosts) · the
>   review-trivials batch (middleware Unwrap comment, `uuid_conflict` under
>   force — NB force salts the dedupe key so forced-import UUID collision is
>   the only real case, pinned by test — `importBatchFallback` doc, TX_PWR
>   omit-when-rounds-to-0 with the operator's 5 W-floor note) · SessionPanel
>   truncate FIXED (`table-fixed` — ellipsis classes were inert under auto
>   layout).
> - **TX-safety review round 1 (7 findings, all verified real):** triage split
>   into the ADR (1/2/4) + companion batch (3/5/6/7). **Companion BUILT:**
>   `rigWritableLocked` strike-aware predicate in every mutating entry point ·
>   CI-V snapshots skip while keyed · generation-gated auto-off backstops
>   (with DIRECT tests for the 0%-covered callbacks) · identity un-latch
>   (garbled first ID recovers). **Residual round (a1d031cf):** generation
>   recheck UNDER keyMu in the release paths · keyed recheck INSIDE the cmdMu
>   closures · stale identity_unrecognised hub-cache cleared on confirmation.
> - **ADR 0051 BUILT + Accepted** (~26 files): `txconfirm.go` confirm-or-alarm
>   core (uncertain state; rigdef `read_tx_status` `TX;`→`TXn;` on FTdx10 +
>   FT-710, surgical 9-line def edits; CI-V confirms via ACK; frame-
>   watermarked any-rig-data fallback for query-less defs) · `tx-alarm` SSE
>   (hub-replayed) + persistent red "CHECK YOUR RADIO" banners in BOTH SPAs
>   (dismiss ≠ clear; logging SPA via i18n) · teardown under `keyMu` (final
>   TX0 is the last serialized wire action) · `strandedKeyed` DELETED for the
>   stateless unconditional defensive unkey (supersedes the ADR 0042
>   residual, noted there) · `ErrTxUncertain` → 409 `rig_tx_unconfirmed`.
> - **Review round 2 (8bd88c1b, 5 findings) closed:** defensive recovery
>   UNCONDITIONAL with the full cycle (the fresh-restart shape — the Critical;
>   my own recovery test had seeded state in-process, now restart-modeled) ·
>   uncertainty set BEFORE identity unlocks (no key races the recovery
>   window; wire phase async — my first sync version self-deadlocked the
>   CI-V readLoop awaiting its own ACK, caught by the suite) · teardown
>   retains `txUncertain` on an ACCEPTED write (write-accepted ≠ confirmed;
>   no banner on healthy shutdown — alarm-fatigue trade recorded) ·
>   `SendCommands` refuses while unconfirmed · frame watermark. CI-V test
>   fixtures needed honest updates (reply-arming vs stale queued ACKs —
>   production register-before-write order verified CORRECT). Bridge suite
>   8×-stable under `-race`; api/ft8/cat green; both SPA suites green.
> - **NEXT: `task deploy:local:dev`** — NONE of today's post-morning work is
>   live yet (ADR 0051 + both review batches + banners + trivials + chip
>   toggle + truncate all wait on it). Then the standing dogfood validations;
>   ClubLog key still pending. P0 empty again; the review cadence
>   (paste → verify → triage → build) has now absorbed SIX external review
>   rounds today across smcloud, frontend/app, and the bridge.

> **Session 224 (2026-07-18, from mid-afternoon into evening) — THE USB/TX-SAFETY
> INCIDENT + CLUBLOG ENABLEMENT SAGA. Operator-driven throughout; committed
> per-step.**
> - **P0-class incident (~15:42): rig stuck in CONTINUOUS TRANSMIT.** Root
>   cause chain (kernel+daemon logs cross-referenced): the motherboard's
>   onboard Genesys hub (bus5 port2 — also carries kbd/mouse) degraded from
>   15:35 (`clear tt` EPROTO), then the CP2105's write endpoint stalled
>   (`urb stopped -32` on EVERY write) DURING the RR73 to JO3OER — TX1 keyed
>   the rig before the stall; the unkey TX0 and EVERY backstop after it
>   (18 s auto-off, disarm, release-on-disconnect) were written into the dead
>   pipe and "succeeded" daemon-side. Log looked perfectly clean while the
>   rig sat keyed; operator recovered manually (kill smd + unkey at rig,
>   15:44). NOT a tab-close bug (coincident timing). NB `5-2.3` = the
>   FTdx10's own INTERNAL hub (CP2105 + PCM2903C behind one USB-B) — the
>   morning "Plasma" audio incident was almost certainly this same failing
>   link. **Fixes applied + VALIDATED:** rig moved to a DIRECT root port
>   (bus 7, own controller — config unchanged, by-id is topology-independent),
>   ferrites both ends, FTdx10 **TX TIME OUT TIMER enabled** (FUNC →
>   OPERATION SETTING → GENERAL; 3 min) — and a live 30m TX test (the
>   incident's exact conditions; suspected band-specific RFI via the DX
>   Commander vertical) ran CLEAN: 3 QSOs (RA1OW/R2DR/YO3CUS), ZERO kernel
>   USB errors under watch. **Manual now carries a TOT page** (ft8.md
>   "Before you transmit" + tuning stub pointer) — committed. **Inbox holds
>   the P0-class SM gap for triage:** stuck-TX alarm (daemon KNOWS — unkey
>   errored/liveness dropped inside the TX window — but told nobody; design
>   = ERROR + dedicated SSE alarm → persistent "CHECK YOUR RADIO" banner).
> - **ClubLog enablement saga (start-to-parked):** enabled → first real QSO
>   403'd → the forwarder's 403 circuit breaker + startup discard behaviour
>   VERIFIED live, exactly as documented → creds "fixed" → still 403 →
>   actual root cause: **no application API key** (operator had the app
>   password; the key is APPLICATION-assigned and must be granted by the
>   ClubLog helpdesk). **Key requested 2026-07-18** — form answers drafted
>   from the real clublog.go behaviour (403 breaker, 60s→30min×5 retry,
>   120s/5 pacing, stdlib net/url) + intended-use text; forwarder DISABLED
>   until it arrives (startup purge keeps the queue clean — observed
>   discarding 4 rows). Credential model pinned: per-operator = email +
>   application password + callsign; per-software = the API key → when
>   granted it's SM's key, so 7Q8AC configures only personal fields. Inbox
>   note holds the distribution decision (embed-in-repo vs documented paste)
>   + the re-enable/backfill steps (3 failed rows + gap, or full 5.6k).
> - Also: one false alarm ("CAT lost" = smd simply left stopped after the
>   clublog-disable edit; restarted 16:47, all green), and QSO forwarding
>   verified healthy end-to-end on the new topology (smcloud ~5 s, QRZ on
>   tick, ZR1ADI's interrupted rows self-healed idempotently post-restart).
> - **NEXT:** ClubLog key arrival → inbox note has the full sequence; inbox
>   triage owes verdicts on the stuck-TX alarm (P0-class) + the watchdog
>   validation note; standing S220 dogfood validations + type-4 → ADR 0048
>   flip unchanged.

> **Session 223 (2026-07-18, late afternoon) — BACKLOG TRUTH ARC: three
> consecutive "build X" picks found ALREADY BUILT, so the whole P2 backlog got
> a code-verification sweep + the archive relocation. Committed + pushed
> per-step by the operator.**
> - **Three stale items in a row (each investigated, struck with evidence):**
>   (1) *ADIF `MY_*` export omission* — was real 2026-07-08, fixed the SAME DAY
>   (`ae894b9d`, daemon rebuild-from-DB export replacing frontend/app's
>   client-side builder); verified all 7 archived `sent-adif` files + all
>   5,590 rows' `additional_data` carry the full MY_* set (blob completeness
>   explicitly checked — nothing missing at rest). (2) *`country.dxcc` fill* —
>   built 2026-06-25 (`MergeStationFromCountry` + `DXCCForPrefix`), history
>   backfilled by the 07-16 QRZ rebuild; today 5,589/5,590 carry `dxcc` (the
>   one holdout, 9M6M, is QRZ-classified "NON-DXCC" — correctly not guessed).
>   (3) *configurable operating bands* — `station.operating_bands` shipped
>   2026-07-09, five days BEFORE its own triage entry; the operator is running
>   with `["80m"…"10m"]`; one RigPanel feeds every band surface incl. the
>   digit-jump.
> - **Verification sweep (4 parallel read-only investigators over ~35 open
>   items):** 4 more stale (negative-limit panic → validation 2026-06-19;
>   `default_recipient` operator-email field; FT8 same-session dupe
>   suppression in BOTH frontends; edit-overlay mode dropdown), 5 downgraded
>   to "mostly built" with the true remaining scope recorded (attempt-limit =
>   Settings input only; upload-purge = endpoint+UI only; CQ feedback; FT8
>   freq-step = logging-only; SPA-review clusters ~half done), ~24 confirmed
>   genuinely open and annotated "verified open 2026-07-18" with file:line
>   evidence. Notables: the offset-snap item's DESIGN GROUND MOVED (continuous
>   picker + ★ suggestions shipped under it — re-decide before building);
>   `bridge.New` nil-check premise drifted (no Serial/Cat fields — re-scoped);
>   `sequencer.go`'s "edited from the FT8 Settings tab" comment is aspirational.
> - **Archive relocation:** 13 resolved entries moved to `backlog-archive.md`
>   (new dated section), incl. the operating-bands detail block. Live backlog
>   now: P0 empty · P1 = one operator-decision item (192/193 retest, content
>   annotated for the call) · P2 ≈ 24 evidence-backed open items. Only
>   validation-pending strikes remain in place (type-4, map) by design.
> - **Process lesson (now a memory):** the backlog drifted badly out of sync
>   with the early-July build pace — VERIFY a backlog item against the code
>   before building it; triage entries can postdate their own fixes.
> - **NEXT:** unchanged from S222 (watchdog live at next deploy; S220 dogfood
>   validations; type-4 on-air → ADR 0048 flip). For a build session, the now-
>   trusted shortlist: attempt-limit Settings input (small), RST-validator
>   backport, upload-purge endpoint+UI, or a review-lows batch.

> **Session 222 (2026-07-18, mid-afternoon) — AUDIO INCIDENT + WATCHDOG: live
> Plasma-upgrade capture failure diagnosed and fixed, then the durable
> dead-stream watchdog built the same evening. Committed + pushed.**
> - **Incident:** new KDE Plasma's audio device fiddling destroyed+recreated
>   the rig codec's (PCM2903C) PipeWire nodes under a live FT8 capture —
>   smd's source-output left DANGLING (`Source: 4294967295`), daemon
>   "decoding live slots" on pure silence, ZERO errors anywhere.
>   `pactl move-source-output` refuses a dangling stream; fix was close the
>   FT8 view > 5 s linger → reopen (demand-driven capture = no daemon
>   restart). Decodes confirmed back.
> - **Watchdog BUILT (`internal/ft8/deadsource.go` + scheduler/service
>   wiring):** scheduler-side monitor closes a window at EVERY 15 s boundary
>   (timer fires even with zero samples — the incident's shape; the ring
>   never filled so no Slot was emitted, a decode-side check would never
>   run); dead = starved (< quarter-slot delivered) or silent (all literal
>   zeros — analog inputs always carry ADC noise); 2 strikes (CAT
>   `noDataStrikeLimit` pattern) → warn + async release, whose tail
>   re-acquires for the still-present subscriber → fresh OS stream links to
>   current nodes. Once per session; reacquire-failure falls back to the
>   CAT-reconcile retry. Worst case ~45 s vs silent-forever. 7 tests
>   (pure strike policy + release/reacquire plumbing); ft8 suite `-race`
>   green. Docs: ft8.md capture section + inbox note struck BUILT.
> - **NEXT:** watchdog goes live at the next `task deploy:local:dev` (running
>   daemon predates it); end-to-end validation = next Plasma fiddle or
>   `pw-cli destroy` on the codec node mid-capture. Standing S220 dogfood
>   validations still open (map eyeball, in-place session edit mid-CQ,
>   abandon-fix layer 1, type-4 → ADR 0048 flip).

## Active cycle (the 1–3 things in flight now)

> **▶ RE-UPDATED later 2026-07-25 after the robustness pass. The queue is now
> dominated by DEPLOY + ON-AIR VALIDATION — a large amount of TX-path and
> config-path code has landed unproven:**
>
> - **A. DEPLOY.** `task deploy:local:dev`. Nothing from 2026-07-25 has run
>   outside tests: the CAT identity re-probe, FOUR FT8 TX guards (head-loss floor,
>   slot-overrun, RST clamp, attended-only disarm), the whole config/forwarder
>   credential change, and the config.json diagnostics.
> - **B. ON-AIR eyeball of the FT8 TX guards.** A normal QSO, a mid-exchange
>   Abandon, and — if it can be provoked — a late rung, confirming a rejected
>   transmission reports FAILED and does NOT log a QSO.
> - **C. Robustness pass continues.** Step 3 (`bridge.New` deps) is PARKED behind
>   the in-flight bridge work; then the sweep. Sweep shortlist, clear of
>   `internal/bridge`: the **lumberjack goroutine leak** (336 leaked across an
>   `internal/api` run) and **external-failure surfacing** (SMTP / upload / QRZ
>   errors reaching the operator). The one with RF consequences is the **unbounded
>   FT8 final-rung retry** — `sequencer.go:685-709` puts the repeat cap and
>   skip-if-silent inside `if !confirming`, so a 73/RR73 that keeps failing
>   re-keys PTT every cycle forever — but it sits on the TX path near the bridge
>   work, so coordinate first.
> - **D. Smaller, captured, not yet done:** `their_snr` is unvalidated at the API
>   boundary (type-4/FD still log whatever integer arrives as RST_SENT);
>   `reconcileCat`'s `dropMic` is gated on `capturing`, leaving the ARMED flag set
>   on a CAT drop while armed-but-not-capturing (RF safety holds via
>   `clearFt8TxOnDisconnect` — a state-symmetry gap only); RST_SENT logs positives
>   unsigned (`strconv.Itoa` → "49"), and whether ADIF wants "+49" is unresolved.
> - **E. Config SPA cannot express a `Clearable` reset.** Values are never echoed,
>   so a blank box is indistinguishable from an untouched one; expressing a reset
>   needs an explicit "reset to default" control shown only for clearable fields.
>   Documented at the code site and DEFERRED because that SPA is being retired —
>   **the app's forwarders section should carry it**, and `clearable` is already on
>   the wire in `GET /v1/forwarder-types`.
>
> **Older items from earlier on 2026-07-25 (CI red now closed by a green run):**
>
> 1. **CI red — FIXED locally, PENDING PUSH + a green run.** It was three flaky
>    tests, not one (bridge write-count · api stress `-timeout` · worker `:memory:`
>    DB — full detail in Current state). Commits `16b42897`/`e41c5645`/`68232b1d`
>    are LOCAL; **push, then watch the first CI run** (green across a couple pushes
>    closes it). `gh` is now installed + authed — pulling CI logs is easy going
>    forward (`gh run view <id> --log-failed`).
> 2. **DEPLOY + on-air-validate the CAT identity re-probe fix** (`bb3af343` +
>    `2a890373` — the "identity unverified on a band change while CAT shows green"
>    bug). `task deploy:local:dev`, then boot → green → change band → confirm it
>    recovers within ~1s (no tab reload). Bridge code, unproven on air.
> 3. **Validate the TX-safety additions on air:** provoke an alarm and check it
>    self-clears within a probe interval, and that the **Re-check** button works.
>    Neither has ever run on air. Note the Re-check probe only *interprets* an
>    answer while `txUncertain` is set — its intended use (a standing alarm), so it
>    cannot serve as a general "what is the rig doing" query.
> 4. **DECIDE: is `rts: false` the right shipped default?** It costs every Yaesu
>    on factory settings a dead CAT link until they set `CAT RTS = DISABLE`;
>    `rts: true` instead re-exposes anyone on `RPTT SELECT = RTS` to a stuck
>    transmitter. Current call is safety-first (`false`) + documentation. Revisit
>    only with a reason, not a preference.
>
> **CONSIDERED AND DELIBERATELY NOT BUILT (ADR 0057 applies):** `observeTxStatus`
> treats `2` as *confirmed idle* — correct in that `2` means "not keyed by CAT",
> but the 2026-07-23 failure is precisely the case where `2` meant a genuinely
> stuck transmitter. Session 232's trace supplies a discriminator we lacked: a
> benign `2` clears to `0` in ~1 s, a control-line `2` persists. A "`2` that has
> not cleared after N seconds is not idle" rule is therefore *buildable* — but the
> actual fix for this failure was the rigdef, not another detection layer, and the
> standing rule is **no new TX-safety mechanism without an observed failure**.
> Recorded so the reasoning survives; build only if a failure demands it.
>
> **PTT drops mid-SSB — EXPLAINED 2026-07-25 = the rig's own 3-min Time-Out Timer,
> NOT SM (log evidence in Current state).** The two longest transmissions of the
> 2026-07-25 session were BOTH exactly 180.0s, nothing exceeded 185s (a hard 3:00
> cap), and SM wrote nothing at either drop — only the passive `tx-status 2→0`. The
> triple-beep is the Yaesu TOT alarm. This RETIRES the "defensive-tx_off-on-reconnect"
> candidate: the session-232 "daemon wrote NOTHING 06:39–07:16" pair was the same TOT.
> Fix is on the radio: RAISE the TIME OUT TIMER but keep it ENABLED — per ADR 0057 it
> is SM's dead-wire backstop (the only unkey that survives a dead CAT link mid-tune /
> FT8-TX); NEVER disable it. No SM change. Backlog P3:
> surface/set TOT via CAT so the operator sees the limit + is warned before a cut.
>
> **Stuck-tune reproduction line CLOSED** by the root-cause confirmation: 0/3 at
> 5s, 0/3 at 2s, 0/3 after an FT8 session, all on the pre-fix build — duration and
> FT8-residue hypotheses were both dead because the mechanism was a wire, not
> timing. `scripts/tune-duration-probe.sh` is now disposable; delete it unless a
> new stuck-TX case appears.
>

> **The full ranked queue lives in `docs/backlog.md` → "Worklist index".** This
> section is ONLY what's actively in flight — it does **not** re-rank the backlog
> (that's the backlog's job; this doc points at it).
>
> **▶ NEXT: _FT8 reduced type-4 ladder — WORK A REAL NONSTANDARD STATION ON AIR._** The
> ladder itself is **BUILT + offline-gated (2026-07-16, ADR 0048, session 213)** — daemon
> (`type4.go` / `type4_sequencer.go`), service, `mode:"type4"` routes, SSE `type4:true`, and
> the SPA answer path all shipped; `TestType4_RoundTrip` (RF-safety) + 20-odd unit/sequencer
> tests green; full ft8+api suite + race + static build clean; SPA 545 tests green. **The one
> remaining step is on-air validation** — click a real `CQ PJ4/NA2AA` / `CQ …/D`, complete
> the `bare-calls→RR73→73` exchange, confirm it logs (RST_SENT=SNR, RST_RCVD blank, no grid),
> then **flip ADR 0048 Proposed→Accepted**. Matching is on the **spelled** partner — **no
> 22-bit hash table** was built (ADR 0048 rejected it: go-ft8 exposes no decoded-hash to
> match against, and the partner always spells itself). **NOT deployed to dogfood yet** (a
> CGO build + `task deploy:local:dev` restarts the live daemon — do on operator go-ahead).
> Detail: `docs/ft8.md` "Nonstandard / compound calls". **The 7Q8AC-ship focus below is
> CLEARED** (shipped 2026-07-09); the daily-driver track is `frontend/app` (memory
> `sm-frontend-app-consolidation`).
>
> **▶ Focus (set 2026-07-04): _Next shippable state for 7Q8AC._** The goal is a
> release the external operator (7Q8AC, Malawi, offline-first) can run; "stabilise &
> finish in-flight" is the means. The P0/P1 items below ARE the ship gate — clear them
> before opening any new P2 workstream (theming included):
> - ~~**P0** — `PUT /v1/config` omitted blocks zeroed~~ **FIXED 2026-07-04** (→ archive);
>   `default_logbook.id` stays a **P3** residual (no logbook-switch consumer yet). **P0 now clear.**
> - ~~**P1** — FT8 caller-side sequencing (Call CQ pile-up): on-air validation~~ **PASSED
>   2026-07-04** — 33 QSOs / ~74 min on 17 m, full ladder + auto-resume + enrichment;
>   guaranteed-stop confirmed (rig off → warn + TX stop). One bug found + fixed same session:
>   **FT8 self-decode** (`dropOwnTransmissions`, `TestDropOwnTransmissions`). See archive.
>   (FT8 **Field Day** UI + further FD validation remain **PARKED** — testable only during a
>   Field Day contest; ARRL/RAC-only, so not a 7Q8AC concern. See backlog Parked tier.)
> - ~~**P1** — multi-tab rig hazard~~ **awareness banner SHIPPED 2026-07-04** (daemon
>   `rig-clients` SSE + logging-SPA banner; `TestSubscribe_BroadcastsClientCount`). Full
>   operating-lock (ownership/take-over) → **P2** — not a single-op 7Q8AC blocker.
> - ~~**P1** — bridge review F3/F4~~ **DONE 2026-07-04** (see backlog-archive): F3 tune-restore
>   detached from the request ctx (regression test); F4 `deliverAck` accepted-limitation comment.
> - ~~**P1** — SPA fetch timeouts (flaky-link ship risk)~~ **SHIPPED 2026-07-05 (session 200)** —
>   `safeFetch` default 15 s / 30 s write timeout; a fired timeout → retriable `'network'`. See backlog-archive.
> - **P1** — behavioural retest of the shipped session-192/193 daemon changes on the dogfood daemon
>   (detail: items 1–2 below). **← the one P1 left; needs operator hardware.**
>
> **Parked big workstreams (built on go-ahead, NOT this cycle — see backlog):**
> `internal/api` split (ADR 0043, opportunistic), SM Cloud P1 (ADR 0040), DB-manager SPA.
>
> The numbered items below are the **detail / trail** behind the above (some
> superseded — operator_pick, IC-7300 arc — kept for history):
> 0aa. **`internal/api` split — continue opportunistically (ADR 0043; NOT a standalone project).**
>    Session 197 shipped `httpkit` + the import-freeze ratchet. The bulk per-surface split (ports,
>    per-surface packages, sibling-isolation boundary tests) is **deferred until smcloud pulls the
>    seams** — peel a surface only when cloud work touches it. Do NOT big-bang it (the ~9k-line api
>    test suite is the cost). The `qso-logged` consumer-unification is likewise deferred (spine
>    exists; keep `qso.stored` minimal). ADR 0043 is the map.
> 0a. **SM Cloud P1 — build (on go-ahead; DESIGNED session 196, NOT started).** Per ADR 0040 +
>    `docs/v2-design/sm-cloud-p1.md`, sequence **S1–S6**: Postgres store → `cmd/smcloud` HTTP
>    API (upsert-by-UUID + reconcile + export) → `smcloud` forwarder → daemon reconcile → `smd`
>    JSON-restore. P1 single-tenant; onboarding 7Q8AC (tenant #2) is gated on the security
>    assessment. Memory `project_sm_online_db_community`.
> 0a2. ~~ADR 0039 SPA side~~ **DONE session 196** — logbook "uploaded?" tri-state column +
>    manual upload + `missing_from` filter + config-SPA Forwarders toggles all shipped.
> 0a3. **Bridge review F3/F4 (Low, deferred session 196):** F3 — post-unkey restore skipped on a
>    dead request ctx (detached-ctx fix, mirrors the qsoservice dedupe pattern); F4 — a late CI-V
>    ACK can bleed into the next command's wait (protocol-inherent; a `deliverAck` comment is the
>    fix). **FT8 Band-Activity slot divider (todo-next):** the accumulate divider already shows
>    time+band; add **parity** (`slotParity`), check the `cqToTop` suppression, dogfood, then
>    decide whether the Rx-Frequency pane needs its own grouping (backlog "FT8 accumulate-mode
>    duplicate rows").
> 0b. **DB-manager SPA (spine designed session 195, build pending):** the 4th SPA — files +
>    logbook CRUD + forwarding-queue health + cache inspect + backup/restore + ADIF import;
>    NO schema editing. The reference.db/log-db split (its prerequisite) is shipped+validated.
>    Multi-file switching (active-file pointer, restart-to-switch) + a **log-viewer
>    diagnostics tab** (backlogged) ride with it. Memory `project_sm_db_manager_and_multifile`.
>    *(Session-195 daemon work — ADR 0038/0039, the DB split — is already deploy-validated on
>    the live station, so it does NOT need the behavioural retest in item 1.)*
> 0. **FT8 Spectrum view follow-ups (operator-set opener, still open):** the two **FT8 Spectrum view** follow-ups
>    captured in the backlog — **(1) colour revision** (the first-pass slate/green/amber/
>    orange-red palette wants reworking, reconcile with the shared-theme/dark-mode work) and
>    **(2) drag-to-set the offset indicator** (Pointer Events + `setPointerCapture`, reuse
>    `offsetFromFraction`, persist-on-release, `touch-action:none`, live proximity-colour
>    feedback). Both deferred from session 193 to here. See the backlog "FT8 Spectrum view"
>    items.
> 1. **Behavioural retest on the dogfood daemon** (`task deploy:local:dev` — embeds all three
>    SPAs). Still the biggest unvalidated batch: session-192/193 **daemon** changes —
>    **new-entity DXCC matching** (confirm European Russia + Germany no longer show the `*`),
>    the **config-SPA decode-log toggle** (enable → restart → `ft8.decode_log` writes ALL.TXT),
>    and the **Tx even/odd parity** (pick Even/Odd → first CQ lands on that parity). Plus the
>    FT8 SPA surfaces (`*` marker, pile-up ↑, **Spectrum view** click-anywhere + grading,
>    logbook-count bump), the LSPA trims, and still-unconfirmed session-191 surfaces
>    (Email/PSK/Station/QSL, favicon, eye-glyph, CAT/FT8 toggles).
> 2. **New-entity DXCC table coverage:** the embedded table covers the ~154 entities in the
>    dogfood log. If a known-worked entity shows a stray `*`, add its `primaryDXCCPrefix` via
>    `$SM_WORKING_DIR/dxcc-entities.json` or regenerate (`scripts/gen-dxcc-entities.py`).
>    Memory `project_sm_new_entity_dxcc`.
> 3. **UI themes / dark mode + shared-theme layer (filed 2026-06-24):** the largest UI item —
>    a colour-token refactor across all three SPAs first. (**Cross-SPA nav links + a DEV/version
>    tab-title marker SHIPPED session 196**; the SSE-consolidation follow-up + FT8 settings
>    tooltips/beginner-expert are new backlog items filed session 196.)
> 4. **FT8 occupancy waterfall** — the rendered scrolling-waterfall view (backlog, now with
>    full rationale + feasibility); the soften-the-red strand shipped as the Spectrum view
>    (session 193). The ~10fps cadence is the trigger to revisit PocketFFT for the occupancy
>    FFT (memory `project_sm_realfft_stays_pure_go`).
> 5. **PSK Reporter follow-ups (future, in backlog):** the **retrieve/query side** (who heard
>    *you*) and **generalize to a spot-submitter registry only when a 2nd destination (DX
>    cluster) lands**.
>
> *(Maintenance: rolled Session 181 → archive 2026-07-02 when adding 196; live list is now
> 182–196 = 15 entries, at the ~15 threshold.)*
>
> The FT8-TX items further below are STALE — TX (a)–(e) + answer-a-CQ + caller-side +
> work-a-caller + pile-up stacking all shipped; "auto-sequence" is OUT OF SCOPE /
> QEX-forbidden (attended-only). Read the top `### Session N` entries for true state.

### Near-term goal: Icom IC-7300 CAT (borrowed rig) — ENGINE + RIGDEF SHIPPED & VALIDATED; finishing the rough edges

**IC-7300 CAT is now full-featured & on-rig validated** (Sessions 172–175): CI-V
engine + rigdef, inbound commands via **wait-for-ACK** (ADR 0034 rev), **full
state-mirror polling** for VFO-B/USB-D/split → display parity with Yaesu (ADR
0035), VFO swap (+ optimistic mirror), FT8 band buttons assert USB-D, and **FT8 RX
working** (codec = PCM2901: capture index 4 / playback index 2). FT8 **TX keying
added to the rigdef** (`tx_on`/`tx_off`, unexposed) — bench not yet run.

**⇒ The IC-7300 arc is CLOSED (Session 176, 2026-06-16):** first Icom on-air FT8 TX
validated end-to-end (`-key` bench — keyed on slot, USB-D, clean self-unkey), and the
ADR 0036 cleanup is done (deleted; folded into `config.md` §10.4 #1). No IC-7300
next-action remains.

**Diagnosed, parked — not bugs:** split **control** (a `set_split` toggle;
split *display* already works via the poll); **band-jump `Ctrl+Shift+5–9`** on Icom
(no `BS` equivalent — needs band-stacking register `1A 01` or `set_freq`-to-default,
a design call); **band highlight** (SPA derive current band from freq). The **per-rig
audio model daemon side SHIPPED 2026-06-16** (per-direction name-based `RigConfig.Audio.{rx,tx}`,
config.md §10.4 #1, Session 177) — only the **by-name picker UI** (config-SPA rig-profile editor)
remains. **Commit** any uncommitted arc.

> **The detailed sub-items below (wait-for-ACK fork, USB-D differentiation, freq
> up/down shortcuts) are now DONE** — wait-for-ACK shipped (ADR 0034 rev), USB-D is
> solved by the `26 00` poll (ADR 0035), freq shortcuts work on the rig. Kept for
> history; Sessions 174–175 are the current state.

**⇒ NEXT ACTION (resume here): operator decides the wait-for-ACK fork, then build it.**
Session 173 re-validated the command path standalone and designed the fix —
**"adopt-on-ACK" supersedes the earlier "read-after-write"** (better: no second
round-trip, sidesteps the half-duplex read collision, resolves USB-vs-USB-D on
the command path). The IC-7300 ACKs a commanded change with `FB`/`FA` (~20 ms) and
sends NO broadcast, so adopt-on-ACK is the only way the SPA learns the command
landed. **The full design is in ADR 0034 → "Command path: wait-for-ACK".** Before
coding, the operator picks: **synchronous** (recommended — `SendCommands` waits
~20 ms, `FA`→HTTP error) **vs async pending-queue** (non-blocking, `FA`→SSE error
event); and confirms the data-driven `Command.sets_state` op→state approach. Then
build the chosen variant (classifier + readLoop routing + `cmdMu`/`pendingAck`
waiter + per-op synthesize via `mapStatusToPayload` + `civ_ack_ms` knob; Kenwood
path untouched). Possible refinement: coalesce freq-step key-repeat. Then, in order:
1. **USB-D differentiation** — DECISION PENDING. Accept (documented: the `04` read
   gives base mode, `1A 06` data flag never broadcast) vs build the `1A 06`
   snapshot read + stateful base+flag mode-assembly (options 1+2). Operator made
   the operational case (FT8 leaves USB-D → phone TX in a data slot). Note even
   1+2 goes stale on a silent front-panel data toggle — only polling (rejected)
   fully closes it.
2. **Freq up/down keyboard shortcuts** broken for the IC-7300 — diagnose (not yet
   looked at).
3. **No band highlight** — SPA band buttons don't derive current band from freq
   (SPA-side).
4. **Doc pass** — ADR 0034 (spacing/identity/FB-ACK/wait-for-ACK findings now
   documented), memory `project_sm_ic7300_borrowed` (done), the
   `bridge.timeouts.civ_read_gap_ms`/`civ_ack_ms` knobs, and `install.md`
   prerequisites (Transceive ON, USB Port = Link to [REMOTE], baud = CI-V Baud
   Rate, USB SEND OFF) when shipping. CLAUDE.md serial-bridge bullet when the
   command path lands.

Read strategy (REVISED for Icom by ADR 0035): push for the fast operating
freq/mode **plus** a targeted, collision-aware **poll** of the un-pushed fields
(VFO-B/mode+data/split) — Yaesu stays push-only. Commands use **wait-for-ACK**
(adopt-on-`FB`), not the old read-after-write framing. Validated facts + gotchas
live in ADR 0034 + ADR 0035 + memory `project_sm_ic7300_borrowed`.

### Parked follow-ups (named, deliberate defer)

- **Contest logging not built.** Flagged session 66 (2026-05-16). The SPA today is steady-state casual-QSO logging — no contest mode, no macro keys (though F1, F4–F12 are already reserved by ADR 0007 for this), no exchange-field handling (serial numbers, RST+state, etc.), no real-time dupe checking, no multiplier tracking, no Cabrillo export, no contest-specific ADIF fields (`STX`, `STX_STRING`, `SRX`, `SRX_STRING`, `CONTEST_ID`). Scope question to settle when it's picked up: separate client (e.g. `frontend/contest/`) versus a mode switch inside `frontend/logging/`. Contest logging has different UX rhythm (high rate, keyboard-first, minimal panels) and different field shape (per-contest exchange template) — likely warrants its own SPA in line with the logging-vs-logbook split per `feedback_logging_vs_logbook_scope`, but pin that decision when an operator-driven need surfaces (likely the next CQ WW or similar contest the operator wants to enter). Daemon side is largely already there — `types.Qso` follows ADIF (so contest fields slot in via existing `additional_data` pattern), multi-rig API-aware for SO2R contests, UUIDv7 for sync.

- **FT8 SPA surface — BUILT (superseded the "holding scaffold / log-only" note).** The Operating Mode `<select>` in `LoggingCard` (Phone/CW ↔ FT8) renders `Ft8Panel`: live **Band Activity** decode feed (CQ flag + worked-before enrichment), **Rx Frequency** pane, **Clear Offsets** + the **Occupancy** picker strip, the **Operate** tab (Arm/Call-CQ/Abandon + the live role-aware message ladder), a **Settings** tab (daemon-backed display prefs), and a main-panel footer slot countdown. Decode→QSO is the e4 logging path (a completed *exchange* is a QSO; ADR 0024's integration point, realised via the injected `SetQsoLogger` sink — no `qsoservice` import). Remaining FT8 SPA work is tracked in `docs/backlog.md`: FT8 session-log tab, Rx-pane worked-station enrichment card, footer info-strip, CQ-to-top toggle, and the `operator_pick` answerer stack. `ft8.device` is still index-only (name-matching deferred).

- **FT8 AP-decode hints (ADR 0025, Accepted; pieces 2–4 deferred).** The next decode-recall lever after OSD: feed go-ft8's a-priori decoder a ranked, capped, deduped callsign hint set so it can hypothesise weak signals (the −14 dB tail OSD still misses). **go-ft8 v0.2.0 (session 124) shipped the API** (piece 1 done); the daemon doesn't use it yet. **Decision already shaped (ADR 0025):** SM builds the hint set in a storage-backed provider *outside* `internal/ft8` and injects it via an `APHintProvider` interface seam (mirrors `captureSource`); neither go-ft8 nor `internal/ft8` touches the logbook DB — preserves the ADR 0013/0024 import-graph invariant. Division of labour: SM ranks/caps (≈50–200, mix of heard-this-session + worked-on-band/mode + needed + watchlist); go-ft8 scores + tries top-K (≈2–4) AP hypotheses BP-only. go-ft8 copies/caps the hints + does cheap per-candidate known-bit scoring but **never ranks** — ranking is all SM. **Four separable pieces (ADR 0025):** (1) go-ft8 AP value API + scoring/top-K/diagnostics, (2) `internal/ft8` keeps a recent-heard set in-subsystem, fed as `APCallHints` (stateless; the long-lived `Decoder` is a later optimisation), (3) `internal/ft8hints` provider (blend recent-heard + worked-band/mode + watchlist/needed + later spots), (4) `cmd/smd` injection à la `captureSource`. **Piece 1 (go-ft8 API) shipped in v0.2.0; no in-place `SetAPCallHints` mutator, so AP works in the stateless path — the stateful decoder is now an optimisation, not a gate.** Smallest useful next increment = piece 2 (Service-held recent-heard set fed as `APCallHints` to the existing stateless decode; no DB, no decoder refactor), live-A/B-able like OSD was. Pieces 3–4 (logbook provider + injection) follow. Deferred — operator chose bump-only in session 124. See ADR 0025 + memory `project_sm_ft8_integration`.

- **FT8 TRANSMIT — DECIDED 2026-06-06 (ADR 0029, Accepted); steps (a)/(b)/(c) SHIPPED (2026-06-07), (d) NEXT.** Reverses the old "FT8 TX not in v1" stance (you can't complete an FT8 contact receive-only). **Design:** daemon-owned TX, layered **tones → GFSK audio → audio-output device → PTT → slot timing**, reusing the ADR 0027 guaranteed-stop discipline (`tx_on`/`tx_off` controller-only, never `exposed`). **Manual sequencing FIRST** — operator advances each rung of the fixed CQ→73 ladder; **auto-sequence deferred to a later ADR** (strict superset: same plumbing + an unattended state machine; manual de-risks the TX chain on real RF first). **Library seam:** go-ft8's `EncodeStandardMessage` returns the 79-symbol tone sequence and deliberately stops there (audio/scheduling/PTT/I-O are SM's), standard structured messages only (no free text / compound calls yet). **De-risking lever:** the encode→modulate chain is offline round-trip-verifiable against the shipped decoder (zero RF) before any audio device/PTT exists. **Invariant evolution:** "a decode is NOT a QSO" → "a completed *exchange* is a QSO"; `internal/ft8` imports `qsoservice` (never reverse) so narrow-daemon-scope (ADR 0013) holds by import graph. **TX-frequency selection** = a per-slot spectrum **occupancy / clear-offset picker** (one averaged FFT/slot via the retained CGO-free `internal/audio/realfft.go` + decode `FreqHz`), NOT a rendered waterfall — occupancy is data, not pixels. New audio-**OUTPUT** path mirrors the malgo capture seam (CGO-only, fail-soft, probe-listed device) → live TX needs a CGO build, like live decode. **Build order (RX-safe first):** **(a) per-slot occupancy detector (RX-only, useful immediately — the smallest first increment)** → (b) modulator + offline round-trip → (c) audio-output device → (d) PTT/slot controller → (e) manual sequencer + logging, SPA growing alongside; RF only enters at (c). Multi-ADR, multi-session — each layer may spawn its own ADR. See ADR 0029 + memory `project_sm_ft8_integration`.
  - **Step (a) PROGRESS (2026-06-07):** the per-slot occupancy **detector core is built + wired**. `internal/ft8/occupancy.go` — pure/CGO-free `Occupancy(slot, samples, decodes, cfg) → OccupancyReport`: Hann-windowed Welch FFT (`audio.NewRealPlan(3840)`, 3.125 Hz bins) → median-floor×factor threshold → energy bands; decode `FreqHz` → `[FreqHz, FreqHz+50]` upward-span bands (NOT ±25 — go-ft8's `FreqHz` is the base/sync tone per WSJT-X convention; ADR line corrected and confirmed go-ft8 is right to expose it that way, for TX symmetry); merge (overlap/touch → `both`, conservative) → ranked clear offsets (weights: margin / edge-distance / centeredness, capped at 8). Contract types `OccupancyReport`/`Band`/`SlotRef` + `SlotRefFromTime` (even/odd). Wired into `Service.decodeLoop` (computes per slot, publishes via `LatestOccupancy()` atomic slot) + config `ft8.tx.occupancy.*` (renamed from the ADR's `offset_ranking`; `types.Ft8TXConfig`/`Ft8OccupancyConfig`, pointer-wrapped, zero=default via `resolveOccupancyConfig`). Validated against the real `20m_slot1` corpus slot (26 decodes → 19 bands; both/decode-only/energy-only tiers all firing; suggestions land in real gaps). 14 unit tests + real-slot integration (gated `-short`). **#2 SSE SHIPPED (2026-06-07):** `GET /v1/ft8/events` streams `event: ft8-occupancy` (JSON `OccupancyReport` per slot). Owned by the ft8 subsystem mirroring the bridge: `internal/ft8/occupancy_hub.go` (`occHub` — fan-out + one-slot replay cache, slow-subscriber eviction, ADR 0009 late-subscriber-replay) + `internal/ft8/handler.go` (`HTTPHandler`/`Subscribe`/per-write deadline, no bootstrap poll — replay cache covers late tabs). `decodeLoop` publishes per slot; `LatestOccupancy()` now reads the hub cache; hub closed on `Stop`. Route registered in `api/server.go` only when `ft8Svc.Enabled()` (404→SPA fallthrough otherwise), wrapped in `limitEventSubscribers` (shares the SSE cap with `/v1/events` + `/v1/rig/events`); `api.New` gained an `*ft8.Service` param (cmd/smd + testServer updated). Hub + handler tests incl. `-race`. **SPA display SHIPPED (2026-06-07) — step (a) COMPLETE.** Chosen visual model (operator pick): **compact list, no spectrum strip**. `frontend/logging/src/lib/states/ft8.svelte.ts` — singleton `ft8State` (reactive `connected`/`slot`/`busyCount`/`suggested`/`occupied`), `EventSource('/v1/ft8/events')` listening `ft8-occupancy`, `startFt8()`/`stopFt8()`; null occupied/suggested coerced to `[]`. Stream lifecycle scoped to the FT8 view (`Ft8Panel` onMount/onDestroy — LoggingCard mounts it only when Operating Mode = FT8). `Ft8Panel.svelte`: Band Activity shows `HH:MM:SS · even/odd · N busy` (new `formatUtcClock` in `utils/time.ts`); TX Frequency lists the ranked clear offsets as read-only chips (empty/waiting states handled). 7 `ft8.test.ts` cases (FakeEventSource harness mirroring `bridge.test.ts`); lint/check/format/build all green. Read-only — clicking a clear offset to drive TX is **step (e)**; `occupied` bands carried in state but not rendered (reserved for step e / a future strip). install.md `tx.occupancy.*` knobs still deferred until the picker is interactive.
  - **Live-data validation + refinements (2026-06-07, dogfooding):** detector confirmed accurate against WSJT-X (855 occupied via decode@809; "2341 clear" was a weak decoded station at 2338 — decode tier protecting a station the waterfall barely shows). Added: **energy min-width gate** (`minEnergyBandHz`≈12 Hz, drops single-bin noise slivers; decode/both bands never gated) and a configurable **guard margin** (`ft8.tx.occupancy.guard_margin_hz`, default 10 Hz, 0=off, `*int` so explicit-0 survives resolve) so suggested offsets never sit flush against a neighbour. **Step-(e) picker decided:** a clickable occupancy **strip** (static per-slot, busy shaded / clear selectable — NOT a scrolling waterfall) **alongside** the Clear Slots list; daemon TX gate refuses/snaps overlapping offsets (good-practice enforcement vs WSJT-X's click-anywhere; best-effort at pick time). **New `docs/ft8.md`** captures the whole FT8 picture (enable/build/config/SPA/detector/TX roadmap). Build/workflow: **dev `task` builds (run/run:smd/build/build:smd) pinned CGO-on** (live FT8 without a deploy — `task run:smd` is the fast loop); `task build:smd:static` + CI's embed gate explicitly `CGO_ENABLED=0` (shipped static shape); operating-mode switch now persists to localStorage (survives reload). See `docs/ft8.md` + ADR 0029.
  - **Step (b) SHIPPED (2026-06-07) — GFSK modulator + offline round-trip, ZERO RF.** `internal/ft8/modulate.go`: `Modulate(tones []uint8, offsetHz) []float32` — continuous-phase GFSK (WSJT-X scheme: Gaussian freq pulse BT=2.0, h=1, 6.25 Hz spacing, 1920 samples/symbol, raised-cosine edge ramp), output `(nsym+2)*1920` normalised [-1,1]; `EncodeToSlot(text, offsetHz, dtSec) ([]int16, error)` calls `goft8.EncodeStandardMessage` → `Modulate` → lays into a 180000-sample slot. Tone geometry hardcoded (go-ft8's `ft8SamplesPerSymbol`/spacing are unexported — ADR 0029 export-later note stands). **Round-trip PROVEN:** `TestModulate_RoundTrip` encodes 6 messages across the CQ→73 ladder at 300–2900 Hz → modulate → `DecodeSlot` → text + freq (±2 Hz) recovered every time; `TestModulate_RoundTripOccupancy` confirms a generated signal marks its own slot busy in the step-(a) detector. Cheap shape/empty/length/reject tests un-gated; decode round-trips gated `-short`.
  - **Live DECODE FEED (Band Activity) SHIPPED (2026-06-07)** — RX-display, independent of the TX build order. Decodes were previously only logged; now published. Daemon: ft8 hub generalised from occupancy-only to a multi-event fan-out (`occupancy_hub.go`→`hub.go`, `hubEvent{name,payload}`, per-type replay cache — the bridge pattern), new **`ft8-decode`** SSE event on `/v1/ft8/events` carrying `DecodeReport{slot, decodes:[{text, freq_hz, dt_s, snr}]}` (`snr` added session 162 once go-ft8 v0.3.0 exposed it). `decodeLoop` publishes decode + occupancy per slot. SPA `ft8.svelte.ts`: `ft8State.decodes` rolling history (newest-slot-first, freq-ascending within slot, cap 100, monotonic-id keys), listens `ft8-decode`, cleared on stop. `Ft8Panel` Band Activity box renders the scrollable list (operator chose **accumulate/scrollback**, WSJT-X-like, over per-slot-replace); operator has restructured the panel (Main Freq / Band Activity / TX Frequency / Clear Slots columns). Go hub+handler tests incl. `-race`; 12 `ft8.test.ts` cases; lint/check/build green. **Temporary validation view (2026-06-07):** the (otherwise empty until step e) **TX Frequency** panel currently renders `ft8State.occupied` as an "Occupied (Hz)" list with each band's source+level (`both 0.91` / `energy 0.06` / `decode`) — added to debug an operator report that a known-clear freq (855 Hz, per live WSJT-X) wasn't in the suggestions. Diagnosis pending live comparison: if 855 isn't in any occupied band it's purely the ranked top-8 cap/edge-weighting crowding low-freq clear slots out (relax ranking); if it shows `energy 0.0x` it's a threshold false-positive (raise `threshold_factor`); if `decode`, it's real. Step (e) reclaims this panel for the TX picker.
  - **Step (c) SHIPPED (2026-06-07) — audio-OUTPUT device, AUDIO ONLY (no PTT, RF-safe).** `internal/audio/playback/` — the output mirror of `internal/audio/capture`: a malgo/miniaudio **S16, 12 kHz, mono** `Player` behind `//go:build cgo` (`playback.go`), with the pure callback core (`fillFrame` copy+silence-pad, `bytesAsInt16` zero-copy) in an **untagged** `buffer.go` so it's unit-tested in the CGO-free lane (`buffer_test.go`, 7 cases); `doc.go` carries the package clause on the static build. Lifecycle `New → Init → Play(samples) → <done channel> → Stop / Close`: `Play` is **non-blocking** and returns a channel closed when the whole waveform has been handed to the device (natural end); **the caller owns the stop** (`Stop` halts immediately) — exactly the guaranteed-stop discipline step (d)'s controller inherits. The int16 from `ft8.EncodeToSlot` streams straight in (no float conversion, unlike capture's f32→i16 seam). Integration tests gated `integration && cgo` (real hardware: init/list/play-to-completion/stop-mid-waveform). **Config:** `types.Ft8TXConfig.Device` (`ft8.tx.device`, string index, separate enumeration from capture `ft8.device`, system-default when empty). **Smoke tool `cmd/ft8-tx-probe`** (`//go:build cgo`): `-list` enumerates playback devices for `ft8.tx.device`; `-msg=… -offset=… -dt=… [-wav=…]` encodes a standard message and plays it (optionally writes the slot WAV for an A/B decode back through `ft8-decode-file`/`jt9`) — **drives a sound card, not the rig; no PTT, no RF.** All builds green: CGO-free helper tests + static build, CGO build of playback + probe + all `cmd/...`, gofmt/vet clean, full `internal/ft8` + `internal/audio/...` suites pass. **Actual RF first enters at step (d)** (the original "RF at (c)" framing refined — (c) is sound-card audio; PTT keying is (d)). **NEXT (TX): step (d)** — PTT + slot-timing controller (daemon-owned guaranteed stop: key TX via the controller-only `tx_on`/`tx_off`, start `Player.Play` aligned to the slot boundary at +0.5 s, hard-stop on slot end / disconnect / single-flight, mirroring ADR 0027's tune controller).

- **DX cluster integration — idea, needs a discussion (flagged session 123, 2026-06-02).** Receive spots (a telnet DX-cluster / DXSpider feed) and possibly send spots (self-spot / spot a worked station). Not yet scoped — the point of the note is to *have the conversation* before any design. Why it's on the list: (a) "spotted recently" is a named AP-hint source in ADR 0025, so a spot feed directly feeds FT8 AP recall; (b) spots are broadly useful to the logging UX (live band activity, DX/needed alerts). Open questions for the discussion: is this a **daemon subsystem** (a long-lived network connection emitting spots over SSE, shaped like the bridge — consumed by the SPA) or a client feature? How does it respect narrow-daemon-scope (ADR 0013) — spot *reception* is arguably ingest-like, but "needed/award" highlighting and self-spotting touch the logbook, which is logbook-app territory per `feedback_logging_vs_logbook_scope`. Protocol/auth (cluster login by callsign), spot filtering, and dedupe also need deciding. No ADR yet — discuss scope first, then decide whether it's one initiative or split (rx feed vs tx self-spot).

- **Inbound CAT command path — DAEMON-SIDE SHIPPED (ADR 0026 Accepted, session 126); SPA pending.** ⚠ See the **Session 126** entry above for the full state. Daemon-side is done + tested (data-driven `cat` commands, `bridge.SendCommand`, `POST /v1/rig/command`, `BridgeInfo.ops`); implementation committed in `5e8af9b7`, capability unit + docs pending commit. Remaining: `ft8.bands` config, SPA FT8 card, SPA i18n codes for the new HTTP error codes (confirm-by-push validated on the FTdx10 2026-06-04). The planning pass + new ADR this bullet used to ask for are **done** (ADR 0026). The rest of this bullet is the original framing, retained for context: Flagged session 66 (2026-05-16) when "Ctrl+\\ VFO swap" surfaced as a deferred polish item. Operator's mental model: keyboard shortcuts work consistently across manual AND CAT modes (no other shortcut is gated by CAT state). Implementing Ctrl+\\ as manual-only would be surprising UX. Implementing it for CAT mode opens the v1 inbound-command path that ADR 0019 explicitly deferred. Natural scope at that point isn't just VFO swap — it's the full v1 SPA-drives-rig surface: set selected VFO, set split on/off, set frequency, set mode. (PTT stays deferred per ADR 0019 — separate concerns: per-connection asserted state, disconnect-safety-release, future arbitration.) Requires: bridge command-write methods, daemon HTTP endpoint shape (`POST /v1/rig/cmd` or per-field), rigdef SET-command encoders (currently only INIT + READ are encoded), error handling for rig-rejected commands, multi-rig awareness from day one. **Deliberately parked** so dogfooding the existing read-only surface surfaces what actually needs SET-side support and in what order. ADR 0019's "Triggers to revisit — The SPA needs to drive the rig" already captures this. When this gets picked up, expect a planning pass + new ADR before code.

### The immediate next action (post-review, pick a phase)

QRZ port complete, review triage complete, Task #29 (cmd/smd/main.go
tests) complete in session 14, SSE event stream complete in session
14. The forwarding subsystem + its live notification surface is
**done** — the next session picks one of three directions below.

My standing recommendation is a **daemon-only alpha checkpoint**:
cut a tagged build, dogfood via curl + SSE + the existing HTTP
endpoints, and use the results to inform the next subsystem
choice (a second real forwarder vs. bridge/CAT vs. client work).
The forwarding + events surface is the minimum viable
daemon-side feature set; running it against real QSOs for a
week will surface gaps cheaper than guessing at the next
subsystem. If alpha feels premature, the second-best option is
a second real forwarder (ClubLog or LoTW) — it validates the
"prefix-agnostic plumbing" claim and gives the SSE stream more
to say. Bridge/CAT is a larger effort with its own design doc
still to write.

The 8-stage QRZ plan is retained below for historical context;
do **not** re-derive the design decisions captured in it.

**QRZ API reference** (from the operator's paste of QRZ's developer
guide — use this, not an inferred version):

- Endpoint: `https://logbook.qrz.com/api`, HTTP POST with
  `application/x-www-form-urlencoded`.
- User-Agent header required (≤128 chars, should include callsign
  + app name for identifiability).
- **INSERT**: `ACTION=INSERT`, `KEY=<apikey>`, `ADIF=<single-record>`.
  Response: `RESULT=OK|FAIL|REPLACE` + `LOGID` + `COUNT`.
- **UPDATE**: no native update — use `ACTION=INSERT` +
  `OPTION=REPLACE`. Response `RESULT=REPLACE` when it overwrote a
  duplicate. This is what v1 did.
- **DELETE**: `ACTION=DELETE`, `LOGIDS=<id>` (comma list for many).
  Response: `RESULT=OK|PARTIAL|FAIL` + `COUNT`.

**Resolved design decisions** (don't re-open):

- **`Forwarder.Submit` signature**: `(ctx, qso, action, priorUpstreamID string)`
  (stage 1). Worker populates `priorUpstreamID` from the prior
  insert row's `upstream_id` for delete actions only.
- **`Forwarder.AdifPrefix()`** (stage 1). QRZ returns `"QRZCOM"`.
  Worker stamps `QRZCOM_QSO_UPLOAD_STATUS="Y"` +
  `QRZCOM_QSO_UPLOAD_DATE=today` on success (insert/update, not
  delete — soft-deleted QSOs don't export). Failures/transients
  stamp nothing.
- **Delete LOGID wiring**: option A from the session-12 discussion.
  Worker does a DB lookup before `Submit`; forwarder receives LOGID
  via `priorUpstreamID`; empty lookup → terminal "no upstream id
  for delete".
- **QRZ credentials shape**: `{"api_key": "..."}` only — QRZ
  enforces the callsign/logbook match server-side, so a local
  `callsign` field would only introduce drift risk without a
  guarantee. (stage 2, landed)
- **QRZ response classification** (stage 3, landed): per-action
  matrix in `response.go` and `forwarding-implementation.md` §8.1.
  Short form: `RESULT=AUTH` → Terminal (global); `RESULT=OK` /
  `RESULT=REPLACE` → Success with `UpstreamID = LOGID`;
  `RESULT=FAIL` on delete → **Success** (idempotent);
  `RESULT=FAIL` elsewhere → Terminal; `RESULT=PARTIAL` / unknown
  on any action → Terminal; missing `LOGID` on claimed-OK insert →
  Terminal. Transport-level errors (HTTP 4xx/5xx, network, timeout)
  are classified at the `Submit` call site in stage 4 — network
  and 5xx/429 → Transient, 4xx → Terminal.
- **Retry-defaults ownership** (stage 7): each forwarder package
  exports `var DefaultRetry types.RetryConfig`.
  `spawnForwarderWorkers` in `cmd/smd/main.go` looks it up by type.
  Delete the `defaultForwarderRetry` temporary fallback.
- **Test creds**: operator has a QRZ test logbook with `USER` and
  API key in env vars. Used for manual integration verification
  after code lands — **not** for automated tests.
- **Automated tests**: `httptest.NewServer` everywhere, hermetic
  and CI-safe.

**Remaining stages** (each is a committable unit):

| # | Stage | Status |
|---|-------|--------|
| 1 | Extend `Forwarder` interface (`AdifPrefix`, `priorUpstreamID`) | **done** (session 12) |
| 2 | `internal/forwarding/qrz/` skeleton — credentials struct (`api_key` only), `New`, `Type()="qrz"`, `AdifPrefix()="QRZCOM"`, registry init, stubbed Submit, validation tests | **done** (session 13) |
| 3 | Response parser + classification function — `parseResponse` + `classifyResponse` with per-action helpers (`classifyInsert`/`Update`/`Delete`); `AUTH` global, single-LOGID-delete `FAIL` → Success; 26 unit tests | **done** (session 13) |
| 4 | Insert + update `Submit` — real HTTP, `buildForm` + `classifyHTTPStatus`, `DefaultEndpoint`/`DefaultHTTPTimeout`/`UserAgent`, package-internal `newWithEndpoint`; 18 httptest tests + live harness (`TestLive_InsertThenUpdate` quick, `TestLive_InteractiveFlow` with `/dev/tty` pauses); live-validated against real QRZ | **done** (session 13) |
| 5 | Delete `Submit` + worker LOGID lookup — `FetchInsertUpstreamIDWithContext` (defensive ORDER BY, UNIQUE-constraint-aware), worker `resolvePriorUpstreamID` short-circuit, QRZ `buildForm` delete branch; CI fix for `:memory:` + `-race` flake (DSN `cache=shared`); live harness delete via `Submit` | **done** (session 13) |
| 6 | ADIF-stamp wiring — `MarkUploadSuccessWithAdifStampWithContext` writes both the qso_upload transition and a `json_set` stamp on `qso.additional_data` in one tx (no new columns; matches the "additional_data absorbs ADIF spec evolution" invariant); worker `markSuccess` dispatch gates on AdifPrefix + action; prefix-agnostic so new forwarders land without sqlite/migration changes | **done** (session 13) |
| 7 | Retry-defaults ownership refactor — per-forwarder `DefaultRetry` vars, `forwarding.RegisterDefaultRetry` / `DefaultRetryFor` registry companions, `spawnForwarderWorkers` lookup-by-type + loud error for missing defaults, hardcoded `defaultForwarderRetry` deleted | **done** (session 13) |
| 8 | Import `internal/forwarding/qrz` in `cmd/smd/main.go` (regular import — main sets qrz.UserAgent); wired `qrz.UserAgent = "station-manager/" + Version` and `adif.ProgramVersion = Version` at the top of run(); flipped `adif.ProgramVersion` from const to var; ldflags smoke-check passes | **done** (session 13) |

### Follow-ups after the QRZ port

1. **Alpha checkpoint.** Tag a build, dogfood the daemon against
   real QSOs for a week: ingest via `POST /v1/qso` (curl or a
   disposable script), QRZ forwarding on, SSE stream tailed with
   `curl -N` or a browser `EventSource`. The forwarding +
   events surface is the smallest self-contained daemon-side
   feature set; real use will surface gaps cheaper than guessing.
   **My standing recommendation for the next phase.**

2. **A second real forwarder (ClubLog / LoTW / eQSL)**. Exercises
   the "prefix-agnostic generic plumbing" claim. Would validate
   the registry + `DefaultRetry` ownership pattern in anger. Also
   a good smoke test for whether the stage-6 ADIF-stamp json_set
   generalises as cleanly as we think it does.

3. **Bridge / CAT design — substantial progress session 15, now at a
   decision point.** Design is in `docs/v2-design/bridge.md`, rewritten
   in-session from a two-frontend shape to a much smaller Unix-socket-only
   SM-internal multiplexer. The live question is **§6 YAGNI: build now or
   defer?** User lean at session end is *defer*, with `internal/cat` given
   a pluggable transport abstraction (§8.3) so the deferred path costs
   nothing. Recommended next-session work order:

   **a. Answer §6.** Everything else depends on this.
   **b. If deferred:** settle §8.3 (`internal/cat` transport abstraction
      shape) as a design-only exercise. This unblocks the logging app for
      milestone 2 without foreclosing the bridge.
   **c. If built now:** sequence is (i) `internal/cat` transport abstraction,
      (ii) NDJSON schema (§8.1), (iii) bridge implementation, (iv) logging
      app wired through `SocketTransport`, (v) defer CAT control app to its
      own design session.

   My recommendation: **defer the bridge, but do §8.3 now.** Keeps the
   logging app on the fastest path (direct `SerialTransport`) and makes the
   eventual switch to a bridge mechanical.

### Parked follow-ups (low priority, not blockers)

- **Dead-method sweep (SQL audit item 3).** Several sqlite methods
  have only test callers today. The former forwarder-queue
  candidates (`FetchPendingUploads`, `UpdateQsoUploadStatus`) have
  already been deleted in session 11 — they were v1 worker code,
  replaced by the stage-6 purpose-built methods. The remaining
  low-signal methods
  (`FetchQsoSliceByLogbookId`,
  `FetchQsoByDedupeKey`'s no-context wrapper,
  `FetchContactedStationByCallsign`, `FetchCountryByCallsign`,
  `FetchCountryByName`) still need a specific "delete or keep"
  decision. Enrichment methods likely return in milestone 2; the
  QSO list helpers may be dead. Park until we know.
  `FetchQsoCountByLogbookId` removed from this list session 67
  (2026-05-17) — gained a real caller via the new
  `handler_logbook_count.go` for the LoggingCard header badge.
- **SQL audit item 4** — optional `(call, logbook_id) WHERE
  deleted_at IS NULL` composite for contact-history with
  `?logbook=` filter. Defer until a concrete performance
  complaint surfaces.

### v2 design work

- **Pick the ORM/generator approach** → `docs/v2-design/db-layer.md`.
  sqlboiler stays until there's a reason to change.
- **Multi-rig as first-class assumption** — bridge-side shape now
  captured in `docs/v2-design/bridge.md` (first-class from day one
  in the bridge). Data-model side (rig id on `types.Qso`, logbook
  schema impact) still open; address when rig control construction
  starts.

### Deferred features

- **Logging-app text-file fallback reconciliation** — milestone 2+.
- **Enrichment / contacted_station population** — milestone 2.
  Client-side concern; daemon submit path stays fast and network-free.
- **Daemon dashboard / monitoring UI** — post-milestone 2.

### v1 branch follow-ups

- Data race candidate fix (session 6) not yet verified on v1 branch.
- Hardcoded QRZ forwarder — v2 concern, unlikely to be fixed on v1.

### Maintenance

- Update this file at the end of every session.
- **Roll-off:** when the live `### Session N` list passes ~15 entries, move the
  oldest block into `session-handoff-archive.md` (newest-first, verbatim). Last
  roll-off: 2026-07-18 later (Sessions 209–211 → archive; live kept 212–223). Prior: 2026-07-18 (203–208),
  2026-07-13 (Sessions 182–197 → archive).
