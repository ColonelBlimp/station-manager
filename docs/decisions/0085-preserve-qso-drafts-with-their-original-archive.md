---
number: 0085
title: Preserve QSO drafts with their original archive
status: Accepted
date: 2026-09-30
---

# 0085 — Preserve QSO drafts with their original archive

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
