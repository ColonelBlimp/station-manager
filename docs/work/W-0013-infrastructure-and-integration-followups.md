# W-0013 — Harden deferred infrastructure and integration seams

**Status:** Deferred — trigger or adjacent work required
**Selected:** Not selected
**Outcome:** Deferred infrastructure changes land only when a real consumer opens the seam, with
bounded resource use and no accidental hardware, credential, or network dependency in normal tests.

## Inventory

- one multiplexed SPA event stream, when separate SSE ownership becomes a measured maintenance
  cost;
- `/v1/hardware` per-direction audio availability and bounded enumeration caching;
- CI-V `sets_state` value-compatibility validation;
- `internal/iocdi` concurrency and build-time contract hardening;
- multi-tab operating ownership and explicit takeover, building on the shipped awareness banner;
- opt-in WSJT-X-compatible UDP decode broadcast as an independent non-blocking decode sink, after
  the recipient/filtering contract is verified from authoritative protocol documentation;
- spot-submitter registry only when a second destination exists;
- config hot reload only as a deliberately scoped lifecycle consumer;
- before multi-instance SM Cloud: explicit migrate-only/serve-only operation and verification of
  concurrent migration locking. The single-instance boot migration remains current behavior.

## Verification boundary

Integration output must be bounded and non-blocking at its producer. Tests use local in-memory or
loopback fixtures and make dropped/slow consumers observable without delaying logging or decoding.
No ordinary command contacts an external service, opens operator hardware, or changes a live
database.

## References

- [`ADR 0040`](../decisions/0040-sm-cloud-p1-backup-restore.md)
- [`ADR 0034`](../decisions/0034-civ-codec-protocol-seam.md)
- Expanded rationale: `d0391ed7:docs/backlog.md` and `d0391ed7:docs/dogfood-inbox.md`.
