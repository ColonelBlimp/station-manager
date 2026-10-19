package archive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// W-0021 Q7 commit 5 (ADR 0071 refinement 2026-10-16; operator direction
// 2026-10-19): Manager.Provision re-creates cloud archive U as a new managed
// file from its validated whole-archive export.
//
//	PV1 every preflight refusal, through Provision and through
//	    PreflightProvision alike, happens before the temporary file exists: no
//	    .creating is ever created (closeFile never runs), the managed
//	    directory is not made, no row is restored, and the data directory and
//	    config.json are byte-identical. Residue refusals name the exact path;
//	    structural export defects (invalid or duplicate UUIDs, a membership
//	    index outside the logbooks, missing metadata, counts that disagree, a
//	    payload naming another QSO) are invalid_export with safe context; no
//	    refusal carries a value. Only Provision needs the restorer.
//	PV2 PreflightProvision (the dry run) reports the plan and creates nothing.
//	PV3 a new entry: the file carries U, the cloud's logbook UUIDs, labels
//	    and callsigns, the chosen default, every row (revision, modified_at,
//	    tombstone) in its logbook, the log-only schema, 0600, no bindings with
//	    the seed decision recorded, no upload queued, and nothing else in the
//	    managed directory; the catalogue gains one inactive managed entry; the
//	    active and pending selections are unchanged; U's summary matches the
//	    file exactly and Home's is kept.
//	PV4 an existing managed entry whose file is missing (here the active
//	    archive): its fields and both selections are unchanged and config.json
//	    is not written.
//	PV5 rows that do not round-trip through the restore adapter, the worker's
//	    read and the forwarder's projection refuse during construction, before
//	    placement: corrected coordinates (live and tombstone), a payload key
//	    this build does not model, a sub-second modified_at; and, through a
//	    controlled readback mismatch, modified_at, revision, deleted_at (moved,
//	    dropped, added), membership and identity. Every mismatching row is
//	    reported, in export order. The error and the log name each UUID and
//	    field and never a value; nothing is left.
//	PV6 a row the restore adapter refuses (a local constraint) refuses the
//	    construction, naming the row, and nothing is left.
//	PV7 a destination that appears after preflight is never replaced: the
//	    link refuses, the foreign file is untouched, nothing of ours is left.
//	PV8 the link succeeds and the temporary name cannot be removed: before
//	    publication (a new entry) the placed file is removed and the creation
//	    refused; after it (an existing entry) the archive stands: both names
//	    are one file carrying U, the catalogue and selections are unchanged,
//	    the leftover name is reported and logged, and one trace shows the
//	    unlink attempted with the file placed and the whole durability barrier
//	    after it, its failure reported as uncertainty.
//	PV9 a directory sync failing after the link: a new entry is refused and
//	    removed; an existing entry stands, durability uncertain.
//	PV10 the catalogue write DurabilityUncertain: the new entry stands, in
//	    memory and on disk, the caveat reported and logged.
//	PV11 the catalogue write failing: the placed file is removed, nothing
//	    left.
//	PV12 a rollback whose cleanup cannot remove this invocation's files keeps
//	    the original cause first and names and logs each leftover, sidecars
//	    included, which stays unlisted, not activatable, and diagnosed at the
//	    next start.
//	PV13 a restore refusal names the row and carries no value, in its text,
//	    its chain, or the log the daemon writes from it.
//	PV14 the startup diagnosis reports the sidecars of unlisted and unfinished
//	    files, never a catalogued archive's own WAL or SHM.
//
// Commit 4's characterization of Create (manager_test.go) is unchanged.

const (
	pvArchive = "0197f9a0-0000-7000-8000-0000000e0001"
	pvLBA     = "0197f9a0-0000-7000-8000-0000000e00a1"
	pvLBB     = "0197f9a0-0000-7000-8000-0000000e00b1"
	pvLBC     = "0197f9a0-0000-7000-8000-0000000e00c1"
	pvQ1      = "0197f9a0-0000-7000-8000-0000000e0101"
	pvQ2      = "0197f9a0-0000-7000-8000-0000000e0102"
	pvQ3      = "0197f9a0-0000-7000-8000-0000000e0103"
	pvQ4      = "0197f9a0-0000-7000-8000-0000000e0104"
	pvV4      = "0197f9a0-0000-4000-8000-0000000e0f99"
	// pvPending is another managed archive, the pending selection.
	pvPending = "0197f9a0-0000-7000-8000-0000000e0f01"

	// The secret values a refusal must never print.
	pvFarLat   = "51.500000"
	pvFarLon   = "-0.100000"
	pvUnknown  = "PV-UNMODELLED-VALUE"
	pvSubMicro = ".123456"
)

var (
	pvAt     = time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	pvGoneAt = time.Date(2026, 7, 17, 7, 0, 0, 0, time.UTC)
)

// pvQso is a QSO whose coordinates agree with its grid, so the restore
// adapter keeps it as it is.
func pvQso(uuid, call string, revision int64) types.Qso {
	q := types.Qso{UUID: uuid, ModifiedAt: pvAt, Revision: revision}
	q.ContactedStation.Call = call
	q.ContactedStation.Country = "Ukraine"
	q.ContactedStation.Gridsquare = "KN28"
	q.ContactedStation.Lat = "48.500000"
	q.ContactedStation.Lon = "25.000000"
	q.QsoDetails.Band = "20m"
	q.QsoDetails.Mode = "SSB"
	q.QsoDetails.Freq = "14.255"
	q.QsoDetails.QsoDate = "20260717"
	q.QsoDetails.TimeOn = "142559"
	q.QsoDetails.TimeOff = "142559"
	q.QsoDetails.RstSent = "59"
	q.QsoDetails.RstRcvd = "57"
	q.LoggingStation.StationCallsign = "7Q5MLV"
	return q
}

func pvTombstone(uuid, call string, revision int64) types.Qso {
	q := pvQso(uuid, call, revision)
	q.ModifiedAt, q.DeletedAt = pvGoneAt, pvGoneAt
	return q
}

// pvPushed is the record a real push of q puts on the wire, captured from the
// forwarder's PUT body: exactly what the cloud stores and exports.
func pvPushed(t *testing.T, q types.Qso) smcloud.ArchiveExportRecord {
	t.Helper()
	act := forwarding.Action(action.Insert)
	if !q.DeletedAt.IsZero() {
		act = action.Delete
	}
	var body []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"received":1,"applied":1}`))
	}))
	defer ts.Close()
	creds, _ := json.Marshal(map[string]string{"url": ts.URL, "token": "tok"})
	f, err := smcloud.New(types.ForwarderConfig{Name: "smcloud", Type: smcloud.Type, Credentials: creds})
	if err != nil {
		t.Fatal(err)
	}
	if res := f.Submit(context.Background(), q, act, ""); res.Err != nil {
		t.Fatalf("push %s: %v", q.UUID, res.Err)
	}
	var req struct {
		Qsos []struct {
			ModifiedAt time.Time       `json:"modified_at"`
			Revision   int64           `json:"revision"`
			DeletedAt  *time.Time      `json:"deleted_at"`
			Qso        json.RawMessage `json:"qso"`
		} `json:"qsos"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Qsos) != 1 {
		t.Fatalf("push body %s: %v", body, err)
	}
	r := req.Qsos[0]
	return smcloud.ArchiveExportRecord{UUID: q.UUID, ModifiedAt: r.ModifiedAt, Revision: r.Revision, DeletedAt: r.DeletedAt, Qso: r.Qso}
}

type pvLogbook struct {
	uuid, label, callsign string
	rows                  []smcloud.ArchiveExportRecord
}

// pvExport assembles a validated-shape export: each row's Logbook index and
// each logbook's counts follow from the rows given.
func pvExport(label string, legacy bool, lbs ...pvLogbook) *smcloud.ArchiveExport {
	e := &smcloud.ArchiveExport{CloudArchive: smcloud.CloudArchive{UUID: pvArchive, Label: label, Legacy: legacy, Logbooks: []smcloud.CloudArchiveLogbook{}}, Qsos: []smcloud.ArchiveExportRecord{}}
	for i, lb := range lbs {
		uuid := lb.uuid
		cl := smcloud.CloudArchiveLogbook{UUID: &uuid, Label: lb.label, Callsign: lb.callsign}
		for _, r := range lb.rows {
			r.Logbook = i
			e.Qsos = append(e.Qsos, r)
			cl.Count++
			if r.DeletedAt != nil {
				cl.Tombstones++
			}
		}
		e.Logbooks = append(e.Logbooks, cl)
	}
	return e
}

// pvStandard is the archive most tests provision: Contest with a live row and
// a tombstone, Portable with a live row (the default), Empty with none.
func pvStandard(t *testing.T) *smcloud.ArchiveExport {
	t.Helper()
	return pvExport("Field Day", false,
		pvLogbook{pvLBA, "Contest", "7Q5MLV", []smcloud.ArchiveExportRecord{pvPushed(t, pvQso(pvQ1, "DL9UW", 3)), pvPushed(t, pvTombstone(pvQ2, "EA1B", 4))}},
		pvLogbook{pvLBB, "Portable", "7Q5MLV/P", []smcloud.ArchiveExportRecord{pvPushed(t, pvQso(pvQ3, "OH2BH", 1))}},
		pvLogbook{pvLBC, "Empty", "7Q5MLV", nil},
	)
}

// pvFixture is a manager wired with the real restore adapter, Home active,
// and counters on the construction's first step and on the restores.
type pvFixture struct {
	m        *Manager
	cfgSvc   *config.Service
	log      *strings.Builder
	creates  int
	restores int
	// onRestore, when set, runs before each restore.
	onRestore func()
	// tamper, when set, changes what a restore writes (a controlled readback
	// mismatch).
	tamper func(q *types.Qso, logbookID *int64)
	// homeSummary is Home's summary as seeded; provisioning must keep it.
	homeSummary ArchiveSummary
}

func newPVFixture(t *testing.T) *pvFixture {
	t.Helper()
	m, cfgSvc, buf := testManager(t)
	seedHome(t, cfgSvc)
	// A pending selection, so clearing it is caught, and Home's summary, so
	// provisioning U must keep another archive's record.
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = append(c.QsoArchives, types.QsoArchiveConfig{ID: pvPending, Label: "Pending", Ownership: types.QsoArchiveOwnershipManaged})
		c.PendingQsoArchiveID = pvPending
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snap := cfgSvc.Snapshot()
	if err := os.MkdirAll(GlobalDir(snap), 0o700); err != nil {
		t.Fatal(err)
	}
	homeLogbooks := []LogbookSummary{{UUID: "0197f9a0-0000-7000-8000-0000000e0f02", Name: "Home log", Callsign: "7Q5MLV", QsoCount: 5, Default: true}}
	if err := RecordClosedArchive(SummariesPath(snap), idA, filepath.Join(snap.DataDir, "home.db"), homeLogbooks, pvAt); err != nil {
		t.Fatal(err)
	}
	sums, _ := ReadSummaries(SummariesPath(snap))
	fx := &pvFixture{m: m, cfgSvc: cfgSvc, log: buf, homeSummary: sums[idA]}
	orig := closeFile
	t.Cleanup(func() { closeFile = orig })
	closeFile = func(f *os.File) error {
		fx.creates++ // runs only after the .creating file was created
		return orig(f)
	}
	m.SetRowRestorer(func(ctx context.Context, db *sqlite.Service, logbookID int64, q types.Qso) error {
		fx.restores++
		if fx.onRestore != nil {
			fx.onRestore()
		}
		if fx.tamper != nil {
			fx.tamper(&q, &logbookID)
		}
		svc := &qsoservice.Service{DB: db, Logger: m.logger}
		st, err := svc.Restore(ctx, logbookID, q)
		if err == nil && st != qsoservice.RestoreStored {
			return fmt.Errorf("qso %s was already present", q.UUID)
		}
		return err
	})
	return fx
}

func (fx *pvFixture) req(e *smcloud.ArchiveExport, def string) ProvisionRequest {
	return ProvisionRequest{Export: e, DefaultLogbook: def}
}

func (fx *pvFixture) final() string {
	return filepath.Join(ManagedDir(fx.cfgSvc.Snapshot()), pvArchive+".db")
}

// dataTree records every file under the data directory and config.json, by
// content and mode, so "nothing changed" is checked byte for byte.
func (fx *pvFixture) dataTree(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	add := func(p string) {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if fi.IsDir() {
			out[p] = "dir " + fi.Mode().String()
			return
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		out[p] = fmt.Sprintf("%s %x", fi.Mode(), sha256.Sum256(b))
	}
	data := fx.cfgSvc.Snapshot().DataDir
	if err := filepath.WalkDir(data, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		add(p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	add(fx.cfgSvc.Path)
	return out
}

func wantSameTree(t *testing.T, where string, before, after map[string]string) {
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

func wantRefusal(t *testing.T, err error, code string) *ProvisionRefusal {
	t.Helper()
	var r *ProvisionRefusal
	if !stderr.As(err, &r) || r.Code != code {
		t.Fatalf("err = %v; want a ProvisionRefusal %s", err, code)
	}
	return r
}

func TestProvision_PV1_PreflightRefusesBeforeTheTemporaryFile(t *testing.T) {
	type arrange func(t *testing.T, fx *pvFixture) ProvisionRequest
	withFile := func(name string) arrange {
		return func(t *testing.T, fx *pvFixture) ProvisionRequest {
			if err := os.MkdirAll(ManagedDir(fx.cfgSvc.Snapshot()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ManagedDir(fx.cfgSvc.Snapshot()), name), []byte("residue"), 0o600); err != nil {
				t.Fatal(err)
			}
			return fx.req(pvStandard(t), pvLBB)
		}
	}
	withEntry := func(e types.QsoArchiveConfig) arrange {
		return func(t *testing.T, fx *pvFixture) ProvisionRequest {
			if _, err := fx.cfgSvc.Update(func(c *config.Config) error { c.QsoArchives = append(c.QsoArchives, e); return nil }); err != nil {
				t.Fatal(err)
			}
			return fx.req(pvStandard(t), pvLBB)
		}
	}
	export := func(mutate func(e *smcloud.ArchiveExport), def string) arrange {
		return func(t *testing.T, fx *pvFixture) ProvisionRequest {
			e := pvStandard(t)
			mutate(e)
			return fx.req(e, def)
		}
	}
	str := func(s string) *string { return &s }
	elsewhere := filepath.Join(os.TempDir(), "pv-external.db")
	for _, c := range []struct {
		name, code string
		arrange    arrange
		// residue is the file a residue refusal must name, in the managed
		// directory; context is what the message must name (never a value).
		residue string
		context []string
	}{
		{name: "no export", code: "no_export", arrange: func(t *testing.T, fx *pvFixture) ProvisionRequest { return fx.req(nil, pvLBB) }},
		{name: "the legacy marker", code: "legacy_archive", arrange: export(func(e *smcloud.ArchiveExport) { e.Legacy = true }, pvLBB)},
		{name: "no logbook", code: "no_logbooks", arrange: func(t *testing.T, fx *pvFixture) ProvisionRequest { return fx.req(pvExport("Field Day", false), "") }},
		{name: "several logbooks, no default", code: "default_logbook_required", arrange: export(func(*smcloud.ArchiveExport) {}, "")},
		{name: "a default that is not a member", code: "default_logbook_not_member", arrange: export(func(*smcloud.ArchiveExport) {}, pvArchive)},
		{name: "two logbooks with one label", code: "logbook_label_collision", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[2].Label = "Contest" }, pvLBB), context: []string{pvLBA, pvLBC}},
		{name: "an empty logbook label", code: "invalid_logbook_label", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[2].Label = " " }, pvLBB), context: []string{pvLBC}},
		{name: "a logbook label too long", code: "invalid_logbook_label", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[2].Label = strings.Repeat("x", 65) }, pvLBB), context: []string{pvLBC}},
		{name: "an empty logbook callsign", code: "invalid_logbook_callsign", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[2].Callsign = "" }, pvLBB), context: []string{pvLBC}},
		{name: "an invalid logbook callsign", code: "invalid_logbook_callsign", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[2].Callsign = "NODIGITS" }, pvLBB), context: []string{pvLBC}},
		{name: "a logbook with no UUID", code: "logbook_without_uuid", arrange: export(func(e *smcloud.ArchiveExport) {
			e.Logbooks[2].UUID, e.Logbooks[2].LegacyName = nil, str("Empty")
		}, pvLBB)},
		{name: "an empty archive label for a new entry", code: "invalid_archive_label", arrange: export(func(e *smcloud.ArchiveExport) { e.Label = "" }, pvLBB)},
		{name: "an archive label too long for a new entry", code: "invalid_archive_label", arrange: export(func(e *smcloud.ArchiveExport) { e.Label = strings.Repeat("x", 65) }, pvLBB)},
		{name: "a legacy entry for U", code: "entry_not_managed", arrange: withEntry(types.QsoArchiveConfig{ID: pvArchive, Label: "Old", Ownership: types.QsoArchiveOwnershipLegacy, Path: elsewhere})},
		{name: "an external entry for U", code: "entry_not_managed", arrange: withEntry(types.QsoArchiveConfig{ID: pvArchive, Label: "Old", Ownership: types.QsoArchiveOwnershipExternal, Path: elsewhere})},
		{name: "U's file present", code: "archive_file_exists", arrange: withFile(pvArchive + ".db"), residue: pvArchive + ".db"},
		{name: "U's -wal present", code: "archive_file_exists", arrange: withFile(pvArchive + ".db-wal"), residue: pvArchive + ".db-wal"},
		{name: "U's -shm present", code: "archive_file_exists", arrange: withFile(pvArchive + ".db-shm"), residue: pvArchive + ".db-shm"},
		{name: "an unfinished temporary file", code: "temp_file_exists", arrange: withFile(pvArchive + ".db.creating"), residue: pvArchive + ".db.creating"},
		{name: "an unfinished temporary -wal", code: "temp_file_exists", arrange: withFile(pvArchive + ".db.creating-wal"), residue: pvArchive + ".db.creating-wal"},
		{name: "an unfinished temporary -shm", code: "temp_file_exists", arrange: withFile(pvArchive + ".db.creating-shm"), residue: pvArchive + ".db.creating-shm"},
		// Structural defects of a publicly constructible export.
		{name: "an invalid archive UUID", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.UUID = "nope" }, pvLBB), context: []string{"archive"}},
		{name: "a UUIDv4 logbook", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[2].UUID = str(pvV4) }, pvLBB), context: []string{pvV4}},
		{name: "a duplicate logbook UUID", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[2].UUID = str(pvLBA) }, pvLBB), context: []string{pvLBA}},
		{name: "an invalid QSO UUID", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[0].UUID = "nope" }, pvLBB), context: []string{"row 0"}},
		{name: "a UUIDv4 QSO", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[0].UUID = pvV4 }, pvLBB), context: []string{pvV4}},
		{name: "a duplicate QSO UUID", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[2].UUID = pvQ1 }, pvLBB), context: []string{pvQ1}},
		{name: "a membership index past the logbooks", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[0].Logbook = 3 }, pvLBB), context: []string{pvQ1}},
		{name: "a negative membership index", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[0].Logbook = -1 }, pvLBB), context: []string{pvQ1}},
		{name: "a row with no modified_at", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[0].ModifiedAt = time.Time{} }, pvLBB), context: []string{pvQ1, "modified_at"}},
		{name: "a row with no payload", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[0].Qso = nil }, pvLBB), context: []string{pvQ1, "qso"}},
		{name: "a payload that is not an object", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[0].Qso = json.RawMessage(`[1]`) }, pvLBB), context: []string{pvQ1, "qso"}},
		{name: "a payload naming another QSO", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Qsos[0].Qso = e.Qsos[2].Qso }, pvLBB), context: []string{pvQ1, "qso.uuid"}},
		{name: "a row count that disagrees", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[0].Count++ }, pvLBB), context: []string{pvLBA, "count"}},
		{name: "a tombstone count that disagrees", code: "invalid_export", arrange: export(func(e *smcloud.ArchiveExport) { e.Logbooks[0].Tombstones = 0 }, pvLBB), context: []string{pvLBA, "tombstones"}},
	} {
		for _, via := range []string{"Provision", "PreflightProvision"} {
			t.Run(via+"/"+c.name, func(t *testing.T) {
				fx := newPVFixture(t)
				req := c.arrange(t, fx)
				_, managedBefore := os.Stat(ManagedDir(fx.cfgSvc.Snapshot()))
				before := fx.dataTree(t)
				var err error
				if via == "Provision" {
					_, err = fx.m.Provision(context.Background(), req)
				} else {
					_, err = fx.m.PreflightProvision(req)
				}
				r := wantRefusal(t, err, c.code)
				if c.residue != "" {
					if want := filepath.Join(ManagedDir(fx.cfgSvc.Snapshot()), c.residue); r.Path != want || !strings.Contains(err.Error(), want) {
						t.Fatalf("refusal path %q (%v); want exactly %s, named", r.Path, err, want)
					}
				}
				for _, s := range c.context {
					if !strings.Contains(r.Message, s) {
						t.Fatalf("refusal %q does not name %s", r.Message, s)
					}
				}
				wantNoValues(t, err, fx.log.String(), "DL9UW", "OH2BH", "48.500000")
				if fx.creates != 0 || fx.restores != 0 {
					t.Fatalf("the refusal came after construction began: %d temporary file(s) created, %d row(s) restored; want none", fx.creates, fx.restores)
				}
				if _, managedAfter := os.Stat(ManagedDir(fx.cfgSvc.Snapshot())); os.IsNotExist(managedBefore) && !os.IsNotExist(managedAfter) {
					t.Fatal("the managed directory was created by a refused provisioning")
				}
				wantSameTree(t, c.name, before, fx.dataTree(t))
			})
		}
	}
	t.Run("restorer not wired: only provisioning needs it", func(t *testing.T) {
		m, cfgSvc, _ := testManager(t)
		if _, err := m.PreflightProvision(ProvisionRequest{Export: pvStandard(t), DefaultLogbook: pvLBB}); err != nil {
			t.Fatalf("the dry run refused without a restorer: %v", err)
		}
		_, err := m.Provision(context.Background(), ProvisionRequest{Export: pvStandard(t), DefaultLogbook: pvLBB})
		wantRefusal(t, err, "restorer_unavailable")
		if _, statErr := os.Stat(ManagedDir(cfgSvc.Snapshot())); !os.IsNotExist(statErr) {
			t.Fatal("the managed directory was created")
		}
	})
}

func TestProvision_PV2_ThePlanCreatesNothing(t *testing.T) {
	fx := newPVFixture(t)
	before := fx.dataTree(t)
	plan, err := fx.m.PreflightProvision(fx.req(pvStandard(t), pvLBB))
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if plan.ArchiveUUID != pvArchive || plan.Path != fx.final() || plan.Entry != nil {
		t.Fatalf("plan = %+v; want U at %s with a new entry", plan, fx.final())
	}
	want := []PlannedLogbook{
		{UUID: pvLBA, Name: "Contest", Callsign: "7Q5MLV", Rows: 2, Tombstones: 1},
		{UUID: pvLBB, Name: "Portable", Callsign: "7Q5MLV/P", Rows: 1, Default: true},
		{UUID: pvLBC, Name: "Empty", Callsign: "7Q5MLV"},
	}
	if !slices.Equal(plan.Logbooks, want) {
		t.Fatalf("plan logbooks = %+v; want %+v", plan.Logbooks, want)
	}
	if fx.creates != 0 || fx.restores != 0 {
		t.Fatalf("the plan created %d temporary file(s) and restored %d row(s)", fx.creates, fx.restores)
	}
	wantSameTree(t, "the plan", before, fx.dataTree(t))
}

// wantProvisionedFile reads the placed file back as the cloud described it.
func wantProvisionedFile(t *testing.T, fx *pvFixture, path string) {
	t.Helper()
	wantOnlyFile(t, filepath.Dir(path), path)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file %s: %v; want mode 0600", path, err)
	}
	for _, absent := range []string{"country", "contacted_station", "schema_migrations_reference"} {
		if hasTableIn(t, path, absent) {
			t.Fatalf("the provisioned file carries %q; only the log migration set may run", absent)
		}
	}
	db := openManaged(t, fx.cfgSvc.Snapshot(), path)
	ctx := context.Background()
	lbs, err := db.FetchAllLogbooksWithContext(ctx)
	if err != nil || len(lbs) != 3 {
		t.Fatalf("logbooks = %+v (%v); want three", lbs, err)
	}
	byUUID := map[string]types.Logbook{}
	for _, lb := range lbs {
		byUUID[lb.UUID] = lb
	}
	for _, w := range []types.Logbook{{UUID: pvLBA, Name: "Contest", Callsign: "7Q5MLV"}, {UUID: pvLBB, Name: "Portable", Callsign: "7Q5MLV/P"}, {UUID: pvLBC, Name: "Empty", Callsign: "7Q5MLV"}} {
		if got := byUUID[w.UUID]; got.Name != w.Name || got.Callsign != w.Callsign {
			t.Fatalf("logbook %s = %+v; want %s / %s", w.UUID, got, w.Name, w.Callsign)
		}
	}
	identity, err := db.ArchiveIdentityWithContext(ctx)
	if err != nil || identity.ArchiveUUID != pvArchive || identity.DefaultLogbookID != byUUID[pvLBB].ID {
		t.Fatalf("identity = %+v (%v); want U with Portable the default", identity, err)
	}
	for _, w := range []struct {
		uuid, logbook string
		revision      int64
		at            time.Time
		gone          bool
	}{
		{pvQ1, pvLBA, 3, pvAt, false},
		{pvQ2, pvLBA, 4, pvGoneAt, true},
		{pvQ3, pvLBB, 1, pvAt, false},
	} {
		q, err := db.FetchQsoByUUIDIncludingDeletedWithContext(ctx, w.uuid)
		deletedOK := q.DeletedAt.IsZero()
		if w.gone {
			deletedOK = q.DeletedAt.Equal(pvGoneAt)
		}
		if err != nil || q.LogbookID != byUUID[w.logbook].ID || q.Revision != w.revision || !q.ModifiedAt.Equal(w.at) || !deletedOK {
			t.Fatalf("row %s = logbook %d revision %d modified %s deleted %s (%v); want %s, %d, %s, tombstone %v",
				w.uuid, q.LogbookID, q.Revision, q.ModifiedAt, q.DeletedAt, err, w.logbook, w.revision, w.at, w.gone)
		}
	}
	if at, err := db.DestinationBindingsSeededAtWithContext(ctx); err != nil || at == nil {
		t.Fatalf("seed marker = %v (%v); want the empty seed decision recorded", at, err)
	}
	if bindings, err := db.ListLogbookDestinationsWithContext(ctx); err != nil || len(bindings) != 0 {
		t.Fatalf("bindings = %v (%v); want none", bindings, err)
	}
	if n := countRows(t, path, "qso_upload"); n != 0 {
		t.Fatalf("%d upload(s) queued; provisioning queues none", n)
	}
}

// wantSummaries: U's summary describes the provisioned file exactly (live rows
// counted, the default marked, signed against the file) and Home's is kept.
func wantSummaries(t *testing.T, fx *pvFixture) {
	t.Helper()
	sums, miss := ReadSummaries(SummariesPath(fx.cfgSvc.Snapshot()))
	if miss != "" {
		t.Fatalf("summaries: miss %q", miss)
	}
	want := []LogbookSummary{
		{UUID: pvLBA, Name: "Contest", Callsign: "7Q5MLV", QsoCount: 1},
		{UUID: pvLBB, Name: "Portable", Callsign: "7Q5MLV/P", QsoCount: 1, Default: true},
		{UUID: pvLBC, Name: "Empty", Callsign: "7Q5MLV"},
	}
	sum, ok := sums[pvArchive]
	if !ok || !slices.Equal(sum.Logbooks, want) {
		t.Fatalf("U's summary = %+v (present %v); want %+v", sum.Logbooks, ok, want)
	}
	now, err := SignatureOf(fx.final())
	if err != nil || !sum.Current(now) {
		t.Fatalf("U's summary reads stale against its file (%v)", err)
	}
	if !reflect.DeepEqual(sums[idA], fx.homeSummary) {
		t.Fatalf("Home's summary = %+v; want it kept as %+v", sums[idA], fx.homeSummary)
	}
}

func countRows(t *testing.T, path, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestProvision_PV3_ANewEntry(t *testing.T) {
	fx := newPVFixture(t)
	before := catalogueNow(t, fx.cfgSvc)
	res, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	want := types.QsoArchiveConfig{ID: pvArchive, Label: "Field Day", Ownership: types.QsoArchiveOwnershipManaged}
	if !res.Added || res.Entry != want || res.Plan.Path != fx.final() || res.DurabilityUncertain || res.LeftoverTemp != "" {
		t.Fatalf("result = %+v; want a new inactive managed entry %+v at %s, durable, nothing left over", res, want, fx.final())
	}
	wantProvisionedFile(t, fx, fx.final())
	after := catalogueNow(t, fx.cfgSvc)
	if !slices.Equal(after.memory, append(slices.Clone(before.memory), want)) || !slices.Equal(after.disk, append(slices.Clone(before.disk), want)) {
		t.Fatalf("catalogue = %+v; want the entry added in memory and on disk", after)
	}
	if after.active != before.active || after.pending != before.pending || after.diskActive != before.diskActive || after.diskPending != before.diskPending {
		t.Fatalf("selection changed: %+v; want %+v", after, before)
	}
	wantSummaries(t, fx)
}

func TestProvision_PV4_AnExistingManagedEntryIsKept(t *testing.T) {
	fx := newPVFixture(t)
	kept := types.QsoArchiveConfig{ID: pvArchive, Label: "Kept label", Ownership: types.QsoArchiveOwnershipManaged, RequestKey: "kept-key", LastActivationError: FailFileMissing}
	if _, err := fx.cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = append(c.QsoArchives, kept)
		c.ActiveQsoArchiveID = pvArchive
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := catalogueNow(t, fx.cfgSvc)
	cfgBytes, _ := os.ReadFile(fx.cfgSvc.Path)
	cfgInfo, _ := os.Stat(fx.cfgSvc.Path)
	res, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if res.Added || res.Entry != kept || res.DurabilityUncertain || res.LeftoverTemp != "" {
		t.Fatalf("result = %+v; want the kept entry %+v, durable, nothing left over", res, kept)
	}
	wantProvisionedFile(t, fx, fx.final())
	if after := catalogueNow(t, fx.cfgSvc); !after.equal(before) {
		t.Fatalf("catalogue = %+v; want unchanged %+v", after, before)
	}
	nowBytes, _ := os.ReadFile(fx.cfgSvc.Path)
	nowInfo, _ := os.Stat(fx.cfgSvc.Path)
	if string(nowBytes) != string(cfgBytes) || !nowInfo.ModTime().Equal(cfgInfo.ModTime()) {
		t.Fatal("config.json was written; an existing entry needs no catalogue write")
	}
	wantSummaries(t, fx)
}

// wantNothingOfOurs: the managed directory holds none of this invocation's
// files, the catalogue and selection are unchanged, and no summary of U.
func wantNothingOfOurs(t *testing.T, fx *pvFixture, before catalogueState, foreign ...string) {
	t.Helper()
	entries, err := os.ReadDir(ManagedDir(fx.cfgSvc.Snapshot()))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, foreign) {
		t.Fatalf("the managed directory holds %v; want only %v", names, foreign)
	}
	if after := catalogueNow(t, fx.cfgSvc); !after.equal(before) {
		t.Fatalf("catalogue = %+v; want unchanged %+v", after, before)
	}
	sums, _ := ReadSummaries(SummariesPath(fx.cfgSvc.Snapshot()))
	if _, ok := sums[pvArchive]; ok {
		t.Fatal("a summary of U was recorded by a refused provisioning")
	}
	if !reflect.DeepEqual(sums[idA], fx.homeSummary) {
		t.Fatalf("Home's summary = %+v; want it kept", sums[idA])
	}
}

// wantNoValues: neither the error nor the log carries any of the values.
func wantNoValues(t *testing.T, err error, log string, values ...string) {
	t.Helper()
	for _, v := range values {
		if strings.Contains(err.Error(), v) || strings.Contains(log, v) {
			t.Fatalf("the value %q leaked: error %q", v, err)
		}
	}
}

func TestProvision_PV5_ARowThatDoesNotRoundTripRefusesBeforePlacement(t *testing.T) {
	far := func(q types.Qso) types.Qso {
		q.ContactedStation.Lat, q.ContactedStation.Lon = pvFarLat, pvFarLon
		return q
	}
	if utils.CoordsInsideGrid("KN28", pvFarLat, pvFarLon) || !utils.CoordsInsideGrid("KN28", "48.500000", "25.000000") {
		t.Fatal("fixture: the far coordinates must contradict KN28 and the near ones agree")
	}
	// replace swaps one exported row (keeping its membership).
	replace := func(uuid string, row func(t *testing.T) smcloud.ArchiveExportRecord) func(t *testing.T, e *smcloud.ArchiveExport) {
		return func(t *testing.T, e *smcloud.ArchiveExport) {
			for i := range e.Qsos {
				if e.Qsos[i].UUID == uuid {
					lb := e.Qsos[i].Logbook
					e.Qsos[i] = row(t)
					e.Qsos[i].Logbook = lb
				}
			}
		}
	}
	unknownKey := func(t *testing.T) smcloud.ArchiveExportRecord {
		r := pvPushed(t, pvQso(pvQ1, "DL9UW", 3))
		var top map[string]json.RawMessage
		if err := json.Unmarshal(r.Qso, &top); err != nil {
			t.Fatal(err)
		}
		top["x_future"] = json.RawMessage(`{"note":"` + pvUnknown + `"}`)
		r.Qso, _ = json.Marshal(top)
		return r
	}
	// tamper changes what the restore writes for one row, so the readback
	// differs from the cloud's record in exactly one controlled field.
	tamper := func(uuid string, change func(q *types.Qso, logbookID *int64)) func(q *types.Qso, logbookID *int64) {
		return func(q *types.Qso, logbookID *int64) {
			if q.UUID == uuid {
				change(q, logbookID)
			}
		}
	}
	for _, c := range []struct {
		name   string
		export func(t *testing.T, e *smcloud.ArchiveExport)
		tamper func(q *types.Qso, logbookID *int64)
		want   []RoundTripMismatch
		values []string
	}{
		{name: "coordinates the adapter corrects",
			export: replace(pvQ1, func(t *testing.T) smcloud.ArchiveExportRecord { return pvPushed(t, far(pvQso(pvQ1, "DL9UW", 3))) }),
			want:   []RoundTripMismatch{{pvQ1, []string{"qso.lat", "qso.lon"}}}, values: []string{pvFarLat, pvFarLon, "48.5", "25.0"}},
		{name: "a tombstone's coordinates the adapter corrects",
			export: replace(pvQ2, func(t *testing.T) smcloud.ArchiveExportRecord { return pvPushed(t, far(pvTombstone(pvQ2, "EA1B", 4))) }),
			want:   []RoundTripMismatch{{pvQ2, []string{"qso.lat", "qso.lon"}}}, values: []string{pvFarLat, pvFarLon}},
		{name: "a payload key this build does not model", export: replace(pvQ1, unknownKey),
			want: []RoundTripMismatch{{pvQ1, []string{"qso.x_future"}}}, values: []string{pvUnknown}},
		{name: "a sub-second modified_at",
			export: replace(pvQ1, func(t *testing.T) smcloud.ArchiveExportRecord {
				q := pvQso(pvQ1, "DL9UW", 3)
				q.ModifiedAt = pvAt.Add(123456 * time.Microsecond)
				return pvPushed(t, q)
			}),
			want: []RoundTripMismatch{{pvQ1, []string{"modified_at"}}}, values: []string{pvSubMicro, "2026-07-17"}},
		{name: "a modified_at a whole second off",
			tamper: tamper(pvQ1, func(q *types.Qso, _ *int64) { q.ModifiedAt = q.ModifiedAt.Add(time.Second) }),
			want:   []RoundTripMismatch{{pvQ1, []string{"modified_at"}}}, values: []string{"06:00:01", "2026-07-17"}},
		{name: "a revision",
			tamper: tamper(pvQ1, func(q *types.Qso, _ *int64) { q.Revision = 9 }),
			want:   []RoundTripMismatch{{pvQ1, []string{"revision"}}}},
		{name: "a tombstone's deleted_at",
			tamper: tamper(pvQ2, func(q *types.Qso, _ *int64) { q.DeletedAt = q.DeletedAt.Add(time.Second) }),
			want:   []RoundTripMismatch{{pvQ2, []string{"deleted_at"}}}, values: []string{"07:00:01", "2026-07-17"}},
		{name: "a tombstone restored live",
			tamper: tamper(pvQ2, func(q *types.Qso, _ *int64) { q.DeletedAt = time.Time{} }),
			want:   []RoundTripMismatch{{pvQ2, []string{"deleted_at"}}}},
		{name: "a live row restored as a tombstone",
			tamper: tamper(pvQ1, func(q *types.Qso, _ *int64) { q.DeletedAt = pvGoneAt }),
			want:   []RoundTripMismatch{{pvQ1, []string{"deleted_at"}}}},
		{name: "membership: a row restored into another logbook",
			// Portable is the second logbook built; Empty the third.
			tamper: tamper(pvQ3, func(_ *types.Qso, logbookID *int64) { *logbookID++ }),
			want:   []RoundTripMismatch{{pvQ3, []string{"logbook"}}}},
		{name: "identity: a row restored under another UUID",
			tamper: tamper(pvQ1, func(q *types.Qso, _ *int64) { q.UUID = pvQ4 }),
			want:   []RoundTripMismatch{{pvQ1, []string{"uuid"}}}},
		{name: "every mismatching row is reported",
			tamper: func(q *types.Qso, logbookID *int64) {
				switch q.UUID {
				case pvQ1:
					q.Revision = 9
				case pvQ3:
					*logbookID++
				}
			},
			want: []RoundTripMismatch{{pvQ1, []string{"revision"}}, {pvQ3, []string{"logbook"}}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fx := newPVFixture(t)
			e := pvStandard(t)
			if c.export != nil {
				c.export(t, e)
			}
			fx.tamper = c.tamper
			before := catalogueNow(t, fx.cfgSvc)
			_, err := fx.m.Provision(context.Background(), fx.req(e, pvLBB))
			var rt *RoundTripError
			if !stderr.As(err, &rt) {
				t.Fatalf("err = %v; want a RoundTripError", err)
			}
			if len(rt.Rows) != len(c.want) {
				t.Fatalf("mismatches = %+v; want %+v", rt.Rows, c.want)
			}
			for i, w := range c.want {
				if rt.Rows[i].UUID != w.UUID || !slices.Equal(rt.Rows[i].Fields, w.Fields) {
					t.Fatalf("mismatches = %+v; want %+v", rt.Rows, c.want)
				}
				for _, s := range append([]string{w.UUID}, w.Fields...) {
					if !strings.Contains(err.Error(), s) {
						t.Fatalf("the error %q does not name %s", err, s)
					}
				}
			}
			wantNoValues(t, err, fx.log.String(), c.values...)
			if fx.creates != 1 {
				t.Fatalf("%d temporary file(s) created; a construction refusal comes after exactly one", fx.creates)
			}
			wantNothingOfOurs(t, fx, before)
		})
	}
}

func TestProvision_PV6_ARowTheRestoreAdapterRefuses(t *testing.T) {
	fx := newPVFixture(t)
	// Q4 repeats Q1's contact in the same logbook: the dedupe index refuses it.
	e := pvStandard(t)
	twin := pvPushed(t, pvQso(pvQ4, "DL9UW", 1))
	twin.Logbook = 0
	e.Qsos = append(e.Qsos, twin)
	e.Logbooks[0].Count++
	before := catalogueNow(t, fx.cfgSvc)
	_, err := fx.m.Provision(context.Background(), fx.req(e, pvLBB))
	if err == nil || !strings.Contains(err.Error(), pvQ4) {
		t.Fatalf("err = %v; want a refusal naming %s", err, pvQ4)
	}
	wantNoValues(t, err, fx.log.String(), "DL9UW", "14.255")
	wantNothingOfOurs(t, fx, before)
}

// pvExisting gives U a managed entry whose file is missing, as the active
// archive.
func pvExisting(t *testing.T, fx *pvFixture) types.QsoArchiveConfig {
	t.Helper()
	kept := types.QsoArchiveConfig{ID: pvArchive, Label: "Kept label", Ownership: types.QsoArchiveOwnershipManaged, RequestKey: "kept-key"}
	if _, err := fx.cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = append(c.QsoArchives, kept)
		c.ActiveQsoArchiveID = pvArchive
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return kept
}

func TestProvision_PV7_ADestinationAppearingAfterPreflightIsNeverReplaced(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing entry %v", existing), func(t *testing.T) {
			fx := newPVFixture(t)
			if existing {
				pvExisting(t, fx)
			}
			before := catalogueNow(t, fx.cfgSvc)
			fx.onRestore = func() {
				if _, err := os.Stat(fx.final()); os.IsNotExist(err) {
					_ = os.WriteFile(fx.final(), []byte("foreign"), 0o600)
				}
			}
			_, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
			wantRefusal(t, err, "destination_appeared")
			if b, _ := os.ReadFile(fx.final()); string(b) != "foreign" {
				t.Fatalf("the destination now holds %q; want the foreign file untouched", b)
			}
			wantNothingOfOurs(t, fx, before, pvArchive+".db")
		})
	}
}

// pvFailRemoving makes removeFile fail for the given paths: every time when
// persistent, otherwise once each.
func pvFailRemoving(t *testing.T, persistent bool, paths ...string) {
	t.Helper()
	orig := removeFile
	t.Cleanup(func() { removeFile = orig })
	failed := map[string]bool{}
	removeFile = func(p string) error {
		if slices.Contains(paths, p) && (persistent || !failed[p]) {
			failed[p] = true
			return &os.PathError{Op: "remove", Path: p, Err: stderr.New("remove boom")}
		}
		return orig(p)
	}
}

// pvSyncsAfter records the directory syncs made after the first removal
// failure, and fails them when fail is set.
func pvSyncsAfter(t *testing.T, fail bool) *[]string {
	t.Helper()
	var syncs []string
	origSync := syncDir
	t.Cleanup(func() { syncDir = origSync })
	syncDir = func(p string) error {
		syncs = append(syncs, p)
		if fail {
			return stderr.New("sync boom")
		}
		return origSync(p)
	}
	return &syncs
}

func TestProvision_PV8_TheTemporaryNameCannotBeRemovedAfterTheLink(t *testing.T) {
	t.Run("a new entry: before publication, undone", func(t *testing.T) {
		fx := newPVFixture(t)
		pvFailRemoving(t, false, fx.final()+creatingSuffix)
		before := catalogueNow(t, fx.cfgSvc)
		_, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
		if err == nil || !strings.Contains(err.Error(), "remove boom") {
			t.Fatalf("err = %v; want the removal failure, before publication", err)
		}
		wantNothingOfOurs(t, fx, before)
	})
	for _, failBarrier := range []bool{false, true} {
		name := "an existing entry: published by the link, then made durable"
		if failBarrier {
			name = "an existing entry: published by the link, the barrier failing"
		}
		t.Run(name, func(t *testing.T) {
			fx := newPVFixture(t)
			kept := pvExisting(t, fx)
			tmp := fx.final() + creatingSuffix
			trace := pvTraceUnlinkAndSyncs(t, fx, failBarrier)
			before := catalogueNow(t, fx.cfgSvc)
			res, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
			if err != nil || res.Entry != kept || res.LeftoverTemp != tmp || res.DurabilityUncertain != failBarrier {
				t.Fatalf("provision = %+v, %v; want the kept archive, the leftover %s reported, durability uncertain %v", res, err, tmp, failBarrier)
			}
			// The trace: the unlink was attempted with the archive already
			// placed, and the whole barrier came after it, in order.
			dir := ManagedDir(fx.cfgSvc.Snapshot())
			chain := []string{"sync " + dir}
			if !failBarrier {
				chain = append(chain, "sync "+filepath.Dir(dir), "sync "+fx.cfgSvc.Snapshot().DataDir)
			}
			i := slices.Index(*trace, "unlink failed, archive placed")
			if i < 0 || slices.Contains(*trace, "unlink before placement") {
				t.Fatalf("trace = %v; want the unlink attempted with the archive placed", *trace)
			}
			if after := (*trace)[i+1:]; len(after) < len(chain) || !slices.Equal(after[:len(chain)], chain) {
				t.Fatalf("trace = %v; want the barrier %v after the failed unlink", *trace, chain)
			}
			// Published: both names stand, as one file carrying U.
			wantPlacedAs(t, fx.final(), tmp)
			if !strings.Contains(fx.log.String(), tmp) {
				t.Fatal("the leftover temporary name was not logged")
			}
			if after := catalogueNow(t, fx.cfgSvc); !after.equal(before) {
				t.Fatalf("catalogue = %+v; want unchanged %+v", after, before)
			}
		})
	}
}

// pvTraceUnlinkAndSyncs fails every removal of U's temporary name and records,
// in one trace, each attempt (whether U's file was already placed as the same
// file) and each directory sync after it; the syncs fail when failBarrier.
func pvTraceUnlinkAndSyncs(t *testing.T, fx *pvFixture, failBarrier bool) *[]string {
	t.Helper()
	tmp := fx.final() + creatingSuffix
	var trace []string
	origRemove, origSync := removeFile, syncDir
	t.Cleanup(func() { removeFile, syncDir = origRemove, origSync })
	removeFile = func(p string) error {
		if p != tmp {
			return origRemove(p)
		}
		event := "unlink before placement"
		if a, errA := os.Stat(fx.final()); errA == nil {
			if b, errB := os.Stat(tmp); errB == nil && os.SameFile(a, b) {
				event = "unlink failed, archive placed"
			}
		}
		trace = append(trace, event)
		return &os.PathError{Op: "remove", Path: p, Err: stderr.New("remove boom")}
	}
	syncDir = func(p string) error {
		trace = append(trace, "sync "+p)
		if failBarrier {
			return stderr.New("sync boom")
		}
		return origSync(p)
	}
	return &trace
}

// wantPlacedAs: the final and temporary names are one file, carrying U.
func wantPlacedAs(t *testing.T, final, tmp string) {
	t.Helper()
	a, errA := os.Stat(final)
	b, errB := os.Stat(tmp)
	if errA != nil || errB != nil || !os.SameFile(a, b) {
		t.Fatalf("final %v, temporary %v; want both names standing as one file", errA, errB)
	}
	if identity, found, err := sqlite.PeekArchiveIdentity(final); err != nil || !found || identity.ArchiveUUID != pvArchive {
		t.Fatalf("the published file's identity = %+v (found %v, %v); want U", identity, found, err)
	}
}

// PV12: a rollback whose cleanup cannot remove this invocation's files keeps
// the original cause first, names each leftover in the error and the log, and
// leaves them unlisted (not activatable, diagnosed at the next start).
func TestProvision_PV12_ARollbackThatCannotCleanUp(t *testing.T) {
	for _, c := range []struct {
		name     string
		cause    string
		arrange  func(t *testing.T, fx *pvFixture)
		leftover []string // in the managed directory, sorted
	}{
		{"a construction refusal; the temporary file stays", "do not round-trip", func(t *testing.T, fx *pvFixture) {
			fx.tamper = func(q *types.Qso, _ *int64) {
				if q.UUID == pvQ1 {
					q.Revision = 9
				}
			}
			pvFailRemoving(t, true, fx.final()+creatingSuffix)
		}, []string{pvArchive + ".db.creating"}},
		{"the temporary name; the rollback cannot remove it either", "remove boom", func(t *testing.T, fx *pvFixture) {
			pvFailRemoving(t, true, fx.final()+creatingSuffix)
		}, []string{pvArchive + ".db.creating"}},
		{"a directory sync; the placed file stays", "sync boom", func(t *testing.T, fx *pvFixture) {
			pvFailRemoving(t, true, fx.final())
			pvSyncsAfter(t, true)
		}, []string{pvArchive + ".db"}},
		{"a construction refusal with the directory locked; every sidecar stays", "do not round-trip", func(t *testing.T, fx *pvFixture) {
			fx.tamper = func(q *types.Qso, _ *int64) {
				if q.UUID == pvQ1 {
					q.Revision = 9
				}
			}
			dir := ManagedDir(fx.cfgSvc.Snapshot())
			fx.onRestore = func() { _ = os.Chmod(dir, 0o500) }
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		}, []string{pvArchive + ".db.creating", pvArchive + ".db.creating-shm", pvArchive + ".db.creating-wal"}},
		{"the catalogue write; the placed file stays", "catalogue", func(t *testing.T, fx *pvFixture) {
			pvFailRemoving(t, true, fx.final())
			dir := filepath.Dir(fx.cfgSvc.Path)
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		}, []string{pvArchive + ".db"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fx := newPVFixture(t)
			before := catalogueNow(t, fx.cfgSvc)
			c.arrange(t, fx)
			_, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
			_ = os.Chmod(filepath.Dir(fx.cfgSvc.Path), 0o700)
			_ = os.Chmod(ManagedDir(fx.cfgSvc.Snapshot()), 0o700)
			if err == nil {
				t.Fatal("provision succeeded")
			}
			msg := err.Error()
			i := strings.Index(msg, "could not be removed")
			if i < 0 || !strings.Contains(msg[:i], c.cause) {
				t.Fatalf("err = %q; want the cause %q first, then the cleanup failure", msg, c.cause)
			}
			dir := ManagedDir(fx.cfgSvc.Snapshot())
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			if !slices.Equal(names, c.leftover) {
				t.Fatalf("the managed directory holds %v; want the leftovers %v", names, c.leftover)
			}
			for _, name := range c.leftover {
				p := filepath.Join(dir, name)
				if !strings.Contains(msg[i:], p) || !strings.Contains(fx.log.String(), p) {
					t.Fatalf("the leftover %s is not named in the error %q or the log", p, msg)
				}
			}
			if after := catalogueNow(t, fx.cfgSvc); !after.equal(before) {
				t.Fatalf("catalogue = %+v; want unchanged %+v", after, before)
			}
			var re *RequestError
			if _, err := fx.m.Activate(context.Background(), pvArchive); !stderr.As(err, &re) || re.Code != "archive_not_found" {
				t.Fatalf("activating U = %v; want archive_not_found", err)
			}
			found := DiagnoseCreatingArtefacts(fx.cfgSvc.Snapshot(), fx.m.logger)
			for _, name := range c.leftover {
				if !slices.Contains(found, filepath.Join(dir, name)) {
					t.Fatalf("diagnosed %v; want the leftover %s", found, name)
				}
			}
		})
	}
}

func TestProvision_PV9_ADirectorySyncFailingAfterTheLink(t *testing.T) {
	failSyncs := func(t *testing.T) {
		orig := syncDir
		t.Cleanup(func() { syncDir = orig })
		syncDir = func(string) error { return stderr.New("sync boom") }
	}
	t.Run("a new entry: before publication, undone", func(t *testing.T) {
		fx := newPVFixture(t)
		failSyncs(t)
		before := catalogueNow(t, fx.cfgSvc)
		_, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
		if err == nil || !strings.Contains(err.Error(), "sync boom") {
			t.Fatalf("err = %v; want the sync failure", err)
		}
		wantNothingOfOurs(t, fx, before)
	})
	t.Run("an existing entry: published, durability uncertain", func(t *testing.T) {
		fx := newPVFixture(t)
		kept := pvExisting(t, fx)
		failSyncs(t)
		res, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
		if err != nil || res.Entry != kept || !res.DurabilityUncertain {
			t.Fatalf("provision = %+v, %v; want the kept archive, durability uncertain", res, err)
		}
		wantProvisionedFile(t, fx, fx.final())
	})
}

func TestProvision_PV10_ACatalogueWriteOfUncertainDurability(t *testing.T) {
	fx := newPVFixture(t)
	dir := filepath.Dir(fx.cfgSvc.Path)
	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	res, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
	_ = os.Chmod(dir, 0o700)
	if err != nil || !res.Added || !res.DurabilityUncertain {
		t.Fatalf("provision = %+v, %v; want the new entry, durability uncertain", res, err)
	}
	if !strings.Contains(fx.log.String(), `"durability_uncertain":true`) {
		t.Fatal("the uncertain durability was not logged")
	}
	after := catalogueNow(t, fx.cfgSvc)
	if !slices.Contains(after.memory, res.Entry) || !slices.Contains(after.disk, res.Entry) {
		t.Fatalf("catalogue = %+v; want the entry in memory and on disk", after)
	}
	wantProvisionedFile(t, fx, fx.final())
}

func TestProvision_PV11_ACatalogueWriteFailing(t *testing.T) {
	fx := newPVFixture(t)
	before := catalogueNow(t, fx.cfgSvc)
	dir := filepath.Dir(fx.cfgSvc.Path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
	_ = os.Chmod(dir, 0o700)
	if err == nil || !strings.Contains(err.Error(), "catalogue") {
		t.Fatalf("err = %v; want the catalogue write's failure", err)
	}
	wantNothingOfOurs(t, fx, before)
}

// PV13: a row the restore adapter refuses with a value in its own error (a
// malformed frequency) is refused naming the row, and the value reaches
// neither the error's text, nor any error in its chain, nor the log when the
// error is logged as the daemon logs one (Err walks the chain).
func TestProvision_PV13_ARestoreRefusalCarriesNoValueIntoTheLog(t *testing.T) {
	const badFreq = "14.2x55"
	fx := newPVFixture(t)
	e := pvStandard(t)
	q := pvQso(pvQ4, "DL9UW", 1)
	q.QsoDetails.Freq = badFreq
	row := pvPushed(t, q)
	row.Logbook = 1
	e.Qsos = append(e.Qsos, row)
	e.Logbooks[1].Count++
	_, err := fx.m.Provision(context.Background(), fx.req(e, pvLBB))
	if err == nil || !strings.Contains(err.Error(), pvQ4) {
		t.Fatalf("err = %v; want a refusal naming %s", err, pvQ4)
	}
	for e := err; e != nil; e = stderr.Unwrap(e) {
		if strings.Contains(e.Error(), badFreq) {
			t.Fatalf("an error in the chain carries the value: %q", e)
		}
	}
	fx.m.logger.ErrorWith().Err(err).Msg("provisioning refused")
	if strings.Contains(fx.log.String(), badFreq) {
		t.Fatalf("the logged error carries the value: %s", fx.log.String())
	}
	if !strings.Contains(fx.log.String(), pvQ4) {
		t.Fatal("the logged error does not name the row")
	}
}

// PV14: the startup diagnosis reports a leftover sidecar of an unlisted or
// unfinished file, and never a catalogued file: neither a catalogued
// archive's own WAL or SHM, nor an archive whose own catalogued path has a
// sidecar's or a temporary file's name.
func TestDiagnoseCreatingArtefacts_PV14_SidecarsOfUnlistedFilesOnly(t *testing.T) {
	fx := newPVFixture(t)
	res, err := fx.m.Provision(context.Background(), fx.req(pvStandard(t), pvLBB))
	if err != nil {
		t.Fatal(err)
	}
	dir := ManagedDir(fx.cfgSvc.Snapshot())
	unlisted := "0197f9a0-0000-7000-8000-0000000e0f05"
	var want []string
	for _, name := range []string{
		unlisted + ".db-wal", unlisted + ".db-shm",
		unlisted + ".db.creating-wal", unlisted + ".db.creating-shm",
	} {
		want = append(want, filepath.Join(dir, name))
	}
	// Two external archives whose catalogued files are named like a sidecar
	// and like an unfinished file's sidecar.
	asWal := filepath.Join(dir, "0197f9a0-0000-7000-8000-0000000e0f06.db-wal")
	asCreating := filepath.Join(dir, "0197f9a0-0000-7000-8000-0000000e0f07.db.creating-shm")
	if _, err := fx.cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = append(c.QsoArchives,
			types.QsoArchiveConfig{ID: "0197f9a0-0000-7000-8000-0000000e0f06", Label: "Wal-named", Ownership: types.QsoArchiveOwnershipExternal, Path: asWal},
			types.QsoArchiveConfig{ID: "0197f9a0-0000-7000-8000-0000000e0f07", Label: "Creating-named", Ownership: types.QsoArchiveOwnershipExternal, Path: asCreating})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range append(slices.Clone(want), res.Plan.Path+"-wal", res.Plan.Path+"-shm", asWal, asCreating) {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	found := DiagnoseCreatingArtefacts(fx.cfgSvc.Snapshot(), fx.m.logger)
	slices.Sort(found)
	slices.Sort(want)
	if !slices.Equal(found, want) {
		t.Fatalf("diagnosed %v; want exactly the unlisted and unfinished sidecars %v, never a catalogued file", found, want)
	}
}
