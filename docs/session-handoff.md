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

## Current state (as of 2026-06-19)

> **Recent arc (session 186, 2026-06-19):** **full-codebase code-review sweep** — an
> external review landed one package at a time (`adif`, `api`, `audio`, `bridge`, `cat`,
> `config`, `database`, `email`, `errors`) and every finding was fixed or deliberately
> triaged, with a resolution section appended to each `docs/reviews/archive/internal-<pkg>-2026-06-19.md`
> and committed before the next. Notable: a parser-panic fix (`adif`), CI-V ACK address
> hardening + data-driven command range metadata (`cat`), `0600` secret-file perms + rig-model
> validation (`config`), STARTTLS test coverage + mailer-boundary validation (`email`), and a
> structured-logging chain-walk fix (`errors`); `UpsertLogbook` removed as dead. Pre-review
> small fixes the same day: ANT_AZ rounded to whole degrees, session-email progress-toast
> wording. **Earlier arc (session 185, 2026-06-18):** **PSK Reporter reception-report upload SHIPPED**
> (`internal/pskreporter` — opt-in, IPFIX/UDP, live-validated against the real collector AND
> running in production on the dogfood daemon); **FT8 "Next"-drain bug** + **ladder-highlight
> bug** fixed (both operator-reported, root-caused from dogfood logs); **port-8080
> restart-flap** addressed (`server.StopAccepting` releases the listener first + a Taskfile
> guard so `task run:smd` can't collide with the systemd daemon); **go-ft8 v0.3.5** brings
> standard `/P`. **Earlier arc (sessions 178–184, all 2026-06-16/17, uncommitted unless noted):** bridge
> TX-safety review fixes; FT8 "Working [call]" channel banner + auto-abandon countdown
> (+ `ft8.tx.max_repeats` knob); **FT8 work-a-caller** (`StartWorkCaller` / `qso/work`) +
> the dedicated `worker` ladder; **wrong-band FT8 logging fixed** at two layers —
> daemon now logs the bridge's live dial at completion (`CurrentDialMHz`), and the
> **IC-7300 POLL now reads the operating freq (`25 00`)** (the real root cause; needs a
> redeploy + bench re-confirm); FT8 Settings **Float-CQ-to-top** toggle; **FT8 pile-up
> callsign stacking** (Ctrl+click → SPA FIFO → drains via work-a-caller; SPA-only); and
> the **session-email subject = logbook-callsign-prefixed + `Contains N QSOs.` body**.
> Per-session detail in the `### Session N` entries below.

**main is v2.** Daemon (`cmd/smd`) + embedded Svelte 5 SPA (`frontend/logging/`, served at `GET /` when `Protocol=tcp && ServeSPA=true`). Day-to-day ham ops run from the frozen `v1` branch; v2 is under active development. Full suite green; CI gates every push to main.

In-tree and shipped:

- **Daemon core** — milestones 1/1b/1c (ingest → validate → store → forward → emit status → serve queries). CGO-free SQLite (modernc), UUIDv7 QSO identity (ADR 0016), `qso_history` append-only audit, dedupe key, soft-delete, one-fails-all-fail QSO writes.
- **Enrichment** (ADR 0017) — hamnut + QRZ providers, domain-tables-as-cache, three-state read policy, bounded async refresh worker. Never blocks logging.
- **Forwarder** (ADR 0022) — multi-destination `Forwarder` interface + worker + registry; enqueue gated on config presence, not `Enabled`.
- **Bridge** (`internal/bridge`, ADR 0013 + 0019) — M3a closed 2026-05-11. Read-only rig state over `/v1/rig/events` SSE; AUTO-mode CAT → filter → SPA; pipeline supervisor (ADR 0020) self-heals first-boot ordering + mid-session disruption; rig-mode → ADIF mappings; i18n error codes (ADR 0010). **Inbound command path** (ADR 0026): `POST /v1/rig/command` drives freq/mode/VFO/band (data-driven `cat` commands + `BridgeInfo.ops`); SPA rig-control on Shift+Ctrl shortcuts. **Tune-carrier path** (ADR 0027): `POST /v1/rig/tune` + a daemon-owned TX state machine keys a reduced-power RTTY carrier for external-amp tuning — the first TX feature; the daemon owns the guaranteed stop; click-only Tune button. **Rig profiles** (ADR 0028, Phase 1 shipped 2026-06-05): `config.Rigs []types.RigConfig` (+`audio`) catalogue with `default_rig_id` as the active selector; legacy loose `bridge.serial`/`bridge.cat` migrate into a single id-1 rig at load; per-rig audio is **per-direction name-based** `audio.{rx,tx}` (Session 177); `ActiveBridge()`/`ActiveFt8()` project the active rig; bridge/ft8 internals unchanged. Switch = edit `default_rig_id` + restart. Discovery endpoint + picker UI + runtime hot-swap deferred to the config-SPA work.
- **SPA logging client** — QsoPanel + CountryPanel + InfoPanel with four tabs (Worked / Details / My Station / Session), all shipped. **FT8 view (`Ft8Panel`)** carries the live Band Activity decode feed + occupancy/Clear-Slots readout; CQ decode lines are enriched SPA-side with a country flag + worked-before tint (Session 158). Keyboard-first flow, enrichment + contact-history wiring, QSO edit overlay, per-session QSO list. My Station has seven sub-tabs (identity / location / equipment / CW / qso / Mode Mappings / **About** — the last a read-only `/v1/version` diagnostics panel). The Comment field carries a **paste-list** (localStorage MRU of recently-logged comments, clipboard-list dropdown). **Session email-out**: posts `{to, uuids[]}`; the daemon rebuilds the ADIF from the live DB rows (proper `<EOH>` header), durably stamps `sm_fwrd_by_email_*` (SessionPanel "Emailed" column), and archives a copy under `<workingDir>/exports/sent-adif/`. See memory `project_sm_session_email_sent_status`.
- **Config SPA (`frontend/config/`)** — second embedded SPA, scaffolded 2026-06-14, served at `/config/` (sub-path on the same origin; Vite `base:'/config/'` + `StripPrefix` route; dev on :5174). Separate sibling project, NOT a route in the logging SPA. Placeholder shell only — its purpose is a **parking place for set-once config** that's UI noise in the logging client. Its **first real surface is a rig-profiles editor** (design settled in `frontend-spa.md` → "Config SPA — rig-profiles editor"). **Slice 1 shipped 2026-06-14:** `GET /v1/hardware` (`internal/hardware` — serial enum pure-Go all builds, audio enum CGO-seam, graceful degrade) for the profile pickers. CI gates it (config install/lint/check/build). See `docs/v2-design/api-endpoints.md` (new canonical endpoint reference) + `frontend-spa.md`.
- **CD pipeline** — `.github/workflows/ci.yml` gates every push to main (SPA lint/check/test/build + gofmt/vet/`go test -race`/embed-build/all `cmd/...`). Local mirror `task ci:local`; dogfood refresh `task deploy:local:dev`.
- **Operator daemon control** — the RPM ships `/usr/bin/smctl` (`start|stop|restart|status`) alongside `/usr/bin/smd`; it wraps `systemctl --user … smd` and prints a state-verified `SM Started.` / `SM Stopped.` line (bare `systemctl` is silent on success). See `docs/install.md §3`.
- **FT8 decode subsystem** (`internal/ft8`, ADR 0024) — opt-in (`ft8.enabled`), fail-soft, decode-is-not-a-QSO (logs "heard this" lines only; narrow-daemon-scope holds by import graph). Offline `DecodeFile` + CGO-free pipeline core (ring + UTC slot scheduler + Service) shipped 2026-05-31; **live miniaudio/malgo capture shipped 2026-06-02** behind `//go:build cgo` (+ a `!cgo` idle stub). Live FT8 needs a **CGO build** (`SM_FFT=pocketfft`); the static default decodes WAVs but can't capture. FTdx10 smoke: 4/4 slots, 0 drops, 12–16 decodes/slot. **Capture is demand-driven (Session 157):** the device is acquired on the first `/v1/ft8/events` subscriber and released a short linger after the last leaves — an idle daemon holds no mic until the FT8 view is open. **FT8 TRANSMIT (ADR 0029/0030/0031/0033) — both flows shipped.** Steps (a)–(d) + e1–e4 done: **answer-a-CQ completes + logs** (click a CQ → daemon auto-advances → 73 → logged QSO), and **Call CQ runs a sequenced caller session** (ADR 0033 `auto_first`: calls CQ, auto-works the first answerer to RR73, logs, loops the pile-up until Abandon). Daemon-owned guaranteed stop throughout; attended-only. **Completed FT8 QSOs surface to a shared session log** via the one-shot **`ft8-logged` SSE event** → SPA `sessionQsosState` → a new **Session tab** in `Ft8Panel` (same `SessionPanel` + email-out as Phone/CW; UUID carried so edit/email work). Band Activity shows a **per-CQ beam-heading column** (short-path bearing to aim the antenna). Pending: the `operator_pick` answerer-stack + its Settings toggle, and on-air validation of the caller side + the session-tab logging. See `docs/install.md §8` + `docs/ft8.md` + memory `project_sm_ft8_integration`.

Out of tree:

- **The FT8 decoder library** is out-of-tree **go-ft8** (`github.com/ColonelBlimp/go-ft8`), a WSJT-X/jt9-derivative (GPL-3.0-only) that SM links — the in-tree clean-room MIT decoder was abandoned and preserved at tag `ft8-snapshot-2026-05-30` (recoverable via `git checkout`). SM carries only the thin `internal/ft8` wrapper + live capture, not the decoder. `internal/audio` (CGO-free WAV/FFT) deliberately retained. See the CLAUDE.md FT8 bullet + memory `project_ft8_library`.

**Licence: GPL-3.0-only as of 2026-05-31 (was MIT).** Linking go-ft8 (a GPL-3.0-only WSJT-X derivative) pulls SM under copyleft. See ADR 0023 + `docs/licensing.md` + memory `project_sm_license_gplv3`.

Authoritative current-state detail lives in `CLAUDE.md` + the memory files; the long-form session-by-session record is the `### Session N` entries below + git history. **Next steps** are at the bottom of this file.

### Session 190 (2026-06-24) — **Reviewed + hardened Codex's RST-length fix; validated fresh-install + 4509-QSO import end-to-end.** While the operator was offline (2026-06-23) Codex shipped the crucial **RST-length relaxation** (migration `0002_relax_rst_length`, commits `8ae65fc1` + `39fb7e10`): the `qso.rst_sent`/`rst_rcvd` `CHECK (length ≤ 3)` from `0001_init` was rejecting legitimate imported QSOs with wider RST values (e.g. `SP5VYF rst_rcvd=4657` in the operator's real 7Q5MLV export), surfaced by `runImport` as errored records. **Review verdict: correct, FK-safe, well-tested.** The SQLite table-rebuild (rename→recreate→copy→drop, mandatory since you can't `ALTER` a CHECK) reproduces every column/index/trigger/constraint faithfully — only the two RST CHECKs drop — and rebuilds the child tables (`qso_upload`, `qso_history`) *before* dropping `qso_old`, so FK enforcement (ON via DSN + runtime PRAGMA) never breaks; both migration tests assert upload + history rows survive. Two minor fixes applied this session: **(1)** restored the em-dash `—` in `0002`'s `qso_history` append-only RAISE messages (Codex had drifted to ASCII `-`, diverging from `0001`); **(2)** added `CHECK (length(rst_*) ≤ 10)` to the up-migration's relaxed columns (unbounded → generous cap that still admits real wide values but catches garbage). Down migration keeps its `≤ 3` restore. **Also struck a stale backlog entry:** the WSJT-X-style `ALL.TXT` decode log was filed as "not done" on 2026-06-22 but had since shipped (`ft8.decode_log` / `internal/ft8/decodelog.go`, commits `46f207ba` + `037e9aef`) — a fail-soft queued writer logging RX decodes + our own TX rungs, off by default, default path `$SM_WORKING_DIR/log/ft8-all.txt`; marked SHIPPED 2026-06-23 with pointers. **Fresh-install dogfood validation (operator-run):** first `smctl import` of the 4509-record export hit `target logbook id=1 does not exist` — a **fresh-install ordering gotcha**: the default logbook is only seeded on the first config PUT carrying a station callsign (`seedDefaultLogbook`, the My Station setup transition). After saving the callsign in My Station, import ran clean: **4509 stored, 0 errors, 0 dupes**; DB spot-check confirmed schema at migration **v2 (dirty=0)**, the single wide-RST `SP5VYF` row stored intact, and the `≤ 10` cap exercised on a genuinely-fresh DB. Re-import was **idempotent** — 4509 duplicates, `stored=0`, and `MAX(id)` unchanged at 4509 (dedupe short-circuits *before* INSERT, burning no row ids); `distinct dedupe_keys = total = 4509`. Operator chose to leave the malformed `4657` RST as faithful-import (SM stores, doesn't mangle). Open (non-blocking): friendlier "complete first-run setup before importing" message in `smd import` (vs raw not-found), and `/log` the fresh-install ordering gotcha for the onboarding arc. All green: `go test ./internal/database/sqlite/ ./cmd/smd/` after both edits.

### Session 189 (2026-06-23) — **Fresh-install config fixes: dangling `default_rig_id` + bridge timeouts now visible (sparse-but-served).** Continued the clean-DB dogfood config review. **(1) Dangling `default_rig_id`:** `applyDefaults` stamped `default_rig_id = 1` unconditionally, so a rig-less fresh install pointed at a non-existent rig (→ "Rig:" blank in the Phone/CW header). Fixed: `applyDefaults` only sets it when a rig catalogue exists (else stays 0 = "no active rig"); `validate.go` now rejects any non-resolving id except 0-with-no-rigs; `LoggingCard.svelte` shows **"Rig: not set"** when unset. Tests: `TestValidate_DefaultRigID`, `TestLoad_DefaultsApplied`, `TestHandleGetConfig_PreSetup`. **(2) Persistence-shape decision (and a course-correction):** I initially recommended "go sparse," then found `config.md §15.2` already settled it (sparse-on-disk rejected, filled-on-disk kept, upgrade-drift handled by the §13 migration guard) — my rec reversed a decision without checking. Re-investigating revealed the codebase actually uses **three** models, and `ft8`/`psk_reporter` are deliberately **sparse-but-served-resolved** (file empty, `/v1/config` GET serves resolved defaults via `Resolve*`). `bridge.timeouts`/`tune` were the only genuinely *invisible* block (not in file, not in GET). Operator chose to bring bridge onto the sparse-but-served pattern: added `bridge.ResolveTimeouts`/`ResolveTune` (reuse the exact `Service.New` helpers, so served == runtime; tune ceilings stay non-overridable) → served on GET as `bridge_timeouts`/`bridge_tune`. `config.json` stays sparse; SPA reads effective values; no default-freeze, no constant duplication. `qsl: {}` correctly stays empty (operator data). **`forwarders: null` confirmed correct** (don't pre-list — ADR 0022 enqueues by presence). Tests: `TestResolveTimeouts`/`TestResolveTune`. Docs: new `config.md §15.5`, api-endpoints.md GET/PUT `/v1/config`, backlog item resolved. Lesson reinforced: **read the settled design (config.md is Tier-1) before recommending a reversal.** All green: full `go build`/`vet`/`go test ./...`; SPA check/lint/format on `LoggingCard`.

### Session 188 (2026-06-23) — **Importer redesign: default NO-UPLOAD + `smctl import` wrapper (clean-DB dogfood findings).** During a clean-slate dogfood deploy the operator hit "import tries to upload to QRZ." Root cause: `SubmitImport` shared the public enqueue path, so importing a historical log queued `pending` upload rows for every configured forwarder; the old `--uploaded` flag + QRZ-LOGID stamp only neutralised *some* of them (footgun: forget the flag → mass re-upload). Redesigned per operator direction: **import uploads nothing by default**; `SubmitImport` gained `forwardTo []string` and enqueues only forwarders the operator explicitly names via the new `smd import --forward qrz,…` (validated against config, case-insensitive). The QRZ per-QSO `app_qrzlog_logid` (present on all 4509 records of the operator's real export) is now **preserved as QSO provenance** in `additional_data` (`types.Qso.QrzlogLogid` + `adif` round-trip both directions) instead of only as an upload-row `upstream_id` — so it survives even though import queues no row. Removed the now-dead `--uploaded` flag + `stampQrzUpload`/`markUploadedForForwarders` (bulk-forward of an already-stored log is the future logbook-SPA's job). New **`smctl import`** wraps the single-writer dance (stop→import→restart-if-was-running). Mode-normalization verified against the real export (USB/LSB→SSB+SUBMODE; FT8/SSB/CW are main modes, pass through) + pinned by `TestNormalizeImportedMode`. Docs updated: `install.md` §5, manual `importing.md`/`appendix-cli.md`. Dogfood inbox triaged → `backlog.md` (1 bug + 5 enh). All green: full `go build ./...`, `go vet ./...`, `go test -short ./...`; dry-run of the 4509-record real export parses clean. `preserveUUID`→`isImport` rename throughout `submit()`.

### Session 187 (2026-06-22) — **ClubLog forwarder SHIPPED.** New `internal/forwarding/clublog/` destination following the QRZ template (registered `"clublog"` + default retry via `init()`; named import + `clublog.UserAgent` override in `cmd/smd`). All green: gofmt clean, `go vet`, `go test ./internal/forwarding/... ./cmd/smd/...`, `-race` on the package, static `CGO_ENABLED=0` build.
- **API** (from the ClubLog freshdesk docs): insert = `POST realtime.php` with the QSO as one ADIF record + `email`/`password`/`callsign`/`api`; delete = `POST delete.php` matched by `dxcall`+`datetime`(`YYYY-MM-DD HH:MM:SS`)+`bandid`(numeric, via a `band→bandid` map). Classification is **status-code-driven** (2xx → success incl. "QSO OK/Modified/Duplicate" and delete "Not Deleted"; 400 → terminal; 408/429/5xx → transient).
- **Key design points** (captured in the package doc comment): (1) **API key always operator-supplied** in `credentials.{email,password,callsign,api}` — ClubLog issues one *application* key and auto-deletes keys found in public repos, so nothing is embedded in source. (2) **No `update` path** — ClubLog real-time can't edit fields (a re-upload is just a duplicate), so `action=update` → terminal with a clear message; sensible default `action_filter: ["insert","delete"]`. (3) **No upstream record id** → `UpstreamID` always empty, `priorUpstreamID` unused (delete built from the QSO's own fields). (4) **403 circuit breaker** — ClubLog mandates "STOP sending immediately or the IP is firewalled"; the first 403 trips an internal `atomic.Bool` so every later `Submit` short-circuits to terminal *without* a network call until daemon restart (the worker processes a batch per tick, so this prevents a rapid burst of 403s). No new HTTP route / config-shape change; `forwarding.md` stays the frozen Tier-2 record of intent.
- **Code-review follow-ups (same session, all green):**
  - **High — ClubLog deletes were unreachable.** The worker marked any delete `failed` when the prior `upstream_id` was empty *before* `Submit` ran (`worker.go` `resolvePriorUpstreamID`), and ClubLog inserts return no id — so every ClubLog delete died there. Fix (Option A): the worker no longer gates on an empty id; it fetches + passes it through and the forwarder decides (QRZ self-rejects empty in `buildForm`; ClubLog deletes by QSO fields). Replaced the worker-side short-circuit test with two integration tests (`TestWorker_Delete_NoPriorInsert_IdLessForwarderReaches` + `_IdRequiringForwarderFails`).
  - **Medium — ClubLog upload stamps weren't typed.** Added `ClubLogUploadDate`/`ClubLogUploadStatus` to `types.Qso` + the `adif` Record (both map directions) + `spec_validation_test.go` classification, verified against ADIF 3.1.7 (`CLUBLOG_QSO_UPLOAD_STATUS`/`_DATE` are official fields). Previously the `$.clublog_qso_upload_*` keys the worker stamps were dropped on the next typed round-trip (`type_to_model.go` marshals `types.Qso`).
  - **Medium — empty `action_filter` queued doomed update rows.** Root fix: forwarders register their **supported actions** (`forwarding.RegisterSupportedActions`, mirroring `RegisterDefaultRetry`) — qrz=insert/update/delete, clublog=insert/delete, stub=all. `applyDefaults` defaults an omitted filter to the supported set (fallback all-three for unregistered types); `validateForwarders` rejects an explicit unsupported action at load. `config` now imports `forwarding` (acyclic). Registry + config tests added.

### Session 186 (2026-06-19) — **Full-codebase code-review sweep: 9 packages reviewed, every finding fixed or triaged.** All green per package (build/vet/`-race`/suites; SPA prettier/vitest where touched). Each package committed + pushed before the next; resolution sections appended to `docs/reviews/archive/internal-<pkg>-2026-06-19.md`.
- **Pre-review small fixes (same day):** session-email send-progress toast wording (`Sending email…`); **ANT_AZ rounded to whole degrees** — `enrichment.svelte.ts` `activeBearing` + FT8 daemon `qsolog.go` (`FormatFloat` prec 0) + the CountryPanel bearing display (it's a rotator heading; sub-degree is spurious). Confirmed Notes is a fresh per-QSO field (not enrichment- or prior-QSO-populated; visible in Worked panel only).
- **Review fixes by package (order received):**
  - **adif** — H1: `<TAG:n>` oversized-length parser panic (Atoi error ignored → overflow → bad slice bound) folded into the tolerant clamp. M1: received-QSL fields (`QSLMSG_RCVD`/`QSL_RCVD_VIA`/`QSL_RCVD_NOTES`) now round-trip through `QslSection`. M2: kept the deliberate value right-trim (padded-export defence), documented; L1 stale comments fixed.
  - **api** — M1: session-email `to` parsed with `net/mail.ParseAddress` BEFORE any side effect (rejects CRLF header-injection + multi-address; display-name normalized). M2: FT8 handlers validate `slot_utc` + route unknown service errors to a generic logged 500 (`writeServerError`, no `err.Error()` leak). L1: dedupe request UUIDs; L2/L3 tests + stale freq-doc fixes.
  - **audio** — M1: `ReadWAV` rejects a short `data` chunk (`len != chunkSize`). L1: `playback.Player` made terminal-after-`Close` (mirrors capture, honours the dead `ErrClosed`) + new non-integration lifecycle tests. L3 comment refresh.
  - **bridge** — M1: an ACKed CI-V commanded freq now refreshes `CurrentDialMHz` (the dial FT8/PSK logging reads), not just SSE. M2: all CI-V snapshot writers (startup/bootstrap/poll/liveness) serialise behind `cmdMu` via a new `underCmdMuCIV` helper. L1: tune↔FT8 mutual-exclusion typed as `ErrTxActive` → 409 `rig_tx_active`. L2 stale comments.
  - **cat** — M1 (safety): CI-V ACK now requires the frame be addressed TO the controller (E0) FROM the rig + be exactly 5 bytes, so a shared-bus ACK can't complete a `tx_on`/`tx_off` confirm. M2 (operator chose **option 1**): data-driven `Command.Min`/`Max` numeric range metadata (Yaesu `FA/FB` 30k–75M Hz, `PC` 5–100 W) enforced in both protocol paths + `validateCommandRange`; CI-V `EncodingBCDPower` now **rejects** over-`ScaleMaxWatts` watts instead of clamping to full power. L2: `CloneRigDefinition` (deep copy; `Lookup` left zero-cost). **L1 (sets_state value-compat) DEFERRED to backlog.**
  - **config** — M1 (security): `WriteJSON` writes `0600` (config holds plaintext SMTP/lookup/forwarder secrets), tightens legacy `0644`, preserves stricter. M2: rig `Model` validated against `cat.Lookup` at the config boundary. M3: `Update`/`UpdateInMemoryThenPersist` deep-`Clone` so an aborted closure's nested mutation can't leak. L1 (operator chose **option 1**): `UnknownKeys` logs hand-edit typos at startup (Load behaviour unchanged). L2 doc refresh.
  - **database** — M1: `UpdateContactedStation`/`UpdateCountry` rewritten to the active-row pattern (`UpdateAll WHERE id AND deleted_at IS NULL` + `ErrNotFound`); **`UpsertLogbook` REMOVED** (dead + caller-supplied-PK upsert is nonsensical for an autoincrement key — operator: "drop it"). M2: migration verification now covers all 6 runtime tables. L1 doc fixes. Closed the 2026-06-14 contacted-station backlog item.
  - **email** — M1: STARTTLS now has coverage — a self-signed-cert TLS-upgrade fake (happy path) + a fail-closed test (no plaintext fallback); minimal `tlsRoots` test hook. M2: `Send` owns address/subject/attachment validation (`ErrInvalidMessage` before any I/O) + MIME params via `mime.FormatMediaType`; config validates `smtp.from`. L1: `smtp.enabled` kill-switch docs across email/config/cmd-smd/handler/endpoint-ref.
  - **errors** — M1 (observability): added `DetailedFrame` (direct type assertion) and switched `logging.buildErrorChain` to it — a mixed `DetailedError → fmt.Errorf("%w") → DetailedError` chain no longer drops the stdlib-wrapper frame or double-counts the nested error. L1 doc fixes (`LocalMsg`/`Cause()` removed, links → `archive/`).

### Session 185 (2026-06-18) — **PSK Reporter reception-report upload SHIPPED + live-validated; FT8 Next-drain & ladder-highlight bugs fixed; port-8080 restart-flap fix + Taskfile dev-daemon guard; go-ft8 v0.3.5 `/P`.** All green throughout (Go build/vet/`-race`/suites; SPA check/lint/build + 835 vitest).
- **go-ft8 v0.3.4 → v0.3.5: standard `/P` now encodes.** `EncodeStandardMessage` accepts the standard `/P` variant, so SM works `/P` stations **end-to-end with NO SM code change** — every TX guard decides by trying the encode + skipping on error, so the upstream gain flows straight through. **Type-4 compound (`PJ4/K1ABC`, `/MM`, …) + free text still skipped** (need WSJT-X hashed-callsign encode, gated on go-ft8). Proven offline (`internal/ft8/modulate_test.go`: `TestEncodeStandardMessage_Portable` + round-trip). Committed.
- **PSK Reporter reception-report upload — new `internal/pskreporter` (report/upload side only).** The FT8 "who's hearing me" / propagation-map feed. **IPFIX/UDP encoder, byte-exact vs the spec's worked example** (`ipfix_test.go`); a buffer → dedup (best SNR per call) → flush service: ~5 min cadence (program-relative timer + jitter, NOT clock-synced), descriptors first-3-datagrams + hourly, one long-lived UDP socket (constant source port). **Fed by FT8 decodes via `ft8.Service.SetDecodeSink`** (one-way DI, same pattern as `SetQsoLogger` — `internal/ft8` stays narrow); `cmd/smd` extracts a spot per decode with **`ft8.SpotFrom`** (reuses the sequencer `parseMessage`; CQ→caller+grid, directed→sender, hashed/free-text skipped), reporting **freq = dial + audio offset** (the real RF, via `bridge.CurrentDialMHz`), SNR, mode FT8, slot time, `informationSource=1`. Receiver identity from `logging_station` (call/grid) + `StationManager <ver>` + **antenna from `MY_ANTENNA`** (sourced from the station config, NOT a separate `psk_reporter` key). Config block `psk_reporter` (`enabled` default **OFF** — opt-in/public; `host`/`port`), set-once like SMTP (not on `/v1/config`). **Collector host = `report.pskreporter.info` (NOT `pskreporter.info` — that hostname is the Cloudflare website and silently drops UDP); port 4739 = production, 14739 = test** (same host, parses without writing the live DB). `cmd/ft8-psk-probe` = a dev/test CLI (flags; defaults to the test port; `-dry`). **Live-validated against the real collector** (`/cgi-bin/psk-analysis.pl` showed our datagram received + every field parsed) AND **running in production on the dogfood daemon** (`pskreporter: uploaded spots` 80–81/flush). **3 review findings fixed:** (1) optional startup made non-fatal (cmd/smd logs+continues; `AddSpot` guards `conn==nil` so a failed start can't grow the buffer unbounded); (2) a full-buffer flush no longer does UDP I/O on the FT8 decode goroutine — `AddSpot` signals the flush loop (non-blocking); (3) the receiver template is a **fixed 4-field shape** (antenna always present, empty when unset) so a runtime `SetReceiver` can't desync the cached template. Committed + deployed.
- **FT8 "Next" drain bug fixed (operator-reported).** Clicking Next on a pile-up no-show *stopped* the auto-drain with no way to restart. **Root-caused from the dogfood logs:** Next → `qso/abandon`, then the drain's immediate `qso/work` hit a transient **`503 rig_not_ready`** (the TX→RX settle right after cancelling the in-flight TX); the drain treated it as terminal — cleared its latch, **lost the already-dequeued head**, and never retried (nothing reactive re-fired the `$effect`). Fix (`Ft8Panel.svelte` drain `$effect`): don't dequeue until the start **succeeds**; on a transient (`rig_not_ready`/network) keep the head + **retry** (~1.5 s, up to ~9 s) via a reactive tick; if the rig's genuinely down after the retries, **pause** (queue kept, Resume restarts); drop only on a hard per-entry rejection. Committed + deployed.
- **FT8 ladder-highlight bug fixed (operator-reported, cosmetic).** Starting a work-a-caller (or answer-a-CQ) exchange briefly highlighted the **RX reply row** instead of the opening **TX rung**, then snapped back when TX keyed. Root cause: `tx.transmitting ? txRow : txRow+1` reads "not transmitting" as "waiting for reply" — but at the open the daemon publishes the rung with `repeats=0` + `transmitting=false` **before** the first key (`work_sequencer.go` 69→75→78). Fix: a `rowFor(txRow,len)` helper keys off `qso.repeats` — TX row when transmitting OR `repeats===0` (about to send), RX row only once sent + waiting (`repeats>0` && !transmitting). Applied to all three ladders (work/answer/caller). The daemon always *sent* the right messages — display-only. Uncommitted (latest).
- **Port-8080 restart-flap.** (1) **`server.StopAccepting()`** closes the listener at the START of shutdown — it was held until the final `server.Shutdown`, behind the multi-second bridge/ft8/psk teardown (`ft8.Stop` waits for an in-flight decode), so a replacement process racing the old daemon's teardown got `address already in use`. Now `:8080` frees immediately; the later `Shutdown` still drains connections. Race-guarded listener field, `net.ErrClosed` treated as a clean stop; regression test `TestServer_StopAccepting_ReleasesPort`. Committed + deployed. (2) **The actual recurring cause = a SECOND smd** — the dogfood `task run:smd` (or a stray `build/bin/smd`) overlapping the systemd daemon on `:8080`. Proven from the journal: systemd smd cleanly Stopped at 16:44:15 yet `:8080` was still in use at 16:44:25 → a freed listen port is instantly rebindable (`SO_REUSEADDR`), so another process held it. Fix: **Taskfile `run`/`run:smd` now auto-stop the systemd smd first** (tolerant of not-running/not-installed) + an echo reminder to restart it after. ⚠️ **Validate Taskfile edits with `task --list`, not `python yaml.safe_load`** — a `: ` (colon-space) inside a cmd string parses as a valid-but-wrong YAML *map*, which `safe_load` accepts but Task rejects ("invalid keys in command"); my first guard shipped broken and was caught on the next `task deploy:local:dev`. Taskfile guard committed (after the YAML fix).
- **Dogfood-inbox:** "footer for Band activity needs to be bigger text" (capture-only).
- **Roll-off due:** the live `### Session N` list is past the ~15 cap; roll the oldest entries to `session-handoff-archive.md` next maintenance pass. (Done in session 186: rolled 171 and older down, leaving 172–186 live.)

### Session 184 (2026-06-17) — **Session-email subject = logbook-callsign-prefixed + body QSO-count line (daemon-only).** All green: `go test ./internal/api`, build.
- **Why:** as multi-operator interest grows, a QSL manager receiving logs from several operators got identical-looking mail (the logging callsign was only inside the ADIF). Now the **subject is prefixed with the logbook callsign** (e.g. `G4ABC Station Manager session ADIF — …`) and the **body adds `Contains N QSOs.`** under "ADIF for this session attached." (singular `QSO` for 1).
- **Where:** `internal/api/handler_session_email.go` — extracted pure helpers `sessionEmailSubject(callsign, supplied, now)` (callsign comes from `db.LogbookCallsignByIDWithContext(qsos[0].LogbookID)` — the literal logbook callsign the QSOs were logged under; best-effort, a fetch failure just omits the prefix, never fails the send) + `sessionEmailBody(n, now)`. Unit-tested (`TestSessionEmailSubject_CallsignPrefix`, `TestSessionEmailBody_QsoCountAndPluralisation`). Hardcoded shape for now.
- **Backlog:** added "Configurable session-email subject + body (formatting tags)" — operator-editable templates (`{callsign}`/`{count}`/`{date}`/…) in config.json, defaults = today's hardcoded strings; this shipped as the first step. **Uncommitted.**

### Session 183 (2026-06-17) — **FT8 pile-up callsign stacking SHIPPED (SPA-only).** All green: SPA check/lint/format + 833 vitest; no daemon change.
- **The feature (operator-designed workflow):** during a pile-up, stations call you but are only *clickable* (work-now) when armed+idle, so callers spotted mid-QSO vanish before you can act. Now **Ctrl/Cmd+click** a calling-you decode to push it onto a **FIFO pile-up stack** (worked oldest-first); the Operate view **drains** it via the existing work-a-caller path whenever armed+idle, advancing as each contact completes, while the operator keeps adding. Capture (Ctrl+click) works in **any** state (mid-QSO, disarmed — pure capture, no TX), which is the whole point (callers are only visible in your RX parity).
- **Architecture (operator's call):** SPA owns the queue, daemon untouched. New `ft8PileupStack.svelte.ts` (in-memory FIFO, dedup-by-call refresh-in-place, push/peek/dequeue/remove/clear + `enabled` drain flag; erased on tab close, like `callsignStack`). `Ft8Panel`: `onCallerClick` (Ctrl→enqueue, plain→work-now), a `✓` marks stacked rows, and a drain `$effect` (armed+idle+freqKnown+offset+enabled+non-empty → `startFt8WorkCaller(head)`, re-entry latch until `qso.active` confirms). `Ft8PileupDrawer.svelte` in the Operate tab (call·grid·SNR, per-entry remove, Clear-all, Resume-when-paused) + a depth badge on the Operate tab. **Abandon pauses** the drain (queue kept; Resume restarts).
- **Supersedes** the daemon `caller_answer_mode: operator_pick` Call-CQ mode (still `501`-rejected, now unlikely to be built — the stack gives operator-chosen working for anyone calling you). `auto_first` Call CQ stays as the hands-off loop. Attended-only preserved (operator Ctrl+clicks every station — *more* attended than `auto_first`).
- Tests: `ft8PileupStack.test.ts` (7). Docs: ADR 0033 amendment, `ft8.md`, `backlog.md` (item closed), `keyboard-shortcuts.md`. **Uncommitted.**

### Session 182 (2026-06-17) — **FT8 Settings toggle + IC-7300 operating-freq POLL fix (the real cause of the wrong-band logging) + backlog adds + filter design pending.** All green: Go cat/bridge/types tests, SPA check/lint/format + 826 vitest.
- **IC-7300 POLL was missing the operating-freq read (root cause of the wrong-band logging + "waiting for rig").** ADR 0035 kept VFO-A push-only (Transceive), but push fires only on a freq **change** — parked, VFO-A is never re-sent, so a fresh SPA tab/reconnect misses it and `catState.vfoA` sits at the 14.250 placeholder. Confirmed by tapping `curl -sN :8080/v1/rig/events`: `vfoA` came once, then the poll cycle repeated `vfoB/mode/split/power` with no `vfoA`. **Fix: added `2500` (operating-freq read) to the IC-7300 rigdef POLL** (`icom-ic7300.json` POLL = `["2500","2501","2600","0F","140A"]`, mirroring READ); push stays as the real-time-on-change layer, poll is the steady-state backstop. `civ_test.go` `TestEmbeddedIC7300` POLL golden updated; ADR 0035 revised; memory `project_sm_ic7300_borrowed` updated. **⇒ Embedded rigdef → needs a redeploy + bench re-confirm of poll cadence with the extra read.** This is the proper fix behind the session-181 SPA freq guard (which stays as belt-and-braces). NB diagnosis: the live daemon was the **systemd/dogfood** `/usr/bin/smd` (holds the IC-7300 ttyUSB, logs to `~/.local/share/station-manager/log/`), not `task run:smd` (logs `build/log/smd.log`) — `deploy:local:dev` runs dev code on systemd.

- **Float-CQ-to-top toggle (shipped, backlog item closed).** New daemon-backed `ft8.display.cq_to_top` (bool, default off): `types.Ft8DisplayConfig.CqToTop` + `ResolveFt8Display` passthrough; SPA mirror (`config.svelte.ts` `Ft8DisplayView.cqToTop`, `api/config.ts` `Ft8DisplayFields.cq_to_top`); Settings-tab checkbox; `Ft8Panel` `orderedDecodes` `$derived` stable-partitions CQ rows above the rest (gated on `cqToTop`, applies in BOTH feed modes), and **suppresses per-slot separators** while on (the list is no longer slot-ordered). Tests: Go `cq_to_top` resolve case, SPA `config.ft8.test.ts` hydrate/default. Docs: `ft8.md`.
- **Backlog adds (operator-flagged on air):** (1) **work compound/portable (`/P`,`/MM`,…) callsigns + free text** — `EncodeStandardMessage` rejects them so the sequencer skips such answerers/callers; needs WSJT-X hashed-callsign (type-2) support (likely a `go-ft8` API addition) + free-text encode + UX. Real protocol work. (2) **Band Activity display filter** — design pending (below).
- **⇒ OPEN DESIGN (this session, not built): Band Activity prefix/substring filter.** Session-scoped (in-memory like the selected offset, NOT config). Open points: match target (callsign vs whole text), prefix vs substring, placement, interaction with float-CQ-to-top. Pin design → then build.
- **Uncommitted** (toggle + docs + backlog).

### Session 181 (2026-06-17) — **FT8 wrong-frequency logging bug fixed + dial-freq label added.** SPA-only; all green (check/lint/format + 826 vitest).
- **Bug (operator-reported):** every FT8 QSO logged at **14.250 / 20m** while actually on **21.074 / 15m**. Root cause: `catState.vfoA`/`vfoB` init to a *valid-looking* placeholder **`DEFAULT_VFO_HZ = 14_250_000`**; the FT8 path logs `displayedState` selected-VFO freq, and on the IC-7300 (CI-V freq arrives via the **bridge poll**, not a broadcast) the rig was "responding" before the freq poll landed — so `vfoA` was still the placeholder, logged silently with **no frequency shown anywhere** to catch it. (Confirmed not a VFO-B mixup — VFO-B was 14.139.5; the value was exactly the placeholder. The band-button highlight later showing 15m confirmed the freq *does* arrive, just not in time for those QSOs.)
- **Fix (SPA):** new `catState.freqKnown` — flips true on the first `rig-state` carrying a `vfoA`, false on disconnect (`bridge.svelte.ts` `mergeRigState` + both `rigResponding=false` sites). FT8 now treats `freqKnown = displayedState.isLive && catState.freqKnown` as a gate: **FT8 TX/QSO-start is blocked** (answer/work-caller rows un-clickable via `canAnswer`, Call CQ disabled via `canSend`) and **no band button highlights** until a real dial freq is known — so a placeholder-band QSO can't be logged again.
- **UX (operator request):** a **dial-frequency label below the Main-Freq buttons** (`formatFrequency(opFreq)`, the exact value FT8 logs) showing **"waiting for rig…"** (amber) until `freqKnown`. Its absence was why the bug went unnoticed.
- Files: `cat.svelte.ts` (+`freqKnown`), `bridge.svelte.ts` (set/reset), `Ft8Panel.svelte` (label + highlight gate + `canAnswer`), `Ft8MsgPanel.svelte` (`canSend`). Tests: 2 new `bridge.test.ts` cases (freqKnown flips on vfoA / resets on error). Docs: `ft8.md`. **Uncommitted.**
- **REAL FIX (added after operator pushback — the SPA guard/label didn't address it).** The operator confirmed the SPA *displayed* the right freq, so the bug wasn't a stale SPA mirror — it was the **logged** freq: a **start-time snapshot** (`operating_freq_mhz` → `s.dialFreqMHz`) used verbatim by `BuildQso`, and **for Call CQ captured ONCE and reused for the whole pile-up** (`caller_sequencer.go:59`). So a session started before the IC-7300's freq poll landed logged the placeholder for every QSO. Fix: the daemon now logs the **rig's live dial from the bridge at completion** — new `bridge.Service.CurrentDialMHz()` (rolling `lastVfoA/lastVfoB/lastSelectedVfo/dialKnown` snapshot, fed by `captureDialFreq` in the read loop next to `captureTuneSnapshot`, mu-guarded, cleared in `clearTuneOnDisconnect`); the `cmd/smd` e4 sink prefers it over `c.DialFreqMHz`, SPA value as fallback. Same injection pattern as `CurrentPowerW` → `internal/ft8` stays bridge-import-clean (boundary test green). Tests: `TestCurrentDialMHz`, `TestClearTuneOnDisconnect_ForgetsDial`. Verified: bridge `-race`, ft8/api/bridge suites, CGO-off build, vet, gofmt. SPA freq label + freqKnown guard KEPT (operator: keep the guard).
- **Possible follow-up (not done):** why the IC-7300 freq poll can lag for many slots at session start — worth a look in the bridge CI-V poll, but the bridge-sourced-at-completion freq + the guard make it safe regardless.

### Session 180 (2026-06-17) — **FT8 "work a caller": work stations calling YOU, picked from the pile-up (ADR 0033 amendment).** ALL GREEN: full `go test ./...`; SPA check/lint/format + 824 vitest. (Gates were briefly blocked by a classifier outage mid-session; re-run clean after it cleared.)
- **The gap:** you could answer a CQ or Call-CQ-and-auto-work-the-first-answerer, but a station *calling you directly* (`7Q5MLV PA3KUS JO21`) — e.g. tail-enders after you answered someone — was not actionable. Now it is: pick it from the Band-Activity pile-up and work it. Chosen scope = the focused "work-a-caller" increment (the full Call-CQ `operator_pick` stack still pending; this is its manual, no-CQ sibling + foundation).
- **Daemon (`internal/ft8`, narrow scope intact):** new mode **`seqWorking`** + **`Sequencer.StartWorkCaller`** + **`onSlotWorking`** (new file `work_sequencer.go`) — reuses `CallerExchange` (we report first → R-report → RR73 → log), but with **no CQ phase** and an **idle terminal** (on RR73 logs + goes idle; on max-repeats abandons — unlike Call-CQ which loops to CQ). `fireOpening` gained a `seqWorking` case (opening report can fire same-slot). `statusLocked` gained a `seqWorking` branch (role `caller`, cap on the pre-RR73 rung). The report we send = our SNR of the picked decode (SPA passes `their_snr`).
- **Daemon gate + API:** `Service.StartWorkCaller` (arm/identity/`TxReady` gate, mirrors `StartQso`); **`POST /v1/ft8/qso/work`** `{their_call, their_grid, their_snr, slot_utc, offset_hz, operating_freq_mhz}` → 202; abandon via the shared `qso/abandon`.
- **SPA:** `ft8Message.ts` `parseDirectedToMe(text, myCall)` (matches ONLY the grid-bearing opening, not mid-exchange `R-12`/`RR73`/`73`); `ft8qso.ts` `startFt8WorkCaller`; `Ft8Panel.svelte` tints directed-at-you decodes **amber** (live, even mid-contact) and makes them **clickable when armed + idle** → `workCaller` (live-but-not-clickable-until-idle, the operator's pick). Directed-at-you calls also get the flag/worked-before enrichment.
- Tests: Go `TestWorkCaller_*` (happy path / max-repeats→idle / start errors / unencodable caller / abandon), `TestStartWorkCaller_Gating`, `TestHandleFt8QsoWork`; SPA `parseDirectedToMe` describe (10 cases). Docs: ADR 0033 amendment, `docs/ft8.md`, `api-endpoints.md`.
- **`RR73` Maidenhead collision fix (caught by the SPA test):** `RR73` satisfies the grid regex `^[A-R]{2}[0-9]{2}$` (R,R ∈ A–R; 7,3 ∈ 0–9), so `parseDirectedToMe` first read `<me> <them> RR73` (a roger, mid-QSO) as a fresh caller. Excluded `RR73` explicitly, mirroring the daemon parser (`sequence.go` checks RRR/RR73/73 before `isGrid4`). RRR/73/R-report don't match the regex, so RR73 is the only clash.
- **On-air validated 2026-06-17** (operator worked 9A4ZM via the pile-up). One display wrinkle found + fixed: a work-a-caller session reused the **Call-CQ ladder**, showing a spurious `tx CQ …` row that never happened + an unfilled `<GRID>` opening. Fix: `seqWorking` now publishes a distinct **`role: "worker"`** + `QsoStatus.TheirGrid` (carried for all roles); the SPA renders a dedicated **no-CQ work ladder** (opening = their call to us, real grid) and fills `<GRID>` from `qso.their_grid` on the caller ladder too. Daemon role const + the `their_grid` field; SPA `Ft8MsgPanel` `workLadder`/`workStep` routed by role. All green: SPA check/lint/format + 824 vitest; Go ft8/api. **Committed** (the e2e feature); this ladder fix is uncommitted pending the operator's next commit.

### Session 179 (2026-06-17) — **FT8 Operate UX: "Working [callsign]" channel banner + per-rung auto-abandon countdown.** All green: SPA check/lint/format + 816 vitest; Go ft8/types/config/api tests, vet, CGO-off build, gofmt.
- **"Working [callsign]" channel banner (SPA-only, RX-safe).** An always-visible strip below the slot countdown (every lower tab) that shows only while a contact is in flight (`qso.active`): `Working <call> — channel clear/BUSY`, green/red on whether the **selected TX channel** overlaps the latest occupied bands, re-evaluated each slot. Closes the pick-time → TX-time gap (a channel chosen clear can go busy a slot or two later, before TX keys). New reactive getter `ft8State.channelOccupied` (`[offset, offset+signalWidth)` vs `occupied`); consolidated the pre-existing duplicate `selectedOffsetBusy` derived onto it. Grey "unknown" when no offset/occupancy yet.
- **Per-rung attempts-remaining countdown** folded into that banner (`· N calls left`). The sequencer auto-abandons an unanswered rung after a repeat cap (ADR 0031 off-ramp); the banner now counts it down. Daemon: added `MaxRepeats` to the `ft8-qso` `QsoStatus` payload, advertised **only on the capped rungs** (answerer pre-73 / caller working-an-answerer pre-RR73; 0 on calling-CQ + one-shot 73/RR73), so the SPA shows the countdown iff `max_repeats>0` with no cap-vs-one-shot logic of its own. remaining = `max_repeats − repeats`, floored at 0.
- **The cap is now a config knob** (operator request): `ft8.tx.max_repeats`, default **6**, `0`/absent → default, **hard-clamped ≤ 10** (`types.Ft8MaxRepeatsCeiling` + `ResolveFt8MaxRepeats`) — a safety bound alongside the tune-power/auto-off clamps so no config value can keep the rig calling a dead station for minutes. Threaded via `newSequencer(…, maxRepeats, …)` from `types.ResolveFt8MaxRepeats(cfg.TX)`; `defaultSeqMaxRepeats` now references the types const (single source of truth). Config-only today (no Settings-tab control yet, like `caller_answer_mode`). Registered in `config.md` §4 safety-ceiling table + `defaults.go` doc block.
- Tests: Go `TestResolveFt8MaxRepeats` (clamp/default), `TestSequencer_MaxRepeatsHonouredAndExposed`; SPA `channelOccupied` describe (5) + `ft8-qso` `max_repeats` mapping (2). Docs: `docs/ft8.md` (banner + countdown + `tx.max_repeats` knob), `config.md`, `defaults.go`.

### Session 178 (2026-06-16) — **Bridge TX-safety hardening (review fix, 4 findings) + small FT8/UI fixes.** All green: `go test ./...`, `-race` bridge+ft8, vet, CGO-off build.
- **A bridge TX-safety code review landed (4 findings on the keyed-transmission path); all fixed with regression tests.** Doc: `docs/reviews/internal-bridge-2026-06-16.md`; durable notes in memory `project_sm_serial_bridge` ("TX-safety hardening" section).
  - **H1 — generic commands could write during PTT.** `SendCommands` gated only on connect+identity; `set_power` is Exposed, so a command could override the tune-power clamp and TX at full power. Fix: `ErrTxActive` sentinel; refuse when `tuneActive||ft8TxActive` → API **409 `rig_tx_active`**.
  - **H2 — CI-V FT8/tune key/unkey bypassed the ACK contract.** On `icom_civ` the `tx_on`/`tx_off` were plain writes; a NAK/no-ACK `tx_off` looked like a clean unkey → backstop cancelled → **stranded PTT**. Fix: factored `writeCIVFramesAwaitAck` out of `sendCommandsCIV`; new protocol-aware `writeKeyedLine` (CI-V waits FB/FA per frame, Yaesu unchanged) used by **both** FT8 + tune key/unkey/restore. A failed `tx_off` now keeps TX armed for the backstop.
  - **M — releases not serialized.** New `keyMu` across the full `KeyFt8Tx`/`StartTune`/`releaseFt8Tx`/`releaseTune` bodies (lock order `keyMu→cmdMu→mu`); no double-release / key-over-settle.
  - **L — `TxReady`** now also requires `!tuneActive && !ft8TxActive`.
  - Tests added: `TestSendCommands_RefusedWhileTransmitting`, `TestTxReady_FalseWhileTransmitting`, `TestKeyUnkeyFt8TxCIV_ConfirmedByAck`, `TestUnkeyFt8TxCIV_NoAckKeepsArmed`, `TestReleaseTune_ConcurrentStopsReleaseOnce`, `TestKeyRelease_ConcurrentStartStopNoDeadlock`.
  - **⇒ On-air follow-up:** H2 changed the bench-validated IC-7300 FT8 TX path (now waits for `tx_on`/`tx_off` ACKs) — **re-validate FT8 TX on the IC-7300 before relying on it on air** (FTdx10/Yaesu fire-and-forget path unchanged).
- **Smaller fixes earlier in the session (committed):** FT8 enrichment pane now shows the short-path **beam heading** (`045°`, matches the per-CQ column); the **serial-open error toast** was shortened (drop the ~95-char by-id path, UI-only — daemon still logs it); the FT8 hub now **clears the decode+occupancy replay cache on capture release** (`clearActivity`) so reopening the SPA with the rig off no longer shows the previous session's Band Activity (offset, in localStorage, is kept). The backlog "Abandon doesn't stop in-flight TX" item was confirmed **already fixed** (646d476d) and pruned.

### Session 177 (2026-06-16) — **Per-direction name-based audio devices SHIPPED & validated with a real FT8 QSO.** The deferred 2e (config.md §10.4 #1) daemon side, done — device selection is now a per-rig property.
- **Audio model REVISED single-name → per-direction (operator's call), then shipped.** Session 176 had closed §10.4 #1 as *single* name-based `RigConfig.Audio.Device`; the operator chose **two flat fields** `RigConfig.Audio.{rx, tx}` (each a device **name**) instead — more robust (a rig isn't guaranteed to share one codec/name across RX+TX, and the single field was never wired for playback). §10.4 #1 updated in place to record the reversal (no new ADR; the deleted 0036 had drafted a two-field model but contradicted the *then*-current single-name decision — now the decision itself is per-direction).
- **What shipped (daemon only, no SPA):** `RigAudioConfig{RX,TX string}` (names) replacing `{Device}`; `Config.ActiveFt8()` projects `audio.rx→Ft8Config.Device` (capture) + `audio.tx→Ft8Config.TX.Device` (playback) — completing the half-wiring; `internal/audio/{capture,playback}` gained a `DeviceName` config field resolving name→live-index at **acquire** (fail-soft: no match → that direction idle, never wrong default; integer string still honoured as a raw index for un-migrated configs); `internal/ft8` `resolveAudioDevice()` + the `newPlayer(name,index)` seam carry it; `internal/ft8` still imports no rig catalogue (consumes plain `Ft8Config`, same discipline as `ActiveBridge`). **Global `ft8.device`/`ft8.tx.device` operator knobs DROPPED** — switching `default_rig_id` now re-binds audio with the CAT port+driver. **No index→name auto-migration** (loader can't enumerate safely); legacy migration no longer fakes audio from an index.
- **Validated end-to-end:** unit (`TestApplyRigProfiles_ResolvesAndProjects` asserts RX→Device, TX→TX.Device), full `go test ./...` (CGO on+off), gofmt, config-SPA `svelte-check` all green. Live RX decode on dev (named `"PCM2901 Audio Codec Analog Stereo"` → IC-7300 capture), then a **full two-way FT8 QSO with a UK station on the deployed dogfood binary via the IC-7300** — name-based audio for BOTH RX and TX, and switching `default_rig_id`→3 re-bound to PCM2901 with no other change.
- **Configs updated:** `build/config.json` (dev) IC-7300 rig carries `audio.{rx,tx}="PCM2901 …"`, globals removed. Dogfood `~/.local/share/.../config.json`: **IC-7300 added (id 3, PCM2901)** + FTdx10 (id 1) given `audio.{rx,tx}="PCM2903C Audio CODEC Analog Stereo"`; stale global `ft8.tx.device` removed; default_rig_id since switched to 3 (IC-7300). FT-710 (id 2) left untouched (not plugged in → codec name unknown). **Note capture/playback enumerate the codec under the *same* name per direction** (PCM2901 capture idx 3 / playback idx 1; PCM2903C capture idx 1 / playback idx 0 — names match, indices don't).
- **Docs:** config.md §10.3/§10.4 #1/§10.5 + decisions table, rig-profiles.md, `internal/types/{rig,ft8}.go` + `internal/hardware` comments, memory `project_sm_ic7300_borrowed`.
- **⇒ Only remaining rig-profiles item:** the **by-name picker UI** (config-SPA rig-profile editor) — operator hand-edits `audio.rx`/`audio.tx` names until it lands. Everything daemon-side is done.

### Session 176 (2026-06-16) — **First Icom on-air FT8 TX validated end-to-end.** The Session 175 `-key` bench ran; full clean key→TX→auto-unkey cycle on the IC-7300.
- **`ft8-tx-probe -key` bench passed on the real rig (first Icom RF through SM).** RF-safe phase first: `/tmp/ft8-tx-probe -device=2 -msg="CQ 7Q5MLV KH78" -offset=1500 -wav=/tmp/tx.wav` → `/tmp/ft8-decode-file /tmp/tx.wav` gave an **exact round-trip** (`1500.0 Hz CQ 7Q5MLV KH78`), proving encode→modulate→PCM2901-out→decode agree. Then the keyed phase: stop the dogfood daemon (frees serial), `/tmp/ft8-tx-probe -key -config build/config.json -msg="CQ 7Q5MLV KH78" -offset=1500`. Operator confirmed: **rig keyed on the UTC slot, switched to USB-D (`ft8.tx.mode` from rigdef), transmitted, and unkeyed cleanly on its own** at waveform end → back to RX — no Ctrl-C, no 18 s auto-off backstop. So the `ft8.TxKeyer`→bridge `KeyFt8Tx`→tune-controller guaranteed-stop chain (ADR 0030) is proven on the **second rig family**. Probe binaries are throwaway (`/tmp/ft8-tx-probe`, `/tmp/ft8-decode-file`); nothing committed this session except docs/memory.
- **ADR 0036 cleanup DONE.** Deleted `docs/decisions/0036-rig-profile-audio-devices.md` (it duplicated + contradicted `config.md` §10.4 #1, which already decided the **single name-based** `RigConfig.Audio.Device` → both endpoints on 2026-06-13). Repointed ADR 0028's audio-model forward-note at config.md §10.4 #1 (single-name, per-direction resolution; not the two-field shape 0036 had drafted). Added a validation note to §10.4 #1: the IC-7300's PCM2901 codec appears under the *same name* as capture idx 4 / playback idx 2 — a clean demonstration of single-name→both-endpoints (and of why an index can't be the identifier) — plus the `ActiveFt8()` clobber-bug fix. **The whole IC-7300 arc is now closed**; per-rig audio *implementation* stays deferred to the config-SPA rig-profile-editor workstream (unchanged).

## Next steps (priority order)

> **⚠️ CURRENT NEXT STEPS (as of session 185, 2026-06-18) — the items deeper below are
> STALE history (operator_pick is SUPERSEDED, IC-7300 arc closed; kept for the trail):**
> 1. **Commit the FT8 ladder-highlight fix** (`Ft8MsgPanel.svelte` `rowFor`) — the one
>    piece of session 185 left uncommitted. Most of session 185 (PSK Reporter, port fix,
>    Next-drain fix, go-ft8 v0.3.5 `/P`) is already committed + deployed; confirm the
>    178–184 arc landed too.
> 2. **Redeploy + on-air verify the two SPA bug fixes** (`task deploy:local:dev`): the
>    **Next-drain fix** (Next now advances past a no-show instead of stalling on the
>    transient `rig_not_ready`) and the **ladder-highlight fix** (an exchange opens on our
>    TX rung, not the RX reply row). PSK Reporter is **already live + validated** (don't
>    re-check). After a `task run:smd` dev session, restart the dogfood daemon
>    (`systemctl --user start smd`) — the Taskfile now stops it for you on `run:smd`.
> 3. **Parked design — Band Activity prefix/substring filter** (session 182; backlog,
>    ready to shape → build).
> 4. **PSK Reporter follow-ups (future, in backlog):** the **retrieve/query side** (who
>    heard *you* — distinct from the report side just shipped); a **config-SPA surface**
>    for the `psk_reporter` block (hand-config for now); and **generalize to a
>    spot-submitter registry only when a 2nd destination (DX cluster) lands** (build
>    specific, not generic — one destination doesn't justify the framework).
> 5. **Maintenance:** roll sessions 169–170 into `session-handoff-archive.md` (the live
>    `### Session N` list is at ~17, past the ~15 cap).
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
  roll-off: 2026-06-24 (Sessions 172–175 → archive; live kept 176–190).
