package codec

import "strconv"

// BitBuilder accumulates bits MSB-first into a bit-per-byte buffer —
// the convention shared with LDPCEncode, FreeTextToF71, and Pack /
// Unpack. Used at Layer 2 to compose Layer 1 primitives into FT8 bit
// sequences (77-bit message bodies, 91-bit message+CRC, 174-bit
// codewords during round-trip checks) per QEX paper Table 1's i3.n3
// message-type layouts.
//
// Zero value is ready to use.
//
// MSB-first means: Append(0xA5, 8) appends the bits in the order
// [1, 0, 1, 0, 0, 1, 0, 1] — top bit first, bottom bit last. Matches
// FT8's wire-order convention and what the other codec primitives
// produce.
type BitBuilder struct {
	bits []byte
}

// Append appends the low nbits bits of value, MSB-first. Returns the
// receiver for method chaining.
//
// Panics if nbits is outside [0, 64], or if value has any bits set
// above the nbits-th position (i.e. value >> nbits != 0). The
// overflow check catches caller-side width-mismatch bugs (e.g.
// passing a full uint32 c28 into a 27-bit slot) at the moment they
// happen, rather than producing a garbled 77-bit message that
// surfaces as a decode failure much later.
func (b *BitBuilder) Append(value uint64, nbits int) *BitBuilder {
	const op = "codec.BitBuilder.Append"
	if nbits < 0 || nbits > 64 {
		panic(op + ": nbits must be in [0, 64], got " + strconv.Itoa(nbits))
	}
	// nbits == 64 short-circuits the overflow check — any uint64
	// value fits in 64 bits by definition. (Go's shift spec defines
	// uint64(x) >> 64 == 0, so dropping the guard would also work,
	// but the explicit short-circuit avoids leaving readers wondering
	// whether we were guarding against C-style undefined shift.)
	if nbits < 64 && value>>nbits != 0 {
		panic(op + ": value 0x" + strconv.FormatUint(value, 16) + " has bits above bit position " + strconv.Itoa(nbits))
	}
	for i := nbits - 1; i >= 0; i-- {
		b.bits = append(b.bits, byte((value>>i)&1))
	}
	return b
}

// AppendBits appends an already-bit-per-byte slice (e.g. the output
// of FreeTextToF71). Each element of src must be 0 or 1; panics
// otherwise. Returns the receiver for method chaining.
func (b *BitBuilder) AppendBits(src []byte) *BitBuilder {
	const op = "codec.BitBuilder.AppendBits"
	// Two-pass: validate first, then bulk-copy via variadic append.
	// A fail-fast loop that appended byte-by-byte would lose the
	// memcpy-fast-path that `append(slice, src...)` takes for []byte
	// — measurable for the larger primitives (f71 = 71 bytes).
	for i, v := range src {
		if v > 1 {
			panic(op + ": src[" + strconv.Itoa(i) + "] = " + strconv.Itoa(int(v)) + ", must be 0 or 1")
		}
	}
	b.bits = append(b.bits, src...)
	return b
}

// Bits returns the accumulated buffer. The returned slice aliases
// the builder's internal storage — callers that mutate it (or that
// keep using the builder afterwards) will corrupt subsequent
// appends. Use slices.Clone if independence is required.
func (b *BitBuilder) Bits() []byte {
	return b.bits
}

// Len returns the number of bits accumulated.
func (b *BitBuilder) Len() int {
	return len(b.bits)
}
