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
// OFF the /v1/config API (the handler strips them; GET reports password_set,
// never the value), and WriteJSON writes the file 0600 — enforced on every
// write, tightening a legacy 0644, never loosening.
//
// Encryption at rest was assessed 2026-07-07 and REJECTED — plaintext + 0600 +
// API-stripping is the accepted posture. The daemon must hold the plaintext to
// use these secrets (SMTP AUTH, provider logins), so a local decryption key
// would sit on the same disk, readable by the same user, next to the
// ciphertext: anyone who can read config.json can equally read the key or the
// daemon's memory. That is obfuscation, not protection, and the added failure
// modes (lost key = lost config) plus first-run ceremony are a real cost for a
// single-operator local daemon. The layers that do the work: file permissions
// (this package), API redaction (the handler), revocable app-specific
// passwords (operator practice — rotate on any suspected disclosure), and
// full-disk encryption for the stolen-machine case (an OS concern, not SM's).
// If a multi-user-host deployment ever appears, the upgrade path is opt-in
// systemd LoadCredential= (TPM-bound via systemd-creds), not a homegrown
// key-beside-ciphertext scheme.
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
