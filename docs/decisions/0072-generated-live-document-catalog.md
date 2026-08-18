---
number: 0072
title: Generate the live-document catalog and GitHub documentation map
status: Accepted
date: 2026-08-18
---

# 0072 — Generate the live-document catalog and GitHub documentation map

## Context

`docs/README.md` is both the implementation-document map and the page reached
from the repository's public GitHub README. It has been maintained by hand,
including a growing list of live references, temporary work records, and
historical directories. That makes the routing prose readable but leaves no
machine-checkable answer to three questions: whether a registered live path
still exists, which canonical document owns a topic, and whether the GitHub
view agrees with the routing data used by contributors and agents.

The OSS maintainability plan requires a small live-document catalog, lookup by
topic or applicable code path, and a compact generated `docs/README.md`. It
also requires records to remain cold rather than enumerating every ADR, review,
report, or session note as live context.

## Decision

Keep the live-document metadata in `docs/catalog.json`. Each entry has a stable
ID, repository-relative path, class, audiences, topics, applicable code scopes,
and one-line summary. The allowed classes are Kernel, Current, Canonical, Work
item, and Operator. Operator is separate because the embedded manual is live
guidance but is not implementation context.

A standard-library Go command under `cmd/docscatalog` owns three operations:

- generate the whole `docs/README.md` GitHub view from the catalog;
- check catalog integrity and exact agreement with the committed generated
  README; and
- find matching live documents from a topic or repository code path without
  reading their contents.

Taskfile targets expose those operations. The normal CI gate runs the check.
Canonical topic names have exactly one owner; path lookup may return that
canonical reference plus an applicable scoped Kernel or selected Work item.

Records are deliberately absent from the live entries. The generated README
routes `docs/decisions/`, `docs/reviews/`, `docs/reports/`, `docs/v1-analysis/`,
most of `docs/v2-design/`, and the retained history files by directory
convention. A live exception inside a normally historical directory—currently
the API and configuration references—is listed explicitly in the catalog.

`docs/README.md` is generated in full rather than carrying generated markers
inside a handwritten file. That keeps the public view and the lookup source
from becoming two partially overlapping authorities.

## Alternatives considered

### Keep `docs/README.md` handwritten and add validation around it

Rejected. Markdown prose does not provide stable IDs, structured code scopes,
or an unambiguous topic-owner relation without building a second parser and a
second set of conventions. The catalog and README could still drift.

### Put catalog front matter in every document

Rejected for this slice. It would require touching many unrelated live files,
make directory records participate in routing mechanics, and complicate
directory entries such as the operator manual. One small manifest makes the
live set explicit and keeps record files cold.

### Keep a manifest but leave the GitHub map handwritten

Rejected. That creates two sources of truth at the exact public boundary the
catalog is meant to repair.

### Add full-text, indexed, or vector search

Rejected. Topic/path metadata plus normal repository search is sufficient for
this corpus and avoids a service, index lifecycle, and new dependency surface.

## Consequences

- Adding, moving, or reclassifying a live document requires a catalog edit and
  regeneration of `docs/README.md`.
- GitHub readers get an audience, class, and canonical-topic view generated
  from the same data used by local lookup.
- CI catches missing live paths, duplicate IDs, ambiguous canonical topics,
  invalid metadata, and stale generated output.
- Historical records remain discoverable through conventions and direct links
  without becoming automatic or catalog-wide context.
- This decision does not decompose the backlog or dogfood inbox; their current
  live entries are transitional until that separate maintainability slice.
