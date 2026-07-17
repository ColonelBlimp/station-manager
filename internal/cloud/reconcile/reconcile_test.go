package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// The protocol properties both reconcile ends stand on: determinism, input-
// order independence, µs canonicalisation (a ns-precision local value and its
// µs-truncated stored form MUST hash identically), and UUID case folding.

func TestSummary_Deterministic(t *testing.T) {
	at := time.Date(2026, 7, 17, 5, 0, 0, 123456000, time.UTC)
	entries := []Entry{
		{UUID: "0197f9a0-0000-7000-8000-000000000001", ModifiedAt: at},
		{UUID: "0197f9a0-0000-7000-8000-000000000002", ModifiedAt: at.Add(time.Second)},
	}
	c1, h1 := Summary(entries)
	c2, h2 := Summary(entries)
	if c1 != 2 || c1 != c2 || h1 != h2 {
		t.Fatalf("not deterministic: (%d,%s) vs (%d,%s)", c1, h1, c2, h2)
	}
}

func TestSummary_OrderIndependent(t *testing.T) {
	at := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	a := Entry{UUID: "0197f9a0-0000-7000-8000-00000000000a", ModifiedAt: at}
	b := Entry{UUID: "0197f9a0-0000-7000-8000-00000000000b", ModifiedAt: at.Add(time.Minute)}
	_, h1 := Summary([]Entry{a, b})
	_, h2 := Summary([]Entry{b, a})
	if h1 != h2 {
		t.Fatalf("order changed the hash: %s vs %s", h1, h2)
	}
}

// The load-bearing case: the daemon side reads ns-precision local timestamps;
// the cloud stored the same instant µs-truncated. They must hash equal.
func TestSummary_MicrosecondCanonicalisation(t *testing.T) {
	nsLocal := time.Date(2026, 7, 17, 5, 0, 0, 123456789, time.UTC)  // ns precision
	usStored := time.Date(2026, 7, 17, 5, 0, 0, 123456000, time.UTC) // what Postgres kept
	uuid := "0197f9a0-0000-7000-8000-000000000001"
	_, h1 := Summary([]Entry{{UUID: uuid, ModifiedAt: nsLocal}})
	_, h2 := Summary([]Entry{{UUID: uuid, ModifiedAt: usStored}})
	if h1 != h2 {
		t.Fatalf("ns local vs µs stored hash mismatch: %s vs %s", h1, h2)
	}
}

// Zone-independence: the same instant in another zone hashes identically
// (UnixMicro is zone-free by construction — this pins it).
func TestSummary_ZoneIndependent(t *testing.T) {
	utc := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	cat := utc.In(time.FixedZone("CAT", 2*3600))
	uuid := "0197f9a0-0000-7000-8000-000000000001"
	_, h1 := Summary([]Entry{{UUID: uuid, ModifiedAt: utc}})
	_, h2 := Summary([]Entry{{UUID: uuid, ModifiedAt: cat}})
	if h1 != h2 {
		t.Fatalf("zone changed the hash: %s vs %s", h1, h2)
	}
}

func TestSummary_UuidCaseFolded(t *testing.T) {
	at := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	_, h1 := Summary([]Entry{{UUID: "0197F9A0-0000-7000-8000-000000000001", ModifiedAt: at}})
	_, h2 := Summary([]Entry{{UUID: "0197f9a0-0000-7000-8000-000000000001", ModifiedAt: at}})
	if h1 != h2 {
		t.Fatalf("uuid case changed the hash: %s vs %s", h1, h2)
	}
}

func TestSummary_Empty(t *testing.T) {
	c, h := Summary(nil)
	if c != 0 {
		t.Fatalf("count = %d, want 0", c)
	}
	want := sha256.Sum256(nil)
	if h != hex.EncodeToString(want[:]) {
		t.Fatalf("empty hash = %s, want sha256 of empty input", h)
	}
}

func TestSummary_ContentSensitive(t *testing.T) {
	at := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	uuid := "0197f9a0-0000-7000-8000-000000000001"
	_, base := Summary([]Entry{{UUID: uuid, ModifiedAt: at}})
	_, editedTs := Summary([]Entry{{UUID: uuid, ModifiedAt: at.Add(time.Microsecond)}})
	_, extraRow := Summary([]Entry{
		{UUID: uuid, ModifiedAt: at},
		{UUID: "0197f9a0-0000-7000-8000-000000000002", ModifiedAt: at},
	})
	if base == editedTs {
		t.Fatal("a µs edit must change the hash")
	}
	if base == extraRow {
		t.Fatal("an extra row must change the hash")
	}
}
