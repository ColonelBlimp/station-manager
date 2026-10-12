package main

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.4 commit 3 (ADR 0092; rulings F1–F3, H1–H3, 2026-10-10): the wire
   of a binding born on SM Cloud identity. The enable gate stays closed: the
   stamp is written by SQL between generations.

     SI1  a stamped, enabled binding on a second Home logbook uploads only to
          /v1/archives/{a}/logbooks/{its uuid}/qsos; its legacy Cloud logbook
          field never reaches a request; its reconciler is identity.
     SI2  an account change between generations is not a hold: the next
          uploads carry the new token on the same scoped path.
     SI3  disabled: no worker, the stamp kept across the restart; enabled
          again: identity again.
     SI4  an unstamped, unadopted binding keeps the name wire (the control,
          in the same generation as SI1).
     SI5  a stamp beside an adoption marker: held with its own reason, no
          worker, no reconciler, its queue kept; a healthy peer uploads.
     SI6  a stamped binding targets the archive FILE's UUID and label.
     SI7  a stamped binding whose identity forwarder cannot be built stops
          the start; it never falls back to a name worker.
*/

const bornConflictFault = "conflicting identity and adoption markers; manual recovery required"

type bornRequest struct{ method, path, auth, body string }

// bornCloud accepts every upload and records every request.
type bornCloud struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []bornRequest
}

func newBornCloud(t *testing.T) *bornCloud {
	t.Helper()
	c := &bornCloud{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.reqs = append(c.reqs, bornRequest{r.Method, r.URL.Path, r.Header.Get("Authorization"), string(body)})
		c.mu.Unlock()
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/qsos"):
			_, _ = io.WriteString(w, `{"received":1,"applied":1}`)
		case r.URL.Path == "/v1/logbooks":
			_, _ = io.WriteString(w, `{"logbooks":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *bornCloud) requests() []bornRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.reqs)
}

// putsOf is every PUT whose body carries the QSO.
func (c *bornCloud) putsOf(uuid string) []bornRequest {
	var out []bornRequest
	for _, r := range c.requests() {
		if r.method == http.MethodPut && strings.Contains(r.body, uuid) {
			out = append(out, r)
		}
	}
	return out
}

type bornFixture struct {
	cloud  *bornCloud
	d      *daemon
	orch   *orchestrator.Orchestrator
	second int64
	uuidB  string
}

// newBornFixture: Home with its seeded default binding "smcloud" (name wire,
// never adopted) and a second logbook whose binding "smcloud-b" carries a
// decoy legacy Cloud logbook name; mark runs between the generations (the
// stamp, by SQL).
func newBornFixture(t *testing.T, mark string) *bornFixture {
	t.Helper()
	f := &bornFixture{cloud: newBornCloud(t)}
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		creds, _ := json.Marshal(map[string]string{"url": f.cloud.srv.URL, "token": "t", "logbook": "shack"})
		c.Forwarders = []types.ForwarderConfig{{Name: "smcloud", Type: smcloud.Type, Enabled: true, Credentials: creds,
			TickIntervalSec: 1, BatchSize: 5, ActionFilter: []string{"insert", "update", "delete"}}}
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("first start: %v", err)
	}
	var err error
	if f.second, err = d.db.InsertLogbook(types.Logbook{Name: "Second", Callsign: "7Q5MLV"}); err != nil {
		t.Fatal(err)
	}
	books, err := d.db.FetchAllLogbooksWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, lb := range books {
		if lb.ID == f.second {
			f.uuidB = lb.UUID
		}
	}
	stopGen(t, orch)
	rawExec(t, d, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
		VALUES (?, 'smcloud', 'smcloud-b', 1, '{"logbook":"decoy-name"}')`, f.second)
	if mark != "" {
		rawExec(t, d, mark)
	}
	ownerOnlySocketDir(t, d)
	f.d, f.orch = startGen(t, d)
	return f
}

func rawExec(t *testing.T, d *daemon, q string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+d.paths.QSO)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

const stampB = `UPDATE logbook_destination SET identity_since = datetime('now') WHERE forwarder_name = 'smcloud-b'`

func logQsoIn(t *testing.T, d *daemon, logbookID int64, call, timeOn string) string {
	t.Helper()
	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.200", QsoDate: "20261010", TimeOn: timeOn},
	}
	res, err := d.qso.Submit(context.Background(), logbookID, rec, true)
	if err != nil {
		t.Fatalf("log %s: %v", call, err)
	}
	return res.UUID
}

func uploadStatusFor(t *testing.T, d *daemon, uuid, forwarder string) string {
	t.Helper()
	ctx := context.Background()
	q, err := d.db.FetchQsoByUUIDWithContext(ctx, uuid)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ForwarderName == forwarder {
			return string(r.Status)
		}
	}
	return ""
}

func (f *bornFixture) scopedQsos(d *daemon) string {
	return "/v1/archives/" + d.cfgSvc.Snapshot().ActiveQsoArchiveID + "/logbooks/" + f.uuidB + "/qsos"
}

func TestBornIdentity_SI1_SI4_StampedUploadsByUUIDUnstampedByName(t *testing.T) {
	f := newBornFixture(t, stampB)
	b1 := logQsoIn(t, f.d, f.second, "DL9UW", "080001")
	h1 := logQsoIn(t, f.d, 1, "9A4ZM", "080002")
	waitUntil(t, "the stamped binding's QSO to upload", func() bool { return uploadStatusFor(t, f.d, b1, "smcloud-b") == "uploaded" })
	waitUntil(t, "the default binding's QSO to upload", func() bool { return uploadStatusFor(t, f.d, h1, "smcloud") == "uploaded" })
	for _, r := range f.cloud.putsOf(b1) {
		if r.path != f.scopedQsos(f.d) {
			t.Fatalf("SI1: the stamped binding uploaded to %s; want only %s", r.path, f.scopedQsos(f.d))
		}
	}
	for _, r := range f.cloud.putsOf(h1) {
		if r.path != "/v1/qsos" {
			t.Fatalf("SI4: the unstamped binding uploaded to %s; want the name wire /v1/qsos", r.path)
		}
	}
	for _, r := range f.cloud.requests() {
		if strings.Contains(r.body, "decoy-name") || strings.Contains(r.path, "decoy-name") {
			t.Fatalf("SI1: the legacy Cloud logbook field reached a request: %s %s", r.method, r.path)
		}
	}
	if w := runningReconcilers(f.d); w["smcloud-b"] != "identity" || w["smcloud"] != "name" {
		t.Fatalf("reconcilers = %v; want smcloud-b identity, smcloud name", w)
	}
}

func TestBornIdentity_SI2_AnAccountChangeIsNotAHold(t *testing.T) {
	f := newBornFixture(t, stampB)
	stopGen(t, f.orch)
	if _, err := f.d.cfgSvc.Update(func(c *config.Config) error {
		creds, _ := json.Marshal(map[string]string{"url": f.cloud.srv.URL, "token": "t2"})
		c.Forwarders[0].Credentials = creds
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	d3, _ := startGen(t, f.d)
	b1 := logQsoIn(t, d3, f.second, "DL9UW", "080001")
	waitUntil(t, "the upload under the new account", func() bool { return uploadStatusFor(t, d3, b1, "smcloud-b") == "uploaded" })
	puts := f.cloud.putsOf(b1)
	if len(puts) == 0 {
		t.Fatal("no upload seen")
	}
	for _, r := range puts {
		if r.path != f.scopedQsos(d3) || r.auth != "Bearer t2" {
			t.Fatalf("upload %s with %q; want %s with the new token", r.path, r.auth, f.scopedQsos(d3))
		}
	}
	if _, held := d3.wires.held["smcloud-b"]; held {
		t.Fatal("a born-identity binding was held after an account change")
	}
}

func TestBornIdentity_SI3_DisabledKeepsTheStamp(t *testing.T) {
	f := newBornFixture(t, stampB)
	stopGen(t, f.orch)
	rawExec(t, f.d, `UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'smcloud-b'`)
	d3, orch3 := startGen(t, f.d)
	if _, ok := runningReconcilers(d3)["smcloud-b"]; ok {
		t.Fatal("a disabled binding has a reconciler")
	}
	b, _ := bindingNamed(t, d3, "smcloud-b")
	if b.IdentitySince == nil {
		t.Fatal("disabling cleared the stamp")
	}
	stopGen(t, orch3)
	rawExec(t, f.d, `UPDATE logbook_destination SET enabled = 1 WHERE forwarder_name = 'smcloud-b'`)
	d4, _ := startGen(t, d3)
	if w := runningReconcilers(d4)["smcloud-b"]; w != "identity" {
		t.Fatalf("re-enabled: reconciler = %q; want identity", w)
	}
	b1 := logQsoIn(t, d4, f.second, "DL9UW", "080003")
	waitUntil(t, "the re-enabled binding's upload", func() bool { return uploadStatusFor(t, d4, b1, "smcloud-b") == "uploaded" })
	for _, r := range f.cloud.putsOf(b1) {
		if r.path != f.scopedQsos(d4) {
			t.Fatalf("re-enabled: uploaded to %s; want %s", r.path, f.scopedQsos(d4))
		}
	}
}

func bindingNamed(t *testing.T, d *daemon, name string) (types.LogbookDestination, bool) {
	t.Helper()
	rows, err := d.db.ListLogbookDestinationsWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ForwarderName == name {
			return r, true
		}
	}
	return types.LogbookDestination{}, false
}

func TestBornIdentity_SI5_ConflictingMarkersHold(t *testing.T) {
	for name, markers := range map[string]string{
		"with a reservation": `adoption_reserved_at = datetime('now')`,
		"with an adoption":   `adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = 'fp'`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newBornFixture(t, `UPDATE logbook_destination SET identity_since = datetime('now'), `+markers+` WHERE forwarder_name = 'smcloud-b'`)
			b1 := logQsoIn(t, f.d, f.second, "DL9UW", "080001")
			h1 := logQsoIn(t, f.d, 1, "9A4ZM", "080002")
			waitUntil(t, "the healthy peer's upload", func() bool { return uploadStatusFor(t, f.d, h1, "smcloud") == "uploaded" })
			if s := uploadStatusFor(t, f.d, b1, "smcloud-b"); s != "pending" {
				t.Fatalf("the conflicting binding's upload = %q; want kept pending", s)
			}
			if got := f.cloud.putsOf(b1); len(got) != 0 {
				t.Fatalf("the conflicting binding uploaded: %+v", got)
			}
			if _, ok := runningReconcilers(f.d)["smcloud-b"]; ok {
				t.Fatal("the conflicting binding has a reconciler")
			}
			_, results, _ := reconcileAll(t, f.d)
			found := false
			for _, r := range results {
				if r.ForwarderName == "smcloud-b" {
					found = true
					if r.Error != bornConflictFault {
						t.Fatalf("aggregate entry = %+v; want the conflict reason", r)
					}
				}
			}
			if !found {
				t.Fatalf("aggregate %+v has no entry for the conflicting binding", results)
			}
		})
	}
}

func TestBornIdentity_SI6_TargetsTheFilesArchive(t *testing.T) {
	f := newBornFixture(t, stampB)
	ctx := context.Background()
	cfg := f.d.cfgSvc.Snapshot()
	var built []smcloud.IdentityTarget
	restore := newIdentityForwarder
	newIdentityForwarder = func(fc types.ForwarderConfig, tg smcloud.IdentityTarget) (*smcloud.Forwarder, error) {
		built = append(built, tg)
		return smcloud.NewIdentity(fc, tg)
	}
	t.Cleanup(func() { newIdentityForwarder = restore })
	snap, err := resolveDestinationRoutes(ctx, f.d.db, cfg, f.d.logger)
	if err != nil {
		t.Fatal(err)
	}
	sel, err := selectWires(ctx, f.d.db, cfg, snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sel.identity["smcloud-b"]; !ok || len(built) != 1 {
		t.Fatalf("identity forwarders %v, built %+v; want one for smcloud-b", sel.identity, built)
	}
	tg := built[0]
	if tg.ArchiveUUID != cfg.ActiveQsoArchiveID || tg.ArchiveLabel != "Home" || tg.LogbookUUID != f.uuidB || tg.LogbookLabel != "Second" {
		t.Fatalf("target = %+v; want the file's archive and the second logbook", tg)
	}
	if sel.targets["smcloud-b"] != tg {
		t.Fatalf("reconcile target %+v differs from the forwarder's %+v", sel.targets["smcloud-b"], tg)
	}
}

func TestBornIdentity_SI7_AFailedConstructionStopsTheStart(t *testing.T) {
	f := newBornFixture(t, stampB)
	stopGen(t, f.orch)
	restore := newIdentityForwarder
	newIdentityForwarder = func(types.ForwarderConfig, smcloud.IdentityTarget) (*smcloud.Forwarder, error) {
		return nil, stderr.New("identity construction refused (test)")
	}
	t.Cleanup(func() { newIdentityForwarder = restore })
	d3, orch3 := buildOrchestratedDaemon(t, f.d.cfgSvc, f.d.cfgSvc.Snapshot())
	if err := orch3.Start(d3.workerCtx); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("start = %v; want it stopped by the identity construction", err)
	}
}
