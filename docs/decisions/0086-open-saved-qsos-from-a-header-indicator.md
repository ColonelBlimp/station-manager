---
number: 0086
title: Open saved QSOs from a header indicator
status: Accepted
date: 2026-09-30
---

# 0086 — Open saved QSOs from a header indicator

## Context

The saved-QSO strip preserves access to unfinished work, but pushes every page
down and grows with each record. The operator has proposed a compact indicator
opening an overlay. Saved QSOs belong to this browser; Station Events describes
station history and cannot provide access to another browser's saved work.

Acceptance is persistent access to every saved record on every page, without
moving the working surface. Closing a panel or dismissing an announcement must
leave the record available. The nearest confusable outcome is a dismissed
message being treated as a discarded QSO.

## Decision

Replace the strip with a **Saved QSOs (N)** control inside the header, beside
Logbook, opening an overlay panel; use the same control in the Map toolbar.
Announce newly preserved work once after its reload with an ordinary toast,
without opening the panel automatically.

This replaces the inbox's 2026-09-30 placement ruling, "immediately below the
header on every page". ADR 0085's preservation and recovery decisions remain
accepted. This ADR authorises the UI change; it does not claim it is built.

## Alternatives considered

- **Keep the strip:** offers immediate visibility, but permanently consumes
  vertical space and grows with pending work.
- **Use a sticky toast as the record's home:** ADR 0008's message-only,
  dismissible, bounded toast stack cannot own recovery actions or lasting
  access. A transient announcement can point to the persistent control.
- **Float the pill below the header:** avoids reflow but covers working content;
  an existing header slot gives it a stable location.
- **Open the panel once after reload:** makes discovery obvious but interrupts
  the newly loaded page. The ordinary toast announces without taking focus.
- **Move saved QSOs into Station Events:** mixes browser-local unfinished work
  with per-archive server history. Other browsers cannot open these records,
  and the active archive may differ from their source.

## Consequences

- Show the count while records exist, including records from inactive archives
  and those with unknown logging outcomes. Keep the control accessible on narrow
  screens even when other header metadata is hidden. Loading failures must not
  masquerade as an empty collection.
- The panel owns the list, source identities, details, Copy and confirmed
  Discard. Closing it preserves every record. Keyboard dismissal returns focus
  to its trigger and must not clear a Phone / CW draft underneath.
- Announce once in the tab that reloads after successfully preserving new work.
  Use the existing ordinary toast duration. Later reloads and cross-tab list
  refreshes do not repeat that announcement. Wording must retain any unknown
  logging outcome; never assert "not logged" for an uncertain submission.
- Committed saves and discards must update other open tabs' counts and lists;
  returning to a tab reconciles missed changes. An older read must not resurrect
  a discarded entry. Notifications are refresh hints, not exclusive ownership
  for the future Restore action.
- Slice 2 uses this panel for recovery. From another page, offer navigation to
  Phone / CW; Restore itself acts there under ADR 0085's original archive,
  logbook UUID, empty-form and exclusive-ownership checks. Navigation never
  switches archives automatically.
- The failed-save full-screen gate, Retry save, confirmed Discard and reload,
  and reachable stop/disarm controls retain their existing responsibilities.
  Update the manual with the UI implementation, not ahead of it.

## Triggers to revisit

- Operators repeatedly miss saved work despite the count and announcement.
- The control cannot remain accessible in the header at supported narrow sizes.
- Recovery becomes shared across browsers, changing the ownership boundary.

## References

- [ADR 0085: preserve drafts with their original archive](0085-preserve-qso-drafts-with-their-original-archive.md).
- [Dogfood inbox](../dogfood-inbox.md), 2026-09-30 saved-QSO placement discussion.
