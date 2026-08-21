# Decisions

Append-only log of architectural and design decisions made for Station Manager. One file per decision — numbered, dated, with a status field. The point is not to capture *what was decided* (the code shows that); it's to capture **the alternatives considered, why each lost, and what would change the answer**, so future revisits don't re-derive the analysis from scratch.

This is not a substitute for `docs/v2-design/` — those are *forward-looking design documents* covering how a subsystem is shaped. ADRs are the **reasoning trail**: shorter, more decision-focused, and chronologically ordered.

## When to write an ADR

Write one when:

- A choice has multiple plausible options and you've actually weighed them.
- The decision will be revisited if the situation changes.
- "Why did we do it this way?" is a question someone (you, in three months) might ask.

Don't write one for:

- Routine code-level choices ("use a slice not a map here") — those belong in commit messages or code comments.
- Decisions with one obvious answer.
- Decisions captured exhaustively elsewhere — link to the existing doc instead.

## Naming

`NNNN-kebab-case-title.md`. Number is zero-padded to four digits and assigned at write time (next-available). The number gives chronological order without filename timestamps. The kebab-case title makes filenames greppable by topic (`grep -l toolkit decisions/`).

## Lifecycle

A decision's `status` field walks through these values:

- **Proposed** — written but not yet committed to. Use this when capturing a recommendation before the decision is made; flip to Accepted when committed.
- **Accepted** — committed to and in effect. Most ADRs should land here.
- **Deprecated** — no longer recommended but not actively replaced. Rare; usually decisions get superseded instead.
- **Superseded by NNNN** — replaced by a later ADR. The newer ADR explains why.

When a decision changes:

1. Don't edit the old ADR's body — it's a historical record.
2. Update only the old ADR's `status` to `Superseded by NNNN` and add a note pointing forward.
3. Write a new ADR explaining the change. The new ADR's "Context" section explains what changed since the old one.

## Format

Use `template.md` as a starting point. The template prescribes five sections:

- **Context** — what situation forced the decision.
- **Decision** — what was chosen, in one or two sentences.
- **Alternatives considered** — each rejected option with the specific reason it lost. This is the section that earns the ADR's keep.
- **Consequences** — what now follows from this choice (good and bad).
- **Triggers to revisit** — concrete signals that would make us reopen the decision. If you can't name any, the decision probably wasn't really under tension and may not need an ADR.

Keep ADRs short — one page is the target, two pages the ceiling. If you need more, you're writing a design doc; put the design in `docs/v2-design/` and let the ADR link to it.

## Cross-references

- `docs/v1-analysis/design-decisions-log.md` — *retrospective* keep/change/delete verdicts on v1 shape decisions. The forward-looking equivalent for v2 lives here.
- `docs/v1-analysis/invariants.md` — load-bearing rules that any decision must respect.
- `docs/v2-design/` — design documents that ADRs may reference for the long form.
- `docs/current.md` — bounded current state and next action, not a decision log.
- `docs/session-handoff.md` — route to retired session history, not a decision log.
