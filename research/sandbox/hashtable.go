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
