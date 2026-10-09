package main

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 5a: each SM Cloud binding's wire is chosen at start
   (ADR 0090 T1; ADR 0091; rulings C1–C3, 2026-10-09).

     SW1  a binding confirmed for the account it starts with uploads by UUID
          (the identity path, never /v1/qsos), and keeps the by-name
          reconciler.
     SW2  a reservation alone keeps the legacy wire.
     SW3  a binding adopted for another account, or for none, is held: no
          worker, no reconciler (periodic or on demand); the start's queue
          housekeeping keeps its queued uploads (not disabled, not unbound),
          and newly logged QSOs queue for it; the bindings view says the
          uploads wait for confirmation.
     SW4  confirmed during the run, the view says a restart is required; after
          the restart, every queued upload drains through the identity worker
          and nothing is held.
     SW5  an identity forwarder that cannot be constructed stops the start; it
          never falls back to a legacy worker.
*/

type identityCloud struct {
	srv       *httptest.Server
	mu        sync.Mutex
	puts      []string
	qsoStatus int
}

func newIdentityCloud(t *testing.T) *identityCloud {
	t.Helper()
	c := &identityCloud{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/qsos") {
			c.mu.Lock()
			c.puts = append(c.puts, r.URL.Path)
			status := c.qsoStatus
			c.mu.Unlock()
			if status != 0 {
				w.WriteHeader(status)
				return
			}
			_, _ = io.WriteString(w, `{"received":1,"applied":1}`)
			return
		}
		// The adopter's requests: an old server, so it does nothing.
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *identityCloud) setStatus(s int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.qsoStatus = s
}

func (c *identityCloud) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.puts)
}

// homeWithCloud is a first generation on Home with one enabled SM Cloud
// binding (seeded from config as "smcloud") pointed at the fake cloud, which
// refuses uploads with 503 so queued rows stay queued.
func homeWithCloud(t *testing.T, cloud *identityCloud) (*daemon, *orchestrator.Orchestrator) {
	t.Helper()
	cloud.setStatus(http.StatusServiceUnavailable)
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		creds, _ := json.Marshal(map[string]string{"url": cloud.srv.URL, "token": "t", "logbook": "shack"})
		c.Forwarders = []types.ForwarderConfig{{Name: "smcloud", Type: smcloud.Type, Enabled: true, Credentials: creds, TickIntervalSec: 1, BatchSize: 5,
			ActionFilter: []string{"insert", "update", "delete"}}}
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("first start: %v", err)
	}
	return d, orch
}

func logQso(t *testing.T, d *daemon, call string) string {
	t.Helper()
	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.200", QsoDate: "20261009", TimeOn: "0800" + call[len(call)-2:]},
	}
	res, err := d.qso.Submit(context.Background(), 1, rec, true)
	if err != nil {
		t.Fatalf("log %s: %v", call, err)
	}
	return res.UUID
}

// uploadStatus is the SM Cloud upload row of a QSO: "" when there is none.
func uploadStatus(t *testing.T, d *daemon, uuid string) string {
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
		if r.ForwarderName == "smcloud" {
			return string(r.Status)
		}
	}
	return ""
}

// stampAdoption writes the binding's adoption columns between generations
// (after stopGen), as the adopter would have, and makes the queued rows due.
func stampAdoption(t *testing.T, d *daemon, reserved bool, adopted bool, account string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+d.paths.QSO)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	q := `UPDATE logbook_destination SET adoption_reserved_at = CASE WHEN ? THEN datetime('now') END,
		remote_adopted_at = CASE WHEN ? THEN datetime('now') END, remote_adopted_account = ? WHERE forwarder_name = 'smcloud'`
	res, err := db.Exec(q, reserved, adopted, account)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("stamped %d bindings; want 1", n)
	}
	// The first generation's refused attempts set a backoff; the downtime
	// outlasted it, so the queued rows are due at the next start.
	if _, err := db.Exec(`UPDATE qso_upload SET next_attempt_at = strftime('%s', 'now') - 1 WHERE status = 'pending'`); err != nil {
		t.Fatal(err)
	}
}

func startAccount(t *testing.T, d *daemon) string {
	t.Helper()
	cfg := d.cfgSvc.Snapshot()
	fp, err := archive.CurrentAccount(cfg, smcloud.Type, cfg.ActiveQsoArchiveID)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func restartGen(t *testing.T, d *daemon, orch *orchestrator.Orchestrator) (*daemon, *orchestrator.Orchestrator) {
	t.Helper()
	stopGen(t, orch)
	return startGen(t, d)
}

// stopGen ends a generation and requires every node to have drained: only
// then are its workers and connections gone, so the raw writes between
// generations, and the next generation, cannot race them. A failed, timed-out
// or skipped node fails the test, named with its error.
func stopGen(t *testing.T, orch *orchestrator.Orchestrator) {
	t.Helper()
	report := orch.Shutdown(5*time.Second, nil)
	names := map[orchestrator.Result]string{orchestrator.Failed: "failed", orchestrator.TimedOut: "timed out", orchestrator.Skipped: "skipped"}
	var bad []string
	for _, o := range report.Outcomes {
		if o.Result != orchestrator.Drained {
			what, ok := names[o.Result]
			if !ok {
				what = fmt.Sprintf("result %d", o.Result)
			}
			bad = append(bad, fmt.Sprintf("%s %s (err %v, blocked by %v)", o.Node, what, o.Err, o.BlockedBy))
		}
	}
	if len(bad) > 0 || report.FirstTimedOut != "" || len(report.Outcomes) == 0 {
		t.Fatalf("the generation did not drain cleanly (first timed out %q, %d outcomes): %s",
			report.FirstTimedOut, len(report.Outcomes), strings.Join(bad, "; "))
	}
}

// startGen starts the next generation on d's config.
func startGen(t *testing.T, d *daemon) (*daemon, *orchestrator.Orchestrator) {
	t.Helper()
	d2, orch2 := buildOrchestratedDaemon(t, d.cfgSvc, d.cfgSvc.Snapshot())
	if err := orch2.Start(d2.workerCtx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	return d2, orch2
}

func identityPath(t *testing.T, d *daemon) string {
	t.Helper()
	books, err := d.db.FetchAllLogbooksWithContext(context.Background())
	if err != nil || len(books) == 0 {
		t.Fatalf("logbooks: %v", err)
	}
	return "/v1/archives/" + d.cfgSvc.Snapshot().ActiveQsoArchiveID + "/logbooks/" + books[0].UUID + "/qsos"
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func heldLine(t *testing.T, d *daemon) *types.BindingAdoptionView {
	t.Helper()
	v, err := d.archives.Bindings(context.Background(), d.cfgSvc.Snapshot().ActiveQsoArchiveID)
	if err != nil {
		t.Fatal(err)
	}
	for _, dest := range v.Destinations {
		for _, row := range dest.Logbooks {
			if row.ForwarderName == "smcloud" {
				return row.UploadsHeld
			}
		}
	}
	t.Fatal("no smcloud row")
	return nil
}

func smcloudWorkersStarted(t *testing.T, d *daemon) int {
	t.Helper()
	started, _ := workerNamesStarted(t, filepath.Join(d.cfgSvc.WorkingDir(), "log", "smd.log"))
	n := 0
	for _, s := range started {
		if s == "smcloud" {
			n++
		}
	}
	return n
}

func TestIdentityWire_SW1_ConfirmedUploadsByUUID(t *testing.T) {
	cloud := newIdentityCloud(t)
	d, orch := homeWithCloud(t, cloud)
	u1 := logQso(t, d, "DL9UW")
	stopGen(t, orch)
	stampAdoption(t, d, true, true, startAccount(t, d))
	before := len(cloud.seen())
	cloud.setStatus(0)
	d2, _ := startGen(t, d)

	waitUntil(t, "the queued QSO to upload", func() bool { return uploadStatus(t, d2, u1) == "uploaded" })
	want := identityPath(t, d2)
	for _, p := range cloud.seen()[before:] {
		if p != want {
			t.Fatalf("an upload went to %s; want only %s", p, want)
		}
	}
	if d2.smcloudRec == nil {
		t.Fatal("the identity binding lost its by-name reconciler")
	}
	if h := heldLine(t, d2); h != nil {
		t.Fatalf("uploads_held = %+v on an identity binding", *h)
	}
}

func TestIdentityWire_SW2_AReservationAloneKeepsTheLegacyWire(t *testing.T) {
	cloud := newIdentityCloud(t)
	d, orch := homeWithCloud(t, cloud)
	u1 := logQso(t, d, "DL9UW")
	stopGen(t, orch)
	stampAdoption(t, d, true, false, "")
	before := len(cloud.seen())
	cloud.setStatus(0)
	d2, _ := startGen(t, d)
	waitUntil(t, "the queued QSO to upload", func() bool { return uploadStatus(t, d2, u1) == "uploaded" })
	for _, p := range cloud.seen()[before:] {
		if p != "/v1/qsos" {
			t.Fatalf("an upload went to %s; want the legacy /v1/qsos", p)
		}
	}
}

func TestIdentityWire_SW3_SW4_HeldThenConfirmedThenRestarted(t *testing.T) {
	for name, account := range map[string]string{"another account": "another", "no account": ""} {
		t.Run(name, func(t *testing.T) {
			cloud := newIdentityCloud(t)
			d, orch := homeWithCloud(t, cloud)
			u1 := logQso(t, d, "DL9UW")
			stopGen(t, orch)
			stampAdoption(t, d, true, true, account)
			cloud.setStatus(0)
			workersBefore := smcloudWorkersStarted(t, d)
			before := len(cloud.seen())
			d2, orch2 := startGen(t, d)

			// SW3: held.
			u2 := logQso(t, d2, "9A4ZM")
			time.Sleep(2500 * time.Millisecond) // more than two worker ticks
			if got := cloud.seen()[before:]; len(got) != 0 {
				t.Fatalf("a held binding uploaded: %v", got)
			}
			if n := smcloudWorkersStarted(t, d2); n != workersBefore {
				t.Fatalf("a worker started for the held binding (%d starts; %d before)", n, workersBefore)
			}
			for _, u := range []string{u1, u2} {
				if s := uploadStatus(t, d2, u); s != "pending" {
					t.Fatalf("upload of %s = %q; want kept pending", u, s)
				}
			}
			if d2.smcloudRec != nil {
				t.Fatal("a held binding has a reconciler")
			}
			if h := heldLine(t, d2); h == nil || h.State != archive.UploadsHeldWaiting {
				t.Fatalf("uploads_held = %+v; want waiting for confirmation", h)
			}
			if account != "another" {
				return
			}

			// SW4: confirmed during the run, then restarted.
			tx, cancel, err := d2.db.BeginTxContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(`UPDATE logbook_destination SET remote_adopted_account = ? WHERE forwarder_name = 'smcloud'`, startAccount(t, d2))
			if err == nil {
				err = tx.Commit()
			}
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			if h := heldLine(t, d2); h == nil || h.State != archive.UploadsHeldRestart {
				t.Fatalf("uploads_held = %+v; want restart required", h)
			}
			d3, _ := restartGen(t, d2, orch2)
			for _, u := range []string{u1, u2} {
				waitUntil(t, "the held upload to drain", func() bool { return uploadStatus(t, d3, u) == "uploaded" })
			}
			want := identityPath(t, d3)
			for _, p := range cloud.seen()[before:] {
				if p != want {
					t.Fatalf("an upload went to %s; want only %s", p, want)
				}
			}
			if h := heldLine(t, d3); h != nil {
				t.Fatalf("uploads_held = %+v after the restart", *h)
			}
		})
	}
}

func TestIdentityWire_SW5_ConstructionFailureStopsTheStart(t *testing.T) {
	cloud := newIdentityCloud(t)
	d, orch := homeWithCloud(t, cloud)
	logQso(t, d, "DL9UW")
	stopGen(t, orch)
	stampAdoption(t, d, true, true, startAccount(t, d))
	cloud.setStatus(0)
	restore := newIdentityForwarder
	newIdentityForwarder = func(types.ForwarderConfig, smcloud.IdentityTarget) (*smcloud.Forwarder, error) {
		return nil, stderr.New("identity construction refused (test)")
	}
	t.Cleanup(func() { newIdentityForwarder = restore })
	workersBefore := smcloudWorkersStarted(t, d)
	before := len(cloud.seen())

	d2, orch2 := buildOrchestratedDaemon(t, d.cfgSvc, d.cfgSvc.Snapshot())
	err := orch2.Start(d2.workerCtx)
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("start = %v; want it stopped by the identity construction", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if got := cloud.seen()[before:]; len(got) != 0 {
		t.Fatalf("uploads after a failed identity construction: %v", got)
	}
	if n := smcloudWorkersStarted(t, d2); n != workersBefore {
		t.Fatal("a legacy worker started in place of the identity forwarder")
	}
}

/*
   Operator review of 5a (2026-10-09):

     SW6  a held binding has no running worker, so a queue retry is refused
          and its failed upload stays failed; its queue stays visible. After
          the confirmation and a restart the retry is accepted, and the upload
          drains by identity.
     SW7  started held, confirmed, then disabled and saved: the line says the
          queued uploads are discarded at the next restart, not that they
          resume; after the restart they are gone, nothing was sent, and
          nothing is held.
     SW8  the account is judged against the archive FILE's UUID, not the
          active selector: with Home's file open while another archive is the
          selector, a binding confirmed for Home's file is an identity binding
          targeting Home; one fingerprinted for the selector is held.
*/

// apiCall sends one request to the generation's API over its unix socket
// (started after ownerOnlySocketDir).
func apiCall(t *testing.T, d *daemon, method, path string) (int, []byte) {
	t.Helper()
	sock := d.cfgSvc.Snapshot().SocketPath
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	req, err := http.NewRequest(method, "http://smd"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A listener that fails to start reports why on errCh: name that failure
	// instead of waiting out the deadline.
	var resp *http.Response
	deadline := time.Now().Add(15 * time.Second)
	for resp, err = client.Do(req); err != nil; resp, err = client.Do(req) {
		select {
		case lerr := <-d.errCh:
			t.Fatalf("the API listener on %s failed: %v", sock, lerr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the API on %s did not answer: %v", sock, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// ownerOnlySocketDir makes the socket's directory owner-only, as the listener
// requires; the next generation started answers on it.
func ownerOnlySocketDir(t *testing.T, d *daemon) {
	t.Helper()
	if err := os.Chmod(filepath.Dir(d.cfgSvc.Snapshot().SocketPath), 0o700); err != nil {
		t.Fatal(err)
	}
}

// setBinding changes the saved binding during a run, as a save would.
func setBinding(t *testing.T, d *daemon, set string, args ...any) {
	t.Helper()
	tx, cancel, err := d.db.BeginTxContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, err = tx.Exec(`UPDATE logbook_destination SET `+set+` WHERE forwarder_name = 'smcloud'`, args...); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityWire_SW6_NoRetryWhileHeld(t *testing.T) {
	cloud := newIdentityCloud(t)
	d, orch := homeWithCloud(t, cloud)
	u1 := logQso(t, d, "DL9UW")
	stopGen(t, orch)
	stampAdoption(t, d, true, true, "another")
	db, err := sql.Open("sqlite", "file:"+d.paths.QSO)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE qso_upload SET status = 'failed' WHERE forwarder_name = 'smcloud'`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	cloud.setStatus(0)
	before := len(cloud.seen())
	ownerOnlySocketDir(t, d)
	d2, orch2 := startGen(t, d)

	code, body := apiCall(t, d2, http.MethodPost, "/v1/forwarder/smcloud/queue/retry")
	if code != http.StatusBadRequest || !strings.Contains(string(body), "forwarder_disabled") {
		t.Fatalf("retry while held = %d %s; want 400 forwarder_disabled", code, body)
	}
	if s := uploadStatus(t, d2, u1); s != "failed" {
		t.Fatalf("upload = %q after a refused retry; want still failed", s)
	}
	if code, body := apiCall(t, d2, http.MethodGet, "/v1/forwarder-queues"); code != http.StatusOK ||
		!strings.Contains(string(body), `"name":"smcloud","waiting":0,"failed":1`) {
		t.Fatalf("queues while held = %d %s; want smcloud listed with its failed upload", code, body)
	}

	setBinding(t, d2, `remote_adopted_account = ?`, startAccount(t, d2))
	d3, _ := restartGen(t, d2, orch2)
	if code, body := apiCall(t, d3, http.MethodPost, "/v1/forwarder/smcloud/queue/retry"); code != http.StatusOK ||
		!strings.Contains(string(body), `"rearmed":1`) {
		t.Fatalf("retry after confirmation and a restart = %d %s; want 200 rearmed 1", code, body)
	}
	waitUntil(t, "the retried upload to drain", func() bool { return uploadStatus(t, d3, u1) == "uploaded" })
	want := identityPath(t, d3)
	for _, p := range cloud.seen()[before:] {
		if p != want {
			t.Fatalf("an upload went to %s; want only %s", p, want)
		}
	}
}

func TestIdentityWire_SW7_DisabledWhileHeld(t *testing.T) {
	cloud := newIdentityCloud(t)
	d, orch := homeWithCloud(t, cloud)
	u1 := logQso(t, d, "DL9UW")
	stopGen(t, orch)
	stampAdoption(t, d, true, true, "another")
	cloud.setStatus(0)
	before := len(cloud.seen())
	d2, orch2 := startGen(t, d)

	setBinding(t, d2, `remote_adopted_account = ?`, startAccount(t, d2))
	if h := heldLine(t, d2); h == nil || h.State != archive.UploadsHeldRestart {
		t.Fatalf("uploads_held = %+v; want restart required", h)
	}
	setBinding(t, d2, `enabled = 0`)
	h := heldLine(t, d2)
	if h == nil || h.State != archive.UploadsHeldDisabled ||
		h.Message != "Uploads are held; this binding is off, so its queued uploads are discarded at the next restart." {
		t.Fatalf("uploads_held = %+v after the disable; want the discard at the next restart", h)
	}

	d3, _ := restartGen(t, d2, orch2)
	if s := uploadStatus(t, d3, u1); s == "pending" || s == "uploaded" {
		t.Fatalf("upload = %q after the restart; want it discarded", s)
	}
	if got := cloud.seen()[before:]; len(got) != 0 {
		t.Fatalf("a disabled binding uploaded: %v", got)
	}
	if h := heldLine(t, d3); h != nil {
		t.Fatalf("uploads_held = %+v after the restart", *h)
	}
}

func TestIdentityWire_SW8_JudgedByTheFilesUUID(t *testing.T) {
	cloud := newIdentityCloud(t)
	d, _ := homeWithCloud(t, cloud)
	ctx := context.Background()
	cfg := d.cfgSvc.Snapshot()
	home := cfg.ActiveQsoArchiveID
	const selector = "019fd5c5-efcc-7193-be4f-1fee532ee3c3"
	cfg.ActiveQsoArchiveID = selector // another archive remains the selector
	forHome, err := archive.CurrentAccount(cfg, smcloud.Type, home)
	if err != nil {
		t.Fatal(err)
	}
	forSelector, err := archive.CurrentAccount(cfg, smcloud.Type, selector)
	if err != nil {
		t.Fatal(err)
	}
	if forHome == forSelector {
		t.Fatal("the fixture's fingerprints do not differ")
	}
	var built []smcloud.IdentityTarget
	restore := newIdentityForwarder
	newIdentityForwarder = func(fc types.ForwarderConfig, tg smcloud.IdentityTarget) (*smcloud.Forwarder, error) {
		built = append(built, tg)
		return smcloud.NewIdentity(fc, tg)
	}
	t.Cleanup(func() { newIdentityForwarder = restore })

	for _, c := range []struct {
		name, account string
		identity      bool
	}{{"confirmed for Home's file", forHome, true}, {"fingerprinted for the selector", forSelector, false}} {
		built = nil
		setBinding(t, d, `adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = ?`, c.account)
		snap, err := resolveDestinationRoutes(ctx, d.db, cfg, d.logger)
		if err != nil {
			t.Fatal(err)
		}
		sel, err := selectWires(ctx, d.db, cfg, snap)
		if err != nil {
			t.Fatal(err)
		}
		_, identity := sel.identity["smcloud"]
		_, held := sel.held["smcloud"]
		if identity != c.identity || held == c.identity {
			t.Fatalf("%s: identity %v, held %v; want identity %v", c.name, identity, held, c.identity)
		}
		if c.identity && (len(built) != 1 || built[0].ArchiveUUID != home || built[0].ArchiveLabel != "Home") {
			t.Fatalf("%s: built %+v; want one target for Home's file %s", c.name, built, home)
		}
	}
}
