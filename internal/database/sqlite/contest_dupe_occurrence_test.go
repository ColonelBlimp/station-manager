package sqlite

import (
	"context"
	"testing"
)

// W-0023 weekend slice (dogfood 2026-10-31: 7Q7EB logged twice on 20m with no
// warning). A contest duplicate is the same call on the same band within the
// same contest occurrence. A contact before the contest, or in another
// occurrence, is not one.
func TestIsContestOccurrenceDuplicate(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2) // seeds K1AAA 20m in each logbook, no contest fields
	ctx := context.Background()
	save := func(logbookID int64, call, band, contest, occurrence string) {
		t.Helper()
		q := validTestQso(logbookID, call, band, "SSB", "20261031", "1300")
		q.QsoDetails.ContestId, q.AppSmContestStart = contest, occurrence
		if _, err := svc.InsertQso(q); err != nil {
			t.Fatalf("insert %s: %v", call, err)
		}
	}
	save(1, "7Q7EB", "20m", "UKEIDXSSB", "20261031")
	save(1, "G4OLD", "20m", "UKEIDXSSB", "20251025") // last year's edition
	save(2, "G4LB2", "20m", "UKEIDXSSB", "20261031") // another logbook

	for _, c := range []struct {
		name       string
		call, band string
		want       bool
	}{
		{"same call, band and occurrence", "7Q7EB", "20m", true},
		{"same call, another band", "7Q7EB", "40m", false},
		{"worked before the contest (no contest fields)", "K1AAA", "20m", false},
		{"worked in another occurrence", "G4OLD", "20m", false},
		{"worked in another logbook", "G4LB2", "20m", false},
		{"never worked", "EI2XYZ", "20m", false},
	} {
		got, err := svc.IsContestOccurrenceDuplicateWithContext(ctx, 1, c.call, c.band, "UKEIDXSSB", "20261031")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: duplicate = %v, want %v", c.name, got, c.want)
		}
	}
	if _, err := svc.IsContestOccurrenceDuplicateWithContext(ctx, 1, "7Q7EB", "20m", "", "20261031"); err == nil {
		t.Error("blank contest: want an error")
	}
	if _, err := svc.IsContestOccurrenceDuplicateWithContext(ctx, 1, "7Q7EB", "20m", "UKEIDXSSB", ""); err == nil {
		t.Error("blank occurrence: want an error")
	}
}
