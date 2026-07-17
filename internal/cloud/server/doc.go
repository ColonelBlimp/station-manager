// Package server is the SM Cloud HTTP API (ADR 0040 /
// docs/v2-design/sm-cloud-p1.md, step S2): the thin transport over
// internal/cloud/store that the daemon's smcloud forwarder (S3), reconcile
// routine (S4), and restore command (S5) talk to.
//
// # Routes
//
//	PUT  /v1/qsos                     batch upsert-by-UUID; deleted_at set = tombstone
//	GET  /v1/logbooks                 the tenant's logbooks (id + name)
//	GET  /v1/logbooks/{id}/reconcile  {count, hash} over the live rows (reconcile.Summary)
//	GET  /v1/logbooks/{id}/manifest   the (uuid, modified_at, deleted) diff list
//	GET  /v1/export                   full-fidelity dump of everything the tenant owns
//	GET  /v1/health                   liveness + DB ping (unauthenticated)
//	GET  /v1/version                  build version (unauthenticated)
//
// # Wire contract
//
// The QSO payload on the wire IS types.Qso — same repo/module, so the contract
// holds at compile time (ADR 0040 § Codebase). The server stores the payload
// bytes VERBATIM (json.RawMessage end to end): it unmarshals only to validate
// shape and extract the UUID, never re-marshals — so restore returns exactly
// what backup sent, byte for byte (UUID, HH:MM:SS seconds, additional_data
// intact). modified_at/deleted_at ride an envelope beside the payload because
// they are storage-row facts (trigger-bumped locally), not ADIF fields.
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
// Imports types + the cloud store/reconcile packages + stdlib (log/slog for
// output — the service runs under systemd/journald, stderr is the sink).
// Nothing daemon-specific: no internal/api, no bridge, no storage — the
// package-boundary rule from cmd/smd/doc.go holds by import graph.
package server
