# Current work

Updated: 2026-08-29

- **Goal:** W-0008 (harden audited contract boundaries). Slice 1 (Persistence) DONE — PT-3/4/5/6 shipped + pushed, review-clean (PT-6 = `f5a74422`, CI-green). **Slice 2 (Config): CC-3 (datastore/logging boundary validation) DONE; CC-4 next.** W-0001/W-0003/W-0004/W-0005 closed. [`docs/backlog.md`](backlog.md) owns priority.
- **State:** CC-3 done (uncommitted until directed): `Config.Validate` now validates the `datastore` and operational `logging` blocks with field-specific `invalid_datastore`/`invalid_logging` codes, mirroring the SQLite + logging consumers 1:1 (decision (b): separate validators + parity tests via `export_test.go`; no shared `types` rule, no I/O). RED + reversion-proved; whole-tree `go test`/`vet`, gofmt, golangci, observatory all green.
- **Next:** **CC-4 review package** — make `Service.Update`/`UpdateInMemoryThenPersist` enforce `Normalize→Validate` before commit (re-touches the PT-6 methods); present acceptance criteria + nearest-confusable + its sub-decisions (boot rewrite; a validating Update failing a previously-tolerated boot config), HOLD for approval. Then slices 3–4 (AW-3/AW-6/AW-1 need decisions). [`W-0017`](work/W-0017-deflake-bridge-sse-streaming-test.md) latent; W-0002 RF-gated.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private — no cross-package test seam.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`backlog`](backlog.md), [`W-0008`](work/W-0008-harden-audited-contract-boundaries.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md).
- **Coordination:** Leave committing and pushing to the operator; non-Markdown commits draw a codex review to triage.
