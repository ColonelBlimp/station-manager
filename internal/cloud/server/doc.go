// Package server is the SM Cloud HTTP API (ADR 0040 /
// docs/v2-design/sm-cloud-p1.md, step S2): the thin transport over
// internal/cloud/store that the daemon's smcloud forwarder (S3), reconcile
// routine (S4), and restore command (S5) talk to.
//
// # Routes
//
//	PUT  /v1/qsos                     batch upsert-by-UUID; deleted_at set = tombstone
//	PUT  /v1/evidence                 §5 evidence-sync batch: per-row outcomes (evidencewire);
//	                                  retention-slice supersession = tombstone-then-delete, and
//	                                  tombstones gate every kind's upsert (re-offer → tombstoned)
//	GET  /v1/logbooks                 the tenant's logbooks (id + name)
//	GET  /v1/logbooks/{id}/reconcile  {count, hash} over the live rows (reconcile.Summary)
//	GET  /v1/logbooks/{id}/manifest   the (uuid, modified_at, deleted) diff list
//	GET  /v1/export                   full-fidelity dump of everything the tenant owns
//	GET  /v1/health                   liveness + DB ping (unauthenticated)
//	GET  /v1/version                  build version (unauthenticated)
//
// # Wire contract
//
// The QSO payload on the wire is canonical types.Qso JSON, projected by the
// daemon to remove the daemon-local fields the cloud never reads (id,
// logbook_id, dedupe_key, csid, country_details.id, contact_history[].id — see
// smcloud.projectCloudQso). The server stores the projected bytes VERBATIM
// (json.RawMessage end to end): it unmarshals only to validate shape and
// extract the UUID, never re-marshals — so restore returns exactly what backup
// sent, byte for byte (UUID, HH:MM:SS seconds, additional_data intact). modified_at/revision/deleted_at ride an envelope beside the
// payload because they are storage-row facts (trigger-stamped locally), not
// ADIF fields. revision (ADR 0050) is the version marker the upsert guard
// orders on — revision first, modified_at breaking ties — because local
// modified_at is second-precision and cannot order same-second edits; an
// absent revision decodes as 0 (legacy client) and gets pure-timestamp
// semantics via the tie fallback.
//
// PUT bodies must be a SINGLE JSON document (trailing content is a 400), and
// every uploaded UUID must be a valid UUIDv7 — the store's uuid column would
// take any RFC 4122 value, but restore (qsoservice.Restore) admits only v7,
// and an accepted backup must be restorable. Validation runs before the
// EnsureLogbook side effect, so a rejected batch provisions nothing.
// /v1/export reads logbooks + records from ONE repeatable-read snapshot
// (store.ExportSnapshot), so a push landing mid-export can't produce QSOs
// whose logbook is missing from the same dump.
//
// # Auth
//
// Bearer token → tenant, constant-time compared. P1 provisions a single
// (token, tenant) pair at boot (trust-on-provisioning, ADR 0040 § Identity);
// the map shape means multi-tenant is data, not a rearchitecture. Everything
// except /v1/health and /v1/version requires auth; a per-logbook read on a
// logbook the tenant doesn't own is a 404 (not 403 — existence is not leaked).
//
// # Boundary
//
// Imports types + the cloud store/reconcile packages + internal/utils (UUID
// validation) + stdlib (log/slog for output — the service runs under
// systemd/journald, stderr is the sink).
// Nothing daemon-specific: no internal/api, no bridge, no storage — the
// package-boundary rule from cmd/smd/doc.go holds by import graph.
package server
