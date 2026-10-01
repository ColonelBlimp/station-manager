package types

// SubmitAttribution is the station identity a live submit stamps on a QSO
// (ADR 0085): MY_RIG, from the rig the bridge connected to at startup with its
// per-rig override; and the effective OPERATOR and MY_NAME. Every field is
// meaningful when empty: "" is a known-empty value (a suppressing MY_RIG
// override, no operator configured), never "unknown".
type SubmitAttribution struct {
	MyRig    string `json:"my_rig"`
	Operator string `json:"operator"`
	MyName   string `json:"my_name"`
}
