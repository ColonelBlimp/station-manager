// Package types defines the domain types shared across the entire project.
//
// # Import constraint
//
// This package imports ONLY the Go standard library. This is a load-bearing
// invariant: because every other internal package imports types, any
// non-stdlib dependency added here becomes a transitive dependency of the
// whole project. Push external dependencies outward into the consumer
// package instead.
//
// # ADIF alignment
//
// [Qso] and its embedded sub-structs ([QsoDetails], [ContactedStation],
// [LoggingStation], [Qsl]) are shaped to mirror the ADIF specification.
// Every ADIF tag the software supports has a corresponding field; the
// nesting is a Go-level organizational convenience (ADIF itself is flat).
// Adding a new ADIF field should be a one-line change to one of these
// structs, and the storage layer carries it through automatically via the
// additional_data JSON blob pattern (see internal/database/sqlite/adapters).
//
// # Struct tag conventions
//
// Several struct tags appear on types in this package. They are inert
// metadata here — the consuming packages interpret them at runtime:
//
//   - json:"..."         Standard encoding/json tags. Used everywhere.
//   - validate:"..."     go-playground/validator tags. Interpreted by
//     internal/database/sqlite/validation.go (and
//     eventually internal/config) at struct-validation
//     time. Custom validators "band" and "mode" are
//     registered in internal/adif.
//   - hamnut:"..."        Field mapping tags for the hamnut enrichment
//     client's reflection-based lookup. Present on
//     [Country] fields. Consumed by the enrichment
//     layer (not yet ported to v2).
//   - boil:"...,bind"    sqlboiler ORM binding tags. Present on
//     [QsoUpload] and [ContactHistory]. The ORM choice
//     is undecided for v2; these tags remain while
//     sqlboiler is carried forward.
package types
