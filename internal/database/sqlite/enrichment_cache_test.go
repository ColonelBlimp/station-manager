package sqlite

import (
	"context"
	stderr "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Tests for the enrichment-cache helpers added under ADR 0017:
//   - FetchCountryByPrefixWithContext (exact-prefix read)
//   - UpsertCountryWithContext (full-row replace, hamnut-only)
//   - UpsertContactedStationWithContext (non-empty-wins-per-field merge)
//
// The longest-prefix-match read path
// (FetchCountryByCallsignWithContext) is covered by the existing
// service_test.go suite and is not duplicated here.

func TestFetchCountryByPrefix_ExactMatchHitsAndMisses(t *testing.T) {
	svc := testService(t)

	if _, err := svc.InsertCountry(types.Country{
		Name:   "Germany",
		Prefix: "DL",
		Ccode:  "DEU",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := svc.FetchCountryByPrefix("DL")
	if err != nil {
		t.Fatalf("fetch hit: %v", err)
	}
	if got.Name != "Germany" {
		t.Fatalf("Name = %q, want Germany", got.Name)
	}

	// Exact match — "DL1ABC" is a callsign, not a prefix; this lookup
	// must miss even though the longest-prefix-match read path would
	// find DL.
	if _, err := svc.FetchCountryByPrefix("DL1ABC"); !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-prefix string, got %v", err)
	}

	if _, err := svc.FetchCountryByPrefix(""); err == nil {
		t.Fatal("expected error for empty prefix")
	}
}

func TestUpsertCountry_InsertsThenReplaces(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	// First write — cold insert.
	in := types.Country{
		Name:      "Germany",
		Prefix:    "DL",
		Ccode:     "DEU",
		Continent: "EU",
		CQZone:    "14",
		ITUZone:   "28",
	}
	before := time.Now().Add(-time.Second)
	if err := svc.UpsertCountryWithContext(ctx, in); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	got, err := svc.FetchCountryByPrefix("DL")
	if err != nil {
		t.Fatalf("fetch after insert: %v", err)
	}
	if got.Name != "Germany" || got.Ccode != "DEU" || got.Continent != "EU" {
		t.Fatalf("after insert: got %+v, want Germany/DEU/EU", got)
	}
	if got.LastRefreshedAt.IsZero() || got.LastRefreshedAt.Before(before) {
		t.Fatalf("LastRefreshedAt = %v, want non-zero set near now", got.LastRefreshedAt)
	}
	firstRefresh := got.LastRefreshedAt

	// Second write — full-row replace. Hamnut-style refresh of all
	// fields. last_refreshed_at must advance.
	time.Sleep(1100 * time.Millisecond) // SQLite datetime() resolution is per-second
	updated := types.Country{
		Name:      "Federal Republic of Germany",
		Prefix:    "DL",
		Ccode:     "DEU",
		Continent: "EU",
		CQZone:    "14",
		ITUZone:   "28",
	}
	if err := svc.UpsertCountryWithContext(ctx, updated); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got2, err := svc.FetchCountryByPrefix("DL")
	if err != nil {
		t.Fatalf("fetch after replace: %v", err)
	}
	if got2.Name != "Federal Republic of Germany" {
		t.Fatalf("after replace: Name = %q, want updated", got2.Name)
	}
	if !got2.LastRefreshedAt.After(firstRefresh) {
		t.Fatalf("LastRefreshedAt did not advance: first=%v second=%v", firstRefresh, got2.LastRefreshedAt)
	}
	// PK preserved across the replace (same row, not a delete+insert).
	if got2.ID != got.ID {
		t.Fatalf("ID changed across upsert: %d → %d", got.ID, got2.ID)
	}
}

func TestUpsertCountry_RejectsEmptyPrefix(t *testing.T) {
	svc := testService(t)
	if err := svc.UpsertCountry(types.Country{Name: "X", Prefix: ""}); err == nil {
		t.Fatal("expected error for empty prefix")
	}
}

// TestUpsertCountry_RejectsLikeWildcardPrefix covers review m5 — the
// country-prefix is interpolated directly into a LIKE pattern by
// FetchCountryByCallsignWithContext, so a row with '%', '_', or '\'
// in its prefix would silently over-match every callsign on the
// longest-prefix-match read path. The upsert path is the single
// chokepoint that prevents such rows from ever landing.
func TestUpsertCountry_RejectsLikeWildcardPrefix(t *testing.T) {
	svc := testService(t)

	bad := []string{
		"M%",   // SQL LIKE wildcard
		"M_",   // SQL LIKE single-char wildcard
		"M\\A", // LIKE escape character
		"%",    // wildcard alone
		"_",    // single-char wildcard alone
	}
	for _, p := range bad {
		t.Run(p, func(t *testing.T) {
			err := svc.UpsertCountry(types.Country{Name: "TestLand", Prefix: p})
			if err == nil {
				t.Fatalf("expected error for prefix %q (LIKE meta-char must be rejected at the upsert gate)", p)
			}
		})
	}

	// And confirm valid alphanumeric prefixes still go through.
	if err := svc.UpsertCountry(types.Country{Name: "Englandland", Prefix: "M"}); err != nil {
		t.Fatalf("valid prefix rejected: %v", err)
	}
}

func TestUpsertContactedStation_ColdInsertAndMerge(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	// 1. Cold insert — operator types only Name during a QSO with a
	// previously-unknown callsign (the ZS6XYZ scenario from the ADR
	// 0017 discussion). Path 2 (QSO-submit upsert).
	if err := svc.UpsertContactedStationWithContext(ctx, types.ContactedStation{
		Call: "ZS6XYZ",
		Name: "John",
		QTH:  "Pretoria",
	}); err != nil {
		t.Fatalf("cold insert: %v", err)
	}
	got, err := svc.FetchContactedStationByCallsign("ZS6XYZ")
	if err != nil {
		t.Fatalf("fetch after cold insert: %v", err)
	}
	if got.Name != "John" || got.QTH != "Pretoria" {
		t.Fatalf("after cold insert: got %+v, want John/Pretoria", got)
	}
	if got.LastRefreshedAt.IsZero() {
		t.Fatal("LastRefreshedAt is zero after upsert")
	}
	firstRefresh := got.LastRefreshedAt

	// 2. Provider catches up months later — QRZ now has the call with
	// a fuller record. Per ADR 0017 #11, refresh data wins on conflict
	// — Name overwritten, fields the provider supplies overwrite, and
	// fields the provider doesn't supply (e.g. QTH not in this synthetic
	// QRZ response) keep the existing value via the non-empty-wins
	// merge.
	time.Sleep(1100 * time.Millisecond) // datetime() per-second resolution
	if err := svc.UpsertContactedStationWithContext(ctx, types.ContactedStation{
		Call:       "ZS6XYZ",
		Name:       "John P. Smith",
		Gridsquare: "KG44",
	}); err != nil {
		t.Fatalf("merge upsert: %v", err)
	}
	got2, err := svc.FetchContactedStationByCallsign("ZS6XYZ")
	if err != nil {
		t.Fatalf("fetch after merge: %v", err)
	}
	if got2.Name != "John P. Smith" {
		t.Fatalf("after merge: Name = %q, want refresh value to win", got2.Name)
	}
	if got2.Gridsquare != "KG44" {
		t.Fatalf("after merge: Gridsquare = %q, want new value", got2.Gridsquare)
	}
	// QTH not supplied in the second write — non-empty-wins-per-field
	// keeps the operator's "Pretoria" rather than overwriting with empty.
	if got2.QTH != "Pretoria" {
		t.Fatalf("after merge: QTH = %q, want preserved Pretoria", got2.QTH)
	}
	if !got2.LastRefreshedAt.After(firstRefresh) {
		t.Fatalf("LastRefreshedAt did not advance: first=%v second=%v", firstRefresh, got2.LastRefreshedAt)
	}
	// PK preserved across the merge.
	if got2.CSID != got.CSID {
		t.Fatalf("CSID changed across upsert: %d → %d", got.CSID, got2.CSID)
	}
}

func TestUpsertContactedStation_RejectsEmptyCall(t *testing.T) {
	svc := testService(t)
	if err := svc.UpsertContactedStation(types.ContactedStation{Name: "X"}); err == nil {
		t.Fatal("expected error for empty call")
	}
}

func TestLastRefreshedAt_RoundTripsThroughAdapter(t *testing.T) {
	svc := testService(t)

	// A callsign-class-style write goes through Insert→adapter→DB.
	id, err := svc.InsertContactedStation(types.ContactedStation{
		Call:            "M0CMC",
		Name:            "Marc",
		Country:         "England",
		LastRefreshedAt: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id < 1 {
		t.Fatalf("id = %d", id)
	}

	got, err := svc.FetchContactedStationByCallsign("M0CMC")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.LastRefreshedAt.IsZero() {
		t.Fatal("LastRefreshedAt round-tripped as zero (write or read lost the value)")
	}
	// SQLite datetime resolution is per-second; allow that.
	wantUnix := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC).Unix()
	if got.LastRefreshedAt.Unix() != wantUnix {
		t.Fatalf("LastRefreshedAt = %v (unix=%d), want unix=%d",
			got.LastRefreshedAt, got.LastRefreshedAt.Unix(), wantUnix)
	}
}
