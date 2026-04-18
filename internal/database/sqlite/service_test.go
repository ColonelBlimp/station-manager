package sqlite

import (
	stderr "errors"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
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
	q.QsoDetails.Freq = "7050" // kHz
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

func TestUpdateQsoUploadStatus(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	qsoID, err := svc.InsertQso(qso)
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}

	if err := svc.InsertQsoUpload(qsoID, "insert", "qrzforwardingservice"); err != nil {
		t.Fatalf("insert upload: %v", err)
	}

	uploads, _ := svc.FetchPendingUploads()
	if len(uploads) != 1 {
		t.Fatalf("len = %d, want 1", len(uploads))
	}
	uploadID := uploads[0].ID

	// Mark as uploaded
	err = svc.UpdateQsoUploadStatus(uploadID, "uploaded", "insert", 1, "")
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
}

func TestUpdateQsoUploadStatus_InvalidID(t *testing.T) {
	svc := testService(t)
	err := svc.UpdateQsoUploadStatus(0, "uploaded", "insert", 1, "")
	if err == nil {
		t.Fatal("expected error for id=0")
	}
}

func TestInsertAndFetchPendingUploads(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	qsoID, err := svc.InsertQso(qso)
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}

	if err := svc.InsertQsoUpload(qsoID, "insert", "qrzforwardingservice"); err != nil {
		t.Fatalf("insert upload: %v", err)
	}

	uploads, err := svc.FetchPendingUploads()
	if err != nil {
		t.Fatalf("fetch pending: %v", err)
	}
	if len(uploads) != 1 {
		t.Fatalf("len = %d, want 1", len(uploads))
	}
	if uploads[0].QsoID != qsoID {
		t.Fatalf("qso id = %d, want %d", uploads[0].QsoID, qsoID)
	}
}
