# Config System — review + redesign

> **Status:** review complete; redesign **decided across §9–15** (2026-06-13);
> **implementation in progress.** §1–8 are the current-state review; §9–15 are the redesign
> decisions. Implementation so far: **§13 version/migration scaffold + §10 per-rig moves 2a–2d
> SHIPPED**, plus loose-block removal (`bridge.serial`/`cat` → `*omitempty`) + a canonicalising
> `Normalize` that drops rig overrides equal to the rigdef default (see §10.5; §10's 2e audio is
> deferred to the config-SPA workstream). **§12 validation
> unification SHIPPED** (§12a consolidation + §12b rig/field rules + the `Validate`/`Normalize` PUT
> rewire — see §12 status). **§13 versioning/migration scaffold SHIPPED** (§13.6). **§15 persistence
> shape SETTLED** — sparse-on-disk rejected; filled-on-disk kept, default-value drift handled by a
> §13 migration with the equals-old-default guard (no code — §15.2/§15.4). **§11 reload — decided,
> general OnChange/Reload mechanism GATED on the config-SPA write path** (see §11.5); one field,
> `ft8_max_repeats`, is live-applied today via a targeted setter (2026-07-03), not the general mechanism. **§14 defaults — §14a consolidation shipped; §14b `*T` fold stays
> deferred** (sparse was its only justification — see §14.5 / §15.2); §14's defaults-fill
> consolidation is the only remaining optional code item. Multi-rig / N-writer parked (§8).
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
| Forwarder destinations | config.json | `Config.Forwarders` | `enabled` gates enqueue (ADR 0039); non-sparse (seeded per registered type); per-action `endpoints` |
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
| `Forwarders[]` | D (forwarder workers) | start | — | One worker per **enabled** entry; disabled queues nothing (ADR 0039) |
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
| `Device` (capture) | D | start | **yes** | Resolved view; authoritative source `RigConfig.Audio.RX` (name), projected by `ActiveFt8()` |
| `EnableOSD` (`*bool`) | D | start | — | nil→true |
| `TX.Device` (playback) | D | start | **yes** (not yet projected) | Should follow `RigConfig.Audio` like capture |
| `TX.Mode` | D | start (read at arm via `txMode()`) | **YES — rig-specific but GLOBAL** | `"DATA-U"` is Yaesu vocabulary; the trigger for this review |
| `TX.CallerAnswerMode` | D | start (resolved) | — | `auto_first` / `operator_pick` |
| `TX.Occupancy.*` | D | start (per-slot read of snapshot) | — | Occupancy detector tuning |
| `FieldDay.{Class,Section}` | D (TX) | served; PUT-writable (presence-aware) | — | Operator's ARRL Field Day exchange — consumed by both FD paths (answer a CQ FD + work a caller in FD; ADR 0037, shipped). Empty = unset. Class strict (`^[1-9][0-9]?[A-F]$`, in `types`); Section checked against go-ft8's canonical ARRL/RAC list (`ValidARRLFieldDaySection`, in `internal/config` — `types` stays stdlib-only). Stored upper-cased |
| `FieldDay.DefaultRstRcvd` | D (TX) | served; PUT-writable (presence-aware) | — | RST_RCVD logged for an FD QSO (FD exchanges no report; some OQRS require it non-empty). Operator string (`59`/`599`/`-15`); empty = blank. Trimmed, NOT upper-cased. Applied in the `cmd/smd` e4 sink; RST_SENT is the measured SNR via `BuildQso` |
| `Display.*` (history/feed/colours) | SPA | served; PUT-writable (presence-aware) | — | Daemon stores, ignores; pure SPA prefs |
| `Frequencies` (band→Hz) | SPA | served (resolved) | — | Main-Freq buttons; PUT not yet wired |
| `DecodeLog.{Enabled,Path}` | D | start (read at capture acquire) | — | JTDX ALL.TXT decode log (RX + our TX); off by default; `Path` default `$SM_WORKING_DIR/log/ft8-all.txt`. Independent of log level. Detail in `docs/ft8.md` |

### `RigConfig` (`internal/types/rig.go`)

`ID`, `Model`, `Port`, `Audio.Device` (all per-rig), and `Overrides` (per-rig serial
params: `baud_rate`, `data_bits`, `stop_bits`, `parity`, `line_delimiter`,
`read_timeout_ms`, plus the **tri-state `rts`/`dtr`** added 2026-07-23 — `*bool`, so an
explicit `false` is a real setting and is NOT read as "unset"; omitted inherits the
rigdef, and every shipping rigdef now de-asserts both. These lines are a PTT source on
rigs that map one to them (Icom USB SEND, Yaesu PSK/DATA `RPTT SELECT`), so asserting one
on such a rig keys the transmitter for the life of the connection — see ADR 0057 and the
CAT chapter of the manual). The active rig's fields ARE projected at runtime: `ActiveBridge()` overlays
`Model`→`cat.driver`, `Port`, and `Overrides` onto the bridge serial/cat config, and
`ActiveFt8()` projects the active rig's `Audio.Device` (both pinned by tests). `Model` is
validated against the embedded `cat.Lookup` catalogue at Load/PUT (review 2026-06-19 M2),
so an unknown driver id is rejected at the config boundary, not at bridge startup.

### `QslDefaults` (`internal/types/qsl.go`, config key `qsl`)

Operator's standing outgoing-QSL defaults: `qsl_via` (QSL route/manager), `qslmsg`
(standing card/upload message), `qsl_sent_via` (default send method B/D/E/M). A SUBSET
of `types.Qsl` — the per-QSO confirmation status fields are deliberately excluded
(per-contact, not config). Stamped onto a logged QSO **only when the field is empty**
(per-QSO value wins) and **only when the default is non-empty**, so an unset default
never adds an empty ADIF tag. A single record-level stamp point — `adif.Record.ApplyQslDefaults`
— is called by **both** daemon logging paths (the Phone/CW submit handler and the FT8
e4 sink, after `QsoToRecord`); ADIF **import** is deliberately left alone. No SPA
involvement (it's pure config the daemon owns). Writable via `PUT /v1/config`
(presence-aware). A config-SPA editor is future work; set it by hand for now.

### `PskReporterConfig` (`internal/types/pskreporter.go`, config key `psk_reporter`)

FT8 reception-report upload to PSK Reporter. `enabled` (default **false** — opt-in,
publishes RX to a public service), `host`/`port` (default `report.pskreporter.info:4739`;
port `14739` on the same host is the test server — NOT `pskreporter.info`, which is the
Cloudflare website and drops UDP). Receiver identity — callsign, grid, **and antenna
(from `MY_ANTENNA`)** — comes from `LoggingStation`, not here. Read at startup → fed to `internal/pskreporter`;
**not on `/v1/config`** (set-once, like the SMTP block — a config-SPA surface can come
later). Detail in `docs/ft8.md`.

### `MapConfig` (`internal/types/mapconfig.go`, config key `map`)

Contacts-map display settings. `band_colors` maps lowercase ADIF band tokens
(`"20m"`, `"70cm"`) to `#rrggbb` arc colours, layered over the SPA's built-in
palette (`frontend/app/src/lib/map/bandColors.ts`). **Sparse:** an absent/empty
block (or band) means the default palette — defaults are never materialised into
config.json. Validated by `validateMap` (band-token key, 6-digit hex value).
Served RAW on `GET /v1/config` (no secrets), presence-aware on PUT (omit →
untouched; carry → replace the whole block). Applied client-side — a change
needs a map-page reload, not a daemon restart.

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
| (per-type, registry-seeded) `endpoints` / `action_filter` / `retry` | each forwarder package registers its defaults (`RegisterDefaultEndpoints` / `RegisterSupportedActions` / `RegisterDefaultRetry`) | `forwarders[].endpoints` (action-keyed URLs) / `action_filter` / `retry` (ADR 0039) |
| `defaultCountryTTLDays` / `StationTTLDays` | 365 / 90 | `lookup.country_ttl_days` / `station_ttl_days` — **`*int`: absent = default, explicit `0` = "never goes stale"** (see below) |
| `defaultRefreshMaxInFlight` | 4 | `lookup.refresh_max_in_flight` (plain int; `0` = package default in both the accessor and the defaults pass, so it has no absent-vs-zero conflict) |
| `defaultLookupHTTPTimeoutSec` | 10 | `lookup.hamnut.timeout_sec`, `lookup.chain[].timeout_sec` |
| `defaultSmtpPort` / `defaultSmtpTimeoutSec` | 587 / 30 | `smtp.port` / `smtp.timeout_sec` |
| (inline) protocol / socket | `tcp` / `127.0.0.1:8080` (tcp) or `$TMP/smd.sock` | `server.protocol` / `socket_path` |
| (inline) datastore driver / path | `sqlite` / `${DataDir}/db/station-manager.db` | `datastore.driver` / `path` |
| (inline) logging level / dir | `info` / `log` | `logging.level` / `rel_log_file_dir` |

### (a‴) lookup sources: `label`, and what a TTL of `0` means

`lookup.hamnut.label` / `lookup.chain[].label` (added 2026-08-03) is the exact
counterpart of the forwarder `label` documented below: the operator's own
display name for a source, settable **only by hand in config.json**, empty
meaning "use the name this build knows". It matters slightly more here, because
a source the build does not recognise has **no** built-in name and otherwise
displays its raw service id (`hamqth`); a label is the only way to give it a
readable one without shipping a new binary. Deliberately **not** `name` — that
is the key `mergeLookup` matches on to carry a provider's stored password across
a save, and the key `LookupServiceConfig` resolves at startup, so renaming it
silently detaches the credentials. **`mergeLookupProvider` carries `label` over
from the stored entry explicitly**, for exactly the reason spelled out for
forwarders below: the rebuild keeps only what it names, so an unrelated save
would otherwise delete it. Pinned by `internal/api/lookup_label_test.go` (M2/M3).

**The two cache TTLs are `*int` (changed 2026-08-03).** `nil`/absent means "use
the default" and is filled by `config.Normalize`; an explicit **`0` means "trust
this cache indefinitely"** — the reading `lookup.Orchestrator.isStale` has always
applied to a non-positive TTL. Before the change the field was a plain `int` and
`applyDefaults` stamped 365/90 over any zero, so an operator who set `0` got what
they asked for until the next restart and then silently got a year. The defaults
live in `Normalize` rather than `applyDefaults` so they apply on **PUT** as well
as Load (the same reason `normalizeLookupURLs` and `normalizeSmtpDefaults` are
there). A negative is still a validation error.

**An ENABLED source that needs credentials must have usable ones**, enforced at
save time by `validateLookupProvider`. Without that gate a PUT emptying or
shortening them returned 200 and the daemon then failed to **start** at the next
restart (`buildEnrichment`'s error aborts `run()`), hours later and with nothing
linking the two.

**Where a provider's facts come from: its own registration (ADR 0062).** Each
provider package's `init()` registers a descriptor in `internal/lookupdef` — a
true leaf — carrying its display name, help text, credential requirements and
endpoint defaults, plus a constructor in `internal/lookup`. `config` reads the
descriptors for URL/timeout defaulting and the credential rule; `cmd/smd`
blank-imports the provider packages to trigger registration and wires whatever is
registered. Adding a provider is a package and an import line, not an edit to
`buildEnrichment`, config's defaults, config's validator and the SPA.

Two consequences to know when reading config code:

- **An UNREGISTERED provider is left alone**, never defaulted to empty and never
  refused: the operator may be running config from a newer build, and refusing to
  load it would strand them on a daemon that will not start.
- **`internal/config`'s own unit tests see an EMPTY registry**, because they
  cannot import a provider package (`internal/lookup/qrz` imports
  `internal/config`). They register the descriptors they assume — which is why
  each test now states its providers instead of inheriting them from a hardcoded
  list.

### (a″) forwarders are non-sparse + config-driven (ADR 0039)

`applyDefaults` does an **add-missing-by-type** merge: for every registered
forwarder type (`forwarding.DefaultForwarderConfigs()`) the operator hasn't
configured, it appends a **disabled** seed entry (name = type, supported-action
`action_filter`, default `endpoints`). So the config is non-sparse — the operator
flips `enabled` + supplies credentials rather than hand-adding an entry. It runs
*after* the operator-file overlay (a JSON array replaces, not merges, so a base
seed wouldn't survive) and is a no-op when the registry is empty (config unit
tests that don't import the forwarder packages). `cfg.Forwarders = nil` stays the
base default.

`label` (added 2026-08-02) is the operator's own display name for a destination,
settable **only by hand in config.json** — no API surface writes it and the
Settings tab has no control for it. Empty means "use the type's built-in
`DisplayName`". It exists because that built-in is a string in the binary
(`smcloud.go`'s `"SM Cloud backup"`), so renaming it is a build + deploy the
operator cannot perform, and names date as a service grows. It is deliberately
**not** `name`: `name` is the durable key behind `qso_upload`'s
`UNIQUE (qso_id, forwarder_name, action)`, so renaming *that* would make the
daemon forget which QSOs had already been sent and re-upload them upstream.
Nothing joins on `label`.

**`label` and `endpoints` are both config-only, so `mergeForwarders` must carry
them over explicitly** (`handler_config.go`): a PUT rebuilds each entry from the
SPA payload and keeps only the fields it names, so anything absent from the wire
is deleted by an unrelated save. `endpoints` had exactly that defect until
2026-08-02 and it was silent — the save wrote the map out empty and
`applyDefaults` re-seeded the registry default at the next Load, replacing an
operator's override with the stock URL. Pinned by `internal/api/forwarder_label_test.go`
(L2/L3). **Any future config-only field on `ForwarderConfig` belongs in that
carry-over too.**

`endpoints` is an **action-keyed** map (`insert`/`update`/`delete` → URL) so
single-URL (QRZ) and per-action (ClubLog) forwarders share one shape; the value
lives in config (overridable without a recompile) with the package
`DefaultEndpoint` const as the fallback for any key left unset. `enabled` now
**gates enqueue** (disabled = don't queue; startup discards a disabled
forwarder's pending/failed rows) — superseding ADR 0022's presence-gating; see
ADR 0039.

The **ClubLog application API key is NOT a config credential** (ADR 0054). It is
build-injected — stamped into the binary via `-ldflags` from the gitignored
`.env` (`CLUBLOG_API_KEY`), never stored in `config.json`. A ClubLog forwarder's
`credentials` therefore hold only `email`/`password`/`callsign`; a legacy `api`
left in an existing config is **scrubbed from disk at startup**, but only when
the running build actually has a baked key to replace it (a keyless build must
not delete the operator's only usable key).

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
| `ResolveFt8Display` | history_max 100 (clamp [10,2000]), feed_mode `accumulate`, highlight_unworked `#15803d`, highlight_worked `#9ca3af`, highlight_calling `#b45309` (the three `highlight_*` are **vestigial** as of 2026-08-05 — resolved and round-tripped, read by nothing; see `docs/ft8.md`) |
| `ResolveFt8Frequencies` | `DefaultFt8Frequencies()` per band, operator overrides where `>0` |
| `ResolveFt8CallerAnswerMode` | `auto_first` |

These differ from (a): defaults are applied at **GET-serve / read time**, not at load.

### (c) nil-pointer-means-default (distinguishes "unset" from explicit `false`/`0`)

| Field | nil → | Knob |
|---|---|---|
| `Server.ServeSPA` (`*bool`) | `protocol=="tcp"` | `server.serve_spa` |
| `Ft8.EnableOSD` (`*bool`) | `true` | `ft8.enable_osd` |
| `Ft8.TX.Occupancy.GuardMarginHz` (`*int`) | guard on (default Hz); explicit `0` = guard off | `ft8.tx.occupancy.guard_margin_hz` |

`Ft8.TX.InhibitIdle` (`*bool`, `ft8.tx.inhibit_idle`, nil → `true`) is a
**resolver**, not an `applyDefaults` entry — `ActiveFt8()` leaves the whole `TX`
block nil when there is no `ft8.tx`, no `ft8.tx.mode` and no rig TX-audio device,
and a default cannot be written into a block that does not exist. Readers go
through `types.ResolveFt8InhibitIdle`, which treats an absent BLOCK and an absent
FIELD alike. Same reason `ft8.tx.max_repeats` resolves rather than defaults.

**`ft8.audio` (`Ft8AudioConfig`, both fields `*float64`, resolver
`types.ResolveFt8Audio`, added 2026-08-06):** the RX audio-level meter's
classification window in dBFS — `low_dbfs` (default −60; below = too quiet to
decode reliably) and `high_dbfs` (default −10; above = running hot). Served
RESOLVED on GET as `ft8_audio`; **read-only over `/v1/config`** (like
`ft8_frequencies` — a PUT never carries it, calibration is a config.json edit +
restart). Validation checks the RESOLVED pair (a lone override can invert the
window against a default): both within [−120, 0], low strictly below high.
Defaults are WSJT-X-convention starting points awaiting hardware calibration
against the operator's PCM2903C. The daemon publishes raw measurements
(`ft8-audio-level` on `/v1/ft8/events`, ~4 Hz, peak+RMS dBFS); the SPA
classifies. Clipping is pinned SPA-side at a fixed near-0 dBFS peak check, not
configured here.

### (d) safety-ceiling — const is a non-overridable MAX, not a default ⚠️

These must survive any redesign as ceilings; flattening them into ordinary defaults
would let config create an unsafe state.

| Ceiling | Where | Rule |
|---|---|---|
| Tune power ≤ 40 W | clamped at `bridge.Service` construction (`internal/bridge`) | const 20 W default, config `bridge.tune.power_w` clamped ≤ 40 |
| Tune duration ≤ 30 s | `bridge.Service` construction | const 15 s default, clamped ≤ 30 |
| Tune restore-settle ≤ 2 s | `bridge.Service` construction | const 150 ms default, clamped ≤ 2 s |
| FT8 TX hard auto-off (`ft8TxMaxDuration`) | `internal/bridge/ft8tx.go` | 18 s, not operator-overridable |
| FT8 sequencer repeat cap ≤ 10 (`Ft8MaxRepeatsCeiling`) | `types.ResolveFt8MaxRepeats` | const 6 default, config `ft8.tx.max_repeats` clamped ≤ 10; surfaced on `/v1/config` as `ft8_max_repeats` (logging FT8 Settings tab) + **applied live** via `Service.SetMaxRepeats` (§11.5) |
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
  [0–522], amp multiplier [0–1000], default power [0–2000], `station.operating_bands`
  (each a known band per `enums/bands`, no duplicates; empty = "all bands" — the SPA
  defaults its band selector to HF..6m), bridge mode-mappings (driver known, valid ADIF,
  diffed to overrides), ft8_display feed-mode enum. **This is a separate validator from
  the startup ones — overlapping fields are checked in two places with no shared
  function.** `validateStationPrefs` runs on BOTH paths (startup + PUT).
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

- **Ownership taxonomy + the rule for a *new* field** — ✅ **DECIDED 2026-06-13; see §9.**
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
  assumption hold. ⏸ **PARKED 2026-06-13** — no current driver; only becomes real with
  two-radios-at-once (SO2R) or multi-station contesting. §9–15 deliberately assume single-active
  (one operator, one active rig, one daemon). Revisit if that changes.

**Guardrail for the redesign:** *build specific, not generic* — a clean, explicit shape,
not a generic settings framework (the v1 `adapters/` cautionary tale).

## 9. Redesign — ownership taxonomy (decided 2026-06-13)

The drift-killer: a fixed set of scopes **plus a placement rule** applied to every config
value, new or existing. Resolves §7 finding #1 ("no rule for where a new field goes").

### 9.1 The seven scopes

| Scope | What it is | Authority / home | Mutable? |
|---|---|---|---|
| **A — rig capability** | what a rig *model* can do + its per-model defaults | rigdef `internal/cat/rigs/*.json` | immutable (embedded) |
| **B — per-rig config** | operator config that varies with the active rig | `RigConfig` (catalogue), per instance | operator |
| **C — global daemon config** | one value per daemon, rig-independent | config.json top-level | operator |
| **D — operator/station identity** | who the operator/station is | config.json `logging_station` / `station` | operator |
| **E — SPA presentation prefs** | daemon stores, never reads | config.json (served to the SPA) | operator (via SPA) |
| **F — session/operating state** | ephemeral runtime state | in-memory / localStorage — **never config.json** | n/a |
| **G — entity / derived data** | rows + caches | the **DB** — **never config.json** | per-feature |

### 9.2 Inside B — split by *default source*, not storage

Everything in B is stored **per rig instance, inside that rig's `RigConfig` block** —
self-contained, no per-model side tables (see 9.4). The B1/B2 split is only about whether
a sensible default exists:

- **B1 — per-instance, must-set** (no default; hardware/cabling-specific): serial **port**,
  **audio device(s)** (capture + TX playback).
- **B2 — per-model, rigdef-default + optional override**: FT8 **data-mode literal**, serial
  **param overrides**, **mode-mappings**. Resolution: `rig.Model` → rigdef per-model default
  → apply this rig's override; the **merged** value is authoritative.

**`mode_mappings` is the reference implementation of B2 today** (config SPA Rigs tab →
Mode Mappings; rigdef ships defaults; only operator deltas persist; merged at `/v1/config` GET).
`ft8.tx.mode` and the serial overrides simply adopt the same pattern.

### 9.3 The placement rule (apply to any field; first match wins)

1. *What the rig model can do* → **A** (rigdef; not operator config).
2. *Describes the rig model* (e.g. `MY_RIG`) → **derive from the rigdef at log time; don't store.**
3. *Value depends on the active rig* → **B** — no sensible default (hardware) → **B1**;
   the rigdef can default it per model → **B2**.
4. *Who the operator/station is* → **D**.
5. *Daemon reads it to change behaviour, one value daemon-wide* → **C**.
6. *Only the SPA reads it* → **E**.
7. *Ephemeral operating state* → **F** (never config.json).
8. *Row / derived data* → **G** (the DB).

**Drift-killer:** if the value would differ on a different rig, it is **B** — never a
global block.

### 9.4 Principles

- **Independently-varying equipment is its own axis, not per-rig.** Rig↔antenna is **N:M**
  (e.g. 2 rigs, 1 shared antenna; one-to-many / many-to-one / many-to-many / one-to-one all
  occur). So **`MY_ANTENNA` is NOT per-rig** — it's a plain operator-set free-text identity
  field (**D**), commonly **blank** (operators often exchange equipment verbally and note it
  in the comment field, not `MY_ANTENNA`); **never required, never derived.** A future
  multi-antenna + switch setup would add its own "active antenna" selector (mirroring rigs),
  orthogonal to the rig — deferred (operator has one antenna).
- **Don't normalize config across rigs to avoid repeated values.** Two same-model rigs are
  just two rigs; coinciding params are coincidence, not shared state. No per-model side
  tables — each `RigConfig` is self-contained.
- **Derive what the rigdef knows — as an overridable default.** `MY_RIG` *defaults* to the
  active rig's rigdef `name` (follows the QSO's rig automatically), but the operator can
  override it **per rig** (`RigConfig.MyRig *string`: nil = derive the name, `""` = suppress /
  don't publish the rig, a value = publish it verbatim). It's just another **B2** field, not a
  special derive-only case. **General rule:** the single submit-time injection point (§10.4 #2)
  stamps *derived defaults*, never forced values — every such field stays operator-overridable,
  with explicit-blank meaning "suppress."

### 9.5 Implications (consequences of the taxonomy — sequenced in the "finish the per-rig move" + "versioning/migration" topics, NOT actioned here)

- `ft8.tx.mode` → **B2** inside `RigConfig`; the rigdef declares the per-model data-mode
  default (a new rigdef field). Fixes the `DATA-U`-is-global bug + the incoming IC-7300.
- `bridge.mode_mappings` → fold into `RigConfig` (per instance); drop the top-level
  driver-keyed block.
- `RigConfig.Overrides` (serial) → **B2**; wire it (declared-but-unused today).
- `ft8.tx.device` → **B1** (per instance), resolved like the capture device.
- Loose `bridge.serial` / `bridge.cat` + `ft8.device` → removed once the fields fully live
  in `RigConfig` (ends the transitional projection).
- `MY_RIG` → per-rig `RigConfig.MyRig *string` override over a rigdef-`Name` default (B2),
  stamped at the single submit-time injection point; drop `logging_station.MyRig`.

## 10. Redesign — finish the per-rig move (design — decided 2026-06-13; implementation pending)

The mechanism for the §9.5 implications: how each B-scope field physically moves into
`RigConfig`, how it resolves (rigdef default ← per-rig override), and how existing config
migrates. All four §10.4 sub-decisions are now decided (2026-06-13); implementation pending.

### 10.1 Resolution model (unchanged shape, new source)

The single resolution point stays `Config.ActiveBridge()` / `Config.ActiveFt8()` (plus
`cat.Lookup(model)` for rigdef defaults): **rigdef(model) defaults ← active `RigConfig`
overrides → projected `BridgeConfig` / `Ft8Config`.** Every subsystem keeps reading the
projected config exactly as today (`bridge.New(cfg.ActiveBridge())`,
`ft8.NewService(cfg.ActiveFt8())`, the TX controller's `txMode()`); only the *source* of the
projected values changes. Rigdefs stay the immutable defaults layer (A); `RigConfig` holds only
B1 (must-set) + B2 (overrides).

### 10.2 What moves (per B-scope field)

| Field | Today | New home | Resolution |
|---|---|---|---|
| FT8 data-mode (`ft8.tx.mode`) | global `Ft8TXConfig.Mode` | new rigdef `RigDefinition.Ft8Mode` default + optional `RigConfig.Ft8Mode` | `ActiveFt8()`: `rc.Ft8Mode → else rigdef(rc.Model).Ft8Mode` → projected onto `Ft8Config.TX.Mode` |
| mode-mappings | global `bridge.ModeMappings[driver][lit]` | `RigConfig.ModeMappings[lit]` (driver key dropped — the rig knows its model) | `bridgeInfoFor`: `rigdef(rc.Model).ModeMappings` ← `rc.ModeMappings` (same merge, new source) |
| serial overrides | `RigConfig.Overrides` (declared, **unwired**) | wire it | `buildSerialConfig`: `rigdef.Serial` ← `rc.Overrides` (zero field = rigdef default) |
| FT8 audio (capture + TX playback) | capture `RigConfig.Audio.Device` (index) + global TX `Ft8TXConfig.Device` | **per-direction name-based** `RigConfig.Audio.{rx, tx}` (rev. 2026-06-16) | `ActiveFt8()` projects `audio.rx → Ft8Config.Device` (capture) + `audio.tx → Ft8Config.TX.Device` (playback); the audio layer resolves each name → live index at acquire time |
| `MY_RIG` | stored `logging_station.MyRig` | per-rig `RigConfig.MyRig *string` override over a rigdef-`Name` default (B2) | stamped at submit (single injection point): `rc.MyRig != nil ? *rc.MyRig : rigdef(rc.Model).Name` — nil=derive, `""`=suppress |

Once these live in `RigConfig`, the transitional loose globals are removed: `bridge.serial` /
`bridge.cat`, `ft8.device`, `ft8.tx.mode`, `bridge.mode_mappings`. `Active*()` then resolve
purely from the active `RigConfig` + rigdef. The rigdef already maps `DATA-U`/`DATA-L` →
`{mode: FT8}`; `Ft8Mode` just names *which* literal is the default FT8 operating mode (a flat
add to `cat.RigDefinition` + the `rigs/*.json` files).

### 10.3 Migration

Extend `applyRigProfiles` (already folds loose `bridge`/`ft8` identity into the id-1 rig) to
also fold: global `ft8.tx.mode` → that rig's `Ft8Mode`; global `bridge.mode_mappings[driver]` →
the matching rig's `ModeMappings` (by model); drop `logging_station.MyRig` (now derived).
In-memory, idempotent. (config.json isn't authoritative and the operator reloads the DB from a
QRZ export, so this is low-stakes — but it keeps existing config.json files loading cleanly.)

### 10.4 Open sub-decisions (pending operator confirmation)

1. **Audio device identification** — ✅ **DECIDED 2026-06-13: name-based** (device names, not
   indices, because an index drifts across replug/reboot and differs between a codec's capture and
   playback enumerations). ✏️ **REVISED 2026-06-16: per-direction, two fields**
   `RigConfig.Audio.{rx, tx}` (each a device **name**), superseding the original single
   `RigConfig.Audio.Device`. *Why the revision:* the single-field model assumed one name resolves to
   both endpoints — true when the same codec enumerates under an identical name in both lists, but
   not guaranteed (a rig may use genuinely different devices for RX and TX, or differing names), and
   the single field was never wired for playback. Per-direction is explicit, robust, and only costs
   the operator a second pick (the config-SPA can auto-fill both from one choice for the common
   single-codec case). The borrowed IC-7300 motivated it: its USB codec `"PCM2901 Audio Codec Analog
   Stereo"` is capture index **4** / playback index **2** — same name, two indices — which is
   exactly why names beat indices, and why each direction resolves independently.
   - **Resolution:** `Config.ActiveFt8()` projects `audio.rx → Ft8Config.Device` (capture) and
     `audio.tx → Ft8Config.TX.Device` (playback); the audio layer (`internal/audio/{capture,
     playback}`, via a `DeviceName` config field) matches the name against the live enumeration at
     **acquire time** (survives replug) and fails soft — no match → that direction goes idle rather
     than grabbing the wrong system default. An integer-string value is still honoured as a raw
     index for any un-migrated config.
   - **The global `ft8.device` / `ft8.tx.device` are dropped as operator knobs** — device selection
     is now purely a per-rig property, so switching `default_rig_id` re-binds audio along with the
     CAT port + driver. `Ft8Config.Device` / `Ft8Config.TX.Device` survive only as resolved
     runtime-view fields `ActiveFt8()` fills (the FT8 subsystem keeps consuming a plain
     `Ft8Config` — it never imports the rig catalogue), mirroring `ActiveBridge()`.
   - **No index→name auto-migration** (the loader can't safely enumerate devices), so an existing
     index config's `ft8.device`/`ft8.tx.device` are simply dropped and each rig's `audio.rx`/`tx`
     are set once by name. Trivial here (single dev/dogfood host).
   - **The earlier interim `ActiveFt8()` clobber bug** (it overwrote a loose `ft8.device` with the
     active rig's *empty* audio device, zeroing it to the system default) was fixed 2026-06-15.
   - ✅ **Daemon side SHIPPED + live-validated 2026-06-16** (`RigAudioConfig.{RX,TX}`, `ActiveFt8`
     per-direction projection, `capture.Config`/`playback.Config` `DeviceName` name resolution).
     Live RX decode confirmed on dev: the daemon matched `"PCM2901 Audio Codec Analog Stereo"` →
     IC-7300 capture device and decodes landed. ⏸ **The device-by-name picker UI is still DEFERRED to the config-SPA workstream** —
     its rightful home — but the operator no longer hand-edits indices: they hand-edit *names* now,
     and the SPA will replace that with a pick-list off `GET /v1/hardware`.
2. **`MY_RIG` derivation point** — ✅ **DECIDED 2026-06-13: daemon-side at QSO submit**, a
   single injection point both the phone/CW (handler) and FT8 (sink) submit paths flow through
   (via an injected resolver, so `qsoservice` stays decoupled from `cat`/`config`). It stamps a
   *derived default* (rigdef `Name`) that the per-rig `RigConfig.MyRig *string` override can
   replace or suppress (§9.4). The principle generalises to any future derived-but-overridable
   identity field.
3. **Rigdef field name** — ✅ **DECIDED 2026-06-13: `ft8_mode`** (`RigDefinition.Ft8Mode`,
   `json:"ft8_mode"`). Specific to its FT8 consumers and unambiguous vs other data literals;
   generalise only if a future FT4/RTTY feature actually reuses it (build specific, not generic).
4. **mode-mappings key** — ✅ **DECIDED 2026-06-13: key by rig literal, drop the driver key.**
   `RigConfig.ModeMappings map[string]types.ModeMapping`; the rig's `Model` already *is* the
   driver. Merge becomes `rigdef(rc.Model).ModeMappings` ← `rc.ModeMappings`.

### 10.5 Implementation status (2026-06-13)

**SHIPPED** (behind the §13 version/migration scaffold), one slice each:
- **2a** — rigdef `Ft8Mode` + per-rig `RigConfig.Ft8Mode` override + `ActiveFt8` projection onto
  `Ft8Config.TX.Mode`; legacy global `ft8.tx.mode` folded typed (`migrateGlobalFt8Mode`).
- **2b** — `RigConfig.ModeMappings` (keyed by rig literal); **global `bridge.ModeMappings`
  removed**; first **raw `v1→v2` migration**; per-rig ADIF validation; `bridgeInfoFor` + the
  PUT handler retargeted to the active rig. (+ an api round-trip test plugging the prior gap.)
- **2c** — `RigConfig.Overrides` wired into `buildSerialConfig` via an `ActiveBridge` projection.
- **2d** — `RigConfig.MyRig` + `Config.ResolveMyRig` + submit-time stamp in `qsoservice.Submit`
  (reuses the existing `Config` dependency — no separate injected resolver; `SubmitImport`
  preserves imported `MY_RIG`); legacy `logging_station.my_rig` folded typed.

**SHIPPED — loose-block removal + canonicalising Normalize (2026-06-13):**
- **`bridge.serial` / `bridge.cat` dropped.** `BridgeConfig.Serial`/`Cat` are now
  `*pointer,omitempty`: the **stored** config carries `nil` (so empty `"serial": {}` / `"cat": {}`
  no longer persist), and `ActiveBridge()` builds a fresh, always-non-nil runtime projection of the
  active rig (no aliasing of stored state; callers deref freely). The legacy-synthesis fold reads
  them nil-guarded.
- **`Normalize` now canonicalises to "only what differs":** it nils any per-rig `Ft8Mode`/`MyRig`
  override that merely restates the rigdef default (`normalizeRigOverrides`, via `cat.Lookup`) and
  nils empty loose `serial`/`cat` blocks (a non-nil pointer to a zero struct would re-serialize as
  `{}`). Runs on **every** Load + PUT, so configs stay clean, not just at migration.
- **`migrateGlobalMyRig` no longer misattributes a stock name:** the legacy global `my_rig` is
  folded onto the active rig **only when it's a genuinely custom string**. If it equals the rigdef
  name of *any* catalogue rig (`rigNamed`), it's just stock identity that derives per-rig — so it's
  dropped rather than stamped onto the active rig, which may be a different model. (Caught when a
  v1 copy with `default_rig_id` pointing at one model carried a global `my_rig` naming another:
  e.g. active = FTdx10 but `my_rig: "Yaesu FT-710"` was folding "Yaesu FT-710" onto the FTdx10.)
  `normalizeRigOverrides` only strips a rig's *own*-name match; the cross-catalogue case needs this
  fold-level guard. Verified end
  to end on the dev `build/config.json`: both rigs reduce to `{id, model, port}` while
  `ResolveMyRig` / `ActiveFt8().TX.Mode` still derive correctly, and `bridge` reduces to
  `{enabled, timeouts, tune}`.

**SHIPPED 2026-06-16 — 2e name-based audio device (§10.4 #1), daemon side:** per-direction
`RigConfig.Audio.{rx,tx}` (names) replace the single index field; `ActiveFt8()` projects both
directions; `internal/audio/{capture,playback}` resolve name→index at acquire (fail-soft); the
global `ft8.device`/`ft8.tx.device` operator knobs are **dropped**. Only the device-by-name
**picker UI** remains deferred to the config-SPA workstream (until then the operator hand-edits
`audio.rx`/`audio.tx` names directly). The logging SPA's My Station **"Rig" field is inert**
(MY_RIG derived) — remove it when the per-rig editor lands. `bridge.tune` `{}` is a *legitimate* defaults block (tune knobs are code-constant
ceilings, ADR 0027), not vestigial — kept.

### 10.6 Editing surface — dedicated config SPA (direction; separate workstream)

Operator direction (2026-06-13): **set-once config moves to a dedicated config SPA, separate
from the logging SPA.** Things configured once and rarely/never changed are UI *noise* in the
live logging client — examples called out: **per-rig mode-mappings** (moved to the config SPA's
Rigs tab 2026-06-25; removed from the logging SPA) and the **FT8 Band Activity colour-coding /
display prefs** (`ft8_display`: highlight
colours, row cap, feed mode). These belong in the config SPA; the logging SPA keeps only what's
needed *during* logging (QSO form, live rig state, session, operationally-relevant identity).

Consistent with ADR 0001 (three clients: logging / logbook / config) and the
logging-vs-logbook scope rule. The config SPA itself is a **separate design/build workstream**
(`frontend-spa.md` / a future ADR), **not** part of this config *data-model* redesign — but the
redesign assumes config editing lands there, so the per-rig mode-mappings editor and the FT8
display-prefs editor target the **config SPA**, not the logging SPA.

**Status (2026-06-24):** the config SPA is now a **category-tab shell** —
`Station · Rigs · FT8 · Forwarding · Email · Enrichment`. The shell + the
**Station tab** (the set-once `LoggingStation` MY_* fields — zones/DXCC/country,
postal address, altitude, antenna, CW) shipped; the rest are placeholders. Full
design (shell, the operational-vs-set-once Station split, the Rigs master-detail
editor + its recommended write path) lives in
[`frontend-spa.md`](frontend-spa.md) → "Config SPA — design". The logging-side
removal of the moved Station fields is **Phase 2**, deferred until the config SPA
is live.

## 11. Redesign — dynamic reload (decided 2026-06-13)

Resolves the §8 "dynamic reload" input + the operator's no-restart-friction concern. Three
active reload classes + one mechanism; rig-hot-swap parked.

### 11.1 Reload classes

| Class | Goes live by | Examples |
|---|---|---|
| **Live value** (stateless / per-request) | already live — read `Snapshot()` at use | identity, station, default IDs, TTLs |
| **SPA-consumed** | already live — SPA re-reads `configState` on the PUT response | mode-mappings, ft8 display / frequencies |
| **Hot-reloadable** (lightweight, no hardware) | injected `OnChange` → subsystem `Reload(snapshot)` | ft8 value (`ft8_mode` / occupancy / `enable_osd`), enrichment providers, forwarder workers, mailer creds |
| **Restart-only** | restart; PUT persists + surfaces a "restart required" hint | infrastructure (HTTP listener, DB handle, log sink) **+ rig-hardware bindings** (serial port/driver, audio device, `DefaultRigID` / rig-swap) |

**Rule:** hot-reload a value unless it's bound to a process-level *or* rig-hardware resource
acquired at boot — those are restart-only (the rig-hardware ones until rig-hot-swap is
unparked, §11.4).

### 11.2 Mechanism

`config.Service` gains an injected `OnChange(func(old, new Config))` hook (set by `cmd/smd`,
the `SetQsoLogger` DI pattern), fired after a successful `Update` / `UpdateInMemoryThenPersist`
— covering PUT, startup self-heal, and future config-SPA edits, since all writers go through
those. The composition root dispatches the new snapshot to each hot-reloadable subsystem's
**idempotent `Reload(snapshot)`**:

- **ft8** — refresh cached cfg; `ft8_mode` (next arm), occupancy (next slot), `enable_osd`
  (next decode) pick up the new values. **No device re-acquire** (parked).
- **enrichment** — rebuild provider clients/sessions when creds / URL / enable changed.
- **forwarders** — start / stop / restart workers for the changed set.
- **mailer** — refresh SMTP creds.

`Reload` with an unchanged relevant slice is a no-op. **Not** a generic config-event bus —
explicit DI wiring in the composition root (build specific; `events.Hub` stays for SSE client
fan-out, not internal control flow).

### 11.3 Fail-soft + "restart required"

Reload runs *after* the config is validated + persisted. A reload that can't apply logs +
degrades — it never crashes the daemon or rolls back the saved config (same spirit as
"enrichment never blocks logging"). For **restart-only** fields the daemon persists the change
but does not apply it live; it surfaces a **"restart required"** hint (a flag on the
`/v1/config` response / a toast) so the operator knows the PUT took but won't take effect until
restart.

### 11.4 Parked — rig-hot-swap

Changing the active rig at runtime — a `DefaultRigID` swap plus the rig-hardware re-acquire it
implies (close+reopen the serial pipeline, release+re-acquire the audio device, re-resolve
`Active*()`) — is **parked**: a "could be nice," not a current need. Until unparked, switching
rigs or editing the active rig's port / driver / audio device is **restart-only**. When it
lands it reuses the bridge supervisor's close+reopen (ADR 0020) + ft8's demand-driven
acquire/release, and closes ADR 0028's deferred "runtime hot-swap" item.

### 11.5 Status

Design decided; **implementation gated on the config-SPA write path (deferred 2026-06-13).**

When the implementation was scoped, every runtime config-write path was traced: `PUT /v1/config`
writes only `LoggingStation` / `Station` / `ft8_display` / active-rig `mode_mappings` (all
**already-live** classes 1–2 — read-at-use or SPA-re-read-on-response, needing no reload), and the
two `cmd/smd` startup writes (`UserAgent`, default-logbook self-heal) fire before subsystems exist.
**Almost no runtime writer touches a class-3 (hot-reloadable) field** — forwarder config, SMTP creds,
enrichment/lookup, `ft8.enabled`/`device`/`ft8_mode` are all edited the "stop → edit config.json →
restart" way. So building `OnChange` + the four `Reload`s now would be the mechanism *ahead of its
trigger*: the hook would fire on every My Station save and every subsystem `Reload` would no-op,
and it couldn't be tested end-to-end (no runtime writer to exercise it).

**The one exception (2026-07-03): `ft8_max_repeats`** — the FT8 sequencer's unanswered-rung repeat
cap — is applied live by a **targeted `s.ft8.SetMaxRepeats` call in the `PUT /v1/config` handler**
(the sequencer's own `mu` makes the write safe mid-contact), NOT via the general `OnChange`/`Reload`
mechanism, which stays deferred. It's a bespoke live field because the operator needs to dial the
cap down *mid-pile-up* to stop wasting slots on a dead contact — a per-QSO operating adjustment that
doesn't justify standing up the whole reload machinery. If a second such field appears, that's the
signal to revisit the general mechanism.

The trigger is the **config-SPA** (§10.6, separate workstream): that's what makes
forwarder/mailer/enrichment/rig-hardware editable at runtime. §11 lands *with* it — editor and
reload-on-edit built and tested as one unit, with `Reload` semantics informed by the real editor.
Until then those fields stay restart-only (the "restart required" hint, §11.3, is likewise moot:
no PUT-writable field is restart-only today). The design above stands; only the build is deferred.

## 12. Redesign — validation unification (decided 2026-06-13)

Resolves the §8 "validation unification" input + the §5 / finding #4 startup-vs-PUT divergence.

### 12.1 The problem

Validation is fragmented three ways: **load-only** (fatal — forwarders, lookup, smtp, bridge,
rig catalogue); **PUT-only** (400 — callsign, grid, zones, amp, power, ft8_display — *not*
checked at Load, so a hand-edited bad value loads silently but a PUT of it is rejected); and
**both via separate code** (bridge mode-mappings — validated in `validateBridge` *and* the
handler, two impls that can drift).

### 12.2 One validator, caller decides disposition

A single **`config.Validate(cfg Config) []Finding`** in `internal/config` — the one source of
truth for all config rules, consolidating `validateForwarders/Lookup/Smtp/Bridge` + the
handler's field checks. **Pure** (no mutation). Run by **Load**, **PUT**, and future
**config-SPA** writes. Each `Finding` is `{field, code, message}` (ADR 0010 code+i18n pattern),
severity **error | warning**. Post-§10 it also covers the per-rig shape (each `RigConfig`'s
mode-mappings, `ft8_mode`, overrides). Disposition is the caller's:

- **Load:** any error → **fatal abort** (clear message).
- **PUT / config-SPA:** errors → **400** (field+code; SPA renders via i18n); else persist.
- **Warnings** (advisory — e.g. non-loopback bind): logged at Load, returned in the PUT
  response, never block.

### 12.3 Normalize separated from validate

The transforms — `applyDefaults`, callsign-uppercase, maidenhead→lat/lon derivation,
mode-mapping diff-to-overrides — are a distinct `normalize` step that runs **before**
`Validate`, shared by both paths. Pipeline: **parse → normalize → validate → persist**,
identical at startup and runtime. Side-effecting bits (the setup-complete→seed-logbook
transition, `DefaultLogbookID` resolution) stay in the handler — they aren't validation.

### 12.4 Behavioral change (confirmed)

Load now enforces the field rules that today only PUT enforces → a hand-edited malformed
callsign / zone / power becomes a **fatal startup error** (clear, names the field) instead of
loading silently. This just extends today's already-fatal *structural* validation to the
*field* rules, and kills "daemon runs with config a PUT would reject" (no silent bad data).
Pre-setup empties stay valid (fresh installs unaffected); fits the "edit config.json while
stopped, restart" workflow — a bad edit fails the restart with a clear message.

### 12.5 Ordering with reload (§11)

`validate → Update (persist) → OnChange → Reload` — a subsystem never sees unvalidated config.

### 12.6 Status

**SHIPPED (2026-06-13).** Landed in slices:

- **§12a** — `config.Validate(cfg) []Finding` consolidating the four standalone validators
  (forwarders, lookup, smtp, bridge) + the non-loopback-bind advisory; `Finding{Field, Code,
  Message, Warning}`; `Load` routes through it (errors → fatal). `internal/config/validate.go`.
- **§12b-1** — rig-catalogue + per-rig mode-mapping validation moved out of `applyRigProfiles`
  (now fold-only) into `validateRigs`.
- **§12b-2** — the operator-identity field rules (callsign, grid, zones, amp/power, ft8_display
  feed-mode) folded into `Validate`; an exported `config.Normalize` (uppercase callsign,
  maidenhead→lat/lon derive, trim zones) run before `Validate` by both paths; the PUT handler
  rewired to **build a candidate → `Normalize` → `Validate` (whole-config) → 400 on first error
  finding → persist via `*cfg = candidate`**, dropping all its inline checks/transforms. The
  config callsign rule is reimplemented in `internal/config` (can't import `qsoservice` — cycle;
  api keeps its own `isValidCallsign` for the QSO/logbook surfaces); `api.isValidZone` retired
  (config-only) and the api bounds-const block removed.

Two behavioral consequences confirmed and accepted: an **invalid `ft8_display.feed_mode` is a
400** (the candidate stores the *raw* feed_mode so `Validate` rejects it; `ResolveFt8Display`
runs only after validation passes — option A); and **PUT validates the whole candidate**, so a
PUT that touches one field is rejected if the config carries an unrelated invalid bridge (an
enabled-but-portless bridge couldn't `Load` anyway). The clamp-with-warning ceiling enforcement
(§14.2) is the one piece of `Normalize` not yet wired — it lands with §14.

## 13. Redesign — versioning & migration (decided 2026-06-13)

Resolves the §8 "versioning/migration" input. It's the partner to §10: the per-rig field moves
*are* a schema migration, and config.json persists operator **intent** (callsign, rig catalogue,
provider creds, SMTP) that is **not** in the DB and **not** reconstructable from a QRZ export —
so it must survive schema changes. Today there's no `version` field; the only migration is the
ad-hoc `applyRigProfiles` shape-detect fold, which gets fragile as migrations stack.

### 13.1 Version field + ordered migrations

- **`Config.Version int`** (`json:"version"`) + a `currentConfigVersion` const in
  `internal/config`.
- **Ordered migration registry** — each step `vN → vN+1`, run from the file's version up to
  current. A version-less file = **v0** (baseline). The current `applyRigProfiles` loose→catalogue
  fold becomes the **v0→v1** migration; §10's field moves become the next one. Plain ordered
  `[]migration`, each a small explicit transform — **not** a generic schema-diff engine
  (build specific).

### 13.2 Raw-document migrations (decided)

Migrations operate on the **raw JSON document** (map / `json.RawMessage`), **not** the typed
`Config` struct. A migration reads old keys (`ft8.tx.mode`, `bridge.mode_mappings`, `MyRig`,
loose `bridge.serial`/`ft8.device`) directly from the document *before* they're gone, so the
typed `Config` can **cleanly drop** those fields — this is what makes §10's removals real. Cost:
migrations are written against maps (a bit more verbose, less type-safe) — accepted.

### 13.3 Pipeline placement

**parse → migrate → normalize → validate → persist** — migrate is the new first step, ahead of
§12's normalize + validate. A migration brings an old document to the current shape; then the
existing normalize + validate run as usual.

### 13.4 Rewrite-on-migration (best-effort) + downgrade guard

- Migrate **in-memory** = authoritative. If the file was an older version, **best-effort rewrite**
  it to the current shape (atomic, logged) so the upgrade persists once and dead keys are cleaned.
  Best-effort: a read-only config dir doesn't block startup (memory wins, like
  `UpdateInMemoryThenPersist`); the next PUT persists anyway.
- **Downgrade guard:** file `version` **>** `currentConfigVersion` → **fatal**, clear message
  ("config is from a newer Station Manager; downgrade not supported"). Don't risk misreading
  newer fields.

### 13.5 Default-value changes — the equals-old-default guard

When a future release **changes the value of an existing default** (not adds a field — additions
auto-materialise on the next write via `applyDefaults`), filled-on-disk would otherwise freeze the
old value (§15.2). The fix is a migration step that rewrites the stale value **only when the
operator never customised it**:

```
if doc["smtp"]["port"] == oldDefault { doc["smtp"]["port"] = newDefault }
```

An untouched default propagates; a deliberately-customised value is left alone. This keeps
default-drift a **deliberate, reviewed, per-change** act (the release that changes the default
writes the migration) rather than an always-on silent re-resolution — and it's why sparse-on-disk
was unnecessary (§15.2). Rare in practice; default *value* changes are infrequent.

### 13.6 Status

**Scaffold + first migrations SHIPPED (2026-06-13).** `Config.Version` + `currentConfigVersion`
(now **2**) + the ordered raw-document registry + downgrade guard live in
`internal/config/migrations.go`; `Load` runs `migrateDocument` as the first pipeline step. The
loose→catalogue fold became the v0→v1 step (in-memory typed folds) and §10 2b's global
`bridge.mode_mappings` removal landed as the **v1→v2** raw migration. The equals-old-default guard
(§13.5) is a documented pattern with no current instance — the first default-value change will add
one.

## 14. Redesign — defaults home (decided 2026-06-13)

Resolves the §8 "defaults home" input + §7 finding (defaults sprawl across four mechanisms, no
single answer to "what's the default for X, and is it overridable?") + the `(a)/(a′)` wart
(some defaults live in `DefaultConfig`, not `applyDefaults`, only because the field can't tell
"unset" from "explicit false/0").

### 14.1 Single declaration site

**`internal/config/defaults.go`** holds every default value as a named const/var — consolidating
today's `applyDefaults` const block, the `DefaultConfig` seed values (logging booleans,
`smtp.starttls`), and the ft8 `Resolve*` default values — **plus a clearly separated
"safety ceilings (non-overridable)" section.** One file answers "what's the default / ceiling
for X."

### 14.2 One fallback-application pass (kills the wart)

A single `applyDefaults` / `resolve` fills every unset field from the central defaults, using
**`*T`** for the fields where zero/false is a legitimate operator value (the code already flags
"convert to `*bool`, mirror `ServeSPA`"). This removes the `DefaultConfig`-seed special case —
no more `(a)/(a′)` split. The ft8 `Resolve*` defaults fold into the same central values (the GET
handler still serves the SPA a resolved view, but reads from the one defaults home — no ft8-only
default copies).

### 14.3 Three separated roles: fill / clamp / reject

- **Defaults fill** — unset → central default (§14.2), in `normalize`.
- **Safety ceilings clamp** — the RF/safety limits (tune power ≤40W, duration ≤30s,
  restore-settle ≤2s, `ft8TxMaxDuration` 18s) live in the labelled ceilings section,
  **non-overridable**, applied as **clamps in §12's `normalize`** (one place) and emit a §12
  **warning** when they bite ("100 W capped to 40 W") — safe *and* visible, never refuses to
  start over an RF value.
- **Sanity/typo ranges reject** — timeout range, amp/power/zone bounds → **validation** rejects
  (§12 *error*); a 100-hour timeout is an error worth refusing.

### 14.4 Persistence interaction (settled by §15)

Whether filled defaults materialize to config.json or it stays sparse was a **persistence-shape**
question, **settled in §15: filled-on-disk** (sparse rejected). Defaults-home unifies *declaration*
+ *resolution* + the ceiling distinction only; the on-disk shape stays filled, with default-value
drift handled by a §13 migration (§13.5 / §15.2).

### 14.5 Implementation status (2026-06-13)

**SHIPPED — §14a consolidation/index:** `internal/config/defaults.go` is the single
discoverability point (the four-flavor model + the relocated numeric const block + the index of
subsystem-enforced safety ceilings, which stay in `bridge`/`ft8`/`types` to avoid coupling).

**DEFERRED — §14b `*T` wart fix:** converting the `(a′)` `DefaultConfig`-seed booleans
(`logging.with_timestamp`/`file_logging`/`log_file_compress`, `smtp.starttls`) to `*T` would
fold the two default-application mechanisms into one, but it churns the shared
`types.LoggingConfig`/`SmtpConfig` consumed by the logging + email subsystems (and reddens their
tests) for a cosmetic gain. The `DefaultConfig`-seed is the standard Go idiom for a default-true
bool. Accepted as-is. **§15 makes the deferral permanent:** sparse-on-disk (the only thing that
would have made `*T` load-bearing — to tell "operator set false" from "unset") was rejected, so
there's no remaining reason to revisit this.

**Clamp-with-warning in `normalize`** is the one piece of §12's normalize not yet wired (§12.6);
ceilings stay clamped in their owning subsystems (`bridge`/`ft8`) until §14's resolve pass lands.

## 15. Redesign — persistence shape (decided 2026-06-13)

Resolves the §8 "persistence shape" input + the §14 filled-vs-sparse deferral. Confirms the
single-file invariant and settles how config is written.

### 15.1 Single `config.json` — confirmed

Not split, not DB. The §9 taxonomy maps to storage:

| Scope | Stored in |
|---|---|
| C global · D identity · B per-rig overrides · E SPA prefs | **config.json** |
| A rig capability | embedded rigdefs (immutable) |
| F session / operating state | memory / localStorage — never config.json |
| G entities, caches, queue | the **DB** — never config.json |

Rule: **config in the one file; entities & derived state in the DB.** DB-for-config stays
rejected; the single-file invariant holds.

### 15.2 Filled on disk; default-drift handled by a §13 migration (decided 2026-06-13)

**Sparse-on-disk was considered and rejected** in favour of keeping the current **filled-on-disk**
shape (`applyDefaults` materialises in memory at Load; `WriteJSON` writes the resolved struct).

Sparse existed to fix **one** real problem: filled-on-disk *freezes* an old default into the file
(it writes `smtp.port: 587`), so a future change to that default silently never reaches an operator
whose config.json already materialised the old value. The other sparse benefits — "reads as pure
operator intent," smaller diffs — are cosmetic on a single-operator, config-isn't-authoritative
project. And full sparse is invasive: it requires moving `applyDefaults` from a Load-time mutation
to resolve-on-read (auditing every direct `.Cfg` read that bypasses `Snapshot()`), reworking the
§12b-2 PUT candidate pattern (which round-trips through the *resolved* snapshot and would re-fill a
sparse `s.Cfg`), and un-deferring §14b (the `*T`/omitempty fields — the only thing that makes
"operator set 0/false" distinguishable from "unset"). Large, breakage-heavy, for a modest gain.

Instead, the freeze is solved **where it actually occurs** — at a default *value* change — using the
already-shipped §13 migration mechanism. When a future release changes a default's value, it bumps
`currentConfigVersion` and adds a migration step that rewrites the stale value **only if the operator
never customised it** (the equals-old-default guard, §13.5). Propagation becomes a deliberate,
reviewed act per change, not an always-on silent re-resolution that can shift a working setup out
from under the operator on upgrade. New *fields* (vs changed default *values*) still auto-materialise
on the next write via `applyDefaults`, so additions need no migration — only value changes do, and
those are rare.

Consequence: **§14b stays deferred** (sparse was the only thing that made `*T` load-bearing;
without it, §14b is the cosmetic refactor it always was), and the config redesign's only remaining
open code item is §14's optional defaults-fill consolidation — no longer blocking anything.

### 15.3 Deterministic, atomic, daemon-owned writes

- `json.MarshalIndent` is **deterministic** (stable struct-field order; Go sorts map keys) →
  re-saving an unchanged config yields an *identical* file, no churn. (Addresses the "rewrite
  reorders fields" worry — it doesn't, beyond the deterministic shape; sparse means even fewer
  keys move.)
- **Atomic** temp-file + rename (`WriteJSON`) — kept.
- **The daemon owns the file while running.** Hand-edits happen while stopped (the existing
  "stop → edit → restart" rule stays); **no file-watch / auto-reload** — it would fight
  daemon-owns-writes for little gain.
- JSON has **no comments** — accepted; not switching format.

### 15.4 Status

**SETTLED (2026-06-13) — no code required.** The single-file invariant (§15.1) and the
deterministic / atomic / daemon-owned write discipline (§15.3) are already how `WriteJSON` behaves;
sparse-on-disk was rejected (§15.2) in favour of keeping filled-on-disk and handling default-value
drift via a §13 migration when (rarely) a default changes. Nothing to build now — this section
records the decision. The only remaining redesign code item is §14's optional defaults-fill
consolidation (and §11, gated on the config-SPA write path — §11.5).

### 15.5 Sparse-but-served for operator-invisible defaults (2026-06-23)

§15.2 rejected *full* sparse (the wholesale move of `applyDefaults` to resolve-on-read). It did
**not** preclude a narrower, per-block pattern that some blocks already use and that resolves the
"the operator can't see this default" problem without materialising it on disk:

- The block stays **sparse on disk** (zero = "use the built-in default," applied at point-of-use in
  the consuming package).
- `/v1/config` **GET serves the RESOLVED view** via a dedicated `Resolve*` helper, so the SPA reads
  effective values even though config.json never freezes them. No migration needed (nothing frozen
  on disk), and the consuming package stays the single owner of its defaults (no drift).

Blocks on this pattern: `ft8.display` / `ft8.frequencies` (`types.ResolveFt8Display` /
`ResolveFt8Frequencies`), `psk_reporter` (zero-resolves at use), and — added 2026-06-23 —
`bridge.timeouts` / `bridge.tune` (`bridge.ResolveTimeouts` / `ResolveTune`, the same resolution
`Service.New` applies, so served == runtime; tune ceilings stay non-overridable in `internal/bridge`).
This is why a fresh `config.json` legitimately shows `bridge.timeouts: {}` while the GET reports
`bridge_timeouts.liveness_ms: 5000` — sparse file, resolved API. Genuinely filled-on-disk blocks
(`server`/`datastore`/`logging`/`smtp`/`lookup`) keep their §15.2 treatment; the distinction is
whether the operator has another way to see the effective value (resolved GET) or not.
