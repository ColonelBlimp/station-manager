package types

// EvidenceConfig is the top-level `evidence` config block — the local FT8
// evidence-capture consent layer (spot-network design §4.1/§8, operator
// decisions 2026-08-10).
//
// Capture is DEFAULT OFF: it is the first of three separately-controlled
// consent layers (capture / sync / publication), and no evidence.db exists
// until the operator opts in. Disabling capture later stops new writes but
// deletes nothing.
//
// CapBytes is the PHYSICAL size cap over evidence.db + its WAL and
// shared-memory siblings; capture drops new slots at a soft watermark below
// it (headroom reserved for WAL/checkpoint churn and the loss record of the
// dropping itself) and resumes if capacity returns. 0 = the default
// 524,288,000 bytes (500 MiB exactly — an exact byte count to avoid unit
// ambiguity).
type EvidenceConfig struct {
	Capture  bool          `json:"capture"`
	CapBytes int64         `json:"cap_bytes,omitempty"`
	Antennas []AntennaDecl `json:"antennas,omitempty"`
}

// AntennaDecl is one entry of the §4.2 station-profile declaration
// (operator rulings 2026-08-10): a per-band operating statement — "on these
// bands, FT8 evidence capture happens through this antenna" — NOT a shack
// inventory. The daemon pins immutable profile versions from it in
// evidence.db at startup (restart-only activation, like the rest of the
// evidence block); nothing here is a live-changing fact.
//
// Name is the lineage identity: trimmed, case-sensitive, unique. Renaming
// starts a new lineage. Bands are ADIF band tokens; a band may be claimed
// by ONE antenna, once (duplicates in or across entries are validation
// errors — silent normalization would conceal a typo), and an empty list is
// invalid (it declares nothing; retire by removing the entry).
//
// HeightM is the feedpoint height above ground in metres: finite, ≥ 0, no
// upper bound (unusual installations are physically honest); a pointer
// because 0 m (a ground-mounted vertical) is a real value distinct from
// "not declared". Locator is a Maidenhead locator, validated and
// canonicalized; omitted means the version pins "not declared" — it does
// NOT inherit the station grid (which is live-writable via /v1/config, and
// inheriting it would smuggle a changing fact into a restart-only system).
// Transmit power is deliberately ABSENT this slice (not honest across
// bands/days; overlaps station.default_power) — see the §4.2 amendment.
type AntennaDecl struct {
	Name     string   `json:"name"`
	Type     string   `json:"type,omitempty"`
	Bands    []string `json:"bands"`
	HeightM  *float64 `json:"height_m,omitempty"`
	Feedline string   `json:"feedline,omitempty"`
	Locator  string   `json:"locator,omitempty"`
}

// EvidenceMinCapBytes is the smallest cap config validation accepts when
// capture is enabled: the writer reserves 16 MiB of headroom below the cap
// (the soft watermark), so this leaves an equal working floor above it — a
// smaller cap would make capture drop immediately or leave it nowhere to
// write. Lives here (not in internal/evidence) because internal/config
// cannot import the evidence package (evidence → logging → config cycle)
// and a parallel constant would drift.
const EvidenceMinCapBytes int64 = 32 << 20
