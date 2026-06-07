package playback

import "unsafe"

// BytesPerInt16 is the number of bytes in an int16 sample.
const BytesPerInt16 = 2

// fillFrame copies as much of buf[pos:] as fits into out (the device's
// per-callback output frame, mono int16), zero-pads any remainder with
// silence, and reports how many real samples were written plus the advanced
// playback position.
//
// It is the pure core of the playback callback, factored out so the
// copy/silence-pad logic is unit-testable in the CGO-free lane. When pos has
// reached or passed len(buf) the whole frame is silence and written is 0 —
// the device keeps running (outputting silence) until the caller Stops it,
// which is how a finished waveform tails off cleanly without a click.
func fillFrame(out, buf []int16, pos int) (written, newPos int) {
	if pos < 0 {
		pos = 0
	}
	if pos < len(buf) {
		written = copy(out, buf[pos:])
	}
	for i := written; i < len(out); i++ {
		out[i] = 0
	}
	return written, pos + written
}

// bytesAsInt16 performs zero-copy conversion of a byte slice to an int16
// slice, for writing samples directly into the malgo callback's output
// buffer.
//
// WARNING: the returned slice shares memory with the input — it is only valid
// for the duration of the callback. WARNING: the byte slice must be 2-byte
// aligned. malgo callback buffers satisfy this; do not call with arbitrary
// byte slices.
func bytesAsInt16(data []byte) []int16 {
	if len(data) < BytesPerInt16 {
		return nil
	}
	numSamples := len(data) / BytesPerInt16
	return unsafe.Slice((*int16)(unsafe.Pointer(&data[0])), numSamples)
}
