---
number: 0050
title: Replace modified_at with a per-row revision counter as the sync version marker
status: Proposed
date: 2026-07-18
---

# 0050 — Replace modified_at with a per-row revision counter as the sync version marker

## Context

The SM Cloud sync protocol (ADR 0040) orders writes by `modified_at`: the
cloud upsert guard applies a push only when `EXCLUDED.modified_at >=
qsos.modified_at`, and reconcile detects drift by hashing sorted
`(uuid, modified_at)` pairs (`internal/cloud/reconcile.Summary`,
`uuid|unixmicro` lines). The 2026-07-18 review round 3 (P1 finding) showed
this is not a safe version marker:

- **Local precision is seconds.** `trg_qso_set_modified_at` stamps
  `datetime('now')` (migration 0004), and both manifest readers truncate to
  seconds to agree with it. Two distinct edits of the same QSO within one
  second carry **equal** timestamps — the payloads differ, the version
  marker doesn't.
- **Equal timestamps make arrival order the tiebreak.** With `>=`,
  last-arrival wins. The upload-queue worker is serial and the forwarder
  pushes fetch-overlay (current row state at processing time), so worker
  pushes can't self-invert — but the hourly **Reconciler pushes from its own
  goroutine**. A reconcile push holding a stale fetch can land *after* a
  worker push for the same UUID in the same second: the cloud regresses to
  the older payload.
- **The regression is invisible forever.** Reconcile hashes only
  (uuid, modified_at); equal timestamps hash equal, so the diverged payload
  reads as `in_sync` until some future edit bumps the row.

The window is narrow (edit-during-reconcile plus same-second timing) but the
failure is silent backup corruption — exactly what SM Cloud exists to
prevent. Wall-clock time is also fundamentally non-monotonic (an NTP step
backward can make a newer edit look older at any precision), so this is a
protocol-shape problem, not a tuning problem. P1 is the cheapest moment to
change the protocol: one deployment, one tenant, and both ends already apply
schema migrations at boot.

## Decision

Add a **per-row monotonic revision counter** as the primary version marker
through the whole protocol: a `revision` column on the local `qso` table
bumped by the update trigger, carried on the push/export wire beside
`modified_at`, stored and guarded on in the cloud upsert
(`revision` first, `modified_at >=` as the tie fallback), and included in
the reconcile manifest and summary hash. `modified_at` stays for display,
recency semantics, and legacy-tie fallback; it stops being the ordering
authority.

## Alternatives considered

### Do nothing (accept the window)

The race needs a reconcile push and a worker push for the same UUID in the
same second with inverted delivery. Rare — but the consequence is a silent,
permanently-invisible divergence between the log and its backup, and the
whole point of the P1 workstream was "the backup can be trusted." Rejected:
low probability × unbounded silent damage is the wrong trade for a backup
system, and the fix is cheapest now.

### Flip the guard from `>=` to `>`

Strictly worse. Under the serial worker, push order = edit order, so the
`>=` last-arrival-wins tiebreak is what makes a legitimate same-second
second edit *converge*. With `>`, that second push is rejected, the cloud
pins the **stale** payload permanently, and reconcile still reads in-sync.
The reviewer and our own analysis independently reached this rejection.

### Sub-second local timestamps

Store `%f` fractions in the SQLite trigger and stop truncating. Shrinks the
tie window; doesn't close it (two trigger firings can share a millisecond),
and does nothing about clock steps — wall-clock is simply not a monotonic
sequence. It also ripples through every DATETIME parse/format site that
migration 0004 just stabilised, for a fix that is still probabilistic.
Rejected.

### Payload hash in the reconcile manifest (detect, then heal by re-push)

Add a payload digest per manifest entry; divergence at equal timestamps
becomes visible, and local-is-authoritative re-push heals it. Two kills:
(1) the cloud stores the *pushed bytes* verbatim while the local side would
re-marshal `types.Qso` fresh at hash time — byte-identical independent JSON
marshalling is not a sound invariant to build a protocol on (any field-order,
whitespace, or encoding drift re-flags the entire logbook every cycle);
(2) detection without ordering still can't say which side should win a
future tie — it patches the symptom, not the marker. Rejected, though it
influenced the decision to include `revision` in the summary hash (same
detection benefit, sound comparison).

### Lamport / vector clocks

The general solution for concurrent multi-writer replication. SM has exactly
one writer per logbook (the daemon; local is authoritative, the cloud never
originates edits), so causal ordering degenerates to a plain per-row counter.
Rejected as overbuilt — but this is the natural escalation path if a second
concurrent writer ever appears (see triggers).

## Consequences

The build, end to end (order matters only within each side; the guard's
fallback makes the two deploys order-independent):

- **Local (SQLite) migration 0005:** `ALTER TABLE qso ADD COLUMN revision
  INTEGER NOT NULL DEFAULT 0`, and recreate `trg_qso_set_modified_at` so the
  inner update also sets `revision = OLD.revision + 1`. The trigger's
  existing `WHEN NEW.modified_at = OLD.modified_at` guard stays — it also
  keeps the bump self-terminating. Existing rows start at 0. sqlboiler
  models regenerate; `adapters.QsoModelToType` maps the column;
  `FetchQsoManifestWithContext` adds it to manifest entries.
- **Restore preserves revision:** `InsertRestoredQsoWithContext` writes it
  explicitly (INSERT fires no update trigger, same as `modified_at` today),
  so a restored logbook resumes the sequence instead of restarting at 0.
  **Restore becomes the only sanctioned same-UUID recovery path** — an
  out-of-band re-import that reuses old UUIDs but resets revision to 0 would
  push as "stale" against a cloud holding higher revisions. (Fresh-UUID
  imports, like the 2026-07-16 QRZ rebuild, are unaffected — new UUIDs never
  collide.)
- **Wire:** the push envelope and `ExportQso` gain `revision` beside
  `modified_at`. Absent field decodes as 0 = legacy behaviour.
- **Cloud migration 0003:** `revision BIGINT NOT NULL DEFAULT 0` on `qsos`;
  the upsert guard becomes `EXCLUDED.revision > qsos.revision OR
  (EXCLUDED.revision = qsos.revision AND EXCLUDED.modified_at >=
  qsos.modified_at)`, still ANDed with the tenant guard. Equal-revision
  (all-legacy) rows keep today's exact semantics — this is what makes
  deployment ordering and legacy clients safe.
- **Reconcile:** `reconcile.Entry` gains `Revision`; the summary line
  becomes `uuid|unixmicro|revision`; `diffManifests` treats a revision
  mismatch as drift (local authoritative → push). Both ends import the
  shared package, so the formula can't half-change at compile time. During
  a deploy skew (one end old, one new) the hashes simply mismatch, reconcile
  flags full drift, and the re-push converges — wasteful once, never
  corrupting. Ship daemon + smcloud together (one commit, F44 RPM rebuild +
  local deploy) to avoid even that.

What this buys: the same-second regression window closes (distinct edits
always carry distinct, ordered revisions); same-second payload divergence
becomes *visible* to reconcile (revisions differ even when timestamps tie);
sync ordering survives NTP clock steps. What it costs: a field on
`types.Qso`/the wire that is storage-row bookkeeping rather than ADIF data
(same category as `modified_at`/`deleted_at`, and it rides the same
envelope), two boot-time migrations, and a sqlboiler regen.

## Triggers to revisit

- A second concurrent writer per logbook (multi-device operation, a cloud
  UI that edits rows) breaks the single-writer assumption a scalar counter
  relies on — reopen the vector-clock alternative.
- If any tooling outside `smd restore` ever needs to write same-UUID rows
  (e.g. a future ADIF re-import that preserves UUIDs), it must learn to
  fetch-and-continue the revision sequence first, or it will be rejected as
  stale by design.

## References

- ADR 0040 — SM Cloud architecture (phased plan, reconcile soundness).
- `docs/v2-design/sm-cloud-p1.md` — the P1 build this amends.
- 2026-07-18 review round 3, finding 1 (P1) — recorded in
  `docs/backlog.md` "smcloud hardening — pre-Phase-2 gate" and
  `docs/session-handoff.md` Session 220/221.
- Code anchors: `internal/cloud/store/store.go` (upsert guard),
  `internal/cloud/reconcile/reconcile.go` (summary hash),
  `internal/database/sqlite/migrations/log/0004_utc_timestamps.up.sql`
  (the trigger being amended), `internal/forwarding/smcloud/smcloud.go`
  (push envelope), `internal/database/sqlite/manifest.go` (local manifest +
  restore write).
