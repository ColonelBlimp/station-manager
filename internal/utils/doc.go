// Package utils provides small, pure utility functions shared across the
// project. It has no internal dependencies — only the Go standard library
// and (minimally) golang.org/x/net for character-set decoding.
//
// # Scope
//
// The package contains two categories of code:
//
// Path and process helpers — [WorkingDir], [AbsDirPathForExecutable],
// [ExecName], [PathExists], [XDGDataDir]. These are consumed by daemon
// startup, logging, and sqlite initialization.
//
// Ham-radio domain helpers — ADIF date/time formatting and validation,
// frequency conversion, lat/long coordinate conversion, and DXCC entity
// lookup. These are pure functions with comprehensive test coverage,
// consumed by the API layer, qsoservice, and enrichment code.
//
// Outbound HTTP — [NewHTTPClient] builds the single, specifically-configured
// *http.Client the lookup providers and forwarders share (conservative
// timeouts, env proxy, default TLS).
//
// The earlier generic infrastructure (deep-copy, reflection-based field
// setters, FIFO queues, a generic HTTP-client *factory*, network error
// classifiers) was removed during the v2 review per the project's "build
// specific, not generic" principle. NewHTTPClient is the deliberate specific
// replacement for that factory, not a return to the generic shape.
package utils
