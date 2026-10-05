---
number: 0085
title: Preserve QSO drafts with their original archive
status: Superseded by 0087
date: 2026-09-30
---

# 0085 — Preserve QSO drafts with their original archive

> **Superseded 2026-10-05 by ADR 0087.** Archives switch only from Settings → Archives, gated on Activate with Discard-and-switch; unlogged work in other windows is stated as lost, and the browser-held recovery this ADR defines is removed. Read 0087 for the current decision.

## Context

The archive-activation item in the dogfood inbox identifies three ways to lose
an unlogged Phone / CW QSO: activation from the same tab, entry during the
restart wait, and a different tab reloading after discovering the switch.
The operator's 2026-09-28 ruling requires that last reload: a page bound to the
old archive cannot safely continue operating after the switch. The failed-save
exception adopted below keeps that page blocked until preservation succeeds or
the operator explicitly discards the draft.

The operator has adopted the three policy rulings recorded in the inbox.
Rules 1 and 2 are built; rule 3 is not implemented. Accepted describes the
policy, not completion. All four rule 3 mechanism decisions below are adopted
and authorised for implementation, including the narrow failed-save exception
to the earlier reload ruling.
Preserving entered work and choosing its logging destination are separate
decisions. The nearest confusable outcome is a recovered QSO silently targeting
the new archive's Default logbook, whose callsign may differ.

## Decision

Refuse same-tab activation over an unlogged QSO and prevent new entry during
activation. For another tab, preserve its draft through the normal reload
with its original archive and logbook identity, making it
recoverable when that archive is active again; do not implicitly transfer it
to the new Default logbook. If preservation fails or is unconfirmed, hold the
reload with operations blocked until saving succeeds or the operator explicitly
chooses Discard and reload.

## Alternatives considered

### Restore directly into the new archive's ordinary entry form (3A)

Preserves text and permits immediate logging, but changes the destination
without a separate choice. A notice alone is insufficient: the ordinary Log
action would now submit to a different archive and potentially a different
callsign. Retain preservation through reload, but keep the saved draft
associated with its source.

### Keep the old page blocked for manual copying (3B)

Avoids immediate loss but requires manual transcription and prevents normal
rebinding. Rejected as the normal recovery path; adopted only when saving fails
or cannot be confirmed, with the page still blocked.

### Discard the draft on reload

Keeps archive bindings correct but leaves the reported silent data loss intact.

## Consequences

- Both activation entry points refuse while any unlogged work exists, including
  partial or invalid entries. Check before confirmation and again afterwards,
  before starting activation. Suggested message: "You have an unlogged QSO on
  Phone / CW — log or clear it, then switch."
- During activation, entry controls and shortcuts cannot create or modify a
  draft. Show the switch-in-progress message in the form. A definite refusal
  restores editing; an uncertain outcome retains the existing gate. The
  full-screen block is not raised earlier.
- A tab discovering another archive preserves its work and reloads. Afterwards
  it names the source archive and says the QSO is saved but unlogged. New entry
  belongs to the active archive; the saved QSO remains available when switching
  back, without overwriting another draft or triggering the same-tab refusal
  merely because a saved draft exists for an inactive archive.
- Recovery must retain entered times and values. The current QsoDraft excludes
  live frequency, band and mode, so serializing that object alone cannot prove
  complete contact recovery. Implementation must address that context and
  failed persistence explicitly before claiming loss-free recovery. The
  decisions below select storage and define the failed-save reload exception.
- Recovery never logs automatically. A future explicit transfer action would
  need to name the destination archive, logbook and callsign.

## Rule 3 mechanism decisions

All four decisions are adopted. Rule 3 may be built in the two slices below;
no further adoption decision is required before implementation.

1. **Browser storage, with separate records.** Use IndexedDB, with stable
   archive UUID, logbook UUID and draft UUID; labels are display metadata.
   Keep two drafts from the same archive separately, even if their callsigns
   match. Repeated saving of one draft updates that record, not the whole
   archive's collection. Await transaction completion before reloading.
   IndexedDB supplies transactions and origin-scoped storage; tab-scoped
   sessionStorage is unsuitable for recovery after closing that tab (sources
   below). This is recovery in the same browser profile and origin, not a
   station-wide backup. Browser deletion or eviction is outside that guarantee.
   Restoration needs exclusive ownership so two tabs cannot independently
   restore and submit the same saved draft. Closing a tab must leave the
   saved record recoverable; do not invent a timeout that discards it.
2. **Preserve contact context.** Save frequency, band, resolved ADIF mode and
   submode alongside the entered fields, original times and source identity.
   Show these on recovery and use them for validation and submission without
   retuning the rig. Live mode changes must not replace recovered reports or
   restart the QSO clock. Preserve the original station/operator attribution
   used by the submission path as well. A reading acquired after reconnect is
   not proof of the original contact's settings: capture the last known
   context before rebind, label it as such, and require explicit correction
   when missing or uncertain. Do not silently substitute today's rig values.
3. **Explicit Restore and Discard.** Offer each saved record with its source
   archive/logbook, callsign and time. Restore only into an empty form, after
   proving the original archive and resolving the original logbook UUID;
   recheck after any asynchronous claim. Refuse if the destination cannot be
   resolved; never fall back to the current Default logbook. Restoring does
   not delete the saved copy: keep subsequent edits protected until confirmed
   logging or explicit discard. An uncertain submit remains unresolved and
   must not be presented as definitely unlogged or automatically retried.
   Clear/Escape on a recovered draft must not silently erase its saved copy.
4. **Failed saving holds the reload.** This narrowly revises the
   2026-09-28 ruling: normal successful preservation still reloads; a failed or
   unconfirmed save keeps the old page blocked. The notice must say the QSO
   could not be saved and expose its full contents for copying, with Retry
   save and an explicit destructive Discard and reload action. The existing
   overlay makes the app underneath inert, so merely retaining the form
   behind it is insufficient. All automatic reload paths and the overlay's
   Reload action must respect this state. Logging and new transmission remain
   blocked, while existing stop/disarm controls remain available. A successful
   retry resumes reload. Reloading first and warning afterwards cannot recover
   the lost data and may lose the warning too when storage is unavailable.

Acceptance must distinguish concurrent saves from overwriting, concurrent
restores from duplicate ownership, and saved context from live rig values.
Exercise tab closure and reload after Restore, occupied forms, missing source
logbooks, an uncertain submit, and a failed/aborted save followed by both Retry
and explicit Discard. Source logbooks with colliding local integer IDs must
not be mistaken for one another.

## Implementation slices

1. **Preserve before reload; hold on failed saving.** Capture the source-bound
   draft and contact context, save it in IndexedDB, await transaction
   completion, then reload and identify the saved draft's source archive.
   On failure, expose the full QSO in the blocking notice with Retry save and
   Discard and reload, retaining stop/disarm access. Every archive-rebind
   reload path, including Reload now, uses the same preservation decision.
   Also expose successfully saved contents for reading and copying after
   reload, so this commit is useful before Restore exists. Do not promise a
   Restore action until slice 2 provides it. This slice addresses loss during
   archive-rebind reloads; it is not the complete recovery workflow.
2. **Restore and Discard.** Add per-record actions on the original archive,
   exclusive restore ownership, source UUID resolution and empty-form checks
   after waits. Protect recovered edits and submit with the saved contact
   context; retain the saved record until confirmed logging or explicit
   discard, preserving uncertain submission outcomes without automatic retry.

No new Go field is needed merely to expose the source logbook UUID: the
existing GET /v1/config response joins a types.Logbook carrying `uuid`.
The SPA's fetchStationContext currently projects its numeric ID and name but
omits UUID. Carry the existing UUID through that projection during slice 1,
under the existing archive-identity boot bracket. Missing identity must not
fall back to a numeric ID or a label. Numeric IDs remain valid for local API
submission after resolving the saved UUID within the proven active archive.

## Slice 2 implementation assessment — 2026-10-01

The operator has approved exclusive per-record ownership across navigation,
with Clear/Escape saving recovered edits before emptying the form and releasing
the claim. The additional implementation rulings below, including pre-submit
persistence, are recorded as adopted in the 2026-10-01 inbox follow-up. These
describe the selected behavior, not implementation completion. The subsequent
attribution recommendation is distinguished below.

### Ownership and availability

Use an exclusive Web Lock named for the saved-record UUID, requested with
`ifAvailable: true`. Keep its callback pending while the tab owns the recovered
draft. A competing Restore refuses immediately; it never queues a later
restoration or steals ownership. Discard must acquire the same lock before
deleting an unowned record; the owning tab uses its existing claim. Re-read
the record and recheck the destination and empty form after acquisition.
`locks.query()` supplies display hints only: its snapshot cannot authorise a
mutation. The Web Locks specification releases locks during document unload
cleanup; application-controlled release waits for the final save.

Prefer this to a persistent IndexedDB lease: the lease would introduce expiry,
crash-recovery delays and stale-owner fencing policy without a selected need.
Detect actual Web Locks availability and handle acquisition errors. Where
exclusive ownership cannot be obtained, explain why Restore is unavailable
and retain reading/copying. Do not infer availability from the server bind:
Web Locks requires a secure browser context; loopback HTTP is potentially
trustworthy, ordinary LAN HTTP is not, and the page's actual origin determines
the context. Switching addresses does not migrate origin-bound saved records.
Sources: [Web Locks specification](https://w3c.github.io/web-locks/),
[query snapshots](https://developer.mozilla.org/en-US/docs/Web/API/LockManager/query),
[secure contexts](https://developer.mozilla.org/en-US/docs/Web/Security/Defenses/Secure_Contexts).

### Edits, release and contact context

- Save on each change without a debounce timer. Serialise writes, coalescing
  edits made during a write into the next latest snapshot. A failed write
  leaves visible unsaved work and a retry path. Clear/Escape keeps the form
  and claim until the final revision commits; older completions must not
  clear newer edits or resurrect a discarded/logged record.
- Automatic tab closure is not an application-controlled save barrier.
  Preserve the latest committed revision and warn about pending/failed
  writes on ordinary close/reload where possible. Do not promise an unload
  transaction will finish or that a crash preserves an uncommitted keystroke
  ([IndexedDB shutdown limits](https://developer.mozilla.org/en-US/docs/Web/API/IndexedDB_API/Using_IndexedDB#warning_about_browser_shutdown)).
- Always show recovered frequency, band, mode/submode and their provenance;
  require explicit confirmation or correction before Log on each Restore.
  Changing those values invalidates confirmation. Missing or inconsistent
  values require correction; confirmation cannot waive validation.
- Bypass only the live-rig readiness gate for the recovered submission.
  Archive identity, active Default logbook UUID, claim ownership, recovered
  context confirmation, field validation and submit-in-flight checks still
  apply. Reports are validated against the recovered mode. Live rig updates
  cannot refill reports or change recovered timestamps or contact context.
- Assemble the recovered submission using its saved/corrected context and
  original operator/grid. Prove the stored result, not merely the outgoing
  ADIF: the current service stamps STATION_CALLSIGN from the logbook, defaults
  an empty OPERATOR from current configuration, and stamps MY_RIG from the
  running rig. The saved record does not capture MY_RIG today. These existing
  server rules must be accounted for before claiming full historical
  attribution preservation; do not silently substitute a newly configured
  operator for an unknown original one
  ([submit service](../../internal/qsoservice/submit.go), Submit and prepareQso).

### Submission outcome and cleanup

The proposed delete-then-mark-logged fallback alone leaves a gap: after both
writes fail, the disk record could still say unlogged. The duplicate check is
not a saved-draft identity check. It hashes editable contact values and force
submits use a random key
([dedupe](../../internal/qsoservice/dedupe.go), ComputeDedupeKey;
[submit service](../../internal/qsoservice/submit.go), prepareQso).

Adopted refinement: while holding the claim, commit the exact attempted
submission and an unresolved outcome before sending its POST. If that commit
fails, retain the form and do not send this recovered submission. Subsequent
edit saves must preserve the unresolved attempt independently of editable
fields. After confirmed logging, delete the saved record; if deletion fails,
persist a terminal logged outcome with the returned QSO UUID. If both cleanup
writes fail, the pre-existing unresolved record survives, so a later tab must
check the original Logbook rather than see a definitely-unlogged QSO. The
current tab must still report the confirmed success accurately and offer
cleanup retry, never resubmission as cleanup.

A lost response, malformed success response, or tab closure during submission
retains the unresolved state. A definite refusal of the current attempt does
not settle an older uncertain attempt. No automatic retry or force-submit
resolves uncertainty. Distinguish this from server-side idempotency, which
would require its own API/storage design.

Acceptance must cover competing Restore/Discard, closure during a pending
edit or POST, an edit arriving during final save, failure to persist the
attempt (no POST), both post-success cleanup writes failing, and reloading
after each boundary. Use real browser contexts for ownership/closure proofs
and a stored-QSO assertion for recovered context and attribution.

### Attribution recommendation — 2026-10-01

Select the inbox's option 1: expose effective Phone / CW submit attribution in
the station-context read, preserve it with the saved draft, and refuse Restore
when the saved MY_RIG differs from the value the running submit service would
stamp. Show both values and retain the record for reading/copying. An
acknowledgement accepting today's different MY_RIG would change the contact's
attribution; leave that outside this recovery slice. Letting the client
override MY_RIG would bypass the server's authoritative stamping and is not
selected.

The projection must use the same resolution as Submit: the startup-pinned rig
identity with its current per-rig override, including an explicitly empty
override. Neither bridge.rig_name nor the pending default_rig_id is a
substitute. Include effective OPERATOR and MY_NAME (the latter can change with
the roster); save and submit their known values explicitly. Distinguish
known-empty values from unavailable attribution or an older saved record
without it. Never backfill missing historical attribution from today's
configuration. A known-empty field must not silently pick up a new server
default on recovery.

A successful context read is not a reservation. Check eligibility at Restore,
then enforce the expected attribution in the recovered-submit path on the
server using the same resolved values that will be stamped. The client states
an expectation; it does not choose MY_RIG. A change between the read and POST
must refuse before storing any QSO or upload row. Match the resolved ADIF value
exactly; this proves the value to be logged, not unique physical-rig identity.

Ship the Go/API prerequisite with api-endpoints.md before the Restore UI.
Characterize existing stamping first, then prove the projection agrees with
the stored QSO for a pending default-rig change, per-rig overrides and explicit
empty overrides, explicit versus default operators, and roster-derived names.
The recovered-submit guard also needs a read-to-submit change case that proves
refusal without writes. Existing records with missing attribution remain
readable/copyable; their missing values cannot satisfy a proven match.

### Outcome and discard recommendations — RS1–RS23 review, 2026-10-01

These recommendations answer the three open questions in the W-0021 acceptance
draft; they do not claim implementation or operator adoption.

1. **An unknown outcome permits Restore under the ordinary recovery gates.**
   Before another submit, require explicit confirmation that the operator has
   checked the original archive/logbook and the contact is not already logged.
   Cancellation sends nothing. This authorises one new attempt, not unattended
   retries, and does not turn the previous unknown outcome into a proven
   failure. Keep its attempted payload available independently of subsequent
   edits. A further unknown result requires a fresh check before another try.
   Refusing all recovery would strand a contact whose first request never
   committed; an ordinary unqualified Log would invite accidental duplication.
2. **A duplicate response keeps the form and saved record for an explicit
   decision.** Do not retire the saved record automatically against the returned
   UUID: the key ignores reports, notes, attribution and seconds within a minute
   ([ComputeDedupeKey](../../internal/qsoservice/dedupe.go)). Make that existing
   QSO available for checking. Retain an explicit Log anyway path only for the
   operator's decision that this is a separate contact; it is not a way to
   resolve uncertainty about whether the same contact was stored. A forced
   attempt still needs the claim, all recovered-submit gates and a committed
   attempt record. Cancel keeps all work. If the operator finds the same
   contact already logged, confirmed Discard retires only the browser copy.
3. **The owning tab may Discard directly with confirmation.** No prior Clear
   is required. Settle outstanding saves and prevent queued/late saves from
   recreating the record, then delete it. Clear the recovered form and release
   the claim only after deletion commits. Failure retains the form, record
   and claim; cancellation changes nothing. Disable Clear/Discard while a
   submit is actively in flight. An unknown past outcome does not prevent
   explicit discard, but its confirmation must retain the fact that a QSO may
   already exist; discarding never deletes anything in the Logbook.

The acceptance draft must also preserve v1 records' existing unknown outcomes,
send saved MY_NAME and MY_GRIDSQUARE as well as OPERATOR, and distinguish the
saved operator/name from today's defaults: only MY_RIG is unconditionally
server-stamped; the expectation compares the actual prepared QSO. Persist an
exact attempt, not just a boolean mark, and preserve older uncertainty through
a later definite refusal. Malformed success responses and closure during POST
belong to the unknown-outcome cases. A confirmed success is terminal in the
current tab even if both cleanup writes fail: show its UUID, disable submit,
and offer cleanup retry without sending another QSO.

### Browser verification and build boundaries — RS1–RS25 review, 2026-10-01

Use the scripted operator drill plus jsdom tests for this slice. Adding
Playwright, browser binaries and CI coverage is a separate tooling decision;
it would improve repeatability but is not required to begin the record-format
work. The jsdom lock/channel fakes prove application decisions and failure
handling, not actual browser ownership or tab-lifecycle behavior.

Keep the real-browser parts of RS8 and RS11 pending until observed on the
candidate. Use two windows/tabs in the same browser profile and at the same
origin, with synthetic saved records. Coordinate competing Restore attempts;
a second attempt only after an established claim does not exercise the race.
Record the single winner and unchanged losing form, refusal of foreign
Discard, retention across panel close and in-app navigation, and release on
both owner closure and reload with the latest committed revision preserved.
Retain the candidate, browser/version and observed results in the work item's
evidence; mocked results do not substitute for this drill.

The proposed four commits may separate record/panel preparation, claim/form
internals, edit/release handling, and recovered submission. Keep the public
Restore action unavailable until all four are integrated: an intermediate
commit must not expose a recovered form to ordinary Log, Clear or stack/load
paths. Alternatively, combine the dependent behavior into one releasable
commit. Each earlier commit must remain usable with the existing saved-QSO
read, copy and discard behavior.

## Attribution freshness contract — 2026-10-02

Settled by the operator after Codex review of `21feafae` (another client's config change
left a saved draft's attribution stale; a timed-out Station save could pair operator A with
attribution resolved for B).

- **Config events.** The daemon publishes `config.updated` on `/v1/events` once a config
  write has made a new config live — a durable or durability-uncertain write, and an
  in-memory change whose disk write failed — independently of whether the writer receives
  its response; never for a rejected write. The payload carries no config values.
- **Invalidation.** An event clears the page's attribution and invalidates any read in
  flight; a disconnect of the events stream clears it too; every proven reconnect re-reads
  it. The page's own config writes, read ordering and the archive-binding guard are kept.
- **Requested operator.** `GET /v1/submit-attribution?operator=` resolves exactly as a
  submit carrying that OPERATOR: present-but-empty is an empty submitted operator, so the
  `default_operator` fallback applies; absent keeps the original `logging_station.operator`
  behaviour.
- **Cache identity.** The page remembers the operator it REQUESTED, separately from the
  effective operator returned, and returns the attribution only for that operator. A change
  of the page's operator (startup context, Station save) invalidates and re-reads. Until a
  matching read completes, a save records attribution as missing — and saving still works.
- **Boundary.** SSE gives eventual invalidation, not instant knowledge of a remote write.
  The server's submit-time expectation check remains the final protection.

## Attribution retained across disconnect — 2026-10-05

Adopt option 1 from the S2.5 failure review: retain the last attribution read for
this page's requested operator when the events stream drops, marking it
unconfirmed. This supersedes only the disconnect-clears rule above. A config
write from this page, a `config.updated` event, or a change of requested operator
still clears the value immediately and invalidates reads in flight.

The two-window drill exposed an interaction missed on 2026-10-02: the window
holding the draft discovers another archive after a disconnect. Clearing its
attribution first means preservation always writes missing attribution, so the
app's own saved record cannot offer Restore. S2.5 is FAIL; an empty lock list
does not prove the ownership drill ran.

The retained value is available to the switch-triggered save, not as confirmed
current attribution for Restore. Disconnect invalidates in-flight reads so a
late response cannot replace it. Every proven same-archive reconnect re-reads;
a failed read leaves the retained value unconfirmed. An archive change cannot
refresh it from the new archive. No successful prior read, or a definite
invalidation without a successful replacement read, still means missing.
Known-empty values remain distinct from missing values. Existing records with
missing attribution are never backfilled.

The weighed alternative was to keep clearing on disconnect and leave records
from other windows read-and-copy only, bringing forward the parked recovery
redesign. Retention is selected because it gives the existing Restore path an
app-created record without changing destination or server stamping. A config
change missed during the outage can make the retained value stale. Restore
still checks the current rig attribution; Log sends the saved expectations,
and the server refuses a mismatch with `attribution_changed`, storing nothing
and leaving the form and record available.

Acceptance: exercise the real boot read → events drop → another archive →
preservation path, asserting the saved attribution and original identities;
then evaluate Restore with the original archive/logbook active again. Merely
showing a readable saved record is insufficient. Also cover late reads, failed
same-archive refresh, definite invalidation, and missing versus known-empty
values. RS8/RS11 still require a fresh record and the operator's browser drill;
the old `7Q7CT` record cannot be repaired with today's attribution.

## Triggers to revisit

Revisit if the operator wants to move unfinished contacts between archives,
or if logging to an inactive archive becomes a supported, separately designed
operation. Either changes the destination decision rather than the need to
preserve entered work.

## References

- [Dogfood inbox](../dogfood-inbox.md), archive-activation item dated 2026-09-28
  and proposals dated 2026-09-30.
- [ADR 0071](0071-first-class-qso-archives.md), archive isolation and reload.
- [Archive state](../../frontend/app/src/lib/config/archives.svelte.ts),
  activateArchive, requestReload and verifyArchiveGeneration.
- [Phone / CW draft](../../frontend/app/src/lib/operate/qso.svelte.ts),
  QsoDraft, draftInProgress and the QSO clock.
- [Submission assembly](../../frontend/app/src/main.ts), setSubmit.
- [Config response](../../internal/api/handler_config.go), the DefaultLogbook
  database join; [logbook type](../../internal/types/logbook.go), its UUID wire
  field; [station context](../../frontend/app/src/lib/api/seams.ts), the SPA
  projection that currently omits it.
- [MDN IndexedDB](https://developer.mozilla.org/en-US/docs/Web/API/IndexedDB_API),
  transactions, origin scope and storage eviction;
  [transaction completion](https://developer.mozilla.org/en-US/docs/Web/API/IDBTransaction),
  successful completion versus request success and durability limits.
- [MDN sessionStorage](https://developer.mozilla.org/en-US/docs/Web/API/Window/sessionStorage),
  tab lifetime and closure.
