---
number: NNNN
title: <short imperative title>
status: Proposed
date: YYYY-MM-DD
---

# NNNN — <title>

## Context

What situation forced this decision? What was true at the time of writing that made the question worth answering? Constraints, requirements, prior decisions, observed pain. Two or three short paragraphs at most.

## Decision

One or two sentences naming what was chosen. The "what" only — leave the "why" to the alternatives section.

## Alternatives considered

For each option that was on the table:

### <option name>

What it would have looked like, and the specific reason it lost. Be concrete: "rejected because X" beats "rejected because it didn't fit." If an option was barely considered, say so — that's still useful information for a future reader trying to figure out whether to reopen the question.

## Consequences

What follows from this choice — both the things you signed up for (good) and the things you accepted (cost). Avoid platitudes ("more flexibility"); name specifics ("daemon binary grows by ~2 MB to embed the SPA").

## Triggers to revisit

Concrete signals that would make us reopen this decision. Examples:

- "If <X observable thing> happens, reconsider <option Y>."
- "If <constraint A> is removed, <option B> becomes preferable."
- "If we onboard a second operator, the single-user assumption breaks and Y matters."

If you can't name any triggers, the decision may not be under genuine tension — consider whether an ADR is the right artifact, or whether this belongs in a design doc / commit message / code comment instead.

## References

- Related ADRs (numbered).
- Design documents in `docs/v2-design/`.
- Specific files/commits if this decision is anchored to code.
