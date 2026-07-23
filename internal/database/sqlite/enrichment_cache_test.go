package sqlite

import (
	"context"
	stderr "errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
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
	if err := svc.UpsertCountry(types.Country{Name: "Englandland", Prefix: "MW"}); err != nil {
		t.Fatalf("valid prefix rejected: %v", err)
	}
}

// TestUpsertCountry_RejectsSingleCharPrefix pins the U-block fix: ITU
// one-letter blocks span multiple DXCC entities ('U' = European/Asiatic
// Russia + Ukraine + Uzbekistan + Kazakhstan), so a one-char row over-matches
// every callsign in the block on the longest-prefix read. A cached
// prefix='U' → "European Russia" row misfiled every Ukrainian call between
// 2026-06-26 and 2026-07-11; the write gate (plus reference migration 0002
// for already-cached rows) closes that class.
func TestUpsertCountry_RejectsSingleCharPrefix(t *testing.T) {
	svc := testService(t)

	for _, p := range []string{"U", "R", "F", "u", " U "} {
		t.Run(p, func(t *testing.T) {
			err := svc.UpsertCountry(types.Country{Name: "European Russia", Prefix: p})
			if err == nil {
				t.Fatalf("expected error for single-char prefix %q", p)
			}
		})
	}

	// Two-char prefixes are the shortest accepted key.
	if err := svc.UpsertCountry(types.Country{Name: "Ukraine", Prefix: "UR"}); err != nil {
		t.Fatalf("two-char prefix rejected: %v", err)
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

// concurrentCacheService wires a FILE-backed Service with a real connection
// pool. The shared :memory: helpers pin MaxOpenConns to 1, which serialises
// every writer and so cannot exercise a read-then-write race at all.
func concurrentCacheService(t *testing.T, path string) *Service {
	t.Helper()

	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = path
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}
	logSvc := &logging.Service{}
	logSvc.ConfigService = cfgSvc
	logSvc.WorkingDir = cfgSvc.WorkingDir()
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	svc := &Service{}
	svc.ConfigService = cfgSvc
	svc.LoggerService = logSvc
	if err := svc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	svc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      path,
		MaxOpenConns:              4,
		MaxIdleConns:              4,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := svc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() {
		_ = svc.Close()
		_ = logSvc.Close()
	})
	if err := svc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	return svc
}

// TestUpsertContactedStation_ConcurrentColdMissesDoNotCollide pins the fix for
// finding 4 (2026-07-22 sqlite review): the writer used to pick INSERT vs
// UPDATE from its own earlier read, so simultaneous cold misses all chose
// INSERT and everyone but the first hit the active-callsign unique index. The
// single INSERT … ON CONFLICT … DO UPDATE lets the database resolve that
// collision instead — every writer succeeds and exactly one row exists.
func TestUpsertContactedStation_ConcurrentColdMissesDoNotCollide(t *testing.T) {
	svc := concurrentCacheService(t, filepath.Join(t.TempDir(), "cache.db"))
	ctx := context.Background()

	const writers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	start := make(chan struct{})

	for i := range writers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start // release them together, so they race on the same cold miss
			errCh <- svc.UpsertContactedStationWithContext(ctx, types.ContactedStation{
				Call: "ZS6XYZ",
				Name: fmt.Sprintf("Writer %d", n),
				QTH:  "Pretoria",
			})
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("concurrent upsert failed: %v", err)
		}
	}

	var n int
	if err := svc.handle.QueryRow(
		`SELECT COUNT(*) FROM contacted_station WHERE call = 'ZS6XYZ' AND deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 1 {
		t.Errorf("live rows for ZS6XYZ = %d, want exactly 1", n)
	}

	// Whichever writer landed last, the shared field must have survived —
	// a collision must not leave the row half-written.
	got, err := svc.FetchContactedStationByCallsignWithContext(ctx, "ZS6XYZ")
	if err != nil {
		t.Fatalf("fetch after concurrent upserts: %v", err)
	}
	if got.QTH != "Pretoria" {
		t.Errorf("QTH = %q, want Pretoria", got.QTH)
	}
	if !strings.HasPrefix(got.Name, "Writer ") {
		t.Errorf("Name = %q, want one of the writers' values", got.Name)
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
