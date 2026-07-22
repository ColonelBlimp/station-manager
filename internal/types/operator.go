package types

// Operator is one entry in the station's operator roster (ADR 0055) — a person
// who may sit at the key. When an operator is the "current operator" for a
// logging session, their Callsign becomes a QSO's ADIF OPERATOR and their Name
// its MY_NAME.
//
// The roster is config-level (like Rigs / Forwarders), deliberately NOT a DB
// table: per-operator QSO counts come from grouping on the stamped OPERATOR
// field, so no relational storage is needed. Each entry is the minimal
// "operator profile" — extensible later (a personal QSL message, a default RST)
// without a schema change, since it is plain config.
type Operator struct {
	// Callsign is the operator's own callsign (ADIF OPERATOR). Uppercased and
	// trimmed by config.Normalize.
	Callsign string `json:"callsign"`
	// Name is the operator's name (ADIF MY_NAME). Optional.
	Name string `json:"name,omitempty"`
}
