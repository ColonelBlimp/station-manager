package cat

import (
	stderr "errors"
	"reflect"
	"testing"
)

// TestDecodeEquivalentToReference runs every decode fixture through both
// the real cat.Decode and the frozen referenceDecode/referenceLookup
// pair, and asserts their outputs match. This is the direct
// equivalence check: if either path drifts from v1's behaviour while
// the other doesn't, this test catches it even if the fixture table is
// also (incorrectly) updated to match.
//
// The reference must stay frozen — do not "fix bugs" in
// referenceDecode/referenceLookup, fix them in cat.Decode and update
// the fixtures. The reference is the thing we're carving out FROM.
func TestDecodeEquivalentToReference(t *testing.T) {
	for _, tc := range decodeCases {
		t.Run(tc.name, func(t *testing.T) {
			def, ok := Lookup(tc.rigID)
			if !ok {
				t.Fatalf("Lookup(%q) not found", tc.rigID)
			}

			refState, refTail, refMatched := referenceLookup([]byte(tc.input), def.States)
			codecResult, codecErr := Decode(def, []byte(tc.input))
			codecNoMatch := stderr.Is(codecErr, ErrNoMatch)

			if refMatched == codecNoMatch {
				t.Fatalf("match/no-match divergence: reference matched=%v, codec matched=%v (err=%v)",
					refMatched, !codecNoMatch, codecErr)
			}

			if !refMatched {
				// Both agreed: no match. Nothing further to compare.
				return
			}

			if codecErr != nil {
				t.Fatalf("codec returned error where reference matched: %v", codecErr)
			}

			refResult := referenceDecode(refState, refTail)
			if !reflect.DeepEqual(codecResult, refResult) {
				t.Errorf("codec output differs from reference\n codec:     %v\n reference: %v",
					codecResult, refResult)
			}
		})
	}
}

// TestEncodeEquivalentToReference does the same equivalence check for
// the send path: every encode fixture must produce identical output
// from cat.Encode and referenceEncode.
func TestEncodeEquivalentToReference(t *testing.T) {
	for _, tc := range encodeCases {
		t.Run(tc.name, func(t *testing.T) {
			def, ok := Lookup(tc.rigID)
			if !ok {
				t.Fatalf("Lookup(%q) not found", tc.rigID)
			}

			refResult, refErr := referenceEncode(def, tc.cmdName, tc.args...)
			codecBytes, codecErr := Encode(def, tc.cmdName, tc.args...)

			refSucceeded := refErr == nil
			codecSucceeded := codecErr == nil

			if refSucceeded != codecSucceeded {
				t.Fatalf("success/error divergence: reference err=%v, codec err=%v", refErr, codecErr)
			}

			if !refSucceeded {
				// Both agreed: error. The codec must wrap ErrUnknownCommand
				// for fixtures with wantErr=true; the reference returns a
				// plain fmt.Errorf — we don't assert on reference's error
				// shape, only that both failed.
				return
			}

			if string(codecBytes) != refResult {
				t.Errorf("codec output differs from reference\n codec:     %q\n reference: %q",
					string(codecBytes), refResult)
			}
		})
	}
}
