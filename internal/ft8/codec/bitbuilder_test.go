package codec

import (
	"slices"
	"testing"
)

func TestBitBuilder_AppendKnownPatterns(t *testing.T) {
	// Hand-computed expected outputs to verify MSB-first ordering.
	// A regression in shift direction or bit ordering fails these
	// immediately without depending on a helper that might share
	// the same bug.
	cases := []struct {
		name  string
		value uint64
		nbits int
		want  []byte
	}{
		{"width_1_one", 1, 1, []byte{1}},
		{"width_1_zero", 0, 1, []byte{0}},
		{"width_3_i3_typ_1", 1, 3, []byte{0, 0, 1}},
		{"width_3_i3_typ_4", 4, 3, []byte{1, 0, 0}},
		{"width_4_A", 0xA, 4, []byte{1, 0, 1, 0}},
		{"width_4_5", 0x5, 4, []byte{0, 1, 0, 1}},
		{"width_8_A5", 0xA5, 8, []byte{1, 0, 1, 0, 0, 1, 0, 1}},
		{"width_8_all_ones", 0xFF, 8, []byte{1, 1, 1, 1, 1, 1, 1, 1}},
		{"width_8_all_zeros", 0, 8, []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		{"width_0_no_emit", 0, 0, []byte{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b BitBuilder
			b.Append(tc.value, tc.nbits)
			got := b.Bits()
			if !slices.Equal(got, tc.want) {
				t.Errorf("Append(%#x, %d) = %v, want %v", tc.value, tc.nbits, got, tc.want)
			}
		})
	}
}

// TestBitBuilder_AppendFT8Widths verifies MSB-first emission for
// the widths Layer 2 actually uses, via an independent bit-extraction
// reference. A regression in Append doesn't infect the reference.
func TestBitBuilder_AppendFT8Widths(t *testing.T) {
	bitsOfValue := func(v uint64, n int) []byte {
		out := make([]byte, n)
		for i := range n {
			out[i] = byte((v >> (n - 1 - i)) & 1)
		}
		return out
	}
	cases := []struct {
		name  string
		value uint64
		nbits int
	}{
		{"g15_pattern", 0x5AAA, 15},
		{"h22_alternating", 0b1010101010101010101010, 22},
		{"c28_max", (1 << 28) - 1, 28},
		{"c28_zero", 0, 28},
		{"r5_pattern", 0b10101, 5},
		{"i3_n3_combined", 0b101010, 6},
		{"c58_max", (1 << 58) - 1, 58},
		{"width_64_all_ones", ^uint64(0), 64},
		{"width_64_alternating", 0xAAAAAAAAAAAAAAAA, 64},
		{"width_64_zero", 0, 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b BitBuilder
			b.Append(tc.value, tc.nbits)
			got := b.Bits()
			want := bitsOfValue(tc.value, tc.nbits)
			if !slices.Equal(got, want) {
				t.Errorf("Append(%#x, %d) = %v, want %v", tc.value, tc.nbits, got, want)
			}
		})
	}
}

func TestBitBuilder_MethodChaining(t *testing.T) {
	var b BitBuilder
	result := b.Append(0xA, 4).Append(0x5, 4).Bits()
	want := []byte{1, 0, 1, 0, 0, 1, 0, 1}
	if !slices.Equal(result, want) {
		t.Errorf("chained append got %v, want %v", result, want)
	}
}

func TestBitBuilder_AppendBitsConcatenates(t *testing.T) {
	var b BitBuilder
	b.AppendBits([]byte{1, 0, 1}).AppendBits([]byte{0, 1, 0})
	want := []byte{1, 0, 1, 0, 1, 0}
	if got := b.Bits(); !slices.Equal(got, want) {
		t.Errorf("AppendBits chain = %v, want %v", got, want)
	}
}

func TestBitBuilder_AppendBitsEmptySource(t *testing.T) {
	var b BitBuilder
	b.AppendBits(nil).AppendBits([]byte{})
	if got := b.Len(); got != 0 {
		t.Errorf("Len after empty AppendBits = %d, want 0", got)
	}
}

func TestBitBuilder_MixedAppendAndAppendBits(t *testing.T) {
	// Real Layer 2 use: an integer-shaped primitive (c28, g15) +
	// a bit-per-byte primitive (f71) combined into one message.
	var b BitBuilder
	b.Append(0xF, 4).AppendBits([]byte{1, 0, 1, 0}).Append(0x5, 4)
	want := []byte{1, 1, 1, 1, 1, 0, 1, 0, 0, 1, 0, 1}
	if got := b.Bits(); !slices.Equal(got, want) {
		t.Errorf("mixed append = %v, want %v", got, want)
	}
	if got := b.Len(); got != 12 {
		t.Errorf("Len = %d, want 12", got)
	}
}

func TestBitBuilder_LenTracks(t *testing.T) {
	var b BitBuilder
	if got := b.Len(); got != 0 {
		t.Errorf("zero-value Len = %d, want 0", got)
	}
	b.Append(0x1, 1)
	if got := b.Len(); got != 1 {
		t.Errorf("after 1-bit append, Len = %d, want 1", got)
	}
	b.Append(0xFF, 8)
	if got := b.Len(); got != 9 {
		t.Errorf("after 8-bit append, Len = %d, want 9", got)
	}
	b.AppendBits([]byte{0, 1, 0})
	if got := b.Len(); got != 12 {
		t.Errorf("after AppendBits, Len = %d, want 12", got)
	}
}

func TestBitBuilder_ZeroValueBitsIsEmpty(t *testing.T) {
	var b BitBuilder
	if got := len(b.Bits()); got != 0 {
		t.Errorf("zero-value Bits() len = %d, want 0", got)
	}
}

func TestBitBuilder_BitsAliasesInternalStorage(t *testing.T) {
	// Pin the documented contract: Bits() aliases the builder's
	// internal storage, it doesn't clone. A future change that
	// switched to returning a copy would break callers relying on
	// the documented aliasing — this test surfaces the regression
	// instead of letting it through.
	var b BitBuilder
	b.Append(0xA, 4) // [1, 0, 1, 0]
	s := b.Bits()
	s[0] = 0
	if got := b.Bits()[0]; got != 0 {
		t.Errorf("mutation through returned slice not visible via subsequent Bits(): got %d, want 0 — Bits() docs say it aliases internal storage", got)
	}
}

func TestBitBuilder_Build77BitMessage(t *testing.T) {
	// QEX Table 1 Type 1 (Std Msg) layout:
	//   c28 (28) | r1 (1) | c28 (28) | r1 (1) | R1 (1) | g15 (15) | i3 (3)
	// Total = 28+1+28+1+1+15+3 = 77. Pins the canonical Layer 2 use case.
	var b BitBuilder
	b.Append(0x0123456, 28).
		Append(0, 1).
		Append(0x1234567, 28).
		Append(1, 1).
		Append(0, 1).
		Append(0x1234, 15).
		Append(1, 3)
	if got := b.Len(); got != 77 {
		t.Errorf("Len = %d, want 77", got)
	}
	if got := len(b.Bits()); got != 77 {
		t.Errorf("len(Bits()) = %d, want 77", got)
	}
	// Spot-check the bits at known positions:
	// - bits[0..27] = c28 #1 = 0x0123456 MSB-first
	// - bits[28] = r1 #1 = 0
	// - bits[76] = bottom bit of i3 = 1
	bits := b.Bits()
	if bits[28] != 0 {
		t.Errorf("bits[28] (r1 #1) = %d, want 0", bits[28])
	}
	if bits[76] != 1 {
		t.Errorf("bits[76] (i3 LSB) = %d, want 1", bits[76])
	}
}

func TestBitBuilder_PanicNegativeNbits(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Append(0, -1) should panic, did not")
		}
	}()
	var b BitBuilder
	b.Append(0, -1)
}

func TestBitBuilder_PanicTooManyNbits(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Append(0, 65) should panic, did not")
		}
	}()
	var b BitBuilder
	b.Append(0, 65)
}

func TestBitBuilder_PanicValueOverflow(t *testing.T) {
	// 0x100 = 9-bit value into 8-bit slot.
	defer func() {
		if r := recover(); r == nil {
			t.Error("Append(0x100, 8) should panic, did not")
		}
	}()
	var b BitBuilder
	b.Append(0x100, 8)
}

func TestBitBuilder_PanicValueOverflow_OneBitOver(t *testing.T) {
	// Off-by-one: value = 1 << nbits is the smallest overflow.
	defer func() {
		if r := recover(); r == nil {
			t.Error("Append(1<<28, 28) should panic, did not")
		}
	}()
	var b BitBuilder
	b.Append(1<<28, 28)
}

func TestBitBuilder_NoPanic_ValueExactlyFits(t *testing.T) {
	// value = (1 << nbits) - 1 is the largest in-range value.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Append((1<<28)-1, 28) should NOT panic, got %v", r)
		}
	}()
	var b BitBuilder
	b.Append((1<<28)-1, 28)
}

func TestBitBuilder_NoPanic_Width64MaxValue(t *testing.T) {
	// nbits=64 disables the overflow check (any uint64 fits in 64 bits).
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Append(^uint64(0), 64) should NOT panic, got %v", r)
		}
	}()
	var b BitBuilder
	b.Append(^uint64(0), 64)
}

func TestBitBuilder_PanicAppendBitsNonBinary(t *testing.T) {
	cases := []struct {
		name string
		src  []byte
	}{
		{"byte_two", []byte{1, 2, 0}},
		{"byte_255", []byte{255}},
		{"mid_slice", []byte{0, 1, 0, 7, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("AppendBits(%v) should panic, did not", tc.src)
				}
			}()
			var b BitBuilder
			b.AppendBits(tc.src)
		})
	}
}

func BenchmarkBitBuilder_Build77BitMessage(b *testing.B) {
	// Build the canonical Type 1 message from scratch each iteration —
	// representative of the Layer 2 hot path (one message per TX).
	// Some allocation expected: the zero-value builder grows its
	// backing slice as bits accumulate; reuse would avoid this but
	// is premature for 1-per-TX cadence.
	b.ReportAllocs()
	for range b.N {
		var bb BitBuilder
		bb.Append(0x0123456, 28).
			Append(0, 1).
			Append(0x1234567, 28).
			Append(1, 1).
			Append(0, 1).
			Append(0x1234, 15).
			Append(1, 3)
		_ = bb.Bits()
	}
}
