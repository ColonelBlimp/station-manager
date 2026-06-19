// Package adif handles parsing and serialisation of ADIF (Amateur Data
// Interchange Format) data.
//
// # Parsing
//
// [Parse] accepts raw ADIF bytes and returns a populated [Adif] struct.
// It is intentionally tolerant: unknown fields are ignored, tags are
// case-insensitive, and only string fields are supported. The parser is a
// single length-aware forward scan: each value is skipped by its declared
// `<TAG:n>` byte length, so <EOH>/<EOR> are recognised as markers only at a
// tag boundary, never inside a value. It uses reflection over struct tags
// (adif:"..." with json:"..." as fallback) to populate [Record] fields.
//
// Parsed values are right-trimmed of trailing whitespace — a deliberate
// cleanliness choice (real-world exporters pad within the declared length, and
// a padded callsign must still match in dupe-detection), not strict
// byte-faithfulness to the length prefix. See [Parse] for the trade-off.
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
// [QslSection] and [types.Qsl] now round-trip every QSL field the canonical
// [types.Qso] model can hold — including the incoming-QSL fields QSLMSG_RCVD,
// QSL_RCVD_VIA, and QSL_RCVD_NOTES (review 2026-06-19 M1). [QslSection] still
// carries a few ADIF fields that have NO home on [types.Qso] and are therefore
// intentionally NOT persisted: QSLMSG_INTL (the internationalised QSL message)
// and the QRZCOM_QSO_DOWNLOAD_DATE / QRZCOM_QSO_DOWNLOAD_STATUS pair. They are
// parsed into the transient [Record] (so a parse doesn't choke on them) and
// dropped at the persistence boundary — a faithful re-import of those specific
// fields is unsupported by design until a concrete need appears.
package adif
