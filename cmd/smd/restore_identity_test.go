package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	migrate "github.com/golang-migrate/migrate/v4"
)

/*
   W-0021 5F.5 (ADR 0088; rulings K1–K6 and the test-plan amendments,
   2026-10-13): `smd restore` restores the target logbook's OWN cloud logbook,
   by the target archive's and logbook's UUIDs, through the scoped export. The
   legacy name is an explicit --cloud-logbook NAME. A fake SM Cloud server
   answers here; EI3 runs the real one.

     RI1  no flags, Home active, a target with no binding: GET /v1/version,
          then the scoped export at (Home's id, its default logbook's UUID) and
          nothing else. Both rows restore with UUID, revision and tombstone
          kept; no binding is created and no upload queued; a re-run adds
          nothing.
     RI2  no flags with a managed archive active: that archive's own default.
     RI3  --archive names an inactive archive: ITS default logbook (not the
          active archive's id); --logbook selects another of its logbooks.
     RI4  an export that is not complete and correctly addressed (no head,
          another archive, another logbook, cut off after a valid row) is
          refused: nothing inserted, and the data directory untouched (no
          migration, no file created, no summary written). A valid empty
          export succeeds.
     RI5  the support preflight: unsupported, unreachable and any other error
          are refused, each in its own words, before any export is requested;
          the data directory untouched.
     RI6  supported, then a 404: refused naming both UUIDs and --cloud-logbook,
          without claiming the server supports identity; no whole-tenant
          export follows (no fallback); the data directory untouched.
     RI7  an identity --dry-run counts the rows and leaves the data directory
          untouched.
     RI8  a target whose binding is stamped (enabled or disabled), and one with
          an unstamped legacy binding, restore by identity; the binding is
          unchanged (never created, enabled or stamped) and nothing is queued.
     RI9  --cloud-logbook NAME into a stamped target is refused in Home and in a
          managed archive, enabled or disabled, before the export is fetched,
          naming the identity restore; an unstamped target restores the legacy
          row (control), reading only the whole-tenant export.
     RI10 --forwarder is retired: refused before any request, pointing to
          --cloud-logbook NAME, where NAME is the cloud logbook's name.
     RI11 an install with no archive identity yet (no catalogue): no flags is
          refused before any request, pointing to --cloud-logbook.
     RI12 a target changed while its export was fetched (an identity target's
          logbook UUID; a legacy target's binding stamped, enabled or
          disabled) is refused by the re-check through the writing
          connection; nothing is inserted.
     RI13 an archive at log schema 12 or 13 (UUIDs, no binding table): an
          identity dry-run resolves its default logbook by UUID without
          migrating, and leaves the data directory untouched.

   "The data directory untouched" is treeDiff's sense (ruling A, 2026-10-14):
   every entry that existed is unchanged byte for byte and mode for mode,
   existing sidecars included, and nothing is created, except what a read-only
   open leaves beside the one archive file the command inspected (a zero-byte
   -wal and its 32 KiB -shm, with that file's mode). A write held in a
   nonempty WAL is a change. RI10 and RI11 inspect no file and allow none.
*/

const riToken = "SMC-RI-TOKEN-55"

// fakeCloud is a scripted SM Cloud server recording every request.
type fakeCloud struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []string
	version int    // /v1/version status; 0 answers 200 with identity_protocol 1
	plain   bool   // /v1/version answers 200 without identity_protocol
	scoped  string // non-empty: the scoped export's whole body, verbatim
	status  int    // non-zero: the scoped export's status
	rows    []string
}

func newFakeCloud(t *testing.T, rows ...string) *fakeCloud {
	t.Helper()
	c := &fakeCloud{rows: rows}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, _ *http.Request) {
		switch {
		case c.version != 0:
			w.WriteHeader(c.version)
		case c.plain:
			_, _ = w.Write([]byte(`{"version":"old"}`))
		default:
			_, _ = w.Write([]byte(`{"version":"new","identity_protocol":1}`))
		}
	})
	mux.HandleFunc("GET /v1/archives/{a}/logbooks/{l}/export", func(w http.ResponseWriter, r *http.Request) {
		if c.status != 0 {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(`{"code":"not_found","message":"no such logbook"}`))
			return
		}
		if c.scoped != "" {
			_, _ = w.Write([]byte(c.scoped))
			return
		}
		_, _ = w.Write([]byte(riScopedBody(r.PathValue("a"), r.PathValue("l"), c.rows...)))
	})
	mux.HandleFunc("GET /v1/export", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"logbooks":[{"id":4,"name":"Drill"}],"qsos":[` + riLegacyRow + `]}`))
	})
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.reqs = append(c.reqs, r.Method+" "+r.URL.Path)
		c.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *fakeCloud) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.reqs)
}

func riScopedBody(archiveUUID, logbookUUID string, rows ...string) string {
	return `{"archive":{"uuid":"` + archiveUUID + `","label":"Cloud"},"logbook":{"uuid":"` + logbookUUID +
		`","label":"Cloud","callsign":"M0TEST"},"qsos":[` + strings.Join(rows, ",") + `]}` + "\n"
}

// riRow is one scoped-export row whose payload is a valid QSO.
func riRow(t *testing.T, uuid string, revision int64, deleted bool) string {
	t.Helper()
	q := types.Qso{UUID: uuid}
	q.Call, q.Band, q.Mode, q.Freq, q.QsoDate, q.TimeOn = "DL9UW", "20m", "SSB", "14.255", "20261001", "100000"
	payload, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"uuid": uuid, "modified_at": "2026-10-01T10:00:00Z", "revision": revision, "qso": json.RawMessage(payload)}
	if deleted {
		row["deleted_at"] = "2026-10-02T11:00:00Z"
	}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const riLegacyUUID = "019fd5c5-efcc-7193-be4f-1fee532ee3aa"

var riLegacyRow = func() string {
	q := types.Qso{UUID: riLegacyUUID}
	q.Call, q.Band, q.Mode, q.Freq, q.QsoDate, q.TimeOn = "G4ABC", "40m", "CW", "7.020", "20260901", "090000"
	payload, _ := json.Marshal(q)
	return `{"uuid":"` + riLegacyUUID + `","logbook_id":4,"modified_at":"2026-09-01T09:00:00Z","qso":` + string(payload) + `}`
}()

// riBed is Home (legacy, archA, logbook 1 its default) and a managed archive
// (archB: logbook 1 "Contest", logbook 2 "Second" its default), with an SM
// Cloud station account at url and no binding anywhere.
type riBed struct {
	tmp, cfgPath, home, managed string
	homeLb1, mgLb1, mgLb2       string // logbook UUIDs
}

func newRIBed(t *testing.T, url, active string) *riBed {
	t.Helper()
	b := &riBed{}
	b.tmp = setupImportTestbed(t, func(c *config.Config) {
		b.home = c.Datastore.Path
		c.QsoArchives = []types.QsoArchiveConfig{
			{ID: archA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: b.home},
			{ID: archB, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged},
		}
		c.ActiveQsoArchiveID = active
		creds, _ := json.Marshal(map[string]string{"url": url, "token": riToken})
		c.Forwarders = []types.ForwarderConfig{{Name: "smcloud", Type: smcloud.Type, Credentials: creds}}
	})
	b.cfgPath = filepath.Join(b.tmp, "config.json")
	cfg, err := config.Load(b.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	b.managed = provisionManagedFile(t, cfg, archB)
	mg := openArchiveFile(t, cfg, b.managed)
	second, err := mg.InsertLogbookWithContext(t.Context(), types.Logbook{Name: "Second", Callsign: "M0TEST"})
	if err != nil || second != 2 {
		t.Fatalf("insert second logbook: %v (%d)", err, second)
	}
	if err := mg.SetArchiveDefaultLogbookWithContext(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	_ = mg.Close()
	giveIdentity(t, cfg, b.home, archA)
	b.homeLb1 = riLogbookUUID(t, b.home, 1)
	b.mgLb1, b.mgLb2 = riLogbookUUID(t, b.managed, 1), riLogbookUUID(t, b.managed, 2)
	return b
}

// riLogbookUUID reads a logbook's UUID read-write, so the file is left as a
// cleanly stopped daemon leaves it: no -wal or -shm beside it.
func riLogbookUUID(t *testing.T, path string, id int64) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var uuid string
	if err := db.QueryRow(`SELECT COALESCE(uuid, '') FROM logbook WHERE id = ?`, id).Scan(&uuid); err != nil || uuid == "" {
		t.Fatalf("%s: logbook %d has no UUID (%v)", path, id, err)
	}
	return uuid
}

func riExec(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

type riQso struct {
	UUID     string
	Logbook  int64
	Revision int64
	Deleted  bool
}

func riQsos(t *testing.T, path string) []riQso {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT uuid, logbook_id, revision, deleted_at IS NOT NULL FROM qso ORDER BY uuid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []riQso
	for rows.Next() {
		var q riQso
		if err := rows.Scan(&q.UUID, &q.Logbook, &q.Revision, &q.Deleted); err != nil {
			t.Fatal(err)
		}
		out = append(out, q)
	}
	return out
}

func riCount(t *testing.T, path, query string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// treeState is every entry under root: a directory's mode, a file's mode,
// size and content hash. A command that migrates, creates, chmods or
// summarizes changes it.
func treeState(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			out[rel] = info.Mode().String()
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[rel] = fmt.Sprintf("%s %d %s", info.Mode(), info.Size(), hex.EncodeToString(sum[:8]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// shmSize is the -shm SQLite creates when a WAL-mode file is opened: its
// first 32 KiB wal-index region (measured, W-0021 5F.5).
const shmSize = 32 << 10

// treeDiff lists how after differs from before, under 5F.5's meaning of
// "untouched" (ruling A, 2026-10-14). Every entry that existed before must be
// byte-for-byte and mode-for-mode the same, existing sidecars included. The
// only new entries allowed are those a read-only open leaves beside an archive
// file the command inspected (inspected, relative to the root): a zero-byte
// -wal and, beside it, its 32 KiB -shm, each with the archive file's own mode.
// A nonempty -wal is a write, even with the main file unchanged.
func treeDiff(before, after map[string]string, inspected ...string) []string {
	var diff []string
	for k, v := range before {
		if a, ok := after[k]; !ok {
			diff = append(diff, k+": removed")
		} else if a != v {
			diff = append(diff, fmt.Sprintf("%s: %q → %q", k, v, a))
		}
	}
	sidecar := func(k, v string) bool {
		for _, db := range inspected {
			dbFields := strings.Fields(after[db])
			if len(dbFields) != 3 {
				continue
			}
			f := strings.Fields(v)
			if len(f) != 3 || f[0] != dbFields[0] {
				continue
			}
			if k == db+"-wal" && f[1] == "0" {
				return true
			}
			if wal := strings.Fields(after[db+"-wal"]); k == db+"-shm" && f[1] == strconv.Itoa(shmSize) && len(wal) == 3 && wal[1] == "0" {
				return true
			}
		}
		return false
	}
	for k, v := range after {
		if _, ok := before[k]; !ok && !sidecar(k, v) {
			diff = append(diff, fmt.Sprintf("%s: created %q", k, v))
		}
	}
	slices.Sort(diff)
	return diff
}

// wantTreeUnchanged fails unless the data directory under root is untouched
// in treeDiff's sense; inspected are the absolute paths of the archive files
// the command may have read.
func wantTreeUnchanged(t *testing.T, label, root string, before, after map[string]string, inspected ...string) {
	t.Helper()
	rel := make([]string, 0, len(inspected))
	for _, p := range inspected {
		r, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		rel = append(rel, r)
	}
	if diff := treeDiff(before, after, rel...); len(diff) > 0 {
		t.Fatalf("%s changed the data directory:\n  %s", label, strings.Join(diff, "\n  "))
	}
}

// The check itself (ruling A): what a read-only open leaves beside the
// inspected archive passes; the same beside another file does not; and a
// write held in a nonempty WAL fails even though the main file is unchanged.
func TestRestoreIdentity_TheUntouchedCheck(t *testing.T) {
	b := newRIBed(t, "http://127.0.0.1:9", archA)
	home, _ := filepath.Rel(b.tmp, b.home)
	before := treeState(t, b.tmp)
	if _, err := sqlite.PeekDestinationBindings(b.home); err != nil {
		t.Fatal(err)
	}
	afterPeek := treeState(t, b.tmp)
	if _, ok := afterPeek[home+"-shm"]; !ok {
		t.Fatalf("a read-only open left no -shm beside %s; the fixture no longer exercises sidecars", home)
	}
	if diff := treeDiff(before, afterPeek, home); len(diff) > 0 {
		t.Fatalf("a read-only open of the inspected archive was counted as a change: %q", diff)
	}
	managed, _ := filepath.Rel(b.tmp, b.managed)
	if diff := treeDiff(before, afterPeek, managed); len(diff) != 2 {
		t.Fatalf("sidecars beside an archive not inspected: diff %q; want both reported", diff)
	}

	db, err := sql.Open("sqlite", "file:"+b.home)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`INSERT INTO logbook (name, callsign) VALUES ('Held', 'M0TEST')`); err != nil {
		t.Fatal(err)
	}
	held := treeState(t, b.tmp)
	if held[home] != before[home] {
		t.Fatalf("the main file changed (%q → %q); the fixture no longer holds the write in the WAL", before[home], held[home])
	}
	diff := treeDiff(before, held, home)
	if !slices.ContainsFunc(diff, func(d string) bool { return strings.HasPrefix(d, home+"-wal:") }) {
		t.Fatalf("a write held in a nonempty WAL passed as untouched: diff %q", diff)
	}
}

func riScopedPath(archiveUUID, logbookUUID string) string {
	return "GET /v1/archives/" + archiveUUID + "/logbooks/" + logbookUUID + "/export"
}

func wantRequests(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s: requests %q; want exactly %q", label, got, want)
	}
}

func riRestore(t *testing.T, args ...string) error {
	t.Helper()
	var err error
	captureStderr(t, func() { err = runRestore(args) })
	return err
}

func TestRestoreIdentity_RI1_HomeDefaultByItsOwnUUIDs(t *testing.T) {
	live, gone := utils.NewUUIDv7(), utils.NewUUIDv7()
	cloud := newFakeCloud(t, riRow(t, live, 3, false), riRow(t, gone, 5, true))
	b := newRIBed(t, cloud.srv.URL, archA)

	if err := riRestore(t, "--config", b.cfgPath); err != nil {
		t.Fatalf("restore: %v", err)
	}
	wantRequests(t, "RI1", cloud.requests(), "GET /v1/version", riScopedPath(archA, b.homeLb1))
	got := riQsos(t, b.home)
	want := []riQso{{live, 1, 3, false}, {gone, 1, 5, true}}
	slices.SortFunc(want, func(x, y riQso) int { return strings.Compare(x.UUID, y.UUID) })
	if !slices.Equal(got, want) {
		t.Fatalf("Home's QSOs = %+v; want %+v (UUID, logbook, revision and tombstone kept)", got, want)
	}
	if n := riCount(t, b.home, `SELECT COUNT(*) FROM logbook_destination`); n != 0 {
		t.Fatalf("restore created %d binding(s)", n)
	}
	if n := riCount(t, b.home, `SELECT COUNT(*) FROM qso_upload`); n != 0 {
		t.Fatalf("restore queued %d upload(s)", n)
	}
	if err := riRestore(t, "--config", b.cfgPath); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if again := riQsos(t, b.home); !slices.Equal(again, want) {
		t.Fatalf("a re-run changed Home's QSOs: %+v", again)
	}
}

func TestRestoreIdentity_RI2_AManagedArchiveActive(t *testing.T) {
	cloud := newFakeCloud(t, riRow(t, utils.NewUUIDv7(), 1, false))
	b := newRIBed(t, cloud.srv.URL, archB)
	if err := riRestore(t, "--config", b.cfgPath); err != nil {
		t.Fatalf("restore: %v", err)
	}
	wantRequests(t, "RI2", cloud.requests(), "GET /v1/version", riScopedPath(archB, b.mgLb2))
	if got := riQsos(t, b.managed); len(got) != 1 || got[0].Logbook != 2 {
		t.Fatalf("the managed archive's QSOs = %+v; want one, in its default logbook 2", got)
	}
	if got := riQsos(t, b.home); len(got) != 0 {
		t.Fatalf("Home received %d QSO(s)", len(got))
	}
}

func TestRestoreIdentity_RI3_AnInactiveArchivesOwnLogbooks(t *testing.T) {
	cloud := newFakeCloud(t, riRow(t, utils.NewUUIDv7(), 1, false))
	b := newRIBed(t, cloud.srv.URL, archA)
	if err := riRestore(t, "--config", b.cfgPath, "--archive", archB); err != nil {
		t.Fatalf("restore --archive: %v", err)
	}
	wantRequests(t, "RI3 default", cloud.requests(), "GET /v1/version", riScopedPath(archB, b.mgLb2))

	cloud.mu.Lock()
	cloud.reqs, cloud.rows = nil, []string{riRow(t, utils.NewUUIDv7(), 1, false)}
	cloud.mu.Unlock()
	if err := riRestore(t, "--config", b.cfgPath, "--archive", archB, "--logbook", "1"); err != nil {
		t.Fatalf("restore --archive --logbook: %v", err)
	}
	wantRequests(t, "RI3 --logbook", cloud.requests(), "GET /v1/version", riScopedPath(archB, b.mgLb1))
	got := riQsos(t, b.managed)
	if len(got) != 2 || got[0].Logbook == got[1].Logbook {
		t.Fatalf("the managed archive's QSOs = %+v; want one in each logbook", got)
	}
}

func TestRestoreIdentity_RI4_OnlyACompleteCorrectlyAddressedExport(t *testing.T) {
	row := func(t *testing.T) string { return riRow(t, utils.NewUUIDv7(), 1, false) }
	for name, body := range map[string]func(t *testing.T, b *riBed) string{
		"no head": func(t *testing.T, _ *riBed) string { return `{"qsos":[` + row(t) + `]}` },
		"another archive": func(t *testing.T, b *riBed) string {
			return riScopedBody(archB, b.homeLb1, row(t))
		},
		"another logbook": func(t *testing.T, b *riBed) string {
			return riScopedBody(archA, b.mgLb1, row(t))
		},
		"cut off after a valid row": func(t *testing.T, b *riBed) string {
			second := row(t)
			whole := riScopedBody(archA, b.homeLb1, row(t), second)
			return whole[:strings.Index(whole, second)+20]
		},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := newFakeCloud(t)
			b := newRIBed(t, cloud.srv.URL, archA)
			cloud.scoped = body(t, b)
			before := treeState(t, b.tmp)
			err := riRestore(t, "--config", b.cfgPath)
			wantRequests(t, "RI4 "+name, cloud.requests(), "GET /v1/version", riScopedPath(archA, b.homeLb1))
			if err == nil {
				t.Fatal("restore accepted the export")
			}
			wantTreeUnchanged(t, "a refused export", b.tmp, before, treeState(t, b.tmp), b.home)
			if got := riQsos(t, b.home); len(got) != 0 {
				t.Fatalf("a refused export inserted %d QSO(s)", len(got))
			}
		})
	}
	t.Run("a valid empty export", func(t *testing.T) {
		cloud := newFakeCloud(t)
		b := newRIBed(t, cloud.srv.URL, archA)
		if err := riRestore(t, "--config", b.cfgPath); err != nil {
			t.Fatalf("restore of an empty logbook: %v", err)
		}
		wantRequests(t, "RI4 empty", cloud.requests(), "GET /v1/version", riScopedPath(archA, b.homeLb1))
	})
}

func TestRestoreIdentity_RI5_TheSupportPreflight(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unreachable := "http://" + closed.Addr().String()
	_ = closed.Close()

	texts := map[string]string{}
	for name, setup := range map[string]func(c *fakeCloud) string{
		"unsupported": func(c *fakeCloud) string { c.plain = true; return c.srv.URL },
		"unreachable": func(*fakeCloud) string { return unreachable },
		"other error": func(c *fakeCloud) string { c.version = http.StatusInternalServerError; return c.srv.URL },
	} {
		t.Run(name, func(t *testing.T) {
			cloud := newFakeCloud(t, riRow(t, utils.NewUUIDv7(), 1, false))
			b := newRIBed(t, setup(cloud), archA)
			before := treeState(t, b.tmp)
			err := riRestore(t, "--config", b.cfgPath)
			if err == nil {
				t.Fatal("restore went ahead")
			}
			texts[name] = err.Error()
			for _, r := range cloud.requests() {
				if r != "GET /v1/version" {
					t.Fatalf("a refused preflight still sent %s", r)
				}
			}
			wantTreeUnchanged(t, "a refused preflight", b.tmp, before, treeState(t, b.tmp), b.home)
		})
	}
	for _, mark := range []struct{ name, word string }{
		{"unsupported", "does not report archive identity support"},
		{"unreachable", "could not be reached"},
		{"other error", "HTTP 500"},
	} {
		if !strings.Contains(texts[mark.name], mark.word) {
			t.Errorf("%s: refusal %q; want it to say %q", mark.name, texts[mark.name], mark.word)
		}
	}
}

func TestRestoreIdentity_RI6_ANotFoundNeverFallsBack(t *testing.T) {
	cloud := newFakeCloud(t)
	cloud.status = http.StatusNotFound
	b := newRIBed(t, cloud.srv.URL, archA)
	before := treeState(t, b.tmp)
	err := riRestore(t, "--config", b.cfgPath)
	if err == nil {
		t.Fatal("restore went ahead after a 404")
	}
	for _, want := range []string{archA, b.homeLb1, "--cloud-logbook"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q; want it to name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "supports") {
		t.Fatalf("refusal %q claims the server supports identity; the preflight cannot prove it still does", err)
	}
	wantRequests(t, "RI6", cloud.requests(), "GET /v1/version", riScopedPath(archA, b.homeLb1))
	wantTreeUnchanged(t, "a 404", b.tmp, before, treeState(t, b.tmp), b.home)
}

func TestRestoreIdentity_RI7_ADryRunTouchesNothing(t *testing.T) {
	cloud := newFakeCloud(t, riRow(t, utils.NewUUIDv7(), 1, false), riRow(t, utils.NewUUIDv7(), 2, true))
	b := newRIBed(t, cloud.srv.URL, archA)
	before := treeState(t, b.tmp)
	var err error
	out := captureStderr(t, func() { err = runRestore([]string{"--config", b.cfgPath, "--dry-run"}) })
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out, "2 record(s)") || !strings.Contains(out, "1 tombstone(s)") {
		t.Fatalf("dry-run output %q; want the rows and tombstones counted", out)
	}
	wantRequests(t, "RI7", cloud.requests(), "GET /v1/version", riScopedPath(archA, b.homeLb1))
	wantTreeUnchanged(t, "an identity dry-run", b.tmp, before, treeState(t, b.tmp), b.home)
}

func TestRestoreIdentity_RI8_BindingsAreLeftAlone(t *testing.T) {
	for name, binding := range map[string]string{
		"stamped, enabled":           `1, datetime('now')`,
		"stamped, disabled":          `0, datetime('now')`,
		"unstamped legacy, disabled": `0, NULL`,
	} {
		t.Run(name, func(t *testing.T) {
			cloud := newFakeCloud(t, riRow(t, utils.NewUUIDv7(), 1, false))
			b := newRIBed(t, cloud.srv.URL, archA)
			riExec(t, b.home, `UPDATE archive_metadata SET destination_bindings_seeded_at = datetime('now')`,
				`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, identity_since)
					VALUES (1, 'smcloud', 'smcloud', `+binding+`)`)
			const shape = `SELECT enabled || '/' || COALESCE(identity_since, '-') FROM logbook_destination`
			beforeShape := riCountText(t, b.home, shape)
			if err := riRestore(t, "--config", b.cfgPath); err != nil {
				t.Fatalf("restore: %v", err)
			}
			wantRequests(t, "RI8", cloud.requests(), "GET /v1/version", riScopedPath(archA, b.homeLb1))
			if got := riQsos(t, b.home); len(got) != 1 {
				t.Fatalf("Home's QSOs = %+v; want the one row", got)
			}
			if after := riCountText(t, b.home, shape); after != beforeShape {
				t.Fatalf("the binding changed: %s → %s", beforeShape, after)
			}
			if n := riCount(t, b.home, `SELECT COUNT(*) FROM logbook_destination`); n != 1 {
				t.Fatalf("%d binding(s) after the restore; want the one", n)
			}
			if n := riCount(t, b.home, `SELECT COUNT(*) FROM qso_upload`); n != 0 {
				t.Fatalf("restore queued %d upload(s)", n)
			}
		})
	}
}

func riCountText(t *testing.T, path, query string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var s string
	if err := db.QueryRow(query).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRestoreIdentity_RI9_ALegacyRestoreIntoAStampedTarget(t *testing.T) {
	type target struct {
		archive, path string
		logbook       int64
	}
	for name, tc := range map[string]struct {
		pick    func(b *riBed) target
		binding string
	}{
		"Home, stamped, enabled":     {func(b *riBed) target { return target{archA, b.home, 1} }, `1, datetime('now')`},
		"Home, stamped, disabled":    {func(b *riBed) target { return target{archA, b.home, 1} }, `0, datetime('now')`},
		"managed, stamped, enabled":  {func(b *riBed) target { return target{archB, b.managed, 2} }, `1, datetime('now')`},
		"managed, stamped, disabled": {func(b *riBed) target { return target{archB, b.managed, 2} }, `0, datetime('now')`},
		"control: Home, unstamped":   {func(b *riBed) target { return target{archA, b.home, 1} }, `1, NULL`},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := newFakeCloud(t)
			b := newRIBed(t, cloud.srv.URL, archA)
			tg := tc.pick(b)
			riExec(t, tg.path, `UPDATE archive_metadata SET destination_bindings_seeded_at = datetime('now')`,
				fmt.Sprintf(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, identity_since)
					VALUES (%d, 'smcloud', 'smcloud', %s)`, tg.logbook, tc.binding))
			before := treeState(t, b.tmp)
			err := riRestore(t, "--config", b.cfgPath, "--archive", tg.archive, "--logbook", fmt.Sprint(tg.logbook), "--cloud-logbook", "Drill")
			if strings.HasPrefix(name, "control") {
				if err != nil {
					t.Fatalf("legacy restore into an unstamped target: %v", err)
				}
				wantRequests(t, "RI9 control", cloud.requests(), "GET /v1/export")
				if got := riQsos(t, tg.path); len(got) != 1 || got[0].UUID != riLegacyUUID {
					t.Fatalf("target QSOs = %+v; want the legacy row", got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "without --cloud-logbook") {
				t.Fatalf("restore = %v; want a refusal naming the identity restore (without --cloud-logbook)", err)
			}
			wantRequests(t, "RI9 refusal", cloud.requests())
			wantTreeUnchanged(t, "a refused legacy restore", b.tmp, before, treeState(t, b.tmp), tg.path)
		})
	}
}

func TestRestoreIdentity_RI10_ForwarderIsRetired(t *testing.T) {
	cloud := newFakeCloud(t)
	b := newRIBed(t, cloud.srv.URL, archA)
	before := treeState(t, b.tmp)
	err := riRestore(t, "--config", b.cfgPath, "--forwarder", "smcloud")
	if err == nil {
		t.Fatal("--forwarder accepted")
	}
	for _, want := range []string{"--forwarder", "retired", "--cloud-logbook NAME", "cloud logbook's name", "not a binding"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q; want it to say %q", err, want)
		}
	}
	wantRequests(t, "RI10", cloud.requests())
	wantTreeUnchanged(t, "a retired flag", b.tmp, before, treeState(t, b.tmp))
}

func TestRestoreIdentity_RI11_NoArchiveIdentityYet(t *testing.T) {
	cloud := newFakeCloud(t, riRow(t, utils.NewUUIDv7(), 1, false))
	tmp := setupImportTestbed(t, func(c *config.Config) {
		creds, _ := json.Marshal(map[string]string{"url": cloud.srv.URL, "token": riToken})
		c.Forwarders = []types.ForwarderConfig{{Name: "smcloud", Type: smcloud.Type, Credentials: creds}}
	})
	before := treeState(t, tmp)
	err := riRestore(t, "--config", filepath.Join(tmp, "config.json"))
	if err == nil || !strings.Contains(err.Error(), "--cloud-logbook") {
		t.Fatalf("restore = %v; want a refusal pointing to --cloud-logbook", err)
	}
	wantRequests(t, "RI11", cloud.requests())
	wantTreeUnchanged(t, "a pre-adoption refusal", tmp, before, treeState(t, tmp))
}

// RI12: the read-only resolution is re-checked through the writing
// connection. A target changed while its export was fetched (an identity
// target's logbook UUID; a legacy target's binding stamped, enabled or
// disabled) is refused and nothing is inserted.
func TestRestoreIdentity_RI12_ATargetChangedDuringTheFetch(t *testing.T) {
	t.Run("identity: the logbook's UUID", func(t *testing.T) {
		cloud := newFakeCloud(t, riRow(t, utils.NewUUIDv7(), 1, false))
		b := newRIBed(t, cloud.srv.URL, archA)
		orig := fetchScopedExport
		fetchScopedExport = func(ctx context.Context, account types.ForwarderConfig, a, l string) (*smcloud.ScopedExport, error) {
			riExec(t, b.home, `UPDATE logbook SET uuid = '`+utils.NewUUIDv7()+`' WHERE id = 1`)
			return orig(ctx, account, a, l)
		}
		t.Cleanup(func() { fetchScopedExport = orig })
		err := riRestore(t, "--config", b.cfgPath)
		if err == nil || !strings.Contains(err.Error(), "changed while the export was fetched") {
			t.Fatalf("restore = %v; want the changed target refused", err)
		}
		if got := riQsos(t, b.home); len(got) != 0 {
			t.Fatalf("a changed target received %d QSO(s)", len(got))
		}
	})
	for _, enabled := range []int{1, 0} {
		t.Run(fmt.Sprintf("legacy: the binding stamped, enabled=%d", enabled), func(t *testing.T) {
			cloud := newFakeCloud(t)
			b := newRIBed(t, cloud.srv.URL, archB)
			riExec(t, b.managed, `UPDATE archive_metadata SET destination_bindings_seeded_at = datetime('now')`,
				fmt.Sprintf(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled) VALUES (2, 'smcloud', 'smcloud', %d)`, enabled))
			orig := fetchSMCloudExport
			fetchSMCloudExport = func(ctx context.Context, account types.ForwarderConfig) (*smcloud.Export, error) {
				riExec(t, b.managed, `UPDATE logbook_destination SET identity_since = datetime('now')`)
				return orig(ctx, account)
			}
			t.Cleanup(func() { fetchSMCloudExport = orig })
			err := riRestore(t, "--config", b.cfgPath, "--cloud-logbook", "Drill")
			if err == nil || !strings.Contains(err.Error(), "without --cloud-logbook") {
				t.Fatalf("restore = %v; want the newly stamped target refused", err)
			}
			if got := riQsos(t, b.managed); len(got) != 0 {
				t.Fatalf("a newly stamped target received %d QSO(s)", len(got))
			}
		})
	}
}

// riMigrateTo moves an archive file to a log schema version through the real
// migration steps, read-write, leaving no sidecars behind.
func riMigrateTo(t *testing.T, path string, version uint) {
	t.Helper()
	db, err := sql.Open(sqlite.SqliteDriver, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	src, drv, err := sqlite.GetMigrationDrivers(db, sqlite.MigrationSetLog)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	m, err := migrate.NewWithInstance("iofs", src, sqlite.SqliteDriver, drv)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(version); err != nil {
		t.Fatalf("migrate %s to %d: %v", path, version, err)
	}
}

// RI13: an archive backed up at log schema 12 or 13 already carries its
// archive and logbook UUIDs but no binding table. An identity dry-run resolves
// its default logbook by those UUIDs, fetches that logbook's export and leaves
// the file at its schema, untouched.
func TestRestoreIdentity_RI13_AnOlderSchemaResolvesWithoutMigrating(t *testing.T) {
	for _, version := range []uint{13, 12} {
		t.Run(fmt.Sprintf("schema %d", version), func(t *testing.T) {
			cloud := newFakeCloud(t, riRow(t, utils.NewUUIDv7(), 1, false))
			b := newRIBed(t, cloud.srv.URL, archA)
			riMigrateTo(t, b.home, version)
			before := treeState(t, b.tmp)
			var err error
			out := captureStderr(t, func() { err = runRestore([]string{"--config", b.cfgPath, "--dry-run"}) })
			if err != nil {
				t.Fatalf("dry-run at schema %d: %v", version, err)
			}
			if !strings.Contains(out, "1 record(s)") {
				t.Fatalf("dry-run output %q; want the row counted", out)
			}
			wantRequests(t, "RI13", cloud.requests(), "GET /v1/version", riScopedPath(archA, b.homeLb1))
			wantTreeUnchanged(t, "an older-schema dry-run", b.tmp, before, treeState(t, b.tmp), b.home)
			if got := riCount(t, b.home, `SELECT MAX(version) FROM schema_migrations_log`); got != int(version) {
				t.Fatalf("schema after the dry-run = %d; want %d (no migration)", got, version)
			}
		})
	}
}
