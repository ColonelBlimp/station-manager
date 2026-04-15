// Package main is the entry point for smd, the Station Manager daemon.
//
// smd owns the local SQLite QSO database and forwards logged QSOs to
// configured online services. It exposes an HTTP API over a Unix domain
// socket; clients (Wails desktop apps, wsjtx-bridge, importer-style CLIs)
// submit QSOs and query state through that API.
//
// Scope is deliberately narrow: log + forward, nothing else. Rig control,
// CAT, PTT, audio, FT8 protocol decoding, and capture UX all live in
// separate client or bridge processes that talk to smd over HTTP.
//
// See docs/v1-analysis/invariants.md and docs/v2-design/structure.md for
// the full design.
package main
