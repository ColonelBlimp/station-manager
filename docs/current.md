# Current work

Updated: 2026-08-29

- **Goal:** W-0008 (harden audited contract boundaries). Slice 1 (Persistence) DONE — PT-3/4/5/6 shipped + pushed (PT-6 = `f5a74422`). **Slice 2 (Config) DONE — CC-3 shipped `f7d1de4a` (CI-green); CC-4 done (uncommitted until directed).** Slice 3 (API wire) next. W-0001/W-0003/W-0004/W-0005 closed. [`docs/backlog.md`](backlog.md) owns priority.
- **State:** CC-4 done: all three `Service` update primitives (`Update`/`UpdateIfChanged`/`UpdateInMemoryThenPersist`) enforce `Normalize→Validate` on the fresh under-lock candidate before any commit (rulings A1 keep `Update` unconditional / B1 validate all three / validate `UpdateIfChanged` before its Diff), rejecting with a typed `*ValidationError`; `UpdateInMemoryThenPersist` still commits memory once the candidate validates even if persistence fails. RED + reversion-proved (fixed 2 PT-6 tests' invalid `New(Config{})` base); whole-tree `go test`/`vet`, gofmt, golangci, observatory, `-race` green.
- **Next:** **Slice 3 (API wire) review package** — AW-2/AW-3/AW-4/AW-5/AW-6/AW-1 (AW-3/AW-6/AW-1 need operator decisions); present AC + nearest-confusable per finding, HOLD for approval. [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
