// Package config holds the daemon's runtime configuration — the single
// config.json the operator hand-edits and the daemon reads/rewrites.
//
// It started (milestone 1) as a deliberately minimal block — data dir, HTTP
// API binding, logging, sqlite location — and grew with its consumers. It now
// owns the full surface: forwarders, enrichment lookups, SMTP/email, the serial
// bridge, FT8 (capture + display + TX), rig profiles + QSL defaults + PSK
// Reporter, plus the shared validation (Validate → []Finding) and the
// version-stamped migration chain that runs at Load. The v1 aggregation shape
// (AppConfig, RequiredConfigs, OptionalConfigs) was deliberately NOT ported —
// that was a three-Wails-apps artefact.
//
// Secrets: config.json stores plaintext credentials (SMTP password,
// lookup-provider passwords, forwarder API keys). They are deliberately kept
// OFF the /v1/config API (the handler strips them), and WriteJSON writes the
// file 0600 so the on-disk copy isn't world/group-readable either. Encryption
// at rest is deferred pending a security assessment.
//
// First-run: the daemon's loadConfig (cmd/smd/main.go) calls WriteJSON to seed
// a default config.json when none exists — a discoverable, hand-editable file.
// Runtime updates from PUT /v1/config go through Service.Update, which applies
// the change to a deep Clone, persists atomically via WriteJSON (temp-file +
// rename), and swaps the in-memory config in only after the disk write succeeds.
//
// See docs/v2-design/config.md (the canonical config reference) and
// docs/v1-analysis/lessons-for-v2.md → "Enumerate all API surfaces before
// designing any of them" — the same principle applies to configuration.
package config
