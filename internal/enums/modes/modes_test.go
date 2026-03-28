package modes

import "testing"

func TestIsValidMode(t *testing.T) {
	valid := []string{"AM", "cw", "  fm  ", "RTTY", "ssb", "digitalvoice", "MFSK", "PSK", "hell", "packet"}
	for _, in := range valid {
		if !IsValidMode(in) {
			t.Fatalf("expected %q to be valid", in)
		}
	}

	invalid := []string{"", "foo", "lsb", "usb"}
	for _, in := range invalid {
		if IsValidMode(in) {
			t.Fatalf("expected %q to be invalid", in)
		}
	}
}

func TestIsValidSubMode(t *testing.T) {
	valid := []string{"FST4", "ft4", " dmr ", "USB", "lsb", "aprs"}
	for _, in := range valid {
		if !IsValidSubMode(in) {
			t.Fatalf("expected %q to be valid", in)
		}
	}

	invalid := []string{"", "AM", "CW", "foo"}
	for _, in := range invalid {
		if IsValidSubMode(in) {
			t.Fatalf("expected %q to be invalid", in)
		}
	}
}

func TestGetModeBySubmode(t *testing.T) {
	cases := []struct {
		in   string
		want Mode
	}{
		{in: "ft4", want: MFSK},
		{in: "QPSK31", want: PSK},
		{in: "  dstar ", want: DIGITALVOICE},
		{in: "HFSK", want: HELL},
		{in: "PKT", want: PACKET},
		{in: "USB", want: SSB},
		{in: "lsb", want: SSB},
	}

	for _, tc := range cases {
		got, ok := GetModeBySubmode(tc.in)
		if !ok {
			t.Fatalf("expected mapping for %q", tc.in)
		}
		if got != tc.want {
			t.Fatalf("GetModeBySubmode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if got, ok := GetModeBySubmode("unknown"); ok || got != "" {
		t.Fatalf("GetModeBySubmode(%q) = (%q, %v), want (%q, %v)", "unknown", got, ok, "", false)
	}
}

func TestStringMethods(t *testing.T) {
	if got := AM.String(); got != "AM" {
		t.Fatalf("AM.String() = %q, want %q", got, "AM")
	}
	if got := USB.String(); got != "USB" {
		t.Fatalf("USB.String() = %q, want %q", got, "USB")
	}
}
