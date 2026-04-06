package message

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// --------------- Pack Type 0 -------------------------------------------------

func TestPackType0_RoundTrip(t *testing.T) {
	texts := []string{
		"TNX BOB 73 GL",
		"HELLO",
		"",
		"A",
		"0123456789ABC",
		"+-./?",
		"?????????????",
		"CQ CQ CQ",
		"73 DE W1AW",
		"TEST 1 2 3",
	}
	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			msg := &Message{
				MsgType:  TypeFreeText,
				FreeText: text,
			}
			payload, err := Pack(msg)
			require.NoError(t, err)

			decoded, err := Unpack(payload)
			require.NoError(t, err)
			require.Equal(t, TypeFreeText, decoded.MsgType)
			require.Equal(t, text, decoded.String())
		})
	}
}

// TestPackType0_I3N3Bits verifies that packed Type 0 messages have i3=0, n3=0.
func TestPackType0_I3N3Bits(t *testing.T) {
	msg := &Message{
		MsgType:  TypeFreeText,
		FreeText: "HELLO",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	i3 := UnpackBits(payload[:], 74, 3)
	n3 := UnpackBits(payload[:], 71, 3)
	require.Equal(t, uint64(0), i3, "i3 must be 0")
	require.Equal(t, uint64(0), n3, "n3 must be 0")
}

// TestPackType0_KnownPayload verifies a known free-text payload against
// independently derived values.
func TestPackType0_KnownPayload(t *testing.T) {
	msg := &Message{
		MsgType:  TypeFreeText,
		FreeText: "TNX BOB 73 GL",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	// Independently derived: "TNX BOB 73 GL" encodes to hi=0x30, lo=0x4ACFFAE330617641.
	// Layout: f71(7 hi bits, 64 lo bits) | n3=000(3) | i3=000(3)
	// hi=0x30=0b0110000, packed in 7 bits → byte[0] bits 0-6 = 0110000_
	// lo follows at offset 7.
	//
	// Verify by unpacking the encoded values.
	gotHi := uint8(UnpackBits(payload[:], 0, 7))
	gotLo := UnpackBits(payload[:], 7, 64)
	require.Equal(t, uint8(0x30), gotHi, "hi mismatch")
	require.Equal(t, uint64(0x4ACFFAE330617641), gotLo, "lo mismatch")

	// Also verify the round-trip.
	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "TNX BOB 73 GL", decoded.String())
}

// TestPackType0_DoesNotMutateMessage verifies that Pack does not modify the
// input Message's encoded fields.
func TestPackType0_DoesNotMutateMessage(t *testing.T) {
	msg := &Message{
		MsgType:  TypeFreeText,
		FreeText: "TNX BOB 73 GL",
	}
	_, err := Pack(msg)
	require.NoError(t, err)
	require.Equal(t, uint8(0), msg.FreeTextHi, "Pack must not mutate FreeTextHi")
	require.Equal(t, uint64(0), msg.FreeTextLo, "Pack must not mutate FreeTextLo")
}

// TestPackType0_TooLong verifies that a free-text message exceeding 13 chars
// returns an error.
func TestPackType0_TooLong(t *testing.T) {
	msg := &Message{
		MsgType:  TypeFreeText,
		FreeText: "01234567890123", // 14 chars
	}
	_, err := Pack(msg)
	require.Error(t, err)
}

// TestUnpackType0_AllSpaces verifies the edge case of all-spaces payload.
func TestUnpackType0_AllSpaces(t *testing.T) {
	// All-spaces encode: hi=0x44, lo=0x9979E458016C9FFF
	var payload [MsgBytes]byte
	PackBits(payload[:], 0, 7, uint64(0x44))
	PackBits(payload[:], 7, 64, 0x9979E458016C9FFF)
	// i3=0, n3=0 already zero.

	msg, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, TypeFreeText, msg.MsgType)
	require.Equal(t, "", msg.FreeText) // all-spaces trims to empty
}

// TestPackType0_CaseInsensitive verifies that lowercase input produces the
// same payload as uppercase.
func TestPackType0_CaseInsensitive(t *testing.T) {
	msg1 := &Message{MsgType: TypeFreeText, FreeText: "hello"}
	msg2 := &Message{MsgType: TypeFreeText, FreeText: "HELLO"}

	p1, err := Pack(msg1)
	require.NoError(t, err)
	p2, err := Pack(msg2)
	require.NoError(t, err)

	require.Equal(t, hex.EncodeToString(p1[:]), hex.EncodeToString(p2[:]))
}
