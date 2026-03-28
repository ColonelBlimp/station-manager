package bands

import "testing"

func TestIsValidBand(t *testing.T) {
	valid := []string{
		Band160.String(),
		Band80.String(),
		Band60.String(),
		Band40.String(),
		Band30.String(),
		Band20.String(),
		Band17.String(),
		Band15.String(),
		Band12.String(),
		Band10.String(),
		Band6.String(),
	}

	for _, in := range valid {
		if !IsValidBand(in) {
			t.Fatalf("expected %q to be a valid band", in)
		}
	}

	invalid := []string{"", "2m", "20M", " 20m ", "unknown"}
	for _, in := range invalid {
		if IsValidBand(in) {
			t.Fatalf("expected %q to be invalid", in)
		}
	}
}

func TestBandString(t *testing.T) {
	if got := Band20.String(); got != "20m" {
		t.Fatalf("Band20.String() = %q, want %q", got, "20m")
	}
}
