package sqlite

import (
	"context"
	stderr "errors"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// testService wires a Service to an in-memory sqlite database, opens it,
// and runs migrations. The returned service is ready for use.
func testService(t *testing.T) *Service {
	t.Helper()

	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"

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
	// Force the in-memory path.
	svc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      ":memory:",
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := svc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := svc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = svc.Close()
		_ = logSvc.Close()
	})
	return svc
}

// validTestQso builds a minimal valid QSO. The call field defaults to
// "M0CMC" and the dedupe key is computed manually so rows inserted by
// InsertQsoTx bypass qsoservice.
func validTestQso(logbookID int64, call, band, mode, qsoDate, timeOn string) types.Qso {
	q := types.Qso{LogbookID: logbookID}
	q.ContactedStation.Call = call
	q.ContactedStation.Country = "Test"
	q.QsoDetails.Band = band
	q.QsoDetails.Mode = mode
	q.QsoDetails.Freq = "7.050" // canonical MHz decimal — types.Qso.Freq is ADIF-native
	q.QsoDetails.QsoDate = qsoDate
	q.QsoDetails.TimeOn = timeOn
	q.QsoDetails.TimeOff = timeOn
	q.QsoDetails.RstSent = "59"
	q.QsoDetails.RstRcvd = "59"
	q.LoggingStation.StationCallsign = "G4ABC"
	// 64-char hex dedupe key — the value itself doesn't matter for these
	// tests; uniqueness does.
	q.DedupeKey = call + band + mode + qsoDate + timeOn + "padding000000000000000000000000000000000000000000"
	if len(q.DedupeKey) > 64 {
		q.DedupeKey = q.DedupeKey[:64]
	}
	return q
}

// ---- Service lifecycle ----

func TestService_InitializeWithoutLogger_Fails(t *testing.T) {
	svc := &Service{}
	err := svc.Initialize()
	if err == nil {
		t.Fatal("expected error when logger is nil")
	}
}

func TestService_InitializeWithoutConfig_Fails(t *testing.T) {
	logSvc := &logging.Service{}
	svc := &Service{LoggerService: logSvc}
	err := svc.Initialize()
	if err == nil {
		t.Fatal("expected error when config is nil")
	}
}

func TestService_OpenWithoutInitialize_Fails(t *testing.T) {
	svc := &Service{}
	err := svc.Open()
	if err == nil {
		t.Fatal("expected error when service not initialized")
	}
}

func TestService_CloseIsIdempotent(t *testing.T) {
	svc := testService(t)
	if err := svc.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestService_InitializeIsIdempotent(t *testing.T) {
	svc := testService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("re-init: %v", err)
	}
}

// TestService_InitOpenCloseInitOpen is the M4 regression: Close must
// reset the Initialize guard so a subsequent Initialize re-executes
// (previously it was a silent no-op, masking any config change that
// might have happened between cycles).
func TestService_InitOpenCloseInitOpen(t *testing.T) {
	svc := testService(t) // already initialised + open + migrated

	if err := svc.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	// Re-init must actually run — if it silently no-ops, isInitialized
	// stays false from the reset and Open will fail with the
	// not-initialised error.
	if err := svc.Initialize(); err != nil {
		t.Fatalf("re-init after close: %v", err)
	}

	// testService forces :memory: after Initialize because the DI path
	// resolves the on-disk default. Repeat that here.
	svc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      ":memory:",
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}

	if err := svc.Open(); err != nil {
		t.Fatalf("re-open after re-init: %v", err)
	}
	if err := svc.Migrate(); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if err := svc.Ping(); err != nil {
		t.Fatalf("ping after cycle: %v", err)
	}
}

func TestService_DoubleOpen_Fails(t *testing.T) {
	svc := testService(t)
	err := svc.Open()
	if err == nil {
		t.Fatal("expected error on second open")
	}
}

func TestService_Ping_OK(t *testing.T) {
	svc := testService(t)
	if err := svc.Ping(); err != nil {
		t.Fatalf("ping on open db: %v", err)
	}
}

// ---- Logbook + QSO happy paths ----

func TestInsertLogbook_AndFetch(t *testing.T) {
	svc := testService(t)
	id, err := svc.InsertLogbook(types.Logbook{
		Name:     "Test",
		Callsign: "G4ABC",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id < 1 {
		t.Fatalf("unexpected id: %d", id)
	}

	got, err := svc.FetchLogbookByID(id)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Callsign != "G4ABC" {
		t.Fatalf("callsign = %q, want G4ABC", got.Callsign)
	}
}

func TestFetchLogbookByID_NotFound(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchLogbookByID(999)
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLogbookExistsByID(t *testing.T) {
	svc := testService(t)
	lbID, err := svc.InsertLogbook(types.Logbook{Name: "Primary", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Present row → true.
	got, err := svc.LogbookExistsByID(lbID)
	if err != nil {
		t.Fatalf("exists (present): %v", err)
	}
	if !got {
		t.Fatalf("exists = false, want true for id %d", lbID)
	}

	// Missing row → false, no error (not ErrNotFound — this is a boolean
	// question, not a fetch).
	got, err = svc.LogbookExistsByID(999)
	if err != nil {
		t.Fatalf("exists (missing): %v", err)
	}
	if got {
		t.Fatal("exists = true, want false for id 999")
	}

	// Soft-deleted row → false. `models.LogbookExists` filters
	// `deleted_at IS NULL`.
	if err = svc.DeleteLogbookByID(lbID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = svc.LogbookExistsByID(lbID)
	if err != nil {
		t.Fatalf("exists (soft-deleted): %v", err)
	}
	if got {
		t.Fatal("exists = true after soft-delete, want false")
	}

	// Invalid id → error.
	if _, err = svc.LogbookExistsByID(0); err == nil {
		t.Fatal("expected error for id=0")
	}
}

func TestLogbookCallsignByID(t *testing.T) {
	svc := testService(t)
	lbID, err := svc.InsertLogbook(types.Logbook{Name: "Primary", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Present row → callsign returned, no error.
	got, err := svc.LogbookCallsignByID(lbID)
	if err != nil {
		t.Fatalf("callsign (present): %v", err)
	}
	if got != "G4ABC" {
		t.Fatalf("callsign = %q, want G4ABC", got)
	}

	// Missing row → ErrNotFound. Unlike LogbookExistsByID (boolean
	// question → no error), this is a fetch, so ErrNotFound is the
	// right signal for "row doesn't exist".
	_, err = svc.LogbookCallsignByID(999)
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing id, got %v", err)
	}

	// Soft-deleted row → ErrNotFound. Consistent with how
	// FetchLogbookByIDWithContext behaves.
	if err = svc.DeleteLogbookByID(lbID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = svc.LogbookCallsignByID(lbID)
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for soft-deleted row, got %v", err)
	}

	// Invalid id → error (not ErrNotFound — id < 1 is a programming
	// error, not a missing row).
	_, err = svc.LogbookCallsignByID(0)
	if err == nil {
		t.Fatal("expected error for id=0")
	}
	if stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected non-ErrNotFound error for id=0, got %v", err)
	}
}

func TestInsertQso_AndFetchByID(t *testing.T) {
	svc := testService(t)
	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	id, err := svc.InsertQso(qso)
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}

	got, err := svc.FetchQsoById(id)
	if err != nil {
		t.Fatalf("fetch qso: %v", err)
	}
	if got.ContactedStation.Call != "M0CMC" {
		t.Fatalf("call = %q, want M0CMC", got.ContactedStation.Call)
	}
	if got.LogbookID != lbID {
		t.Fatalf("logbook id = %d, want %d", got.LogbookID, lbID)
	}
}

func TestFetchQsoById_NotFound(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchQsoById(999)
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFetchQsoById_InvalidID(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchQsoById(0)
	if err == nil {
		t.Fatal("expected error for id=0")
	}
}

// ---- Dedupe ----

func TestFetchQsoByDedupeKey_Match(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	_, err := svc.InsertQso(qso)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := svc.FetchQsoByDedupeKey(lbID, qso.DedupeKey)
	if err != nil {
		t.Fatalf("fetch by dedupe key: %v", err)
	}
	if got.ContactedStation.Call != "M0CMC" {
		t.Fatalf("call = %q, want M0CMC", got.ContactedStation.Call)
	}
}

func TestFetchQsoByDedupeKey_NoMatch(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	_, err := svc.FetchQsoByDedupeKey(lbID, "0000000000000000000000000000000000000000000000000000000000000000")
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFetchQsoByDedupeKey_EmptyKey(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchQsoByDedupeKey(1, "")
	if err == nil {
		t.Fatal("expected error for empty dedupe key")
	}
}

func TestFetchQsoByDedupeKey_InvalidLogbookID(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchQsoByDedupeKey(0, "somekey")
	if err == nil {
		t.Fatal("expected error for invalid logbook id")
	}
}

// ---- Contest duplicate ----

func TestIsContestDuplicate_HitAndMiss(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "Contest", Callsign: "G4ABC"})

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	if _, err := svc.InsertQso(qso); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Hit: same callsign + band in same logbook, mode skipped (band-only contest).
	hit, err := svc.IsContestDuplicateByLogbookID(lbID, "M0CMC", "40m", "")
	if err != nil {
		t.Fatalf("dupe check hit: %v", err)
	}
	if !hit {
		t.Fatal("expected contest duplicate hit")
	}

	// Hit: same callsign + band + matching mode.
	hitMode, err := svc.IsContestDuplicateByLogbookID(lbID, "M0CMC", "40m", "SSB")
	if err != nil {
		t.Fatalf("dupe check hit (mode): %v", err)
	}
	if !hitMode {
		t.Fatal("expected contest duplicate hit with mode")
	}

	// Miss: same callsign + band but different mode (band+mode contest).
	missMode, err := svc.IsContestDuplicateByLogbookID(lbID, "M0CMC", "40m", "CW")
	if err != nil {
		t.Fatalf("dupe check miss (mode): %v", err)
	}
	if missMode {
		t.Fatal("expected miss on different mode")
	}

	// Miss: different band
	miss, err := svc.IsContestDuplicateByLogbookID(lbID, "M0CMC", "20m", "")
	if err != nil {
		t.Fatalf("dupe check miss: %v", err)
	}
	if miss {
		t.Fatal("expected miss on different band")
	}

	// Miss: different callsign
	miss2, err := svc.IsContestDuplicateByLogbookID(lbID, "DL1ABC", "40m", "")
	if err != nil {
		t.Fatalf("dupe check miss2: %v", err)
	}
	if miss2 {
		t.Fatal("expected miss on different callsign")
	}
}

func TestIsContestDuplicate_InvalidInputs(t *testing.T) {
	svc := testService(t)
	if _, err := svc.IsContestDuplicateByLogbookID(0, "M0CMC", "40m", ""); err == nil {
		t.Fatal("expected error for id=0")
	}
	if _, err := svc.IsContestDuplicateByLogbookID(1, "", "40m", ""); err == nil {
		t.Fatal("expected error for empty callsign")
	}
	if _, err := svc.IsContestDuplicateByLogbookID(1, "M0CMC", "", ""); err == nil {
		t.Fatal("expected error for empty band")
	}
}

// ---- Logbook update ----

func TestUpdateLogbook_UpdatesFields(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "Old", Callsign: "G4ABC"})

	err := svc.UpdateLogbook(types.Logbook{
		ID:          lbID,
		Name:        "New",
		Callsign:    "G4ABC",
		Description: "updated",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := svc.FetchLogbookByID(lbID)
	if got.Name != "New" {
		t.Fatalf("name = %q, want New", got.Name)
	}
	if got.Description != "updated" {
		t.Fatalf("description = %q, want updated", got.Description)
	}
}

func TestUpdateLogbook_NotFound(t *testing.T) {
	svc := testService(t)
	err := svc.UpdateLogbook(types.Logbook{
		ID:       999,
		Name:     "Ghost",
		Callsign: "G4ABC",
	})
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateLogbook_InvalidID(t *testing.T) {
	svc := testService(t)
	err := svc.UpdateLogbook(types.Logbook{ID: 0, Name: "X", Callsign: "G4ABC"})
	if err == nil {
		t.Fatal("expected error for id=0")
	}
}

// ---- Logbook delete ----

func TestDeleteLogbookByID_Empty_OK(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "ToDelete", Callsign: "G4ABC"})

	if err := svc.DeleteLogbookByID(lbID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestDeleteLogbookByID_WithQSOs_Rejected(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "HasQSOs", Callsign: "G4ABC"})

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	if _, err := svc.InsertQso(qso); err != nil {
		t.Fatalf("insert qso: %v", err)
	}

	err := svc.DeleteLogbookByID(lbID)
	if err == nil {
		t.Fatal("expected error when deleting logbook with QSOs")
	}
}

func TestDeleteLogbookByID_NotFound(t *testing.T) {
	svc := testService(t)
	err := svc.DeleteLogbookByID(999)
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- ContactedStation CRUD ----

func TestContactedStation_InsertFetchUpdate(t *testing.T) {
	svc := testService(t)

	id, err := svc.InsertContactedStation(types.ContactedStation{
		Call:    "M0CMC",
		Name:    "Marc",
		Country: "England",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := svc.FetchContactedStationByCallsign("M0CMC")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.CSID != id {
		t.Fatalf("csid = %d, want %d", got.CSID, id)
	}
	if got.Name != "Marc" {
		t.Fatalf("name = %q, want Marc", got.Name)
	}

	got.Name = "Marc L"
	if err := svc.UpdateContactedStation(got); err != nil {
		t.Fatalf("update: %v", err)
	}

	got2, _ := svc.FetchContactedStationByCallsign("M0CMC")
	if got2.Name != "Marc L" {
		t.Fatalf("updated name = %q, want Marc L", got2.Name)
	}
}

func TestFetchContactedStationByCallsign_NotFound(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchContactedStationByCallsign("NOBODY1")
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFetchContactedStationByCallsign_EmptyCallsign(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchContactedStationByCallsign("")
	if err == nil {
		t.Fatal("expected error for empty callsign")
	}
}

// ---- Country CRUD ----

func TestCountry_InsertFetchByName(t *testing.T) {
	svc := testService(t)

	id, err := svc.InsertCountry(types.Country{
		Name:       "Germany",
		Prefix:     "DL",
		Continent:  "EU",
		Ccode:      "DE",
		DXCCPrefix: "DL",
		TimeOffset: "+1",
		CQZone:     "14",
		ITUZone:    "28",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id < 1 {
		t.Fatalf("unexpected id: %d", id)
	}

	got, err := svc.FetchCountryByName("Germany")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Prefix != "DL" {
		t.Fatalf("prefix = %q, want DL", got.Prefix)
	}
}

func TestFetchCountryByCallsign_PrefixMatch(t *testing.T) {
	svc := testService(t)
	_, err := svc.InsertCountry(types.Country{
		Name: "Germany", Prefix: "DL", Continent: "EU",
		Ccode: "DE", DXCCPrefix: "DL", TimeOffset: "+1",
		CQZone: "14", ITUZone: "28",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := svc.FetchCountryByCallsign("DL1ABC")
	if err != nil {
		t.Fatalf("fetch by callsign: %v", err)
	}
	if got.Name != "Germany" {
		t.Fatalf("name = %q, want Germany", got.Name)
	}
}

func TestFetchCountryByCallsign_NoMatch(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchCountryByCallsign("ZZ1ABC")
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- QSO list and count ----

func TestFetchQsoSliceByLogbookId(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	// Insert 3 QSOs
	for i, call := range []string{"DL1ABC", "JA1ABC", "W1ABC"} {
		qso := validTestQso(lbID, call, "40m", "SSB", "20250508", "084"+string(rune('5'+i)))
		if _, err := svc.InsertQso(qso); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	got, err := svc.FetchQsoSliceByLogbookId(lbID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
}

func TestFetchQsoCountByLogbookId(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	count, err := svc.FetchQsoCountByLogbookId(lbID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	if _, err := svc.InsertQso(qso); err != nil {
		t.Fatalf("insert: %v", err)
	}

	count, err = svc.FetchQsoCountByLogbookId(lbID)
	if err != nil {
		t.Fatalf("count after insert: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

// ---- Contact history ----

func TestFetchQsoSliceByCallsign(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	// Two QSOs with same callsign, one with different callsign
	for _, q := range []types.Qso{
		validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"),
		validTestQso(lbID, "M0CMC", "20m", "CW", "20250509", "1200"),
		validTestQso(lbID, "DL1ABC", "40m", "SSB", "20250508", "0900"),
	} {
		if _, err := svc.InsertQso(q); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	got, err := svc.FetchQsoSliceByCallsign("M0CMC", 0, 0)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestFetchQsoSliceByCallsign_EmptyCallsign(t *testing.T) {
	svc := testService(t)
	_, err := svc.FetchQsoSliceByCallsign("", 0, 0)
	if err == nil {
		t.Fatal("expected error for empty callsign")
	}
}

// ---- Upload queue ----

// ---- Additional coverage: updates, paging, all-fetch, upsert, upload status ----

func TestUpdateQso(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	id, err := svc.InsertQso(qso)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Modify and update
	qso.ID = id
	qso.QsoDetails.Comment = "edited"
	if err := svc.UpdateQso(qso); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := svc.FetchQsoById(id)
	if got.QsoDetails.Comment != "edited" {
		t.Fatalf("comment = %q, want edited", got.QsoDetails.Comment)
	}
}

func TestUpdateQso_InvalidID(t *testing.T) {
	svc := testService(t)
	err := svc.UpdateQso(types.Qso{ID: 0})
	if err == nil {
		t.Fatal("expected error for id=0")
	}
}

func TestFetchAllLogbooks(t *testing.T) {
	svc := testService(t)
	_, _ = svc.InsertLogbook(types.Logbook{Name: "A", Callsign: "G4ABC"})
	_, _ = svc.InsertLogbook(types.Logbook{Name: "B", Callsign: "M0CMC"})

	got, err := svc.FetchAllLogbooks()
	if err != nil {
		t.Fatalf("fetch all: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestFetchQsoSlicePaging(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	for i, call := range []string{"DL1ABC", "JA1ABC", "W1ABC", "VK3ABC"} {
		qso := validTestQso(lbID, call, "40m", "SSB", "20250508", "084"+string(rune('5'+i)))
		if _, err := svc.InsertQso(qso); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// First page, 2 per page
	page1, err := svc.FetchQsoSlicePaging(lbID, 1, 2, Ascending)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 len = %d, want 2", len(page1))
	}

	// Second page
	page2, err := svc.FetchQsoSlicePaging(lbID, 2, 2, Ascending)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page 2 len = %d, want 2", len(page2))
	}
}

func TestFetchQsoSlicePaging_InvalidInputs(t *testing.T) {
	svc := testService(t)
	if _, err := svc.FetchQsoSlicePaging(0, 1, 10, Ascending); err == nil {
		t.Fatal("expected error for logbook id=0")
	}
	if _, err := svc.FetchQsoSlicePaging(1, 0, 10, Ascending); err == nil {
		t.Fatal("expected error for page=0")
	}
	if _, err := svc.FetchQsoSlicePaging(1, 1, 0, Ascending); err == nil {
		t.Fatal("expected error for pageSize=0")
	}
}

func TestUpdateCountry(t *testing.T) {
	svc := testService(t)
	id, err := svc.InsertCountry(types.Country{
		Name: "Germany", Prefix: "DL", Continent: "EU",
		Ccode: "DE", DXCCPrefix: "DL", TimeOffset: "+1",
		CQZone: "14", ITUZone: "28",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	err = svc.UpdateCountry(types.Country{
		ID:         id,
		Name:       "Federal Republic of Germany",
		Prefix:     "DL",
		Continent:  "EU",
		Ccode:      "DE",
		DXCCPrefix: "DL",
		TimeOffset: "+1",
		CQZone:     "14",
		ITUZone:    "28",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := svc.FetchCountryByName("Federal Republic of Germany")
	if got.Prefix != "DL" {
		t.Fatalf("prefix = %q, want DL", got.Prefix)
	}
}

func TestUpsertLogbook(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "Original", Callsign: "G4ABC"})

	// Upsert to update
	err := svc.UpsertLogbook(types.Logbook{
		ID:          lbID,
		Name:        "Upserted",
		Callsign:    "G4ABC",
		Description: "new description",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, _ := svc.FetchLogbookByID(lbID)
	if got.Name != "Upserted" {
		t.Fatalf("name = %q, want Upserted", got.Name)
	}
	if got.Description != "new description" {
		t.Fatalf("description = %q, want 'new description'", got.Description)
	}
}

func TestUpsertLogbook_InvalidID(t *testing.T) {
	svc := testService(t)
	err := svc.UpsertLogbook(types.Logbook{ID: 0, Name: "X", Callsign: "G4ABC"})
	if err == nil {
		t.Fatal("expected error for id=0")
	}
}

// ---- Upload queue (qso_upload) ----

// enqueueUpload is a test-only helper that inserts one qso_upload row via
// the transactional API. Wraps BeginTx/InsertQsoUploadTx/Commit so the
// upload-methods tests can set up fixtures without repeating the dance.
func enqueueUpload(t *testing.T, svc *Service, qsoID int64, name, typ string, act action.Action) {
	t.Helper()
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	if err = svc.InsertQsoUploadTx(ctx, tx, qsoID, act, name, typ); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert upload: %v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// rawExec runs a raw SQL statement against the service's DB. Test-only —
// used to back-date next_attempt_at columns for claim-ordering tests
// where the production API doesn't expose the knob.
func rawExec(t *testing.T, svc *Service, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	if _, err = tx.ExecContext(ctx, sql, args...); err != nil {
		_ = tx.Rollback()
		t.Fatalf("raw exec: %v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestClaimPendingUploads_Empty(t *testing.T) {
	svc := testService(t)

	got, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 5)
	if err != nil {
		t.Fatalf("claim on empty table: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0 (nothing to claim)", len(got))
	}
}

func TestClaimPendingUploads_HappyPath(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	qsoID, err := svc.InsertQso(qso)
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 5)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("len = %d, want 1", len(claimed))
	}

	row := claimed[0]
	if row.QsoID != qsoID {
		t.Fatalf("QsoID = %d, want %d", row.QsoID, qsoID)
	}
	if row.Status != "in_progress" {
		t.Fatalf("Status = %q, want in_progress (claim should flip it)", row.Status)
	}
	if row.ForwarderName != "qrz" || row.ForwarderType != "qrz" {
		t.Fatalf("name/type = %q/%q", row.ForwarderName, row.ForwarderType)
	}
	if row.LastAttemptAt == 0 {
		t.Fatal("LastAttemptAt not set by claim")
	}

	// Second claim should pick up nothing — the row is now in_progress,
	// not pending.
	again, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 5)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second claim returned %d rows, want 0", len(again))
	}
}

func TestClaimPendingUploads_ScopedByForwarderName(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	qsoID, _ := svc.InsertQso(qso)

	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	enqueueUpload(t, svc, qsoID, "clublog", "clublog", action.Insert)

	// Claim as qrz — clublog row must NOT be returned.
	got, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 5)
	if err != nil {
		t.Fatalf("claim qrz: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ForwarderName != "qrz" {
		t.Fatalf("claimed forwarder_name = %q, want qrz", got[0].ForwarderName)
	}
}

func TestClaimPendingUploads_OrderedByNextAttemptAt(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	// Three QSOs, three queue rows all for the same forwarder.
	var qsoIDs [3]int64
	for i := range 3 {
		q := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "084"+string(rune('5'+i)))
		q.DedupeKey = q.DedupeKey[:63] + string(rune('a'+i)) // uniquify
		id, err := svc.InsertQso(q)
		if err != nil {
			t.Fatalf("insert qso %d: %v", i, err)
		}
		qsoIDs[i] = id
		enqueueUpload(t, svc, id, "qrz", "qrz", action.Insert)
	}

	// Manually back-date the first two rows' next_attempt_at so we know
	// the expected claim order: row-0 (earliest) → row-1 → row-2.
	rawExec(t, svc,
		`UPDATE qso_upload SET next_attempt_at = ? WHERE qso_id = ?`,
		int64(1000), qsoIDs[0])
	rawExec(t, svc,
		`UPDATE qso_upload SET next_attempt_at = ? WHERE qso_id = ?`,
		int64(2000), qsoIDs[1])
	// row 2 keeps its default next_attempt_at ≈ now (large unix time)

	claimed, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 2)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("len = %d, want 2 (batch cap)", len(claimed))
	}
	// Oldest two next_attempt_at should come out, in order.
	if claimed[0].QsoID != qsoIDs[0] {
		t.Fatalf("first claim QsoID = %d, want %d (earliest next_attempt_at)", claimed[0].QsoID, qsoIDs[0])
	}
	if claimed[1].QsoID != qsoIDs[1] {
		t.Fatalf("second claim QsoID = %d, want %d", claimed[1].QsoID, qsoIDs[1])
	}
}

func TestClaimPendingUploads_SkipsFutureNextAttempt(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	// Push next_attempt_at an hour into the future. Claim must not pick
	// this up.
	future := time.Now().Add(time.Hour).Unix()
	rawExec(t, svc,
		`UPDATE qso_upload SET next_attempt_at = ? WHERE qso_id = ?`,
		future, qsoID)

	got, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 5)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0 (next_attempt_at in future)", len(got))
	}
}

func TestMarkUploadSuccess(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	if len(claimed) != 1 {
		t.Fatalf("claim: len = %d, want 1", len(claimed))
	}
	rowID := claimed[0].ID

	if err := svc.MarkUploadSuccessWithContext(context.Background(), rowID, "upstream-42"); err != nil {
		t.Fatalf("mark success: %v", err)
	}

	uploads, err := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(uploads) != 1 {
		t.Fatalf("len = %d, want 1", len(uploads))
	}
	got := uploads[0]
	if got.Status != "uploaded" {
		t.Fatalf("Status = %q, want uploaded", got.Status)
	}
	if got.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", got.Attempts)
	}
	if got.UpstreamID != "upstream-42" {
		t.Fatalf("UpstreamID = %q, want upstream-42", got.UpstreamID)
	}
	if got.LastError != "" {
		t.Fatalf("LastError = %q, want empty", got.LastError)
	}
}

func TestMarkUploadSuccess_EmptyUpstreamID_StoresNull(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	rowID := claimed[0].ID

	if err := svc.MarkUploadSuccessWithContext(context.Background(), rowID, ""); err != nil {
		t.Fatalf("mark success: %v", err)
	}

	uploads, _ := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if uploads[0].UpstreamID != "" {
		t.Fatalf("UpstreamID = %q, want empty (stored as NULL)", uploads[0].UpstreamID)
	}
}

func TestMarkUploadTransientRetry(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	rowID := claimed[0].ID

	future := time.Now().Add(time.Minute).Unix()
	if err := svc.MarkUploadTransientRetryWithContext(context.Background(), rowID, future, "timeout"); err != nil {
		t.Fatalf("mark transient retry: %v", err)
	}

	uploads, _ := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	got := uploads[0]
	if got.Status != "pending" {
		t.Fatalf("Status = %q, want pending (transient retry goes back to pending)", got.Status)
	}
	if got.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", got.Attempts)
	}
	if got.NextAttemptAt != future {
		t.Fatalf("NextAttemptAt = %d, want %d", got.NextAttemptAt, future)
	}
	if got.LastError != "timeout" {
		t.Fatalf("LastError = %q, want timeout", got.LastError)
	}
}

func TestMarkUploadFailed(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	rowID := claimed[0].ID

	if err := svc.MarkUploadFailedWithContext(context.Background(), rowID, "bad credentials"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	uploads, _ := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	got := uploads[0]
	if got.Status != "failed" {
		t.Fatalf("Status = %q, want failed", got.Status)
	}
	if got.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", got.Attempts)
	}
	if got.LastError != "bad credentials" {
		t.Fatalf("LastError = %q, want 'bad credentials'", got.LastError)
	}
}

func TestResetOrphanedUploads(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	// Claim (flips pending → in_progress) and then crash — don't mark
	// the outcome. The row is now orphaned.
	if _, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1); err != nil {
		t.Fatalf("claim: %v", err)
	}

	n, err := svc.ResetOrphanedUploadsWithContext(context.Background())
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if n != 1 {
		t.Fatalf("reset count = %d, want 1", n)
	}

	uploads, _ := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if uploads[0].Status != "pending" {
		t.Fatalf("Status = %q after reset, want pending", uploads[0].Status)
	}
}

func TestResetOrphanedUploads_NoOrphans_ReturnsZero(t *testing.T) {
	svc := testService(t)
	n, err := svc.ResetOrphanedUploadsWithContext(context.Background())
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if n != 0 {
		t.Fatalf("reset count = %d, want 0", n)
	}
}

func TestFetchUploadsByQsoID_Empty(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	got, err := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0 (no queue rows for this qso)", len(got))
	}
}

func TestFetchUploadsByQsoID_Multiple(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	enqueueUpload(t, svc, qsoID, "clublog", "clublog", action.Insert)
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Update)

	got, err := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Ordered (forwarder_name, action): clublog/insert, qrz/insert, qrz/update.
	wantOrder := []string{"clublog|insert", "qrz|insert", "qrz|update"}
	for i, w := range wantOrder {
		have := got[i].ForwarderName + "|" + got[i].Action
		if have != w {
			t.Fatalf("row %d = %q, want %q", i, have, w)
		}
	}
}

// ---- FetchInsertUpstreamID (stage 5: delete-action LOGID resolution) ----

func TestFetchInsertUpstreamID_NoRows_ReturnsEmpty(t *testing.T) {
	svc := testService(t)

	got, err := svc.FetchInsertUpstreamIDWithContext(context.Background(), 999, "qrz")
	if err != nil {
		t.Fatalf("fetch on empty: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty string", got)
	}
}

func TestFetchInsertUpstreamID_HappyPath(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// Enqueue + claim + mark-success on an insert row so qso_upload has
	// an uploaded row with a populated upstream_id.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	if err := svc.MarkUploadSuccessWithContext(context.Background(), claimed[0].ID, "1440100000"); err != nil {
		t.Fatalf("mark success: %v", err)
	}

	got, err := svc.FetchInsertUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "1440100000" {
		t.Fatalf("got %q, want 1440100000", got)
	}
}

func TestFetchInsertUpstreamID_IgnoresOtherForwarders(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// Successful insert on "clublog" must not leak into a "qrz" query.
	enqueueUpload(t, svc, qsoID, "clublog", "clublog", action.Insert)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "clublog", 1)
	_ = svc.MarkUploadSuccessWithContext(context.Background(), claimed[0].ID, "cl-999")

	got, err := svc.FetchInsertUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty (clublog upstream_id must not match qrz query)", got)
	}
}

func TestFetchInsertUpstreamID_IgnoresNonInsertActions(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// Successful update (with upstream_id!) should not satisfy a
	// delete's LOGID lookup — the update didn't create the record.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Update)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	_ = svc.MarkUploadSuccessWithContext(context.Background(), claimed[0].ID, "update-upstream")

	got, err := svc.FetchInsertUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty (update row must not satisfy insert lookup)", got)
	}
}

func TestFetchInsertUpstreamID_IgnoresPendingAndFailed(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// A pending insert row shouldn't match — no upstream_id yet.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	got, err := svc.FetchInsertUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty (pending row must not match)", got)
	}

	// A failed insert row shouldn't match either — even though the
	// row exists, it didn't produce an upstream id.
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	_ = svc.MarkUploadFailedWithContext(context.Background(), claimed[0].ID, "boom")

	got, err = svc.FetchInsertUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty (failed row must not match)", got)
	}
}

// Documents the schema constraint that makes the ORDER BY in
// FetchInsertUpstreamIDWithContext defensive rather than load-bearing:
// the qso_upload table has UNIQUE(qso_id, forwarder_name, action), so
// a second insert row for the same (qso, forwarder) pair is rejected
// by the DB, not chosen by our query. If the schema ever relaxes this
// constraint, the ORDER BY modified_at DESC LIMIT 1 protects us; the
// test below pins the current behaviour.
func TestFetchInsertUpstreamID_UniqueConstraintPreventsDuplicates(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// First insert row succeeds.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	// Second insert row for the same (qso, forwarder, insert) must
	// fail on the UNIQUE constraint.
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	err = svc.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, "qrz", "qrz")
	_ = tx.Rollback()
	if err == nil {
		t.Fatal("expected UNIQUE constraint to reject a second insert row for (qso, forwarder)")
	}
}

func TestFetchInsertUpstreamID_InvalidInputs(t *testing.T) {
	svc := testService(t)

	if _, err := svc.FetchInsertUpstreamIDWithContext(context.Background(), 0, "qrz"); err == nil {
		t.Fatal("expected error for qsoID=0")
	}
	if _, err := svc.FetchInsertUpstreamIDWithContext(context.Background(), 1, ""); err == nil {
		t.Fatal("expected error for empty forwarderName")
	}
}

// ---- MarkUploadSuccessWithAdifStamp (stage 6: ADIF upload-status stamp) ----

// todayUTC8 returns today's UTC date in ADIF YYYYMMDD form, matching
// what the method under test stamps. Used by the assertions below.
func todayUTC8() string {
	return time.Now().UTC().Format("20060102")
}

func TestMarkUploadSuccessWithAdifStamp_HappyPath(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	rowID := claimed[0].ID

	if err := svc.MarkUploadSuccessWithAdifStampWithContext(
		context.Background(), rowID, "logid-42", qsoID, "QRZCOM",
	); err != nil {
		t.Fatalf("mark + stamp: %v", err)
	}

	// qso_upload row transitioned as expected.
	uploads, _ := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(uploads))
	}
	got := uploads[0]
	if got.Status != "uploaded" {
		t.Fatalf("Status = %q, want uploaded", got.Status)
	}
	if got.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", got.Attempts)
	}
	if got.UpstreamID != "logid-42" {
		t.Fatalf("UpstreamID = %q, want logid-42", got.UpstreamID)
	}

	// QSO row stamped — surfaced on read via QrzComUploadStatus / QrzComUploadDate.
	qso, err := svc.FetchQsoByIdWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch qso: %v", err)
	}
	if qso.QrzComUploadStatus != "Y" {
		t.Fatalf("QrzComUploadStatus = %q, want Y", qso.QrzComUploadStatus)
	}
	if qso.QrzComUploadDate != todayUTC8() {
		t.Fatalf("QrzComUploadDate = %q, want %q", qso.QrzComUploadDate, todayUTC8())
	}
}

func TestMarkUploadSuccessWithAdifStamp_AdditionalDataContainsKeys(t *testing.T) {
	// Belt-and-braces: read the raw additional_data blob via raw SQL
	// and confirm the JSON keys are literally present. Catches any
	// future refactor that might route types.Qso through a different
	// persistence shape.
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)

	if err := svc.MarkUploadSuccessWithAdifStampWithContext(
		context.Background(), claimed[0].ID, "logid-99", qsoID, "QRZCOM",
	); err != nil {
		t.Fatalf("mark + stamp: %v", err)
	}

	tx, cancel, _ := svc.BeginTxContext(context.Background())
	defer cancel()
	var raw string
	err := tx.QueryRowContext(
		context.Background(),
		`SELECT additional_data FROM qso WHERE id = ?`, qsoID,
	).Scan(&raw)
	_ = tx.Rollback()
	if err != nil {
		t.Fatalf("read raw additional_data: %v", err)
	}
	if !strings.Contains(raw, `"qrzcom_qso_upload_status":"Y"`) {
		t.Fatalf("additional_data missing upload_status stamp: %s", raw)
	}
	if !strings.Contains(raw, `"qrzcom_qso_upload_date":"`+todayUTC8()+`"`) {
		t.Fatalf("additional_data missing upload_date stamp: %s", raw)
	}
}

func TestMarkUploadSuccessWithAdifStamp_UnknownPrefix_WorksGenerically(t *testing.T) {
	// The method is prefix-agnostic — a new forwarder's prefix writes
	// the right JSON keys even when no Go struct field currently
	// surfaces them. Pin this so future forwarders don't have to
	// change the sqlite layer.
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "clublog", "clublog", action.Insert)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "clublog", 1)

	if err := svc.MarkUploadSuccessWithAdifStampWithContext(
		context.Background(), claimed[0].ID, "cl-5", qsoID, "CLUBLOG",
	); err != nil {
		t.Fatalf("mark + stamp with CLUBLOG prefix: %v", err)
	}

	tx, cancel, _ := svc.BeginTxContext(context.Background())
	defer cancel()
	var raw string
	_ = tx.QueryRowContext(
		context.Background(),
		`SELECT additional_data FROM qso WHERE id = ?`, qsoID,
	).Scan(&raw)
	_ = tx.Rollback()

	if !strings.Contains(raw, `"clublog_qso_upload_status":"Y"`) {
		t.Fatalf("additional_data missing CLUBLOG stamp: %s", raw)
	}
}

func TestMarkUploadSuccessWithAdifStamp_InvalidPrefix_Rejected(t *testing.T) {
	svc := testService(t)

	cases := []string{
		"",
		"qrzcom",  // lowercase
		"QRZ com", // space
		"QRZCOM!", // punctuation
		"1QRZ",    // digit-first
		"Q;DROP",  // injection attempt
		"QRZCOMé", // multi-byte UTF-8 — ASCII-scoped regex must reject
	}
	for _, bad := range cases {
		err := svc.MarkUploadSuccessWithAdifStampWithContext(
			context.Background(), 1, "upstream", 1, bad,
		)
		if err == nil {
			t.Fatalf("prefix %q: want error, got nil", bad)
		}
	}
}

func TestMarkUploadSuccessWithAdifStamp_MissingUploadRow_NotFound(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// qsoID exists but no qso_upload row.
	err := svc.MarkUploadSuccessWithAdifStampWithContext(
		context.Background(), 9999, "upstream", qsoID, "QRZCOM",
	)
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMarkUploadSuccessWithAdifStamp_MissingQso_RollsBack(t *testing.T) {
	// One-fails-all-fail: when the qso row lookup fails, the
	// qso_upload transition must roll back too, leaving the row in
	// its claimed (in_progress) state so the worker can retry.
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)

	// Point at a non-existent QSO id.
	err := svc.MarkUploadSuccessWithAdifStampWithContext(
		context.Background(), claimed[0].ID, "u", 99999, "QRZCOM",
	)
	if !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	// Upload row must NOT be marked uploaded — transaction rolled
	// back — so the worker can observe it is still in_progress and
	// reset-orphans on restart will requeue it.
	uploads, _ := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(uploads))
	}
	if uploads[0].Status == "uploaded" {
		t.Fatal("qso_upload reached uploaded despite failed qso stamp — rollback broken")
	}
}
