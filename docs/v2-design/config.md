# Config System — current-state review

> **Status:** review (current state). **Phase:** review-first; the redesign is a
> separate follow-up — see §8 for the parked inputs. **No code changes accompany
> this document.**
>
> **Method:** the **Go code is the source of truth** for everything below. On-disk
> `config.json` files (dev `build/config.json`, dogfood `~/.local/share/station-manager/config.json`)
> drift from the code and from each other and are **not** authoritative — that drift is
> itself a finding (§7). Every entry traces to a symbol in the cited file.
>
> This is the consolidation point the config system never had: prior decisions are
> scattered across ADR 0002 (SPA config shape), 0003 (daemon-only source), 0004
> (daemon/SPA split), 0028 (rig profiles) and `rig-profiles.md`, with no single owner.

## 1. Scope & method

- **Authority = code.** Structs, `applyDefaults`, `Resolve*`, `validate*`, and the
  subsystem reads define the system; config.json is just one (often stale) serialization.
- **Rigdefs are an immutable substrate.** `internal/cat/rigs/*.json` are `//go:embed`-ed
  at build time (`internal/cat/rigdb.go`); the operator cannot edit them (external-dir
  loading is an unimplemented stub). They are **in scope as a truth/defaults source**
  (CAT command/state tables, serial defaults, mode-mapping defaults that config.json
  *overrides*) but **out of scope as a mutable-config target**.
- **In scope:** `internal/config/config.go`, the `types.*Config` structs, the
  `GET/PUT /v1/config` handler, the SPA config mirror, and how each subsystem reads config.
- **Out of scope (this phase):** any redesign decision (§8) and any code change.

## 2. Source-of-truth map

Where authority *actually* lives in code, per category. "config.json" below means the
in-memory `config.Config` loaded from it — not the file as found on disk.

| Category | Authority | Where (code) | Notes |
|---|---|---|---|
| Server / HTTP tunables | config.json | `config.go` `ServerConfig` + `applyDefaults` | All overridable; consts are fallbacks |
| Datastore (sqlite path, pool) | config.json | `types.DatastoreConfig` + `applyDefaults` | Read once at startup |
| Logging | config.json | `types.LoggingConfig` + `DefaultConfig`/`applyDefaults` | |
| Operator identity (ADIF `MY_*`) | config.json | `Config.LoggingStation` (`types.LoggingStation`) | `MyLat`/`MyLon` daemon-derived from `MyGridsquare` on PUT |
| Station prefs (amp, default power) | config.json | `Config.Station` (`types.StationConfig`) | |
| Enrichment providers + TTLs | config.json | `Config.Lookup` (`types.EnrichmentConfig`) | |
| Forwarder destinations | config.json | `Config.Forwarders` | Presence = intent (ADR 0022) |
| SMTP / mailer | config.json | `Config.Smtp` (`types.SmtpConfig`) | No PUT path; file-only |
| **Rig capability** (CAT cmds/states, serial defaults, **mode-mapping defaults**) | **rigdefs (immutable)** | `internal/cat/rigs/*.json` via `cat.rigDB` | Operator overrides layer on top in config.json |
| Rig identity/selection (model, port, audio) | config.json | `Config.Rigs[]` (`types.RigConfig`) + `DefaultRigID` | Catalogue is authoritative; **no DB rows for rigs** |
| Mode-mapping **overrides** | config.json | `Config.Bridge.ModeMappings`; merged with rigdef at GET (`bridgeInfoFor`) | Merged view is authoritative; only operator deltas persist |
| **Logbook metadata** (name, per-logbook callsign, description) | **Database** | `logbook` table; `Config.DefaultLogbookID` is only a selector | DB owns the rows; GET joins them |
| **Enrichment cache** (country, contacted_station) | **Database** (derived) | `country` / `contacted_station` tables; config TTLs decide staleness | Split: DB owns `last_refreshed_at`, config owns the TTL |
| **Forwarding queue** | **Database** (ephemeral) | `qso_upload` table; config lists destinations | Split: DB owns per-row state, config owns which destinations exist |
| Working directory | env → config → exe dir | `utils.WorkingDir(cfg.DataDir)`; `SM_WORKING_DIR` wins | |
| Runtime config (what subsystems actually run with) | in-memory resolved `Config` | `config.Service.Cfg` via `Snapshot()`; `ActiveBridge()`/`ActiveFt8()` projections | The real runtime authority once loaded |

**Headline:** authority is genuinely split — config.json (operator intent), the DB
(logbook metadata + caches + queue), rigdefs (rig capability + defaults), and the
in-memory resolved `Config` (runtime). Treating config.json as *the* source of truth is
the framing error this review corrects.

## 3. Field inventory

Consumer = who reads it (**D** = a daemon subsystem, **SPA** = served via `/v1/config`
and mirrored in `configState`, **both**). Lifecycle: **start** = snapshot at
construction/`Start`; **per-op** = read live each operation (via `Snapshot()`/accessors);
**served** = sent to the SPA and read reactively. Rig? flags values that are (or
secretly are) rig-specific.

### Top-level `Config` (`internal/config/config.go`)

| Field | Consumer | Lifecycle | Rig? | Notes |
|---|---|---|---|---|
| `DataDir` | D (datastore, logging) | start | — | Resolved via `utils.WorkingDir` |
| `UserAgent` | D (forwarders, lookups) | start | — | First-run filled `station-manager/<version>` |
| `SocketPath` | D (api listener) | start | — | |
| `Server` | D (api) | start (+ per-op page/rate limits) | — | See ServerConfig |
| `Datastore` | D (sqlite) | start | — | |
| `Logging` | D (logger) | start | — | |
| `Forwarders[]` | D (forwarder workers) | start | — | One worker per entry |
| `LoggingStation` | both | per-op + served; PUT-writable | — | ADIF `MY_*`; daemon derives lat/lon |
| `SetupComplete` | both | served; server-managed | — | Handler flips false→true on first callsign |
| `DefaultLogbookID` | both | per-op | — | Selector; row metadata in DB |
| `DefaultRigID` | both | per-op | — | Selects active rig in `Active*()` |
| `Station` | both | per-op + served; PUT-writable | — | Amp + CAT-off default power |
| `Lookup` | D (enrichment) | start (TTLs via per-op accessors) | — | |
| `Smtp` | D (mailer) | start; `enabled`+recipient served | — | No PUT path |
| `Bridge` | both | start (via `ActiveBridge()`); subset served | **partly** | `Serial.Port` + `Cat.Driver` are rig-specific |
| `Ft8` | both | start (via `ActiveFt8()`); display/freqs served | **partly** | `Device` + `tx.mode` are rig-specific |
| `Rigs[]` | both | per-op (`RigByID`) | **yes** | The rig catalogue (ADR 0028) |

### `BridgeConfig` (`internal/types/bridge.go`)

| Field | Consumer | Lifecycle | Rig? | Notes |
|---|---|---|---|---|
| `Enabled` | both | start | — | Gates the whole subsystem |
| `Serial.Port` | D + served | start | **yes** | Loose field; **authoritative source is `RigConfig.Port`**, projected by `ActiveBridge()` |
| `Cat.Driver` | D + served | start | **yes** | Loose field; **authoritative source is `RigConfig.Model`** |
| `Timeouts.*` (liveness/backoff×2/steady/write-watchdog) | D | start | global (could be rig-specific) | Same thresholds for every rig today |
| `Tune.*` (power/duration/restore-settle) | D | start | global | Clamped at construction (see §4 ceilings) |
| `ModeMappings` | served | per-op (merged with rigdef at GET) | **yes** (keyed by driver + rig literal) | Operator overrides only; correct by design |

### `Ft8Config` (`internal/types/ft8.go`)

| Field | Consumer | Lifecycle | Rig? | Notes |
|---|---|---|---|---|
| `Enabled` | both | start | — | Gates the subsystem |
| `Device` (capture) | D | start | **yes** | Loose; authoritative source `RigConfig.Audio.Device`, projected by `ActiveFt8()` |
| `EnableOSD` (`*bool`) | D | start | — | nil→true |
| `TX.Device` (playback) | D | start | **yes** (not yet projected) | Should follow `RigConfig.Audio` like capture |
| `TX.Mode` | D | start (read at arm via `txMode()`) | **YES — rig-specific but GLOBAL** | `"DATA-U"` is Yaesu vocabulary; the trigger for this review |
| `TX.CallerAnswerMode` | D | start (resolved) | — | `auto_first` / `operator_pick` |
| `TX.Occupancy.*` | D | start (per-slot read of snapshot) | — | Occupancy detector tuning |
| `Display.*` (history/feed/colours) | SPA | served; PUT-writable (presence-aware) | — | Daemon stores, ignores; pure SPA prefs |
| `Frequencies` (band→Hz) | SPA | served (resolved) | — | Main-Freq buttons; PUT not yet wired |

### `RigConfig` (`internal/types/rig.go`)

`ID`, `Model`, `Port`, `Audio.Device` (all per-rig), and `Overrides` (per-rig serial
params — **declared but NOT yet wired**; `ActiveBridge()` does not apply them). This is
the per-rig home that the rig-specific fields above *should* live in but mostly don't yet.

### Other blocks (all global, config.json-authoritative, read at start)

`StationConfig`, `LoggingStation` (ADIF `MY_*`), `ForwarderConfig` (+`RetryConfig`),
`EnrichmentConfig`/`LookupConfig`, `SmtpConfig`, `DatastoreConfig`, `LoggingConfig`,
`ServerConfig` — fields enumerated in their respective `internal/types/*.go`.

## 4. Default-const ↔ knob catalog

Every code constant that a config knob overrides, by flavor. **The "no magic numbers"
rule holds for flavors (a)–(c): consts are fallbacks.** Flavor (d) is different — the
const is a *ceiling*, not a default, and must stay non-overridable.

### (a) default-fill — `applyDefaults` replaces a zero value (`internal/config/config.go`, const block ~L462)

| Const | Value | Knob |
|---|---|---|
| `defaultReadHeaderTimeoutSec` | 5 | `server.read_header_timeout_sec` |
| `defaultReadTimeoutSec` | 10 | `server.read_timeout_sec` |
| `defaultWriteTimeoutSec` | 30 | `server.write_timeout_sec` |
| `defaultIdleTimeoutSec` | 120 | `server.idle_timeout_sec` |
| `defaultShutdownTimeoutSec` | 10 | `server.shutdown_timeout_sec` |
| `defaultMaxBodyBytes` | 1 MiB | `server.max_body_bytes` |
| `defaultPageLimit` / `defaultMaxPageLimit` | 50 / 500 | `server.default_page_limit` / `max_page_limit` |
| `defaultMaxConcurrentRequests` | 128 | `server.max_concurrent_requests` |
| `defaultMaxEventSubscribers` | 16 | `server.max_event_subscribers` |
| `defaultSubmitRatePerSec` / `defaultSubmitRateBurst` | 20 / 40 | `server.submit_rate_per_sec` / `submit_rate_burst` |
| `defaultMaxContactHistoryResults` | 100 | `server.max_contact_history_results` |
| `defaultMaxOpenConns` / `defaultMaxIdleConns` | 8 / 8 | `datastore.max_open_conns` / `max_idle_conns` |
| `defaultContextTimeoutSec` / `defaultTxContextTimeoutSec` | 10 / 10 | `datastore.context_timeout` / `transaction_context_timeout` |
| `defaultLogFileMaxSizeMB` / `MaxBackups` / `MaxAgeDays` | 100 / 5 / 30 | `logging.log_file_*` |
| `defaultAmpMultiplier` | 1.0 | `station.amp_multiplier` |
| `defaultLogbookID` / `defaultRigID` | 1 / 1 | `default_logbook_id` / `default_rig_id` |
| `defaultForwarderTickIntervalSec` / `BatchSize` | 120 / 5 | `forwarders[].tick_interval_sec` / `batch_size` |
| `defaultCountryTTLDays` / `StationTTLDays` / `RefreshMaxInFlight` | 365 / 90 / 4 | `lookup.country_ttl_days` / `station_ttl_days` / `refresh_max_in_flight` |
| `defaultLookupHTTPTimeoutSec` | 10 | `lookup.hamnut.timeout_sec`, `lookup.chain[].timeout_sec` |
| `defaultSmtpPort` / `defaultSmtpTimeoutSec` | 587 / 30 | `smtp.port` / `smtp.timeout_sec` |
| (inline) protocol / socket | `tcp` / `127.0.0.1:8080` (tcp) or `$TMP/smd.sock` | `server.protocol` / `socket_path` |
| (inline) datastore driver / path | `sqlite` / `${DataDir}/db/station-manager.db` | `datastore.driver` / `path` |
| (inline) logging level / dir | `info` / `log` | `logging.level` / `rel_log_file_dir` |

### (a′) first-run seed defaults — set in `DefaultConfig`, NOT `applyDefaults`

Booleans where `false` is a legitimate operator choice, so they're seeded once at
first-run rather than re-applied every load (which would silently flip an explicit
`false`): `logging.with_timestamp`, `logging.file_logging`, `logging.log_file_compress`,
`smtp.starttls` (all default **true**). Also the disabled-but-prepopulated hamnut + QRZ
provider templates. **A known wart:** this split exists only because these fields aren't
`*bool` (the code comments flag converting them to pointers, mirroring `ServeSPA`).

### (b) serve/read-time resolve (`internal/types/ft8.go`)

| Resolver | Defaults |
|---|---|
| `ResolveFt8Display` | history_max 100 (clamp [10,2000]), feed_mode `accumulate`, highlight_unworked `#15803d`, highlight_worked `#9ca3af` |
| `ResolveFt8Frequencies` | `DefaultFt8Frequencies()` per band, operator overrides where `>0` |
| `ResolveFt8CallerAnswerMode` | `auto_first` |

These differ from (a): defaults are applied at **GET-serve / read time**, not at load.

### (c) nil-pointer-means-default (distinguishes "unset" from explicit `false`/`0`)

| Field | nil → | Knob |
|---|---|---|
| `Server.ServeSPA` (`*bool`) | `protocol=="tcp"` | `server.serve_spa` |
| `Ft8.EnableOSD` (`*bool`) | `true` | `ft8.enable_osd` |
| `Ft8.TX.Occupancy.GuardMarginHz` (`*int`) | guard on (default Hz); explicit `0` = guard off | `ft8.tx.occupancy.guard_margin_hz` |

### (d) safety-ceiling — const is a non-overridable MAX, not a default ⚠️

These must survive any redesign as ceilings; flattening them into ordinary defaults
would let config create an unsafe state.

| Ceiling | Where | Rule |
|---|---|---|
| Tune power ≤ 40 W | clamped at `bridge.Service` construction (`internal/bridge`) | const 20 W default, config `bridge.tune.power_w` clamped ≤ 40 |
| Tune duration ≤ 30 s | `bridge.Service` construction | const 15 s default, clamped ≤ 30 |
| Tune restore-settle ≤ 2 s | `bridge.Service` construction | const 150 ms default, clamped ≤ 2 s |
| FT8 TX hard auto-off (`ft8TxMaxDuration`) | `internal/bridge/ft8tx.go` | 18 s, not operator-overridable |
| Band-activity `history_max` clamp [10, 2000] | `ResolveFt8Display` | both a default *and* a clamp |
| Bridge timeout sane range [50 ms, 1 h] | `validateBridge` | rejects out-of-range as a typo guard |

## 5. Validation map

Three+1 distinct mechanisms — **and startup vs PUT validate through different code
paths** (a divergence, §7).

- **Startup-fatal** (`config.Load` aborts the daemon): JSON parse; `applyRigProfiles`
  (rig id > 0, unique, non-empty model, `default_rig_id` resolves); `validateForwarders`
  (name/type non-empty, unique names, action-filter enum, non-negative tick/batch, retry
  bounds); `validateLookup` (non-negative TTLs; per-enabled-provider URL + positive
  timeout; unique chain names; no collision with hamnut); `validateSmtp` (when enabled:
  host, from, port 1–65535, positive timeout); `validateBridge` (mode-mappings valid
  ADIF; timeout ranges; non-negative tune knobs; when enabled: port + driver). Then in
  `cmd/smd`: `Service.Initialize()` checks (bridge logger; ft8 logger + capture source).
- **PUT-400** (`internal/api/handler_config.go`, runtime, non-fatal): callsign (3–32 +
  digit), maidenhead (4/6/8 + lat/lon derivation), CQ zone [1–40], ITU zone [1–90], DXCC
  [0–522], amp multiplier [0–1000], default power [0–2000], bridge mode-mappings (driver
  known, valid ADIF, diffed to overrides), ft8_display feed-mode enum. **This is a
  separate validator from the startup ones — overlapping fields are checked in two
  places with no shared function.**
- **Soft-fail** (degrade, daemon continues): QRZ session-key fetch failure at provider
  `Initialize` → provider disabled + warning (`internal/lookup/qrz`).
- **Advisory** (`config.Warnings`, non-fatal, logged once): e.g. `protocol=tcp` with a
  non-loopback bind (no-auth exposure warning).

## 6. Lifecycle & dynamics

- **Read-once-at-construction (restart to change):** Server, Datastore, Logging, Bridge
  (`bridge.New(cfg.ActiveBridge())`), FT8 (`ft8.NewService(cfg.ActiveFt8())`), Forwarders,
  Enrichment providers, Mailer. Both `internal/bridge` and `internal/ft8` carry explicit
  comments: *"Config is read once and snapshotted; runtime PUT changes don't reach an
  existing Service. Operator restart picks up edits."*
- **Per-op (live):** `LoggingStation`, `Station`, `DefaultLogbookID`, `DefaultRigID`,
  `Bridge.ModeMappings`, enrichment TTL accessors — read via `Snapshot()` each request.
- **Effectively dynamic (no restart):** only SPA-consumed settings — `ft8_display`,
  `ft8_frequencies`, `station`, `logging_station` — because the SPA re-reads `configState`
  on every PUT response. This is dynamic *by accident of who consumes them*, not by design.
- **No reload mechanism exists.** `config.Service.Update` rewrites the whole file
  (atomic temp+rename via `WriteJSON`) and swaps the in-memory `Cfg`, but **notifies no
  subsystem**. There is no config-change event, watch, or re-read hook anywhere.
- **Service surface:** `Load`, `Snapshot` (value copy under RLock), `Update`
  (disk-then-memory), `UpdateInMemoryThenPersist` (memory-then-best-effort-disk),
  `Initialize`, `WriteJSON`, `Warnings`, `Active{Bridge,Ft8}`, `RigByID`.

## 7. Findings / divergences (observations only — no fixes here)

1. **Profile half-migration (ADR 0028).** Only `Model`/`Port`/`Audio.Device` were moved
   per-rig. Rig-specific fields keep landing in global blocks — `ft8.tx.mode` (`DATA-U`)
   is the live example; `ft8.tx.device` and arguably `bridge.timeouts`/`bridge.tune` are
   candidates. **There is no rule for where a new field goes**, so drift is the default.
2. **Loose-block + projection is transitional.** `bridge.serial.port`, `bridge.cat.driver`,
   `ft8.device` are dead-but-present on disk; `Active*()` always projects the active rig
   over them. They linger with no deprecation/removal plan, and `RigConfig.Overrides` is
   declared but unwired.
3. **`ft8.tx.mode` is rig-specific but global** — breaks the moment a non-Yaesu rig
   (the incoming IC-7300) is the active rig.
4. **Startup vs PUT validation divergence.** Overlapping fields (e.g. mode-mappings) are
   validated by two separate code paths with no shared validator — they can drift.
5. **No config version / migration story.** The only migration is the ad-hoc
   `applyRigProfiles` fold, re-run every load. No `version` field; no path for future
   schema moves (e.g. relocating a field per-rig) without bespoke code.
6. **config.json drifts from code.** Dev and dogfood files diverge from each other and
   lag the code; nothing keeps them in sync, and the whole-file-rewrite-on-PUT reorders
   fields and clobbers hand edits (hence the "don't edit config.json while the daemon
   runs" operating rule).
7. **Defaults are spread across four mechanisms** (`applyDefaults`, `DefaultConfig`,
   `Resolve*`, nil-pointer) plus safety-clamps — no single place answers "what's the
   default for X, and is it overridable?"
8. **Restart-only is the norm.** Every daemon-consumed setting needs a restart; only
   SPA-consumed ones are live, and only incidentally.

## 8. Inputs to the redesign (parked — undecided)

To be resolved in the redesign phase, not here:

- **Ownership taxonomy + the rule for a *new* field**: global / per-rig / SPA-display /
  session — and a single documented rule so drift can't recur.
- **Finish the per-rig move**: which fields move into `RigConfig` (starting with
  `ft8.tx.mode`); projection-overlay vs the active rig fully owning its sub-config;
  whether/how to delete the loose blocks; wire `RigConfig.Overrides`.
- **rigdef-derived defaults**: how much rig-specific config should *default from the
  rigdef* (operator stores only overrides), shrinking config.json.
- **Dynamic reload**: which settings hot-reload vs stay restart-only; the one
  notify→re-read mechanism that would make live ones live.
- **Validation unification**: one validator shared by startup and PUT; fatal-vs-degrade
  policy.
- **Defaults model**: one home for defaults; keep the safety-ceiling distinction explicit
  and non-overridable.
- **Versioning/migration**: a `version` field + a real migration path.
- **Persistence shape**: single file vs split; the whole-file-rewrite friction; whether
  any config truth should move to the DB.
- **Multi-rig / N-writer future** (`topology.md`): does the single-active `DefaultRigID`
  assumption hold.

**Guardrail for the redesign:** *build specific, not generic* — a clean, explicit shape,
not a generic settings framework (the v1 `adapters/` cautionary tale).
