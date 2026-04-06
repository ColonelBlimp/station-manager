package message

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// --------------- 3DA0 (Swaziland) workaround ---------------------------------

func TestEncodeCallsign_3DA0_RoundTrip(t *testing.T) {
	tests := []struct {
		call string
	}{
		{"3DA0AA"},
		{"3DA0XY"},
		{"3DA0ZZ"},
	}
	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			n28, err := EncodeCallsign(tt.call)
			require.NoError(t, err)
			decoded, err := DecodeCallsign(n28)
			require.NoError(t, err)
			require.Equal(t, tt.call, decoded)
		})
	}
}

// TestEncodeCallsign_3DA0_PacksAs3D0 verifies the pack-time workaround:
// "3DA0XY" is encoded as "3D0XY" internally.
func TestEncodeCallsign_3DA0_PacksAs3D0(t *testing.T) {
	n28_3da0, err := EncodeCallsign("3DA0XY")
	require.NoError(t, err)

	// Encode the remapped form directly.
	n28_3d0, err := EncodeCallsign("3D0XY")
	require.NoError(t, err)

	require.Equal(t, n28_3d0, n28_3da0,
		"3DA0XY should encode to the same n28 as 3D0XY")
}

// TestDecodeCallsign_3D0_DecodesAs3DA0 verifies that "3D0..." always decodes
// back to "3DA0...".
func TestDecodeCallsign_3D0_DecodesAs3DA0(t *testing.T) {
	// Encode "3D0XY" (the internal form) and decode it.
	n28, err := EncodeCallsign("3D0XY")
	require.NoError(t, err)

	decoded, err := DecodeCallsign(n28)
	require.NoError(t, err)
	require.Equal(t, "3DA0XY", decoded,
		"3D0XY should always decode as 3DA0XY")
}

// --------------- 3X (Guinea) workaround --------------------------------------

func TestEncodeCallsign_3X_RoundTrip(t *testing.T) {
	tests := []struct {
		call string
	}{
		{"3XA1BC"},
		{"3XA0AA"},
		{"3XZ9ZZ"},
	}
	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			n28, err := EncodeCallsign(tt.call)
			require.NoError(t, err)
			decoded, err := DecodeCallsign(n28)
			require.NoError(t, err)
			require.Equal(t, tt.call, decoded)
		})
	}
}

// TestEncodeCallsign_3X_PacksAsQ verifies the pack-time workaround:
// "3XAYY" is encoded as "QAYY" internally.
func TestEncodeCallsign_3X_PacksAsQ(t *testing.T) {
	n28_3x, err := EncodeCallsign("3XA1BC")
	require.NoError(t, err)

	// Encode the remapped form directly.
	n28_q, err := EncodeCallsign("QA1BC")
	require.NoError(t, err)

	require.Equal(t, n28_q, n28_3x,
		"3XA1BC should encode to the same n28 as QA1BC")
}

// TestDecodeCallsign_Q_DecodesAs3X verifies that "Q[A-Z]..." always decodes
// back to "3X[A-Z]...".
func TestDecodeCallsign_Q_DecodesAs3X(t *testing.T) {
	n28, err := EncodeCallsign("QA1BC")
	require.NoError(t, err)

	decoded, err := DecodeCallsign(n28)
	require.NoError(t, err)
	require.Equal(t, "3XA1BC", decoded,
		"QA1BC should always decode as 3XA1BC")
}

// --------------- Workaround integration with Pack/Unpack ---------------------

func TestPackType1_3DA0_RoundTrip(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ",
		Call2:   "3DA0XY",
		Grid:    "KG40",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "CQ 3DA0XY KG40", decoded.String())
}

func TestPackType1_3X_RoundTrip(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ",
		Call2:   "3XA1BC",
		Grid:    "IK14",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "CQ 3XA1BC IK14", decoded.String())
}

// --------------- packCallWorkaround ------------------------------------------

func TestPackCallWorkaround_NoOp(t *testing.T) {
	// Normal callsigns should pass through unchanged.
	tests := []string{"W1AW", "VK2XYZ", "K1ABC", "9A1A"}
	for _, call := range tests {
		require.Equal(t, call, packCallWorkaround(call))
	}
}

func TestPackCallWorkaround_3DA0(t *testing.T) {
	require.Equal(t, "3D0XY", packCallWorkaround("3DA0XY"))
	require.Equal(t, "3D0AA", packCallWorkaround("3DA0AA"))
}

func TestPackCallWorkaround_3X(t *testing.T) {
	require.Equal(t, "QA1BC", packCallWorkaround("3XA1BC"))
	require.Equal(t, "QZ9ZZ", packCallWorkaround("3XZ9ZZ"))
}

// --------------- unpackCallWorkaround ----------------------------------------

func TestUnpackCallWorkaround_NoOp(t *testing.T) {
	tests := []string{"W1AW", "VK2XYZ", "K1ABC", "9A1A"}
	for _, call := range tests {
		require.Equal(t, call, unpackCallWorkaround(call))
	}
}

func TestUnpackCallWorkaround_3D0(t *testing.T) {
	require.Equal(t, "3DA0XY", unpackCallWorkaround("3D0XY"))
	require.Equal(t, "3DA0AA", unpackCallWorkaround("3D0AA"))
}

func TestUnpackCallWorkaround_Q(t *testing.T) {
	require.Equal(t, "3XA1BC", unpackCallWorkaround("QA1BC"))
	require.Equal(t, "3XZ9ZZ", unpackCallWorkaround("QZ9ZZ"))
}

// Q followed by a digit should NOT be remapped (not a 3X workaround).
func TestUnpackCallWorkaround_QDigit_NoRemap(t *testing.T) {
	require.Equal(t, "Q1ABC", unpackCallWorkaround("Q1ABC"))
}
