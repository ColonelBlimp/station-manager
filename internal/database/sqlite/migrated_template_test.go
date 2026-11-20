package sqlite

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// A migrated log database for tests that need a real file at the head and are
// not about migrating: the operating session's beds, which are built about
// ninety times and each ran all the log migrations (755 s for this package
// under the hosted race detector, 2026-11-20).
//
// The template is made once, by the real Service running the real migrations,
// and closed, so its main file is the whole database. It is then held as
// bytes: nothing can write to it. Each test gets those bytes as a file of its
// own, opened through the real Service, so no state is shared between tests.
//
// Tests of the migrations themselves, and anything that stops at an older
// version, do not use it: they run the migrations (testFileService,
// testFileServiceWithoutMigrations, testServiceAtVersion).

var (
	migratedTemplateOnce  sync.Once
	migratedTemplateBytes []byte
	migratedTemplateErr   error
)

// openFileServiceIn wires and opens a Service on dir/db/log.db, with the log
// migration set, without migrating. The caller closes both services.
func openFileServiceIn(dir string) (*Service, *logging.Service, string, error) {
	path := filepath.Join(dir, "db", "log.db")
	cfg := config.DefaultConfig(dir)
	cfg.Datastore.Path = path
	cfg.Logging.FileLogging = false
	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		return nil, nil, "", fmt.Errorf("config init: %w", err)
	}
	logSvc := &logging.Service{ConfigService: cfgSvc, WorkingDir: cfgSvc.WorkingDir()}
	if err := logSvc.Initialize(); err != nil {
		return nil, nil, "", fmt.Errorf("logging init: %w", err)
	}
	svc := &Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := svc.Initialize(); err != nil {
		_ = logSvc.Close()
		return nil, nil, "", fmt.Errorf("sqlite init: %w", err)
	}
	svc.SetMigrationSets(MigrationSetLog)
	if err := svc.Open(); err != nil {
		_ = logSvc.Close()
		return nil, nil, "", fmt.Errorf("sqlite open: %w", err)
	}
	return svc, logSvc, path, nil
}

// buildMigratedTemplate migrates a file to the head through the real Service,
// closes it, and keeps the main file's bytes. Its scratch directory is removed
// before it returns.
func buildMigratedTemplate() {
	dir, err := os.MkdirTemp("", "sm-sqlite-template-")
	if err != nil {
		migratedTemplateErr = fmt.Errorf("template scratch dir: %w", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	svc, logSvc, path, err := openFileServiceIn(dir)
	if err != nil {
		migratedTemplateErr = err
		return
	}
	defer func() { _ = logSvc.Close() }()
	if err := svc.Migrate(); err != nil {
		_ = svc.Close()
		migratedTemplateErr = fmt.Errorf("template migrate: %w", err)
		return
	}
	if err := svc.Close(); err != nil {
		migratedTemplateErr = fmt.Errorf("template close: %w", err)
		return
	}
	// A clean close of the last connection checkpoints and removes the WAL, so
	// the main file alone is the whole database. A template whose WAL survived
	// would be copied incomplete: refuse it.
	for _, side := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(side); err == nil {
			migratedTemplateErr = fmt.Errorf("template %s still present after close; the main file is not self-contained", filepath.Base(side))
			return
		}
	}
	if migratedTemplateBytes, err = os.ReadFile(path); err != nil {
		migratedTemplateErr = fmt.Errorf("read the template: %w", err)
	}
}

// testMigratedFileService is testFileService for a test that is not about
// migrating: a file of the test's own, at the head, opened through the real
// Service. The file is a copy of the template's bytes; no migration runs.
func testMigratedFileService(t *testing.T) (*Service, string) {
	t.Helper()
	migratedTemplateOnce.Do(buildMigratedTemplate)
	if migratedTemplateErr != nil {
		t.Fatalf("the migrated template is unavailable: %v", migratedTemplateErr)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "db", "log.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("make the database directory: %v", err)
	}
	if err := os.WriteFile(path, migratedTemplateBytes, 0o600); err != nil {
		t.Fatalf("write the test's copy of the template: %v", err)
	}
	svc, logSvc, opened, err := openFileServiceIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	if opened != path {
		_ = svc.Close()
		t.Fatalf("the service opened %s, not the copy at %s", opened, path)
	}
	t.Cleanup(func() { _ = svc.Close(); _ = logSvc.Close() })
	return svc, path
}

// The template is what the real migrations make: a copy of it and a file
// migrated here hold the same schema, at the same version, not dirty, and the
// copy holds no row in any table a session bed fills.
func TestMigratedTemplate_IsWhatTheMigrationsMake(t *testing.T) {
	const schema = `SELECT type, name, tbl_name, sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_stat%' ORDER BY type, name`
	copied, _ := testMigratedFileService(t)
	migrated, _ := testFileService(t)

	if v, head := schemaVersion(t, copied), logHead(t); v != head {
		t.Fatalf("the template is at version %d, the head is %d", v, head)
	}
	if n := countT(t, copied, `SELECT COUNT(*) FROM `+schemaMigrationsTable(MigrationSetLog)+` WHERE dirty = 1`); n != 0 {
		t.Fatal("the template is dirty")
	}
	a, b := dumpQuery(t, copied, schema), dumpQuery(t, migrated, schema)
	if len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Fatalf("the template's schema differs from a file migrated here:\n template %v\n migrated %v", a, b)
	}
	for _, table := range append([]string{"logbook", "qso", "qso_upload", "archive_metadata"}, operatingSessionTables...) {
		if n := countT(t, copied, `SELECT COUNT(*) FROM `+table); n != 0 {
			t.Errorf("the template holds %d row(s) in %s; a bed would start with them", n, table)
		}
	}
}

// Two beds are two files: what one stores, the other never sees, and the
// template's bytes are as they were. Each bed has its own archive identity.
func TestSessionBeds_ShareNothing(t *testing.T) {
	migratedTemplateOnce.Do(buildMigratedTemplate)
	before := sha256.Sum256(migratedTemplateBytes)

	first := newSessBed(t, true)
	st := first.open(lifeA, 0, SessionApplied)
	first.log(first.x, lifeA, 1)
	first.newSession(lifeA, st.Generation, 10, SessionApplied)
	first.exit(lifeB, 0, false, 11, SessionExitRecorded)
	if first.generation() == 0 || first.epoch() == 0 {
		t.Fatal("the first bed stored nothing; the comparison would prove nothing")
	}

	second := newSessBed(t, false)
	if first.svc == second.svc || first.svc.handle == second.svc.handle {
		t.Fatal("the two beds share a service or a connection pool")
	}
	for _, table := range append([]string{"qso", "qso_upload"}, operatingSessionTables...) {
		if n := second.count(`SELECT COUNT(*) FROM ` + table); n != 0 {
			t.Errorf("the second bed starts with %d row(s) in %s", n, table)
		}
	}
	if g, e := second.generation(), second.epoch(); g != 0 || e != 0 {
		t.Fatalf("the second bed starts at generation %d, epoch %d; want 0 and 0", g, e)
	}
	if n := second.count(`SELECT COUNT(*) FROM logbook`); n != 2 {
		t.Fatalf("the second bed holds %d logbooks, want its own two", n)
	}
	if a, b := first.text(`SELECT archive_uuid FROM archive_metadata`), second.text(`SELECT archive_uuid FROM archive_metadata`); a == b || a == "" {
		t.Fatalf("the two beds have archive identities %q and %q", a, b)
	}
	if first.xUUID == second.xUUID {
		t.Fatal("the two beds' logbooks share a uuid")
	}
	// The first bed is as it was after the second was made and read.
	if first.count(`SELECT COUNT(*) FROM operating_session`) == 0 || first.count(`SELECT COUNT(*) FROM operating_app_exit`) != 1 {
		t.Fatal("the first bed lost its rows")
	}
	if after := sha256.Sum256(migratedTemplateBytes); !bytes.Equal(before[:], after[:]) {
		t.Fatal("the template's bytes changed")
	}
}
