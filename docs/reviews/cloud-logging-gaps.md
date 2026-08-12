# URGENT TODO — `cmd/smcloud` + `internal/cloud` logging gaps

**Status:** open · **Raised:** 2026-08-11 · **6 findings (C1–C6) + 1 client-side
cross-ref** · **Source:** logging review of the cloud service (`cmd/smcloud`,
`internal/cloud/server`) and the daemon-side evidence client, operator-directed,
review only — no code was changed. Filed from `.codex-reviews/20260811-001.txt`.

**This is the SIXTH package area, and it was NOT part of the 2026-08-01 five-package
sweep** ([api](api-logging-gaps.md), [bridge](bridge-logging-gaps.md),
[forwarding](forwarding-logging-gaps.md), [ft8](ft8-logging-gaps.md),
[qsoservice](qsoservice-logging-gaps.md)). The SHIP-GATE reconciliation of 2026-08-11
covered only those five; the cloud side had never been audited on this axis. So none
of these overlap the reconciled open sets — this is genuinely new material.

**Verify file:line before building.** The pointers below were transcribed from the
2026-08-11 audit and spot-checked the same day (`evidence.go:44` and `gzip.go:37`
confirmed verbatim), but this is a transcription of an external audit, not a
line-by-line re-review. Re-confirm the cite before you implement, per the
verify-before-building discipline — cloud code drifts independently of `main`.

---

## The axis used

Same as the five siblings: **can the operator tell this apart from the nearest
confusable state?** The two High findings are exactly false-success-vs-real-success
confusions on the wire — the highest-value gaps. C3–C6 are missing-context /
missing-provenance.

The existing logging is otherwise sensible: QSO writes include received/applied
counts, export aborts are logged, database-health failures are recorded, and the
Caddy config explicitly strips credentials from the access log.

---

## Findings

### [High] C1 — Evidence outcomes are silent

`internal/cloud/server/evidence.go:44` logs **only** the batch-storage failure
(`s.log.Error("evidence batch failed", …)`). Permanent rejects, digest conflicts,
tombstones, and missing profiles all return **HTTP 200 with no server log**. The
daemon-side client then quarantines the rejects without logging them
(`internal/evidence/sync.go:607`), and the reverse-proxy sees only a successful 200.
**Confusable state:** a batch that was fully accepted vs one where records were
rejected/tombstoned — indistinguishable at every hop. This is the load-bearing gap:
the whole point of the evidence pipeline is knowing what the far end did with a record.

### [High] C2 — Gzip can produce a false success log

`internal/cloud/server/gzip.go:37` defers `gz.Close()`, and `Close` is what flushes
buffered data + writes the gzip footer. But the export is logged as success at
`internal/cloud/server/server.go:476` **before** that deferred flush runs, so a
truncated / footer-less response can coexist with a "success" record.
**Confusable state:** a complete export vs a truncated one — the success line claims
the former for both.

### [Medium] C3 — Startup and migrations lack a reliable audit trail

`cmd/smcloud/main.go:397` and around: the base logger does not carry the build
version; startup failures are printed as **unstructured stderr** (not through the
structured logger); migrations record **no applied-version / duration**; `"starting"`
is logged **before** `net.Listen` (so a bind failure looks like a clean start that
then vanished); and there is no `"ready"` or `"stopped"` marker.
**Confusable state:** "started and serving" vs "logged starting, then failed to bind".

### [Medium] C4 — Application and access logs cannot be correlated

No request-ID / application-request middleware in `internal/cloud/server/server.go:74`.
Caddy supplies the VPS access log but it cannot connect a specific access entry to an
application error or tenant. **Direct-LAN deployments have no access log at all**
(`docs/smcloud-deploy.md:420`). **Confusable state:** which access-log line produced a
given application error — unrecoverable.

### [Medium] C5 — Several authenticated failures lack structured tenant context

Evidence failures omit tenant + batch size (`internal/cloud/server/evidence.go:46`);
logbook-list failures omit tenant (`internal/cloud/server/server.go:304`); pre-stream
export failures omit tenant (`internal/cloud/server/server.go:460`).
**Confusable state:** which tenant a failure belongs to — not present on the line.

### [Low] C6 — Standard-library HTTP diagnostics bypass slog

`cmd/smcloud/main.go:454` sets no `http.Server.ErrorLog`, so recovered panics and
transport diagnostics go to the default unstructured stderr logger. Journald still
captures them, but without the version or the consistent structured fields.

---

## Client-side cross-reference (already filed)

An `applied=0` stale-QSO acknowledgement is collapsed into ordinary success in
`internal/forwarding/smcloud/smcloud.go:309`. **Already recorded as F11** in
[`forwarding-logging-gaps.md`](forwarding-logging-gaps.md) — listed here only so the
cloud picture is complete; do not double-count it.

---

`backlog.md` owns the ranking; fold in and delete once shipped.
