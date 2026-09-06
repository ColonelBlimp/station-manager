# W-0008 — Harden audited persistence, configuration, API, and frontend contracts

**Status:** Open — AW-1's alpha.3 removal gate and the frontend-wire slice remain
**Selected:** Slice 3, AW-1 — alpha.2 compatibility complete; alpha.3 removal is release-gated
**Outcome:** Verified P2 contract defects fail loudly or preserve the operator's intended state;
ambiguous outcomes are represented honestly instead of being reported as definite success or
failure.

`W-0008` is an immutable identity. Its internal order is the inherited order from the 2026-08-20
audit reconciliation; [`docs/backlog.md`](../backlog.md) alone ranks this dossier against others.

## Ordered slices

1. **Persistence:** PT-3 session-email stamps only the revision actually sent and reports the true
   emailed set; PT-4 partial logbook PATCH applies an atomic field-level partial update (writes only
   the present members; no revision-conflict response — the logbook row has no revision column); PT-5 import fallback does
   not proceed after an unverified rollback; PT-6/CC-5 config replacement gains unique temporary
   files and crash-durability, after the operator decides how to report a post-rename directory
   sync failure.
2. **Configuration:** CC-3 extends semantic validation to `datastore` and `logging`; CC-4 makes the
   persistence primitives enforce the intended normalize-then-validate contract. CC-2 belongs to
   [W-0006](../archive/work/W-0006-reject-unknown-config-keys.md), not here.
   CC-5 (alpha.2 dogfood Finding #6, backlog P1 #1): at load, reconcile only the identifiable
   alpha.1-generated legacy shape — a pre-version-3 document whose `qrzcq` forwarder carries the
   exact ordered slice `["insert","update","delete"]` alpha.1's omitted-filter path emitted — to
   the literal `["insert"]`; any other explicit unsupported action, including a permutation of
   those values or the same content in a version-3 document, stays rejected
   (`RegisterSupportedActions` contract, `TestValidateForwarders_RejectsUnsupportedAction`). No
   reconciliation-specific record; covered by the existing one-time schema-migration record.
   Built 2026-09-06 as `migrateAlpha1QrzcqFilter` inside the v2→v3 migration. Evidence: RED first
   (`migrate_v3_qrzcq_test.go` pins 1 and 4 failed with the filter unchanged and the exact dogfood
   refusal), GREEN, then a reversion proof with the step stashed — pins 1 and 4 failed for the
   claimed reason while the rejection pins 5 and 6 still passed; review round 1 tightened the
   matcher from set to ordered equality after the permutation pins (2b, 6b) went RED against the
   set matcher. CC-6 (Finding #1): `smd
   config-check` runs `config.Load` plus construction of every enabled forwarder — exactly that, not
   the whole daemon start — so a policy or validation refusal surfaces in the dogfood preflight rather
   than at the upgrade restart; the value-free finding helper is shared from `internal/config`
   (`ForwarderStartupFinding`) by the PUT handler and the command, and the command discards the raw
   constructor cause. Built 2026-09-06. Evidence: RED first (`config_check_test.go` pins 1–3 — the
   two dogfood shapes passed the key-only check and the success text claimed nothing about
   loading), GREEN, then a reversion proof with the two new stages reverted in place — pins 1–3 failed
   for the claimed reason while the read-only and unknown-key pins held.
3. **API wire:** AW-2 requires presence-aware command booleans; AW-3 gives genuine no-op QSO PATCH
   an operator-decided outcome; AW-5/AW-6 bound and sanitize SM Cloud ingest; AW-4 makes `/v1/`
   404/405 responses JSON; AW-1 is the separately designed integer-ID to UUID compatibility
   migration.
4. **Frontend wire:** F-03 validates success/safety response shapes before use; F-04 represents an
   ambiguous transport timeout as outcome-unknown and provides a reconciliation path.

## Decisions required

- AW-3: `200` no-op or a client error.
- AW-6: maximum SM Cloud batch rows.
- AW-1: compatibility and removal release.
- PT-6: operator-visible treatment of a failure after rename but before directory sync completes.

## AW-1 alpha.3 removal checklist

AW-1 stays **OPEN** until this lands. The alpha.2 compatibility phase completed on 2026-08-30:
`qso_uuid` is present at every daemon event boundary, public and cloud QSO projections prevent
accidental persistence-shape leakage, and the SPA keys QSO state on `uuid`. Alpha.2
(`v2.0.0-alpha.2`) deliberately retains the deprecated daemon-local numeric QSO identities for one
release; alpha.3 (`v2.0.0-alpha.3`) removes them in one coherent code, test, and canonical-reference
change.

The nearest confusable outcome is removing every numeric `id`. Do **not** remove the SQLite
`qso.id` primary key or `types.Qso.ID`, the public numeric `logbook_id`, or the upload queue row's
own `id`; none of those is the deprecated external QSO identity.

- [ ] Legacy `qso_id` in the `qso.*` + `forward.*` SSE payloads (`internal/events/events.go`) — deprecated in alpha.2.
- [ ] `qso_id` in the `forward.failed` durable notification detail (`internal/forwarding/worker/forward_failed_notification.go`) — deprecated in alpha.2.
- [ ] `SubmitResult.ID` — the `POST /v1/qso` response `id` (`internal/qsoservice/service.go`).
- [ ] `ContactHistory.ID` — the `GET /v1/contact-history` item `id` (`internal/types/history.go`).
- [ ] The transitional `id` retained in the public API QSO projection (GET/PATCH/list), plus the SPA's deprecated optional `LogbookQso.id`; keep the internal `types.Qso.ID` storage field.
- [ ] `qso_id` in `GET /v1/qso/{uuid}/uploads` items (`types.QsoUpload`) — out of alpha.2 code scope; marked deprecated in the reference now. (The upload row's own `id` is a separate queue identity and stays.)
- [ ] SPA: remove `QsoEventPayload.qso_id` and the alpha.2 `qso_id`-only event-decode fallback; require a non-empty `qso_uuid` with numeric `logbook_id` (`frontend/app/src/lib/api/log-events.ts`).
- [ ] Update `docs/v2-design/api-endpoints.md` in the same change: make the affected shapes UUID-only and remove every alpha.2 deprecation/removal note without changing `logbook_id` or queue-row identity.
- [ ] Resolve the temporary alpha.2 SM Cloud queue-drain step in `docs/dogfood-acceptance.md`: remove it only if no pre-alpha.2 daemon remains a supported upgrade source; otherwise state the supported-upgrade boundary explicitly.
- [ ] Add a dated ADR 0016 update recording the completed removal and preserving the internal-id distinctions above.
- [ ] Pin the UUID-only event, submit, contact-history, QSO projection, upload-status, and SPA decode shapes with tests; run focused checks plus `task docs:check` and `task ci:local`.

Alpha.3 acceptance: daemon QSO events and HTTP responses expose UUID as the only QSO identity; the
bundled SPA continues to select, edit, re-enrich, and respond to QSO events by UUID; SM Cloud
payloads remain free of daemon-local ids; logbook routing and upload-queue row identity are
unchanged. The alpha.2 daemon/SPA already provide the migration window for external consumers.

## Verification boundary

One finding or deliberately coupled pair per behavior change, RED first and reversion-proved. Tests
must distinguish missing from explicit `false`, stale from current revision, committed from
outcome-unknown, and rollback-confirmed from rollback-unverified. Update the canonical API or config
reference in the same change as the behavior it describes.

## References

- [`internal-persistence-transaction-audit.md`](../reviews/internal-persistence-transaction-audit.md)
- [`internal-configuration-contract-audit.md`](../reviews/internal-configuration-contract-audit.md)
- [`internal-api-wire-contract-audit.md`](../reviews/internal-api-wire-contract-audit.md)
- [`frontend-app-review.md`](../reviews/frontend-app-review.md)
- Expanded pre-decomposition notes: `d0391ed7:docs/backlog.md`.
