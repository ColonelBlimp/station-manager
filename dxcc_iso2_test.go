package utils

import "testing"

func TestDXCCFromISO2_KnownMappings(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"us", "291"},
		{"US", "291"},
		{"DE", "230"},
		{"JP", "339"},
		{"MW", "468"}, // Malawi
	}
	for _, c := range cases {
		got, ok := DXCCFromISO2(c.in)
		if !ok {
			t.Fatalf("expected ok for %q", c.in)
		}
		if got != c.want {
			t.Fatalf("for %q got %q want %q", c.in, got, c.want)
		}
	}
}

func TestDXCCFromISO2_UnknownOrInvalid(t *testing.T) {
	cases := []string{"", "X", "XXX", "GB", "ZZ"}
	for _, in := range cases {
		if got, ok := DXCCFromISO2(in); ok || got != "" {
			t.Fatalf("expected no result for %q got %q ok=%v", in, got, ok)
		}
	}
}
