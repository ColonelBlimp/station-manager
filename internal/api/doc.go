// Package api holds the daemon's HTTP handler layer.
//
// Handlers in this package are thin wrappers over internal/qsoservice.
// They parse requests (JSON or raw ADIF bodies), call into qsoservice,
// and marshal responses. Transport concerns live here; domain logic does
// not. No business rules in this package — those belong in qsoservice.
//
// The HTTP API is the real compatibility boundary between smd and any
// client that talks to it. Versioning happens here (URL prefixes like
// /v1/), not in the shared source code. See docs/v2-design/structure.md
// → "Source sharing and wire compatibility are separate axes" and (once
// written) docs/v2-design/api.md.
package api
