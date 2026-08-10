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
	Capture  bool  `json:"capture"`
	CapBytes int64 `json:"cap_bytes,omitempty"`
}

// EvidenceMinCapBytes is the smallest cap config validation accepts when
// capture is enabled: the writer reserves 16 MiB of headroom below the cap
// (the soft watermark), so this leaves an equal working floor above it — a
// smaller cap would make capture drop immediately or leave it nowhere to
// write. Lives here (not in internal/evidence) because internal/config
// cannot import the evidence package (evidence → logging → config cycle)
// and a parallel constant would drift.
const EvidenceMinCapBytes int64 = 32 << 20
