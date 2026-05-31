package ft8

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRing_AppendBelowCap(t *testing.T) {
	r := newSampleRing(8)
	r.Append([]int16{1, 2, 3})
	require.Equal(t, int64(3), r.Filled())
	snap := r.Snapshot()
	// Oldest is the leading zero pad, then 1, 2, 3 at the end.
	require.Equal(t, []int16{0, 0, 0, 0, 0, 1, 2, 3}, snap)
}

func TestRing_AppendExactlyCap(t *testing.T) {
	r := newSampleRing(4)
	r.Append([]int16{1, 2, 3, 4})
	require.Equal(t, []int16{1, 2, 3, 4}, r.Snapshot())
}

func TestRing_AppendCrossWrap(t *testing.T) {
	r := newSampleRing(4)
	r.Append([]int16{1, 2, 3})
	r.Append([]int16{4, 5, 6})
	// Most recent 4 samples in chronological order: 3, 4, 5, 6.
	require.Equal(t, []int16{3, 4, 5, 6}, r.Snapshot())
}

func TestRing_AppendOverflowSingleCall(t *testing.T) {
	r := newSampleRing(4)
	r.Append([]int16{1, 2, 3, 4, 5, 6})
	// Most recent 4: 3, 4, 5, 6.
	require.Equal(t, []int16{3, 4, 5, 6}, r.Snapshot())
}

func TestRing_AppendOverflowMassive(t *testing.T) {
	r := newSampleRing(4)
	src := make([]int16, 100)
	for i := range src {
		src[i] = int16(i)
	}
	r.Append(src)
	// Last 4 samples: 96, 97, 98, 99.
	require.Equal(t, []int16{96, 97, 98, 99}, r.Snapshot())
}

func TestRing_AppendEmptyIsNoOp(t *testing.T) {
	r := newSampleRing(4)
	r.Append([]int16{1, 2})
	r.Append(nil)
	r.Append([]int16{})
	require.Equal(t, int64(2), r.Filled())
}

func TestRing_AppendManySmallBatches(t *testing.T) {
	r := newSampleRing(5)
	for i := 1; i <= 12; i++ {
		r.Append([]int16{int16(i)})
	}
	// Most recent 5: 8, 9, 10, 11, 12.
	require.Equal(t, []int16{8, 9, 10, 11, 12}, r.Snapshot())
}

func TestRing_SnapshotIsIndependentCopy(t *testing.T) {
	r := newSampleRing(4)
	r.Append([]int16{1, 2, 3, 4})
	snap := r.Snapshot()
	snap[0] = 999
	require.Equal(t, []int16{1, 2, 3, 4}, r.Snapshot(),
		"mutating a snapshot must not affect the ring")
}
