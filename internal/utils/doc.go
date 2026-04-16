// Package utils provides small, pure utility functions shared across the
// project. It has no internal dependencies — only the Go standard library
// and (minimally) golang.org/x/net for character-set decoding.
//
// # Scope
//
// The package contains two categories of code:
//
// Path and process helpers — [WorkingDir], [AbsDirPathForExecutable],
// [ExecName], [PathExists]. These are consumed by daemon startup, logging,
// and sqlite initialization.
//
// Ham-radio domain helpers — ADIF date/time formatting and validation,
// frequency conversion, lat/long coordinate conversion, and DXCC entity
// lookup. These are pure functions with comprehensive test coverage;
// they will be consumed by the API layer and enrichment code as v2
// features land.
//
// Functions that were generic infrastructure (deep-copy, reflection-based
// field setters, FIFO queues, HTTP client factories, network error
// classifiers) were removed during the v2 review per the project's
// "build specific, not generic" principle.
package utils
