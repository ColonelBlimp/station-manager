package evidencewire

/*
   Digest canon v1 (§5.2 + operator ruling 2026-08-10: digest identity is
   (tenant, kind, uuid) over VERSIONED canonical immutable content). Rules
   pinned here, each against its nearest confusable behaviour:

   D1  Key order carries nothing: the same object with reordered keys
       digests identically — else a restored backup re-marshalling its rows
       would self-reject as a digest conflict.
   D2  Insignificant whitespace carries nothing (same reason).
   D3  Number LEXEMES are preserved, not re-formatted: "1.5" and "1.50"
       digest DIFFERENTLY by design. Determinism comes from one producer
       emitting stable lexemes, and preservation keeps the digest a
       function of the content rather than of any parser's float
       formatting — the confusable design (parse to float64, re-format)
       would silently change digests across parser versions.
   D4  Array order IS content: reordered arrays digest differently.
   D5  Nesting canonicalizes recursively.
   D6  Invalid JSON is an error, never a digest — a digest of garbage
       would let a malformed row pass identity checks.
*/

import "testing"

func digestOf(t *testing.T, s string) string {
	t.Helper()
	d, err := DigestV1Hex([]byte(s))
	if err != nil {
		t.Fatalf("DigestV1Hex(%q): %v", s, err)
	}
	if len(d) != 64 {
		t.Fatalf("DigestV1Hex(%q) = %q, want 64 hex chars (SHA-256)", s, d)
	}
	return d
}

func TestDigestV1_KeyOrderAndWhitespaceCarryNothing(t *testing.T) {
	a := digestOf(t, `{"snr":-8,"text":"CQ K1ABC","freq_hz":1204.5}`)
	b := digestOf(t, ` { "freq_hz" : 1204.5 , "text" : "CQ K1ABC" , "snr" : -8 } `)
	if a != b {
		t.Fatalf("D1/D2: reordered/whitespaced object digests differ: %s vs %s", a, b)
	}
}

func TestDigestV1_NumberLexemesArePreserved(t *testing.T) {
	a := digestOf(t, `{"v":1.5}`)
	b := digestOf(t, `{"v":1.50}`)
	if a == b {
		t.Fatal("D3: 1.5 and 1.50 must digest differently — lexemes are content, re-formatting is the guarded-against design")
	}
}

func TestDigestV1_ArrayOrderIsContent(t *testing.T) {
	a := digestOf(t, `{"bands":["40m","80m"]}`)
	b := digestOf(t, `{"bands":["80m","40m"]}`)
	if a == b {
		t.Fatal("D4: reordered arrays must digest differently")
	}
}

func TestDigestV1_NestedObjectsCanonicalizeRecursively(t *testing.T) {
	a := digestOf(t, `{"outer":{"b":2,"a":1},"list":[{"y":0,"x":9}]}`)
	b := digestOf(t, `{"list":[{"x":9,"y":0}],"outer":{"a":1,"b":2}}`)
	if a != b {
		t.Fatalf("D5: nested reordering must not change the digest: %s vs %s", a, b)
	}
}

func TestDigestV1_InvalidJSONIsAnError(t *testing.T) {
	if _, err := DigestV1Hex([]byte(`{"unterminated":`)); err == nil {
		t.Fatal("D6: invalid JSON must error, never digest")
	}
	if _, err := DigestV1Hex([]byte(`{"a":1} trailing`)); err == nil {
		t.Fatal("D6: trailing garbage must error, never digest")
	}
}
