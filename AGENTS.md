# Station Manager project instructions

This is the tool-neutral instruction kernel for contributors and coding agents. Keep it short:
rules and routing belong here; chronology, active-work detail, and subsystem explanations do not.

## Find the right context

- `main` is the v2 daemon codebase. The `v1` branch is frozen except for user-reported v1 fixes.
- Start with [`docs/README.md`](docs/README.md), the authoritative documentation map. Read only the
  canonical document relevant to the task; do not load the documentation tree wholesale.
- For current work, read only `docs/session-handoff.md`'s `## Now` section unless deeper history is
  necessary. `docs/backlog.md` owns priority; the handoff must not re-rank it.
- Before a non-trivial design decision, consult `docs/v1-analysis/invariants.md` and
  `docs/v1-analysis/lessons-for-v2.md`. Record genuinely weighed alternatives in an ADR under
  `docs/decisions/`.
- Code is authoritative when it disagrees with documentation. Only Tier 1 documents are maintained
  as current truth; ADRs, reviews, session history, and most `docs/v2-design/` files are records.
- `docs/v2-design/api-endpoints.md` and `docs/v2-design/config.md` are the canonical current HTTP and
  configuration references and change with their corresponding code.
- When working in `internal/bridge` or `internal/ft8`, also read that directory's scoped
  `CLAUDE.md` until those files complete their separate migration to generic scoped instructions.

## Safety and external claims

- Never initiate a live transmission, keyed test, rig command, or hardware-dependent experiment
  without the operator's explicit agreement for that occasion. Prefer offline and passive evidence.
- FT8 sessions are operator-initiated. The daemon may continue an operator-started run, but it does
  not begin one independently. Do not describe the system as "attended-only": the enforced presence
  signal is an open FT8 event subscription, not proof that a person remains at the desk. A decode is
  not a QSO; only a completed exchange is.
- `tx_on` and `tx_off` are never exposed through the generic command surface. Tune and FT8 keying
  share the single guaranteed-stop/single-flight controller; only that controller keys TX.
- Claims about rigs, CAT, operating systems, PipeWire, third-party services, or other external
  systems require a citation, source line, or measurement. Label an unverified inference as such.
  Investigate in this order: repository search, authoritative documentation, passive observation,
  then keyed transmission. Read the whole relevant source rather than stopping at the first match.
- Never commit credentials, tokens, operator data, `.env`, or real configuration secrets.

## Load-bearing architecture

- Enrichment never blocks logging. External lookup failure degrades completeness; only a broken
  local database may prevent a QSO from being logged.
- QSO and upload-queue writes are atomic: one failure rolls back all transaction-owned writes.
  Cache and enrichment writes remain best-effort outside that transaction.
- Preserve package boundaries. `internal/storage` and `internal/forwarder` must not import
  `internal/bridge`, and the bridge must not import them. Keep FT8 isolated from `qsoservice`,
  `config`, and `adif`; assembly and submission belong at injected boundaries.
- `internal/types` imports only the standard library. Reuse canonical `types.X` shapes instead of
  creating parallel request/config structs that duplicate them. `types.Qso` mirrors ADIF; do not
  flatten or reshape it without checking the project invariants.
- Lifecycle changes follow ADR 0070 and `docs/v2-design/lifecycle.md`. Do not introduce another
  lifecycle framework or revive hand-wired shutdown ordering.
- Prefer specific concrete implementations over speculative frameworks. A little duplication is
  cheaper than a generic abstraction without several real consumers.

## Change discipline

- Inspect the worktree before editing. Preserve unrelated and concurrent changes; never stage or
  commit them. Do not use destructive Git commands to clear work you did not create.
- Make the smallest coherent change. Do not mix dependency upgrades, formatting sweeps, generated
  output, or opportunistic cleanup into an unrelated task.
- State operator-observable acceptance criteria before choosing a mechanism. Name the nearest
  confusable outcome. Ask the operator to decide thresholds, timeouts, or policy judgments rather
  than inventing plausible values.
- Use TDD for behavior changes: write an informative failing assertion first, implement, then run a
  reversion proof showing the old/wrong behavior fails for the claimed reason. Verify the reversion
  actually applied and that the test reached its intended assertion.
- Tests must be as strong as the rule they claim to prove, and fixtures must make correct and
  incorrect behavior differ. Enumerate states and sequence boundaries created by the change.
- Preserve observable behavior with characterization tests before restructuring existing code.
- When a commit is requested, keep tests and implementation in one atomic, releasable commit. Do not
  amend or push unless the operator asks.

## Code conventions

- Follow standard Go idioms and `gofmt`. Keep functions focused, nesting shallow, and comments about
  intent or constraints rather than narrating mechanics.
- Use `internal/errors` for operation-tagged errors in packages on that error path:
  `errors.New(op).WithErr(err).WithMsg("context")`. Do not replace it casually with `fmt.Errorf`.
- Use `internal/iocdi` for dependency wiring. Register services with their stable `ServiceName`; do
  not add new hand-wired construction paths.
- Do not hand-edit generated files under `internal/database/sqlite/models/` or
  `internal/cloud/store/models/`.
- Runtime data paths use `utils.WorkingDir()`. Do not create parallel path-resolution rules.
- Svelte code uses Svelte 5 runes. Prettier owns formatting; every lint suppression needs an adjacent
  reason comment.
- Minimize dependencies and keep them GPL-3.0-only compatible. Dependency upgrades are separate
  changes. Do not replace deliberate local components (`iocdi`, CAT/serial, ADIF parsing) without a
  concrete project-specific reason. See `docs/licensing.md`.

## Testing and verification

- Prefer integration tests with real services and in-memory SQLite over mock-only interfaces. Use
  unit tests for pure parsing, math, conversion, and other value logic.
- Exercise failure and cancellation paths, especially for enrichment and lifecycle work. Use
  observable barriers or fake clocks instead of timing sleeps where practical.
- Go focused loop: `go test ./path/to/package`; add `-race` for concurrency changes.
- Frontend focused loop, from the affected SPA: `npm run lint`, `npm run format:check`,
  `npx svelte-check --fail-on-warnings`, and `npx vitest run`.
- Full local release gate: `task ci:local`. It is intentionally broader and slower; use it when the
  change is ready for repository-wide verification.
- Hardware, live credentials, destructive databases, and RF acceptance remain explicit opt-ins and
  are never part of an ordinary automated test command.

## Documentation maintenance

- Update a canonical reference in the same change as the behavior it documents. Do not "freshen"
  historical records to describe current code.
- Documentation should explain ownership, intent, constraints, and routing. Put task history and
  evidence in a work item or record, not in this automatic instruction kernel.
- Do not create another ranked worklist, documentation index, or current-state source. Use
  `docs/backlog.md`, `docs/README.md`, and the bounded current-session surface respectively.
