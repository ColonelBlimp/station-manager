# FT8 Evidence Capture and Live Station Presence — Design Document

**Status:** Draft 3 for review
**Author:** Marc, 7Q5MLV
**Date:** August 2026

This draft supersedes the Draft 2 "Spot Network" framing. The custom wire
protocol, collector, and research network are no longer part of the build;
they are summarised as possible future directions (§11), and the four review
rounds of protocol-level design that shaped them are preserved in this
file's git history for the day they are wanted. What remains is one vertical
slice, buildable on infrastructure that already exists.

---

## 1. Summary

While the FT8 subsystem is running — passive monitoring or active operating —
Station Manager Desktop (SMD) records every useful decoded observation into
its local SQLite database. That history synchronises to Station Manager
Cloud (SMC) over the existing authenticated SMD ↔ SMC channel. Live TX / run
/ queue state travels separately, as a current-state snapshot with a
heartbeat. From these two flows plus the QSO store it already holds, SMC
serves one public, callsign-specific page answering three questions for a
calling station:

1. **"I am on air"** — this station is running FT8 now (band, mode).
2. **"You have been heard"** — your signal was decoded here (SNR, how long
   ago).
3. **"You are queued — hang tight"** — you are in this station's pile-up
   queue (ADR 0067's bag-and-drain run model).

One reporting station is enough to make the page useful to an unbounded
audience of callers with web browsers. No new transport, no new database
engine, no public protocol: the slice is local capture, sync over the
existing channel, one latest-state endpoint, one page, and explicit opt-in
and retention controls.

Local capture also stands on its own: it preserves evidence the daemon
currently discards after each pass, during the most interesting propagation
years of the solar cycle (NOAA/SWPC places Cycle 25's maximum around late
2024–early 2026), and it makes the operator's own station fully
instrumented — every decode, with full context, queryable at home.

---

## 2. Motivation

**The caller's questions have no good answer today.** A station calling into
a pile-up learns nothing until they are answered or give up. The community's
partial answers — checking PSK Reporter mid-pile-up for "did their receiver
decode me", DXpedition live logs for "am I in the log" — are delayed,
third-party, and say nothing about *session state*: whether the operator's
software has you queued. That state exists only inside Station Manager
(`internal/ft8/sequencer.go` — the working station, candidate list, and
ordered drain queue are already published daemon state), and the expected
operating value of exposing it is QRM reduction: a caller who can see they
are queued can stop calling and wait.

**The evidence is being discarded.** Each decode pass yields the full 77-bit
payload, the time offset `dt` (the standard bad-clock and data-quality
diagnostic — recovering actual path delay from it would need calibrated
clocks at both ends, so timing diagnosis is the defensible claim), and
decoder metrics. What survives today
is a log line and — for PSK Reporter — the strongest observation per
callsign per five-minute window (`internal/pskreporter/service.go`, the
protocol's own norm). The complete stream exists in memory and is thrown
away. Capture is cheap; the data is unrecoverable.

**Both halves are useful at N = 1.** The page needs one reporting station to
serve every caller; the local evidence needs no network at all to be worth
having. Nothing in this design depends on adoption, third-party clients, or
a research community — those are possible consequences of having the data
(§11), not prerequisites.

**Station Manager keeps reporting to PSK Reporter unchanged.** Same IPFIX
path, same five-minute summary discipline. An operator who adopts SM must
not vanish from the community's map; this feature is additive.

---

## 3. The two data flows

The architecture is two flows with deliberately different semantics, and the
split is load-bearing — every serious defect found in the earlier drafts'
reviews came from letting history and liveness share a channel.

### 3.1 Durable history

Decoded observations: SQLite rows on SMD, synchronised to SMC over the
existing authenticated HTTP channel (the same family as QSO stamp sync).
Delayed uploads and retries are *fine* — history is history whenever it
arrives, and the page displays age honestly.

One quality-of-service rule keeps the heard answer fresh without a third
flow: **sync is prompt while live** — when the FT8 subsystem is running and
SMC is reachable, new observations push at decode-cycle cadence (a cycle's
batch is tiny) — and **lazy for backlog** — after an outage, accumulated
history drains at leisure behind current data.

### 3.2 Live state

TX / run / queue state: a **current-state snapshot** with a heartbeat, sent
to a dedicated latest-state endpoint. It bypasses the history sync entirely,
by construction: a backlog of old observations draining after an outage must
never make a station appear live, and presence must never queue behind
history. Snapshots are full-state and latest-wins — idempotent, matching
SMD's own confirm-by-push philosophy.

### 3.3 The three answers, derived

- **"I am on air"** derives from a recent authenticated heartbeat plus the
  snapshot's explicit state (§6.2's model: monitoring / on-air / transmitting
  are distinct facts from distinct subsystems) — **never inferred from
  uploaded decodes**.
  The confusable state is exactly the dangerous one: a station offline for a
  day, draining its decode backlog, looks active to any design that infers
  liveness from data arrival.
- **"You have been heard"** derives from a recent synced observation in
  which the queried callsign is the **transmitter** (§5.3 — an addressee in
  a directed message was *not* heard), with SNR and age displayed.
- **"You are queued"** derives from the current queue snapshot, with an
  explicit stale indicator when updates stop — a caller can tell "the queue
  hasn't changed" from "the page has lost the station".

---

## 4. What is recorded

"All the data possible" means **all useful decoded-observation data** — not
raw signal. Per decode:

- the raw 77-bit payload (10 bytes; the complete decoded message — preserves
  callsigns, grids, reports, QSO state, contest exchanges, free text, and
  survives future message-family additions without schema change);
- slot time, dial frequency, audio offset;
- SNR, `dt`, decoder flags and metrics (including AP-assisted marking, which
  changes a decode's evidentiary weight);
- a reference to the station configuration in effect (§4.2).

**The claim's boundary, stated plainly:** the payload is the decoded
*message record*, not received-signal evidence. It supports future
re-parsing, but cannot rerun an improved decoder, reassess a false positive,
or recover soft symbol decisions. **Continuous raw audio (PCM/IQ) is
explicitly out of scope** — it would multiply storage by orders of magnitude
and turn this into a different product.

**Three named prerequisites**, so the plan does not discover them
mid-build:

1. **The decode path must become evidence-grade, which is more than
   widening one struct.** `DecodeLine` today exposes normalised text,
   frequency, `dt` and SNR only (`internal/ft8/decode.go`), and much of
   what §4 promises is discarded upstream of it. Four concrete
   requirements:
   - a **verified-payload result even when text unpacking is
     unsupported** — go-ft8 documents decode gaps (DXpedition/Fox-Hound,
     telemetry families), and today an unsupported-but-CRC-valid payload
     never reaches SMD at all; evidence capture stores the payload with
     `text: unsupported`, it does not require the parser to keep up with
     the protocol;
   - **per-message AP/decoder provenance** carried on each result (much of
     this already exists publicly in go-ft8 — SMD's projection is what
     discards it);
   - **no evidence-level filtering of valid payloads** — operator-view
     filters (own-transmission drop, display curation) apply to the view,
     never to the evidence stream;
   - **one stateful `goft8.Decoder` per receiver stream** — SMD currently
     uses the stateless per-slot API (its own comment defers the stateful
     decoder), losing cross-slot callsign-hash context that `<...>`
     resolution (§5.3) depends on.
2. **The split happens at the rich result, not at the existing sink.**
   `SetDecodeSink` receives a `DecodeReport` only *after*
   `dropOwnTransmissions` and after the rich go-ft8 results are projected
   to four fields (`internal/ft8/service.go`) — fan-out there inherits the
   filtering and the projection and can never recover what they discarded.
   The branch point is upstream:

   ```
   go-ft8 rich result (stateful decoder, per stream)
       ├── evidence capture — unfiltered, unprojected
       └── curated operational stream (own-TX filter, projection)
             ├── sequencer / UI
             ├── PSK Reporter
             └── optional WSJT-X-compatible UDP (§11)
   ```

   The existing sink and every view-shaped consumer live on the curated
   branch; evidence taps the rich result before any of it. The rule that
   no consumer may block decoding applies to both branches.
3. **Run identity must be threaded end to end** (§6.2): sequencer-minted,
   carried on `QsoStatus` and `CompletedQso`, stored with the local QSO,
   and synced to the Cloud store — none of which exists today. "Logged
   this run" (§7) is unimplementable without it.

### 4.1 Storage contract

The evidence store is a **separate SQLite file (`evidence.db`)**, not new
tables in the log database — and the reason is the invariant, not tidiness.
SQLite WAL permits one writer per database, and the log database runs a
5-second `busy_timeout` (`internal/database/sqlite/consts.go`): evidence
inserts at decode cadence, cap-driven purges, or maintenance holding that
writer could make a **QSO commit** — the sacred path — wait behind
housekeeping. A shared file can only promise the invariant by discipline; a
separate file promises it by structure. Capture writes into its own file
still follow an asynchronous bounded-writer shape: tiny transactions,
fail-fast (a capture write that cannot proceed drops and counts, never
queues unboundedly), and chunked purges. The behavioural rules the decode
log already established (rotate and drop rather than ever block FT8) carry
over:

- an operator-configurable size cap that is a **physical-disk guarantee,
  not a logical row target**: it is defined over `evidence.db` *plus* its
  WAL and temporary allocation, with reserved headroom, and capture starts
  dropping **before** the physical limit is reached. SQLite's WAL can
  outgrow any logical target when a long reader blocks checkpointing, so
  evidence readers are bounded to short transactions — a full shared
  filesystem would break the QSO path this file exists to isolate (§4.1's
  opening rationale), which makes the physical definition load-bearing
  rather than pedantic. Capture **never blocks decoding or logging** —
  under pressure it drops, never stalls;
- drop order prefers observations SMC has already acknowledged, and
  **every cap purge is recorded — acknowledged and unacknowledged alike**,
  as two record kinds with different meanings, and the loss taxonomy is
  three-valued because "unacknowledged" does not mean "absent from SMC" —
  an offered batch whose acknowledgement was lost in transit may be
  committed remotely without the client ever learning it. Dropping
  unsynced observations is a **loss interval** — start/end time, reason,
  count, band/dial context — carrying a `remote_status`: `never_offered`
  (definitely absent remotely) or `offered_unacknowledged` (remote
  presence **unknown**, never claimed lost). Purging *acknowledged*
  observations is a **local-retention record** — range, count, reason, and
  the acknowledged status — a statement that the local archive ends here
  while the data was present in SMC at acknowledgement time (subject to
  cloud retention and deletion thereafter). Purges and their records
  commit in the **same SQLite transaction**: a crash between deletion and
  record creation would produce exactly the invisible gap this machinery
  exists to prevent. Both are tiny rows and both sync,
  so either archive can describe its own boundaries; this also resolves
  the coverage coupling honestly — a `decoded` coverage row whose local
  observations were purged is interpretable through the retention record
  that says where they went. "When and for how long was the record
  incomplete, and where" is exactly what a later consumer needs, and a
  lifetime counter cannot say it. A gap you know about is data quality;
  one you don't is corruption;
- cap-driven drops are not the only way evidence goes missing, so each
  slot writes a lightweight **coverage record** with an outcome —
  `decoded / no_decode / tx / dial_changed / decoder_error /
  capture_dropped` — one tiny row per 15 s, which is what makes an empty
  stretch of archive *interpretable* (transmitting? band change? decoder
  fault?) rather than ambiguous. "Complete" in this document means
  complete **relative to these records**, never an unqualified claim.
  Coverage records are **first-class synced rows**: UUIDv7 identity,
  per-row outcomes, the same batch protocol as observations (§5) — so the
  synced archive can explain its gaps, not just the local one — and their
  retention is **coupled to the interval they describe**: cap purging
  never removes a slot's coverage record while retaining that slot's
  observations, or the archive would keep data while discarding the
  explanation of what's missing around it. The reverse orphan — a
  `decoded` coverage row whose observations were locally purged — is
  legitimate and self-explaining, because the purge wrote a
  local-retention record (above) saying what was removed, why, and that
  SMC holds it;
- loss and coverage reporting must survive the failure it reports: the
  reporter is a **reserved in-memory accumulator, persisted with priority
  when the writer recovers** — an overloaded evidence writer must not lose
  the record of its own overload through the same failing path. The honest
  crash-time limit is stated: losses accumulated but not yet persisted at
  a hard crash are gone, and that boundary is documented rather than
  papered over;
- the local synced flag is a **scheduling optimisation, not sync state**:
  with per-UUID acknowledgement there is no cursor to reconstruct, and a
  restored backup's rolled-back flags merely cause harmless duplicate
  offers that SMC's upsert absorbs (§5.2). Recovery from an *SMC-side*
  rollback (the cloud store restored from its own backup, losing
  acknowledged rows) is out of scope for this slice — SMC is the backed-up
  layer — and a UUID manifest/reconciliation endpoint is the named future
  remedy if it is ever needed.

### 4.2 Station configuration

Versioned station profiles — transmit power, antenna type and height,
feedline, locator — recorded locally with `validFrom` lineage, never mutated
in place, so an antenna change does not rewrite the meaning of history.
This has logbook value independent of everything else ("which antenna made
this QSO", the ADIF `MY_*` fields), and it is what makes the evidence
interpretable later.

Receiver noise floor is recorded **when a calibrated measurement exists**.
Honesty note: SMD's RX level meter currently reads dBFS on an uncalibrated
audio chain; until a calibration against the rig is done, the field carries
a not-measured sentinel rather than a number that looks like dBm.

---

## 5. Synchronisation (durable history)

### 5.1 Shape

JSON batches on the existing authenticated SMD ↔ SMC HTTP channel. A batch
carries **tagged sync records under one envelope** — observations, coverage
records, loss intervals, local-retention records, profile versions — and
the idempotency contract is uniform across every kind: **each record has a
stable UUIDv7 identity, a content digest, and receives a per-row
outcome**. Nothing about a record's kind exempts it from the rules; a loss
interval that could be silently double-ingested or silently dropped would
undermine the very honesty it exists to provide. A batch is any set of
records — contiguity carries no meaning — and SMC answers with the
per-row terminal outcome, because "acknowledged UUIDs" alone cannot
express the failure modes the design itself defines:

> `accepted` · `already_present` · `tombstoned` · `suppressed` ·
> `retryable_missing_profile` · `permanent_reject(reason)`

The first four are terminal — SMD marks the row synced and never re-offers
it. `retryable_missing_profile` is the one retryable outcome (§5.4's
ordering self-heal). `permanent_reject` (digest conflict, malformed row) is
**quarantined locally**: marked, kept out of every future batch, surfaced
to the operator — and it never blocks the other rows in its batch. Without
explicit outcomes a conflicting row either retries forever or vanishes
silently; both are worse than a quarantine the operator can see. The
synced flag remains pure upload scheduling (§4.1 — a wrong flag costs a
duplicate offer, nothing more). Nothing about ordering is load-bearing, which is what lets
§3.1's priority rule (current observations ahead of backlog) and §4.1's
drop rule (bounded store may shed unsynced rows) coexist with exactness: a
dropped observation is simply absent and counted, and a high-water mark
that could neither advance across a gap nor acknowledge out-of-order
uploads is not part of the design.

### 5.2 Identity and idempotency

Every observation is identified by a **UUIDv7 minted at capture time** —
the same identity model the QSO system already uses (ADR 0016), and the
reason four review rounds' worth of sequence machinery is absent from this
draft:

- **Capture works fully offline by construction.** Identity needs no server
  handshake, no issued epochs, no continuity proof. A fresh install, a
  restored backup, and a long-offline portable session all just mint and
  hold UUIDs.
- **Restore cannot duplicate and cannot be silently wronged.** A restored
  backup re-offers the same UUIDs with the same content — SMC's upsert
  deduplicates them. Rows captured after the restore carry new UUIDs — no
  collision is possible. And SMC keeps a content digest per observation: a
  re-offered UUID whose digest differs is rejected loudly as a client bug,
  never silently deduplicated or accepted.
- **Drops leave honest gaps.** An observation shed under §4.1's cap before
  sync simply never arrives; the loss records are the record. No *sequence*
  tombstones are needed because no contiguous sequence exists to repair.
- **Deletion leaves tombstones — or sync undoes it.** UUID-upsert dedup cuts
  both ways: an observation deleted from SMC by callsign (§8) no longer has
  a row to deduplicate against, so a restored backup's normal re-offer would
  quietly recreate it. SMC therefore keeps **persistent deletion tombstones
  by observation UUID**, and ingest checks them before upsert. A tombstoned
  UUID is refused permanently; the client marks it synced and moves on.

### 5.3 Server-side model

Plain PostgreSQL in SMC's existing deployment — at one station's volume
(tens of thousands of decodes on a busy day) no other engine is justified.
Observations land in an append table plus a **callsign occurrence index**:
one row per callsign occurrence with its **role** — `transmitter`,
`addressee`, or `unknown` — and resolution kind (a hashed `<...>` call
resolved at decode time is recorded then, the only moment that knowledge
exists).

The role column is load-bearing for answer 2: "JA1ABC K1XYZ −12" is evidence
that **K1XYZ** was received — JA1ABC merely appears in the message. Heard
lookups select transmitter-role rows only; deletion by callsign spans every
role.

**A synced observation carries its decode-time interpretation, not just the
payload.** The raw payload alone cannot reproduce a hash resolution the
stateful decoder learned from earlier slots, so each observation includes:
nullable decoded text, a parse status (`parsed / unsupported / partial`),
decoder build and options, and the **client-stamped callsign occurrences**
with role and hash-resolution kind. SMC may re-derive and validate the
ordinarily-parseable fields against the payload, but the client's
decode-time hash resolutions are **preserved as submitted** — they are
knowledge that existed only in that decoder at that moment.

Callsign identity is **one versioned canonicalisation function, applied
everywhere identity is compared** — not a rule local to occurrence rows.
Every source stores the **exact form** it saw (`K1ABC/P`, `K1ABC/R`,
compound forms); every comparison matches on the **canonical base call**
(one licence holder, one identity) computed by the same function:
occurrence matching, presence queue and working-call matching, QSO-store
lookups (*queued* / *being worked* / *logged* / *previously worked*),
lookup-token binding, cache keys, deletion, and opt-out. The ladder spans
three sources and the opt-out promises suppression across all of them — a
function applied to only one lets a portable form be *heard* under its
base identity yet fail to appear *queued*, or escape suppression through
the QSO store. The function is **versioned**: stored records keep exact
forms, matching applies the current version at query time, so a
canonicalisation improvement re-partitions every source consistently and
at once, never one table at a time. Results always display the exact form
that was heard.

**Ingest is one transaction, suppression checked first.** Parsing, the
ongoing-opt-out suppression check (§8), the raw observation row and its
occurrence rows commit **atomically** — there is never a state where a raw
payload survives while its occurrence rows were suppressed, or vice versa.
A suppressed observation is not stored at all, its UUID is **tombstoned**
(so a restored local backup cannot reintroduce it later, §5.2), and the
per-row outcome says `suppressed` so the client marks it synced. The honest
limit is stated too: suppression matches callsigns *knowable at ingest* —
an unresolved `<...>` hash carries no callsign to check and is stored in
its hash form; suppression is a filter on resolvable identity, not
cryptanalysis.

### 5.4 Configuration sync

Observations reference the station configuration in effect (§4.2), so the
referenced profile must reach SMC or the promised context is an unresolved
id. Profiles are tiny, immutable, versioned records; they sync on the same
channel under the same identity model — each profile version carries its own
UUIDv7 beside its `(configId, version)` lineage, and observations reference
the profile UUID. Ordering is the client's job and cheap: a profile version
is upserted before the first observation that references it; SMC treats a
reference to an unknown profile as retryable, not fatal, so a reordered
upload heals itself. Profiles are cloud-stored for context and later
analysis; the page renders none of their content (the operator's chosen
locator precision aside).

---

## 6. Presence (live state)

### 6.1 Session and ordering

The rules here are the distilled, transport-independent survivors of the
earlier drafts' hardest review findings; each sentence exists because its
absence was a found defect:

- SMD opens a presence session: `POST …/presence/session` with a
  client-generated attempt id; SMC returns a server-persisted **incarnation
  token** (strictly increasing per station). Creation is idempotent — a
  retry of the same attempt returns the same incarnation — so a timed-out
  POST plus its retry cannot mint two incarnations and strand the client on
  a stale one.
- Snapshots PUT `(token, snapshotRev)`; `snapshotRev` is per-session,
  monotonic from zero, no client persistence needed. SMC's presence state
  is explicit, so ordering and authority never blur into "newest wins":
  it holds the **authority token** (the one incarnation whose snapshots are
  currently accepted), the **last accepted revision for that authority**,
  and its **last receipt time**. Revision ordering applies *within* the
  authority only — a candidate session **does not participate in ordering
  at all** until its transfer (below) succeeds, and a candidate's rejected
  first snapshot consumes no revision state and disturbs the standing
  authority in no way: a higher incarnation can exist, rejected, while a
  lower one goes on publishing. Incarnation numbers order *attempts*, not
  *authority*.
- **Authority transfers on the first snapshot, not on session creation —
  and only over a currently-stale authority.** A newly minted incarnation
  becomes authoritative **atomically with the acceptance of its first valid
  full snapshot**, and SMC evaluates the takeover condition **at that
  moment, in the database** — a single PostgreSQL **station-presence row**
  holds the authority token, incarnation counter, last accepted revision
  and last receipt time, and both session creation and first-snapshot
  transfer read-modify-write that row under a row lock / compare-and-swap
  transaction. A process mutex is explicitly not the mechanism: it would
  let two SMC replicas accept different authorities, and in-memory
  authority state lost to a restart would re-admit a superseded token. The
  transfer succeeds only if there is no
  current authority, the current authority is stale *right now* (its last
  snapshot older than the staleness bound at acceptance time), or the
  snapshot carries an explicit operator-takeover flag. A candidate whose
  condition fails is rejected — its session never becomes authoritative and
  the client stays yielded. This closes both failure shapes: a token-holder
  that never speaks never holds authority, and a displaced client acting on
  a *sampled* age can never displace an authority that has heartbeated
  since the sample — without the atomic check, two healthy clients
  alternate ownership at exactly the staleness interval.
- **A snapshot is sent immediately on every transition** — FT8 lifecycle
  start/stop, run start/stop/pause/resume, every queue change (bag, unbag,
  drain advance), working-station change, and TX key/unkey. The 60-second
  heartbeat is a **liveness backstop only**: heartbeats alone could satisfy
  neither the one-cycle queued criterion (§7.1) nor the display of a keyed
  interval shorter than the heartbeat period.
- Heartbeats (60 s) re-send the latest snapshot with a **fresh, incremented
  `snapshotRev`** — a heartbeat is a new snapshot. Only revision-advancing
  snapshots refresh liveness; an equal-revision duplicate (transport retry)
  refreshes nothing.
- A PUT bearing a superseded token gets an explicit stale-token response
  (409) — never a silent void — and **the superseded client yields**: it
  stops publishing and surfaces "another instance owns presence" to its
  operator. It must **not** auto-create a new session: with two live
  clients, recreate-on-409 is an endless takeover loop — A's new, higher
  incarnation supersedes B, B recreates and supersedes A, forever. (The
  crashed-token-holder case that once justified auto-recreate is already
  handled by authority-on-first-snapshot above.) Re-acquisition is allowed
  only when the authoritative session has gone stale or by explicit
  operator takeover — and the 409-carried last-snapshot age is **advisory
  scheduling only** (it tells the yielded client when a retry is worth
  attempting); the binding staleness check is SMC's own, made atomically at
  first-snapshot acceptance (above), so a retry against an authority that
  has since heartbeated is simply rejected and the client remains yielded.
- Client-supplied timestamps are display-only (clamped to a sane skew of
  server time); liveness derives from SMC's own receipt times.
- Presence PUTs are fire-and-latest: coalesced while offline (only the
  newest snapshot is kept and sent on reconnect), and **never routed through
  the QSO upload queue**, whose forever-retry semantics are exactly wrong
  for current state.

### 6.2 Snapshot content and the state model

"On air" is three different facts wearing one phrase, and the snapshot keeps
them distinct because they come from three different subsystems:

| Public state | Meaning | Source of truth |
|---|---|---|
| `offline` | subsystem stopped, or presence lost | a **terminal snapshot** on graceful stop; staleness as the fallback |
| `monitoring` | FT8 subsystem running, RX only | FT8 capture-session lifecycle |
| `on_air` | a run/QSO session is active | sequencer state (`QsoStatus`, ADR 0067) |
| `transmitting` | FT8 TX keyed at this instant | the bridge's FT8-TX keyed state — not `TxActive()`, which unites tune carriers with FT8 TX and would show a tune as "transmitting"; never the sequencer |

`transmitting` is a flag on `on_air`, not a fourth ladder rung — it flickers
per cycle. Whether `monitoring` is publicly visible is the operator's choice
(§12.2) — but the choice is between two **coherent** modes, because a header
label alone hides nothing: a fresh *heard* result reveals a live receiver
whatever the header says. Either monitoring is visible (header shows it,
heard answers normally), or the station is publicly on-air-only (while
merely monitoring, the header shows offline **and** heard lookups return
nothing newer than the last run). A mode that relabels the header while
serving fresh heard results would be a privacy control in name only. The
same coherence extends **backwards at run start**: in on-air-only mode,
public heard answers require `observed_at >= runStartedAt` — otherwise the
ordinary ten-minute window would immediately expose observations captured
during the hidden monitoring period the moment a run begins, converting the
privacy mode into mere display deferral.

The snapshot carries: the state above; mode and dial frequency; a **run id
and `runStartedAt`** when a run is active (what scopes "logged this run",
§7); **`lastEndedRunId` and `runEndedAt`, retained in every snapshot until
the grace period (§7) expires**; the station being worked; the queue in
drain order. The ended-run fields exist because presence coalesces: if a
run ends — or runs entirely — while disconnected, the reconnect snapshot is
the *only* snapshot, and without them it would carry no identity or end
time for the grace-period run. SMC computes grace from `runEndedAt`
(validated for skew like every client timestamp, §6.1), never from
reconnect time — starting grace at reconnect would extend it incorrectly.
Two durability rules keep the grace record from being volatile state:
SMD **persists the minimal ended-run record locally** (id, end time) until
grace expires, so a daemon restart while disconnected does not erase it
before SMC ever hears of it; and SMC **retains the newest unexpired grace
record independently of later snapshots** — a snapshot without ended-run
fields (a replacement authority's fresh session, a client restarted past
its own grace) does not clear an existing grace record; only expiry does.

**Run identity is new cross-cutting state — the one thing here the daemon
does not already hold.** Neither `QsoStatus` nor `CompletedQso` carries a
run id today, and the Cloud QSO store cannot be queried by one until the id
travels the whole path: minted by the sequencer, carried on `QsoStatus`
(presence) and on `CompletedQso` → the local QSO record → the existing
cloud sync. The lifecycle rules, so they are decided rather than
discovered: a run id is **minted at run start** (`cq/start`); it
**survives Stop/Resume** — a paused bag-and-drain queue is the same
logical run — and it **survives presence reconnection** (it is daemon
state, not session state); it **ends at Abandon or run end** (ADR 0059
W6). Everything else in the snapshot is composition of inputs the daemon
already holds — FT8 lifecycle, sequencer status, bridge TX state.

**And its scope is narrower than `on_air`, deliberately.** `on_air` covers
*any* active QSO session — answering someone else's CQ, a one-off
work-a-caller contact — but a run id exists only for Call-CQ runs, because
that is the only session shape with a pile-up, a queue, and a "this run" to
scope. Rather than invent a second identity for non-run contacts, the
run-scoped ladder rungs (*queued*, *logged this run*) simply **exist only
during a run**: outside one, the page still shows `on_air` honestly, heard
answers normally, the ladder tops out at *being worked*, and a caller
logged in a one-off contact sees it immediately on the "previously worked"
line (band and time, minutes old) — serviceable without a queue that
doesn't exist claiming otherwise.

**Heartbeats run for as long as the FT8 subsystem is being advertised** —
through monitoring, between runs, across a run's whole life. A graceful
stop sends an explicit **terminal snapshot** (state `offline`) before
heartbeats cease, so the page flips to offline immediately and a deliberate
shutdown stays distinguishable from an unexpectedly stale station ("went
offline at ⟨time⟩" versus "last seen ⟨time⟩, presumed offline"); staleness
remains the fallback for the crash case only. **Accepting a terminal
snapshot atomically marks the token terminal and releases authority** in
the station-presence row (§6.1) while retaining the public offline state —
without the release, the terminal snapshot would be the freshest
authoritative snapshot and a normally-restarted daemon would be rejected
until its own *goodbye* went stale, locked out of its own presence by
having shut down politely. A duplicate terminal PUT (transport retry) is
idempotently acknowledged, not 409'd: the token is recognised as terminal
rather than superseded. (An earlier draft tied
heartbeats to the run; that would have made a monitoring station
indistinguishable from an offline one, contradicting the state model
above.)

---

## 7. The page

**Live delivery is short polling, stated so the acceptance criteria are
end-to-end claims rather than server-side ones.** An open page refreshes by
polling a cached JSON status endpoint every 5 seconds (provisional), with
the server cache TTL at or below the poll period. The per-call ladder
refreshes by a **callsign-bound token**, because a generic status poll
cannot carry per-call state without exposing the queue: the initial lookup
— the rate-limited, oracle-guarded query (§8) — returns a **stateless
signed token** (an HMAC over station, canonical callsign, and expiry — the
server stores nothing per token), and the open result then polls by token
at the same cadence. The refresh cache is keyed by canonical
`(station, callsign)`, not by token, so a thousand tokens for the same
lookup share one entry — and the cache carries **explicit per-station and
global entry caps**, because "bounded by token count" bounds nothing when
distributed probes mint tokens freely. Two budgets, deliberately split:
**new-callsign probes** carry the oracle rate limits; **refreshes of a
valid token** carry their own **higher-but-finite budget** per station,
source, and token — statelessness bounds memory, not traffic, and a valid
signed token replayed without limit would otherwise be a free query lever.
"Stateless" is scoped precisely: the token needs **no durable lookup or
session row**; the rate limiter keeps **bounded ephemeral counters keyed
by token digest**, which expire with the tokens they meter. Cache
population is **single-flight per canonical callsign** (concurrent misses
for one call produce one query) — and since single-flight does nothing
against *distinct* callsigns rotated through the capped cache, database
queries from the public lookup also pass a **per-station and global
concurrency semaphore**, so the worst distributed probe pattern degrades
to bounded, queued work rather than a query amplifier. Tokens expire
(provisionally one hour) and a fresh probe re-establishes them. Five-second polling makes a
typical ~13 s FT8 keyed interval coarsely but honestly visible; the
`transmitting` flag is displayed on that understanding. SSE is the upgrade
path if polling ever feels crude at pilot scale; a page that only updated
on manual refresh would have required deleting the `transmitting`
presentation and weakening criteria 1–2, which is why the mechanism is
specified rather than left to implementation.

One public page per station, plus a per-call lookup. The lookup is the
primary interface for the pilot; a full public queue display is deliberately
deferred until observed behaviour justifies it (§10, §12).

A caller enters their callsign and gets one state from the ladder:

> not heard · heard (SNR, age) · queued (hang tight) · being worked ·
> logged **this run**

— the last from the QSO store SMC already synchronises, **scoped by the
snapshot's run id** (§6.2). The scoping is a correctness rule, not polish:
an unscoped log lookup would rank a contact from years ago above *currently
heard* or *currently queued* — precisely inverting what the caller needs
right now. Prior history appears separately, as an informational line
("previously worked: 17 m FT8, 2026-03-02"), never as the ladder state. The
page header shows station status (on air / monitoring / offline, band,
mode) with an as-of time, going visibly stale when heartbeats stop.

Three normative rules keep the ladder honest, values provisional until the
operator ratifies them:

- **Heard has a TTL, computed from `observed_at` — with delay and skew
  deliberately kept apart.** A *heard* result derives from observations
  whose slot time falls within the last 10 minutes; older decodes fall out
  of the answer (age is always displayed, so the boundary is visible rather
  than magic). The validation rule is one-sided: the **archive preserves
  the reported slot time verbatim** — it is evidence — and the public view
  **excludes** (one normative behaviour, no flagging variant) an
  observation whose slot time is in the *future* beyond the allowed skew of
  SMC's server time, because a future-dated clock is the only failure that
  can keep a call "heard" longer than reality. Distance between
  `ingested_at` and `observed_at` is **never** grounds for exclusion — that
  distance is ordinary upload delay, which the durable flow explicitly
  permits: an observation delayed by a two-minute outage is still honestly
  inside the ten-minute window, and the TTL on `observed_at` retires
  backfilled history naturally. A slow client clock understates recency and
  can fail the 30-second criterion; that failure direction is conservative
  and accepted — no clock-offset correction mechanism is attempted.
- **Heard is band-scoped while a run is active.** A caller decoded on 20 m
  must not read "heard" against a page header showing a 40 m pile-up — that
  answer would persuade exactly the wrong stations to stop calling. During
  an active run, a heard result requires the observation to match the run's
  pinned dial/band **and** `observed_at >= runStartedAt` — this holds in
  every privacy mode, not just on-air-only (§6.2's rule is the privacy
  case of the same principle). Outside a run, heard answers from any recent
  band, with **the observation's band displayed prominently** in the
  result — useful information, honestly labelled, rather than suppressed.
- **The winning observation is deterministic**: the latest valid
  `observed_at` in scope; ties break on higher SNR, then observation UUID.
- **Stale presence downgrades, never asserts.** When heartbeats have
  stopped, *queued* and *being worked* must not render as current claims —
  a dead station's last snapshot telling a caller "hang tight" is the worst
  answer the page can give. Stale state renders as "last known as of
  ⟨time⟩: queued", visually distinct from a live claim.
- **A just-ended run lingers briefly, and the current run always wins.**
  The run id remains queryable for a short grace period (provisionally 10
  minutes) after the run ends, so a caller worked in the final minutes can
  still see "logged this run" before the ladder reverts to history-only.
  Precedence when a new run starts inside the grace window: the ladder
  scopes to the **current** run whenever one exists; the grace-ended run is
  consulted only when no newer run is active.

### 7.1 Acceptance criteria

Operator-observable, each with its nearest confusable state:

1. When the station decodes DX_CALL while live, the lookup shows *heard*
   with SNR within two FT8 cycles (~30 s) — distinguishable from *not
   heard*. (Met natively: prompt-while-live sync, §3.1. No relaxation
   needed.)
2. When the operator bags DX_CALL, the lookup shows *queued* within one
   cycle — distinguishable from *merely heard*.
3. When SMD loses connectivity mid-run, the page shows *stale* within two
   minutes — distinguishable from *run ended* and from *not being heard*.
4. When SMD reconnects and drains a backlog, nothing in that drain changes
   station liveness or resurrects an ended run — *current presence* is
   distinguishable from *replayed history*. (Held structurally by the
   two-flow split, §3.)
5. When the operator logs DX_CALL's contact, the lookup shows *logged this
   run* within one minute (provisional — this is the QSO-sync latency
   bound, and the value is the operator's to ratify), distinguishable from
   *being worked*.

### 7.2 Discovery

A link from the operator's QRZ page, and word of mouth. No in-band
announcement: FT8 free text is 13 characters and transmit cycles are for
working stations.

---

## 8. Privacy, consent, retention

- **Opt-in, default off**, with plain-language disclosure of what leaves the
  machine and what the page shows, before the first byte ships.
- **What is published:** the station's own on-air state; per-call heard
  results (call, SNR, age); queue membership and worked/logged status for
  stations that called us. The norm cover is long-established: PSK
  Reporter's entire dataset is reports about transmitting stations, DX
  Cluster has published third-party voice/CW spots for thirty years, and
  DXpedition live logs are the direct precedent for "am I in the log".
  Every published fact here concerns a station that transmitted to us, on
  the air.
- **Raw message content:** FT8 messages are broadcast unencrypted on amateur
  spectrum — free text included — and the stored payload is treated
  accordingly; the page itself renders interpreted fields only.
- **The lookup is a queue oracle, and this is accepted explicitly rather
  than discovered.** Nothing proves a querier owns the callsign they enter,
  so anyone may poll known callers and partially reconstruct the deferred
  full-queue view. The property is accepted for the pilot on two grounds:
  the underlying facts are already on the air (anyone monitoring the band
  hears who is calling us; the oracle adds only which of them were bagged),
  and an ownership challenge would destroy the zero-friction caller
  experience the feature exists for. The compensations are strong per-source
  and per-callsign rate limits on the lookup, and the full queue staying
  unpublished. If pilot behaviour shows the oracle being abused, the
  ownership question reopens (§12.1).
- **Locator precision** on the page is the operator's choice (4- or
  6-character).
- **Retention and deletion:** local evidence is the operator's own, bounded
  by §4.1. SMC-side history retention is a stated policy with deletion on
  request, driven by the role-complete occurrence index (§5.3) —
  implementable because hash resolutions were recorded at ingest — and made
  durable by deletion tombstones (§5.2), without which a routine backup
  restore would resurrect deleted records. The policy distinguishes two
  different requests: **historical deletion** (a one-time purge of existing
  records) and an **ongoing opt-out** — and the opt-out spans **every
  public answer source, not just evidence**. Ingest suppression (§5.3)
  stops observation retention, but *queued*, *being worked*, and
  previously-logged answers derive from presence snapshots and the QSO
  store — separate sources that would otherwise keep answering for a
  suppressed callsign. An opted-out callsign therefore gets no public
  lookup answer at all (the page behaves as if the call were unknown),
  while the operator's own operational state is untouched: the daemon
  still queues and works the station normally, and the operator's log
  still records the contact. The opt-out governs what the *page* says,
  plus what *evidence* is retained — both halves, stated so neither is
  assumed to imply the other.
- **Destructive requests require a verified workflow — the public lookup
  can never be their authority.** The lookup deliberately proves no
  callsign ownership, which is fine for reading and disqualifying for
  deletion: an unauthenticated deletion or opt-out path would let anyone
  suppress a rival's visibility and mint permanent tombstones in their
  name. Deletion and opt-out therefore run through a separate, verified
  request workflow — at pilot scale, request by email with the operator
  verifying the requester against community-standard identity evidence
  (published contact routes, LoTW membership) and adjudicating manually.
  Every executed request writes an **audit record** (who, what, when,
  verification evidence); execution is **idempotent** (a repeated request
  is a no-op, not a second purge); and **opt-out is revocable** through
  the same verified path — revocation lifts page suppression and future
  retention, while what was already purged stays purged.
  And it states the local-copy boundary plainly: an SMC deletion reaches
  SMC; the operator's private local store is the operator's own record of
  what their receiver decoded — the same standing as a paper logbook — and
  is not subject to third-party deletion.

---

## 9. Client behaviour requirements

- Capture, sync, and presence must **never block or delay decoding,
  logging, or operating**. Local queue, fire-and-forget, drop on sustained
  failure (§4.1).
- SMD functions normally with SMC unreachable, degraded, or the feature
  disabled.
- Sync resumes from SMC's acknowledgement; presence coalesces to the latest
  snapshot; heartbeats run exactly while the FT8 subsystem is advertised
  (§6.2) and stop with it — a client must not fake liveness in either
  direction.

---

## 10. Pilot and evaluation

Time-boxed, with a pre-registered go/no-go: **before the pilot starts**, the
operator sets the thresholds — a threshold set after the data is in is how a
pilot always "passes".

- **Measured:** distinct non-operator callsigns using the per-call lookup
  across a stated number of busy sessions (page logs), and **decoded
  repeat-calling of queued stations from our own decode stream, correlated
  with lookups that returned *queued***. Stated as what it is: a **proxy**,
  not a direct behaviour measurement — a queued caller may keep transmitting
  while undecodable, fade, or move frequency, so "decoded repeats fell"
  supports the QRM-reduction hypothesis without proving it; a strong claim
  would need a control or an independent occupancy measurement the pilot
  does not attempt.
- **Judged:** the operator's read of pile-up behaviour with the queue
  visible, and whether the page changed anything real.

The pilot's outcome gates nothing larger than itself: if it disappoints, the
slice still leaves behind local capture (valuable alone) and a synced
evidence archive.

---

## 11. Possible future directions (explicitly not part of this build)

Recorded so the option is preserved and the reasoning is not re-derived:

- **The cross-station research network** — many reporters, a published
  binary protocol (SBE over QUIC was designed in detail), TimescaleDB-scale
  storage, verified identities, station-context joins. Gated on a concrete
  external demand signal: a researcher asking for the fields the existing
  aggregators lack (HamSCI already consumes PSK Reporter, WSPRNet, and RBN —
  the ask must be for what those cannot supply: TX power, antenna, noise
  floor, `dt`), or a third-party implementer committing to run a reporter.
  Four rounds of external review hardened that protocol design; it lives in
  this file's git history, ready if the gate is ever met.
- **Backfill is option-preserving for retained observations, with
  observable gaps.** Because capture stores complete decoded-message records
  under stable UUIDs, any future collector can be seeded from the archive —
  minus what was truly lost — never-offered observations, which the loss
  intervals record as such (§4.1); offered-but-unacknowledged drops are
  *possibly* present in SMC (recorded `remote_status: unknown`, never
  claimed either way), and locally purged acknowledged history was present
  in SMC at acknowledgement time, with the local-retention records saying
  so.
  Nothing built today forecloses anything; what the cap shed is honestly
  gone.
- **Optional outbound reporting adapters.** A WSJT-X-compatible UDP decode
  broadcast (opt-in) would let the operator feed the wider ecosystem —
  notably **RBN FT4/FT8 spotting** via RBN's own documented node model (the
  RBN Aggregator; CWSL_DIGI is their other path), plus GridTracker-class
  consumers, softening the switching-from-WSJT-X regression. Same shape as
  the PSK Reporter sink: independent, asynchronous, never blocks decoding,
  entirely separate from SMC presence (RBN wants CQ spots, not queue
  state). Depends on the decode-sink fan-out named in §4; before building,
  confirm from RBN's guides that the Aggregator owns CQ-only filtering.
- **Presence for non-SM stations** — the filtered PSK Reporter MQTT
  redistribution (`pskr/filter/v2/…/{rx_call}/…`) could someday drive a
  heard-only page for stations that report to PSK Reporter without running
  SM. Not used here: for our own station the data is first-hand, fresher,
  and dependency-free.

---

## 12. Open questions for reviewers

1. **Queue visibility granularity.** The pilot ships per-call lookup only.
   Should a full public queue display ever follow, and does visible queue
   state improve pile-up behaviour (queued stations stop calling) or invite
   gaming? Operating experience with DXpedition live logs is directly
   relevant.
2. **"Monitoring" visibility.** When the station is RX-only, should the page
   show monitoring, or should the station be publicly on-air-only? Note the
   two options are the coherent pair from §6.2 — hiding the label while
   serving fresh heard results is not one of them, since a fresh heard
   answer reveals the live receiver regardless of the header.
3. **SMC history retention.** Local retention is the operator's; what should
   the cloud-side policy promise, and for how long is synced evidence kept?
4. **Go/no-go values.** The thresholds in §10 are the operator's to set
   before the pilot; are the *instruments* (lookup counts, repeat-calling
   from the decode stream) the right ones?

---

## 13. Rejected and deferred alternatives

| Alternative | Disposition |
|---|---|
| Continuous raw audio (PCM/IQ) capture | Rejected: orders-of-magnitude storage, a different product. The decoded-message record is the deliberate boundary (§4). |
| SBE / QUIC / TimescaleDB collector now | Deferred to §11: a second transport, ack protocol, storage engine, and public service ahead of demonstrated demand. The repo currently carries none of these stacks, and the vertical slice needs none. |
| MQTT (filtered PSK Reporter feed) as the heard source | Rejected for this design: SMD originates the decodes — routing them out through PSK Reporter's five-minute summary and a volunteer broker, back into our own system, is a two-hop detour to a delayed subset of data we hold first-hand. Retained in §11 for the one thing it can do that we cannot: stations not running SM. |
| Inferring "on air" from decode uploads | Rejected as a correctness matter: a backlog drain would impersonate liveness. On-air derives from authenticated heartbeats plus snapshot state only (§3.3). |
| Routing presence through the QSO upload queue | Rejected: forever-retry semantics are exactly wrong for current state; a stale snapshot delivered late is worse than none (§6.1). |
| Public full-queue display in the pilot | Deferred: per-call lookup first; expand on observed behaviour (§12.1). |
| A public research data service / dumps | Not in this design; with it goes the entire deletion-vs-distributed-copies problem the earlier draft had to carry. |
