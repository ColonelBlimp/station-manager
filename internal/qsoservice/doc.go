// Package qsoservice is the daemon's domain service layer.
//
// It holds the business logic that coordinates QSO ingest, enrichment,
// storage, and forwarding — the responsibilities that lived in the v1
// apps/logging/backend/facade package. The HTTP handlers in internal/api
// are thin wrappers over this package.
//
// Invariants this package must uphold (see docs/v1-analysis/invariants.md):
//
//   - Enrichment never blocks logging. External lookups (hamnut, QRZ)
//     degrade the QSO draft when they fail; they never prevent the
//     operator from logging.
//   - One-fails-all-fail for QSO writes. The QSO row and its upload-queue
//     row(s) are atomic in a single transaction; cache/enrichment writes
//     are best-effort outside the transaction.
//   - Narrow daemon scope. This package owns log + forward only. Rig
//     control, CAT, PTT, audio, and capture UX live elsewhere.
package qsoservice
