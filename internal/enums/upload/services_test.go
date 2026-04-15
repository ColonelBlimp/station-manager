package upload

import (
	"testing"
)

func TestOnlineServiceValid(t *testing.T) {
	valid := []OnlineService{
		OnlineServiceSM,
		OnlineServiceQRZ,
		OnlineServiceLoTW,
		OnlineServiceEQSL,
		OnlineServiceClub,
	}

	for _, s := range valid {
		if !s.Valid() {
			t.Fatalf("expected %q to be valid", s)
		}
	}

	invalid := []OnlineService{"", "unknown", "QRZ"}
	for _, s := range invalid {
		if s.Valid() {
			t.Fatalf("expected %q to be invalid", s)
		}
	}
}

func TestOnlineServiceString(t *testing.T) {
	if got := OnlineServiceSM.String(); got != "sm" {
		t.Fatalf("OnlineServiceSM.String() = %q, want %q", got, "sm")
	}

	if got := OnlineServiceQRZ.String(); got != "qrzforwardingservice" {
		t.Fatalf("OnlineServiceQRZ.String() = %q, want %q", got, "qrzforwardingservice")
	}
}
