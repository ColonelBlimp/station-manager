# Backlog — definitive ranked worklist

This is the only authority for priority. [`current.md`](current.md) names the active slice but never
re-ranks this list. Raw observations enter [`dogfood-inbox.md`](dogfood-inbox.md); selected work uses
one dossier under [`work/`](work/). Closed dossiers move to `archive/work/` and disappear from this
index.

Each `W-NNNN` identity is permanent; priority and status are not. Open a dossier only when selecting
that work. The expanded pre-decomposition backlog is preserved in Git at
`d0391ed7:docs/backlog.md` and is a historical record, not current truth.

## Release programme

Get to the next shippable state for 7Q8AC: clear P0 and P1 before opening a new P2 workstream. The
external operator is offline-first; theming and new product work are not ship gates.

## P0 — correctness and safety

None open. The reduced FT8 type-4 ladder is code-complete but still has the operator-initiated
on-air validation gate in W-0002.

## P1 — verified correctness, in order

1. **W-0008 CC-5 · OPEN — reconcile the alpha.1-generated qrzcq `action_filter` at load** (alpha.2 dogfood
   Finding #6, B1-01 WAIVED until the next candidate). Narrow by ruling: only a `qrzcq` forwarder whose stored
   filter is exactly the historical `[insert, update, delete]` default is normalised to the registered set,
   with one log record; explicit unsupported actions stay rejected. Tests prove both cases.

Conditional: alpha.2 Finding #7 (false TX alarm at bridge open, W-0011) becomes P1 #2 once passively reproduced.
W-0007's four findings (equal-version SM Cloud conflict, partial station baseline, concurrent QSO delete,
re-enrichment generation race) are all closed (2026-08-22).

## P2 — next workstreams

Open one workstream per active focus.

1. **W-0002 · validation gate · OPEN — [Validate the reduced FT8 type-4 ladder on air](work/W-0002-ft8-type4-on-air-validation.md).** Confirm one operator-initiated completed exchange with a real nonstandard station; RF action always requires agreement for that occasion.
2. **W-0008 · OPEN — [Harden audited contract boundaries](work/W-0008-harden-audited-contract-boundaries.md).** Ordered persistence, configuration, API-wire, and frontend-wire correctness slices.
3. **W-0010 · OPEN — [Improve forwarding, data, and synchronization reliability](work/W-0010-forwarding-data-and-sync-reliability.md).** Token rotation, duplicate-QSO resolution, legal bulk backfill, idempotent outcomes, and bounded reconciliation.
4. **W-0012 · OPEN — [Complete routed operator-experience follow-ups](work/W-0012-operator-experience-followups.md).** UI, map, onboarding, and diagnostic improvements not owned by W-0004.

## P3 — deferred or trigger-bound

1. **W-0011 · DEFERRED — [FT8 and rig refinements](work/W-0011-ft8-and-rig-refinements.md).** Pick only a concrete operator-recognized problem; preserve TX safety and operator initiation.
2. **W-0013 · DEFERRED — [Infrastructure and integration seams](work/W-0013-infrastructure-and-integration-followups.md).** SSE, hardware enumeration, IoC, multi-tab ownership, UDP compatibility, and multi-instance migration work require a consumer or trigger.
3. **W-0009 · ADJACENT-WORK — [Maintainability audit residuals](work/W-0009-maintainability-audit-residuals.md).** Tighten verified package, build, test, and browser gaps without a broad sweep.
4. **W-0015 · ADJACENT-WORK — [Logging observability residuals](work/W-0015-logging-observability-residuals.md).** Three low-urgency items remain after the logging audit closed.
5. **W-0016 · TRIGGER-BOUND — [Sanitize PSK Reporter string fields](work/W-0016-sanitize-pskreporter-string-fields.md).** Await adjacent work or demonstrated harm, plus the operator's strip-versus-space decision.
6. **W-0017 · ADJACENT-WORK — [De-flake CI-observed environment-sensitive tests](work/W-0017-deflake-bridge-sse-streaming-test.md).** Two sub-items of one flake class: the bridge streaming-startup barrier and the evidence concurrent-SQLite lock. Fix each deterministically (observable barrier; busy handling/retry), not with bigger timeouts.

## Designed or parked — not queued

- **W-0014 · PARKED — [Deferred product workstreams](work/W-0014-deferred-product-workstreams.md).** Discovery inventory only; each member needs go-ahead and its own design/dossier before implementation.
- **FT8 Field Day UI:** blocked until the relevant contest; not a 7Q8AC ship concern.
- **Daemon-initiated FT8 sequencing:** out of scope. Sessions remain operator-initiated; an open event subscription is the presence signal, not proof that a person remains at the desk.
- **Design our own sequencing/timing:** future thinking, not selected work.

## Maintenance rules

- No struck or resolved items remain here, including nested substeps; remove completed substeps once
  their durable outcome exists in a dossier, `archive/work/`, or the preserved Git record.
- Do not add evidence, alternatives, acceptance criteria, incident chronology, or implementation
  diaries to this index. Put them in the linked dossier, an ADR, or a cold record.
- Keep this file at or below 10 KiB. New open work gets the next immutable `W-NNNN` and one terse
  ranked row.
