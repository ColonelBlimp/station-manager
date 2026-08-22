# Current work

Updated: 2026-08-22

- **Goal:** Hand off the closed W-0006 configuration-safety slice and return work selection to the ranked backlog.
- **State:** W-0006 is implemented and verified: supported-version unknown keys are rejected before any write, struct-slice paths are covered, retired version-2 keys migrate through schema version 3, no-op startup is write-free, and `smd config-check` provides read-only preflight. The dossier is archived; [ADR 0074](decisions/0074-reject-unknown-config-keys-before-any-write.md), [ADR 0075](decisions/0075-migrate-retired-keys-before-unknown-key-rejection.md), and [`config.md`](v2-design/config.md) hold the durable decisions and current contract.
- **Next:** Select further work only from [`backlog.md`](backlog.md); W-0007 is the next ranked P1. The isolated `internal/evidence` `SQLITE_BUSY` observation is a separate flaky-test candidate, not part of W-0006.
- **Decisions not to revisit:** Reject after recognised migration and before typed decode; maps and `json.RawMessage` stay opaque; migrated documents persist once under a path-only reason; `docs/backlog.md` alone owns priority.
- **Do not:** mix the evidence flake, dependency upgrades, ADR 0070 lifecycle work, or unrelated cleanup into W-0006; initiate RF/hardware actions; amend or push without operator direction.
- **Relevant files:** [`config reference`](v2-design/config.md), [`ADR 0074`](decisions/0074-reject-unknown-config-keys-before-any-write.md), [`ADR 0075`](decisions/0075-migrate-retired-keys-before-unknown-key-rejection.md), [`archived dossier`](archive/work/W-0006-reject-unknown-config-keys.md), [`backlog`](backlog.md).
- **Coordination:** Keep W-0006 as one atomic code, test, canonical-documentation, ADR, and closure commit; no push is requested.
