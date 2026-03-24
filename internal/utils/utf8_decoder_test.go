package utils

import "testing"

func TestDecodeStringToUTF8(t *testing.T) {
	in := "hello"
	out, err := DecodeStringToUTF8(in)
	if err != nil || out != in {
		t.Fatalf("DecodeStringToUTF8 = %q, %v", out, err)
	}
}

func TestDecodeStringToUTF8_Empty(t *testing.T) {
	out, err := DecodeStringToUTF8("")
	if err != nil {
		t.Fatalf("unexpected error for empty input: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestDecodeStringToUTF8_Unicode(t *testing.T) {
	cases := []string{
		"héllo",
		"日本語",
		"café",
		"Ünïcödé",
	}
	for _, in := range cases {
		out, err := DecodeStringToUTF8(in)
		if err != nil {
			t.Fatalf("DecodeStringToUTF8(%q) error: %v", in, err)
		}
		if out != in {
			t.Fatalf("DecodeStringToUTF8(%q) = %q; want same", in, out)
		}
	}
}
