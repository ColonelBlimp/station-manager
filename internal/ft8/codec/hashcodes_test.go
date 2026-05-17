package codec

import "testing"

// HashCodes test vectors generated 2026-05-17 using the public-
// domain `hashcodes` reference program from QEX ref [14] (built
// from `hashcodes.f90`). Per QEX paper §9 the reference is in the
// public domain; the hash values below are facts produced by
// running it against chosen inputs.
//
// Coverage spans the full input space:
//
//   - Standard amateur calls (K1JT, K1ABC, G4ABC) — used when a
//     standard call appears in a context that wants a hash anyway
//   - Compound call with '/' separator (PJ4/K1ABC) — the canonical
//     non-standard case that hash codes exist for
//   - Special-event call (YW18FIFA) — 8 chars, exercises the right
//     end of the padding range
//   - Malawian call 7Q5MLV (operator's country)
//   - CQ — 2 chars, exercises short-input padding
//   - Empty string — every char padded to space, all hashes = 0
var hashVectors = []struct {
	name        string
	call        string
	wantH10     uint32
	wantH12     uint32
	wantH22     uint32
	wantC28Bias uint32
}{
	{"std_call_K1JT", "K1JT", 511, 2047, 2096289, 4159881},
	{"std_call_K1ABC", "K1ABC", 712, 2851, 2920267, 4983859},
	{"compound_PJ4_K1ABC", "PJ4/K1ABC", 346, 1387, 1420834, 3484426},
	{"special_event_YW18FIFA", "YW18FIFA", 188, 753, 771524, 2835116},
	{"std_call_G4ABC", "G4ABC", 171, 685, 702035, 2765627},
	{"std_call_7Q5MLV_malawi", "7Q5MLV", 1004, 4017, 4114098, 6177690},
	{"two_char_CQ", "CQ", 842, 3368, 3449465, 5513057},
	{"empty_all_spaces", "", 0, 0, 0, 2063592}, // 2063592 = nTokens
}

func TestHashCodes_SpecVectors(t *testing.T) {
	for _, tc := range hashVectors {
		t.Run(tc.name, func(t *testing.T) {
			h10, h12, h22 := HashCodes(tc.call)
			if h10 != tc.wantH10 {
				t.Errorf("HashCodes(%q).h10 = %d, want %d", tc.call, h10, tc.wantH10)
			}
			if h12 != tc.wantH12 {
				t.Errorf("HashCodes(%q).h12 = %d, want %d", tc.call, h12, tc.wantH12)
			}
			if h22 != tc.wantH22 {
				t.Errorf("HashCodes(%q).h22 = %d, want %d", tc.call, h22, tc.wantH22)
			}
		})
	}
}

func TestHashedCallC28_SpecVectors(t *testing.T) {
	for _, tc := range hashVectors {
		t.Run(tc.name, func(t *testing.T) {
			got := HashedCallC28(tc.call)
			if got != tc.wantC28Bias {
				t.Errorf("HashedCallC28(%q) = %d, want %d", tc.call, got, tc.wantC28Bias)
			}
		})
	}
}

func TestHashCodes_WidthInvariants(t *testing.T) {
	// h10 fits in 10 bits, h12 in 12, h22 in 22 — regardless of input.
	// Belt-and-braces against a regression in the right-shift counts.
	for _, tc := range hashVectors {
		h10, h12, h22 := HashCodes(tc.call)
		if h10 >= (1 << HashBits10) {
			t.Errorf("%s: h10=%d exceeds %d bits", tc.name, h10, HashBits10)
		}
		if h12 >= (1 << HashBits12) {
			t.Errorf("%s: h12=%d exceeds %d bits", tc.name, h12, HashBits12)
		}
		if h22 >= (1 << HashBits22) {
			t.Errorf("%s: h22=%d exceeds %d bits", tc.name, h22, HashBits22)
		}
	}
}

func TestHashCodes_NestedConsistency(t *testing.T) {
	// The three widths are derived from the same product via right
	// shifts at 54 / 52 / 42. Each narrower width is the top N bits
	// of the wider one — so h10 is the top 10 bits of h12 (which is
	// 12 bits wide), and h12 is the top 12 bits of h22 (22 bits wide).
	// Pin this so a future "optimisation" that computed them
	// independently with subtly different rounding wouldn't slip
	// past the vector tests.
	for _, tc := range hashVectors {
		h10, h12, h22 := HashCodes(tc.call)
		// h10 should equal h12 >> 2
		if got := h12 >> (HashBits12 - HashBits10); got != h10 {
			t.Errorf("%s: h12>>2=%d should equal h10=%d", tc.name, got, h10)
		}
		// h12 should equal h22 >> 10
		if got := h22 >> (HashBits22 - HashBits12); got != h12 {
			t.Errorf("%s: h22>>10=%d should equal h12=%d", tc.name, got, h12)
		}
	}
}

func TestHashedCallC28_LandsInHashRange(t *testing.T) {
	// Per QEX paper Table 7 the biased value must fall in c28's
	// reserved [nTokens, nTokens+max22) hash range. h22 maxes at
	// 2^22 - 1 = 4,194,303, so biased max = nTokens + 4,194,303 =
	// 6,257,895 < nTokens + max22 = 6,257,896. By construction
	// (h22 + nTokens) always lands in [nTokens, nTokens+max22).
	for _, tc := range hashVectors {
		got := HashedCallC28(tc.call)
		if got < nTokens || got >= nTokens+max22 {
			t.Errorf("%s: HashedCallC28(%q)=%d outside hash range [%d, %d)",
				tc.name, tc.call, got, nTokens, nTokens+max22)
		}
	}
}

func TestHashCodes_RejectsOversizedInput(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("HashCodes(12-char) should panic; did not")
		}
	}()
	HashCodes("123456789012")
}

func TestHashCodes_RejectsNonAlphabetChars(t *testing.T) {
	cases := []struct {
		name string
		call string
	}{
		{"lowercase_letter", "k1abc"},
		{"hyphen_not_in_alphabet", "K1-ABC"},
		{"colon_not_in_alphabet", "K1:ABC"},
		{"non_ascii", "K1ÉBC"},
		{"control_char", "K1\x01BC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("HashCodes(%q) should panic on non-alphabet char; did not", tc.call)
				}
			}()
			HashCodes(tc.call)
		})
	}
}

func TestHashCodes_EmptyInputAllZero(t *testing.T) {
	// Documented contract: empty input produces all-zero hashes
	// because every char pads to space (alphabet index 0).
	h10, h12, h22 := HashCodes("")
	if h10 != 0 || h12 != 0 || h22 != 0 {
		t.Errorf("HashCodes(\"\") = (%d, %d, %d), want (0, 0, 0)", h10, h12, h22)
	}
}

func TestHashCodes_AcceptsSlash(t *testing.T) {
	// '/' is in the hash alphabet specifically so compound calls
	// like "PJ4/K1ABC" can be hashed. Pinned against accidental
	// alphabet narrowing.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("HashCodes(compound call) should NOT panic; got: %v", r)
		}
	}()
	HashCodes("PJ4/K1ABC")
}

func BenchmarkHashCodes(b *testing.B) {
	const call = "PJ4/K1ABC"
	b.ReportAllocs()
	for range b.N {
		_, _, _ = HashCodes(call)
	}
}
