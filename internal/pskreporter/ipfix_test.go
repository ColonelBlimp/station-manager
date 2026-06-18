package pskreporter

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

// hexb parses a space/newline-separated hex string into bytes (test readability).
func hexb(t *testing.T, s string) []byte {
	t.Helper()
	clean := strings.NewReplacer(" ", "", "\n", "", "\t", "").Replace(s)
	b, err := hex.DecodeString(clean)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func TestAppendVarStr(t *testing.T) {
	// "N1DQ" → 04 4E 31 44 51 (spec worked example).
	if got := appendVarStr(nil, "N1DQ"); !bytes.Equal(got, hexb(t, "04 4E 31 44 51")) {
		t.Fatalf("appendVarStr(N1DQ) = % X", got)
	}
	// Empty string → a single 0x00 length byte (the unknown-locator case).
	if got := appendVarStr(nil, ""); !bytes.Equal(got, []byte{0x00}) {
		t.Fatalf("appendVarStr(\"\") = % X, want 00", got)
	}
}

// TestReceiverTemplate_MatchesSpec reproduces the spec's 4-field receiver options
// template descriptor (callsign, locator, decoderSoftware, antennaInformation)
// byte-for-byte. We always emit the 4-field shape so the cached template can't
// desync when antenna is added/removed at runtime.
func TestReceiverTemplate_MatchesSpec(t *testing.T) {
	want := hexb(t, `00 03 00 2C 99 92 00 04 00 01
		80 02 FF FF 00 00 76 8F
		80 04 FF FF 00 00 76 8F
		80 08 FF FF 00 00 76 8F
		80 09 FF FF 00 00 76 8F
		00 00`)
	got := encodeTemplate(rxTemplateID, 1, receiverFields())
	if !bytes.Equal(got, want) {
		t.Fatalf("receiver template\n got % X\nwant % X", got, want)
	}
}

// TestSenderTemplate_MatchesSpec reproduces the spec's count-8 sender template
// (senderCallsign, frequency, sNR, iMD, mode, informationSource, senderLocator,
// flowStartSeconds) byte-for-byte.
func TestSenderTemplate_MatchesSpec(t *testing.T) {
	want := hexb(t, `00 02 00 44 99 93 00 08
		80 01 FF FF 00 00 76 8F
		80 05 00 04 00 00 76 8F
		80 06 00 01 00 00 76 8F
		80 07 00 01 00 00 76 8F
		80 0A FF FF 00 00 76 8F
		80 0B 00 01 00 00 76 8F
		80 03 FF FF 00 00 76 8F
		00 96 00 04`)
	got := encodeTemplate(txTemplateID, 0, senderFields)
	if !bytes.Equal(got, want) {
		t.Fatalf("sender template\n got % X\nwant % X", got, want)
	}
}

// TestReceiverRecord_MatchesSpec reproduces the spec's worked-example receiver
// record (N1DQ / FN42hn / "Homebrew v5.6") byte-for-byte. With no antenna the now-
// always-present antennaInformation is a length-0 field whose 0x00 byte coincides
// with what was previously padding — so the encoded bytes are identical to the
// spec's 3-field example, even though we now use the fixed 4-field template.
func TestReceiverRecord_MatchesSpec(t *testing.T) {
	want := hexb(t, `99 92 00 20
		04 4E 31 44 51
		06 46 4E 34 32 68 6E
		0D 48 6F 6D 65 62 72 65 77 20 76 35 2E 36
		00 00`)
	got := encodeReceiverRecord(Receiver{Call: "N1DQ", Locator: "FN42hn", Software: "Homebrew v5.6"})
	if !bytes.Equal(got, want) {
		t.Fatalf("receiver record\n got % X\nwant % X", got, want)
	}
}

// TestSenderRecords hand-builds the expected count-8 block for one spot and checks
// it, including iMD=0, the empty/known locator, and the block's 4-byte padding.
func TestSenderRecords(t *testing.T) {
	spot := Spot{Call: "N1DQ", FreqHz: 14070567, SNR: -10, Mode: "FT8", Grid: "FN42", TimeUnix: 1200960084}
	// data: 04"N1DQ" | 00D6B327 freq | F6 snr(-10) | 00 imd | 03"FT8" | 01 infoSrc | 04"FN42" | 47953254 time
	want := hexb(t, `99 93 00 20
		04 4E 31 44 51
		00 D6 B3 27
		F6
		00
		03 46 54 38
		01
		04 46 4E 34 32
		47 95 32 54
		00 00 00`)
	got := encodeSenderRecords([]Spot{spot})
	if !bytes.Equal(got, want) {
		t.Fatalf("sender record\n got % X\nwant % X", got, want)
	}

	// Unknown grid → an empty-string locator (length 0x00), not omitted.
	noGrid := encodeSenderRecords([]Spot{{Call: "K1ABC", FreqHz: 1, SNR: 0, Mode: "FT8", TimeUnix: 1}})
	if !bytes.Contains(noGrid, []byte{0x03, 'F', 'T', '8', infoSourceAutomatic, 0x00}) {
		t.Fatalf("unknown-grid block missing empty-locator field: % X", noGrid)
	}
}

// TestEncodeDatagram_HeaderAndAlignment checks the header (version/length) and that
// the whole datagram is 4-byte aligned, with and without templates.
func TestEncodeDatagram_HeaderAndAlignment(t *testing.T) {
	r := Receiver{Call: "G0XYZ", Locator: "IO91", Software: "StationManager 2.0.0"}
	spots := []Spot{{Call: "VK3ABC", FreqHz: 14074000, SNR: -7, Mode: "FT8", Grid: "QF22", TimeUnix: 1700000000}}

	for _, withTpl := range []bool{true, false} {
		dg := encodeDatagram(1700000000, 1, 0xDEADBEEF, withTpl, r, spots)
		if binary.BigEndian.Uint16(dg[0:2]) != ipfixVersion {
			t.Fatalf("version = %#x, want %#x", binary.BigEndian.Uint16(dg[0:2]), ipfixVersion)
		}
		if int(binary.BigEndian.Uint16(dg[2:4])) != len(dg) {
			t.Fatalf("length field %d != datagram len %d", binary.BigEndian.Uint16(dg[2:4]), len(dg))
		}
		if len(dg)%4 != 0 {
			t.Fatalf("datagram not 4-byte aligned: len %d", len(dg))
		}
		if binary.BigEndian.Uint32(dg[12:16]) != 0xDEADBEEF {
			t.Fatal("session identifier not in header")
		}
	}
}
