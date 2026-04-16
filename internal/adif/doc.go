// Package adif handles parsing and serialisation of ADIF (Amateur Data
// Interchange Format) data.
//
// # Parsing
//
// [Parse] accepts raw ADIF bytes and returns a populated [Adif] struct.
// It is intentionally tolerant: unknown fields are ignored, tags are
// case-insensitive, and only string fields are supported. The parser
// splits on <EOH>/<EOR> markers and uses reflection over struct tags
// (adif:"..." with json:"..." as fallback) to populate [Record] fields.
//
// # Serialisation
//
// [Record.String] serialises a single record to ADIF format.
// [Adif.String] serialises a full file (header + records).
// [QsoToRecord] converts a [types.Qso] into a [Record] for export.
// [ComposeToAdifString] is a convenience that converts a [types.QsoSlice]
// into a complete ADIF string with header.
//
// # ADIF alignment
//
// [Record] embeds [types.QsoDetails], [types.ContactedStation], and
// [types.LoggingStation] directly so that their json tags serve as ADIF
// tag names. QSL and Station Manager user-defined fields use separate
// structs ([QslSection], [UserDef]) with explicit adif:"..." tags.
//
// The structural drift between [QslSection] and [types.Qsl] is a known
// issue flagged for design review — the two structs cover overlapping
// but not identical ADIF field sets.
package adif
