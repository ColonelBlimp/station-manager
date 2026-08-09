package ft8

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// containsText reports whether want is among the decoded texts.
func containsText(texts []string, want string) bool {
	for _, t := range texts {
		if t == want {
			return true
		}
	}
	return false
}

// TestDecodeFile_20mSlot1 is the integration oracle: it proves go-ft8 is wired
// in and decoding correctly against a known corpus slot. It is not a parity
// gate against jt9 (that lives in go-ft8); it pins a stable lower bound plus a
// handful of high-confidence callsigns so a regression in the wiring or the
// pinned go-ft8 version is caught.
func TestDecodeFile_20mSlot1(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}

	msgs, err := DecodeFile(filepath.Join("testdata", "20m_slot1.wav"), true, logging.Noop())
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if len(msgs) < 20 {
		t.Fatalf("decoded %d messages, want >= 20", len(msgs))
	}

	got := make([]string, len(msgs))
	for i, m := range msgs {
		got[i] = m.Text
		if m.FreqHz < 0 || m.FreqHz > 3000 {
			t.Errorf("decode %q has implausible freq %.1f Hz", m.Text, m.FreqHz)
		}
	}
	for _, want := range []string{
		"CQ DX S56GD JN65",
		"PA2JFX SV3CNX 73",
		"YM4KF G5MJF -24",
	} {
		if !containsText(got, want) {
			t.Errorf("expected decode %q not found", want)
		}
	}
}

func TestDecodeFile_LiveSlot1(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	msgs, err := DecodeFile(filepath.Join("testdata", "live_slot1.wav"), true, logging.Noop())
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if len(msgs) < 25 {
		t.Fatalf("decoded %d messages, want >= 25", len(msgs))
	}
}

func TestReadSlotWAV_ReadsFullSlot(t *testing.T) {
	samples, err := readSlotWAV(filepath.Join("testdata", "20m_slot1.wav"))
	if err != nil {
		t.Fatalf("readSlotWAV: %v", err)
	}
	// 15 s * 12 kHz = 180000 samples.
	if len(samples) != 180000 {
		t.Fatalf("read %d samples, want 180000", len(samples))
	}
}

// TestReadSlotWAV_RejectsWrongRate proves the reader enforces go-ft8's input
// contract instead of silently mis-decoding a non-12 kHz file.
func TestReadSlotWAV_RejectsWrongRate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrongrate.wav")
	writeTestWAV(t, path, 48000, 1, 16, make([]byte, 200))
	if _, err := readSlotWAV(path); err == nil {
		t.Fatal("expected error for 48 kHz file, got nil")
	}
}

// TestDecodeSlot_FailSoft confirms a decode never panics out of the package:
// empty input and a nil logger are both tolerated.
func TestDecodeSlot_FailSoft(t *testing.T) {
	if got := DecodeSlot(nil, false, nil); len(got) != 0 {
		t.Fatalf("empty input decoded %d messages, want 0", len(got))
	}
	if got := DecodeSlot([]int16{}, false, logging.Noop()); len(got) != 0 {
		t.Fatalf("empty slice decoded %d messages, want 0", len(got))
	}
}

// TestDecodeSlot_RejectsWrongLength confirms the checked API rejects a slot
// that isn't a full 180000-sample frame: DecodeSlot returns nil (and logs a
// warn) rather than feeding a malformed slot to the decoder. Rejection happens
// before decode work, so this is cheap — no -short gate needed.
func TestDecodeSlot_RejectsWrongLength(t *testing.T) {
	short := make([]int16, 1000)
	if got := DecodeSlot(short, false, logging.Noop()); got != nil {
		t.Fatalf("wrong-length slot decoded %d messages, want nil", len(got))
	}
}

// TestDecodeFile_RejectsWrongDuration proves DecodeFile fails on a correctly
// formatted (12 kHz / mono / 16-bit) WAV that isn't exactly one slot long,
// rather than forwarding it to fail-soft DecodeSlot and reporting "0 decodes"
// with a nil error. The offline path takes an arbitrary operator file, so a
// wrong-duration WAV is an error the operator must see, not silent success.
func TestDecodeFile_RejectsWrongDuration(t *testing.T) {
	// Core regression: 100 samples, not 180000. Rejection is cheap (no decode).
	shortPath := filepath.Join(t.TempDir(), "short.wav")
	writeTestWAV(t, shortPath, uint32(goft8.SampleRate), 1, 16, make([]byte, 200))
	if _, err := DecodeFile(shortPath, false, logging.Noop()); err == nil {
		t.Fatal("expected error for wrong-duration WAV, got nil")
	}

	// Guard against the length check rejecting a valid slot: a full slot of
	// silence must decode cleanly (0 messages, no error). This runs a real
	// decode, so gate it under -short like the corpus tests above.
	if testing.Short() {
		return
	}
	fullPath := filepath.Join(t.TempDir(), "full.wav")
	writeTestWAV(t, fullPath, uint32(goft8.SampleRate), 1, 16, make([]byte, SlotSamples*2))
	msgs, err := DecodeFile(fullPath, false, logging.Noop())
	if err != nil {
		t.Fatalf("full-length silent slot: unexpected error %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("silent slot decoded %d messages, want 0", len(msgs))
	}
}

// writeTestWAV writes a minimal canonical PCM WAV for reader-contract tests.
func writeTestWAV(t *testing.T, path string, rate uint32, channels, bits uint16, data []byte) {
	t.Helper()
	var buf []byte
	put := func(b ...byte) { buf = append(buf, b...) }
	putU32 := func(v uint32) { buf = binary.LittleEndian.AppendUint32(buf, v) }
	putU16 := func(v uint16) { buf = binary.LittleEndian.AppendUint16(buf, v) }

	byteRate := rate * uint32(channels) * uint32(bits/8)
	blockAlign := channels * (bits / 8)

	put('R', 'I', 'F', 'F')
	putU32(uint32(36 + len(data)))
	put('W', 'A', 'V', 'E')
	put('f', 'm', 't', ' ')
	putU32(16)
	putU16(1) // PCM
	putU16(channels)
	putU32(rate)
	putU32(byteRate)
	putU16(blockAlign)
	putU16(bits)
	put('d', 'a', 't', 'a')
	putU32(uint32(len(data)))
	buf = append(buf, data...)

	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write test wav: %v", err)
	}
}

// TestDropOwnTransmissions covers the self-decode filter: SM decodes its own FT8
// TX off the rig's audio bleed, so a keyed slot yields a decode whose sender (DE)
// is our own call. Those must be dropped; everything else (answerers, others' CQs,
// free text) passes. A legitimate decode is never FROM our own call, so filtering
// is unconditionally safe.
func TestDropOwnTransmissions(t *testing.T) {
	msgs := []goft8.DecodedMessage{
		{Text: "CQ 7Q5MLV KH78"},     // our own CQ (self-decode) → drop
		{Text: "BI4JJO 7Q5MLV RR73"}, // our own RR73 (self-decode) → drop
		{Text: "7Q5MLV JQ3UGN PM74"}, // an answerer calling us → keep
		{Text: "7Q5MLV BI4JJO R-02"}, // the worked station rogering us → keep
		{Text: "CQ A61DI LL64"},      // someone else's CQ → keep
		{Text: "TU 73 GL"},           // free text, no resolvable sender → keep
	}
	want := []string{
		"7Q5MLV JQ3UGN PM74",
		"7Q5MLV BI4JJO R-02",
		"CQ A61DI LL64",
		"TU 73 GL",
	}

	got := dropOwnTransmissions(msgs, "7Q5MLV")
	if len(got) != len(want) {
		t.Fatalf("kept %d messages, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("kept[%d] = %q, want %q", i, got[i].Text, w)
		}
	}

	// Own call is matched case-insensitively.
	if n := len(dropOwnTransmissions(msgs, "7q5mlv")); n != len(want) {
		t.Errorf("lowercase ownCall kept %d, want %d", n, len(want))
	}
	// Empty / blank own call → no filtering (nothing to compare against).
	if n := len(dropOwnTransmissions(msgs, "")); n != len(msgs) {
		t.Errorf("empty ownCall kept %d, want %d (no-op)", n, len(msgs))
	}
	if n := len(dropOwnTransmissions(msgs, "   ")); n != len(msgs) {
		t.Errorf("blank ownCall kept %d, want %d (no-op)", n, len(msgs))
	}
}

// TestDropUnparsed pins the curated-path filter (codex P2 on 1df6d94d):
// go-ft8 v0.8.0 surfaces CRC-valid unsupported/reserved/invalid payloads as
// text-less DecodedMessages, and every curated consumer assumes text — only
// ParseStatusParsed rows may pass. The evidence branch (design §4) will tap
// upstream of this filter; nothing here may run before it captures.
func TestDropUnparsed(t *testing.T) {
	parsed := goft8.DecodedMessage{Text: "CQ K1ABC FN42", ParseStatus: goft8.ParseStatusParsed, SNR: -8}
	unsupported := goft8.DecodedMessage{ParseStatus: goft8.ParseStatusUnsupported, SNR: -12}
	invalid := goft8.DecodedMessage{ParseStatus: goft8.ParseStatusInvalid, SNR: -20}

	out := dropUnparsed([]goft8.DecodedMessage{unsupported, parsed, invalid})
	if len(out) != 1 {
		t.Fatalf("filter kept %d messages, want only the parsed one: %+v", len(out), out)
	}
	if out[0].Text != "CQ K1ABC FN42" {
		t.Fatalf("filter kept the wrong message: %+v", out[0])
	}

	if got := dropUnparsed(nil); len(got) != 0 {
		t.Fatalf("nil in, non-empty out: %+v", got)
	}
}
