# Configuration contract

**Status:** Canonical current reference
**Authority:** The Go types and behavior under `internal/config`, plus the
configuration handlers under `internal/api`, are authoritative when this document
disagrees with code.
**Scope:** `config.json` shape, ownership, defaults, validation, migration,
persistence, and runtime application. HTTP route detail belongs in
[`api-endpoints.md`](api-endpoints.md); subsystem detail belongs in its own canonical
reference.

## 1. File location and authority

`smd` uses one JSON configuration file. `--config` selects it explicitly; otherwise
the daemon resolves `config.json` under `utils.WorkingDir()` (`$SM_WORKING_DIR`, the
platform data directory, or the executable directory according to the startup path).
Runtime data paths derive from that same working-directory decision.

The file is daemon-owned while `smd` is running. A successful configuration PUT
rewrites the whole document from the daemon's in-memory snapshot, so do not hand-edit
the file concurrently with the daemon. Stop the daemon for direct edits.

Configuration owns operator intent and selectors. It does not duplicate:

- logbook or QSO rows, which are database-owned;
- CAT commands, parsers, and protocol defaults, which are rig-definition-owned;
- browser-session state such as the current page or transient FT8 run; or
- derived runtime objects such as an opened serial port or constructed forwarder.

## 2. Load and service model

`config.Load` applies one ordered pipeline:

1. read the JSON bytes;
2. migrate the raw document to the supported schema version;
3. unmarshal into `config.Config`;
4. apply unambiguous defaults and registered provider/forwarder seeds;
5. fold compatible legacy rig fields into the rig catalogue;
6. normalize canonical values and remove redundant overrides; and
7. run the single `config.Validate` entry point.

Any non-warning finding aborts startup. Warning findings are advisory and are logged
after logging is available. A file from a newer schema version is rejected rather
than guessed at.

`config.Service` is the synchronized runtime owner:

- `Snapshot` takes the read lock and returns a shallow, read-only value; callers
  must `Clone` it before mutating nested slices, maps, or pointers;
- `Update` takes the write lock, deep-clones the value, applies the mutation, writes
  the file, and only then replaces memory;
- a mutation error or disk-write error leaves both live memory and the existing file
  unchanged; and
- `UpdateInMemoryThenPersist` is the named exception for startup self-healing where
  this run must use the corrected value even if best-effort persistence fails. It is
  not the ordinary update path.

The lock covers the disk write and memory swap. A GET that races an update therefore
observes either the prior snapshot or the committed one, never a partially-mutated
configuration.

## 3. Current top-level shape

`internal/config.Config` is the on-disk schema. Nested definitions are canonical
`internal/types` values unless noted.

| JSON key | Go shape | Ownership and purpose |
|---|---|---|
| `version` | `int` | Schema version; currently `2`. |
| `data_dir` | `string` | Root for database, logs, caches, and other daemon state. |
| `useragent` | `string` | Shared outbound HTTP User-Agent; first-run startup supplies `station-manager/<build-version>` when absent. |
| `socket_path` | `string` | TCP address or Unix-socket path selected by `server.protocol`. |
| `server` | `ServerConfig` | Listener, HTTP limits/timeouts, embedded-SPA, profiling, and insecure-network acknowledgement. |
| `datastore` | `types.DatastoreConfig` | SQLite path, options, pool, and context timeouts. |
| `logging` | `types.LoggingConfig` | Daemon logging and rotation. |
| `forwarders` | `[]types.ForwarderConfig` | Durable destination instances, filters, cadence, retry, endpoints, and opaque credentials. |
| `logging_station` | `types.LoggingStation` | ADIF `MY_*` station identity used when emitting QSOs. |
| `operators` / `default_operator` | `[]types.Operator` / `string` | Operator roster and default roster selector. |
| `setup_complete` | `bool` | Server-managed first-run completion flag. |
| `default_logbook_id` | `int64` | Selector for a database-owned logbook row. |
| `default_rig_id` | `int64` | Selector for one entry in `rigs`; `0` is valid only when no rigs exist. |
| `restore_rig_on_mode_switch` | `*bool` | SPA behavior; absent means enabled. |
| `station` | `types.StationConfig` | Non-ADIF operating preferences: amplifier, fallback power, and operating bands. |
| `qsl` | `types.QslDefaults` | Standing outgoing QSL defaults, stamped only into otherwise-empty QSO fields. |
| `psk_reporter` | `types.PskReporterConfig` | Opt-in FT8 reception-report publisher. |
| `map` | `types.MapConfig` | Sparse per-band contact-map arc colors. |
| `lookup` | `types.EnrichmentConfig` | Country source, prioritized callsign chain, completion policy, cache TTLs, and refresh concurrency. |
| `smtp` | `types.SmtpConfig` | Session-email delivery and credentials. |
| `bridge` | `types.BridgeConfig` | Bridge enablement, timeouts, and tune policy; active rig identity is projected from `rigs`. |
| `ft8` | `types.Ft8Config` | FT8 enablement, decoding/transmit policy, display, frequency, audio/meter, decode-log, and Field Day settings. |
| `evidence` | `types.EvidenceConfig` | Explicit capture/sync consent, physical cap, and restart-pinned antenna declarations. |
| `rigs` | `[]types.RigConfig` | Installed-rig catalogue and operator-specific per-rig overrides. |

Unknown JSON fields are ignored by typed unmarshal. Do not rely on that as an
extension mechanism: a later daemon rewrite will drop fields it does not own.

### 3.1 Server shape

`server` contains:

- `protocol`: `tcp` or `unix`;
- `read_header_timeout_sec`, `read_timeout_sec`, `write_timeout_sec`,
  `idle_timeout_sec`, and `shutdown_timeout_sec`;
- `max_body_bytes`, `default_page_limit`, `max_page_limit`,
  `max_contact_history_results`, `max_concurrent_requests`, and
  `max_event_subscribers`;
- `submit_rate_per_sec` and `submit_rate_burst`;
- `serve_spa`, whose absent default is true for TCP and false for Unix sockets;
- `enable_profiling`, default false; and
- `allow_insecure_network`, a file-only acknowledgement described in §5.1.

### 3.2 Identity, station, and QSL blocks

`logging_station` uses ADIF `MY_*` JSON names. Its callsign, owner, operator,
locator, coordinates, zones, address, rig, antenna, and Morse fields are
operator data. `station` separately stores `amp_enabled`, `amp_multiplier`,
`default_power`, and `operating_bands`; those affect application derivations but
are not themselves copied wholesale into ADIF.

`operators` is a roster of `{callsign, name?}` entries. `default_operator` names
one roster callsign. An empty legacy roster is seeded from
`logging_station.operator`, falling back to `station_callsign`, and then points
the default at its first entry.

`qsl` contains the outgoing defaults `qsl_via`, `qslmsg`, and `qsl_sent_via`.
They are defaults, not retroactive edits: a QSO's explicit value wins.

### 3.3 Forwarders, lookup, SMTP, map, and PSK Reporter

Forwarder details and retry semantics live in the forwarding reference. The
configuration contract is:

- `name` is the stable instance key used by durable upload rows; changing it is
  not a cosmetic rename;
- `type` must name a registered implementation;
- `label` is an optional file-only display label;
- `credentials` is type-owned opaque JSON and may contain secrets;
- `action_filter` accepts supported `insert`, `update`, and `delete` actions;
- `tick_interval_sec`, `batch_size`, `retry`, and `endpoints` are persisted
  operator overrides; and
- `allow_insecure_http` is valid only for the `smcloud` type and is file-only.

`lookup.hamnut` is the country/prefix source. `lookup.chain` is the callsign
provider chain. Provider `priority`, not JSON array position, is authoritative;
normalization sorts the chain and validation requires positive, unique,
contiguous priorities, including for disabled entries.

#### Lookup sources, labels, completion policy, and TTL (`§(a‴)` compatibility)

Each provider has a stable `name`, optional file-only `label`, `enabled`, URL,
credentials, timeout, and view URL. Registered providers are seeded disabled so
newly compiled capabilities are discoverable without silently activating a
network dependency.

`continue_if_blank` lists the fields whose absence permits consulting the next
callsign provider. Absent legacy policy normalizes to `name` and `gridsquare`.
An explicit empty list retains first-substantive-result behavior.

`country_ttl_days` and `station_ttl_days` are pointer-valued because absence and
zero differ: absent means use defaults `365` and `90`; explicit `0` means trust
the cache indefinitely. Negative values are invalid. `refresh_max_in_flight`
defaults to `4`.

SMTP stores `enabled`, host, port, username, password, sender, default recipient,
STARTTLS, and timeout. `map` stores sparse band-color overrides. `psk_reporter`
stores its enable flag and collector override; receiver identity comes from the
station configuration.

### 3.4 Evidence

Evidence capture and synchronization are separately opt-in and default off.
`cap_bytes` is the physical cap across the evidence database and its WAL/SHM
siblings. `sync` reuses an enabled `smcloud` forwarder's URL and token; no second
evidence credential surface exists.

Each antenna declaration contains a lineage `name`, optional type/feedline/
height/locator, and one or more bands. Antenna declarations are activated and
pinned when the evidence store opens, so edits require restart. Disabling capture
or sync stops new work; it does not delete prior data.

## 4. Defaults, resolved values, and ceilings

`internal/config/defaults.go` is the discoverability index for defaults. A value
may reach runtime through four distinct mechanisms; do not collapse their
semantics when adding a field.

| Mechanism | Meaning | Representative fields |
|---|---|---|
| Load-time fill | Zero is unambiguously unset and is replaced by a constant; registered provider/forwarder entries may also be added missing and disabled. | HTTP limits/timeouts, datastore pool, log rotation, forwarder cadence, SMTP port/timeout, evidence cap. |
| First-run seed | Applied only to a new `Config`, because false/empty is a legitimate persisted choice. | timestamp/file/compressed logging and SMTP STARTTLS. |
| Read/serve resolver | Keep the file sparse; calculate an effective value for consumers and API responses. | FT8 display, dial frequencies, caller-answer mode, max repeats, audio/meter thresholds, bridge timeouts/tune. |
| Pointer default | `nil` means unset while explicit `false` or `0` remains meaningful. | `server.serve_spa`, `ft8.enable_osd`, occupancy guard, lookup TTLs, restore-on-mode-switch. |

### 4.1 Load-time fallback values

| Area | Defaults |
|---|---|
| Server | header/read/write/idle/shutdown `5/10/30/120/10 s`; body `1 MiB`; pages `50/500`; history `100`; concurrent requests `128`; event subscribers `16`; submit rate/burst `20/40`. |
| Datastore | SQLite at `<data_dir>/db/station-manager.db`; open/idle connections `8/8`; ordinary/transaction context timeouts `10/10 s`. |
| Logging | level `info`, relative directory `log`, file output on when neither output is selected; maximum size/backups/age `100 MiB / 5 / 30 days`. A fresh config also enables timestamps and compression. |
| Station/selectors | amplifier multiplier `1.0`; default logbook `1`; default rig `1` only when a rig catalogue exists. |
| Forwarders | tick `120 s`; batch `5`, unless the registered type supplies stricter defaults. |
| Lookup | country/station TTL `365/90 days`; refresh concurrency `4`; provider HTTP timeout `10 s`. |
| SMTP | port `587`; timeout `30 s`. |
| Evidence | cap `524,288,000` bytes. |

The first-run listener is TCP at `127.0.0.1:8080`, with the embedded SPA served.
Unix-socket mode resolves a private socket path and defaults to headless. FT8,
PSK Reporter, evidence capture, and evidence synchronization remain opt-in.

### 4.2 Sparse resolved settings

- FT8 display: `history_max=100` clamped to `[10,2000]`,
  `feed_mode="accumulate"`; its three old color fields remain round-tripped for
  compatibility but have no current app consumer (resolved values
  `#15803d/#9ca3af/#b45309`).
- FT8 dial frequencies in hertz: `160m 1840000`, `80m 3573000`, `60m 5357000`,
  `40m 7074000`, `30m 10136000`, `20m 14074000`, `17m 18100000`,
  `15m 21074000`, `12m 24915000`, `10m 28074000`, and `6m 50313000`;
  positive per-band overrides replace or extend the map.
- FT8 RX audio window: `-60` to `-10 dBFS`.
- FT8 transmit-meter amber threshold: raw ALC value `30`.
- FT8 caller-answer mode: `operator_pick`.
- FT8 maximum repeats: `6`, clamped to `[1,10]`.
- FT8 occupancy idle-inhibit: true; OSD fallback decoding: true unless explicitly
  disabled.
- FT8 occupancy: passband `200..3000 Hz`, threshold factor `4.0`, ranking weights
  margin/edge/center `0.5/0.2/0.3`, and guard margin `10 Hz` (explicit guard `0`
  disables it).
- Restore-on-mode-switch: true when absent.
- SMTP zero port/timeout normalize to `587/30` before validation.
- PSK Reporter empty collector resolves to `report.pskreporter.info:4739`.
- Bridge timeout defaults in milliseconds: liveness `5000`, reconnect backoff
  initial/max `1000/30000`, steady-state threshold `10000`, write watchdog `500`,
  CI-V read gap/ACK `50/500`, CI-V poll interval/quiet `1000/250`, and FT8 meter
  poll interval/timeout `250/100`.
- Bridge tune defaults: `20 W`, `15000 ms` maximum carrier, and `150 ms` restore
  settle. Sparse zero values select these subsystem-owned defaults.

### 4.3 Non-overridable safety ceilings

Safety ceilings are not defaults and do not become larger through configuration:

- tune power at most `40 W`, tune duration at most `30 s`, and restore settle at
  most `2 s`;
- FT8 transmit hard auto-off at `18 s`;
- FT8 unanswered-rung repeat cap at most `10`; and
- bridge timeout overrides must remain within the validator's sane range of
  `50 ms` to `1 h`, with the CI-V read gap additionally clamped to `2 s`.

## 5. Validation and security posture

Every loaded or API-mutated candidate is normalized, then passed to
`config.Validate`. The complete rule implementation is in
`internal/config/validate.go`; important operator-facing groups are:

- listener protocol, positive HTTP limits, page-limit ordering, and network
  posture;
- unique positive rig IDs, known rig models, a resolving `default_rig_id`, and
  valid ADIF mode mappings;
- registered and usable forwarders, lookup ordering/policy, SMTP, PSK Reporter,
  map, bridge, and evidence dependencies;
- unique valid operator callsigns and a resolving default operator;
- station callsigns, Maidenhead locator, coordinate/locator consistency, zones,
  DXCC, amplifier multiplier, fallback power, and operating bands;
- FT8 display, audio, occupancy, and Field Day values;
- evidence cap, antenna declarations, and the evidence-sync dependency on an
  enabled, credentialed SM Cloud forwarder; and
- datastore driver, path, connection-pool bounds, and context timeouts, plus the
  logging level, relative log directory, rotation bounds, skip-frame count, and
  shutdown timeout — mirrored 1:1 from the SQLite and logging consumers so a bad
  hand-edit fails at the config boundary rather than later at startup.

Warnings are advisory findings. All other findings block Load and cause a config
PUT to fail without changing disk or memory. A PUT that carries forwarders also
constructs each enabled candidate with the same registry used at startup; an
unusable destination is rejected before persistence.

### 5.1 Bind posture — `server.allow_insecure_network`

The API has no network authentication or transport encryption. A TCP address that
is not recognizably loopback—including wildcard binds, LAN/public addresses, and
non-localhost hostnames—therefore exposes data, configuration, daemon control, rig
commands, and transmit-capable surfaces to every reachable client.

Such a bind is fatal unless the operator sets the file-only
`server.allow_insecure_network: true`. The acknowledgement permits startup but
retains a standing warning; it does not add authentication. Profiling on the same
bind adds a separate advisory because `/debug/pprof` can disclose process data and
consume substantial resources.

The acknowledgement is deliberately absent from `/v1/config`, so an API client
cannot grant itself broader network exposure.

### 5.2 Unix socket permissions

Unix mode resolves its default socket only under an owner-private runtime or state
directory. It never falls back to `/tmp`. If no private path can be resolved and no
explicit `socket_path` is configured, validation fails startup. Directory and socket
permissions form the access boundary for this mode.

### 5.3 Private configuration-file permissions

`config.json` contains plaintext SMTP passwords, provider credentials, and
forwarder tokens. `WriteJSON` creates the replacement at owner-only `0600`, preserves
an existing stricter owner-only mode such as `0400`, and tightens a wider legacy
mode on the next write. Secrets must never be committed.

### 5.4 Unknown-key rejection (ADR 0074 / ADR 0075)

A supported-version `config.json` that carries a key the schema does not recognize
is refused at `Load`, before the daemon starts, migrates in place, tightens
permissions, or otherwise writes — so an operator's typo can never be silently
dropped while startup reports success. The refusal names every offending dotted
path — indexed for struct-slice elements, e.g. `rigs[0].typo`, `forwarders[1].nope`
— and never a value, because the message reaches stderr and `smd.log` while
`config.json` does not.

The gate is deliberately strict but narrow. It runs on the migrated document, so a
key a migration consumes is gone before the check (§13). A map's KEYS are always
operator data, never schema: a scalar-valued map (forwarder `endpoints`, band
colors, FT8 frequencies) and a `json.RawMessage` (forwarder `credentials`) are
opaque and never reported. But a map whose value TYPE is a struct — a rig's
`mode_mappings` (`map[string]ModeMapping`) — has each VALUE checked against that
struct, so a typo inside a mapping (`rigs[0].mode_mappings[DATA-U].submdoe`) is
rejected while its mode-literal key is not. A malformed document, a
newer-than-supported version, and an unknown key are three distinct diagnostics;
none is reported as another. `smd config-check [--config <path>]` runs the same evaluation read-only —
without starting the daemon — so a would-be startup refusal is diagnosable ahead of
a deploy, reporting the same paths with values omitted.

## 6. HTTP edit surface

`GET /v1/config`, `PUT /v1/config`, `GET /v1/rigs`, and the capability catalogues
are specified in [`api-endpoints.md`](api-endpoints.md). The essential persistence
rules are:

- GET returns a deliberately projected view, not the complete file;
- PUT is presence-aware: omitting a writable block preserves it, while carrying a
  whole-block field generally replaces that block;
- `rigs` and `default_rig_id` are write-only on `/v1/config`; their full read view
  is `/v1/rigs`;
- server, datastore, logging, evidence, advanced bridge/FT8 knobs, and security
  acknowledgements remain file-only unless explicitly projected;
- server-managed joins and flags such as `setup_complete`, `default_logbook`, the
  narrow `default_rig`, and `mailer` cannot be overwritten by echoing a GET;
- forwarder, lookup, and SMTP passwords are masked on GET and merged on PUT; and
- a successful response is built from the committed configuration.

The first setup PUT with a non-empty station callsign seeds the default logbook and
sets `setup_complete`. Until per-logbook operating identity is implemented, a later
callsign change that conflicts with the default logbook is rejected with `409`
rather than storing a configuration that would make subsequent QSO submissions
fail.

## 7. Secret-preserving merge rules

The API never echoes password or token values. It reports whether a secret is set.

- SMTP and lookup: blank password means keep the stored value;
  `password_clear=true` explicitly removes it, and clear wins if both are sent.
- Forwarders: credentials are merged by stable forwarder `name`. Omitted or blank
  fields keep stored secrets, except registry-declared clearable non-secret options
  where blank intentionally restores the constructor default.
- Replacing or removing a forwarder/provider entry removes the configuration owned
  by that entry. Lookup's callsign chain is replaced as a whole, not merged by
  missing provider name.

Advanced forwarder values and file-only labels/security acknowledgements are carried
forward when an API edit cannot represent them.

## 8. Ownership scopes

Use the narrowest real owner for a new value:

1. Database facts: logbooks, QSOs, upload queue, durable history.
2. Embedded rig-definition facts: commands, parsers, protocol defaults,
   manufacturer/model capabilities.
3. Per-rig operator facts: device binding and genuine rig-specific overrides.
4. Station/operator facts: identities, station preferences, QSL defaults, roster.
5. Subsystem policy: server, datastore, forwarding, lookup, SMTP, bridge, FT8,
   evidence.
6. Session state: current page, current operator selection, transient run state.
7. Derived runtime state: active projections, opened resources, connection state.

Do not store a derived value beside its inputs. Do not move a rig-definition fact
into every installed-rig profile merely because the editor can display it. A small
amount of boundary-specific projection is preferable to parallel source-of-truth
structs.

## 9. Placement and inheritance rules

### 9.1 Installed-instance facts

For operator-installed hardware, the rig catalogue stores instance identity and
environment-specific binding.

### 9.2 Embedded model facts

The embedded catalogue stores model behavior: CAT vocabulary, parsers, capabilities,
and protocol defaults. Those values are displayed to the editor but are not copied
into each installed instance.

### 9.3 Resolution order

Resolution is always:

1. explicit per-rig override, when present;
2. embedded rig-definition default; then
3. subsystem fallback only where that field defines one.

Zero-valued `RigOverrides` fields mean inherit. Pointer-valued `ft8_mode` and
`my_rig` distinguish inheritance from an explicit empty override.

### 9.4 Multiple rigs of one model

Multiple rigs of the same model are independent instances. They may have different
ports, audio devices, and overrides; coincident override values are valid and do not
make one profile the owner of another.

## 10. Rig profiles and active projections

`rigs` is the authoritative installed-rig catalogue. Each `types.RigConfig` holds:

- positive stable `id`;
- `model`, a key into the embedded CAT rig definitions;
- operator-specific serial `port`;
- optional serial `overrides` for baud, data/stop bits, parity, delimiter, read
  timeout, RTS, and DTR;
- name-based `audio.rx` and `audio.tx` device selections;
- optional `ft8_mode` override;
- per-rig rig-literal to ADIF `mode_mappings`; and
- optional `my_rig` override.

`default_rig_id` selects the active instance. `Config.ActiveBridge()` projects its
model, port, and serial overrides into a runtime bridge configuration.
`Config.ActiveFt8()` projects its RX/TX audio names and resolved FT8 mode into the
runtime FT8 configuration. Stored `bridge.serial`, `bridge.cat`, `ft8.device`, and
`ft8.tx.device/mode` are compatibility/projection targets, not parallel current
owners.

Mode mappings merge the selected rig definition's defaults with that instance's
operator overrides. Only deviations from the definition should persist. Live QSO
submission resolves `MY_RIG` from the explicit per-rig pointer; absent derives the
rig definition's display name, while explicit empty suppresses the field.

### 10.1 Catalogue validity

IDs must be positive and unique, models must be known, and `default_rig_id` must
resolve. The only rig-less state is an empty catalogue with selector `0`. Per-rig
mapping modes and submodes must be valid ADIF values.

### 10.2 Inheritance

Serial overrides use zero as inherit. `ft8_mode: null` inherits the rig-definition
default; `ft8_mode: ""` deliberately leaves the rig's current mode unchanged.
`my_rig: null` derives from the definition name; `my_rig: ""` suppresses ADIF
`MY_RIG`.

### 10.3 Legacy fold

A pre-catalogue configuration with loose bridge driver/port can synthesize rig `1`.
The legacy global FT8 mode and `logging_station.my_rig` are folded into the active
rig when the typed compatibility rules can preserve their meaning. Legacy numeric
audio indices are not guessed into stable names.

### 10.4 Per-rig audio identification

#### 1. Name-based, per-direction devices (`§10.4 #1` compatibility)

Audio is a per-rig resource and is selected separately for receive and transmit by
device name. Indices are not stable across reboot/replug and capture/playback
enumerations can assign different indices to the same codec. At acquisition time,
the audio layer resolves each name against the corresponding live enumeration.

An empty name means the system default for that direction. A configured name that
cannot be found is treated as unavailable; the FT8 subsystem must not silently grab
an unrelated device. `GET /v1/hardware` exposes the same enumeration used by the
editor and acquisition path.

### 10.5 Runtime selection

Changing `default_rig_id` changes persisted intent. The running bridge, FT8 device
bindings, and other startup-captured consumers continue to use their constructed
active-rig state until restart; there is no rig hot-swap path.

### 10.6 Editing surface

The consolidated app edits whole rig-catalogue drafts. It reads installed rigs and
the embedded definition summaries from `GET /v1/rigs`, enumerates hardware through
`GET /v1/hardware`, and writes presence-aware `rigs` plus `default_rig_id` through
`PUT /v1/config`. The PUT response deliberately does not widen its narrow active-rig
projection into a second full catalogue surface.

## 11. Runtime application and restart boundaries

### 11.1 General model

There is no general configuration watcher or subsystem rebuild after a PUT.
Persistence success means the new intent is durable and visible through snapshots;
it does not imply every already-constructed service has been replaced.

### 11.2 Application classes

| Class | Examples | When change applies |
|---|---|---|
| Read at use | station/QSL/operator data and per-rig `MY_RIG` for the already-active rig | The next operation that explicitly reads a config snapshot. |
| Explicit live side effect | `ft8.tx.max_repeats` | The PUT also calls the running sequencer's setter. This is the sole current `/v1/config` live side effect. |
| Client-owned | FT8 display/map and restore-on-mode-switch preferences | When the app adopts/refetches the saved value; no daemon rebuild. |
| Restart-required | listener/server, datastore, logging, user agent, forwarder and lookup/refresher construction, SMTP service, active-rig selection, bridge hardware/timeouts/tune, FT8 device/decoder/decode-log/PSK Reporter, evidence activation | After daemon restart. |

The configuration diff/restart hint may classify changes for the UI, but it does not
make restart-only fields live. Rig hot-swap is not implemented.

### 11.5 Current live-applied field

`ft8.tx.max_repeats` is the one field with a server-side live application path. A
successful PUT first persists the candidate and then pushes the committed resolved
value into the running sequencer. A rejected or failed write therefore cannot alter
the live attempt limit, and a concurrent save cannot leave runtime ahead of the
last committed value.

## 12. Normalize and validate

Normalization is a deterministic transform shared by Load and PUT. It currently:

- trims and uppercases callsign identities and operator-roster keys;
- canonicalizes Maidenhead locators, converts legacy ADIF-form coordinates to
  decimal degrees, and derives coordinates from a locator only when both are empty;
- trims zone/DXCC values;
- normalizes provider URLs, lookup TTL/policy/order, and SMTP defaults (the API
  overlay separately canonicalizes Field Day input);
- removes per-rig overrides equal to embedded defaults; and
- drops empty legacy bridge serial/CAT projection blocks.

Normalization does not silently repair contradictions. For example, explicit
coordinates outside the declared locator are rejected, not moved to the locator's
center.

`Validate(Config) []Finding` is pure. Each finding has a field, stable code,
message, and warning severity. Load makes error findings fatal; PUT maps them to
client errors. Both callers validate the whole normalized candidate, so a value
accepted through the API must also survive the next startup.

## 13. Versioning and migration

### 13.1 Version field and ordered registry

The current schema version is `3`. A missing version is the version-`1` baseline.
Migrations are registered and applied one version at a time.

### 13.2 Raw-document migrations

Raw-document migrations run before typed unmarshal so they can read fields removed
from the current structs. They must preserve or explicitly reject malformed operator
data.

Version `1 -> 2` moves removed global `bridge.mode_mappings[driver]` entries into
each matching rig's `mode_mappings`, synthesizing the legacy rig first when needed,
then removes the old key. Typed, idempotent folds handle retained compatibility
fields such as the old global FT8 mode and `logging_station.my_rig`.

Version `2 -> 3` (ADR 0075) consumes the version-2 keys whose typed struct fields
were removed, so an upgraded install still boots (ADR 0067) while the unknown-key
gate below stays strict. It deletes `ft8.tx.auto_work_callers` and
`ft8.meter.alc_red`; folds each `rigs[].audio.device` into the rig's `audio.rx` and
`audio.tx` when those are absent (never overwriting an operator's split values),
then deletes `device`; and moves `psk_reporter.antenna` into
`logging_station.my_antenna` only when the canonical field is absent — otherwise the
canonical value wins — then deletes the retired key. The step is idempotent, and it
also consumes an `audio.device` that the `1 -> 2` step may have synthesized.

### 13.3 Pipeline placement

Raw migration precedes typed unmarshal; the unknown-key gate (§5.4) runs between
them, on the migrated document; typed compatibility folds follow defaulting and
rig-catalogue synthesis; normalization and validation run last. This ordering keeps
removed data visible long enough to migrate, rejects a genuine typo before any
write, and validates only the canonical current candidate.

### 13.4 Persistence and downgrade guard

`Load` itself never writes. The in-memory value is current and carries version `3`.
When the on-disk document was an older version, startup persists the migrated shape
exactly once, under an explicit `schema_version` persistence reason (names the
version, never a value); the next boot reads a current file and writes nothing. A
version greater than the daemon supports is a fatal downgrade guard, kept distinct
from both a malformed document and an unknown-key refusal.

Startup persistence is otherwise delta-driven: a boot that resolves to exactly what
is on disk leaves the file's content and mtime untouched, so a quiet log means a
quiet file. A legacy wider-than-`0600` file is still tightened as an explicit
permission action even on such a no-op (§5.3).

Any future shape change must bump the version, add the next ordered migration, and
test old shape, current shape, malformed input, idempotence, and downgrade refusal.
A key removed from the structs but still written by older installs must be consumed
by a migration (not merely ignored), so the unknown-key gate stays strict (ADR 0075).

## 14. Defaults ownership

Declare config fallback constants in `internal/config/defaults.go` and apply them
through the matching mechanism in §4. A subsystem may own a resolver or safety
ceiling when only it can interpret the value, but `defaults.go` must index that
location so the answer remains discoverable.

Adding a default requires deciding whether zero is a valid operator choice. If it
is, use a pointer, a first-run seed, or a resolver; do not overwrite explicit zero or
false during every Load. Keep fill, clamp, and reject distinct:

- fill supplies an absent fallback;
- clamp enforces a runtime safety ceiling where the resource is owned; and
- reject reports invalid operator input at the shared validation boundary.

## 15. Persistence shape

### 15.1 One document

There is one `config.json`. Do not add per-subsystem config files or a second mutable
configuration source. Database-owned records and immutable embedded catalogues remain
outside it.

### 15.2 Filled and sparse fields

The stored shape intentionally mixes two policies:

- operational daemon fields whose zero is not meaningful are filled, making the
  effective startup values visible in the file; and
- optional/operator-invisible defaults remain sparse and are served or consumed
  through canonical resolvers.

Do not infer a repository-wide sparse or fully-materialized policy. Preserve the
field's established zero semantics.

### 15.3 Deterministic, atomic, daemon-owned writes

`WriteJSON` uses indented JSON, a unique temporary file in the target directory,
explicit private permissions, an fsync of that file, an atomic rename, and a final
fsync of the parent directory (PT-6 — crash-durable). A partial write cannot leave a
half-formed primary file; a nil error with `Durable` means the replacement survives a
crash, while a rename that succeeds before the directory fsync fails is reported as
durability-uncertain — applied and live, not a failure. Go's JSON encoder gives
deterministic map-key ordering for the owned typed shape.

`Service.Update` writes disk before swapping memory and holds the write lock through
both. All three update primitives (`Update`, `UpdateIfChanged`,
`UpdateInMemoryThenPersist`) run the canonical normalize→validate pipeline on the
candidate before any commit, so the boundary itself — not caller discipline — rejects
a shape the next Load would refuse, returning a typed validation error. The update
callback receives a deep clone, so that rejection (like a persistence failure) cannot
leak nested slice/map/pointer mutations into the live snapshot. `UpdateInMemoryThenPersist`
still commits memory once the candidate validates, even if the subsequent write fails.

### 15.4 API writes

PUT overlays represented fields onto the latest locked configuration, normalizes,
validates, probes carried enabled forwarders, writes the whole file, swaps memory,
then builds the response. Unrepresented file-only fields survive because the overlay
starts from the current value.

The read-before-write API workflow is not optimistic concurrency control. Two
independent clients can still produce last-writer-wins results across separate PUTs;
closing that requires an explicit server-side revision/precondition contract.

### 15.5 Sparse-but-served values

FT8 display/frequency/audio/meter defaults and bridge timeout/tune defaults are the
main sparse-but-served cases. `GET /v1/config` returns their resolved effective
values without materializing them into `config.json`. Map and PSK Reporter collector
overrides remain raw sparse blocks where the app can render its own placeholders or
the subsystem owns resolution.
