# W-0010 — Improve forwarding, data, and synchronization reliability

**Status:** Open — staged workstream
**Selected:** Not selected
**Outcome:** Upload recovery, synchronization, email, and duplicate handling preserve operator
intent and converge without routine full-log churn or forbidden third-party API use.

## Ordered outcomes

1. A rotated SM Cloud token does not permanently strand in-flight uploads as terminal failures;
   reconciliation and queue state converge together.
2. Accidental duplicate FT8 QSOs are detected and offered an explicit keep/merge/delete resolution;
   deliberate repeats remain loggable.
3. ClubLog historical backfill uses its bulk-upload contract rather than replaying old QSOs through
   the realtime endpoint. Until then, manual ADIF upload remains the supported history path.
4. Session email subject/body becomes configurable only after PT-3 in
   [W-0008](W-0008-harden-audited-contract-boundaries.md) makes stamps truthful; timed-out submit and
   email writes gain an idempotent, retrievable outcome.
5. FT8 session reconnect reconciles durable QSOs since session start without replaying stale QSOs
   into a fresh session.
6. Phase-2 synchronization may add bucketed range hashes only when whole-manifest bandwidth is a
   measured scaling problem; equal-version conflict correctness remains PT-1 in
   [W-0007](W-0007-close-verified-p1-correctness-findings.md).
7. Low-severity database/import cleanup and a second callsign provider are adjacent-work items, not
   standalone sweeps.
8. SM Cloud is served over https even on the LAN (operator decision 2026-09-05, alpha.2 dogfood
   Finding #1), so the `allow_insecure_http` cleartext acknowledgement can be removed from the live
   station config; `docs/smcloud-deploy.md` owns the TLS steps.

9. A credential rejection at any forwarder does not strand its rows as terminal failures that read as a
   live backlog (alpha.2 dogfood Finding #19, 2026-09-12: one QRZ insert failed on 2026-08-06 during an
   invalid-key window, never retried after the key was corrected, and sat on the Forwarding card as
   "1 queued" for five weeks). Operator-observable outcomes: the Settings → Forwarding card shows failed
   rows apart from rows waiting to send and names the QSOs or links to the logbook's missing-from filter;
   a row failed for an authentication reason is re-armed when that forwarder's credential is changed via
   `PUT /v1/config` (or the card offers "retry failed"), sharing outcome 1's mechanism rather than an
   SM Cloud-only one; a terminal forwarding failure lands in the durable notification history (verify
   the category exists — unconfirmed 2026-09-12); and `qrz.classifyResponse` redacts the key QRZ echoes
   before the message reaches `last_error`, `smd.log` or the wire. Nearest confusable outcomes: a card
   that still counts `clearable` as "queued"; a blanket retry that re-sends rows QRZ rejected for a
   station-callsign mismatch (terminal for a different reason); a redaction that also strips the
   operator-readable reason. Same family, seen while planning the test row's removal: a delete-forward for
   a QSO whose insert never succeeded at an id-keyed destination (QRZ needs the prior `upstream_id`,
   `qrz.buildForm`) is today a NEW terminal failure rather than a no-op — the worker passes the empty id
   through by design so field-keyed deletes (ClubLog) stay reachable, so the skip belongs in the id-keyed
   forwarder, or at enqueue when that forwarder holds no successful insert for the QSO.

## Verification boundary

Fixtures must make retry, historical backfill, deliberate repeat, accidental duplicate, stale
session, and outcome-unknown states observably different. Enrichment and mirror failures remain
best-effort; QSO plus upload-queue writes remain atomic. Third-party credentials and network calls
are never required by ordinary automated tests.

## References

- [`ADR 0039`](../decisions/0039-forwarder-enabled-gates-enqueue-config-driven.md)
- [`ADR 0050`](../decisions/0050-sync-protocol-revision-counter.md)
- [`ADR 0052`](../decisions/0052-smcloud-identity-backup-first-passive-store.md)
- Expanded evidence and resolved substeps: `d0391ed7:docs/backlog.md`.
