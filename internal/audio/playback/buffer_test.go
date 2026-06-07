package playback

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFillFrame_PartialThenSilence(t *testing.T) {
	buf := []int16{1, 2, 3, 4, 5}

	// First frame consumes the first 3 samples.
	out := make([]int16, 3)
	written, pos := fillFrame(out, buf, 0)
	require.Equal(t, 3, written)
	require.Equal(t, 3, pos)
	require.Equal(t, []int16{1, 2, 3}, out)

	// Second frame gets the last 2 real samples then a silence pad.
	out = make([]int16, 3)
	written, pos = fillFrame(out, buf, pos)
	require.Equal(t, 2, written)
	require.Equal(t, 5, pos)
	require.Equal(t, []int16{4, 5, 0}, out)

	// Third frame is pure silence; written stays 0 and pos doesn't advance.
	out = []int16{9, 9, 9}
	written, pos = fillFrame(out, buf, pos)
	require.Equal(t, 0, written)
	require.Equal(t, 5, pos)
	require.Equal(t, []int16{0, 0, 0}, out)
}

func TestFillFrame_ExactFit(t *testing.T) {
	buf := []int16{1, 2, 3, 4}
	out := make([]int16, 4)
	written, pos := fillFrame(out, buf, 0)
	require.Equal(t, 4, written)
	require.Equal(t, 4, pos)
	require.Equal(t, []int16{1, 2, 3, 4}, out)
}

func TestFillFrame_FrameLargerThanBuffer(t *testing.T) {
	buf := []int16{7, 8}
	out := []int16{1, 1, 1, 1, 1}
	written, pos := fillFrame(out, buf, 0)
	require.Equal(t, 2, written)
	require.Equal(t, 2, pos)
	require.Equal(t, []int16{7, 8, 0, 0, 0}, out)
}

func TestFillFrame_PosPastEnd(t *testing.T) {
	buf := []int16{1, 2, 3}
	out := []int16{5, 5}
	written, pos := fillFrame(out, buf, 10)
	require.Equal(t, 0, written)
	require.Equal(t, 10, pos)
	require.Equal(t, []int16{0, 0}, out)
}

func TestFillFrame_EmptyBuffer(t *testing.T) {
	out := []int16{4, 4}
	written, pos := fillFrame(out, nil, 0)
	require.Equal(t, 0, written)
	require.Equal(t, 0, pos)
	require.Equal(t, []int16{0, 0}, out)
}

func TestBytesAsInt16(t *testing.T) {
	// Little-endian: 0x0001 = 1, 0x0002 = 2.
	b := []byte{1, 0, 2, 0, 0xFF, 0xFF}
	got := bytesAsInt16(b)
	require.Equal(t, []int16{1, 2, -1}, got)
}

func TestBytesAsInt16_TooShort(t *testing.T) {
	require.Nil(t, bytesAsInt16([]byte{0}))
	require.Nil(t, bytesAsInt16(nil))
}
