package buildinfo

// ST-7 — BuildScope defaults to "public" (a keyless build) and IsPrivateBuild reports the
// keyed dogfood scope. The build system stamps "private" via -ldflags on the private path;
// the end-to-end binary behaviour is covered by scripts/test-build-boundary.sh.

import "testing"

func TestBuildScope_DefaultsPublic(t *testing.T) {
	if BuildScope != "public" {
		t.Errorf("default BuildScope = %q, want \"public\" (a keyless build must not claim private)", BuildScope)
	}
	if IsPrivateBuild() {
		t.Error("IsPrivateBuild() is true for the default (public) build")
	}
}

func TestIsPrivateBuild(t *testing.T) {
	orig := BuildScope
	defer func() { BuildScope = orig }()

	BuildScope = "private"
	if !IsPrivateBuild() {
		t.Error("IsPrivateBuild() = false for BuildScope=private")
	}
	BuildScope = "public"
	if IsPrivateBuild() {
		t.Error("IsPrivateBuild() = true for BuildScope=public")
	}
}
