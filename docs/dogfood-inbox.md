# Dogfood inbox

Raw, untriaged operational observations land here briefly. This is not a history file or a second
backlog.

## Inbox

_Empty as of 2026-08-21._

## Triage rule

Every top-level capture leaves this file once investigated:

- a real, not-now outcome becomes a stable-ID dossier ranked in [`backlog.md`](backlog.md);
- selected work moves into [`current.md`](current.md) and its dossier;
- fixed, duplicate, working-as-designed, or ruled-out observations disappear once their durable
  outcome exists in code, a canonical reference, an ADR, or Git history.

The final 193 KB mixed inbox/history snapshot is preserved at
`d0391ed7:docs/dogfood-inbox.md`. Its two unstruck end entries were already triaged during this
decomposition: WSJT-X-compatible UDP broadcast routes to
[W-0013](work/W-0013-infrastructure-and-integration-followups.md), and the 2026-08-10 stale-decode
visual collision routes to [W-0011](work/W-0011-ft8-and-rig-refinements.md). Earlier unstruck lines
inside incident narratives were explanatory bullets, not unresolved captures.

## Capture format

```text
- [YYYY-MM-DD HH:MM local] Observable symptom and operating context.
  Evidence collected; unknowns kept explicit; no proposed mechanism unless verified.
```

Never paste credentials, operator data, or uncontrolled log dumps here. RF, tune, CAT-write, and
hardware experiments still require the operator's explicit agreement for that occasion.
