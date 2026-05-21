package ft8

// sampleRing is a fixed-capacity wrapping buffer of float32 samples
// used by the slot scheduler to hold "the last N samples we've seen"
// without per-slot allocation.
//
// Concurrency: not safe for concurrent use. The scheduler owns the
// ring and serialises Append / Snapshot on a single goroutine.
type sampleRing struct {
	buf    []float32
	w      int   // next write index in buf
	filled int64 // total samples ever written (capped saturating semantics not required for callers)
}

// newSampleRing allocates a ring of the given capacity.
func newSampleRing(cap int) *sampleRing {
	return &sampleRing{buf: make([]float32, cap)}
}

// Cap returns the ring capacity.
func (r *sampleRing) Cap() int { return len(r.buf) }

// Filled reports the total number of samples Append has ever consumed.
// Callers use this to gate the first Snapshot until at least Cap()
// samples have arrived (otherwise the snapshot would include stale
// zeroed tail samples).
func (r *sampleRing) Filled() int64 { return r.filled }

// Append copies src into the ring, wrapping at the capacity. Samples
// older than Cap() positions back are overwritten.
func (r *sampleRing) Append(src []float32) {
	n := len(src)
	if n == 0 {
		return
	}
	cap := len(r.buf)
	if n >= cap {
		// Source is at least one full ring — copy only the most recent cap samples
		// into the buffer starting at position 0, and reset the write head.
		copy(r.buf, src[n-cap:])
		r.w = 0
		r.filled += int64(n)
		return
	}
	// Fits in one or two contiguous segments.
	end := r.w + n
	if end <= cap {
		copy(r.buf[r.w:end], src)
		r.w = end % cap
	} else {
		first := cap - r.w
		copy(r.buf[r.w:], src[:first])
		copy(r.buf[:n-first], src[first:])
		r.w = n - first
	}
	r.filled += int64(n)
}

// Snapshot returns a new slice of length Cap() containing the most
// recent Cap() samples in chronological order (oldest first). If
// fewer than Cap() samples have been appended, the head of the
// returned slice is zero-padded.
func (r *sampleRing) Snapshot() []float32 {
	cap := len(r.buf)
	out := make([]float32, cap)
	// The sample at index w is the oldest (next to be overwritten);
	// linearise from w forward, wrapping at cap.
	copy(out, r.buf[r.w:])
	copy(out[cap-r.w:], r.buf[:r.w])
	return out
}
