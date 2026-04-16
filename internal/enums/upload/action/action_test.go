package action

import "testing"

func TestActionString(t *testing.T) {
	cases := map[Action]string{
		Insert: "insert",
		Update: "update",
		Delete: "delete",
	}

	for in, want := range cases {
		if got := in.String(); got != want {
			t.Fatalf("%q.String() = %q, want %q", in, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Action
	}{
		{in: "insert", want: Insert},
		{in: "update", want: Update},
		{in: "delete", want: Delete},
	}

	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParse_UnknownReturnsError(t *testing.T) {
	got, err := Parse("unknown")
	if err == nil {
		t.Fatalf("Parse(%q) = (%q, nil), want error", "unknown", got)
	}
	if got != "" {
		t.Fatalf("Parse(%q) = (%q, err), want empty Action on error", "unknown", got)
	}
}
