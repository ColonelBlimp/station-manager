package archive

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
//
// W-0021 Q7 commit 4 characterizes Create before provisioning shares its
// construction (operator direction 2026-10-19), against today's code:
//
//	publication   the file is placed and the directory chain synced while the
//	              archive is in neither catalogue (memory, disk) nor List, with
//	              no summary; a failed catalogue write leaves no summary, so the
//	              summary follows publication.
//	failures      construction, each directory sync, and the catalogue write
//	              each leave no file, sidecar or summary, the catalogue unchanged
//	              on disk and in memory, and the selection unchanged.
//	cleanup       a removal that fails keeps the original cause, names the file
//	              left behind (unlisted, not activatable, diagnosed at start),
//	              and still attempts every sidecar.
//	artefacts     identities, default logbook, log-only schema, modes, empty
//	              bindings with the seed decision, and a summary matching the
//	              file; no .creating, -wal or -shm before the file is reopened.
//	after it      a summary failure and DurabilityUncertain each keep the
//	              archive and return success, the latter logged as a caveat.
//
// config.fsOps stays private: the catalogue failure and DurabilityUncertain are
// driven through the config directory's permissions instead.

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
	// Before any reopen the managed directory holds exactly the final database:
	// no construction sidecar (<id>.db.creating-wal, -shm) either.
	wantOnlyFile(t, filepath.Dir(res.Path), res.Path)
	// The identity reads from the placed file without opening it read-write, as
	// a durable retry reads it.
	if peeked, found, err := sqlite.PeekArchiveIdentity(res.Path); err != nil || !found || peeked.ArchiveUUID != res.Entry.ID {
		t.Fatalf("peeked identity = %+v found %v (%v); want archive %s", peeked, found, err, res.Entry.ID)
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
	// ADR 0082 part 1: a new managed archive starts with NO destination bindings
	// and its seed decision already recorded, so no later start seeds it from
	// whatever config.json still carries.
	if at, err := db.DestinationBindingsSeededAtWithContext(context.Background()); err != nil || at == nil {
		t.Fatalf("seed marker = %v (%v); want set at provisioning", at, err)
	}
	if bindings, err := db.ListLogbookDestinationsWithContext(context.Background()); err != nil || len(bindings) != 0 {
		t.Fatalf("bindings = %v (%v); want none", bindings, err)
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
	// The summary describes this file: its one logbook as the file holds it, the
	// default, no QSOs, signed against the placed file.
	sums, miss := ReadSummaries(SummariesPath(snap))
	sum, ok := sums[res.Entry.ID]
	if miss != "" || !ok || len(sum.Logbooks) != 1 {
		t.Fatalf("summary = %+v (present %v, miss %q); want one logbook", sum, ok, miss)
	}
	if lb := sum.Logbooks[0]; lb.UUID != lbs[0].UUID || lb.Name != lbs[0].Name || lb.Callsign != lbs[0].Callsign || lb.QsoCount != 0 || !lb.Default {
		t.Fatalf("summary logbook = %+v; want the file's %+v, default, 0 QSOs", lb, lbs[0])
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
// No summary is recorded either: the summary follows publication.
func TestCreate_CataloguePersistFailureLeavesNothingBehind(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	seedHome(t, cfgSvc)
	before := catalogueNow(t, cfgSvc)
	dir := filepath.Dir(cfgSvc.Path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := m.Create(context.Background(), CreateRequest{RequestKey: "req-4", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err == nil || !strings.Contains(err.Error(), "record the archive in the catalogue") {
		t.Fatalf("create = %v; want the catalogue write's failure", err)
	}
	_ = os.Chmod(dir, 0o700)
	wantNothingLeft(t, m, cfgSvc, before)
}

// wantOnlyFile requires dir to hold exactly the file at path.
func wantOnlyFile(t *testing.T, dir, path string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 1 || filepath.Join(dir, entries[0].Name()) != path {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("%s holds %v; want only %s", dir, names, filepath.Base(path))
	}
}

// logRecord is the one JSON log record whose message is msg.
func logRecord(t *testing.T, log, msg string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if rec["message"] != msg {
			continue
		}
		if found != nil {
			t.Fatalf("two records say %q", msg)
		}
		found = rec
	}
	if found == nil {
		t.Fatalf("no record says %q in %s", msg, log)
	}
	return found
}

// seedHome makes a legacy Home archive active, so an unchanged selection is a
// real one.
func seedHome(t *testing.T, cfgSvc *config.Service) {
	t.Helper()
	data := cfgSvc.Snapshot().DataDir
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(data, "home.db")
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
}

// catalogueState is the catalogue and the selection, in memory and on disk.
type catalogueState struct {
	memory, disk            []types.QsoArchiveConfig
	active, pending         string
	diskActive, diskPending string
}

func catalogueNow(t *testing.T, cfgSvc *config.Service) catalogueState {
	t.Helper()
	snap := cfgSvc.Snapshot()
	disk, err := config.Load(cfgSvc.Path)
	if err != nil {
		t.Fatalf("read the catalogue on disk: %v", err)
	}
	return catalogueState{
		memory: snap.QsoArchives, disk: disk.QsoArchives,
		active: snap.ActiveQsoArchiveID, pending: snap.PendingQsoArchiveID,
		diskActive: disk.ActiveQsoArchiveID, diskPending: disk.PendingQsoArchiveID,
	}
}

func (c catalogueState) equal(o catalogueState) bool {
	return slices.Equal(c.memory, o.memory) && slices.Equal(c.disk, o.disk) &&
		c.active == o.active && c.pending == o.pending && c.diskActive == o.diskActive && c.diskPending == o.diskPending
}

// wantNothingLeft: a failed creation left no file, sidecar or summary, and the
// catalogue and selection, in memory and on disk, are as they were.
func wantNothingLeft(t *testing.T, m *Manager, cfgSvc *config.Service, before catalogueState) {
	t.Helper()
	if entries, _ := os.ReadDir(ManagedDir(cfgSvc.Snapshot())); len(entries) != 0 {
		t.Fatalf("the managed directory holds %v after a failed create; want nothing", entries)
	}
	if after := catalogueNow(t, cfgSvc); !after.equal(before) {
		t.Fatalf("catalogue after a failed create = %+v; want unchanged %+v", after, before)
	}
	if sums, _ := ReadSummaries(SummariesPath(cfgSvc.Snapshot())); len(sums) != 0 {
		t.Fatalf("summaries after a failed create = %v; want none", sums)
	}
	if listed := m.List(); len(listed) != len(before.memory) {
		t.Fatalf("List after a failed create = %+v; want only the catalogue as it was", listed)
	}
}

// Construction failing (here the logbook seed, under a cancelled context)
// removes the .creating file and its sidecars before anything is placed.
func TestCreate_ConstructionFailureLeavesNothingBehind(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	seedHome(t, cfgSvc)
	before := catalogueNow(t, cfgSvc)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Create(ctx, CreateRequest{RequestKey: "req-c", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	if err == nil || !strings.Contains(err.Error(), "provision: seed logbook") {
		t.Fatalf("create = %v; want the construction failure", err)
	}
	wantNothingLeft(t, m, cfgSvc, before)
}

// A directory sync failing anywhere in the chain removes the placed file before
// the catalogue is written.
func TestCreate_DirectorySyncFailureLeavesNothingBehind(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		t.Run("sync "+strconv.Itoa(failAt)+" of 3", func(t *testing.T) {
			m, cfgSvc, _ := testManager(t)
			seedHome(t, cfgSvc)
			before := catalogueNow(t, cfgSvc)
			orig := syncDir
			t.Cleanup(func() { syncDir = orig })
			calls := 0
			syncDir = func(p string) error {
				if calls++; calls == failAt {
					return stderr.New("sync boom")
				}
				return orig(p)
			}
			_, err := m.Create(context.Background(), CreateRequest{RequestKey: "req-s", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
			if err == nil || !strings.Contains(err.Error(), "sync boom") || strings.Contains(err.Error(), "could not be removed") {
				t.Fatalf("create = %v; want the sync failure, the file removed", err)
			}
			wantNothingLeft(t, m, cfgSvc, before)
		})
	}
}

// A cleanup that cannot remove the file keeps the failure's own cause, names
// the file left behind, and leaves it unlisted: it cannot be activated, and the
// next start diagnoses it.
func TestCreate_CleanupFailureKeepsTheCauseAndNamesTheLeftover(t *testing.T) {
	lock := func(t *testing.T, dir string) {
		t.Helper()
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name, cause string
		arrange     func(t *testing.T, cfgSvc *config.Service)
	}{
		{"construction", "provision", func(t *testing.T, cfgSvc *config.Service) {
			orig := closeFile
			t.Cleanup(func() { closeFile = orig })
			closeFile = func(f *os.File) error {
				lock(t, ManagedDir(cfgSvc.Snapshot())) // the build cannot write beside the file
				return orig(f)
			}
		}},
		{"directory sync", "sync boom", func(t *testing.T, cfgSvc *config.Service) {
			orig := syncDir
			t.Cleanup(func() { syncDir = orig })
			syncDir = func(string) error {
				lock(t, ManagedDir(cfgSvc.Snapshot()))
				return stderr.New("sync boom")
			}
		}},
		{"catalogue write", "record the archive in the catalogue", func(t *testing.T, cfgSvc *config.Service) {
			lock(t, filepath.Dir(cfgSvc.Path))
			orig := syncDir
			t.Cleanup(func() { syncDir = orig })
			syncDir = func(p string) error {
				if p == cfgSvc.Snapshot().DataDir { // the last sync, just before the catalogue
					lock(t, ManagedDir(cfgSvc.Snapshot()))
				}
				return orig(p)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, cfgSvc, buf := testManager(t)
			seedHome(t, cfgSvc)
			before := catalogueNow(t, cfgSvc)
			dir := ManagedDir(cfgSvc.Snapshot())
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700); _ = os.Chmod(filepath.Dir(cfgSvc.Path), 0o700) })
			c.arrange(t, cfgSvc)
			_, err := m.Create(context.Background(), CreateRequest{RequestKey: "req-x", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
			_ = os.Chmod(dir, 0o700)
			_ = os.Chmod(filepath.Dir(cfgSvc.Path), 0o700)
			if err == nil {
				t.Fatal("create succeeded")
			}
			msg := err.Error()
			i := strings.Index(msg, "; and the file could not be removed")
			if i < 0 || !strings.Contains(msg[:i], c.cause) {
				t.Fatalf("create = %q; want the cause %q first, then the removal failure", msg, c.cause)
			}
			left, _ := os.ReadDir(dir)
			if len(left) != 1 {
				t.Fatalf("the managed directory holds %v; want the one file the cleanup could not remove", left)
			}
			leftover := filepath.Join(dir, left[0].Name())
			if !strings.Contains(msg[i:], leftover) || !strings.Contains(buf.String(), leftover) {
				t.Fatalf("the leftover %s is not named in the error %q or the log", leftover, msg)
			}
			if after := catalogueNow(t, cfgSvc); !after.equal(before) {
				t.Fatalf("catalogue = %+v; want unchanged %+v", after, before)
			}
			id := strings.TrimSuffix(strings.TrimSuffix(left[0].Name(), creatingSuffix), ".db")
			var re *RequestError
			if _, err := m.Activate(context.Background(), id); !stderr.As(err, &re) || re.Code != "archive_not_found" {
				t.Fatalf("activating the leftover = %v; want archive_not_found", err)
			}
			if found := DiagnoseCreatingArtefacts(cfgSvc.Snapshot(), m.logger); len(found) != 1 || found[0] != leftover {
				t.Fatalf("diagnosed = %v; want the leftover %s", found, leftover)
			}
		})
	}
}

// Cleanup attempts every sidecar even when the file itself cannot be removed
// (here a non-empty directory at its path).
func TestWithCleanup_AttemptsEverySidecar(t *testing.T) {
	m, _, buf := testManager(t)
	path := filepath.Join(t.TempDir(), "x.db")
	if err := os.MkdirAll(filepath.Join(path, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, side := range []string{path + "-wal", path + "-shm"} {
		if err := os.WriteFile(side, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cause := stderr.New("boom")
	err := m.withCleanup(cause, path)
	if !stderr.Is(err, cause) || !strings.Contains(err.Error(), path) {
		t.Fatalf("withCleanup = %v; want the cause, naming %s", err, path)
	}
	for _, side := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(side); !os.IsNotExist(err) {
			t.Fatalf("%s survived (%v); want every sidecar attempted and removed", filepath.Base(side), err)
		}
	}
	if !strings.Contains(buf.String(), path) {
		t.Fatal("the unremovable file was not logged")
	}
}

// An uncertain catalogue write (the rename landed, the directory sync failed)
// is a success: the archive is kept, in memory and on disk, and the caveat is
// logged. The config directory without read permission fails only that sync.
func TestCreate_DurabilityUncertainKeepsTheArchive(t *testing.T) {
	m, cfgSvc, buf := testManager(t)
	dir := filepath.Dir(cfgSvc.Path)
	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	res, err := m.Create(context.Background(), CreateRequest{RequestKey: "req-u", Label: "L", LogbookName: "L", LogbookCallsign: "G4ABC"})
	_ = os.Chmod(dir, 0o700)
	if err != nil {
		t.Fatalf("create = %v; want success with the caveat", err)
	}
	if !strings.Contains(buf.String(), `"durability_uncertain":true`) {
		t.Fatalf("the uncertain durability was not logged: %s", buf.String())
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("archive file: %v", err)
	}
	after := catalogueNow(t, cfgSvc)
	if !slices.Contains(after.memory, res.Entry) || !slices.Contains(after.disk, res.Entry) {
		t.Fatalf("catalogue = %+v; want the entry in memory and on disk", after)
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
	var syncedWhileUnlisted, syncedAfter []string
	var placedFiles []string
	syncDir = func(p string) error {
		if err := orig(p); err != nil {
			return err
		}
		snap := cfgSvc.Snapshot()
		if len(snap.QsoArchives) != 0 {
			// After publication the only sync is the summary's own: the archive is
			// already in the catalogue on disk when it is recorded.
			if disk, err := config.Load(cfgSvc.Path); err != nil || len(disk.QsoArchives) != 1 {
				t.Errorf("at sync %s after publication: the catalogue on disk = %v (%v); want the entry", p, disk.QsoArchives, err)
			}
			syncedAfter = append(syncedAfter, p)
			return nil
		}
		syncedWhileUnlisted = append(syncedWhileUnlisted, p)
		// At every sync before publication the file is already placed, and the
		// archive is in neither catalogue, nor listed, nor summarised: a placed
		// file alone is not an archive.
		dir := ManagedDir(snap)
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".db") {
			t.Errorf("at sync %s: the managed directory holds %v (%v); want only the placed file", p, entries, err)
			return nil
		}
		placedFiles = append(placedFiles, filepath.Join(dir, entries[0].Name()))
		if disk, err := config.Load(cfgSvc.Path); err != nil || len(disk.QsoArchives) != 0 {
			t.Errorf("at sync %s: the catalogue on disk = %v (%v); want it not yet written", p, disk.QsoArchives, err)
		}
		if listed := m.List(); len(listed) != 0 {
			t.Errorf("at sync %s: List = %+v; want nothing listed", p, listed)
		}
		if sums, _ := ReadSummaries(SummariesPath(snap)); len(sums) != 0 {
			t.Errorf("at sync %s: summaries %v; want none yet", p, sums)
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
	for _, f := range placedFiles {
		if f != res.Path {
			t.Fatalf("placed file at a sync = %s; want %s", f, res.Path)
		}
	}
	if sidecarDir := filepath.Dir(SummariesPath(cfgSvc.Snapshot())); len(syncedAfter) != 1 || syncedAfter[0] != sidecarDir {
		t.Fatalf("syncs after publication = %v; want only the summary's, %s", syncedAfter, sidecarDir)
	}
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

// The sync chain is bounded by the working directory however it was spelled
// (review 83ffc913): a configured trailing slash must not push the walk past
// data_dir to /, and a directory outside the root syncs only itself.
func TestDirsUpTo_BoundedByACleanedRoot(t *testing.T) {
	got := dirsUpTo("/srv/private/station/db/qso-archives", "/srv/private/station/")
	want := []string{"/srv/private/station/db/qso-archives", "/srv/private/station/db", "/srv/private/station"}
	if len(got) != len(want) {
		t.Fatalf("dirsUpTo = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dirsUpTo = %v, want %v", got, want)
		}
	}
	if got := dirsUpTo("/elsewhere/x", "/srv/private/station"); len(got) != 1 || got[0] != "/elsewhere/x" {
		t.Fatalf("dirsUpTo outside the root = %v, want just the directory", got)
	}
	if got := dirsUpTo("/srv/private/station", "/srv/private/station/"); len(got) != 1 {
		t.Fatalf("dirsUpTo at the root = %v, want just the root", got)
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

// ADR 0084 slice 2a: a created archive's contents are known at once — its one
// logbook, 0 QSOs — recorded in the sidecar against the file as it was placed,
// so Settings lists it without opening it. Derived state: a failed sidecar write
// is logged, never a failed creation.
func TestCreate_RecordsTheNewArchiveSummary(t *testing.T) {
	m, cfgSvc, _ := testManager(t)
	res, err := m.Create(context.Background(), CreateRequest{
		RequestKey: "req-sum", Label: "Contest", LogbookName: "CQWW", LogbookCallsign: "7q5mlv",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sums, miss := ReadSummaries(SummariesPath(cfgSvc.Snapshot()))
	if miss != "" {
		t.Fatalf("sidecar: miss %q", miss)
	}
	got, ok := sums[res.Entry.ID]
	if !ok || len(got.Logbooks) != 1 {
		t.Fatalf("summary for the new archive = %+v (present %v); want one logbook", got, ok)
	}
	lb := got.Logbooks[0]
	if lb.Name != "CQWW" || lb.Callsign != "7Q5MLV" || lb.QsoCount != 0 || !utils.IsValidUUIDv7(lb.UUID) {
		t.Fatalf("logbook summary = %+v; want CQWW / 7Q5MLV / 0 QSOs with its UUID", lb)
	}
	now, err := SignatureOf(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Current(now) {
		t.Fatalf("the new archive's summary reads stale against its own file (%+v vs %+v)", got.Signature, now)
	}
}

func TestCreate_ASidecarFailureDoesNotFailTheCreation(t *testing.T) {
	m, cfgSvc, buf := testManager(t)
	// A directory where the sidecar belongs: every write to it fails.
	if err := os.MkdirAll(SummariesPath(cfgSvc.Snapshot()), 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := m.Create(context.Background(), CreateRequest{
		RequestKey: "req-nosum", Label: "Drill", LogbookName: "Drill", LogbookCallsign: "7Q5MLV",
	})
	if err != nil {
		t.Fatalf("create failed on a sidecar fault: %v", err)
	}
	if rec := logRecord(t, buf.String(), "archive: could not record the new archive summary"); rec["level"] != "warn" || rec["archive_id"] != res.Entry.ID {
		t.Fatalf("the sidecar fault's record = %v; want level warn and archive_id %s on it", rec, res.Entry.ID)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("archive file: %v", err)
	}
	after := catalogueNow(t, cfgSvc)
	if !slices.Contains(after.memory, res.Entry) || !slices.Contains(after.disk, res.Entry) {
		t.Fatalf("catalogue = %+v; want the entry kept in memory and on disk", after)
	}
}
