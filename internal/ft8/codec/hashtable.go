package codec

import (
	"strings"
	"sync"
)

// DefaultHashTableCapacity is the WSJT-X-convention capacity for the
// receiver-side running hash table: keep the last 100 distinct
// callsigns seen on the band. The number is empirically chosen — big
// enough that a normal FT8 session won't churn it on every cycle,
// small enough that h12/h10 collisions stay rare-but-acknowledged.
const DefaultHashTableCapacity = 100

// HashTable is the receiver-side running map from callsign-hash
// (h10/h12/h22) back to the original callsign string. Phase 4's job
// per QEX paper §6: every successful decode that surfaces plaintext
// callsigns Observes them into the table, and later decodes whose
// hash-bearing slots surface "<...>" sentinels (Type 4 NonStd, Type
// 5 EU VHF hashes+g25) Resolve through it.
//
// Capacity is bounded — FIFO eviction of the oldest entry when full,
// with LRU-on-reinsert (a callsign Inserted while already present
// moves to the most-recent slot rather than creating a duplicate).
// This matches WSJT-X's "recently heard" semantic: stations active
// in the current cycle stay resolvable; long-silent ones age out.
//
// Hash-collision strategy: lookups iterate newest-to-oldest, so the
// most recently Inserted call wins on any colliding hash. Older
// colliders remain in the table and stay reachable via their
// non-colliding hashes (h22 collision doesn't imply h12 collision).
// At capacity = 100 the collision rates are h22≈0.002%, h12≈2.4%,
// h10≈10%; only h10 collisions are real in practice, and h10 is
// only consulted by Type 0.x specialty messages.
//
// All methods are safe for concurrent use. The FT8 service layer
// holds one HashTable per receive pipeline; decoders that need
// resolution call into the same instance from worker goroutines.
type HashTable struct {
	mu      sync.RWMutex
	cap     int
	entries []hashEntry // oldest at [0], newest at [len-1]
}

// hashEntry is one slot in the running table. Hashes are precomputed
// at Insert time so lookups don't re-run HashCodes per call.
type hashEntry struct {
	callsign string
	h10      uint16
	h12      uint16
	h22      uint32
}

// NewHashTable returns a HashTable with the given capacity. Capacity
// must be positive; non-positive values fall back to
// DefaultHashTableCapacity.
func NewHashTable(capacity int) *HashTable {
	if capacity <= 0 {
		capacity = DefaultHashTableCapacity
	}
	return &HashTable{
		cap:     capacity,
		entries: make([]hashEntry, 0, capacity),
	}
}

// Insert records a callsign in the table, computing its three hash
// widths via HashCodes. Inputs that aren't real callsigns are
// silently skipped — the receiver loop calls Insert on every Call
// slot of a decoded Message regardless of type, and the filter here
// keeps tokens (CQ / DE / QRZ / "CQ NNN" / "CQ XXXX"), the "<...>"
// hash-pending sentinel, and out-of-alphabet strings out of the
// table.
//
// If the callsign is already present, it is moved to the most-recent
// slot (LRU touch). If insertion would exceed capacity, the oldest
// entry is evicted.
//
// Whitespace is trimmed before validation: HashCodes treats trailing
// space as identical to a shorter callsign, so Insert("K1JT ") and
// Insert("K1JT") would otherwise produce identical hashes but two
// different stored strings.
func (t *HashTable) Insert(callsign string) {
	callsign = strings.TrimSpace(callsign)
	if !isInsertableCallsign(callsign) {
		return
	}
	h10, h12, h22 := HashCodes(callsign)
	e := hashEntry{
		callsign: callsign,
		h10:      uint16(h10),
		h12:      uint16(h12),
		h22:      h22,
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Move existing entry to MRU position; or, if the slot already
	// holds a different callsign for the same h22, leave that older
	// call where it is (collision — newest wins on lookup via the
	// newest-to-oldest iteration, no need to touch ordering here).
	for i, x := range t.entries {
		if x.callsign == callsign {
			t.entries = append(t.entries[:i], t.entries[i+1:]...)
			break
		}
	}
	t.entries = append(t.entries, e)

	// Cap from the front (FIFO of oldest).
	if len(t.entries) > t.cap {
		drop := len(t.entries) - t.cap
		t.entries = t.entries[drop:]
	}
}

// LookupH22 returns the most recently Inserted callsign whose h22
// hash matches, or ("", false) if no entry hashes to that value.
// Newest-wins on collision.
func (t *HashTable) LookupH22(h22 uint32) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for i := len(t.entries) - 1; i >= 0; i-- {
		if t.entries[i].h22 == h22 {
			return t.entries[i].callsign, true
		}
	}
	return "", false
}

// LookupH12 returns the most recently Inserted callsign whose h12
// hash matches. Newest-wins on collision (h12 collisions are ~2.4%
// at capacity 100 — real but uncommon).
func (t *HashTable) LookupH12(h12 uint16) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for i := len(t.entries) - 1; i >= 0; i-- {
		if t.entries[i].h12 == h12 {
			return t.entries[i].callsign, true
		}
	}
	return "", false
}

// LookupH10 returns the most recently Inserted callsign whose h10
// hash matches. Newest-wins on collision (h10 collisions are ~10%
// at capacity 100 — common; only consulted by Type 0.x specialty
// messages).
func (t *HashTable) LookupH10(h10 uint16) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for i := len(t.entries) - 1; i >= 0; i-- {
		if t.entries[i].h10 == h10 {
			return t.entries[i].callsign, true
		}
	}
	return "", false
}

// Len reports the current entry count. Useful for tests and
// instrumentation.
func (t *HashTable) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.entries)
}

// Observe records every plaintext callsign in m into the table. Used
// by the receive pipeline after a successful decode: Insert filters
// tokens / sentinels / empty strings itself, so Observe can hand
// each Call slot straight through without per-Type dispatch.
//
// Suffix bits (/R, /P) are not part of Call1/Call2 — the bare
// callsign is what gets stored, matching how HashCodes is normally
// invoked elsewhere in the codec.
func (t *HashTable) Observe(m Message) {
	t.Insert(m.Call1)
	t.Insert(m.Call2)
}

// Resolve returns a copy of m with sentinel call slots replaced by
// callsigns looked up from the table. The original m is not mutated.
//
// Per-Type behavior:
//
//   - Type 4 (NonStd Call): a single hash slot. Whichever of Call1
//     or Call2 is the sentinel "<...>" is resolved via Hash12.
//   - Type 5 (EU VHF hashes+g25): both call slots are sentinels.
//     Call1 resolves via Hash12, Call2 via Hash22Call2.
//   - Types 1/2/3 (Std Msg, EU VHF /P, RTTY Roundup): either
//     Call1 or Call2 (or both) may be a "<...>" sentinel when the
//     c28 wire value landed in the 22-bit hash partition (compound
//     or special-event callsigns referenced by hash per QEX
//     Table 2). Call1 resolves via Hash22Call1, Call2 via
//     Hash22Call2.
//   - Free Text and other no-hash Types: returned unchanged.
//
// On successful resolution the corresponding Hash12 / Hash22Call1 /
// Hash22Call2 field is zeroed so a downstream observer can't
// accidentally re-resolve or tell a resolved-from-hash call apart
// from one that decoded plaintext. If the table doesn't yet hold
// the callsign for a given hash, that slot stays as the sentinel
// and the Hash field stays non-zero — caller can retry after more
// Observe calls populate the table.
func (t *HashTable) Resolve(m Message) Message {
	switch m.Type {
	case MessageTypeNonStdCall:
		if m.Call1 == hashedCallSentinel {
			if call, ok := t.LookupH12(m.Hash12); ok {
				m.Call1 = call
				m.Hash12 = 0
			}
		} else if m.Call2 == hashedCallSentinel {
			if call, ok := t.LookupH12(m.Hash12); ok {
				m.Call2 = call
				m.Hash12 = 0
			}
		}
	case MessageTypeEUVHFHash:
		if m.Call1 == hashedCallSentinel {
			if call, ok := t.LookupH12(m.Hash12); ok {
				m.Call1 = call
				m.Hash12 = 0
			}
		}
		if m.Call2 == hashedCallSentinel {
			if call, ok := t.LookupH22(m.Hash22Call2); ok {
				m.Call2 = call
				m.Hash22Call2 = 0
			}
		}
	case MessageTypeStd, MessageTypeEUVHFP, MessageTypeRTTYRU:
		// Types 1/2/3: either Call slot may independently be a
		// hash-partition c28 reference. Resolve each side via its
		// own Hash22Call* field.
		if m.Call1 == hashedCallSentinel && m.Hash22Call1 != 0 {
			if call, ok := t.LookupH22(m.Hash22Call1); ok {
				m.Call1 = call
				m.Hash22Call1 = 0
			}
		}
		if m.Call2 == hashedCallSentinel && m.Hash22Call2 != 0 {
			if call, ok := t.LookupH22(m.Hash22Call2); ok {
				m.Call2 = call
				m.Hash22Call2 = 0
			}
		}
	default:
		// Intentional no-op for non-hash-bearing Types (Free Text,
		// specialty 0.x family). Unlike the encode/decode/format
		// switches in this package, an unhandled Type here isn't a
		// wire error — it just means there are no sentinels to
		// resolve.
	}
	return m
}

// isInsertableCallsign is the gate for Insert: true means HashCodes
// will accept the string, and it isn't a token / sentinel. False
// strings are silently dropped at Insert time.
func isInsertableCallsign(s string) bool {
	if s == "" || s == hashedCallSentinel {
		return false
	}
	if _, ok := TokenToC28(s); ok {
		return false
	}
	if len(s) > hashCallsignLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(hashAlphabet, s[i]) < 0 {
			return false
		}
	}
	return true
}
