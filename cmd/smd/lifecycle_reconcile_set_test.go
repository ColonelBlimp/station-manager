package main

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.4 commit 2: one SM Cloud reconciler per enabled, unheld binding,
   and the aggregate POST /v1/smcloud/reconcile (AC 1, AC 3; rulings F6, F8,
   G1–G4, 2026-10-10). The enable gate stays closed: the extra bindings are
   written by SQL between generations.

     DR1  per-binding wire: one generation with an identity binding
          (confirmed), a name binding on a second logbook, a held binding, an
          unresolved one, one both held and unresolved, and a disabled one.
          The aggregate lists the enabled five in listing order (held before
          unresolved), omits the disabled one; the identity entry asks only
          its scoped paths, the name entry only /v1/logbooks; one "reconciler
          started" line per running binding.
     DR2  a reconciler that cannot be built is a fixed fault; its healthy
          peer still runs. One binding's cloud failing leaves its peer's
          summary (200).
     DR3  the set is the start snapshot: a saved disable, a new enable, a
          confirmation and an account change during the run change nothing
          until the restart; the requests keep the start's token.
     DR4  lifecycle: two periodic passes blocked mid-request are cancelled by
          the stop and their loops have exited when the workers drain; an
          on-demand aggregate in flight when the stop begins completes, and
          its repair is in the log database before it closes; with the stop
          budget exhausted the existing dependency skip leaves the log
          database open.
*/

const (
	drHeld   = "uploads are held until adoption is confirmed for the current station account"
	drUnres  = "the binding cannot be resolved against a station account"
	drBuild  = "the reconciler could not be built; the daemon log has the cause"
	drFailed = "the reconcile pass failed; the daemon log has the cause"
)

// setCloud is a fake SM Cloud recording every GET (path and bearer). GETs
// whose path is blocked wait until released or until the request ends.
type setCloud struct {
	srv     *httptest.Server
	mu      sync.Mutex
	gets    []string
	auths   []string
	status  map[string]int
	blocked map[string]chan struct{}
	blockGT bool // every GET blocks until its request ends
	entered chan string
}

func newSetCloud(t *testing.T) *setCloud {
	t.Helper()
	c := &setCloud{status: map[string]int{}, blocked: map[string]chan struct{}{}, entered: make(chan string, 64)}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusServiceUnavailable) // uploads stay queued
			return
		}
		c.mu.Lock()
		c.gets = append(c.gets, r.URL.Path)
		c.auths = append(c.auths, r.Header.Get("Authorization"))
		release, block := c.blocked[r.URL.Path]
		all := c.blockGT
		status := c.status[r.URL.Path]
		c.mu.Unlock()
		if block || all {
			select {
			case c.entered <- r.URL.Path:
			default:
			}
			if all {
				<-r.Context().Done()
				return
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if r.URL.Path == "/v1/logbooks" {
			_, _ = io.WriteString(w, `{"logbooks":[]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound) // the scoped logbook not created yet; an old server to the adopter
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *setCloud) mark() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.gets)
}

func (c *setCloud) since(n int) (paths, auths []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.gets[n:]), slices.Clone(c.auths[n:])
}

func (c *setCloud) block(path string) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan struct{})
	c.blocked[path] = ch
	return ch
}

func (c *setCloud) setStatus(path string, s int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status[path] = s
}

// drFixture is a second generation on Home holding the DR1 bindings.
type drFixture struct {
	cloud  *setCloud
	d      *daemon
	orch   *orchestrator.Orchestrator
	uuidOf map[string]string // forwarder name → its logbook's UUID
	qso    string            // the default logbook's one QSO
	before []string          // "reconciler started" lines before this generation
}

// drBindings are the bindings written between generations; "smcloud" is the
// default logbook's, seeded from config and stamped confirmed.
var drBindings = []struct {
	name, logbook, creds string
	enabled              bool
	adoptedFor           string // "" = not adopted
}{
	{"smcloud-b", "B", `{"logbook":"other"}`, true, ""},
	{"smcloud-h", "H", `{"logbook":"h"}`, true, "another"},
	{"smcloud-u", "U", `{`, true, ""},
	{"smcloud-hu", "HU", `{`, true, "another"},
	{"smcloud-x", "X", `{"logbook":"x"}`, false, ""},
}

func newDRFixture(t *testing.T, beforeStart func(f *drFixture, gen1 *daemon)) *drFixture {
	t.Helper()
	f := &drFixture{cloud: newSetCloud(t), uuidOf: map[string]string{}}
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
	f.qso = logQso(t, d, "DL9UW")
	ids := map[string]int64{}
	for _, b := range drBindings {
		id, err := d.db.InsertLogbook(types.Logbook{Name: b.logbook, Callsign: "7Q5MLV"})
		if err != nil {
			t.Fatal(err)
		}
		ids[b.name] = id
	}
	books, err := d.db.FetchAllLogbooksWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, lb := range books {
		if lb.ID == 1 {
			f.uuidOf["smcloud"] = lb.UUID
		}
		for name, id := range ids {
			if lb.ID == id {
				f.uuidOf[name] = lb.UUID
			}
		}
	}
	f.before = reconcilersStarted(t, d)
	stopGen(t, orch)
	stampAdoption(t, d, true, true, startAccount(t, d))

	db, err := sql.Open("sqlite", "file:"+d.paths.QSO)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range drBindings {
		var adoptedAt, account any
		if b.adoptedFor != "" {
			adoptedAt, account = time.Now().UTC().Format("2006-01-02 15:04:05"), b.adoptedFor
		}
		if _, err := db.Exec(`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials,
			remote_adopted_at, remote_adopted_account) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			ids[b.name], smcloud.Type, b.name, b.enabled, b.creds, adoptedAt, account); err != nil {
			t.Fatalf("insert binding %s: %v", b.name, err)
		}
	}
	_ = db.Close()
	if beforeStart != nil {
		beforeStart(f, d)
	}
	ownerOnlySocketDir(t, d)
	f.d, f.orch = startGen(t, d)
	return f
}

func (f *drFixture) scoped(name string) string {
	return "/v1/archives/" + f.d.cfgSvc.Snapshot().ActiveQsoArchiveID + "/logbooks/" + f.uuidOf[name]
}

// reconcilersStarted is every binding named by a "smcloud reconciler
// started" line in smd.log, in order.
func reconcilersStarted(t *testing.T, d *daemon) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(d.cfgSvc.WorkingDir(), "log", "smd.log"))
	if err != nil {
		t.Fatalf("read smd.log: %v", err)
	}
	var out []string
	for _, ln := range strings.Split(string(data), "\n") {
		var rec struct {
			Message   string `json:"message"`
			Forwarder string `json:"forwarder"`
		}
		if json.Unmarshal([]byte(ln), &rec) == nil && rec.Message == "smcloud reconciler started" {
			out = append(out, rec.Forwarder)
		}
	}
	return out
}

type drResult struct {
	ForwarderName string          `json:"forwarder_name"`
	LogbookUUID   string          `json:"logbook_uuid"`
	Summary       json.RawMessage `json:"summary"`
	Error         string          `json:"error"`
}

func reconcileAll(t *testing.T, d *daemon) (int, []drResult, string) {
	t.Helper()
	code, body := apiCall(t, d, http.MethodPost, "/v1/smcloud/reconcile")
	var out struct {
		Results []drResult `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return code, out.Results, string(body)
}

// wantResults checks the entries in order: a summary fragment, or "" and the
// exact error.
func (f *drFixture) wantResults(t *testing.T, got []drResult, want [][3]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("results = %+v; want %d entries %v", got, len(want), want)
	}
	for i, w := range want {
		g := got[i]
		if g.ForwarderName != w[0] || g.LogbookUUID != f.uuidOf[w[0]] {
			t.Fatalf("result %d = %s/%s; want %s/%s", i, g.ForwarderName, g.LogbookUUID, w[0], f.uuidOf[w[0]])
		}
		hasSummary := len(g.Summary) > 0 && string(g.Summary) != "null"
		if w[1] != "" {
			if !hasSummary || g.Error != "" || !strings.Contains(string(g.Summary), w[1]) {
				t.Fatalf("result %s = summary %s error %q; want a summary with %s", w[0], g.Summary, g.Error, w[1])
			}
			continue
		}
		if hasSummary || g.Error != w[2] {
			t.Fatalf("result %s = summary %s error %q; want only the error %q", w[0], g.Summary, g.Error, w[2])
		}
	}
}

// drAllFive is DR1's aggregate: listing order (logbook, then binding), the
// disabled smcloud-x left out, held before unresolved for smcloud-hu.
func drAllFive(identitySummary, nameSummary string) [][3]string {
	return [][3]string{
		{"smcloud", identitySummary, ""},
		{"smcloud-b", nameSummary, ""},
		{"smcloud-h", "", drHeld},
		{"smcloud-u", "", drUnres},
		{"smcloud-hu", "", drHeld},
	}
}

func TestReconcileSet_DR1_PerBindingWire(t *testing.T) {
	f := newDRFixture(t, nil)
	if got := reconcilersStarted(t, f.d)[len(f.before):]; strings.Join(got, ",") != "smcloud,smcloud-b" {
		t.Fatalf("reconcilers started = %v; want smcloud and smcloud-b only", got)
	}
	mark := f.cloud.mark()
	code, results, body := reconcileAll(t, f.d)
	if code != http.StatusOK {
		t.Fatalf("aggregate = %d %s; want 200", code, body)
	}
	f.wantResults(t, results, drAllFive(`"in_sync":false`, `"cloud_logbook_id":0`))
	if strings.Contains(string(results[0].Summary), "cloud_logbook_id") {
		t.Fatalf("the identity summary carries a numeric id: %s", results[0].Summary)
	}
	paths, _ := f.cloud.since(mark)
	slices.Sort(paths)
	want := []string{"/v1/logbooks", f.scoped("smcloud") + "/reconcile"}
	slices.Sort(want)
	if !slices.Equal(paths, want) {
		t.Fatalf("cloud GETs during the aggregate = %v; want %v (identity scoped only, name by /v1/logbooks)", paths, want)
	}
}

func TestReconcileSet_DR2_FaultsAndFailuresStayPerBinding(t *testing.T) {
	t.Run("a reconciler that cannot be built", func(t *testing.T) {
		restore := newIdentityReconciler
		newIdentityReconciler = func(types.ForwarderConfig, int64, smcloud.IdentityTarget, *sqlite.Service,
			*qsoservice.Service, *logging.Service) (*smcloud.Reconciler, error) {
			return nil, stderr.New("identity reconciler refused (test)")
		}
		t.Cleanup(func() { newIdentityReconciler = restore })
		f := newDRFixture(t, nil)
		code, results, body := reconcileAll(t, f.d)
		if code != http.StatusOK {
			t.Fatalf("aggregate = %d %s; want 200", code, body)
		}
		want := drAllFive("", `"cloud_logbook_id":0`)
		want[0] = [3]string{"smcloud", "", drBuild}
		f.wantResults(t, results, want)
		if strings.Contains(body, "refused (test)") {
			t.Fatalf("the build cause reached the wire: %s", body)
		}
	})

	t.Run("one binding's cloud failing", func(t *testing.T) {
		f := newDRFixture(t, nil)
		f.cloud.setStatus(f.scoped("smcloud")+"/reconcile", http.StatusInternalServerError)
		code, results, body := reconcileAll(t, f.d)
		if code != http.StatusOK {
			t.Fatalf("aggregate = %d %s; want 200", code, body)
		}
		want := drAllFive("", `"cloud_logbook_id":0`)
		want[0] = [3]string{"smcloud", "", drFailed}
		f.wantResults(t, results, want)
	})
}

// setBindingNamed changes one saved binding during a run, as a save would.
func setBindingNamed(t *testing.T, d *daemon, name, set string, args ...any) {
	t.Helper()
	tx, cancel, err := d.db.BeginTxContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	res, err := tx.Exec(`UPDATE logbook_destination SET `+set+` WHERE forwarder_name = ?`, append(args, name)...)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("updated %d bindings named %s", n, name)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileSet_DR3_TheSetIsTheStartSnapshot(t *testing.T) {
	f := newDRFixture(t, nil)
	setBindingNamed(t, f.d, "smcloud-b", "enabled = 0")
	setBindingNamed(t, f.d, "smcloud-x", "enabled = 1")
	setBindingNamed(t, f.d, "smcloud-h", "remote_adopted_account = ?", startAccount(t, f.d))
	if _, err := f.d.cfgSvc.Update(func(c *config.Config) error {
		for i := range c.Forwarders {
			if c.Forwarders[i].Type == smcloud.Type {
				creds, _ := json.Marshal(map[string]string{"url": f.cloud.srv.URL, "token": "t2"})
				c.Forwarders[i].Credentials = creds
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("save the account change: %v", err)
	}
	mark := f.cloud.mark()
	code, results, body := reconcileAll(t, f.d)
	if code != http.StatusOK {
		t.Fatalf("aggregate = %d %s; want 200", code, body)
	}
	f.wantResults(t, results, drAllFive(`"in_sync":false`, `"cloud_logbook_id":0`))
	_, auths := f.cloud.since(mark)
	if len(auths) == 0 {
		t.Fatal("no cloud request during the aggregate")
	}
	for _, a := range auths {
		if a != "Bearer t" {
			t.Fatalf("a request carried %q; want the start's token", a)
		}
	}
}

func TestReconcileSet_DR4_Lifecycle(t *testing.T) {
	t.Run("periodic passes blocked mid-request", func(t *testing.T) {
		var exited atomic.Int32
		restore := runReconcileLoop
		runReconcileLoop = func(rec *smcloud.Reconciler, ctx context.Context) {
			defer exited.Add(1)
			for ctx.Err() == nil {
				_, _ = rec.RunOnce(ctx, smcloud.TriggerPeriodic)
				time.Sleep(20 * time.Millisecond)
			}
		}
		t.Cleanup(func() { runReconcileLoop = restore })
		f := newDRFixture(t, func(f *drFixture, _ *daemon) {
			f.cloud.mu.Lock()
			f.cloud.blockGT = true
			f.cloud.mu.Unlock()
		})
		gen1 := exited.Load() // the first generation's loop, stopped by the fixture
		waitFor := map[string]bool{"/v1/logbooks": false, f.scoped("smcloud") + "/reconcile": false}
		deadline := time.After(15 * time.Second)
		for !waitFor["/v1/logbooks"] || !waitFor[f.scoped("smcloud")+"/reconcile"] {
			select {
			case p := <-f.cloud.entered:
				if _, ok := waitFor[p]; ok {
					waitFor[p] = true
				}
			case <-deadline:
				t.Fatalf("both periodic passes never reached the cloud: %v", waitFor)
			}
		}
		stopGen(t, f.orch)
		if n := exited.Load() - gen1; n != 2 {
			t.Fatalf("%d of 2 reconcile loops had exited when the workers drained", n)
		}
		data, _ := os.ReadFile(filepath.Join(f.d.cfgSvc.WorkingDir(), "log", "smd.log"))
		if strings.Contains(string(data), "panic recovered") {
			t.Fatal("a panic was recovered during the stop")
		}
	})

	t.Run("an aggregate in flight when the stop begins", func(t *testing.T) {
		f := newDRFixture(t, dropUploadRows)
		release := f.cloud.block(f.scoped("smcloud") + "/reconcile")
		post := postAsync(f.d)
		awaitEntered(t, f.cloud, f.scoped("smcloud")+"/reconcile")
		reports := make(chan orchestrator.ShutdownReport, 1)
		go func() { reports <- f.orch.Shutdown(15*time.Second, nil) }()
		awaitNotAccepting(t, f.d)
		close(release)
		rep := <-reports
		requireDrained(t, rep)
		res := <-post
		if res.err != nil || res.code != http.StatusOK || !strings.Contains(res.body, `"enqueued_upserts":1`) {
			t.Fatalf("aggregate across the stop = %d %s (%v); want 200 with the repair", res.code, res.body, res.err)
		}
		if n := reconcileRows(t, f.d.paths.QSO, f.qso); n != 1 {
			t.Fatalf("reconcile upload rows for the QSO after the stop = %d; want 1, written before the log database closed", n)
		}
	})

	t.Run("the stop budget exhausted", func(t *testing.T) {
		f := newDRFixture(t, nil)
		release := f.cloud.block(f.scoped("smcloud") + "/reconcile")
		t.Cleanup(func() { close(release) })
		_ = postAsync(f.d)
		awaitEntered(t, f.cloud, f.scoped("smcloud")+"/reconcile")
		rep := f.orch.Shutdown(1*time.Second, nil)
		if oc := outcomeOf(rep, nodeHTTP); oc.Result == orchestrator.Drained {
			t.Fatalf("http drained with an aggregate still running: %+v", oc)
		}
		if oc := outcomeOf(rep, nodeLogDB); oc.Result != orchestrator.Skipped {
			t.Fatalf("log-db = %+v; want skipped (left open beneath the running aggregate)", oc)
		}
	})
}

// dropUploadRows removes the default logbook's queued uploads between
// generations, so a reconcile repair is the only thing that can queue one.
func dropUploadRows(_ *drFixture, gen1 *daemon) {
	db, err := sql.Open("sqlite", "file:"+gen1.paths.QSO)
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`DELETE FROM qso_upload`); err != nil {
		panic(err)
	}
}

func reconcileRows(t *testing.T, path, qsoUUID string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM qso_upload u JOIN qso q ON q.id = u.qso_id
		WHERE q.uuid = ? AND u.origin = 'reconcile'`, qsoUUID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type postResult struct {
	code int
	body string
	err  error
}

func socketClient(d *daemon) *http.Client {
	sock := d.cfgSvc.Snapshot().SocketPath
	return &http.Client{Timeout: 40 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
}

// postAsync sends the aggregate from its own goroutine, once the API answers.
func postAsync(d *daemon) <-chan postResult {
	out := make(chan postResult, 1)
	client := socketClient(d)
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for {
			resp, err := client.Post("http://smd/v1/smcloud/reconcile", "application/json", nil)
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				out <- postResult{code: resp.StatusCode, body: string(body)}
				return
			}
			if time.Now().After(deadline) {
				out <- postResult{err: err}
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return out
}

func awaitEntered(t *testing.T, c *setCloud, path string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case p := <-c.entered:
			if p == path {
				return
			}
		case <-deadline:
			t.Fatalf("the cloud never received %s", path)
		}
	}
}

// awaitNotAccepting waits until the API's listener is closed: the stop has
// begun.
func awaitNotAccepting(t *testing.T, d *daemon) {
	t.Helper()
	sock := d.cfgSvc.Snapshot().SocketPath
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("the API kept accepting after the stop began")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func requireDrained(t *testing.T, rep orchestrator.ShutdownReport) {
	t.Helper()
	for _, o := range rep.Outcomes {
		if o.Result != orchestrator.Drained {
			t.Fatalf("node %s did not drain (result %d, err %v, blocked by %v)", o.Node, o.Result, o.Err, o.BlockedBy)
		}
	}
	if len(rep.Outcomes) == 0 || rep.FirstTimedOut != "" {
		t.Fatalf("the stop did not drain cleanly (first timed out %q)", rep.FirstTimedOut)
	}
}

// runningReconcilers maps each binding with a running reconciler to its wire.
func runningReconcilers(d *daemon) map[string]string {
	out := map[string]string{}
	for _, e := range d.smcloudSet {
		if e.rec == nil {
			continue
		}
		out[e.name] = "name"
		if e.identity {
			out[e.name] = "identity"
		}
	}
	return out
}
