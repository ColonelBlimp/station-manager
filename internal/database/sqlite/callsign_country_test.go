package sqlite

import (
	"context"
	stderr "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// What the country provider answered for ONE callsign (W-0031). It is kept apart
// from the country table on purpose: that table is matched by prefix, and a row
// there answers for every call that begins with it. An answer remembered here
// answers for exactly the callsign it was given for, and for no other.

func TestCallsignCountry_MissIsNotFound(t *testing.T) {
	svc := testService(t)

	_, err := svc.FetchCallsignCountryWithContext(context.Background(), "VK9XY")
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCallsignCountry_RemembersTheWholeAnswerForThatCallsign(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	want := types.Country{
		Name: "Christmas Island", Prefix: "VK9X", Ccode: "CX", Continent: "OC",
		CQZone: "29", ITUZone: "54", DXCCPrefix: "VK9X", TimeOffset: "7h 0m",
	}
	before := time.Now().UTC().Add(-time.Second)
	if err := svc.UpsertCallsignCountryWithContext(ctx, "VK9XY", want); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := svc.FetchCallsignCountryWithContext(ctx, "VK9XY")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.LastRefreshedAt.Before(before) || got.LastRefreshedAt.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("LastRefreshedAt = %v, want about now", got.LastRefreshedAt)
	}
	got.LastRefreshedAt = time.Time{}
	got.ID = 0
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// The point of the table: exact, never a prefix. A whole-callsign answer must not
// be applied to a longer call that begins with it, nor to a shorter one.
func TestCallsignCountry_IsExactNeverAPrefix(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.UpsertCallsignCountryWithContext(ctx, "KH6M", types.Country{Name: "United States", Prefix: "KH6M"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for _, other := range []string{"KH6MB", "KH6", "KH6MM", "AKH6M"} {
		if _, err := svc.FetchCallsignCountryWithContext(ctx, other); !stderr.Is(err, errors.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound (the answer is for KH6M alone)", other, err)
		}
	}
}

func TestCallsignCountry_IsCaseAndSpaceInsensitiveOnTheCallsign(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.UpsertCallsignCountryWithContext(ctx, " vk9xy ", types.Country{Name: "Christmas Island", Prefix: "VK9X"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := svc.FetchCallsignCountryWithContext(ctx, "Vk9Xy")
	if err != nil || got.Name != "Christmas Island" {
		t.Fatalf("got %q, %v", got.Name, err)
	}
}

// A later answer replaces the earlier one whole, and moves the freshness stamp.
func TestCallsignCountry_ALaterAnswerReplacesTheEarlier(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.UpsertCallsignCountryWithContext(ctx, "VK9XY", types.Country{
		Name: "Australia", Prefix: "VK", CQZone: "30", Ccode: "AU",
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first, _ := svc.FetchCallsignCountryWithContext(ctx, "VK9XY")
	time.Sleep(5 * time.Millisecond)
	if err := svc.UpsertCallsignCountryWithContext(ctx, "VK9XY", types.Country{
		Name: "Christmas Island", Prefix: "VK9X", CQZone: "29",
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, err := svc.FetchCallsignCountryWithContext(ctx, "VK9XY")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Name != "Christmas Island" || got.Prefix != "VK9X" || got.CQZone != "29" || got.Ccode != "" {
		t.Errorf("got %+v, want the second answer whole (an emptied field cleared)", got)
	}
	if !got.LastRefreshedAt.After(first.LastRefreshedAt) {
		t.Errorf("LastRefreshedAt did not move: %v then %v", first.LastRefreshedAt, got.LastRefreshedAt)
	}
}

func TestCallsignCountry_RefusesWhatItCannotKey(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.UpsertCallsignCountryWithContext(ctx, "  ", types.Country{Name: "X", Prefix: "X1"}); err == nil {
		t.Error("an empty callsign was accepted")
	}
	if err := svc.UpsertCallsignCountryWithContext(ctx, "VK9XY", types.Country{Name: "", Prefix: "VK9X"}); err == nil {
		t.Error("an answer with no country name was accepted")
	}
	if _, err := svc.FetchCallsignCountryWithContext(ctx, ""); err == nil || stderr.Is(err, errors.ErrNotFound) {
		t.Errorf("an empty callsign must be refused, not reported as not found: %v", err)
	}
}

// It leaves the prefix table alone: nothing remembered for a callsign becomes a
// prefix row, which is what would let it answer for other calls.
func TestCallsignCountry_WritesNoPrefixRow(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.UpsertCallsignCountryWithContext(ctx, "KH6M", types.Country{Name: "United States", Prefix: "KH6M"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := svc.FetchCountryByCallsignWithContext(ctx, "KH6MB"); !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("the prefix table answered for KH6MB: %v", err)
	}
}

// Forgetting is exact too, and forgetting what was never remembered is not an
// error: the caller is acting on the provider's verdict, not on the row.
func TestCallsignCountry_ForgetIsExactAndIdempotent(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	for _, call := range []string{"VK9XY", "VK9XYZ"} {
		if err := svc.UpsertCallsignCountryWithContext(ctx, call, types.Country{Name: "Christmas Island", Prefix: "VK9X"}); err != nil {
			t.Fatalf("upsert %s: %v", call, err)
		}
	}

	if err := svc.DeleteCallsignCountryWithContext(ctx, " vk9xy "); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.FetchCallsignCountryWithContext(ctx, "VK9XY"); !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("VK9XY still remembered: %v", err)
	}
	if got, err := svc.FetchCallsignCountryWithContext(ctx, "VK9XYZ"); err != nil || got.Name != "Christmas Island" {
		t.Fatalf("another callsign was forgotten with it: %q, %v", got.Name, err)
	}
	if err := svc.DeleteCallsignCountryWithContext(ctx, "VK9XY"); err != nil {
		t.Fatalf("forgetting again: %v", err)
	}
	if err := svc.DeleteCallsignCountryWithContext(ctx, "  "); err == nil {
		t.Fatal("an empty callsign was accepted")
	}
}
