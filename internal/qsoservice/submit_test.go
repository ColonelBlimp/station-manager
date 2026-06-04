package qsoservice

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// TestResolveSubmitUUID pins the H1 UUID policy (ADR 0016): the public submit
// path always mints a fresh UUID (never adopts a wire-supplied one — the
// identity-spoofing guard), while the import/restore path preserves a valid
// supplied UUIDv7 and mints when the supplied value is absent or malformed.
func TestResolveSubmitUUID(t *testing.T) {
	valid := utils.NewUUIDv7()

	// Public path (preserve=false): always mint, even given a valid UUID.
	if got := resolveSubmitUUID(valid, false); got == valid {
		t.Error("public path must mint a fresh UUID, not adopt the supplied one")
	} else if !utils.IsValidUUIDv7(got) {
		t.Errorf("public path minted an invalid UUIDv7: %q", got)
	}

	// Import path (preserve=true): preserve a valid supplied UUIDv7.
	if got := resolveSubmitUUID(valid, true); got != valid {
		t.Errorf("import path must preserve a valid UUIDv7; got %q, want %q", got, valid)
	}

	// Import path with a missing/malformed UUID: fall back to a fresh mint.
	for _, bad := range []string{"", "not-a-uuid", "12345"} {
		got := resolveSubmitUUID(bad, true)
		if got == bad {
			t.Errorf("import path must not adopt invalid UUID %q", bad)
		}
		if !utils.IsValidUUIDv7(got) {
			t.Errorf("import path with invalid input %q must mint a valid UUIDv7; got %q", bad, got)
		}
	}
}
