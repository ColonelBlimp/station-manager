package utils

import "testing"

func TestDecodeStringToUTF8(t *testing.T) {
	in := "hello"
	out, err := DecodeStringToUTF8(in)
	if err != nil || out != in {
		t.Fatalf("DecodeStringToUTF8 = %q, %v", out, err)
	}
}
