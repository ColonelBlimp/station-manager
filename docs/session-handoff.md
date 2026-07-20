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

## Current state (as of 2026-07-20)

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
> - **Deploy-smc-again? decided NO (for now):** the smcloud BINARY is
>   byte-identical since the live deploy (`git diff cmd/smcloud internal/cloud
>   97b6e1da..HEAD` empty) — only packaging changed (smcloudctl + runbook), so
>   a redeploy is cosmetic. Hold it until `smcloudctl` is actually wanted on
>   the box OR 7Q8AC onboards (bundle the wrapper deploy with a restart that
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

> **Session 221 (2026-07-18, early afternoon) — SYNC-PROTOCOL ARC: review round 3
> absorbed (6 findings on internal/cloud sync semantics) AND the resulting
> ADR 0050 revision-counter protocol DESIGNED + BUILT the same day. Three
> commits, all pushed by the operator.**
> - **Review 3 triage (6 findings, all real):** built 4 — **UUIDv7 gate on
>   upload** (PUT rejects non-v7/malformed with 400 BEFORE the EnsureLogbook
>   side effect — Postgres would store any RFC 4122 value but restore admits
>   only v7, so a "successful" backup could be unrestorable), **snapshot
>   export** (`store.ExportSnapshot` — logbooks + records in ONE
>   repeatable-read read-only tx; two autocommit reads could dump QSOs whose
>   logbook is missing from the same export), **composite tenant/logbook FK**
>   (cloud migration 0002 — schema refuses cross-tenant logbook filing
>   independent of handler discipline), **single-JSON-document bodies**
>   (trailing JSON → 400). Deferred with notes: streaming export (bounded at
>   P1 scale, → pre-Phase-2). Kept as permanent test: the **F44 upgrade
>   rehearsal** (`migrate_test.go` — a version-1 cloud DB WITH DATA upgrades
>   via `store.Migrate`, constraints present, data intact).
> - **ADR 0050 (finding 1, the P1) — designed then built on go-ahead:**
>   `modified_at` (SECONDS locally) can't order same-second edits; `>=` +
>   arrival order let a reconcile-goroutine push racing the serial worker
>   regress the cloud payload INVISIBLY (hash ties). Now a per-row monotonic
>   **`revision`** counter is the version marker end to end: local sqlite
>   migration **0005** (column + COMBINED stamp trigger), types.Qso/manifest/
>   adapters/restore carry it, wire envelope + export field, cloud migration
>   **0003** + revision-first guard (`revision > OR (= AND modified_at >=)` —
>   ties get exact legacy semantics), reconcile hash line now
>   `uuid|unixmicro|revision`. **Build discovery recorded in the ADR:** the
>   proposed bump-inside-stamp-trigger shape was WRONG — the daemon edit path
>   stamps modified_at explicitly (stamp trigger never fires there), and a
>   separate revision trigger CHAINS (its inner UPDATE re-fires the stamp
>   trigger, clobbering the µs-UTC stamp with `datetime('now')` — caught by
>   the existing 0004 canonicalisation test). One combined trigger with a
>   CASE owns both stamps. sqlboiler models regenerated at pinned 4.19.7
>   (diff: qso.go +9/−2 only; toml blacklist gained the split tracking
>   tables). Restore PRESERVES revision (a restored row resumes its sequence;
>   out-of-band same-UUID re-imports that reset to 0 push as stale BY DESIGN
>   — restore is the only sanctioned same-UUID recovery path).
> - **Verification:** full-tree `go test -race ./...` ZERO failures incl.
>   live-PG cloud suites + sqlite↔PG e2e reconcile/restore. New tests pin THE
>   finding (same-second lower-revision push rejected; higher revision beats
>   an OLDER clock — NTP-step immunity), trigger characterization (+1 per
>   edit incl. same-second, manifest carries it, restore resumes 7→8), hash
>   revision-sensitivity, diff revision-drift, server round-trip.
> - **DEPLOYED + VERIFIED same day:** paired deploy done (daemon
>   `deploy:local:dev` + F44 smcloud RPM, both `654.gc5151ca8`). The one
>   `in_sync:false` reconcile (14:04) was the predicted formula skew — the
>   daemon's 2-min-after-boot reconcile fired before the F44 restart — and
>   the version-aware diff correctly pushed NOTHING during it; post-restart
>   reconcile (14:14): **`in_sync` 5,590/5,590, zero heals**, all rows tied
>   at revision 0. Backlog item marked DEPLOYED+VERIFIED → archive at next
>   sweep. **NEXT:** the standing S220 dogfood validations (map eyeball,
>   in-place session edit mid-CQ, abandon-fix layer 1 on air, type-4 →
>   ADR 0048 flip); smcloud rate limiter + streaming export stay gated with
>   the ADR 0040 Phase-2 assessment.

> **Session 220 (2026-07-18, later the same day) — SECURITY-REVIEW ARC: three
> external review cycles absorbed (smcloud + frontend/app), every finding
> verified-then-fixed or deliberately backlogged with a design note. All
> committed + pushed by the operator (HEAD `de9239b2`).**
> - **Post-deploy verification (morning):** `task deploy:local:dev` picked up
>   the Session-219 map batch + in-place session edit; daemon active on
>   `2.0.0-alpha.1-643-g862dd384`, smcloud rode through the restart —
>   `in_sync` 5,590/5,590.
> - **smcloud hardening (review 1, 7 findings, all real):** boot now REJECTS
>   the `CHANGE_ME_TOKEN` placeholder + tokens <32 chars; `-listen` defaults
>   to `127.0.0.1:8091` (LAN staging must set `0.0.0.0:8091` explicitly —
>   runbook updated); DSN is env-only (no `-dsn` flag — argv leaks via ps);
>   DB pool bounded (5/2/30 m — unauthenticated `/v1/health` pings Postgres
>   per hit); `/v1/export` extends its own write deadline to 15 min via
>   `http.NewResponseController` (must outlast the restore client's 10 min;
>   the server-wide 2 min would truncate a slow-link dump mid-JSON). New
>   `cmd/smcloud/main_test.go`. **Follow-up (review 2, 3 residuals):**
>   `EnsureTenant` resolves case-insensitively — a pre-normalisation
>   lowercase tenant row is RENAMED to canonical and reused (never a second
>   empty tenant orphaning the backup); two case-variant rows refuse loudly
>   (+2 integration tests); `store.Migrate` uses `WithConnection` + closes
>   the migrator, returning the boot connection to the pool. **The F44 box
>   is unaffected** (uppercase tenant, explicit bind, real 44-char token) —
>   an RPM rebuild (`task rpm:smcloud` + reinstall) is worthwhile, NOT urgent.
> - **frontend/app fixes (review 1: 9 findings + lint/README; review 2: 4
>   residuals):** logbook loaders carry per-loader request GENERATIONS
>   (stale responses discarded — rapid logbook/page-size/filter picks can no
>   longer corrupt rows/cursors); sessionEdit captures its write-back target
>   per save + a generation guard, and EditQsoModal makes Escape/backdrop/✕/
>   Cancel/Ctrl+Enter inert while saving (a late save can no longer land row
>   A's data on row B); Session table **Name/Country cells were SWAPPED** —
>   fixed (was live in the morning deploy); map: catch-up refetch on EVERY
>   stream open (heals the no-backlog gap incl. a failed first connect),
>   startup guarded by a SEPARATE teardown-only `lifecycle` counter (a
>   window-picker refresh mid-startup no longer suppresses the stream
>   install) and no longer leaks listener+EventSource on early teardown; new
>   fail-soft `lib/utils/storage.ts` on every boot-critical localStorage
>   site (a storage SecurityError can no longer kill mount). **Ambiguous
>   writes told honestly:** EVERY non-aborted transport failure on a write
>   is outcome-unknown (the daemon's own 30 s timeout can cut the connection
>   AFTER committing; the browser can't tell that from connect-refused) —
>   QSO submit says "may still have been logged — **check the Logbook**"
>   (the Session list only gains a row on a confirmed response, so it is
>   guaranteed absent in exactly this case); both email surfaces say "may
>   still have gone out — check the inbox / Emailed markers"; email timeout
>   45 s (`EMAIL_TIMEOUT_MS`, outlasts the daemon's 30 s SMTP ceiling). Six
>   test-only lint errors fixed properly; README port 5174→5176. Suite
>   633/633 (+3 regression tests freezing the races).
> - **Backlogged with design notes (P2):** FT8 session reconnect reconcile —
>   trap recorded (naive daemon replay of `ft8-logged` would inject stale
>   events into fresh sessions; uuid dedup can't catch that) · daemon
>   write-idempotency tokens for QSO submit + email (what actually RESOLVES
>   the ambiguity the SPA now merely reports honestly) · **smcloud rate
>   limiting — pre-Phase-2 gate, two-layer design DECIDED (operator):**
>   per-IP limiting at the reverse proxy (it sees real client IPs) + a
>   global in-process concurrency limit in the binary (the bounded pool
>   protects Postgres, NOT the process — excess requests pile up handler
>   goroutines waiting for connections); noted in the `cmd/smcloud` doc
>   header + backlog.
> - **NEXT:** `task deploy:local:dev` at the next convenient moment to pick
>   up the review-fix batches (incl. the visible Name/Country column fix);
>   then the standing dogfood validations (map zoom/tooltip eyeball,
>   in-place session edit mid-CQ-run, FT8 abandon-fix layer 1, type-4
>   nonstandard QSO → flip ADR 0048). F44 smcloud RPM rebuild optional.

> **Session 219 (2026-07-18, same day, operator dogfooding throughout) — MAP
> INTERACTIVITY ARC (5 pieces) + SMCLOUD FIELD-FIDELITY AUDIT (CLEAN) + small
> closures. Map batch COMMITTED + PUSHED by the operator; NOT yet dogfood-
> deployed (deploy deferred until the operating session ends — the batch
> ships together on the next `task deploy:local:dev`).**
> - **Map zoom/pan + stacked hover tooltip (dogfood-requested):** new pure
>   `lib/map/zoom.ts` (viewBox-space transform: `zoomAt` cursor-pinned scale 1–16×,
>   `panBy`/`clampTransform` bounds + exact-identity snap, `toContent`,
>   `endpointsNear` stacked-endpoint hit grouping) + WorldMap interaction layer:
>   manual non-passive wheel (Svelte 5 declares `onwheel` passive), drag-pan with
>   pointer capture, dblclick/Reset-view button, stroke-widths + radii ÷k, tooltip
>   lists every contact stacked at an endpoint (cap 8, "+N more — zoom in"),
>   right-edge flip; native mid-arc `<title>` labels kept. jsdom tests drive real
>   wheel/pointermove events via a pinned bounding rect.
> - **Window persistence (operator-directed after the WAI triage):** picker choice
>   → localStorage `sm-map-window`, restored through `storedDurationMin` (absent/
>   garbled/retired values → 6 h default).
> - **50m level-of-detail (operator asked "can zoom show more?"):** past 3× the
>   basemap swaps 110m → Natural Earth 50m (241 countries, real coastlines) via
>   `worldCountriesHi()` lazy dynamic import — bundled chunk (756 KB / 243 KB gz,
>   offline posture intact), loaded only on first zoom-in; fail-soft to 110m. The
>   50m set carries DUPLICATE feature ids (`036` ×2) — keyed-each keys are now
>   id+index. `chunkSizeWarningLimit` 800 with a why-comment (data chunk, already
>   code-split — anything NEW past it still warns).
> - **Background-tab staleness FIXED (dogfood repro confirmed first):** hidden map
>   tab never caught up (no visibilitychange handling, throttled debounce,
>   possibly-dead SSE) — `mapData` now runs an immediate catch-up `refresh()` on
>   the hidden→visible edge (listener detached on teardown; MapView test drives
>   visible/hidden/unmount edges). Second-monitor posture was never affected.
> - **Triage (same session):** "session timer 14:58 but map lists 1 h+ QSOs" =
>   **WAI per ADR 0049** (window ≠ session; revisit only via a conscious ADR
>   change) — persistence above is the operator's accepted remedy. App suite
>   621 tests green; map suites 61.
> - **SMCLOUD FIELD-FIDELITY AUDIT — CLEAN, and now a repeatable drill:**
>   new **`scripts/smcloud-audit.py`** (qso-audit.py conventions; self-fetches
>   `/v1/export` with the forwarder creds from config.json; flags --db/--config/
>   --export/--logbook; exit 0/1/2). First run: **5,545/5,545 UUID parity, 0 core
>   mismatches, 0 additional_data mismatches, 0 modified_at violations** — the
>   only initial "mismatch" was the audit's own freq comparison (column = integer
>   kHz per schema; payload = ADIF MHz string; conversion now built in).
>   Population gaps (rst ~99%, cqz 95.8%, my_rig 45%) faithfully mirror local.
>   Runbook Phase-1 drill list references the script. Live sync observed during
>   dogfood: 13 new QSOs flowed as logged; 3 pending drained in one 10 s tick →
>   `in_sync:true`.
> - **Small closures:** backlog P1 stale-test item was ALREADY FIXED (found at
>   triage — commit `74cf906d` bumped the schema expectation to v4; verified
>   green; struck). **Re-enrich manual FAQ LANDED** (`manual/.../troubleshooting.md`:
>   cause, per-QSO + bulk Re-enrich remedy, QRZ-absent-callsign limit; hugo
>   builds) — the re-enrich arc is COMPLETE, archive its backlog line on the
>   next roll.
> - **SMCLOUD FAULT DRILLS 1–5 RUN AND PASSED (later same session, operator at
>   the box + assistant monitoring the shack side, ALL WHILE LIVE ON-AIR running
>   an FT8 CQ pile-up — count grew 5,561→5,571 during the abuse, zero QSOs
>   lost):** (1) total cloud DB loss (DROP DATABASE) → schema self-applied at
>   boot, full 5,563-row rebuild; (2) smcloud restart mid-backfill → INVISIBLE
>   (fell between 10 s ticks; the connection-refused→"host unreachable — will
>   retry (no give-up)" path was proven by drill 1's stop window); (3) Postgres
>   killed mid-push → HTTP 500 "logbook provisioning failed" → transient
>   classification, ~65 s backoff, resumed on recovery; (4) LAN cable pulled +
>   QSOs edited while dark → updates held (`no route to host`, distinct from
>   drill 1's refused), drained seconds after replug; the on-demand reconcile
>   500s GRACEFULLY while the box is dark; (5) a row DELETEd via psql under
>   smcloud's feet → reconcile caught the 1-row drift → healed to in_sync.
>   Post-abuse `scripts/smcloud-audit.py`: **CLEAN 5,571/5,571**. Mid-drill
>   bonus: the real QRZ path hit a genuine internet timeout and took the same
>   unreachable-retry path — the flaky-link design validated by actual
>   flakiness. Cloud psql facts learned: table is `qsos`, PK is `uuid` (no
>   int id — deliberate). **Drill 6 (restore rehearsal: `smctl stop` →
>   `smd restore -dry-run` → expect ~all-skip → `smctl start`) still pending —
>   needs the daemon stopped, so it waits for off-air.**
> - **IN-PLACE SESSION EDIT BUILT (dogfood catch, same session): editing a QSO
>   no longer takes the FT8 run off-air.** Trap diagnosed: SessionPanel had no
>   edit → operator navigated to the Logbook ROUTE → Operate unmounts → FT8 SSE
>   drops → 5 s linger → mic released (demand-driven capture, WAI) → no slots →
>   CQ silent. Fix (option a, root-cause): `EditQsoModal` DECOUPLED to injected
>   props (ADR 0045 — one modal, two owners), new `operate/sessionEdit.svelte.ts`
>   controller (hydrate via NEW `fetchQso` GET /v1/qso/{uuid} in api/qso-patch.ts
>   → modal in place → PATCH → canonical write-back onto the session row), the
>   Session-card CALLSIGN is the edit button (fixed column widths untouched —
>   operator constraint), uuid-less legacy rows stay plain text, Re-enrich rides
>   along free. Rejected: capture-across-routes (touches the capture design) +
>   nav-warning (documents the trap instead of removing it). 630 tests green.
> - **DRILL 6 RUN AND PASSED (off-air, end of session) — THE FULL CAMPAIGN IS
>   COMPLETE: SM CLOUD PHASE 1 IS PROVEN.** `smctl stop` → `smd restore
>   -dry-run` (5,590 fetched, 0 tombstones, no writes) → REAL `smd restore`
>   (safe by design: stored 0 / skipped-existing 5,590 / failed 0, 225 ms —
>   the actual write path exercised) → `smctl start` → reconcile `in_sync`
>   (one tick to drain the pre-shutdown stragglers) → final audit **CLEAN
>   5,590/5,590**. Runbook's drill list now carries the campaign stamp.
> - **NEXT:** (1) operator: `task deploy:local:dev` → dogfood-eyeball the map
>   batch (zoom Europe for 50m detail; background-tab repro as validation) +
>   the in-place session edit (edit a session QSO mid-CQ: TX cadence must
>   never break); (2) on-air, opportunistic: FT8 abandon-fix validation + a
>   type-4 nonstandard QSO → flip ADR 0048; (3) **the active-cycle P2 queue
>   is the default focus again** — smcloud P1 has NO remaining items (phase 2
>   VPS waits on the ADR 0040 security assessment).

> **Session 218 (2026-07-17→18) — SMCLOUD RPM BUILT + PHASE 1 LAN STAGING
> DEPLOYED AND VERIFIED: the full 5,532-QSO logbook is backed up on the F44 box,
> `POST /v1/smcloud/reconcile` → `in_sync:true` (5532 == 5532, hash match). The
> operator-declared critical workstream (a real backup) IS NOW LIVE.**
> - **RPM pipeline (committed+pushed `30328456`):** `nfpm-smcloud.yaml` (separate
>   package `smcloud`: static binary → /usr/bin/smcloud, system unit →
>   /usr/lib/systemd/system/, `/etc/smcloud/smcloud.env` as config|noreplace 0600
>   skeleton, runbook+Caddyfile docs, recommends postgresql-server, NO scriptlets)
>   + `scripts/smcloud-rpm.sh` (version.sh git-derived version; pure-Go static →
>   builds on the dev box, NO AlmaLinux container needed; `SMCLOUD_ARCH=arm64` →
>   aarch64) + `task rpm:smcloud` → `build/release/smcloud.x86_64.rpm` (~3.1 MB);
>   unit ExecStart unified on /usr/bin/smcloud. Deployed artifact version
>   `2.0.0-alpha.1-629-g30328456`.
> - **Runbook matured under live fire (committed+pushed):** Phase 1 rewritten as a
>   SELF-CONTAINED numbered walkthrough (1.1 build/scp → 1.2 Postgres → 1.3 token →
>   1.4 install/env/firewall → 1.5 wire daemon → 1.6 verify; every step labelled
>   with which machine; Caddy explicitly VPS-only; token = invented shared secret,
>   exactly two places). Added from real deploy friction: the **Fedora pg_hba
>   gotcha** (default `ident` on TCP → `Ident authentication failed`; fix =
>   `scram-sha-256` on the two host lines in /var/lib/pgsql/data/pg_hba.conf +
>   reload + direct-DSN `psql … -c "select 1"` test), the **hand-edit JSON
>   forwarder entry** for step 1.5 (smcloud is deliberately NOT auto-seeded —
>   no canonical URL), and the **backfill drain-speed note** (defaults 120 s/5 =
>   flaky-link tuning ≈ days for a full backfill; LAN: `tick_interval_sec:10,
>   batch_size:200` ≈ 1,200 rows/min, drained 5.5k in <5 min; don't hammer the
>   reconcile endpoint mid-drain — each call re-enqueues the remainder).
> - **Deploy war stories (all resolved):** staging-box Postgres was pre-installed
>   → only role+db needed; pg_hba ident (above); shack-side `config.json` hand-edit
>   had a trailing comma → smd restart-looped on `migrating config: parsing config
>   document` until fixed (assistant repaired line 100 + validated + restarted).
> - **The local DB is populated again** (5,532 QSOs — the QRZ re-import is done);
>   with the backup live, a repeat of the DB-loss event is now a restore, not a
>   catastrophe.
> - **Inbox triage (UNCOMMITTED):** the two 2026-07-17 map notes (zoom; hover
>   tooltip) → ONE P2 backlog item "Contacts map — zoom/pan + station hover
>   tooltip" (paired: the tooltip's hit-test runs in zoom-transformed screen
>   space; shared engine benefits the future Dashboard map). Inbox now has
>   nothing untriaged (the occupancy-stall line stays open-by-design, watching
>   for recurrence).
> - **NEXT:** (1) **Phase 1 fault drills** (runbook list: cable-pull mid-push,
>   Postgres kill mid-push, smcloud restart mid-backfill, offline-edit heal) +
>   one **`smd restore -dry-run` rehearsal** against the staging box — prove the
>   restore path before it's needed; (2) optionally revert the forwarder
>   tick/batch to defaults (or keep for LAN); a `task rpm:smcloud` rebuild would
>   also refresh the RPM's embedded runbook copy; (3) commit the triage edits
>   (backlog + inbox); (4) carried: dogfood-validate map features + FT8 abandon
>   fix on air; on-air type-4 → ADR 0048 flip; whole-log Dashboard map.

> **Session 217 (2026-07-17) — MAP POLISH ARC (5 features) + INBOX TRIAGE + FT8
> ABANDON LAYER-1 FIX; ALL COMMITTED + PUSHED by the operator (`79378ab3`,
> `b48f84c8`, `7ff37b2f`; `main` == `origin/main` @ `7ff37b2f`). Context shift:
> the DOGFOOD DB IS LOST — smcloud P1 is now the operator-declared critical
> workstream (see NEXT).**
> - **Map light/dark fix + cross-tab theme sync:** the map had no land/ocean contrast in
>   light mode (canvas == surface-muted == gray-100) — new dedicated theme tokens
>   `--color-map-water/land/border` (both themes) in `styles/app.css`; and a theme toggle
>   never reached the already-open map tab — a `storage`-event listener in
>   `lib/ui/state.svelte.ts` mirrors the persisted theme so App.svelte's `$effect`
>   restamps every tab.
> - **Window selector:** the duration picker is a `<select>` with 15/30 min, 1/2/3/6/12/24/48 h
>   (default 6 h); 10-day option dropped.
> - **Grey line (operator-toggled):** `engine.ts` `subsolarPoint` (NOAA low-precision
>   ephemeris, tests pin equinox/solstice/J2000) + `nightCap` (d3 `geoCircle` around the
>   antisolar point); three stacked twilight rings (90/84/78°) = the grey-line band;
>   "Grey line" checkbox persisted (`sm-map-greyline`), 60 s recompute clock.
> - **Per-band arc colours, 3 slices:** (1) `lib/map/bandColors.ts` spectrum-ordered
>   default palette + in-window legend (chips with counts); (2) daemon `map.band_colors`
>   config block — `types.MapConfig`, `validateMap` (lowercase band token + #rrggbb),
>   served RAW / presence-aware on `/v1/config` (psk_reporter pattern), consumed via
>   `fetchStationContext().mapBandColors`, `docs/v2-design/config.md` updated; (3) config
>   SPA **General tab → Contacts map** editor (14 colour pickers, default/reset, sparse
>   overrides — picking the default drops the override; `MAP_DEFAULTS` is a pinned copy
>   of the app palette, keep in sync). Colours apply at map load (reload, not restart).
> - **Sidebar Map entry (`7ff37b2f`):** globe-europe-africa icon in the frontend/app
>   sidebar bottom-utilities, above Manual, opens `/map` in a new tab (the map's
>   standalone/second-monitor posture, ADR 0049 rejection).
> - **FT8 Call-CQ abandon fix, layer 1 (`b48f84c8`):** dogfood catch (max-repeats abandon
>   returned to CQ, losing live answerers) — `pickAnswererLocked` extracted in
>   `caller_sequencer.go`; the drop now re-scans the abandon slot's decodes and replies to
>   another live answerer in the SAME slot, CQ only when nobody else calls. Test
>   `TestCallerSequencer_AbandonWorksLiveAnswererSameSlot`; ft8-suite `-race` green;
>   `docs/ft8.md` updated. **Layer 2 (recency-bounded answerer pool) open in the backlog.**
>   **Needs on-air validation** with the rest of the caller side.
> - **Inbox FULLY TRIAGED (nothing open):** the abandon note → backlog (layer 1 then built
>   same day); the 2026-07-03 Session-panel name-ellipsis note closed (verified fixed in
>   both SPAs). PSK Reporter question answered: support is built + opt-in, dogfood config
>   has it OFF (`"psk_reporter": {}`); enable = `"enabled": true` + restart.
> - **Process memory added** (`offer-commit-points-between-features`): flag a commit
>   boundary when a change lands green and another is directed — session accumulated 5
>   features into one tree → one big commit (`79378ab3`); later work was committed
>   per-feature.
> - **SMCLOUD S2 BUILT (same session, later) — the cloud HTTP API is up, gate passing;
>   UNCOMMITTED for operator review.** New: `internal/cloud/server` (PUT /v1/qsos
>   batch-upsert-by-name'd-logbook + tombstones · GET /v1/logbooks ·
>   /v1/logbooks/{id}/{reconcile,manifest} · /v1/export · /v1/health · /v1/version;
>   bearer-token→tenant auth, constant-time; payload stored/exported VERBATIM as
>   json.RawMessage; stdlib+store+types only, slog to stderr), shared
>   **`internal/cloud/reconcile`** (`Summary` — the canonical µs/lowercase/UnixMicro
>   hash BOTH ends compute; S4 imports it), store additions (`Logbooks`/`Logbook`/
>   `Export` + `Migrate` — embedded golang-migrate runtime applier, same tracking
>   table as the CLI), and **`cmd/smcloud`** (env/flag config, SMCLOUD_TOKEN env-only,
>   graceful shutdown, TLS = S6 proxy). **The S2 round-trip GATE passes** (types.Qso →
>   PUT → export → deep-equal, seconds + app fields intact) + auth/tombstone/stale-push/
>   ownership tests — 15 integration tests live against Postgres (`task db:pg:up`),
>   -race clean; live smoke of every endpoint done. Store+server test suites now
>   serialise via pg_advisory_lock (parallel packages, one dev DB).
>   `docs/v2-design/sm-cloud-p1.md` status updated (S1+S2 built).
> - **SMCLOUD S3 BUILT (same session, later still) — the `smcloud` forwarder;
>   UNCOMMITTED for operator review.** `internal/forwarding/smcloud/` registers type
>   `smcloud` ("SM Cloud backup", insert/update/delete; creds url/token/logbook —
>   NO default endpoints so it is NOT auto-seeded, the operator adds it via the config
>   SPA's data-driven form; NO adif prefix → the worker's plain-mark path never touches
>   the QSO row, protecting `modified_at` for reconcile). **`types.Qso` gained
>   `ModifiedAt`/`DeletedAt` `json:"-"`** (LastRefreshedAt column-only pattern, overlaid
>   in `adapters.QsoModelToType`) so the envelope can carry the row's drift signal;
>   Submit treats zero-modified_at/no-UUID as Terminal (never a silent now()), stale
>   `applied:0` as Success, and mirrors qrz's outcome matrix (no response →
>   Unreachable/forever-retry per ADR 0038). Blank-imported in cmd/smd + UserAgent
>   wired. Tests: 11 in-package (wire shape incl. modified_at-stays-out-of-payload,
>   tombstone, outcome matrix, guards, registry posture) + `TestSubmit_AgainstRealCloudServer`
>   (end-to-end vs the REAL internal/cloud/server + Postgres: deep-equal payload
>   round-trip, stale no-clobber, tombstone). Affected suites green (types, database,
>   forwarding, api, qsoservice, adif, cmd/smd). sm-cloud-p1.md S3 section updated
>   (two deliberate deltas from the sketch documented there).
> - **SMCLOUD S4 BUILT (same session, later again) — reconcile detect+heal;
>   UNCOMMITTED for operator review.** `smcloud.Reconciler` (same ForwarderConfig as
>   the forwarder; hourly loop + 2-min startup delay under the worker ctx in cmd/smd;
>   on-demand **POST /v1/smcloud/reconcile**, 503 until an enabled smcloud forwarder
>   exists — api-endpoints.md updated). Local hash: new `sqlite.FetchQsoManifestWithContext`
>   → shared `internal/cloud/reconcile.Summary`. Heal: upserts via EnqueueUploads
>   (force), missed tombstones via NEW `qsoservice.EnqueueDeleteUploads` (+
>   `findEnabledForwarderFor` refactor). Local authoritative — cloud-only/cloud-newer
>   counted+logged, never touched; 5000/run cap. **Two protocol bugs found by tests,
>   fixed in BOTH readers (adapter + manifest query): NULL-until-first-edit
>   modified_at → created_at fallback; created_at sub-second vs trigger whole-second →
>   truncate to SECONDS** (else a same-second edit/delete reads as a stale push
>   forever). Tests: 11 diff-table cases, `TestReconciler_EndToEnd` (real sqlite +
>   qsoservice vs real cloud server + Postgres: first backfill → drain → in-sync →
>   missed delete → tombstone heal → in-sync), EnqueueDeleteUploads/manifest
>   integration, api handler 503/200/500. All affected suites green; gofmt/vet clean.
> - **SMCLOUD S5 BUILT (same session, final act) — restore; P1 IS CODE-COMPLETE
>   (S1–S5); UNCOMMITTED for operator review.** JSON-native restore path (SubmitImport
>   verified ADIF-shaped → unsuitable): **`qsoservice.Restore`** (UUID+modified_at
>   required; existing rows skip = idempotent re-runs; no validation gauntlet / no
>   upload rows / no enrichment; dedupe reuse-or-recompute; time_off→time_on default)
>   over **`sqlite.InsertRestoredQsoWithContext`** — the ONE writer setting
>   modified_at/deleted_at explicitly (QsoTypeToModel deliberately left unmapped: the
>   UPDATE path round-trips fetched QSOs and would defeat the bump trigger on
>   never-edited rows — caught during build). **`smd restore`** subcommand (daemon
>   stopped; creds from the config's smcloud forwarder entry, enabled or not;
>   -forwarder/-cloud-logbook/-logbook/-dry-run) + `smcloud.FetchExport`. Tombstones
>   restore soft-deleted with original recency. **THE S5 GATE PASSES**:
>   `TestRestore_FullCycle` — two real local stacks around the real cloud
>   server/Postgres; machine 2 deep-equals machine 1's QSO AND **reconciles IN SYNC**
>   (modified_at survived the whole cycle); idempotent re-run all-skips. e2e local
>   stacks moved :memory:→temp-file DBs (shared-cache :memory: is process-wide — two
>   "machines" were one DB). All affected suites + -race green; gofmt/vet clean.
> - **SMCLOUD S6 ARTIFACTS BUILT (same session) — deployment is now a runbook walk;
>   UNCOMMITTED for operator review.** `task build:smcloud` (fully STATIC pure-Go
>   linux binary, version-stamped, 7.2 MB — verified);
>   **`deploy/smcloud/`**: hardened systemd unit (DynamicUser + full sandbox,
>   systemd-analyze-verified), `smcloud.env.example` (loopback listen; token via
>   `openssl rand -base64 32`), `Caddyfile.example` (auto-TLS). Runbook
>   **`docs/smcloud-deploy.md`** (added to the Tier-1 map): decisions (VPS/region —
>   well-connected not Malawi; hostname e.g. cloud.station-manager.org; distro
>   Postgres recommended for P1) → build/scp → Postgres → unit → Caddy → daemon
>   forwarder wiring → first-backfill verify (`POST /v1/smcloud/reconcile` →
>   in_sync) → ops (pg_dump backup-of-the-backup cron, binary-swap upgrades,
>   restore drill, token rotation). Migrations self-apply at service boot.
> - **DEPLOYMENT PLAN DECIDED (operator, late session): PHASE 1 = LAN STAGING on a
>   separate local-network machine running FEDORA 44** — cheap immediate resilience
>   (shack-machine disk/OS loss) + the test/soak/fault-drill/harden ground before any
>   internet-facing VPS (phase 2 adds the off-site half). Runbook gained a full
>   "Phase 1 — LAN staging deploy" section (bind 0.0.0.0:8091, NO TLS/proxy on LAN,
>   never port-forward, static IP, fault-drill list, move-to-VPS = repoint forwarder,
>   reconciler rebuilds); `build:smcloud` gained `SMCLOUD_ARCH` (arm64 verified);
>   env example documents both listen postures. **These three edits (Taskfile.yml,
>   deploy/smcloud/smcloud.env.example, docs/smcloud-deploy.md) are UNCOMMITTED,
>   awaiting operator review.**
> - **NEXT (session plan, set by operator): (1) build the smcloud RPM + DEPLOY to the
>   F44 LAN box.** The RPM work was scoped but NOT started (only discussed):
>   an `nfpm-smcloud.yaml` (package `smcloud`: /usr/bin/smcloud + the system unit at
>   /usr/lib/systemd/system/ + /etc/smcloud/smcloud.env as a noreplace 0600 config +
>   doc files; recommends postgresql-server; unify the unit's ExecStart on /usr/bin/
>   smcloud — it currently says /usr/local/bin), a build script mirroring
>   scripts/dev-rpm.sh (version.sh + static go build + nfpm), a `rpm:smcloud` task,
>   and runbook RPM-path updates. Then walk runbook Phase 1 on the F44 box: Postgres,
>   env file (token via openssl rand), enable unit, wire the daemon forwarder at
>   http://<box>:8091, watch the reconciler's first backfill, fault-drill.
>   Local recovery still pending too: QRZ ADIF import (`smd import <file.adi>`,
>   daemon stopped, NO `--forward`). (2) dogfood-validate the map features + the
>   abandon fix on air; (3) carried: on-air type-4 → ADR 0048 flip; whole-log
>   Dashboard map (needs the `GET /v1/logbook/{id}/map` aggregate).

> **Session 216 (2026-07-16→17) — FIRST-RUN SETUP GATE in frontend/app, committed by the
> operator as `80e1faa3` (feat(setup)).** Fallout of the lost dogfood DB (the operator hit
> the fresh-install path): the whole app shell now gates on `setup_complete` — new
> `lib/setup.svelte.ts` (status loading→blank / needed→SetupCard / complete→shell;
> daemon-unreachable resolves to complete so an outage never greets a configured operator
> with first-run setup), `lib/ui/SetupCard.svelte` (callsign form + post-save "Setup
> complete" interstitial offering Settings), `lib/api/setup.ts` (`completeSetup` → PUT
> /v1/config with just the callsign; the daemon seeds the default logbook), `StationContext`
> gained `configOk`/`setupComplete`, main.ts injects the save action (ADR 0045 — the state
> module never imports lib/api). Gate sits ABOVE the router so the shell-less /map tab is
> covered too. Tests throughout (589 at the time).

> **Session 215 (2026-07-16, afternoon) — QSO CONTACTS MAP BUILT (both phases of the
> session-214 respec), COMMITTED + PUSHED; the type-4 + ADR 0049 doc commits are pushed
> too. NB: this entry was written by a later reconciling session from git — the map
> session itself did not update this handoff (the exact gap the RECONCILE guard warns
> about, and the date-keyed check can't catch same-day drift).**
> - **Phase 1 — render engine (`7817393b`):** `frontend/app/src/lib/map/engine.ts`
>   (d3-geo projection, great-circle arc sampling, antimeridian clipping, basemap paths
>   via topojson) + `WorldMap.svelte` (reusable, presentation-only — origin + arcs
>   props). New deps `d3-geo`/`topojson-client`/`world-atlas` (+ type packages), per the
>   backlog's render decisions (MIT/public-domain → GPL-clean, bundled → offline).
>   Engine unit tests (spherical math, antimeridian) + component render tests.
> - **Phase 2 — map route (`93e7f83b`):** `MapView.svelte` full-tab view at **`/map`**
>   (duration picker, live-status indicator, mapped-of-total summary) +
>   `mapData.svelte.ts` (windowed fetch over `GET /v1/logbook/{id}/qso` cursor pages +
>   live head-refresh on `qso.*` events via the new `api/log-events.ts` transport —
>   reconnect, idempotent updates, minimal payload parse; fail-soft for unmappable
>   rows) + **"Open map ↗"** in the shared `SessionPanel` + router wiring. State +
>   component tests (`collectWindow`, `qsoEpochMs`/`rowPoint`, MapView render).
> - **PUSH STATE:** `main` == `origin/main` @ `93e7f83b` — everything is pushed,
>   **including the four type-4 commits session 214 recorded as push-gated on on-air
>   validation** (that gate was overtaken by the push). **ADR 0048 is still `Proposed`**
>   — working a real nonstandard station on air remains open and still gates the ADR
>   flip.
> - **Verified at reconcile (2026-07-16, later session):** working tree clean;
>   `frontend/app` suite green at HEAD — **576 tests / 47 files pass** (545 → 576, the
>   map's ~31 new tests). Unverified: CI status (no `gh` on this machine) and dogfood
>   deploy (daemon not reachable on :8080 at reconcile time) — assume the map is NOT
>   yet dogfood-deployed / eyeballed live unless the operator says otherwise.
> - **NEXT:** on-air validate the type-4 ladder → flip ADR 0048 Proposed→Accepted;
>   dogfood-deploy + eyeball the map (`task deploy:local:dev`, operator-gated —
>   remember `/app/` is `//go:embed`'d: redeploy, don't reload); the **whole-log
>   Dashboard map** stays the designed follow-on (reuses the shipped engine; needs the
>   `GET /v1/logbook/{id}/map` aggregate — detail in the backlog).

## Active cycle (the 1–3 things in flight now)

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
