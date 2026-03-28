package events

import "testing"

func TestEventNameString(t *testing.T) {
	if got := Status.String(); got != "STATUS" {
		t.Fatalf("Status.String() = %q, want %q", got, "STATUS")
	}
}

func TestAllEvents(t *testing.T) {
	if len(AllEvents) != 1 {
		t.Fatalf("len(AllEvents) = %d, want %d", len(AllEvents), 1)
	}

	got := AllEvents[0]
	if got.Value != Status {
		t.Fatalf("AllEvents[0].Value = %q, want %q", got.Value, Status)
	}
	if got.TSName != "STATUS" {
		t.Fatalf("AllEvents[0].TSName = %q, want %q", got.TSName, "STATUS")
	}
}
