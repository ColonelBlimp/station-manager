package ft8

import (
	"strings"
	"testing"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// TestFieldDay_RoundTrip is the RF-safety gate for ARRL Field Day TX: every message
// in the answer-a-CQ-FD ladder must encode (go-ft8's packer handles FD exchange
// messages), modulate to audio, and decode back to the same text with the SHIPPED
// decoder — proving we never key a frame the rest of the FT8 world can't read. Zero
// RF. Heavy (full decode), so gated under -short like the other round-trip proofs.
//
// The ladder, with us = 7Q5MLV (class 1D, section DX) answering K1ABC's CQ FD:
//
//	K1ABC : CQ FD K1ABC FN42          (their CQ — we decode it)
//	us    : K1ABC 7Q5MLV 1D DX        (Tx1: our exchange)
//	K1ABC : 7Q5MLV K1ABC R 2A EMA     (their R + exchange — we decode it)
//	us    : K1ABC 7Q5MLV RR73         (Tx2: our roger; QSO logs)
func TestFieldDay_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}

	cases := []struct {
		text   string
		offset float64
	}{
		{"CQ FD K1ABC FN42", 1500},     // their CQ
		{"K1ABC 7Q5MLV 1D DX", 1200},   // our exchange (Tx1)
		{"7Q5MLV K1ABC R 2A EMA", 800}, // their R + exchange
		{"K1ABC 7Q5MLV RR73", 2400},    // our roger (Tx2)
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			// Canonical text the decoder will report (encoder may normalise case).
			enc, err := goft8.EncodeStandardMessage(c.text)
			if err != nil {
				t.Fatalf("EncodeStandardMessage(%q): %v", c.text, err)
			}
			slot, err := EncodeToSlot(c.text, c.offset, 0.5)
			if err != nil {
				t.Fatalf("EncodeToSlot(%q): %v", c.text, err)
			}
			msgs := DecodeSlot(slot, true, logging.Noop())
			found := false
			for _, m := range msgs {
				if strings.EqualFold(strings.TrimSpace(m.Text), enc.Text) {
					found = true
					break
				}
			}
			if !found {
				got := make([]string, len(msgs))
				for i, m := range msgs {
					got[i] = m.Text
				}
				t.Fatalf("FD message did not round-trip; decoded %v, want %q", got, enc.Text)
			}
		})
	}
}
