package ft8

import "math"

// floatToInt16 converts a normalised [-1.0, 1.0] float32 audio sample to the
// int16 the FT8 decode pipeline consumes, clamping out-of-range values.
//
// The capture layer (miniaudio) delivers float32; go-ft8's checked decode API
// wants int16 at 12 kHz mono — the WSJT-X/jt9 sample contract. This is the
// single conversion point. It lives in an untagged file (not source_cgo.go)
// so the conversion is unit-tested in the CGO-free CI lane even though its
// only caller is the CGO capture source.
func floatToInt16(f float32) int16 {
	v := int32(f * math.MaxInt16)
	if v > math.MaxInt16 {
		return math.MaxInt16
	}
	if v < math.MinInt16 {
		return math.MinInt16
	}
	return int16(v)
}
