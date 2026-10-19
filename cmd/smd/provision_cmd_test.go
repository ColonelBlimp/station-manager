package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 Q7 commit 6 (ADR 0071 refinement; operator direction 2026-10-19):
   `smd provision`, offline, with the daemon stopped. These tests run without
   PostgreSQL against a fake SM Cloud; PA1 and PA2 against the real one are in
   provision_e2e_test.go.

     PR1 dispatch and flags. `smd provision` is a subcommand: an error exits
         ExitError with "smd: " on stderr, success exits 0. Exactly one of
         --list and --archive; --list takes only --config; --dry-run and
         --default-logbook need --archive; --archive and --default-logbook
         must be UUIDv7s; no positional argument. A refused invocation makes
         no request and changes nothing.
     PR2 PA5: --list and --dry-run, with an explicit config path and with the
         default one (SM_WORKING_DIR), change nothing on disk (the data and
         config directories byte for byte, no log directory created), seed no
         config, and open no archive: an inotify watch on the archive
         directories sees no open of Home's file or W's, and a control shows
         the watch sees such an open.
         --list asks only the version and the listing; --dry-run only the
         version and the export. The dry run reports the plan and says that
         database construction and round-trip compatibility were not tested.
     PR3 a missing config, explicit or default, is an error naming its path,
         for --list, --dry-run and a real provisioning alike; nothing is
         created, the default working directory included.
     PR4 refusals: a preflight refusal names its code, through --dry-run and a
         real run alike; a round-trip refusal names the row and fields; a
         restore refusal names the row. No value of the export reaches stdout,
         stderr or the structured log, and the station's files are unchanged
         (only the log directory exempt).
     PR5 a provisioning reports its outcome on stdout and exits 0: a new
         entry, and a new entry whose catalogue write's durability is
         uncertain (success with the caveat).
     PR6 the report of a published provisioning states each caveat: an
         existing entry kept, durability uncertain, a leftover temporary name
         the next start reports.
     PR7 each failure boundary reaches the command intact: no station
         account, a server without archive_export, a rejected token, an
         incomplete export, residue at U's path (named exactly), and a fatal
         catalogue write. Each exits ExitError with a diagnostic naming the
         boundary, leaves the station's files and config as they were (the
         residue untouched), and prints and logs no value.
*/

const (
	prArchive = "0197f9a0-0000-7000-8000-0000000f1001"
	prHomeID  = "0197f9a0-0000-7000-8000-0000000f1002"
	prLBA     = "0197f9a0-0000-7000-8000-0000000f10a1"
	prLBB     = "0197f9a0-0000-7000-8000-0000000f10b1"
	prLBC     = "0197f9a0-0000-7000-8000-0000000f10c1"
	prQ1      = "0197f9a0-0000-7000-8000-0000000f1101"
	prQ2      = "0197f9a0-0000-7000-8000-0000000f1102"
	prQ3      = "0197f9a0-0000-7000-8000-0000000f1103"
	prToken   = "tok-provision"
	prWID     = "0197f9a0-0000-7000-8000-0000000f1003"
	// Values that must never be printed or logged.
	prSecret  = "PR-UNMODELLED-VALUE"
	prBadFreq = "14.2x55"
)

var (
	prAt     = time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	prGoneAt = time.Date(2026, 7, 17, 7, 0, 0, 0, time.UTC)
)

func prQso(uuid, call string, revision int64, gone bool) types.Qso {
	q := types.Qso{UUID: uuid, ModifiedAt: prAt, Revision: revision}
	if gone {
		q.ModifiedAt, q.DeletedAt = prGoneAt, prGoneAt
	}
	q.ContactedStation.Call = call
	q.ContactedStation.Country = "Ukraine"
	q.ContactedStation.Gridsquare = "KN28"
	q.ContactedStation.Lat = "48.500000"
	q.ContactedStation.Lon = "25.000000"
	q.QsoDetails.Band, q.QsoDetails.Mode, q.QsoDetails.Freq = "20m", "SSB", "14.255"
	q.QsoDetails.QsoDate, q.QsoDetails.TimeOn, q.QsoDetails.TimeOff = "20260717", "142559", "142559"
	q.QsoDetails.RstSent, q.QsoDetails.RstRcvd = "59", "57"
	q.LoggingStation.StationCallsign = "7Q5MLV"
	return q
}

type prLogbook struct {
	uuid, label, callsign string
	rows                  []types.Qso
}

type prWireLogbook struct {
	UUID       string `json:"uuid"`
	Label      string `json:"label"`
	Callsign   string `json:"callsign"`
	Count      int    `json:"count"`
	Tombstones int    `json:"tombstones"`
}

type prWireRow struct {
	UUID        string          `json:"uuid"`
	LogbookUUID string          `json:"logbook_uuid"`
	ModifiedAt  time.Time       `json:"modified_at"`
	Revision    int64           `json:"revision,omitempty"`
	DeletedAt   *time.Time      `json:"deleted_at,omitempty"`
	Qso         json.RawMessage `json:"qso"`
}

// prRecord is the record a push of q puts on the wire (smcloud.UploadFor,
// which Submit sends), as the export carries it.
func prRecord(t *testing.T, q types.Qso, logbook string) prWireRow {
	t.Helper()
	act := forwarding.Action(action.Insert)
	if !q.DeletedAt.IsZero() {
		act = action.Delete
	}
	up, err := smcloud.UploadFor(q, act)
	if err != nil {
		t.Fatal(err)
	}
	return prWireRow{UUID: q.UUID, LogbookUUID: logbook, ModifiedAt: up.ModifiedAt, Revision: up.Revision, DeletedAt: up.DeletedAt, Qso: up.Qso}
}

// prDocs builds the listing and the export of one archive.
func prDocs(t *testing.T, label string, legacy bool, lbs ...prLogbook) (listing, export string, rows []prWireRow) {
	t.Helper()
	wireLBs := []prWireLogbook{}
	for _, lb := range lbs {
		w := prWireLogbook{UUID: lb.uuid, Label: lb.label, Callsign: lb.callsign}
		for _, q := range lb.rows {
			rows = append(rows, prRecord(t, q, lb.uuid))
			w.Count++
			if !q.DeletedAt.IsZero() {
				w.Tombstones++
			}
		}
		wireLBs = append(wireLBs, w)
	}
	head := map[string]any{"uuid": prArchive, "label": label, "legacy": legacy}
	l, _ := json.Marshal(map[string]any{"archives": []any{map[string]any{"uuid": prArchive, "label": label, "legacy": legacy, "logbooks": wireLBs}}})
	if rows == nil {
		rows = []prWireRow{}
	}
	e, _ := json.Marshal(map[string]any{"archive": head, "logbooks": wireLBs, "qsos": rows})
	return string(l), string(e), rows
}

// prStandard: Contest (a live row and a tombstone), Portable (a live row,
// the default), Empty.
func prStandard(t *testing.T, mutate func(*prLogbook, int)) (string, string) {
	t.Helper()
	lbs := []prLogbook{
		{prLBA, "Contest", "7Q5MLV", []types.Qso{prQso(prQ1, "DL9UW", 3, false), prQso(prQ2, "EA1B", 4, true)}},
		{prLBB, "Portable", "7Q5MLV/P", []types.Qso{prQso(prQ3, "OH2BH", 1, false)}},
		{prLBC, "Empty", "7Q5MLV", nil},
	}
	if mutate != nil {
		for i := range lbs {
			mutate(&lbs[i], i)
		}
	}
	l, e, _ := prDocs(t, "Field Day", false, lbs...)
	return l, e
}

// prCloud is a fake SM Cloud at archive_export 1 serving one listing and one
// archive's export, recording every request.
type prCloud struct {
	srv             *httptest.Server
	mu              sync.Mutex
	reqs            []string
	listing, export string
	// version is GET /v1/version's body; exportStatus, when set, the export's
	// status (with its body).
	version      string
	exportStatus int
}

func newPRCloud(t *testing.T, listing, export string) *prCloud {
	t.Helper()
	c := &prCloud{listing: listing, export: export, version: `{"version":"test","identity_protocol":1,"archive_export":1}`}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(c.version)) })
	mux.HandleFunc("GET /v1/archives", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(c.listing)) })
	mux.HandleFunc("GET /v1/archives/"+prArchive+"/export", func(w http.ResponseWriter, _ *http.Request) {
		if c.exportStatus != 0 {
			w.WriteHeader(c.exportStatus)
		}
		_, _ = w.Write([]byte(c.export))
	})
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer "+prToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		c.mu.Lock()
		c.reqs = append(c.reqs, r.Method+" "+r.URL.Path)
		c.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *prCloud) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.reqs)
}

// prBed is a stopped station: config.json in its own directory, the data
// directory apart, Home (legacy, active) with its file, the SM Cloud station
// account, and file logging on.
type prBed struct {
	tmp, etc, data, cfgPath, home string
	managed, wFile                string
}

func newPRBed(t *testing.T, url string, mut func(*config.Config)) *prBed {
	t.Helper()
	b := &prBed{tmp: t.TempDir()}
	b.etc, b.data = filepath.Join(b.tmp, "etc"), filepath.Join(b.tmp, "data")
	for _, d := range []string{b.etc, b.data} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	b.home = filepath.Join(b.data, "home.db")
	if err := os.WriteFile(b.home, []byte("Home's file"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(b.data)
	cfg.SetupComplete = true
	cfg.LoggingStation.StationCallsign = "7Q5MLV"
	cfg.Logging.FileLogging = true
	cfg.QsoArchives = []types.QsoArchiveConfig{
		{ID: prHomeID, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: b.home},
		{ID: prWID, Label: "W", Ownership: types.QsoArchiveOwnershipManaged},
	}
	cfg.ActiveQsoArchiveID = prHomeID
	creds, _ := json.Marshal(map[string]string{"url": url, "token": prToken})
	cfg.Forwarders = []types.ForwarderConfig{{Name: "smcloud", Type: smcloud.Type, Credentials: creds}}
	if mut != nil {
		mut(&cfg)
	}
	b.managed = filepath.Join(b.data, "db", "qso-archives")
	if err := os.MkdirAll(b.managed, 0o700); err != nil {
		t.Fatal(err)
	}
	b.wFile = filepath.Join(b.managed, prWID+".db")
	if err := os.WriteFile(b.wFile, []byte("W's file"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.cfgPath = filepath.Join(b.etc, "config.json")
	if _, err := config.WriteJSON(b.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	return b
}

// tree records every file and directory under root by mode and content.
func prTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			out[p] = "dir " + fi.Mode().String()
			return nil
		}
		if fi.Mode().Perm()&0o400 == 0 {
			out[p] = fmt.Sprintf("%s unreadable %d %d", fi.Mode(), fi.Size(), fi.ModTime().UnixNano())
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[p] = fmt.Sprintf("%s %x", fi.Mode(), sha256.Sum256(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func prSameTree(t *testing.T, where string, before, after map[string]string) {
	t.Helper()
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("%s: %s changed (%q → %q)", where, p, v, after[p])
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Fatalf("%s: %s appeared", where, p)
		}
	}
}

// prWithoutLogs drops the log directory and everything in it: a real run may
// write its ordinary log, and nothing else.
func prWithoutLogs(b *prBed, tree map[string]string) map[string]string {
	logDir := filepath.Join(b.data, config.DefaultConfig(b.data).Logging.RelLogFileDir)
	for p := range tree {
		if p == logDir || strings.HasPrefix(p, logDir+string(filepath.Separator)) {
			delete(tree, p)
		}
	}
	return tree
}

// prRun runs `smd provision args...` through the subcommand dispatch, as
// main does, and returns the exit code, stdout and stderr.
func prRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var code int
	var handled bool
	stdout, stderr := captureOutput(t, func() {
		code, handled = dispatchSubcommand(append([]string{"smd", "provision"}, args...))
	})
	if !handled {
		t.Fatal("smd provision is not a subcommand")
	}
	return code, stdout, stderr
}

// captureOutput runs fn with os.Stdout and os.Stderr captured.
func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	re, we, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wo, we
	var out, errOut []byte
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); out, _ = io.ReadAll(ro) }()
	go func() { defer wg.Done(); errOut, _ = io.ReadAll(re) }()
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
	}()
	fn()
	_ = wo.Close()
	_ = we.Close()
	wg.Wait()
	return string(out), string(errOut)
}

// prLogs is every log file written under root.
func prLogs(t *testing.T, root string) string {
	t.Helper()
	var all strings.Builder
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".log") {
			b, _ := os.ReadFile(p)
			all.Write(b)
		}
		return nil
	})
	return all.String()
}

func TestProvisionCommand_PR1_DispatchAndFlags(t *testing.T) {
	if _, handled := dispatchSubcommand([]string{"smd", "--config", "x"}); handled {
		t.Fatal("the daemon's own flags were taken for a subcommand")
	}
	l, e := prStandard(t, nil)
	for _, c := range []struct {
		name string
		args []string
		want string // in stderr, after "smd: "
	}{
		{"no archive and no list", nil, "--archive UUID or --list is required"},
		{"list with an archive", []string{"--list", "--archive", prArchive}, "--list takes no flag but --config"},
		{"list with a dry run", []string{"--list", "--dry-run"}, "--list takes no flag but --config"},
		{"list with a default logbook", []string{"--list", "--default-logbook", prLBB}, "--list takes no flag but --config"},
		{"a dry run without an archive", []string{"--dry-run"}, "--archive UUID or --list is required"},
		{"a default logbook without an archive", []string{"--default-logbook", prLBB}, "--archive UUID or --list is required"},
		{"an archive that is not a UUID", []string{"--archive", "Field Day"}, "--archive must be a UUIDv7"},
		{"an archive that is a UUIDv4", []string{"--archive", "0197f9a0-0000-4000-8000-0000000f1001"}, "--archive must be a UUIDv7"},
		{"a default logbook that is not a UUID", []string{"--archive", prArchive, "--default-logbook", "Portable"}, "--default-logbook must be a UUIDv7"},
		{"a positional argument", []string{"--archive", prArchive, "extra"}, "unexpected argument"},
		{"an unknown flag", []string{"--archive", prArchive, "--force"}, "flag provided but not defined"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cloud := newPRCloud(t, l, e)
			b := newPRBed(t, cloud.srv.URL, nil)
			before := prTree(t, b.tmp)
			code, stdout, stderr := prRun(t, append([]string{"--config", b.cfgPath}, c.args...)...)
			if code != ExitError || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d, stderr %q; want %d with %q", code, stderr, ExitError, c.want)
			}
			if strings.Contains(stdout, "provisioned") {
				t.Fatalf("stdout %q claims an outcome", stdout)
			}
			if got := cloud.requests(); len(got) != 0 {
				t.Fatalf("requests %v; a refused invocation asks nothing", got)
			}
			prSameTree(t, c.name, before, prTree(t, b.tmp))
		})
	}
	t.Run("success exits 0", func(t *testing.T) {
		cloud := newPRCloud(t, l, e)
		b := newPRBed(t, cloud.srv.URL, nil)
		if code, _, stderr := prRun(t, "--config", b.cfgPath, "--list"); code != 0 {
			t.Fatalf("--list exit %d (%s); want 0", code, stderr)
		}
	})
}

func TestProvisionCommand_PR2_ListAndDryRunChangeNothing(t *testing.T) {
	l, e := prStandard(t, nil)
	for _, c := range []struct {
		name     string
		args     []string
		requests []string
		stdout   []string
	}{
		{"list", []string{"--list"}, []string{"GET /v1/version", "GET /v1/archives"}, []string{
			"archive " + prArchive + `  "Field Day"  here: not catalogued`,
			"  logbook " + prLBA + `  "Contest"  7Q5MLV  2 QSOs (1 tombstone)`,
			"  logbook " + prLBB + `  "Portable"  7Q5MLV/P  1 QSO`,
			"  logbook " + prLBC + `  "Empty"  7Q5MLV  0 QSOs`,
		}},
		{"dry run", []string{"--archive", prArchive, "--default-logbook", prLBB, "--dry-run"},
			[]string{"GET /v1/version", "GET /v1/archives/" + prArchive + "/export"}, []string{
				"archive " + prArchive + ` "Field Day" would be provisioned at `,
				"catalogue: a new inactive entry",
				"  logbook " + prLBA + `  "Contest"  7Q5MLV  2 QSOs (1 tombstone)`,
				"  logbook " + prLBB + `  "Portable"  7Q5MLV/P  1 QSO  (default)`,
				"  logbook " + prLBC + `  "Empty"  7Q5MLV  0 QSOs`,
				"dry run: nothing was written. Database construction and round-trip compatibility were not tested.",
			}},
	} {
		for _, defaultPath := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, default config path %v", c.name, defaultPath), func(t *testing.T) {
				cloud := newPRCloud(t, l, e)
				b := newPRBed(t, cloud.srv.URL, nil)
				args := c.args
				if defaultPath {
					t.Setenv("SM_WORKING_DIR", b.etc)
				} else {
					t.Setenv("SM_WORKING_DIR", filepath.Join(b.tmp, "never-created"))
					args = append([]string{"--config", b.cfgPath}, args...)
				}
				before := prTree(t, b.tmp)
				watch := watchOpens(t, b.data, filepath.Dir(b.managed), b.managed)
				code, stdout, stderr := prRun(t, args...)
				if opened := watch.opened(t); len(opened) != 0 {
					t.Fatalf("files opened in the archive directories: %v; --list and --dry-run open no archive", opened)
				}
				if code != 0 {
					t.Fatalf("exit %d, stderr %q; want 0", code, stderr)
				}
				for _, line := range c.stdout {
					if !strings.Contains(stdout, line) {
						t.Fatalf("stdout:\n%s\nwant the line %q", stdout, line)
					}
				}
				if got := cloud.requests(); !slices.Equal(got, c.requests) {
					t.Fatalf("requests %v; want exactly %v", got, c.requests)
				}
				prSameTree(t, c.name, before, prTree(t, b.tmp))
			})
		}
	}
}

func TestProvisionCommand_PR2_TheOpenWatchSeesAnOpen(t *testing.T) {
	b := newPRBed(t, "http://127.0.0.1:1", nil)
	watch := watchOpens(t, b.data, filepath.Dir(b.managed), b.managed)
	for _, p := range []string{b.home, b.wFile} {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	opened := watch.opened(t)
	if !slices.Contains(opened, b.home) || !slices.Contains(opened, b.wFile) {
		t.Fatalf("the watch saw %v; want both archive files' opens (the control)", opened)
	}
}

func TestProvisionCommand_PR3_AMissingConfigCreatesNothing(t *testing.T) {
	modes := map[string][]string{
		"list":      {"--list"},
		"dry run":   {"--archive", prArchive, "--default-logbook", prLBB, "--dry-run"},
		"provision": {"--archive", prArchive, "--default-logbook", prLBB},
	}
	for mode, modeArgs := range modes {
		for _, explicit := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, explicit %v", mode, explicit), func(t *testing.T) {
				tmp := t.TempDir()
				absent := filepath.Join(tmp, "absent")
				want := filepath.Join(absent, "config.json")
				args := slices.Clone(modeArgs)
				if explicit {
					args = append([]string{"--config", want}, args...)
					t.Setenv("SM_WORKING_DIR", filepath.Join(tmp, "elsewhere"))
				} else {
					t.Setenv("SM_WORKING_DIR", absent)
				}
				code, _, stderr := prRun(t, args...)
				if code != ExitError || !strings.Contains(stderr, want) {
					t.Fatalf("exit %d, stderr %q; want an error naming %s", code, stderr, want)
				}
				if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
					t.Fatalf("%v created; a missing config creates nothing", entries)
				}
			})
		}
	}
}

func TestProvisionCommand_PR4_RefusalsCarryNoValues(t *testing.T) {
	legacyListing, legacyExport, _ := prDocs(t, "Home", true, prLogbook{prLBA, "Default", "7Q5MLV", []types.Qso{prQso(prQ1, "DL9UW", 1, false)}})
	unknownListing, unknownExport := prStandard(t, nil)
	{
		var doc map[string]json.RawMessage
		_ = json.Unmarshal([]byte(unknownExport), &doc)
		var rows []map[string]json.RawMessage
		_ = json.Unmarshal(doc["qsos"], &rows)
		var payload map[string]json.RawMessage
		_ = json.Unmarshal(rows[0]["qso"], &payload)
		payload["x_future"] = json.RawMessage(`{"note":"` + prSecret + `"}`)
		rows[0]["qso"], _ = json.Marshal(payload)
		doc["qsos"], _ = json.Marshal(rows)
		b, _ := json.Marshal(doc)
		unknownExport = string(b)
	}
	freqListing, freqExport := prStandard(t, func(lb *prLogbook, i int) {
		if i == 1 {
			q := prQso("0197f9a0-0000-7000-8000-0000000f1104", "K1ABC", 1, false)
			q.QsoDetails.Freq = prBadFreq
			lb.rows = append(lb.rows, q)
		}
	})
	for _, c := range []struct {
		name            string
		listing, export string
		args            []string
		want            []string
		values          []string
	}{
		{"the legacy archive, dry run", legacyListing, legacyExport, []string{"--dry-run"}, []string{"legacy_archive"}, []string{"DL9UW"}},
		{"the legacy archive", legacyListing, legacyExport, nil, []string{"legacy_archive"}, []string{"DL9UW"}},
		{"a payload key this build does not model", unknownListing, unknownExport, []string{"--default-logbook", prLBB},
			[]string{"do not round-trip", prQ1, "qso.x_future"}, []string{prSecret, "DL9UW"}},
		{"a frequency the restore adapter refuses", freqListing, freqExport, []string{"--default-logbook", prLBB},
			[]string{"0197f9a0-0000-7000-8000-0000000f1104", "restore adapter refused it"}, []string{prBadFreq, "K1ABC"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cloud := newPRCloud(t, c.listing, c.export)
			b := newPRBed(t, cloud.srv.URL, nil)
			before := prTree(t, b.tmp)
			code, stdout, stderr := prRun(t, append([]string{"--config", b.cfgPath, "--archive", prArchive}, c.args...)...)
			if code != ExitError {
				t.Fatalf("exit %d; want %d (stdout %q, stderr %q)", code, ExitError, stdout, stderr)
			}
			for _, s := range c.want {
				if !strings.Contains(stderr, s) {
					t.Fatalf("stderr %q does not name %s", stderr, s)
				}
			}
			logs := prLogs(t, b.tmp)
			for _, v := range c.values {
				if strings.Contains(stdout, v) || strings.Contains(stderr, v) || strings.Contains(logs, v) {
					t.Fatalf("the value %q leaked (stdout %q, stderr %q, logs %q)", v, stdout, stderr, logs)
				}
			}
			// The catalogue, every archive file and the managed directory are
			// as they were: only an ordinary log may have been written.
			prSameTree(t, c.name, before, prWithoutLogs(b, prTree(t, b.tmp)))
		})
	}
}

func TestProvisionCommand_PR5_ANewEntryIsReported(t *testing.T) {
	l, e := prStandard(t, nil)
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprintf("durability uncertain %v", uncertain), func(t *testing.T) {
			cloud := newPRCloud(t, l, e)
			b := newPRBed(t, cloud.srv.URL, nil)
			if uncertain {
				// config.json's directory without read permission: the rename
				// lands, its directory sync fails.
				if err := os.Chmod(b.etc, 0o300); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(b.etc, 0o700) })
			}
			code, stdout, stderr := prRun(t, "--config", b.cfgPath, "--archive", prArchive, "--default-logbook", prLBB)
			_ = os.Chmod(b.etc, 0o700)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q; want 0", code, stderr)
			}
			path := filepath.Join(b.data, "db", "qso-archives", prArchive+".db")
			for _, line := range []string{
				"provisioned archive " + prArchive + " at " + path + ": 3 QSOs in 3 logbooks",
				"catalogue: a new inactive entry",
			} {
				if !strings.Contains(stdout, line) {
					t.Fatalf("stdout:\n%s\nwant %q", stdout, line)
				}
			}
			warned := strings.Contains(stdout, "warning: durability uncertain")
			if warned != uncertain {
				t.Fatalf("stdout:\n%s\ndurability warning %v; want %v", stdout, warned, uncertain)
			}
			cfg, err := config.Load(b.cfgPath)
			if err != nil || cfg.QsoArchiveByID(prArchive) == nil || cfg.ActiveQsoArchiveID != prHomeID {
				t.Fatalf("catalogue on disk %+v (%v); want U added, Home still active", cfg.QsoArchives, err)
			}
			if logs := prLogs(t, b.tmp); strings.Contains(logs, "DL9UW") || !strings.Contains(logs, prArchive) {
				t.Fatalf("the log %q; want the outcome logged, by id, without values", logs)
			}
		})
	}
}

func TestProvisionCommand_PR6_TheReportStatesEveryCaveat(t *testing.T) {
	kept := types.QsoArchiveConfig{ID: prArchive, Label: "Kept label", Ownership: types.QsoArchiveOwnershipManaged}
	res := archive.ProvisionResult{
		Plan:                archive.ProvisionPlan{ArchiveUUID: prArchive, Path: "/data/db/qso-archives/" + prArchive + ".db", Entry: &kept},
		Entry:               kept,
		DurabilityUncertain: true,
		LeftoverTemp:        "/data/db/qso-archives/" + prArchive + ".db.creating",
	}
	var out strings.Builder
	reportProvision(&out, res, 3, 3, true)
	for _, line := range []string{
		"provisioned archive " + prArchive + " at /data/db/qso-archives/" + prArchive + ".db: 3 QSOs in 3 logbooks",
		`catalogue: the existing entry "Kept label" kept, active`,
		"warning: durability uncertain: a directory sync failed after the archive was published; it is in place",
		"warning: the temporary name /data/db/qso-archives/" + prArchive + ".db.creating could not be removed; the next start reports it",
	} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("report:\n%s\nwant %q", out.String(), line)
		}
	}
}

func TestProvisionCommand_PR7_EachFailureBoundaryReachesTheCommand(t *testing.T) {
	l, e := prStandard(t, nil)
	residue := prArchive + ".db.creating"
	residuePath := func(b *prBed) string { return filepath.Join(b.managed, residue) }
	for _, c := range []struct {
		name     string
		arrange  func(t *testing.T, cloud *prCloud, b *prBed)
		mut      func(*config.Config)
		want     func(b *prBed) []string
		requests []string
	}{
		{name: "no station account", mut: func(c *config.Config) { c.Forwarders = nil },
			want: func(*prBed) []string { return []string{"no SM Cloud station account"} }, requests: []string{}},
		{name: "a server without archive_export",
			arrange: func(t *testing.T, cloud *prCloud, _ *prBed) {
				cloud.version = `{"version":"old","identity_protocol":1}`
			},
			want: func(*prBed) []string { return []string{"does not support whole-archive export"} }, requests: []string{"GET /v1/version"}},
		{name: "a rejected token",
			arrange: func(t *testing.T, cloud *prCloud, _ *prBed) {
				cloud.exportStatus = http.StatusUnauthorized
				cloud.export = `{"code":"unauthorized","message":"` + prToken + `"}`
			},
			want: func(*prBed) []string { return []string{"401"} }, requests: []string{"GET /v1/version", "GET /v1/archives/" + prArchive + "/export"}},
		{name: "an incomplete export",
			arrange: func(t *testing.T, cloud *prCloud, _ *prBed) { cloud.export = cloud.export[:len(cloud.export)/2] },
			want:    func(*prBed) []string { return []string{"not a complete"} }, requests: []string{"GET /v1/version", "GET /v1/archives/" + prArchive + "/export"}},
		{name: "residue at U's path",
			arrange: func(t *testing.T, _ *prCloud, b *prBed) {
				if err := os.WriteFile(residuePath(b), []byte("residue"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: func(b *prBed) []string { return []string{"temp_file_exists", residuePath(b)} }, requests: []string{"GET /v1/version", "GET /v1/archives/" + prArchive + "/export"}},
		{name: "a fatal catalogue write",
			arrange: func(t *testing.T, _ *prCloud, b *prBed) {
				if err := os.Chmod(b.etc, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(b.etc, 0o700) })
			},
			want: func(*prBed) []string { return []string{"record the archive in the catalogue"} }, requests: []string{"GET /v1/version", "GET /v1/archives/" + prArchive + "/export"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cloud := newPRCloud(t, l, e)
			b := newPRBed(t, cloud.srv.URL, c.mut)
			if c.arrange != nil {
				c.arrange(t, cloud, b)
			}
			before := prTree(t, b.tmp)
			// The fatal catalogue case's 0500 stays until its cleanup: the
			// comparison below is against the tree as it was arranged.
			code, stdout, stderr := prRun(t, "--config", b.cfgPath, "--archive", prArchive, "--default-logbook", prLBB)
			if code != ExitError {
				t.Fatalf("exit %d (stdout %q, stderr %q); want %d", code, stdout, stderr, ExitError)
			}
			for _, s := range c.want(b) {
				if !strings.Contains(stderr, s) {
					t.Fatalf("stderr %q does not name %s", stderr, s)
				}
			}
			if strings.Contains(stdout, "provisioned") {
				t.Fatalf("stdout %q claims an outcome", stdout)
			}
			if got := cloud.requests(); !slices.Equal(got, c.requests) && !(len(got) == 0 && len(c.requests) == 0) {
				t.Fatalf("requests %v; want %v", got, c.requests)
			}
			logs := prLogs(t, b.tmp)
			for _, v := range []string{"DL9UW", "OH2BH", prToken} {
				if strings.Contains(stdout, v) || strings.Contains(stderr, v) || strings.Contains(logs, v) {
					t.Fatalf("the value %q leaked (stdout %q, stderr %q, logs %q)", v, stdout, stderr, logs)
				}
			}
			// The station is as it was: only an ordinary log may have been written.
			prSameTree(t, c.name, before, prWithoutLogs(b, prTree(t, b.tmp)))
		})
	}
}
