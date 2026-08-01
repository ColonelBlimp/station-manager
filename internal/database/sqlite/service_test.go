package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	migratepkg "github.com/golang-migrate/migrate/v4"
)

// TestMissingCoreTables_FlagsIncompleteSchema guards review 2026-06-19 M2:
// post-migration verification must flag every runtime-required table, not just
// logbook+qso — a "version current" DB missing qso_upload / qso_history /
// contacted_station / country would otherwise pass startup and fail later in
// forwarding, audit-history, or enrichment code.
func TestMissingCoreTables_FlagsIncompleteSchema(t *testing.T) {
	svc := testService(t) // fully migrated — all tables present
	if missing, err := svc.missingCoreTables(); err != nil || len(missing) != 0 {
		t.Fatalf("freshly-migrated schema reported missing=%v err=%v, want none", missing, err)
	}

	// Simulate a partially-damaged schema by dropping the tables the old
	// two-table check ignored. FK enforcement off so DROP order is irrelevant.
	if _, err := svc.handle.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	dropped := []string{"qso_history", "qso_upload", "contacted_station", "country"}
	for _, tbl := range dropped {
		if _, err := svc.handle.Exec("DROP TABLE " + tbl); err != nil {
			t.Fatalf("drop %s: %v", tbl, err)
		}
	}

	missing, err := svc.missingCoreTables()
	if err != nil {
		t.Fatalf("missingCoreTables: %v", err)
	}
	want := map[string]struct{}{}
	for _, tbl := range dropped {
		want[tbl] = struct{}{}
	}
	for _, m := range missing {
		delete(want, m)
	}
	if len(want) > 0 {
		t.Errorf("missingCoreTables failed to flag %v; got %v", want, missing)
	}
}

// testService wires a Service to an in-memory sqlite database, opens it,
// and runs migrations. The returned service is ready for use.
func testService(t *testing.T) *Service {
	svc := testServiceWithoutMigrations(t)
	if err := svc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	return svc
}

// testServiceWithoutMigrations wires and opens a Service without running schema
// migrations. Tests that need to stop at an intermediate migration version use
// this to drive golang-migrate manually.
func testServiceWithoutMigrations(t *testing.T) *Service {
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
	t.Cleanup(func() {
		_ = svc.Close()
		_ = logSvc.Close()
	})
	return svc
}

// applyMigrationSteps drives the LOG set's migrations by a fixed number of
// steps. The RST-length migrations (0001 tight → 0002 relaxed) live in the log
// set, so the version-1-to-2 tests step that set; the reference set is left
// alone (these tests don't touch the enrichment caches).
// splitService wires a file-backed Service restricted to the given migration
// set(s) — the real daemon's per-connection shape (reference.db / log-db
// split), which the default :memory: helpers (single connection, all sets)
// don't exercise.
func splitService(t *testing.T, path string, sets ...string) *Service {
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
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	svc.SetMigrationSets(sets...)
	if err := svc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() {
		_ = svc.Close()
		_ = logSvc.Close()
	})
	if err := svc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate (%v): %v", sets, err)
	}
	return svc
}

func tableExists(t *testing.T, svc *Service, name string) bool {
	t.Helper()
	var got string
	err := svc.handle.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&got)
	if err == nil {
		return true
	}
	if stderr.Is(err, sql.ErrNoRows) {
		return false
	}
	t.Fatalf("tableExists(%q): %v", name, err)
	return false
}

// TestMigrate_SplitSetsIsolateTables proves the migration-set split actually
// partitions the schema: a log-only connection contains the four log tables and
// NONE of the enrichment caches, and a reference-only connection is the mirror.
// This is the regression guard against a cache-table CREATE leaking into the log
// set (or vice versa) — the failure the reference.db / log-db split exists to
// prevent.
func TestMigrate_SplitSetsIsolateTables(t *testing.T) {
	dir := t.TempDir()
	logTables := []string{"logbook", "qso", "qso_upload", "qso_history"}
	refTables := []string{"contacted_station", "country"}

	logSvc := splitService(t, filepath.Join(dir, "log.db"), MigrationSetLog)
	for _, tbl := range logTables {
		if !tableExists(t, logSvc, tbl) {
			t.Errorf("log connection missing expected table %q", tbl)
		}
	}
	for _, tbl := range refTables {
		if tableExists(t, logSvc, tbl) {
			t.Errorf("log connection unexpectedly has reference table %q", tbl)
		}
	}

	refSvc := splitService(t, filepath.Join(dir, "reference.db"), MigrationSetReference)
	for _, tbl := range refTables {
		if !tableExists(t, refSvc, tbl) {
			t.Errorf("reference connection missing expected table %q", tbl)
		}
	}
	for _, tbl := range logTables {
		if tableExists(t, refSvc, tbl) {
			t.Errorf("reference connection unexpectedly has log table %q", tbl)
		}
	}
}

// migrateToVersion migrates the log set to an ABSOLUTE version.
//
// Prefer this over applyMigrationSteps for characterization tests that target a
// specific migration: relative steps silently retarget every time a new
// migration lands (0007 did exactly that to the 0006 widen test, 2026-08-01).
func migrateToVersion(t *testing.T, svc *Service, version uint) {
	t.Helper()
	srcDriver, dbDriver, err := GetMigrationDrivers(svc.handle, MigrationSetLog)
	if err != nil {
		t.Fatalf("migration drivers: %v", err)
	}
	defer func() { _ = srcDriver.Close() }()

	m, err := migratepkg.NewWithInstance("iofs", srcDriver, svc.DatabaseConfig.Driver, dbDriver)
	if err != nil {
		t.Fatalf("migrate instance: %v", err)
	}
	if err := m.Migrate(version); err != nil {
		t.Fatalf("migrate to %d: %v", version, err)
	}
}

func applyMigrationSteps(t *testing.T, svc *Service, steps int) {
	t.Helper()
	srcDriver, dbDriver, err := GetMigrationDrivers(svc.handle, MigrationSetLog)
	if err != nil {
		t.Fatalf("migration drivers: %v", err)
	}
	defer func() { _ = srcDriver.Close() }()

	m, err := migratepkg.NewWithInstance("iofs", srcDriver, svc.DatabaseConfig.Driver, dbDriver)
	if err != nil {
		t.Fatalf("migrate instance: %v", err)
	}
	if err := m.Steps(steps); err != nil {
		t.Fatalf("migrate steps(%d): %v", steps, err)
	}
}

// validTestQso builds a minimal valid QSO. The call field defaults to
// "M0CMC" and the dedupe key is computed manually so rows inserted by
// InsertQsoTx bypass qsoservice.
// insertQsoRawV1 inserts a QSO with raw SQL shaped for the VERSION-1 schema.
// The current sqlboiler model cannot write to pre-0005 schemas: it fetches
// the revision default column after insert, and SQLite's double-quoted-string
// fallback turns the missing column into a literal (a string scan error).
// Migration characterization needs a version-frozen writer, not the live model.
func insertQsoRawV1(t *testing.T, svc *Service, q types.Qso) int64 {
	t.Helper()
	freqKHz, err := utils.ParseFreqMHz(q.QsoDetails.Freq)
	if err != nil {
		t.Fatalf("parse fixture freq: %v", err)
	}
	res, err := svc.handle.Exec(`INSERT INTO qso
		(uuid, call, band, mode, freq, qso_date, time_on, time_off,
		 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.UUID, q.ContactedStation.Call, q.QsoDetails.Band, q.QsoDetails.Mode, freqKHz,
		q.QsoDetails.QsoDate, q.QsoDetails.TimeOn, q.QsoDetails.TimeOff,
		q.QsoDetails.RstSent, q.QsoDetails.RstRcvd, q.ContactedStation.Country,
		q.DedupeKey, q.LogbookID)
	if err != nil {
		t.Fatalf("raw v1 qso insert: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("raw v1 qso insert id: %v", err)
	}
	return id
}

func validTestQso(logbookID int64, call, band, mode, qsoDate, timeOn string) types.Qso {
	q := types.Qso{LogbookID: logbookID, UUID: utils.NewUUIDv7()}
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

func TestMigrate_RelaxesRSTLengthFromVersion1(t *testing.T) {
	svc := testServiceWithoutMigrations(t)
	applyMigrationSteps(t, svc, 1)

	logbookID, err := svc.InsertLogbookWithContext(context.Background(), types.Logbook{
		Name:     "Test",
		Callsign: "G4ABC",
	})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	before := validTestQso(logbookID, "M0CMC", "20m", "SSB", "20240615", "1253")
	beforeID := insertQsoRawV1(t, svc, before)
	enqueueUploadPreV7(t, svc, beforeID, "qrz-main", "qrz", action.Insert)
	insertQsoHistory(t, svc, before.UUID, action.Update, source.API, []byte(`{"call":"M0CMC"}`))

	if err := svc.Migrate(); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	after := validTestQso(logbookID, "SP5VYF", "15m", "SSB", "20240722", "1637")
	after.QsoDetails.RstSent = "5759"
	after.QsoDetails.RstRcvd = "4657"
	if _, err := svc.InsertQsoWithContext(context.Background(), after); err != nil {
		t.Fatalf("insert qso with wide RST values after migration: %v", err)
	}

	qsos, err := svc.FetchQsoSliceByLogbookIdWithContext(context.Background(), logbookID)
	if err != nil {
		t.Fatalf("fetch qsos: %v", err)
	}
	if len(qsos) != 2 {
		t.Fatalf("QSO count = %d, want 2", len(qsos))
	}
	uploads, err := svc.FetchUploadsByQsoIDWithContext(context.Background(), beforeID)
	if err != nil {
		t.Fatalf("fetch preserved uploads: %v", err)
	}
	if len(uploads) != 1 {
		t.Fatalf("preserved upload count = %d, want 1", len(uploads))
	}
	history, err := svc.FetchQsoHistoryByUUIDWithContext(context.Background(), before.UUID)
	if err != nil {
		t.Fatalf("fetch preserved history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("preserved history count = %d, want 1", len(history))
	}

	var foundBefore, foundAfter bool
	for _, q := range qsos {
		switch q.ContactedStation.Call {
		case "M0CMC":
			foundBefore = true
			if q.QsoDetails.RstRcvd != "59" {
				t.Fatalf("pre-migration QSO RstRcvd = %q, want 59", q.QsoDetails.RstRcvd)
			}
		case "SP5VYF":
			foundAfter = true
			if q.QsoDetails.RstSent != "5759" || q.QsoDetails.RstRcvd != "4657" {
				t.Fatalf("wide RST values = sent %q rcvd %q, want 5759/4657",
					q.QsoDetails.RstSent, q.QsoDetails.RstRcvd)
			}
		}
	}
	if !foundBefore || !foundAfter {
		t.Fatalf("expected both pre- and post-migration QSOs, found before=%v after=%v", foundBefore, foundAfter)
	}
}

func TestMigrate_DownRestoresRSTLengthConstraint(t *testing.T) {
	svc := testServiceWithoutMigrations(t)
	if err := svc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}

	logbookID, err := svc.InsertLogbookWithContext(context.Background(), types.Logbook{
		Name:     "Test",
		Callsign: "G4ABC",
	})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	qso := validTestQso(logbookID, "M0CMC", "20m", "SSB", "20240615", "1253")
	qsoID, err := svc.InsertQsoWithContext(context.Background(), qso)
	if err != nil {
		t.Fatalf("insert latest-schema qso: %v", err)
	}
	enqueueUpload(t, svc, qsoID, "qrz-main", "qrz", action.Insert)
	insertQsoHistory(t, svc, qso.UUID, action.Update, source.API, []byte(`{"call":"M0CMC"}`))

	// Down to version 1: past 0002 (relax_rst_length), so the strict pre-0002 RST
	// constraint is restored. Named as an absolute version rather than a step
	// count — the count used to need bumping with every new migration, which its
	// own comment asked for and 0007 duly broke (2026-08-01).
	migrateToVersion(t, svc, 1)

	uploads, err := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch preserved uploads after down migration: %v", err)
	}
	if len(uploads) != 1 {
		t.Fatalf("preserved upload count after down migration = %d, want 1", len(uploads))
	}
	history, err := svc.FetchQsoHistoryByUUIDWithContext(context.Background(), qso.UUID)
	if err != nil {
		t.Fatalf("fetch preserved history after down migration: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("preserved history count after down migration = %d, want 1", len(history))
	}

	wide := validTestQso(logbookID, "SP5VYF", "15m", "SSB", "20240722", "1637")
	wide.QsoDetails.RstRcvd = "4657"
	if _, err := svc.InsertQsoWithContext(context.Background(), wide); err == nil {
		t.Fatal("insert with four-character RST_RCVD succeeded after down migration; want old constraint restored")
	}
}

// TestMigrate_AllowsHHMMSSTime: after 0003 the time_on/time_off CHECK accepts
// HHMMSS (6-digit) as well as HHMM, and stores it verbatim; an invalid seconds
// value is still rejected.
func TestMigrate_AllowsHHMMSSTime(t *testing.T) {
	svc := testServiceWithoutMigrations(t)
	if err := svc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	logbookID, err := svc.InsertLogbookWithContext(context.Background(), types.Logbook{
		Name: "Test", Callsign: "G4ABC",
	})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	// HHMMSS inserts cleanly and round-trips with seconds intact.
	withSecs := validTestQso(logbookID, "K1ABC", "20m", "FT8", "20260101", "084500")
	secsID, err := svc.InsertQsoWithContext(context.Background(), withSecs)
	if err != nil {
		t.Fatalf("insert HHMMSS qso: %v", err)
	}
	got, err := svc.FetchQsoByIdWithContext(context.Background(), secsID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.QsoDetails.TimeOn != "084500" || got.QsoDetails.TimeOff != "084500" {
		t.Fatalf("stored time = on %q off %q, want 084500 (seconds preserved)",
			got.QsoDetails.TimeOn, got.QsoDetails.TimeOff)
	}

	// HHMM still accepted.
	hhmm := validTestQso(logbookID, "K2BBB", "20m", "FT8", "20260101", "0846")
	if _, err := svc.InsertQsoWithContext(context.Background(), hhmm); err != nil {
		t.Fatalf("insert HHMM qso: %v", err)
	}

	// An invalid seconds field (SS=60) must still be rejected by the CHECK.
	badSecs := validTestQso(logbookID, "K3CCC", "20m", "FT8", "20260101", "084560")
	if _, err := svc.InsertQsoWithContext(context.Background(), badSecs); err == nil {
		t.Fatal("insert with SS=60 succeeded; want CHECK rejection")
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

// ---- Upload queue (qso_upload) ----

// enqueueUpload is a test-only helper that inserts one qso_upload row via
// the transactional API. Wraps BeginTx/InsertQsoUploadTx/Commit so the
// upload-methods tests can set up fixtures without repeating the dance.
// enqueueUpload inserts a qso_upload row through the LIVE write path, i.e. the
// current schema. Most callers want this.
func enqueueUpload(t *testing.T, svc *Service, qsoID int64, name, typ string, act action.Action) {
	t.Helper()
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	if err = svc.InsertQsoUploadTx(ctx, tx, qsoID, act, name, typ, origin.Live); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert upload: %v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// enqueueUploadPreV7 inserts a qso_upload row with raw SQL shaped for the schema
// BEFORE migration 0007 added `origin`.
//
// A version-frozen writer, for the same reason insertQsoRawV1 above is one:
// migration CHARACTERIZATION tests stand up an OLD schema and then migrate it, so
// they must write what that old schema accepts. The live writer always speaks the
// CURRENT schema — which is precisely what those tests are not on (Diff B,
// 2026-08-01).
func enqueueUploadPreV7(t *testing.T, svc *Service, qsoID int64, name, typ string, act action.Action) {
	t.Helper()
	if _, err := svc.handle.Exec(`
		INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status)
		VALUES (?, ?, ?, ?, 'pending')`, qsoID, name, typ, act.String()); err != nil {
		t.Fatalf("insert upload (pre-0007 shape): %v", err)
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

// When several lifecycle rows for one QSO are pending with the SAME
// next_attempt_at, the claim must return them in applied order
// insert→update→delete — not the same-second nondeterministic order the bare
// next_attempt_at key gave (review 2026-06-05 M2(b)).
func TestClaimPendingUploads_OrdersLifecycleRowsInsertUpdateDelete(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// Enqueue in reverse (delete, update, insert) so rowid order is the
	// opposite of the expected claim order — proves we sort on action, not id.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Delete)
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Update)
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	// Pin all three to the same next_attempt_at so ordering is decided purely
	// by the action-priority tiebreak (deterministic — no second-boundary race).
	rawExec(t, svc, `UPDATE qso_upload SET next_attempt_at = ? WHERE qso_id = ?`, int64(1000), qsoID)

	claimed, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 5)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 3 {
		t.Fatalf("claimed %d rows, want 3", len(claimed))
	}
	got := []string{claimed[0].Action, claimed[1].Action, claimed[2].Action}
	want := []string{action.Insert.String(), action.Update.String(), action.Delete.String()}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("claim order = %v, want %v", got, want)
		}
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

// TestMarkUploadSuccess_StaleAfterReArm_NoOps defends the stale-completion race
// (code review #1): a worker claims a row (in_progress), an operator edit
// re-arms the SAME (qso, forwarder, action) row to pending mid-send, and the
// stale worker then tries to mark it uploaded. The completion must NO-OP so the
// re-armed row survives to be re-forwarded with the latest state.
func TestMarkUploadSuccess_StaleAfterReArm_NoOps(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	ctx := context.Background()

	// Worker claims the row → in_progress.
	claimed, _ := svc.ClaimPendingUploadsWithContext(ctx, "qrz", 1)
	rowID := claimed[0].ID

	// Operator edit re-arms the same triple → upsert resets it to pending,
	// attempts=0 (InsertQsoUploadTx's ON CONFLICT path).
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	// Stale worker completes — must not clobber the re-armed row, AND must
	// signal the re-arm (ErrUploadReArmed) so the worker skips the terminal
	// event + stamp hook (review 2026-07-20 internal/forwarding #4).
	if err := svc.MarkUploadSuccessWithContext(ctx, rowID, "logid-stale"); !stderr.Is(err, errors.ErrUploadReArmed) {
		t.Fatalf("mark success: want ErrUploadReArmed, got %v", err)
	}

	uploads, _ := svc.FetchUploadsByQsoIDWithContext(ctx, qsoID)
	got := uploads[0]
	if got.Status != "pending" {
		t.Fatalf("Status = %q, want pending (re-arm must survive the stale completion)", got.Status)
	}
	if got.UpstreamID == "logid-stale" {
		t.Fatal("stale upstream_id leaked onto the re-armed row")
	}
	if got.Attempts != 0 {
		t.Fatalf("Attempts = %d, want 0 (re-arm reset it; the stale completion must not bump it)", got.Attempts)
	}
}

// TestFetchCountry_PreservesID_AllowsUpdate defends code review #2: the
// Country adapter must copy the row's primary key, or a fetched country comes
// back with ID==0 and UpdateCountryWithContext (which rejects ID<1) can never
// update it.
func TestFetchCountry_PreservesID_AllowsUpdate(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	c := types.Country{
		Name: "Malawi", Prefix: "7Q", Ccode: "0", Continent: "AF",
		CQZone: "37", ITUZone: "53", DXCCPrefix: "7Q", TimeOffset: "+2",
	}
	if _, err := svc.InsertCountryWithContext(ctx, c); err != nil {
		t.Fatalf("insert country: %v", err)
	}

	got, err := svc.FetchCountryByPrefixWithContext(ctx, "7Q")
	if err != nil {
		t.Fatalf("fetch country: %v", err)
	}
	if got.ID < 1 {
		t.Fatalf("fetched country ID = %d, want >= 1 (adapter dropped the PK)", got.ID)
	}

	got.Name = "Republic of Malawi"
	if err := svc.UpdateCountryWithContext(ctx, got); err != nil {
		t.Fatalf("update fetched country (ID=%d): %v", got.ID, err)
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

// TestDiscardQueuedUploadsForForwarder defends ADR 0039's startup
// reconciliation: a disabled forwarder's not-yet-uploaded rows (pending /
// in_progress / failed) are deleted, its 'uploaded' rows are KEPT (upstream_id
// provenance), and other forwarders are untouched.
// claimOneAndMark claims the single currently-pending row for fwd (asserting
// exactly one) and applies mark to it — the realistic worker flow, which the
// completion methods' in_progress guard now requires.
func claimOneAndMark(t *testing.T, svc *Service, fwd string, mark func(id int64) error) {
	t.Helper()
	claimed, err := svc.ClaimPendingUploadsWithContext(context.Background(), fwd, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claim: got %d pending rows, want exactly 1", len(claimed))
	}
	if err := mark(claimed[0].ID); err != nil {
		t.Fatalf("mark: %v", err)
	}
}

func TestDiscardQueuedUploadsForForwarder(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	ctx := context.Background()

	// qrz uploaded — claim then mark success; MUST survive (carries upstream_id).
	// Done first so it's the only pending qrz row at claim time.
	qUploaded, _ := svc.InsertQso(validTestQso(lbID, "G3XYZ", "20m", "SSB", "20250508", "0900"))
	enqueueUpload(t, svc, qUploaded, "qrz", "qrz", action.Insert)
	claimOneAndMark(t, svc, "qrz", func(id int64) error {
		return svc.MarkUploadSuccessWithContext(ctx, id, "logid-1")
	})

	// qrz failed — claim then mark failed; should be discarded.
	qFailed, _ := svc.InsertQso(validTestQso(lbID, "EA1B", "10m", "SSB", "20250508", "0930"))
	enqueueUpload(t, svc, qFailed, "qrz", "qrz", action.Insert)
	claimOneAndMark(t, svc, "qrz", func(id int64) error {
		return svc.MarkUploadFailedWithContext(ctx, id, "bad data")
	})

	// qrz pending — left pending; should be discarded.
	qPending, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qPending, "qrz", "qrz", action.Insert)

	// clublog pending — different forwarder, MUST be untouched.
	qOther, _ := svc.InsertQso(validTestQso(lbID, "F5ABC", "15m", "SSB", "20250508", "0915"))
	enqueueUpload(t, svc, qOther, "clublog", "clublog", action.Insert)

	discarded, err := svc.DiscardQueuedUploadsForForwarderWithContext(ctx, "qrz")
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if discarded != 2 {
		t.Fatalf("discarded = %d, want 2 (qrz pending + failed)", discarded)
	}

	if rows, _ := svc.FetchUploadsByQsoIDWithContext(ctx, qPending); len(rows) != 0 {
		t.Errorf("qrz pending row not discarded: %d remain", len(rows))
	}
	if rows, _ := svc.FetchUploadsByQsoIDWithContext(ctx, qFailed); len(rows) != 0 {
		t.Errorf("qrz failed row not discarded: %d remain", len(rows))
	}
	if rows, _ := svc.FetchUploadsByQsoIDWithContext(ctx, qUploaded); len(rows) != 1 || rows[0].Status != "uploaded" {
		t.Errorf("qrz uploaded row should survive, got %+v", rows)
	}
	if rows, _ := svc.FetchUploadsByQsoIDWithContext(ctx, qOther); len(rows) != 1 {
		t.Errorf("clublog row should be untouched, got %d rows", len(rows))
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

// ---- FetchPriorUpstreamID (stage 5: delete-action LOGID resolution) ----

func TestFetchPriorUpstreamID_NoRows_ReturnsEmpty(t *testing.T) {
	svc := testService(t)

	got, err := svc.FetchPriorUpstreamIDWithContext(context.Background(), 999, "qrz")
	if err != nil {
		t.Fatalf("fetch on empty: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty string", got)
	}
}

func TestFetchPriorUpstreamID_HappyPath(t *testing.T) {
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

	got, err := svc.FetchPriorUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "1440100000" {
		t.Fatalf("got %q, want 1440100000", got)
	}
}

func TestFetchPriorUpstreamID_IgnoresOtherForwarders(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// Successful insert on "clublog" must not leak into a "qrz" query.
	enqueueUpload(t, svc, qsoID, "clublog", "clublog", action.Insert)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "clublog", 1)
	_ = svc.MarkUploadSuccessWithContext(context.Background(), claimed[0].ID, "cl-999")

	got, err := svc.FetchPriorUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty (clublog upstream_id must not match qrz query)", got)
	}
}

// A successful UPDATE (ACTION=INSERT&OPTION=REPLACE) creates/owns the upstream
// record and returns its id, so its upstream_id MUST satisfy a later delete's
// lookup — otherwise a delete after an out-of-order update can't remove the
// record the update created (review 2026-06-05 M2(a)). This inverts the old
// "ignore non-insert actions" behaviour.
func TestFetchPriorUpstreamID_ConsidersUpdateAction(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// A successful update with an upstream_id (and NO insert row) must be found.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Update)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	_ = svc.MarkUploadSuccessWithContext(context.Background(), claimed[0].ID, "update-upstream")

	got, err := svc.FetchPriorUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "update-upstream" {
		t.Fatalf("got %q, want update-upstream (update is an upstream-creating action)", got)
	}
}

// TestFetchPriorUpstreamID_OrdersBySuccessFreshnessNotCreation is the M1
// regression (review 2026-06-19): the lookup must pick the row whose SUCCESS is
// most recent (modified_at), not the most recently CREATED row (created_at). The
// re-arm case: an insert row created first, then re-armed and re-uploaded AFTER
// a later-created update row succeeded — the insert holds the live upstream id
// despite its older created_at. Timestamps are pinned via raw SQL because
// DATETIME is second-resolution and a fast test would otherwise tie.
func TestFetchPriorUpstreamID_OrdersBySuccessFreshnessNotCreation(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	ins, _ := svc.ClaimPendingUploadsWithContext(ctx, "qrz", 1)
	if err := svc.MarkUploadSuccessWithContext(ctx, ins[0].ID, "insert-id-rearmed"); err != nil {
		t.Fatalf("mark insert: %v", err)
	}
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Update)
	upd, _ := svc.ClaimPendingUploadsWithContext(ctx, "qrz", 1)
	if err := svc.MarkUploadSuccessWithContext(ctx, upd[0].ID, "update-id-stale"); err != nil {
		t.Fatalf("mark update: %v", err)
	}

	// Encode the re-arm: the UPDATE row was CREATED later, but the INSERT row was
	// re-uploaded (modified) later — so it owns the current upstream id. Drop the
	// auto-touch trigger first, else it stamps modified_at = now on our UPDATEs
	// and both rows tie at the test-run second.
	if _, e := svc.handle.Exec(`DROP TRIGGER IF EXISTS trg_qso_upload_set_updated_at`); e != nil {
		t.Fatalf("drop trigger: %v", e)
	}
	if _, e := svc.handle.Exec(
		`UPDATE qso_upload SET created_at='2026-01-01 00:00:01', modified_at='2026-01-01 00:00:09' WHERE id=?`, ins[0].ID); e != nil {
		t.Fatalf("pin insert ts: %v", e)
	}
	if _, e := svc.handle.Exec(
		`UPDATE qso_upload SET created_at='2026-01-01 00:00:05', modified_at='2026-01-01 00:00:03' WHERE id=?`, upd[0].ID); e != nil {
		t.Fatalf("pin update ts: %v", e)
	}

	got, err := svc.FetchPriorUpstreamIDWithContext(ctx, qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "insert-id-rearmed" {
		t.Fatalf("got %q, want insert-id-rearmed — lookup must order by success freshness (modified_at), "+
			"not creation (created_at); under created_at ordering the stale later-created update row wins", got)
	}
}

// A successful DELETE row must NOT satisfy the lookup — a delete removes the
// upstream record, it doesn't create one. Only insert/update count.
func TestFetchPriorUpstreamID_IgnoresDeleteAction(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// Force a delete row to status=uploaded with a (contrived) upstream_id and
	// confirm the lookup still returns empty — only insert/update count.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Delete)
	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	_ = svc.MarkUploadSuccessWithContext(context.Background(), claimed[0].ID, "delete-upstream")

	got, err := svc.FetchPriorUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty (a delete row must not satisfy the lookup)", got)
	}
}

func TestFetchPriorUpstreamID_IgnoresPendingAndFailed(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// A pending insert row shouldn't match — no upstream_id yet.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	got, err := svc.FetchPriorUpstreamIDWithContext(context.Background(), qsoID, "qrz")
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

	got, err = svc.FetchPriorUpstreamIDWithContext(context.Background(), qsoID, "qrz")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty (failed row must not match)", got)
	}
}

// InsertQsoUploadTx UPSERTs on conflict over (qso_id, forwarder_name,
// action) — a second enqueue for the same triple does not violate the
// UNIQUE constraint and does not duplicate the row. Instead, it re-arms
// the existing row to status='pending' with cleared retry state so the
// worker re-attempts the latest operator intent. The previous behaviour
// (constraint violation bubbling out of the transaction) was the C1
// bug from the 2026-05-02 daemon code review — every second PATCH or
// DELETE on the same QSO turned into a 500.
func TestInsertQsoUploadTx_ReArmOnConflict(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))

	// First enqueue creates the row.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	// Simulate a successful upload via the realistic flow: claim (→ in_progress,
	// which the completion guard requires) then mark uploaded with an upstream_id.
	claimed, err := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claim len = %d, want 1", len(claimed))
	}
	uploadID := claimed[0].ID
	if err = svc.MarkUploadSuccessWithContext(context.Background(), uploadID, "qrz-12345"); err != nil {
		t.Fatalf("mark uploaded: %v", err)
	}

	// Second enqueue for the same (qso, forwarder, insert) must succeed
	// (no constraint violation) and re-arm the existing row.
	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	if err = svc.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, "qrz", "qrz", origin.Live); err != nil {
		t.Fatalf("re-enqueue: %v (want nil — UPSERT should re-arm, not fail)", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Verify the row count is still 1 (not duplicated) and that re-arm
	// reset the retry state.
	rows, err := svc.FetchUploadsByQsoIDWithContext(ctx, qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1 (UPSERT must not duplicate)", len(rows))
	}
	r := rows[0]
	if r.Status != "pending" {
		t.Errorf("status = %q, want pending after re-arm", r.Status)
	}
	if r.Attempts != 0 {
		t.Errorf("attempts = %d, want 0 after re-arm", r.Attempts)
	}
	// upstream_id is preserved across re-arm — see api_context.go's
	// InsertQsoUploadTx comment for why (FetchPriorUpstreamID needs it
	// for the delete-after-insert flow).
	if r.UpstreamID != "qrz-12345" {
		t.Errorf("upstream_id = %q, want qrz-12345 (must be preserved across re-arm)", r.UpstreamID)
	}
}

func TestFetchPriorUpstreamID_InvalidInputs(t *testing.T) {
	svc := testService(t)

	if _, err := svc.FetchPriorUpstreamIDWithContext(context.Background(), 0, "qrz"); err == nil {
		t.Fatal("expected error for qsoID=0")
	}
	if _, err := svc.FetchPriorUpstreamIDWithContext(context.Background(), 1, ""); err == nil {
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
