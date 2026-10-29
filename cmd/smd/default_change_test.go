package main

import (
	"context"
	"database/sql"
	stderr "errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0024 slice 3a (ADR 0097 §1, §3): a default change applied at start, and
   the records it leaves. Real daemon generations, each loading config.json
   afresh from disk, on the real SQLite archive; faults are injected through
   the reconciliation's pointer port (newDefaultPointer) and, for config
   persistence, through the config directory's permissions (0300: the rename
   lands but the directory sync fails, an uncertain write; 0500: the write
   fails). Each case checks the resulting state AND what is on disk.

   The fixture: Home with its default A (logbook 1), an empty logbook B under
   the same callsign (the target), and an empty logbook C under another
   callsign (ineligible).

     DC1  ordering: a pending change to B is applied before anything reads
          the default; config, the API and the archive pointer name B; the
          record is removed durably.
     DC2  crash recovery, the file already names B: durability is
          established by a durable rewrite before the record is removed.
     DC3  crash recovery whose durable rewrite fails, or whose B is no longer
          live: the start fails, the selector is kept.
     DC4  the commit/read-back matrix: a refused write leaves A verified and
          the intent pending; a failed write with B visible is rewritten
          durably, or the start fails; an unreadable or unexpected pointer
          fails the start; the selector is kept whenever the start fails.
     DC5  self-healing never conceals: a selector with a NULL or unreadable
          archive pointer fails the start; no "Default" logbook is seeded.
     DC6  refusals at start: a missing, deleted or ineligible target, or
          another archive's selector, is abandoned with its code; A stays.
     DC7  config persistence of the cleanup: durable, uncertain and failed
          give distinct states; the effective default follows the verified
          file even when its projection cannot be persisted.
     DC8  tombstones: every start treats one as unconfirmed and rewrites it
          durably; an unrelated save confirms it only when it is written
          durably holding it; until then it survives every save.
     DC9  boundaries: PUT /v1/config cannot create, replace or remove the
          record; logbook deletion refuses a pending or unconfirmed target.
     DC10 compatibility: without a record the start is unchanged, and no
          record appears on disk.
*/

type changeFixture struct {
	d                *daemon // the first generation, stopped
	cfgPath, archive string
	a, b, c          int64
	uuid             map[int64]string
	running          map[*daemon]*orchestrator.Orchestrator
}

func newChangeFixture(t *testing.T) *changeFixture {
	t.Helper()
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("first start: %v", err)
	}
	f := &changeFixture{d: d, cfgPath: d.cfgSvc.Path, archive: d.cfgSvc.Snapshot().ActiveQsoArchiveID, a: 1,
		uuid: map[int64]string{}, running: map[*daemon]*orchestrator.Orchestrator{}}
	var err error
	if f.b, err = d.db.InsertLogbook(types.Logbook{Name: "Contest", Callsign: "7Q5MLV"}); err != nil {
		t.Fatal(err)
	}
	if f.c, err = d.db.InsertLogbook(types.Logbook{Name: "Other", Callsign: "7Q5XYZ"}); err != nil {
		t.Fatal(err)
	}
	books, err := d.db.FetchAllLogbooksWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, lb := range books {
		f.uuid[lb.ID] = lb.UUID
	}
	stopGen(t, orch)
	return f
}

// selector writes a pending change to logbook id into config.json.
func (f *changeFixture) selector(t *testing.T, id int64) *config.DefaultLogbookChange {
	t.Helper()
	rec := &config.DefaultLogbookChange{RequestID: "019fd5c5-0000-7000-8000-00000000c0de", ArchiveID: f.archive, LogbookUUID: f.uuid[id]}
	f.writeRecord(t, rec)
	return rec
}

func (f *changeFixture) writeRecord(t *testing.T, rec *config.DefaultLogbookChange) {
	t.Helper()
	f.editDisk(t, func(c *config.Config) { c.DefaultLogbookChange = rec })
}

func (f *changeFixture) editDisk(t *testing.T, fn func(c *config.Config)) {
	t.Helper()
	cfg, err := config.Load(f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	fn(&cfg)
	if _, err := config.WriteJSON(f.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
}

// disk is config.json as the next start would load it.
func (f *changeFixture) disk(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func (f *changeFixture) rawDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+f.d.paths.QSO)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// pointer is the archive's own default pointer, read between generations.
func (f *changeFixture) pointer(t *testing.T) int64 {
	t.Helper()
	var p sql.NullInt64
	if err := f.rawDB(t).QueryRow(`SELECT default_logbook_id FROM archive_metadata WHERE singleton = 1`).Scan(&p); err != nil {
		t.Fatal(err)
	}
	return p.Int64
}

func (f *changeFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.rawDB(t).Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// live reports whether logbook id exists and is not soft-deleted.
func (f *changeFixture) live(t *testing.T, id int64) bool {
	t.Helper()
	var n int
	if err := f.rawDB(t).QueryRow(`SELECT COUNT(*) FROM logbook WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// logbooks counts every logbook row, soft-deleted included: a seeded row adds one.
func (f *changeFixture) logbooks(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.rawDB(t).QueryRow(`SELECT COUNT(*) FROM logbook`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// start loads config.json afresh and starts a generation on it.
func (f *changeFixture) start(t *testing.T) (*daemon, *orchestrator.Orchestrator, error) {
	t.Helper()
	cfg, err := config.Load(f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgSvc := config.New(cfg)
	cfgSvc.SetPath(f.cfgPath)
	d, orch := buildOrchestratedDaemon(t, cfgSvc, cfg)
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		return d, orch, err
	}
	f.running[d] = orch
	t.Cleanup(func() {
		if o, ok := f.running[d]; ok {
			stopGen(t, o)
		}
	})
	return d, orch, nil
}

// stop ends d's generation, so raw reads between generations see its writes.
func (f *changeFixture) stop(t *testing.T, d *daemon) {
	t.Helper()
	if o, ok := f.running[d]; ok {
		delete(f.running, d)
		stopGen(t, o)
	}
}

func (f *changeFixture) mustStart(t *testing.T) *daemon {
	t.Helper()
	d, _, err := f.start(t)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return d
}

func (f *changeFixture) mustFail(t *testing.T) {
	t.Helper()
	if _, _, err := f.start(t); err == nil {
		t.Fatal("the start succeeded; want it to fail")
	}
}

// configDir runs fn with the config directory at mode, restoring it after.
func (f *changeFixture) configDir(t *testing.T, mode os.FileMode, fn func()) {
	t.Helper()
	dir := filepath.Dir(f.cfgPath)
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	fn()
}

const (
	uncertainDir os.FileMode = 0o300 // the rename lands; the directory sync fails
	failingDir   os.FileMode = 0o500 // no write lands
)

func wantRecord(t *testing.T, where string, got *config.DefaultLogbookChange, removed string) {
	t.Helper()
	switch {
	case removed == "none" && got != nil:
		t.Fatalf("%s holds the record %+v; want none", where, *got)
	case removed != "none" && got == nil:
		t.Fatalf("%s holds no record; want one removed=%q", where, removed)
	case removed != "none" && got.Removed != removed:
		t.Fatalf("%s holds %+v; want removed=%q", where, *got, removed)
	}
}

func wantReport(t *testing.T, d *daemon, state string, unconfirmed bool) defaultChangeReport {
	t.Helper()
	r := d.defaultChange.get()
	if r.State != state || r.Unconfirmed != unconfirmed {
		t.Fatalf("default change = %+v; want state %q, unconfirmed %v", r, state, unconfirmed)
	}
	return r
}

// ---- the pointer port's faults ----------------------------------------------

// pointerStep is what one durable write does, in order.
type pointerStep int

const (
	stepDurable         pointerStep = iota // the real durable write
	stepRefuse                             // fails before its commit, writing nothing
	stepVisibleThenFail                    // lands non-durably, then reports failure
	stepRedirect                           // writes redirectTo instead, and reports success
	stepReportOKNoWrite                    // reports success, writing nothing
)

// faultPointer wraps the real archive. Each durable write takes the next
// step (stepDurable once they run out); writes counts them.
type faultPointer struct {
	real *sqlite.Service

	mu         sync.Mutex
	steps      []pointerStep
	redirectTo int64
	// unreadableAfterWrite / unreadable: reading the identity fails.
	unreadableAfterWrite, unreadable bool
	writes                           int
}

func (p *faultPointer) ArchiveIdentityWithContext(ctx context.Context) (sqlite.ArchiveIdentity, error) {
	p.mu.Lock()
	bad := p.unreadable || (p.unreadableAfterWrite && p.writes > 0)
	p.mu.Unlock()
	if bad {
		return sqlite.ArchiveIdentity{}, stderr.New("injected: the archive identity cannot be read")
	}
	return p.real.ArchiveIdentityWithContext(ctx)
}

func (p *faultPointer) SetArchiveDefaultLogbookDurablyWithContext(ctx context.Context, id int64) error {
	p.mu.Lock()
	p.writes++
	step := stepDurable
	if len(p.steps) > 0 {
		step, p.steps = p.steps[0], p.steps[1:]
	}
	p.mu.Unlock()
	switch step {
	case stepRefuse:
		return stderr.New("injected: the durable write failed before its commit")
	case stepVisibleThenFail:
		if err := p.real.SetArchiveDefaultLogbookWithContext(ctx, id); err != nil {
			return err
		}
		return stderr.New("injected: the commit's outcome is uncertain")
	case stepRedirect:
		return p.real.SetArchiveDefaultLogbookWithContext(ctx, p.redirectTo)
	case stepReportOKNoWrite:
		return nil
	}
	return p.real.SetArchiveDefaultLogbookDurablyWithContext(ctx, id)
}

func (p *faultPointer) FetchAllLogbooksWithContext(ctx context.Context) ([]types.Logbook, error) {
	return p.real.FetchAllLogbooksWithContext(ctx)
}

func (p *faultPointer) writeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes
}

// withFaults installs fp as the reconciliation's store for this test.
func withFaults(t *testing.T, fp *faultPointer) *faultPointer {
	t.Helper()
	prev := newDefaultPointer
	newDefaultPointer = func(db *sqlite.Service) defaultPointer { fp.real = db; return fp }
	t.Cleanup(func() { newDefaultPointer = prev })
	return fp
}

// ---- DC1 ---------------------------------------------------------------------

func TestDefaultChange_DC1_AppliedBeforeAnythingReadsTheDefault(t *testing.T) {
	f := newChangeFixture(t)
	f.selector(t, f.b)
	fp := withFaults(t, &faultPointer{})
	d := f.mustStart(t)
	if got := d.cfgSvc.Snapshot().DefaultLogbookID; got != f.b {
		t.Fatalf("config's default = %d; want B (%d)", got, f.b)
	}
	if code, body := apiCall(t, d, http.MethodGet, "/v1/config"); code != http.StatusOK || !strings.Contains(string(body), `"default_logbook":{"id":`+itoa(f.b)) {
		t.Fatalf("GET /v1/config = %d %s; want the default logbook B", code, body)
	}
	wantReport(t, d, defaultChangeEffective, false)
	wantRecord(t, "memory", d.cfgSvc.Snapshot().DefaultLogbookChange, "none")
	if fp.writeCount() != 1 {
		t.Fatalf("durable pointer writes = %d; want 1", fp.writeCount())
	}
	disk := f.disk(t)
	if disk.DefaultLogbookID != f.b {
		t.Fatalf("config.json's default = %d; want B", disk.DefaultLogbookID)
	}
	wantRecord(t, "config.json", disk.DefaultLogbookChange, "none")
	if f.logbooks(t) != 3 {
		t.Fatalf("logbooks = %d; want 3 (nothing seeded)", f.logbooks(t))
	}
}

func TestDefaultChange_DC1_ThePointerIsB(t *testing.T) {
	f := newChangeFixture(t)
	f.selector(t, f.b)
	d := f.mustStart(t)
	f.stop(t, d)
	if p := f.pointer(t); p != f.b {
		t.Fatalf("archive pointer = %d; want B (%d)", p, f.b)
	}
}

// ---- DC2 / DC3 ---------------------------------------------------------------

func TestDefaultChange_DC2_CrashRecoveryRewritesDurably(t *testing.T) {
	f := newChangeFixture(t)
	f.selector(t, f.b)
	f.exec(t, `UPDATE archive_metadata SET default_logbook_id = ? WHERE singleton = 1`, f.b) // the file already names B
	fp := withFaults(t, &faultPointer{})
	d := f.mustStart(t)
	if fp.writeCount() != 1 {
		t.Fatalf("durable rewrites = %d; want 1 (visible B is not proven durable)", fp.writeCount())
	}
	if got := d.cfgSvc.Snapshot().DefaultLogbookID; got != f.b {
		t.Fatalf("default = %d; want B", got)
	}
	wantReport(t, d, defaultChangeEffective, false)
	wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "none")
}

func TestDefaultChange_DC3_CrashRecoveryFailures(t *testing.T) {
	t.Run("the durable rewrite fails", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		f.exec(t, `UPDATE archive_metadata SET default_logbook_id = ? WHERE singleton = 1`, f.b)
		withFaults(t, &faultPointer{steps: []pointerStep{stepRefuse}})
		f.mustFail(t)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
		if f.logbooks(t) != 3 {
			t.Fatal("a logbook was seeded")
		}
	})
	t.Run("B is no longer live", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		f.exec(t, `UPDATE archive_metadata SET default_logbook_id = ? WHERE singleton = 1`, f.b)
		f.exec(t, `UPDATE logbook SET deleted_at = datetime('now') WHERE id = ?`, f.b)
		f.mustFail(t)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
}

// ---- DC4 ---------------------------------------------------------------------

func TestDefaultChange_DC4_TheCommitReadBackMatrix(t *testing.T) {
	t.Run("refused before its commit, A verified: still pending", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		withFaults(t, &faultPointer{steps: []pointerStep{stepRefuse}})
		d := f.mustStart(t)
		if got := d.cfgSvc.Snapshot().DefaultLogbookID; got != f.a {
			t.Fatalf("default = %d; want A", got)
		}
		wantReport(t, d, defaultChangeStillPending, true)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
		f.stop(t, d)
		if p := f.pointer(t); p != f.a {
			t.Fatalf("pointer = %d; want A", p)
		}
	})
	t.Run("uncertain with B visible: rewritten durably, then applied", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		fp := withFaults(t, &faultPointer{steps: []pointerStep{stepVisibleThenFail}})
		d := f.mustStart(t)
		if fp.writeCount() != 2 {
			t.Fatalf("durable writes = %d; want 2 (the write, then its durable rewrite)", fp.writeCount())
		}
		wantReport(t, d, defaultChangeEffective, false)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "none")
	})
	t.Run("uncertain with B visible, and the rewrite fails: the start fails", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		fp := withFaults(t, &faultPointer{steps: []pointerStep{stepVisibleThenFail, stepRefuse}})
		f.mustFail(t)
		if fp.writeCount() != 2 {
			t.Fatalf("durable writes = %d; want 2 (the uncertain write, then the failed rewrite)", fp.writeCount())
		}
		if p := f.pointer(t); p != f.b {
			t.Fatalf("pointer = %d; want B still visible (%d) — the case is B visible but unproven", p, f.b)
		}
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
	t.Run("a commit reported, but A reads back: the start fails", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		fp := withFaults(t, &faultPointer{steps: []pointerStep{stepReportOKNoWrite}})
		f.mustFail(t)
		if fp.writeCount() != 1 {
			t.Fatalf("durable writes = %d; want 1", fp.writeCount())
		}
		if p := f.pointer(t); p != f.a {
			t.Fatalf("pointer = %d; want A", p)
		}
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
	t.Run("committed, then the pointer is unreadable: the start fails", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		withFaults(t, &faultPointer{unreadableAfterWrite: true})
		f.mustFail(t)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
	t.Run("committed, then the pointer names another logbook: the start fails", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		withFaults(t, &faultPointer{steps: []pointerStep{stepRedirect}, redirectTo: f.c})
		f.mustFail(t)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
}

// ---- DC5 ---------------------------------------------------------------------

func TestDefaultChange_DC5_SelfHealingNeverConceals(t *testing.T) {
	t.Run("a NULL archive pointer", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		f.exec(t, `UPDATE archive_metadata SET default_logbook_id = NULL WHERE singleton = 1`)
		f.mustFail(t)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
		if p := f.pointer(t); p != 0 {
			t.Fatalf("pointer = %d; want it left NULL, not refilled from config", p)
		}
	})
	t.Run("a NULL pointer and config's default missing: nothing seeded", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		f.healable(t)
		f.mustFail(t)
		if n := f.logbooks(t); n != 3 {
			t.Fatalf("logbooks = %d; want 3 — a Default was seeded over the pending change", n)
		}
	})
	t.Run("an unreadable archive identity", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		withFaults(t, &faultPointer{unreadable: true})
		f.mustFail(t)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
}

// ---- DC6 ---------------------------------------------------------------------

func TestDefaultChange_DC6_RefusedAtStartIsAbandoned(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, f *changeFixture){
		"a missing target": func(t *testing.T, f *changeFixture) {
			rec := f.selector(t, f.b)
			rec.LogbookUUID = "019fd5c5-0000-7000-8000-00000000dead"
			f.writeRecord(t, rec)
		},
		"a deleted target": func(t *testing.T, f *changeFixture) {
			f.selector(t, f.b)
			f.exec(t, `UPDATE logbook SET deleted_at = datetime('now') WHERE id = ?`, f.b)
		},
		"an ineligible target (another callsign)": func(t *testing.T, f *changeFixture) { f.selector(t, f.c) },
		"another archive's selector": func(t *testing.T, f *changeFixture) {
			rec := f.selector(t, f.b)
			rec.ArchiveID = "019fd5c5-efcc-7193-be4f-1fee532ee3a9"
			f.writeRecord(t, rec)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newChangeFixture(t)
			setup(t, f)
			d := f.mustStart(t)
			if got := d.cfgSvc.Snapshot().DefaultLogbookID; got != f.a {
				t.Fatalf("default = %d; want A", got)
			}
			r := wantReport(t, d, defaultChangeAbandoned, false)
			if r.FailureCode == "" {
				t.Fatal("abandoned without a failure code")
			}
			wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "none")
			f.stop(t, d)
			if p := f.pointer(t); p != f.a {
				t.Fatalf("pointer = %d; want A", p)
			}
		})
	}
}

// ---- DC7 ---------------------------------------------------------------------

func TestDefaultChange_DC7_CleanupPersistenceOutcomes(t *testing.T) {
	t.Run("applied, cleanup uncertain: an unconfirmed tombstone", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		var d *daemon
		f.configDir(t, uncertainDir, func() { d = f.mustStart(t) })
		if got := d.cfgSvc.Snapshot().DefaultLogbookID; got != f.b {
			t.Fatalf("default = %d; want B", got)
		}
		wantReport(t, d, defaultChangeCleanupPending, true)
		wantRecord(t, "memory", d.cfgSvc.Snapshot().DefaultLogbookChange, config.RemovedApplied)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, config.RemovedApplied)
	})
	t.Run("applied, cleanup failed: B effective although config.json still says A", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		var d *daemon
		f.configDir(t, failingDir, func() { d = f.mustStart(t) })
		if got := d.cfgSvc.Snapshot().DefaultLogbookID; got != f.b {
			t.Fatalf("default = %d; want B, the verified file's", got)
		}
		wantReport(t, d, defaultChangeCleanupPending, true)
		disk := f.disk(t)
		if disk.DefaultLogbookID != f.a {
			t.Fatalf("config.json's default = %d; want A, the projection not persisted", disk.DefaultLogbookID)
		}
		wantRecord(t, "config.json", disk.DefaultLogbookChange, "")

		// The next start, with config writable, finds the file on B.
		f.stop(t, d)
		d2 := f.mustStart(t)
		wantReport(t, d2, defaultChangeEffective, false)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "none")
	})
	t.Run("abandoned, cleanup uncertain: withdrawal unconfirmed", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.c)
		var d *daemon
		f.configDir(t, uncertainDir, func() { d = f.mustStart(t) })
		wantReport(t, d, defaultChangeRemovalUnconfirmed, true)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, config.RemovedAbandoned)
	})
	t.Run("abandoned, cleanup failed: the intent is still pending", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.c)
		var d *daemon
		f.configDir(t, failingDir, func() { d = f.mustStart(t) })
		wantReport(t, d, defaultChangeStillPending, true)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
}

// ---- DC8 ---------------------------------------------------------------------

func TestDefaultChange_DC8_Tombstones(t *testing.T) {
	tombstone := func(f *changeFixture) *config.DefaultLogbookChange {
		return &config.DefaultLogbookChange{RequestID: "019fd5c5-0000-7000-8000-00000000c0de", ArchiveID: f.archive,
			LogbookUUID: f.uuid[f.b], Removed: config.RemovedWithdrawn}
	}
	t.Run("found at start: rewritten durably, then dropped", func(t *testing.T) {
		f := newChangeFixture(t)
		f.writeRecord(t, tombstone(f))
		d := f.mustStart(t)
		wantReport(t, d, "", false)
		wantRecord(t, "memory", d.cfgSvc.Snapshot().DefaultLogbookChange, "none")
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "none")
	})
	t.Run("found at start, the rewrite uncertain: unconfirmed and kept", func(t *testing.T) {
		f := newChangeFixture(t)
		f.writeRecord(t, tombstone(f))
		var d *daemon
		f.configDir(t, uncertainDir, func() { d = f.mustStart(t) })
		wantReport(t, d, defaultChangeRemovalUnconfirmed, true)
		wantRecord(t, "memory", d.cfgSvc.Snapshot().DefaultLogbookChange, config.RemovedWithdrawn)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, config.RemovedWithdrawn)
	})
	t.Run("unconfirmed: survives uncertain and failed saves; a durable save confirms it", func(t *testing.T) {
		f := newChangeFixture(t)
		f.writeRecord(t, tombstone(f))
		var d *daemon
		f.configDir(t, uncertainDir, func() { d = f.mustStart(t) })
		save := func(n string) (int, []byte) {
			return apiCallBody(t, d, http.MethodPut, "/v1/config", `{"ft8_max_repeats":`+n+`}`)
		}
		f.configDir(t, uncertainDir, func() { save("3") })
		wantReport(t, d, defaultChangeRemovalUnconfirmed, true)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, config.RemovedWithdrawn)
		f.configDir(t, failingDir, func() { save("4") })
		wantReport(t, d, defaultChangeRemovalUnconfirmed, true)
		wantRecord(t, "memory", d.cfgSvc.Snapshot().DefaultLogbookChange, config.RemovedWithdrawn)

		if code, body := save("5"); code != http.StatusOK {
			t.Fatalf("PUT = %d %s", code, body)
		}
		wantReport(t, d, "", false)
		wantRecord(t, "memory", d.cfgSvc.Snapshot().DefaultLogbookChange, "none")
	})
	t.Run("an active selector is not confirmed by a durable save", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		withFaults(t, &faultPointer{steps: []pointerStep{stepRefuse}})
		d := f.mustStart(t)
		if code, body := apiCallBody(t, d, http.MethodPut, "/v1/config", `{"ft8_max_repeats":3}`); code != http.StatusOK {
			t.Fatalf("PUT = %d %s", code, body)
		}
		wantReport(t, d, defaultChangeStillPending, true)
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
}

// ---- DC9 ---------------------------------------------------------------------

func TestDefaultChange_DC9_Boundaries(t *testing.T) {
	// PUT /v1/config ignores a key it does not carry (AW-3's lenient unknown
	// keys answer 200), so the boundary is the outcome, not the status: the
	// record is neither created, replaced nor removed, in memory or on disk.
	t.Run("PUT /v1/config cannot create the record", func(t *testing.T) {
		f := newChangeFixture(t)
		d := f.mustStart(t)
		apiCallBody(t, d, http.MethodPut, "/v1/config",
			`{"default_logbook_change":{"request_id":"019fd5c5-0000-7000-8000-00000000c0de","archive_id":"`+f.archive+`","logbook_uuid":"`+f.uuid[f.b]+`"},"ft8_max_repeats":3}`)
		wantRecord(t, "memory", d.cfgSvc.Snapshot().DefaultLogbookChange, "none")
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "none")
	})
	t.Run("PUT /v1/config neither replaces nor removes it", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		withFaults(t, &faultPointer{steps: []pointerStep{stepRefuse}})
		d := f.mustStart(t)
		if code, body := apiCallBody(t, d, http.MethodPut, "/v1/config", `{"ft8_max_repeats":3}`); code != http.StatusOK {
			t.Fatalf("PUT = %d %s", code, body)
		}
		for _, body := range []string{`{"default_logbook_change":null,"ft8_max_repeats":4}`,
			`{"default_logbook_change":{"request_id":"x","archive_id":"y","logbook_uuid":"z","removed":"cancelled"},"ft8_max_repeats":5}`} {
			apiCallBody(t, d, http.MethodPut, "/v1/config", body)
			if got := d.cfgSvc.Snapshot().DefaultLogbookChange; got == nil || got.RequestID != "019fd5c5-0000-7000-8000-00000000c0de" || got.Removed != "" {
				t.Fatalf("after PUT %s the record is %+v; want the pending selector unchanged", body, got)
			}
		}
		wantRecord(t, "memory", d.cfgSvc.Snapshot().DefaultLogbookChange, "")
		wantRecord(t, "config.json", f.disk(t).DefaultLogbookChange, "")
	})
	t.Run("deleting a pending target is refused", func(t *testing.T) {
		f := newChangeFixture(t)
		f.selector(t, f.b)
		withFaults(t, &faultPointer{steps: []pointerStep{stepRefuse}})
		d := f.mustStart(t)
		code, body := apiCall(t, d, http.MethodDelete, "/v1/logbook/"+itoa(f.b))
		if code != http.StatusConflict || !strings.Contains(string(body), "default_change_target") {
			t.Fatalf("DELETE = %d %s; want 409 default_change_target", code, body)
		}
		f.stop(t, d)
		if !f.live(t, f.b) {
			t.Fatal("the pending target was deleted")
		}
	})
	t.Run("deleting an unconfirmed tombstone's target is refused", func(t *testing.T) {
		f := newChangeFixture(t)
		f.writeRecord(t, &config.DefaultLogbookChange{RequestID: "019fd5c5-0000-7000-8000-00000000c0de", ArchiveID: f.archive,
			LogbookUUID: f.uuid[f.b], Removed: config.RemovedCancelled})
		var d *daemon
		f.configDir(t, uncertainDir, func() { d = f.mustStart(t) })
		code, body := apiCall(t, d, http.MethodDelete, "/v1/logbook/"+itoa(f.b))
		if code != http.StatusConflict || !strings.Contains(string(body), "default_change_target") {
			t.Fatalf("DELETE = %d %s; want 409 default_change_target", code, body)
		}
		f.stop(t, d)
		if !f.live(t, f.b) {
			t.Fatal("the unconfirmed tombstone's target was deleted")
		}
	})
	t.Run("a confirmed tombstone's target is deletable", func(t *testing.T) {
		f := newChangeFixture(t)
		f.writeRecord(t, &config.DefaultLogbookChange{RequestID: "019fd5c5-0000-7000-8000-00000000c0de", ArchiveID: f.archive,
			LogbookUUID: f.uuid[f.b], Removed: config.RemovedCancelled})
		d := f.mustStart(t)
		if code, body := apiCall(t, d, http.MethodDelete, "/v1/logbook/"+itoa(f.b)); code != http.StatusNoContent {
			t.Fatalf("DELETE = %d %s; want 204", code, body)
		}
		f.stop(t, d)
		if f.live(t, f.b) {
			t.Fatal("the confirmed tombstone's target is still live after its delete")
		}
	})
}

// ---- DC10 --------------------------------------------------------------------

// healable sets up what the existing self-heal repairs by seeding a new
// "Default" logbook: no archive pointer, and config's default naming no row.
// Logbook 1 is renamed first, so the seed's name is free and only the guard
// under test can stop it.
func (f *changeFixture) healable(t *testing.T) {
	t.Helper()
	f.exec(t, `UPDATE logbook SET name = 'Home' WHERE id = 1`)
	f.exec(t, `UPDATE archive_metadata SET default_logbook_id = NULL WHERE singleton = 1`)
	f.editDisk(t, func(c *config.Config) { c.DefaultLogbookID = 99 })
}

func TestDefaultChange_DC10_WithoutARecordTheSelfHealStillSeeds(t *testing.T) {
	f := newChangeFixture(t)
	f.healable(t)
	d := f.mustStart(t)
	if got := d.cfgSvc.Snapshot().DefaultLogbookID; got == 99 || got == f.a {
		t.Fatalf("default = %d; want the newly seeded logbook", got)
	}
	f.stop(t, d)
	if n := f.logbooks(t); n != 4 {
		t.Fatalf("logbooks = %d; want 4 (the existing self-heal seeded one)", n)
	}
}

func TestDefaultChange_DC10_WithoutARecordNothingChanges(t *testing.T) {
	f := newChangeFixture(t)
	fp := withFaults(t, &faultPointer{})
	d := f.mustStart(t)
	if got := d.cfgSvc.Snapshot().DefaultLogbookID; got != f.a {
		t.Fatalf("default = %d; want A", got)
	}
	wantReport(t, d, "", false)
	if fp.writeCount() != 0 {
		t.Fatalf("durable pointer writes = %d; want none", fp.writeCount())
	}
	if code, body := apiCallBody(t, d, http.MethodPut, "/v1/config", `{"ft8_max_repeats":3}`); code != http.StatusOK {
		t.Fatalf("PUT = %d %s", code, body)
	}
	raw, err := os.ReadFile(f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "default_logbook_change") {
		t.Fatalf("config.json gained the key: %s", raw)
	}
}

// ---- helpers -------------------------------------------------------------------

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
