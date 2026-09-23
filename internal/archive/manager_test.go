package archive

import (
	"context"
	"database/sql"
	stderr "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The managed provisioner (ADR 0071, W-0021 slice 2C): a new archive is built
// as a `.creating` file in the managed directory (log migration set only), given
// its identity and initial logbook, validated, closed to a single file, set to
// ST-6 modes, renamed into place, and only THEN added to the catalogue —
// inactive. A failure before the catalogue write leaves the active archive
// untouched and no selectable half-built archive; the same request key returns
// the same archive; `.creating` leftovers are diagnosed, never listed.

func testManager(t *testing.T) (*Manager, *config.Service, *strings.Builder) {
	t.Helper()
	// config.json lives in its OWN directory, apart from data_dir, so a test can
	// make only the catalogue write fail while the managed directory stays
	// writable (a step-5 failure, not a step-2 one).
	tmp := t.TempDir()
	cfg := config.DefaultConfig(filepath.Join(tmp, "data"))
	cfg.Logging.FileLogging = false
	cfgSvc := config.New(cfg)
	if err := os.MkdirAll(filepath.Join(tmp, "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(tmp, "cfg", "config.json")
	if _, err := config.WriteJSON(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	cfgSvc.SetPath(cfgPath)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	buf := &strings.Builder{}
	logger := logging.NewForWriter(buf)
	valid := func(c string) bool { return len(c) >= 3 && strings.ContainsAny(c, "0123456789") }
	return NewManager(cfgSvc, logger, valid), cfgSvc, buf
}

func openManaged(t *testing.T, cfg config.Config, path string) *sqlite.Service {
	t.Helper()
	c := cfg
	c.Datastore.Path = path
	c.Logging.FileLogging = false
	cfgSvc := config.New(c)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	logSvc := &logging.Service{ConfigService: cfgSvc, WorkingDir: cfgSvc.WorkingDir()}
	if err := logSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	db := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logSvc}
	if err := db.Initialize(); err != nil {
		t.Fatal(err)
	}
	db.SetMigrationSets(sqlite.MigrationSetLog)
	if err := db.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); _ = logSvc.Close() })
	return db
}

func TestCreate_ProvisionsAManagedArchiveInactive(t *testing.T) {
	m, cfgSvc, buf := testManager(t)
	before := cfgSvc.Snapshot()
	res, err := m.Create(context.Background(), CreateRequest{
		RequestKey: "req-1", Label: "Field Day 2026", LogbookName: "FD", LogbookCallsign: "g4abc",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !utils.IsValidUUIDv7(res.Entry.ID) || res.Entry.Ownership != types.QsoArchiveOwnershipManaged || res.Entry.Path != "" || res.Entry.Label != "Field Day 2026" {
		t.Fatalf("entry = %+v; want a managed, path-less, labelled entry with a UUIDv7", res.Entry)
	}
	snap := cfgSvc.Snapshot()
	wantPath := filepath.Join(ManagedDir(snap), res.Entry.ID+".db")
	if res.Path != wantPath {
		t.Fatalf("path = %q, want %q", res.Path, wantPath)
	}
	// ST-6: a single 0600 file in a 0700 directory, no sidecars, no .creating.
	fi, err := os.Stat(res.Path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v mode %v; want 0600", err, fi)
	}
	if di, _ := os.Stat(filepath.Dir(res.Path)); di.Mode().Perm() != 0o700 {
		t.Fatalf("managed dir mode = %v, want 0700", di.Mode().Perm())
	}
	for _, side := range []string{res.Path + "-wal", res.Path + "-shm", res.Path + ".creating"} {
		if _, err := os.Stat(side); err == nil {
			t.Fatalf("%s left behind", filepath.Base(side))
		}
	}
	// The file carries the catalogue's id, the initial logbook and its default —
	// and the LOG schema only: no reference tables, no reference migration tracking.
	db := openManaged(t, snap, res.Path)
	for _, absent := range []string{"country", "contacted_station", "schema_migrations_reference"} {
		if hasTableIn(t, res.Path, absent) {
			t.Fatalf("provisioned archive carries %q; only the log migration set may run", absent)
		}
	}
	lbs, _ := db.FetchAllLogbooksWithContext(context.Background())
	if len(lbs) != 1 || lbs[0].Name != "FD" || lbs[0].Callsign != "G4ABC" || !utils.IsValidUUIDv7(lbs[0].UUID) {
		t.Fatalf("logbooks = %+v; want one, FD / G4ABC, with a UUIDv7", lbs)
	}
	identity, err := db.ArchiveIdentityWithContext(context.Background())
	if err != nil || identity.ArchiveUUID != res.Entry.ID || identity.DefaultLogbookID != lbs[0].ID {
		t.Fatalf("identity = %+v (%v); want id %s default %d", identity, err, res.Entry.ID, lbs[0].ID)
	}
	// Catalogue: the entry is present and INACTIVE; the active selector is untouched.
	if e := snap.QsoArchiveByID(res.Entry.ID); e == nil || *e != res.Entry {
		t.Fatalf("catalogue entry = %+v, want %+v", e, res.Entry)
	}
	if snap.ActiveQsoArchiveID != before.ActiveQsoArchiveID || snap.PendingQsoArchiveID != "" {
		t.Fatalf("selection changed: active %q pending %q", snap.ActiveQsoArchiveID, snap.PendingQsoArchiveID)
	}
	// Persisted.
	disk, err := config.Load(cfgSvc.Path)
	if err != nil || disk.QsoArchiveByID(res.Entry.ID) == nil {
		t.Fatalf("entry not on disk: %v", err)
	}
	if !strings.Contains(buf.String(), res.Entry.ID) {
		t.Fatal("creation not logged with the archive id")
	}
}

func TestCreate_SameRequestKeyReturnsTheSameArchive(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	req := CreateRequest{RequestKey: "req-2", Label: "Contest", LogbookName: "C", LogbookCallsign: "G4ABC"}
	first, err := m.Create(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Create(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	want := first
	want.Reused = true
	if again != want {
		t.Fatalf("retry = %+v; want the complete first result marked reused %+v", again, want)
	}
	// Durable (review): the association lives in the catalogue entry, so a NEW
	// manager — a restarted daemon, a crash after the catalogue commit — returns
	// the same archive for the same key instead of creating a second one.
	fresh := NewManager(cfgSvc, m.logger, m.validCallsign)
	afterRestart, err := fresh.Create(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart != want {
		t.Fatalf("retry through a new manager = %+v; want %+v", afterRestart, want)
	}
	if e := cfgSvc.Snapshot().QsoArchiveByID(first.Entry.ID); e == nil || e.RequestKey != "req-2" {
		t.Fatalf("catalogue entry does not carry the request key: %+v", e)
	}
	if n := len(cfgSvc.Snapshot().QsoArchives); n != 1 {
		t.Fatalf("catalogue has %d entries after a retried request, want 1", n)
	}
	other, err := m.Create(context.Background(), CreateRequest{RequestKey: "req-3", Label: "Contest", LogbookName: "C", LogbookCallsign: "G4ABC"})
	if err != nil || other.Entry.ID == first.Entry.ID {
		t.Fatalf("a different key must create a different archive: %+v (%v)", other, err)
	}
}

func TestCreate_Validation(t *testing.T) {
	m, _, _ := testManager(t)
	cases := map[string]CreateRequest{
		"empty label":    {RequestKey: "k", Label: " ", LogbookName: "L", LogbookCallsign: "G4ABC"},
		"empty logbook":  {RequestKey: "k", Label: "L", LogbookName: "", LogbookCallsign: "G4ABC"},
		"bad callsign":   {RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "NODIGITS"},
		"empty key":      {RequestKey: "", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"},
		"label too long": {RequestKey: "k", Label: strings.Repeat("x", 65), LogbookName: "L", LogbookCallsign: "G4ABC"},
	}
	for name, req := range cases {
		_, err := m.Create(context.Background(), req)
		var re *RequestError
		if err == nil || !stderr.As(err, &re) {
			t.Errorf("%s: err = %v, want a RequestError", name, err)
		}
	}
	if n := len(m.cfg.Snapshot().QsoArchives); n != 0 {
		t.Fatalf("refused requests left %d catalogue entries", n)
	}
}

// Step 5 failing (the catalogue cannot be persisted) leaves no file in place and
// no entry — the active archive is untouched and nothing half-built is selectable.
func TestCreate_CataloguePersistFailureLeavesNothingBehind(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	dir := filepath.Dir(cfgSvc.Path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := m.Create(context.Background(), CreateRequest{RequestKey: "req-4", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err == nil {
		t.Fatal("create succeeded although the catalogue could not be written")
	}
	_ = os.Chmod(dir, 0o700)
	entries, _ := os.ReadDir(ManagedDir(cfgSvc.Snapshot()))
	if len(entries) != 0 {
		t.Fatalf("managed dir holds %d file(s) after a failed create: %v", len(entries), entries)
	}
	if n := len(cfgSvc.Snapshot().QsoArchives); n != 0 {
		t.Fatalf("catalogue has %d entries after a failed create", n)
	}
}

func TestDiagnoseCreatingArtefacts(t *testing.T) {
	m, cfgSvc, buf := testManager(t)
	dir := ManagedDir(cfgSvc.Snapshot())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, "019fd5c5-efcc-7193-be4f-1fee532ee399.db.creating")
	if err := os.WriteFile(stray, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	found := DiagnoseCreatingArtefacts(cfgSvc.Snapshot(), m.logger)
	if len(found) != 1 || found[0] != stray {
		t.Fatalf("diagnosed = %v, want [%s]", found, stray)
	}
	if !strings.Contains(buf.String(), filepath.Base(stray)) {
		t.Fatal("the artefact was not logged")
	}
	// It is not an archive: creation still works beside it and never lists it.
	if _, err := m.Create(context.Background(), CreateRequest{RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"}); err != nil {
		t.Fatalf("create beside a stray artefact: %v", err)
	}
	for _, e := range cfgSvc.Snapshot().QsoArchives {
		if strings.Contains(e.ID, "ee399") {
			t.Fatal("the artefact was listed as an archive")
		}
	}
}

func hasTableIn(t *testing.T, path, table string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// ST-6 on an EXISTING managed directory (review): a 0755 directory left by an
// older layout or an operator is tightened to 0700, and the file is 0600 from
// its first byte — SQLite creates it with the umask otherwise.
func TestCreate_TightensAnExistingManagedDirectory(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	dir := ManagedDir(cfgSvc.Snapshot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := m.Create(context.Background(), CreateRequest{RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatal(err)
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Fatalf("pre-existing managed dir mode = %v after create, want 0700", di.Mode().Perm())
	}
	if fi, _ := os.Stat(res.Path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", fi.Mode().Perm())
	}
}

// An unlisted final .db in the managed directory — a crash between placing the
// file and the catalogue write, or a cleanup that could not remove it — is
// diagnosed at start too, not only .creating files.
func TestDiagnoseCreatingArtefacts_ReportsUnlistedArchiveFiles(t *testing.T) {
	m, cfgSvc, buf := testManager(t)
	res, err := m.Create(context.Background(), CreateRequest{RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(ManagedDir(cfgSvc.Snapshot()), "019fd5c5-efcc-7193-be4f-1fee532ee398.db")
	if err := os.WriteFile(orphan, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	found := DiagnoseCreatingArtefacts(cfgSvc.Snapshot(), m.logger)
	if len(found) != 1 || found[0] != orphan {
		t.Fatalf("diagnosed = %v, want only the unlisted %s (the catalogued %s is fine)", found, orphan, res.Path)
	}
	if !strings.Contains(buf.String(), "not in the catalogue") {
		t.Fatal("the unlisted file was not logged as such")
	}
}

// ST-6 symlink rule (review): a managed directory that IS a symlink, or resolves
// through one to outside the working directory, is refused — nothing is chmod'd
// through it and nothing is written there.
func TestCreate_RefusesAManagedDirectoryThatEscapesThroughASymlink(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	snap := cfgSvc.Snapshot()
	outside := t.TempDir()
	if err := os.Chmod(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ManagedDir(snap)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, ManagedDir(snap)); err != nil {
		t.Fatal(err)
	}
	_, err := m.Create(context.Background(), CreateRequest{RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("create through a symlinked managed directory = %v; want a refusal naming the symlink", err)
	}
	if fi, _ := os.Stat(outside); fi.Mode().Perm() != 0o755 {
		t.Fatalf("the external target was chmod'd through the symlink: %v", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("files were written outside the working directory: %v", entries)
	}
	if n := len(cfgSvc.Snapshot().QsoArchives); n != 0 {
		t.Fatalf("catalogue gained %d entries", n)
	}
}

// A cleanup that cannot remove the failed creation's file is never silent: the
// removal error is folded into the returned error and logged.
func TestWithCleanup_ReportsAnUnremovableFile(t *testing.T) {
	m, _, buf := testManager(t)
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stuck := filepath.Join(dir, "x.db")
	if err := os.WriteFile(stuck, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // no unlink permission
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	cause := stderr.New("boom")
	err := m.withCleanup(cause, stuck)
	if err == nil || !stderr.Is(err, cause) || !strings.Contains(err.Error(), "could not be removed") {
		t.Fatalf("withCleanup = %v; want the cause wrapped with the removal failure", err)
	}
	if !strings.Contains(buf.String(), "could not remove") {
		t.Fatal("the removal failure was not logged")
	}
}

// A PARENT symlink (<data_dir>/db → outside) with no qso-archives yet: the
// containment check must run before MkdirAll, or the directory is created
// outside first and refused second. The external target stays empty.
func TestCreate_RefusesAParentSymlinkBeforeCreatingAnything(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	snap := cfgSvc.Snapshot()
	outside := t.TempDir()
	dbDir := filepath.Dir(ManagedDir(snap)) // <data_dir>/db
	if err := os.MkdirAll(filepath.Dir(dbDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dbDir); err != nil {
		t.Fatal(err)
	}
	_, err := m.Create(context.Background(), CreateRequest{RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("create through a symlinked parent = %v; want a refusal naming the symlink", err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("a directory was created outside the working directory before the refusal: %v", entries)
	}
}

// The pre-create branch itself (review): when closing the pre-created file
// fails AND the file cannot then be removed, the returned error carries both —
// the branch calls withCleanup, not a bare removal whose error is dropped.
func TestPrecreateArchiveFile_CloseFailureFoldsCleanup(t *testing.T) {
	m, _, buf := testManager(t)
	dir := filepath.Join(t.TempDir(), "d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	tmp := filepath.Join(dir, "x.db.creating")
	orig := closeFile
	t.Cleanup(func() { closeFile = orig })
	closeFile = func(f *os.File) error {
		_ = f.Close()
		_ = os.Chmod(dir, 0o500) // the file now cannot be removed either
		return stderr.New("close boom")
	}
	err := m.precreateArchiveFile(tmp)
	if err == nil || !strings.Contains(err.Error(), "close boom") || !strings.Contains(err.Error(), "could not be removed") {
		t.Fatalf("precreate = %v; want the close failure wrapped with the removal failure", err)
	}
	if !strings.Contains(buf.String(), "could not remove") {
		t.Fatal("the removal failure was not logged")
	}
}

// Review 8c8af208: the managed directory is synced after placement and BEFORE
// the catalogue names the archive, so a power failure cannot keep the entry and
// lose the file; and a durable retry whose file is gone is an error, never a
// "reused" success.
func TestCreate_SyncsTheDirectoryBeforeTheCatalogueAndChecksTheFileOnRetry(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	orig := syncDir
	t.Cleanup(func() { syncDir = orig })
	var syncedWhileUnlisted []string
	syncDir = func(p string) error {
		if err := orig(p); err != nil {
			return err
		}
		if len(cfgSvc.Snapshot().QsoArchives) == 0 { // not yet in the catalogue
			syncedWhileUnlisted = append(syncedWhileUnlisted, p)
		}
		return nil
	}
	res, err := m.Create(context.Background(), CreateRequest{RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatal(err)
	}
	// Every directory from the managed one up THROUGH the working directory
	// (review 16611884): db/ and qso-archives/ are both new in this fixture, and
	// data_dir's entry for db/ is only durable once data_dir itself is synced.
	dir := ManagedDir(cfgSvc.Snapshot())
	want := []string{dir, filepath.Dir(dir), cfgSvc.Snapshot().DataDir}
	if len(syncedWhileUnlisted) != len(want) {
		t.Fatalf("directories synced before the catalogue write = %v; want %v", syncedWhileUnlisted, want)
	}
	for i := range want {
		if syncedWhileUnlisted[i] != want[i] {
			t.Fatalf("directories synced before the catalogue write = %v; want %v", syncedWhileUnlisted, want)
		}
	}
	// The file vanishes (the crash the barrier guards against, or an operator
	// deletion): a retry with the same key must not claim success.
	if err := os.Remove(res.Path); err != nil {
		t.Fatal(err)
	}
	_, err = m.Create(context.Background(), CreateRequest{RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("retry with the file gone = %v; want an error naming the missing file", err)
	}
}

// A durable retry proves the FILE, not the path (review 16611884): a directory,
// arbitrary bytes, or another archive copied over the path is never "reused".
func TestCreate_RetryRequiresTheFilesIdentityToMatch(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	req := CreateRequest{RequestKey: "k", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"}
	res, err := m.Create(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	other, err := m.Create(context.Background(), CreateRequest{RequestKey: "k2", Label: "Other", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatal(err)
	}
	// Another archive's file copied over this path.
	data, err := os.ReadFile(other.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(res.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = m.Create(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), other.Entry.ID) || !strings.Contains(err.Error(), res.Entry.ID) {
		t.Fatalf("retry over another archive's file = %v; want a refusal naming both ids", err)
	}
	// Arbitrary bytes at the path.
	if err := os.WriteFile(res.Path, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), req); err == nil {
		t.Fatal("retry over arbitrary bytes claimed success")
	}
	if n := len(cfgSvc.Snapshot().QsoArchives); n != 2 {
		t.Fatalf("refused retries changed the catalogue: %d entries", n)
	}
}

// Review 8c8af208: a legacy (or external) entry may point INTO the managed
// directory; the diagnosis must never call a catalogued file removable.
func TestDiagnoseCreatingArtefacts_NeverReportsACataloguedLegacyFile(t *testing.T) {
	m, cfgSvc, buf := testManager(t)
	dir := ManagedDir(cfgSvc.Snapshot())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home.db")
	if err := os.WriteFile(home, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = append(c.QsoArchives, types.QsoArchiveConfig{ID: idA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: home})
		c.ActiveQsoArchiveID = idA
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if found := DiagnoseCreatingArtefacts(cfgSvc.Snapshot(), m.logger); len(found) != 0 {
		t.Fatalf("the catalogued legacy archive was reported as an artefact: %v", found)
	}
	if strings.Contains(buf.String(), "not in the catalogue") {
		t.Fatal("the live archive was logged as removable")
	}
}
