---
number: 0022
title: Forwarder enqueue is gated by config presence, not by the Enabled flag; retrospective backfill is operator-driven
status: Superseded in part by 0039
date: 2026-05-17
---

# 0022 — Forwarder enqueue gated by config presence; retrospective backfill is operator-driven

> **Superseded in part by [ADR 0039](0039-forwarder-enabled-gates-enqueue-config-driven.md) (2026-06-30).**
> The **enqueue rule** below (rules 1–2: enqueue by config *presence*, `enabled`
> as worker-lifecycle only, the queued-but-not-uploaded "suspended" state) is
> reversed by 0039 — once ADR 0038 made outages retry forever while enabled, the
> suspended state lost its only use case. Under 0039, `enabled` gates enqueue
> (disabled = don't queue) and config is non-sparse + config-driven. Rule 3 —
> **retrospective backfill is operator-driven, never automatic** — survives and
> is strengthened by 0039. The body below is the original record; read 0039 for
> current behaviour.

## Context

A dogfood incident surfaced a contradiction between the documented design and the actual code. The operator logged a QSO with 3B8IDX while the QRZ forwarder was disabled in `config.json`, then enabled QRZ and restarted the daemon. Nothing forwarded.

The root cause: `internal/qsoservice/forwarders.go`'s `shouldEnqueue` drops the row at submit time when `fc.Enabled` is false, so no `qso_upload` row was ever created for QRZ. Restarting the daemon spawns a worker, but the worker has nothing to drain. `internal/qsoservice/submit.go` (and the matching paths in `update.go` and `delete.go`) all rely on this filter.

The documented design says the opposite. `docs/v2-design/forwarding.md` §8 reads: *"Disabled forwarders are not constructed and their queue rows sit at `pending` until the operator re-enables them and restarts."* That sentence only makes sense if rows are created while the forwarder is disabled. The code and the doc disagree.

The bug is small to fix on its own — drop the `Enabled` check in `shouldEnqueue`. But that fix raises a follow-on question: what about QSOs that were logged before a forwarder existed in config at all? If the operator adds QRZ a year into operation, should the daemon retroactively queue a year's worth of insert rows? The dogfood incident is a small example of the same shape — 3B8IDX has no QRZ row regardless of what `shouldEnqueue` does today.

A further smell appears at first-run. The default config ships a QRZ forwarder entry with `enabled: false` and placeholder credentials. That entry exists so the operator has something to edit, not because the operator has chosen QRZ. Conflating "shipped with the binary" and "the operator has chosen this destination" is what makes the original 3B8IDX surprise possible in the first place.

## Decision

Three coupled rules, settled together:

1. **Enqueue is gated by config presence, not by `Enabled`.** A forwarder configured in `config.json` (even with `enabled: false`) accumulates `qso_upload` rows at submit/update/delete time. A forwarder not in `config.json` does not. Mechanically: `shouldEnqueue` filters only on `action_filter`; the `Enabled` check is removed.

2. **`Enabled` becomes purely a worker-lifecycle flag.** Disabled forwarders are not constructed at daemon start and no worker goroutine spawns for them; their rows accumulate at `pending`. Re-enable + restart spawns the worker, which drains the backlog on its first tick. This matches the wording already in `forwarding.md` §8.

3. **Retrospective backfill is explicit and operator-driven, owned by the logbook app.** When the operator adds a new forwarder to `config.json` after QSOs already exist, those pre-existing QSOs do *not* get auto-enqueued. To upload prior history to a newly-added destination, the operator selects QSOs in the logbook app and marks them for upload to that destination — the same primitive the logbook app needs anyway for managing QSL state, edit history, and bulk export. The daemon API endpoint that backs this UI is future work; the immediate dogfood gap is closed by a one-off SQL insert.

The default-config-ships-QRZ entry is removed as a corollary: the operator picks destinations at first-run (a future SPA setup affordance) or via My Station's Destinations sub-tab. Until that picker lands, an operator wanting QRZ adds the entry to `config.json` by hand. "Defined in config.json" then carries the meaning "the operator has chosen this destination," which is what the enqueue rule above relies on.

## Alternatives considered

### Status quo — `Enabled` gates enqueue

Keep `shouldEnqueue` as it is. Tell operators that enabling a forwarder only affects future QSOs and that pre-existing QSOs need manual intervention.

Rejected. The dogfood incident is a real operator surprise that the documented design explicitly says won't happen. Keeping the status quo means either rewriting the design doc to match the code (and accepting the surprise as intentional) or leaving the contradiction unresolved. Neither is good. The whole point of having design docs is that the code is supposed to honour them.

### Always enqueue + auto-backfill at daemon start

Drop the `Enabled` check AND, at daemon startup, scan for any QSOs that don't yet have a `qso_upload` row for each currently-configured forwarder and create one. Self-healing.

Rejected. This is the case where adding QRZ a year into operation suddenly queues a year of QSOs for upload. The operator may not want that — they may have logged QSOs they don't want shared, or they may want to upload only contest QSOs, or they may want to backfill a date range. Removing operator consent for retrospective upload is the wrong default for ham radio practice, where "which QSOs go to which destination" is genuinely a per-QSO judgement. The recovery for the 3B8IDX incident exists, but it's a one-line SQL fix run knowingly by the operator, not a daemon-side surprise on every startup.

### Always enqueue + auto-backfill when a forwarder is *added*

Drop the `Enabled` check AND, when a new forwarder entry is detected at startup that wasn't there before, backfill rows for it.

Rejected for the same reason as the previous alternative, plus an additional one: "added since last run" is fragile state. It depends on the daemon remembering what was in config.json at the previous start, which needs durable bookkeeping. The bookkeeping is more surface area than the feature is worth, and the underlying consent problem is unchanged.

### New API endpoint `POST /v1/qso/{id}/forward`

Keep current submit-time enqueue; add a daemon endpoint that the operator (or SPA) calls explicitly to queue a backfill row for a specific QSO + forwarder pair.

Partially kept, deferred to the logbook app. The shape — operator selects a QSO, indicates which destination, the daemon enqueues — is the right shape. But the *UI* for choosing which QSOs is logbook-app territory (per the existing "logging vs logbook scope" rule); the *bare daemon endpoint* in isolation is operator-hostile (no operator wants to curl per-QSO). Build the endpoint when the logbook app needs it, not before. Today's 3B8IDX recovery is a manual SQL row insert.

### Halfway-house — flag in `config.json` per forwarder for `backfill_on_add: true/false`

Make the auto-backfill behaviour operator-configurable per destination.

Rejected. The `Enabled` / `enqueue when defined` rule is already operator-configurable through the more direct mechanism of "add the forwarder when ready, then opt in to backfill via the logbook app." A `backfill_on_add` flag is a less precise version of the same control, and configuration that's never going to be exercised after first-run accumulates in `config.json` for no benefit.

## Consequences

**Signed up for:**

- **Code change is tiny.** `shouldEnqueue` drops the `Enabled` check (three lines). Three callers (`submit.go`, `update.go`, `delete.go`) continue to use it unchanged. No new infrastructure, no schema change.
- **Default config no longer ships a QRZ entry.** First-run delivers a `config.json` with `forwarders: []`. The operator adds destinations they actually want, either by hand for now or via a future setup affordance. This is a small first-run experience regression until the picker exists; the gain is that "defined in config.json" carries real operator intent.
- **`pending` rows accumulate for disabled forwarders.** This is the documented design, not a regression. If a forwarder is defined but never enabled, its rows sit at `pending` indefinitely. Cost is small (one row per QSO per disabled-forwarder × small operator volume), and the table is indexed for the worker's claim query anyway.
- **The logbook app must eventually own a "mark for upload to X" UI.** This is now load-bearing for retrospective backfill. The shape: a multi-select grid of QSOs, a destination picker, a confirm action that POSTs to a daemon endpoint that creates `qso_upload` rows with `action='insert'` for each (qso_id, forwarder_name) pair. Build this when the logbook app's first iteration lands; not before.
- **One-off recovery for 3B8IDX-style incidents is manual SQL until the logbook UI exists.** Acceptable because (a) the operator is the dogfooder, (b) these incidents should become rare once enqueue tracks config presence rather than `Enabled`, and (c) the "right" recovery affordance is the logbook UI itself.

**Accepted costs:**

- **The first-run picker is now load-bearing.** Without it, a new operator's `config.json` has no forwarders and they have no way to configure one short of editing the file. The picker is small future work, but it's now on the critical path for non-dogfooder onboarding (deferred far in the future per ADR 0016, but worth flagging).
- **Operators who *want* retrospective auto-upload have to do manual work.** Someone migrating from a different logger, who wants their full history pushed to QRZ on day one, has to use the logbook app's bulk-mark feature (when it exists) or hand-craft a bulk SQL insert. The trade-off is explicit consent over convenience.
- **The contradiction in the existing design doc gets resolved by aligning code with doc, not vice versa.** `docs/v2-design/forwarding.md` §8 already says the right thing; this ADR makes the code match. The doc may need light editing to make the "rows accumulate while disabled because they're created at submit time" mechanism explicit rather than implied.

**Gained:**

- **No operator surprise on the next 3B8IDX-shaped incident.** Enable + restart now picks up whatever was logged while disabled, exactly as the design doc said it would.
- **"Defined in config.json" becomes a meaningful operator intent signal.** Code can rely on it without a per-call `Enabled` check, and downstream features (per-destination action_filter changes, future destinations) don't need to re-derive the same enable-vs-present distinction.
- **Logbook-app scope gains a concrete near-term feature.** The logbook app was previously "future, vague." This decision pins one of its first features: bulk mark-for-upload. That clarifies what the logbook app's MVP needs to do.

## Triggers to revisit

- **Operator regularly forgets to backfill, and large quantities of historical QSOs sit un-uploaded long after a forwarder is added.** If "I added QRZ but my logbook didn't get pushed" becomes a repeated complaint, reconsider auto-backfill-on-add (with operator confirmation) as a default. Today this is purely speculative.
- **The logbook app's "mark for upload" UI proves harder to design than expected.** If the UX turns out to need a different shape than per-row marking (e.g., date-range filters, bulk-by-band rules), this ADR's "operator-driven backfill" doesn't change, but the mechanism it depends on does.
- **Multi-operator station becomes real (per ADR 0014's federation triggers).** When a master daemon receives QSOs from multiple local daemons, each with their own forwarder configs, "which destinations get this QSO" becomes a per-station-then-aggregated question. The current single-operator "I picked these destinations" assumption needs revisiting at that point.
- **A forwarder is added that requires retrospective context the QSO row doesn't carry.** E.g., a destination that needs a digest of the operator's whole log to seed a session. Today every forwarder is per-QSO; that may change.

## References

- ADR 0014 (`0014-upstream-forwarding-deferred.md`) — driver-shaped forwarder layer; this ADR refines how the layer's enqueue side decides what to enqueue.
- ADR 0016 (`0016-sm-cloud-deferred-with-prep.md`) — defers multi-tenant/cloud; the first-run picker future work named in this ADR sits in single-operator scope, not cloud.
- `docs/v2-design/forwarding.md` §6 (action_filter), §8 (re-enable + restart semantics) — the design doc this ADR aligns the code with.
- `docs/v2-design/forwarding-implementation.md` §4 — the implementation walkthrough that contains the `shouldEnqueue` snippet; needs an editing pass after the code change lands.
- `internal/qsoservice/forwarders.go` — where the code change lands.
- `internal/qsoservice/submit.go`, `update.go`, `delete.go` — callers; unchanged in shape, but their semantics shift.
- Memory `feedback_logging_vs_logbook_scope` — the rule this ADR leans on for "retrospective backfill is logbook-app territory, not logging-app."
