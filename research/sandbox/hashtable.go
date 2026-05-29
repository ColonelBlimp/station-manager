package sandbox

import "sync"

// CallsignHashTable maintains the rolling hash registry that
// resolves h12 / h22 fields in Type 4 / Type 1 messages back to
// callsign strings. Standard FT8 operation populates this table
// from successfully-decoded callsigns; receivers in the same QSO
// can then reference each other by hash in subsequent
// transmissions (saving bits for the report field).
//
// Concurrent-safe via an embedded mutex — decoders may be parallel
// across candidates within a slot or across slots.
//
// The table grows unbounded for the session; in real operation the
// FT8 protocol assumes callsigns observed in the last N minutes are
// the ones likely to be referenced, so an LRU policy is the next
// refinement. Not implemented here — fixture-driven validation runs
// in seconds.
type CallsignHashTable struct {
	mu sync.RWMutex

	// byH12 / byH22 hold the most-recently-registered callsign for
	// each hash value. Hash collisions overwrite — FT8 doesn't
	// disambiguate, and the protocol relies on hash space being
	// large enough that collisions are rare in practice.
	byH12 map[uint32]string
	byH22 map[uint32]string
}

// NewCallsignHashTable returns an empty hash table ready for use.
func NewCallsignHashTable() *CallsignHashTable {
	return &CallsignHashTable{
		byH12: make(map[uint32]string),
		byH22: make(map[uint32]string),
	}
}

// Add registers the callsign in the table by both its 12-bit and
// 22-bit hashes. Safe to call repeatedly with the same callsign;
// hashes are deterministic so the table just refreshes the value.
func (t *CallsignHashTable) Add(callsign string) {
	if t == nil || callsign == "" {
		return
	}
	_, h12, h22 := HashCallsign(callsign)
	t.mu.Lock()
	t.byH12[h12] = callsign
	t.byH22[h22] = callsign
	t.mu.Unlock()
}

// LookupH12 returns the callsign registered for h12, plus an OK flag.
// Safe to call on a nil receiver — returns ("", false).
func (t *CallsignHashTable) LookupH12(h12 uint32) (string, bool) {
	if t == nil {
		return "", false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	c, ok := t.byH12[h12]
	return c, ok
}

// LookupH22 returns the callsign registered for h22, plus an OK flag.
// Safe to call on a nil receiver — returns ("", false).
func (t *CallsignHashTable) LookupH22(h22 uint32) (string, bool) {
	if t == nil {
		return "", false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	c, ok := t.byH22[h22]
	return c, ok
}

// Size returns the number of distinct h12 entries. Useful for tests
// and debug logging.
func (t *CallsignHashTable) Size() int {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.byH12)
}

// Callsigns returns a snapshot of every distinct callsign registered
// in the table. No order is guaranteed (map iteration). Safe on a
// nil receiver — returns nil.
//
// Used by AP3 hypothesis enumeration: each pair (c1, c2) of returned
// callsigns becomes a candidate c28_1/c28_2 hypothesis pinned via
// the AP3 priors during the cascade's final stage.
func (t *CallsignHashTable) Callsigns() []string {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.byH12) == 0 {
		return nil
	}
	// De-duplicate via the h22 dimension too — distinct hashes may
	// converge on the same callsign string in pathological cases.
	seen := make(map[string]struct{}, len(t.byH12))
	for _, c := range t.byH12 {
		seen[c] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	return out
}
