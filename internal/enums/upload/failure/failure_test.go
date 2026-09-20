package failure

import "testing"

func TestParse_KnownAndUnknown(t *testing.T) {
	got, err := Parse("auth")
	if err != nil || got != Auth {
		t.Fatalf("Parse(auth) = %q, %v; want auth, nil", got, err)
	}
	for _, bad := range []string{"", "AUTH", "rejected"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) = nil error, want unknown-class error", bad)
		}
	}
}
