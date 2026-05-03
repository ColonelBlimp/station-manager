// Package config holds the daemon's runtime configuration.
//
// The v1 config package served three Wails apps with rig/CAT/audio/FT8/server
// concerns bolted on over time. This v2 rewrite is deliberately minimal: it
// covers what smd actually needs at milestone 1 — a data directory, a Unix
// socket path for the HTTP API, a logging configuration, and the sqlite
// datastore location.
//
// New configuration surfaces (forwarders, enrichment lookups, email, etc.)
// are added as their consumers come online in later milestones. Don't
// preemptively port v1's config aggregation (AppConfig, RequiredConfigs,
// OptionalConfigs); that shape was driven by three-Wails-apps needs that do
// not apply here.
//
// First-run behaviour: the daemon's loadConfig (cmd/smd/main.go) calls
// WriteJSON to seed a default config.json at the resolved candidate path
// when no file exists yet. This gives the operator a discoverable,
// hand-editable file on first launch. WriteJSON itself is package-private
// in the sense that it's only ever called by the daemon's startup path;
// run-time config updates from /v1/config (forthcoming) reuse the same
// helper for atomic rewrites.
//
// See docs/v2-design/structure.md and docs/v1-analysis/lessons-for-v2.md →
// "Enumerate all API surfaces before designing any of them" — the same
// principle applies to configuration.
package config
