---
number: 0094
title: Retain Session lists per archive within each tab
status: Accepted
date: 2026-10-22
---

# 0094 — Retain Session lists per archive within each tab

> **Update 2026-10-23:** B2-10 and B2-11 confirmed the failure against alpha.4, which is held.
> These implementation rulings were settled (Codex, at the operator's request). The design is in
> [W-0021](../work/W-0021-qso-archives.md), "alpha.4 hold: fix design".
>
> - **Legacy lists:** discard an unscoped list once the archive is proven, with a one-time notice
>   that Session history was reset and logged QSOs are intact. Rejected: claiming the rows for
>   the active archive (a guess), keeping only rows that resolve there (a boot-time lookup path
>   for a one-off upgrade), and a hidden unassigned bucket.
> - **Provenance:** every Session or engagement write carries the archive of the daemon that
>   produced it (an identity frame first on each FT8 stream connection; `archive_id` on the
>   submit response). It applies only when that equals the tab's proven binding. Rejected:
>   trusting the order of streams, or adopting a buffer on any later identity read, because an
>   FT8 reconnect can beat the stream that verifies the archive.
> - **Before binding:** an in-memory buffer whose records are released only by a proof of their
>   own archive. Rejected: an unscoped persistent pending bucket. Rows lost on a failed boot are
>   an accepted panel-history limitation; the QSOs remain durable.
> - **FT8 engagement state** is scoped per archive like the list, because it sets
>   `allow_duplicate`. Rejected: per tab.
> - **While binding is unproven,** Session edit, export and email, submit and FT8 transmit
>   admission are disabled.
> - **Request guard** (ruled in review of revision 2): once the archive is proven, those requests,
>   and the Logbook editor's `GET`/`PATCH`/`DELETE` of a QSO, carry `X-SM-Archive-Id`. The daemon
>   checks it against the archive it actually serves, before any side effect, and answers
>   409 `archive_mismatch`. The client then verifies and reloads, with no automatic retry.
>   Provenance on a response cannot stop an action reaching the wrong archive, and ADR 0087
>   retains that prevention requirement. Rejected: exempting UUID-addressed edits, because
>   `smd import` preserves UUIDs, so two archives can hold the same one. Stop, disarm and abandon
>   controls are never guarded.

## Context

The alpha.4 acceptance review confirmed that a reused browser tab retained a test QSO from a
previous daemon installation. Its Session list uses one `sm.session.qsos` store without archive
identity. An archive switch reloads the page, which hydrates that same list, while ordinary QSO
edit and session-export requests now resolve against the newly active archive. Export skips missing
UUIDs, so a mixed list can produce a successful download containing only some displayed rows.
FT8's "worked this session" check also consumes the list.

The cross-install carry-over was observed; the archive-switch consequences remain source-based
findings awaiting the B2 browser cases. During revision 2's A4-01 review, the operator explicitly
asked Codex to choose between retaining separate lists and resetting on every archive change.
This records that design ruling. It does not claim implementation, a browser test result, or
release acceptance.

## Decision

Retain a separate Session list for each archive within each browser tab, identified by the
archive's stable UUID. Switching selects that archive's list; returning restores its previous
list, and a reload or daemon restart serving the same archive preserves it.

## Observable contract

- The panel and rail count describe only the active archive's Session rows. An archive with no
  Session entries in this tab starts with an empty list, even if its durable logbook has contacts.
- Row edit, export, email selection and "worked this session" checks use only that selected list.
  Retained rows from an inactive archive are not offered against the active archive's API.
- A tab with two Home entries shows those two after a same-archive reload. After switching to
  another archive and logging one entry, its list and export contain that one entry. Returning to
  Home restores its two entries; edits saved there persist in Home.
- Switching does not delete or move durable QSOs, reconstruct a list from the entire logbook, or
  merge lists between tabs. Archive labels and daemon instance changes do not establish a new
  Session identity.

## Alternatives considered

### Reset the list on every archive change

This would prevent cross-archive use with less retained browser state. Rejected because visiting
another archive would discard the operator's review/export selection for the sitting, even when
they return immediately. Retaining it under its archive identity preserves useful work without
requiring inactive-archive API access.

### Keep one mixed list across archives

Rejected because its actionable rows do not share the daemon's active archive. Supporting them
would require explicit provenance and cross-archive action semantics that ordinary QSO routes do
not provide; silently skipping unresolved rows is not an acceptable substitute.

## Consequences

Implementation must establish the served archive identity before selecting or using a list, and
must cover every consumer above. Legacy unscoped browser rows need an explicit migration decision;
their ownership must not be guessed from whichever archive happens to be active. The timer's
semantics and W-0012's explicit "New session" action are not decided here.

This remains browser-owned state; ADR 0049's rejection of daemon-owned sessions stands. alpha.4
does not implement this contract. B2-09–11 must report their actual results against it; a code
change would create a new candidate under the release gate. No implementation is authorised by
this ADR.

## Triggers to revisit

- Operators require a deliberately combined session spanning archives, including its edit/export
  semantics.
- A separate session-lifecycle design changes how a tab's lists are explicitly ended or retained.

## References

- [alpha.4 acceptance record](../reports/dogfood-acceptance-v2.0.0-alpha.4.md), Finding #5 and B2-09–11.
- [ADR 0049](0049-daemon-owned-operating-sessions.md), rejected daemon-owned sessions.
- [ADR 0071](0071-first-class-qso-archives.md), archive identity and active-archive API boundaries.
- [ADR 0087](0087-switch-archives-only-from-settings-and-state-the-loss.md), switch and reload behavior.
- `frontend/app/src/lib/operate/session.svelte.ts` and `Ft8BandActivity.svelte`.
- `internal/api/handler_session_export.go`, `fetchSessionQsos`.
