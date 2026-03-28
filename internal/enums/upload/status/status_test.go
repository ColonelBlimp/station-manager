package status

import "testing"

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		Pending:    "pending",
		InProgress: "in_progress",
		Uploaded:   "uploaded",
		Failed:     "failed",
	}

	for in, want := range cases {
		if got := in.String(); got != want {
			t.Fatalf("%q.String() = %q, want %q", in, got, want)
		}
	}
}
