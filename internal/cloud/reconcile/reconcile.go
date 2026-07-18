// Package reconcile is the SHARED half of the SM Cloud reconcile protocol
// (ADR 0040 / docs/v2-design/sm-cloud-p1.md S4): the canonical summary hash
// over a logbook's live rows that both ends — the cloud service
// (internal/cloud/server) and the daemon's reconcile routine — must compute
// identically for "no drift" to read as equal hashes.
//
// The protocol pins timestamp resolution to MICROSECONDS (the store's
// canonicalPrecision): Postgres TIMESTAMPTZ keeps µs while Go time.Time and
// the local SQLite side carry ns, so hashing un-truncated local values would
// flag every row as drifted every cycle. Putting the truncation INSIDE this
// package is what discharges the "the S4 peer MUST apply the same µs
// truncation" obligation — a peer that builds Entries from raw local rows and
// calls Summary gets the canonical form for free.
package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Entry is one live (non-tombstone) row of a logbook, reduced to the identity
// pair the reconcile hash covers. Callers filter tombstones out BEFORE
// building entries — deleted rows are reconciled via the manifest, not the
// summary hash. Revision is the row's monotonic edit counter (ADR 0050);
// 0 = pre-revision row.
type Entry struct {
	UUID       string
	ModifiedAt time.Time
	Revision   int64
}

// Summary computes the reconcile summary over a logbook's live rows: the row
// count and a SHA-256 hex hash of the sorted (uuid, modified_at, revision)
// tuples. Canonicalisation, so both ends agree byte-for-byte:
//
//   - UUIDs are lowercased (Postgres' uuid type prints lowercase; SQLite
//     stores whatever the client wrote) and the list is sorted by that
//     lowercased UUID.
//   - ModifiedAt is truncated to microseconds and rendered as decimal
//     UnixMicro — an integer, so no RFC3339 zero-trimming or zone-format
//     ambiguity can creep in.
//   - Revision rides as a decimal integer (ADR 0050): two same-second edits
//     tie on modified_at but differ on revision, so payload divergence the
//     timestamp can't see still reads as drift.
//
// The hash input is one "uuid|unixmicro|revision\n" line per entry; zero
// entries hash to SHA-256 of the empty string. Input order is irrelevant
// (Summary sorts a copy; the caller's slice is untouched).
func Summary(entries []Entry) (count int, hash string) {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		uuid := strings.ToLower(strings.TrimSpace(e.UUID))
		ts := e.ModifiedAt.Truncate(time.Microsecond).UnixMicro()
		lines = append(lines, fmt.Sprintf("%s|%d|%d\n", uuid, ts, e.Revision))
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, l := range lines {
		_, _ = h.Write([]byte(l))
	}
	return len(entries), hex.EncodeToString(h.Sum(nil))
}
