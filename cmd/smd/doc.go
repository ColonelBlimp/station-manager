// Package main is the entry point for smd, the Station Manager daemon.
//
// smd owns the local SQLite QSO database, forwards logged QSOs to
// configured online services, runs the callsign-enrichment pipeline
// (hamnut country provider + QRZ chain provider), serves the optional
// SMTP mailer, hosts the rig-control bridge subsystem, and runs the
// opt-in FT8 decode subsystem (decode-is-not-a-QSO; logs heard signals
// only) in the default single-binary deployment. It exposes an HTTP API which
// — per ADR 0001 / ADR 0044 — also serves the embedded Svelte app SPA at
// `/app/` (with `GET /` redirecting there) when the listener is TCP and
// ServeSPA is enabled. The listener protocol is configurable: "tcp"
// (default, required for the embedded SPA) or "unix" for a Unix domain
// socket on a headless / split-host deployment.
//
// Scope (per the "narrow daemon scope" invariant — see
// docs/v1-analysis/invariants.md): the log + forward + enrichment
// subsystems do not couple with rig control, audio, FT8 protocol
// decoding, or capture UX. ADR 0013 reframes the original
// process-boundary rule as a package-import-graph rule:
// `internal/database/sqlite` and `internal/forwarding` must not import
// `internal/bridge`, and vice versa. The default single-binary
// deployment honours that boundary while running the bridge
// in-process; a separately-built `cmd/bridge` for split-host
// topologies remains opt-in (parked).
//
// The HTTP API is the single client surface. The Svelte app SPA
// (`frontend/app/`, embedded via `//go:embed`; ADR 0044) is the primary
// in-tree client; the legacy logging SPA that owned `/` was retired
// 2026-07-21. External integrations (importer-style CLIs, wsjtx-bridge)
// are deferred to milestone M3b.
//
// See `docs/v2-design/structure.md` for the deployment shape,
// `docs/v2-design/api.md` for the API surface,
// `docs/decisions/0001-ui-toolkit-browser-spa.md` for the SPA + embedding
// choice, `docs/decisions/0013-daemon-owns-bridge-as-subsystem.md` for
// the bridge boundary rule, and
// `docs/decisions/0017-enrichment-pipeline-domain-table-cache.md` for
// the lookup pipeline shape.
package main
