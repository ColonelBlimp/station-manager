package codec

import (
	"testing"
)

// TestNewHashTable_DefaultCapacity pins that non-positive capacities
// fall back to DefaultHashTableCapacity (so callers don't accidentally
// get a zero-sized table that drops every Insert).
func TestNewHashTable_DefaultCapacity(t *testing.T) {
	t.Parallel()
	for _, cap := range []int{0, -1, -100} {
		ht := NewHashTable(cap)
		if got := ht.cap; got != DefaultHashTableCapacity {
			t.Errorf("NewHashTable(%d).cap = %d, want %d", cap, got, DefaultHashTableCapacity)
		}
	}
}

func TestNewHashTable_CustomCapacity(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(7)
	if got := ht.cap; got != 7 {
		t.Errorf("cap = %d, want 7", got)
	}
}

// TestHashTable_InsertLookup_HappyPath: Insert a callsign, then look
// it up by each of the three hash widths and confirm it comes back.
func TestHashTable_InsertLookup_HappyPath(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	const call = "PJ4/K1ABC"
	ht.Insert(call)

	h10, h12, h22 := HashCodes(call)

	if got, ok := ht.LookupH22(h22); !ok || got != call {
		t.Errorf("LookupH22 = (%q, %v), want (%q, true)", got, ok, call)
	}
	if got, ok := ht.LookupH12(uint16(h12)); !ok || got != call {
		t.Errorf("LookupH12 = (%q, %v), want (%q, true)", got, ok, call)
	}
	if got, ok := ht.LookupH10(uint16(h10)); !ok || got != call {
		t.Errorf("LookupH10 = (%q, %v), want (%q, true)", got, ok, call)
	}
}

// TestHashTable_Lookup_Miss confirms a miss returns the zero value
// + false, not a stale or random entry.
func TestHashTable_Lookup_Miss(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	ht.Insert("K1JT")
	if got, ok := ht.LookupH22(0xFFFFFF); ok {
		t.Errorf("LookupH22(unused) = (%q, true), want miss", got)
	}
	if got, ok := ht.LookupH12(0xFFF); ok {
		t.Errorf("LookupH12(unused) = (%q, true), want miss", got)
	}
	if got, ok := ht.LookupH10(0x3FF); ok {
		t.Errorf("LookupH10(unused) = (%q, true), want miss", got)
	}
}

// TestHashTable_Insert_FiltersJunk pins that the receive-loop-friendly
// silent-skip filter rejects every shape that isn't a real callsign:
// empty, sentinel, tokens (CQ / DE / QRZ / CQ-with-suffix), lowercase,
// out-of-alphabet, too-long.
func TestHashTable_Insert_FiltersJunk(t *testing.T) {
	t.Parallel()
	junk := []string{
		"",
		hashedCallSentinel,
		"CQ", "DE", "QRZ",
		"CQ DX", "CQ POTA", "CQ 123",
		"k1jt",          // lowercase — out of hash alphabet
		"K1!JT",         // punctuation other than /
		"K1JT-OBSERVER", // too long (> 11 chars)
	}
	for _, s := range junk {
		ht := NewHashTable(DefaultHashTableCapacity)
		ht.Insert(s)
		if ht.Len() != 0 {
			t.Errorf("Insert(%q) populated table; want silent skip", s)
		}
	}
}

// TestHashTable_Insert_TrimsWhitespace pins that surrounding whitespace
// is normalised before hashing — HashCodes treats trailing space as a
// shorter callsign, so without trimming we'd silently store two
// entries that hash identically.
func TestHashTable_Insert_TrimsWhitespace(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	ht.Insert("  K1JT  ")
	if ht.Len() != 1 {
		t.Fatalf("Len = %d, want 1", ht.Len())
	}
	_, _, h22 := HashCodes("K1JT")
	got, ok := ht.LookupH22(h22)
	if !ok || got != "K1JT" {
		t.Errorf("LookupH22 = (%q, %v), want (%q, true)", got, ok, "K1JT")
	}
}

// TestHashTable_Insert_LRUOnReinsert pins that re-Inserting an
// existing callsign moves it to MRU rather than appending a duplicate.
// The "moved to MRU" property is what makes long-active stations
// survive eviction across many decode cycles.
func TestHashTable_Insert_LRUOnReinsert(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	ht.Insert("K1JT")
	ht.Insert("G4ABC")
	ht.Insert("K1JT") // re-insert; K1JT should now be at MRU position

	if ht.Len() != 2 {
		t.Fatalf("Len = %d, want 2 (LRU touch must not append duplicate)", ht.Len())
	}
	// K1JT lives at entries[1] (MRU), G4ABC at entries[0] (LRU).
	if ht.entries[0].callsign != "G4ABC" {
		t.Errorf("entries[0].callsign = %q, want G4ABC", ht.entries[0].callsign)
	}
	if ht.entries[1].callsign != "K1JT" {
		t.Errorf("entries[1].callsign = %q, want K1JT", ht.entries[1].callsign)
	}
}

// TestHashTable_FIFOEvictionAtCapacity walks 5 callsigns through a
// capacity-3 table and pins that the oldest fall off the front.
func TestHashTable_FIFOEvictionAtCapacity(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(3)
	calls := []string{"K1JT", "G4ABC", "PA9XYZ", "DL1ABC", "JA1XYZ"}
	for _, c := range calls {
		ht.Insert(c)
	}
	if ht.Len() != 3 {
		t.Fatalf("Len = %d, want 3", ht.Len())
	}

	// The first two should have been evicted; the last three retained.
	for _, evicted := range []string{"K1JT", "G4ABC"} {
		_, _, h22 := HashCodes(evicted)
		if _, ok := ht.LookupH22(h22); ok {
			t.Errorf("LookupH22(%q) found after eviction; want miss", evicted)
		}
	}
	for _, kept := range []string{"PA9XYZ", "DL1ABC", "JA1XYZ"} {
		_, _, h22 := HashCodes(kept)
		if _, ok := ht.LookupH22(h22); !ok {
			t.Errorf("LookupH22(%q) miss; want hit", kept)
		}
	}
}

// TestHashTable_Observe_PopulatesFromCallSlots pins that Observe
// extracts Call1 and Call2 and Inserts each. Token slots (Call1="CQ")
// are filtered by Insert, so the table only ends up with the real
// callsign.
func TestHashTable_Observe_PopulatesFromCallSlots(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	ht.Observe(Message{
		Type:  MessageTypeStd,
		Call1: "K1JT",
		Call2: "G4ABC",
		Grid:  "FN20",
	})
	if ht.Len() != 2 {
		t.Fatalf("Len = %d, want 2", ht.Len())
	}

	htCq := NewHashTable(DefaultHashTableCapacity)
	htCq.Observe(Message{
		Type:  MessageTypeStd,
		Call1: "CQ",
		Call2: "K1JT",
		Grid:  "FN20",
	})
	if htCq.Len() != 1 {
		t.Errorf("CQ slot Observed into table; want filtered (Len = %d, want 1)", htCq.Len())
	}
	if _, _, h22 := HashCodes("K1JT"); true {
		if got, ok := htCq.LookupH22(h22); !ok || got != "K1JT" {
			t.Errorf("K1JT lookup after CQ-skip = (%q, %v), want (K1JT, true)", got, ok)
		}
	}
}

// TestHashTable_Observe_SkipsSentinels pins that Observing a freshly-
// decoded Type 4 (with one side sentinel) inserts only the plaintext
// side. Sentinels never enter the table.
func TestHashTable_Observe_SkipsSentinels(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	ht.Observe(Message{
		Type:   MessageTypeNonStdCall,
		Call1:  hashedCallSentinel,
		Call2:  "PJ4/K1ABC",
		Hash12: 0xAB,
	})
	if ht.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (sentinel must be skipped)", ht.Len())
	}
	_, _, h22 := HashCodes("PJ4/K1ABC")
	if got, ok := ht.LookupH22(h22); !ok || got != "PJ4/K1ABC" {
		t.Errorf("LookupH22 = (%q, %v), want (PJ4/K1ABC, true)", got, ok)
	}
}

// TestHashTable_Resolve_Type4_Call1Sentinel pins the Type 4 path:
// Insert the call first, then Resolve a decoded message whose Call1
// is the sentinel + Hash12 matches.
func TestHashTable_Resolve_Type4_Call1Sentinel(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	const hashedCall = "K1JT"
	ht.Insert(hashedCall)

	_, h12, _ := HashCodes(hashedCall)
	in := Message{
		Type:   MessageTypeNonStdCall,
		Call1:  hashedCallSentinel,
		Call2:  "PJ4/G4ABC",
		Hash12: uint16(h12),
		Grid:   "RR73",
	}
	out := ht.Resolve(in)
	if out.Call1 != hashedCall {
		t.Errorf("Call1 = %q, want %q", out.Call1, hashedCall)
	}
	if out.Hash12 != 0 {
		t.Errorf("Hash12 = %d, want 0 (cleared on resolve)", out.Hash12)
	}
	// Input must not be mutated.
	if in.Call1 != hashedCallSentinel {
		t.Errorf("input mutated: in.Call1 = %q, want sentinel", in.Call1)
	}
}

// TestHashTable_Resolve_Type4_Call2Sentinel is the symmetric h1=1
// branch where Call2 is the hashed slot.
func TestHashTable_Resolve_Type4_Call2Sentinel(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	const hashedCall = "K1JT"
	ht.Insert(hashedCall)

	_, h12, _ := HashCodes(hashedCall)
	in := Message{
		Type:   MessageTypeNonStdCall,
		Call1:  "PJ4/G4ABC",
		Call2:  hashedCallSentinel,
		Hash12: uint16(h12),
	}
	out := ht.Resolve(in)
	if out.Call2 != hashedCall {
		t.Errorf("Call2 = %q, want %q", out.Call2, hashedCall)
	}
	if out.Hash12 != 0 {
		t.Errorf("Hash12 = %d, want 0", out.Hash12)
	}
}

// TestHashTable_Resolve_Type5_BothSentinels is the Type 5 path: both
// call slots arrive as sentinels and the table resolves each via its
// own width (Call1↔Hash12, Call2↔Hash22).
func TestHashTable_Resolve_Type5_BothSentinels(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	ht.Insert("G4ABC")
	ht.Insert("PA9XYZ")

	_, h12, _ := HashCodes("G4ABC")
	_, _, h22 := HashCodes("PA9XYZ")

	in := Message{
		Type:    MessageTypeEUVHFHash,
		Call1:   hashedCallSentinel,
		Call2:   hashedCallSentinel,
		Hash12:  uint16(h12),
		Hash22:  h22,
		Report3: 5,
		Serial:  7,
		Grid6:   "JO22DB",
	}
	out := ht.Resolve(in)
	if out.Call1 != "G4ABC" {
		t.Errorf("Call1 = %q, want G4ABC", out.Call1)
	}
	if out.Call2 != "PA9XYZ" {
		t.Errorf("Call2 = %q, want PA9XYZ", out.Call2)
	}
	if out.Hash12 != 0 || out.Hash22 != 0 {
		t.Errorf("Hash12/Hash22 = %d/%d, want 0/0", out.Hash12, out.Hash22)
	}
}

// TestHashTable_Resolve_Type5_PartialResolution pins that one side
// can resolve while the other stays as a sentinel — the receiver
// might have heard one call but not the other yet.
func TestHashTable_Resolve_Type5_PartialResolution(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	ht.Insert("G4ABC")

	_, h12, _ := HashCodes("G4ABC")

	in := Message{
		Type:    MessageTypeEUVHFHash,
		Call1:   hashedCallSentinel,
		Call2:   hashedCallSentinel,
		Hash12:  uint16(h12),
		Hash22:  0xDEAD, // unknown
		Report3: 0,
		Serial:  0,
		Grid6:   "AA00AA",
	}
	out := ht.Resolve(in)
	if out.Call1 != "G4ABC" {
		t.Errorf("Call1 = %q, want G4ABC", out.Call1)
	}
	if out.Call2 != hashedCallSentinel {
		t.Errorf("Call2 = %q, want sentinel (unresolved)", out.Call2)
	}
	if out.Hash22 != 0xDEAD {
		t.Errorf("Hash22 = %d, want 0xDEAD (preserved for retry)", out.Hash22)
	}
}

// TestHashTable_Resolve_EmptyTable pins that Resolve with no entries
// returns the input unchanged — useful contract for the receive
// pipeline's cold-start moment.
func TestHashTable_Resolve_EmptyTable(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	in := Message{
		Type:   MessageTypeNonStdCall,
		Call1:  hashedCallSentinel,
		Call2:  "PJ4/G4ABC",
		Hash12: 0xABC,
	}
	out := ht.Resolve(in)
	if out.Call1 != hashedCallSentinel {
		t.Errorf("Call1 = %q, want sentinel (unresolved)", out.Call1)
	}
	if out.Hash12 != 0xABC {
		t.Errorf("Hash12 = %d, want preserved", out.Hash12)
	}
}

// TestHashTable_Resolve_NonHashType_Untouched pins that Resolve is a
// no-op for Types that don't carry hash slots in the current codec
// (Type 1, Type 2, Type 0.0). Adding new hash-bearing types in later
// phases will extend the Resolve switch; this test guards against
// silent breakage.
func TestHashTable_Resolve_NonHashType_Untouched(t *testing.T) {
	t.Parallel()
	ht := NewHashTable(DefaultHashTableCapacity)
	ht.Insert("K1JT")

	cases := []Message{
		{Type: MessageTypeStd, Call1: "K1JT", Call2: "G4ABC", Grid: "FN20"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "PA9XYZ", Grid: "JO22"},
		{Type: MessageTypeFreeText, FreeText: "HELLO WORLD"},
	}
	for _, m := range cases {
		got := ht.Resolve(m)
		if got != m {
			t.Errorf("Resolve(%+v) mutated; got %+v", m, got)
		}
	}
}
