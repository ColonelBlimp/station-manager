package ft8

import (
	"strings"
	"testing"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// roundTrips reports whether text encodes, modulates, and decodes back to the
// encoder's canonical form with the SHIPPED decoder — the offline RF-safety check
// (never key a frame we can't read back). Zero RF.
func roundTrips(t *testing.T, text string, offset float64) (canonical string, ok bool) {
	t.Helper()
	enc, err := goft8.EncodeStandardMessage(text)
	if err != nil {
		t.Fatalf("EncodeStandardMessage(%q): %v", text, err)
	}
	slot, err := EncodeToSlot(text, offset, 0.5)
	if err != nil {
		t.Fatalf("EncodeToSlot(%q): %v", text, err)
	}
	for _, m := range DecodeSlot(slot, true, logging.Noop()) {
		if strings.EqualFold(strings.TrimSpace(m.Text), enc.Text) {
			return enc.Text, true
		}
	}
	return enc.Text, false
}

// TestCompoundCQ_Decodes is the headline win of the go-ft8 v0.5.0 bump: a type-4
// compound/nonstandard-call CQ (e.g. a DXpedition "CQ PJ4/NA2AA") now DECODES with
// the shipped decoder. Before v0.5.0 the packer/unpacker had no type-4 path, so such
// a CQ was simply missed — it never reached Band Activity. This is primarily an RX
// gain (the operator can now SEE compound-call CQs); the TX side is limited by the
// protocol (see TestPrefixCompound_EncoderBoundary). Heavy decode → -short-gated.
func TestCompoundCQ_Decodes(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	for _, txt := range []string{"CQ PJ4/NA2AA", "PJ4/NA2AA 7Q5MLV RR73"} {
		t.Run(txt, func(t *testing.T) {
			if canon, ok := roundTrips(t, txt, 1500); !ok {
				t.Fatalf("type-4 message did not round-trip; want %q decoded", canon)
			}
		})
	}
}

// TestPortableP_RoundTrip is a REGRESSION lock, not a new-capability proof: the
// standard `/P` suffix has been encodable since the go-ft8 v0.3.5 bump (2026-06-18)
// and packs as a STANDARD message, so it carries grid + report and walks SM's
// answer-a-CQ ladder unchanged. These are exactly the strings Exchange.TxMessage
// emits (them = NA2AA/P, us = 7Q5MLV) plus the inbound decodes Advance consumes —
// pinned here so a future go-ft8 bump can't silently break the portable ladder.
// Heavy decode → -short-gated.
func TestPortableP_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	cases := []struct {
		text   string
		offset float64
	}{
		{"CQ NA2AA/P FN42", 1500},     // their CQ
		{"NA2AA/P 7Q5MLV KH53", 1200}, // our opening call (Tx1)
		{"7Q5MLV NA2AA/P -13", 800},   // their report
		{"NA2AA/P 7Q5MLV R-05", 2100}, // our rogered report (Tx2)
		{"7Q5MLV NA2AA/P RR73", 1700}, // their roger
		{"NA2AA/P 7Q5MLV 73", 2400},   // our 73 (Tx3; QSO logs)
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			if canon, ok := roundTrips(t, c.text, c.offset); !ok {
				t.Fatalf("portable message did not round-trip; want %q decoded", canon)
			}
		})
	}
}

// TestPrefixCompound_EncoderBoundary pins the protocol limits that keep SM's
// answer-a-CQ guard (StartQso → ErrTxBadMessage) correct for TRUE prefix-compound
// calls (PJ4/NA2AA) — and documents why the go-ft8 v0.5.0 bump does NOT by itself
// let SM run a full QSO with one. Encode-only (no decode), so it runs even under
// -short. Three boundaries, each with a consequence for SM:
//
//   - type-4 CQ / RR73 / 73 ENCODE (with the partner call hashed to <...>). SM can
//     transmit these, but that alone is not a full exchange.
//   - grid + report have NO type-4 form → REJECTED. SM's ladder opens with
//     "<them> <us> <grid>", which is therefore unencodable, so a full standard-ladder
//     QSO with a prefix-compound partner is impossible. SM already fails soft here.
//   - `/R` (RTTY-Roundup suffix) ENCODES but go-ft8's decoder does not yet unpack it
//     ("RTTY Roundup … not yet unpacked"), so it fails the round-trip gate above and
//     SM must not transmit it. Asserted encode-only + noted; if a future go-ft8 adds
//     `/R` decode, promote it into a round-trip test.
//
// The full reduced type-4 QSO flow (hashed CQ→RR73→73) is a backlog feature, now
// unblocked at the library level. If a future go-ft8 makes the grid/report forms
// encodable, the unencodable cases below flip and this test tells us to lift the
// guard and build that flow.
func TestPrefixCompound_EncoderBoundary(t *testing.T) {
	encodable := []string{
		"CQ PJ4/NA2AA",          // bare CQ (no grid)
		"PJ4/NA2AA 7Q5MLV RR73", // roger (partner hashed)
		"PJ4/NA2AA 7Q5MLV 73",   // 73 (partner hashed)
		"CQ NA2AA/R FN42",       // /R encodes (but is not round-trippable — see above)
	}
	for _, txt := range encodable {
		if _, err := goft8.EncodeStandardMessage(txt); err != nil {
			t.Errorf("expected %q to encode, got: %v", txt, err)
		}
	}

	// The grid + report rungs of the standard ladder have no type-4 form.
	unencodable := []string{
		"CQ PJ4/NA2AA FK52",     // CQ with grid
		"PJ4/NA2AA 7Q5MLV KH53", // opening call with grid
		"PJ4/NA2AA 7Q5MLV -13",  // report
		"PJ4/NA2AA 7Q5MLV R-13", // rogered report
	}
	for _, txt := range unencodable {
		if _, err := goft8.EncodeStandardMessage(txt); err == nil {
			t.Errorf("expected %q to be REJECTED (no type-4 grid/report form); it encoded", txt)
		}
	}
}
