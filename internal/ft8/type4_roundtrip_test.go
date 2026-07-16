package ft8

import "testing"

// TestType4_RoundTrip is the RF-safety gate for the reduced type-4 (nonstandard/compound
// call) ladder — ADR 0048. Every message either station transmits in the answer-a-CQ and
// work-a-caller flows must encode (go-ft8 packs type-4), modulate to audio, and decode
// back to the same canonical text with the SHIPPED decoder — proving we never key a frame
// the FT8 world can't read. Zero RF. Heavy (full decode), so gated under -short like the
// other round-trip proofs.
//
// Union of both directions, with us = 7Q5MLV working the nonstandard PJ4/NA2AA:
//
//	answer-a-CQ:  CQ PJ4/NA2AA          (their CQ — we decode)
//	              PJ4/NA2AA 7Q5MLV      (our opening — "PJ4/NA2AA <...>")
//	              7Q5MLV PJ4/NA2AA RR73 (their roger — "<...> PJ4/NA2AA RR73")
//	              PJ4/NA2AA 7Q5MLV 73   (our 73 — "PJ4/NA2AA <...> 73"; QSO logs)
//	work-a-caller:7Q5MLV PJ4/NA2AA      (their call to us — "<...> PJ4/NA2AA")
//	              PJ4/NA2AA 7Q5MLV RR73 (our roger — "PJ4/NA2AA <...> RR73"; QSO logs)
//	              7Q5MLV PJ4/NA2AA 73   (their 73 — "<...> PJ4/NA2AA 73")
func TestType4_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	cases := []struct {
		text   string
		offset float64
	}{
		{"CQ PJ4/NA2AA", 1500},
		{"PJ4/NA2AA 7Q5MLV", 1200},
		{"7Q5MLV PJ4/NA2AA RR73", 800},
		{"PJ4/NA2AA 7Q5MLV 73", 2400},
		{"7Q5MLV PJ4/NA2AA", 1700},
		{"PJ4/NA2AA 7Q5MLV RR73", 1000},
		{"7Q5MLV PJ4/NA2AA 73", 2100},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			if canon, ok := roundTrips(t, c.text, c.offset); !ok {
				t.Fatalf("type-4 message did not round-trip; want %q decoded", canon)
			}
		})
	}
}
