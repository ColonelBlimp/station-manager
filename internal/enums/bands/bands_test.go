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
		// VHF/UHF (review 2026-06-19 M2) — accepted to match the frequency→band
		// mappers in internal/utils and the logging SPA.
		Band4.String(),
		Band2.String(),
		Band125.String(),
		Band70cm.String(),
		Band33cm.String(),
		Band23cm.String(),
	}

	for _, in := range valid {
		if !IsValidBand(in) {
			t.Fatalf("expected %q to be a valid band", in)
		}
	}

	// "2m" is now valid; case/whitespace variants and unknowns stay invalid.
	invalid := []string{"", "2M", " 20m ", "unknown", "13cm"}
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
