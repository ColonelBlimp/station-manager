package main

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
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

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/cloud/server"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

/*
   W-0021 5F.4 commit 4 (ADR 0092; rulings F4, J1–J3, J6, 2026-10-12): the
   gate lifted, end to end.

     VS2  one support check per start with an SM Cloud account, whether Home or
          a managed archive is active; none without an account. The check runs
          in the background: the start and local logging never wait on it. A
          drain cancels a check in flight, waits for it, and a cancelled check
          publishes nothing, even when it returns an allowing state.
     FH12 a legacy binding the running generation started enabled: a saved
          disable, then an enable before any restart, is refused and nothing
          is stamped; after a restart the empty binding is stamped, and the
          next generation uploads its QSO by identity only.
     FH13 a legacy restore into a Home logbook whose binding is stamped,
          enabled or disabled, is refused before any QSO is inserted; an
          unstamped target restores (control).
     EI1  two managed archives with equal labels, equal logbook names and the
          same callsign, each activated in turn and turned on for SM Cloud,
          against the real cloud server: each QSO lands in its own archive's
          cloud logbook, each scoped manifest lists only its own QSO, and no
          request takes the name wire.
     EI2  one archive's QSO pushed for the other is refused as an archive
          conflict: the original is unchanged in the source archive, and the
          target's cloud logbook is unchanged.
*/

const gateRunningReason = "this binding still uploads by its Cloud logbook name until the daemon restarts; restart, then turn it on"

// supportSeam replaces the start's support check and records its calls.
type supportSeam struct {
	mu      sync.Mutex
	urls    []string
	result  smcloud.SupportResult
	block   bool
	started chan struct{}
	exited  atomic.Bool
}

func installSupportSeam(t *testing.T, s *supportSeam) *supportSeam {
	t.Helper()
	s.started = make(chan struct{}, 16)
	old := checkSmcloudSupport
	checkSmcloudSupport = func(ctx context.Context, fc types.ForwarderConfig) (smcloud.SupportResult, error) {
		var creds struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(fc.Credentials, &creds)
		s.mu.Lock()
		s.urls = append(s.urls, creds.URL)
		s.mu.Unlock()
		s.started <- struct{}{}
		if s.block {
			<-ctx.Done()
			s.exited.Store(true)
			// A hostile answer: an allowing state beside the cancellation.
			return smcloud.SupportResult{Server: creds.URL, State: smcloud.SupportUnverified}, ctx.Err()
		}
		// The result belongs to the server asked: an empty server is never a
		// wildcard, so the seam answers for the account it was given.
		res := s.result
		res.Server, _ = smcloud.SupportServer(fc)
		return res, nil
	}
	t.Cleanup(func() { checkSmcloudSupport = old })
	return s
}

// quietSupportCheck answers the start's support check without the network,
// supported for the account's own server, for tests whose fake cloud or
// request list is about something else (W-0021 5F.4).
func quietSupportCheck(t *testing.T) {
	t.Helper()
	old := checkSmcloudSupport
	checkSmcloudSupport = func(_ context.Context, fc types.ForwarderConfig) (smcloud.SupportResult, error) {
		server, _ := smcloud.SupportServer(fc)
		return smcloud.SupportResult{Server: server, State: smcloud.SupportSupported}, nil
	}
	t.Cleanup(func() { checkSmcloudSupport = old })
}

func (s *supportSeam) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.urls)
}

func supportStateIn(t *testing.T, d *daemon) string {
	t.Helper()
	v, err := d.archives.Bindings(context.Background(), d.cfgSvc.Snapshot().ActiveQsoArchiveID)
	if err != nil {
		t.Fatal(err)
	}
	for _, dv := range v.Destinations {
		if dv.Type == smcloud.Type && dv.Support != nil {
			return dv.Support.State
		}
	}
	return ""
}

func smcloudAccountCfg(url string) func(*config.Config) {
	return func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		creds, _ := json.Marshal(map[string]string{"url": url, "token": "t"})
		c.Forwarders = []types.ForwarderConfig{{Name: "smcloud", Type: smcloud.Type, Enabled: true, Credentials: creds,
			TickIntervalSec: 1, BatchSize: 5, ActionFilter: []string{"insert", "update", "delete"}}}
	}
}

func TestGateLifecycle_VS2_OneCheckPerStart(t *testing.T) {
	t.Run("Home active, then a managed archive", func(t *testing.T) {
		const url = "https://cloud.example.org"
		seam := installSupportSeam(t, &supportSeam{result: smcloud.SupportResult{State: smcloud.SupportSupported}})
		d, orch := newOrchestratedDaemon(t, smcloudAccountCfg(url))
		if err := orch.Start(d.workerCtx); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "the first start's check", func() bool { return len(seam.calls()) == 1 })
		waitUntil(t, "the check's result in the view", func() bool { return supportStateIn(t, d) == string(smcloud.SupportSupported) })
		created, err := d.archives.Create(context.Background(), archive.CreateRequest{RequestKey: utils.NewUUIDv7(), Label: "Drill", LogbookName: "Drill", LogbookCallsign: "7Q5MLV"})
		if err != nil {
			t.Fatal(err)
		}
		stopGen(t, orch)
		if _, err := d.cfgSvc.Update(func(c *config.Config) error { c.ActiveQsoArchiveID = created.Entry.ID; return nil }); err != nil {
			t.Fatal(err)
		}
		d2, orch2 := startGen(t, d)
		waitUntil(t, "the managed start's check", func() bool { return len(seam.calls()) == 2 })
		if got := seam.calls(); got[1] != url {
			t.Fatalf("checked %v; want the station account's server", got)
		}
		waitUntil(t, "the managed start's result", func() bool { return supportStateIn(t, d2) == string(smcloud.SupportSupported) })
		stopGen(t, orch2)
		if n := len(seam.calls()); n != 2 {
			t.Fatalf("checks = %d; want one per start", n)
		}
	})

	t.Run("no SM Cloud account: no check; one saved later reads changed", func(t *testing.T) {
		seam := installSupportSeam(t, &supportSeam{})
		d, orch := newOrchestratedDaemon(t, func(c *config.Config) { c.SetupComplete = true; c.DefaultLogbookID = 1 })
		if err := orch.Start(d.workerCtx); err != nil {
			t.Fatal(err)
		}
		// No check ran for it, so it is never "still being checked": a
		// restart checks it.
		creds, _ := json.Marshal(map[string]string{"url": "https://cloud.example.org", "token": "t"})
		if _, err := d.cfgSvc.Update(func(c *config.Config) error {
			c.Forwarders = append(c.Forwarders, types.ForwarderConfig{Type: smcloud.Type, Credentials: creds})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if s := supportStateIn(t, d); s != string(smcloud.SupportChanged) {
			t.Fatalf("support for an account saved after the start = %q; want changed", s)
		}
		stopGen(t, orch)
		if got := seam.calls(); len(got) != 0 {
			t.Fatalf("checked %v without an account", got)
		}
		if res, ok := d.support.published(); ok {
			t.Fatalf("published %+v without an account", res)
		}
	})

	t.Run("a drain cancels the check, which publishes nothing", func(t *testing.T) {
		seam := installSupportSeam(t, &supportSeam{block: true})
		d, orch := newOrchestratedDaemon(t, smcloudAccountCfg("https://cloud.example.org"))
		if err := orch.Start(d.workerCtx); err != nil {
			t.Fatal(err)
		}
		select {
		case <-seam.started:
		case <-time.After(10 * time.Second):
			t.Fatal("the start's check never ran")
		}
		// Local logging does not wait on the check.
		uuid := logQsoIn(t, d, 1, "DL9UW", "080001")
		if uuid == "" {
			t.Fatal("no QSO logged while the check was in flight")
		}
		if s := supportStateIn(t, d); s != string(smcloud.SupportChecking) {
			t.Fatalf("support while in flight = %q; want checking", s)
		}
		stopGen(t, orch)
		if !seam.exited.Load() {
			t.Fatal("the drain returned before the check exited")
		}
		if res, ok := d.support.published(); ok {
			t.Fatalf("a cancelled check published %+v", res)
		}
	})
}

func TestGateLifecycle_FH12_ARestartSeparatesTheNameWireFromTheStamp(t *testing.T) {
	ctx := context.Background()
	installSupportSeam(t, &supportSeam{result: smcloud.SupportResult{State: smcloud.SupportSupported}})
	f := newBornFixture(t, "")
	id := f.d.cfgSvc.Snapshot().ActiveQsoArchiveID
	put := func(d *daemon, enabled bool) error {
		_, err := d.archives.ApplyBindings(ctx, id, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: smcloud.Type, Logbooks: []types.LogbookBindingEdit{{LogbookID: f.second, Enabled: enabled}}}}})
		return err
	}
	waitUntil(t, "the support result", func() bool { return supportStateIn(t, f.d) == string(smcloud.SupportSupported) })
	if err := put(f.d, false); err != nil {
		t.Fatalf("saving the disable: %v", err)
	}
	err := put(f.d, true)
	if err == nil || !strings.Contains(err.Error(), gateRunningReason) {
		t.Fatalf("re-enable before a restart = %v; want refused with %q", err, gateRunningReason)
	}
	if b, _ := bindingNamed(t, f.d, "smcloud-b"); b.Enabled || b.IdentitySince != nil {
		t.Fatalf("after the refusal: %+v; want disabled and unstamped", b)
	}

	stopGen(t, f.orch)
	d3, orch3 := startGen(t, f.d)
	waitUntil(t, "the support result", func() bool { return supportStateIn(t, d3) == string(smcloud.SupportSupported) })
	if err := put(d3, true); err != nil {
		t.Fatalf("after the restart, an empty binding: %v", err)
	}
	if b, _ := bindingNamed(t, d3, "smcloud-b"); !b.Enabled || b.IdentitySince == nil {
		t.Fatalf("after the enable: %+v; want enabled and stamped", b)
	}

	stopGen(t, orch3)
	d4, _ := startGen(t, d3)
	q := logQsoIn(t, d4, f.second, "DL9UW", "080001")
	waitUntil(t, "the stamped binding's upload", func() bool { return uploadStatusFor(t, d4, q, "smcloud-b") == "uploaded" })
	for _, r := range f.cloud.putsOf(q) {
		if r.path != f.scopedQsos(d4) {
			t.Fatalf("uploaded to %s; want only %s", r.path, f.scopedQsos(d4))
		}
	}
	for _, r := range f.cloud.requests() {
		if strings.Contains(r.body, "decoy-name") || strings.Contains(r.path, "decoy-name") {
			t.Fatalf("the legacy Cloud logbook name reached a request: %s %s", r.method, r.path)
		}
	}
}

func TestRestore_FH13_AStampedHomeTargetRefusesALegacyRestore(t *testing.T) {
	for name, binding := range map[string]string{
		"stamped, enabled":            `1, datetime('now')`,
		"stamped, disabled":           `0, datetime('now')`,
		"control: unstamped, enabled": `1, NULL`,
	} {
		t.Run(name, func(t *testing.T) {
			qso := types.Qso{UUID: utils.NewUUIDv7()}
			qso.Call, qso.Band, qso.Mode, qso.Freq, qso.QsoDate, qso.TimeOn = "DL9UW", "20m", "SSB", "14.255", "20260601", "142559"
			rawQso, err := json.Marshal(qso)
			if err != nil {
				t.Fatal(err)
			}
			origFetch := fetchSMCloudExport
			fetchSMCloudExport = func(context.Context, types.ForwarderConfig) (*smcloud.Export, error) {
				return &smcloud.Export{
					Logbooks: []smcloud.ExportLogbook{{ID: 7, Name: "main"}},
					Qsos:     []smcloud.ExportRecord{{UUID: qso.UUID, LogbookID: 7, ModifiedAt: time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC), Qso: rawQso}},
				}, nil
			}
			t.Cleanup(func() { fetchSMCloudExport = origFetch })
			var archivePath string
			tmp := setupImportTestbed(t, func(c *config.Config) {
				archivePath = c.Datastore.Path
				c.QsoArchives = []types.QsoArchiveConfig{{ID: archA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: archivePath}}
				c.ActiveQsoArchiveID = archA
				creds, _ := json.Marshal(map[string]string{"url": "https://cloud.example.test", "token": "test-token"})
				c.Forwarders = []types.ForwarderConfig{{Type: smcloud.Type, Enabled: true, Credentials: creds}}
			})
			cfg, err := config.Load(filepath.Join(tmp, "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			giveIdentity(t, cfg, archivePath, archA)
			raw, err := sql.Open("sqlite", "file:"+archivePath)
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				`UPDATE archive_metadata SET destination_bindings_seeded_at = datetime('now')`,
				`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, identity_since)
					VALUES (1, 'smcloud', 'smcloud', ` + binding + `)`,
			} {
				if _, err := raw.Exec(q); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
			}
			_ = raw.Close()

			err = runRestore([]string{"--cloud-logbook", "main"})
			count := qsoRowsIn(t, archivePath)
			if strings.HasPrefix(name, "control") {
				if err != nil || count != 1 {
					t.Fatalf("restore into an unstamped target = %v with %d QSO(s); want it restored", err, count)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "identity") {
				t.Fatalf("restore = %v; want a refusal naming the target's SM Cloud identity", err)
			}
			if count != 0 {
				t.Fatalf("the refused restore inserted %d QSO(s)", count)
			}
		})
	}
}

func qsoRowsIn(t *testing.T, path string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var n int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM qso`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// realCloud is the cloud server over the dev Postgres, recording every
// request's method and path. It skips without the database.
type realCloud struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []string
	answers []cloudAnswer
}

// cloudAnswer is one request and the server's answer to it.
type cloudAnswer struct {
	method, path string
	status       int
	body         string
}

type recordingWriter struct {
	http.ResponseWriter
	status int
	body   strings.Builder
}

func (w *recordingWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *recordingWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func newRealCloud(t *testing.T) *realCloud {
	t.Helper()
	dsn, skip := store.ResolveTestDSN()
	if skip != "" {
		t.Skip(skip)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("needs a dev Postgres (task db:pg:up): open: %v", err)
	}
	pctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(pctx); err != nil {
		_ = db.Close()
		t.Skipf("needs a dev Postgres (task db:pg:up): ping: %v", err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, 0x534d434c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, 0x534d434c)
		_ = conn.Close()
	})
	drop := `DROP TABLE IF EXISTS evidence_tombstones; DROP TABLE IF EXISTS evidence_records;
DROP TABLE IF EXISTS qsos; DROP TABLE IF EXISTS logbooks; DROP TABLE IF EXISTS archives;
DROP TABLE IF EXISTS tenants; DROP TABLE IF EXISTS schema_migrations`
	if _, err := db.Exec(drop); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(drop); _ = db.Close() })
	st := store.New(db)
	tenant, err := st.EnsureTenant(context.Background(), "7Q5MLV", "")
	if err != nil {
		t.Fatal(err)
	}
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	h := server.New(st, db, quiet, map[string]int64{"tok-e2e": tenant}, "test", 0).Handler()
	c := &realCloud{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.reqs = append(c.reqs, r.Method+" "+r.URL.Path)
		c.mu.Unlock()
		rw := &recordingWriter{ResponseWriter: w}
		h.ServeHTTP(rw, r)
		body := rw.body.String()
		if rw.Header().Get("Content-Encoding") == "gzip" {
			if zr, err := gzip.NewReader(strings.NewReader(body)); err == nil {
				plain, _ := io.ReadAll(zr)
				body = string(plain)
			}
		}
		c.mu.Lock()
		c.answers = append(c.answers, cloudAnswer{r.Method, r.URL.Path, rw.status, body})
		c.mu.Unlock()
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *realCloud) answersTo(method, path string) []cloudAnswer {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []cloudAnswer
	for _, a := range c.answers {
		if a.method == method && a.path == path {
			out = append(out, a)
		}
	}
	return out
}

// wantInSync runs the generation's on-demand reconcile and requires one
// in-sync identity summary for its one binding.
func wantInSync(t *testing.T, d *daemon, logbookUUID string) {
	t.Helper()
	code, results, body := reconcileAll(t, d)
	if code != http.StatusOK || len(results) != 1 || results[0].LogbookUUID != logbookUUID || results[0].Error != "" {
		t.Fatalf("reconcile = %d %s; want one result for %s", code, body, logbookUUID)
	}
	var sum struct {
		InSync     bool `json:"in_sync"`
		LocalCount int  `json:"local_count"`
		CloudCount int  `json:"cloud_count"`
	}
	if err := json.Unmarshal(results[0].Summary, &sum); err != nil || !sum.InSync || sum.LocalCount != 1 || sum.CloudCount != 1 {
		t.Fatalf("reconcile summary %s (%v); want in sync with one QSO on each side", results[0].Summary, err)
	}
}

func (c *realCloud) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.reqs)
}

type scopedManifest struct {
	ArchiveUUID string `json:"archive_uuid"`
	LogbookUUID string `json:"logbook_uuid"`
	Entries     []struct {
		UUID       string    `json:"uuid"`
		ModifiedAt time.Time `json:"modified_at"`
		Revision   int64     `json:"revision"`
		Deleted    bool      `json:"deleted"`
	} `json:"entries"`
}

func (c *realCloud) manifest(t *testing.T, archiveID, logbookUUID string) scopedManifest {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.srv.URL+"/v1/archives/"+archiveID+"/logbooks/"+logbookUUID+"/manifest", nil)
	req.Header.Set("Authorization", "Bearer tok-e2e")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest %s/%s: HTTP %d %s", archiveID, logbookUUID, resp.StatusCode, body)
	}
	var m scopedManifest
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// drillGeneration activates archive id, turns SM Cloud on for its default
// logbook, restarts so the stamped binding gets its worker, logs one QSO and
// waits for its upload. It returns the running generation, the QSO and the
// logbook's UUID.
func drillGeneration(t *testing.T, d *daemon, id string) (*daemon, *orchestrator.Orchestrator, types.Qso, string) {
	t.Helper()
	ctx := context.Background()
	if _, err := d.cfgSvc.Update(func(c *config.Config) error { c.ActiveQsoArchiveID = id; return nil }); err != nil {
		t.Fatal(err)
	}
	d2, orch2 := startGen(t, d)
	waitUntil(t, "the support check", func() bool { return supportStateIn(t, d2) == string(smcloud.SupportSupported) })
	books, err := d2.db.FetchAllLogbooksWithContext(ctx)
	if err != nil || len(books) != 1 {
		t.Fatalf("logbooks of %s: %+v, %v", id, books, err)
	}
	lb := books[0]
	if _, err := d2.archives.ApplyBindings(ctx, id, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: smcloud.Type, Logbooks: []types.LogbookBindingEdit{{LogbookID: lb.ID, Enabled: true}}}}}); err != nil {
		t.Fatalf("turn SM Cloud on in %s: %v", id, err)
	}
	stopGen(t, orch2)
	d3, orch3 := startGen(t, d2)
	uuid := logQsoIn(t, d3, lb.ID, "DL9UW", "080001")
	b, ok := func() (types.LogbookDestination, bool) {
		rows, err := d3.db.ListLogbookDestinationsWithContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.LogbookID == lb.ID {
				return r, true
			}
		}
		return types.LogbookDestination{}, false
	}()
	if !ok || b.IdentitySince == nil {
		t.Fatalf("binding in %s = %+v; want stamped", id, b)
	}
	waitUntil(t, "the upload in "+id, func() bool { return uploadStatusFor(t, d3, uuid, b.ForwarderName) == "uploaded" })
	q, err := d3.db.FetchQsoByUUIDWithContext(ctx, uuid)
	if err != nil {
		t.Fatal(err)
	}
	return d3, orch3, q, lb.UUID
}

func TestGateLifecycle_EI1_EI2_EqualNamedArchivesStayApart(t *testing.T) {
	cloud := newRealCloud(t)
	ctx := context.Background()
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		creds, _ := json.Marshal(map[string]string{"url": cloud.srv.URL, "token": "tok-e2e"})
		c.Forwarders = []types.ForwarderConfig{{Type: smcloud.Type, Enabled: true, Credentials: creds,
			TickIntervalSec: 1, BatchSize: 5, ActionFilter: []string{"insert", "update", "delete"}}}
	})
	ownerOnlySocketDir(t, d) // the reconcile check goes through the API
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for range 2 {
		created, err := d.archives.Create(ctx, archive.CreateRequest{RequestKey: utils.NewUUIDv7(), Label: "Drill", LogbookName: "Drill", LogbookCallsign: "7Q5MLV"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.Entry.ID)
	}
	stopGen(t, orch)
	// Home's own generation used names (its seeded default binding); from here
	// on only the managed archives run.
	fromHere := len(cloud.requests())
	dA, orchA, qA, lbA := drillGeneration(t, d, ids[0])
	wantInSync(t, dA, lbA)
	stopGen(t, orchA)
	dB, _, qB, lbB := drillGeneration(t, dA, ids[1])
	wantInSync(t, dB, lbB)

	mA, mB := cloud.manifest(t, ids[0], lbA), cloud.manifest(t, ids[1], lbB)
	if len(mA.Entries) != 1 || mA.Entries[0].UUID != qA.UUID {
		t.Fatalf("archive A's manifest = %+v; want only its own QSO %s", mA.Entries, qA.UUID)
	}
	if len(mB.Entries) != 1 || mB.Entries[0].UUID != qB.UUID {
		t.Fatalf("archive B's manifest = %+v; want only its own QSO %s", mB.Entries, qB.UUID)
	}
	for _, r := range cloud.requests()[fromHere:] {
		if strings.HasPrefix(r, "PUT /v1/qsos") || strings.HasPrefix(r, "GET /v1/logbooks") {
			t.Fatalf("a request took the name wire: %s", r)
		}
	}

	// EI2: archive A's QSO pushed for archive B.
	creds, _ := json.Marshal(map[string]string{"url": cloud.srv.URL, "token": "tok-e2e"})
	f, err := smcloud.NewIdentity(types.ForwarderConfig{Name: "smcloud.cross", Type: smcloud.Type, Credentials: creds},
		smcloud.IdentityTarget{ArchiveUUID: ids[1], ArchiveLabel: "Drill", LogbookUUID: lbB, LogbookLabel: "Drill", Callsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	newer := qA
	newer.Revision++
	newer.ModifiedAt = newer.ModifiedAt.Add(time.Minute)
	if res := f.Submit(ctx, newer, action.Update, ""); res.Outcome == forwarding.OutcomeSuccess {
		t.Fatal("archive A's QSO was accepted into archive B")
	}
	crossPath := "/v1/archives/" + ids[1] + "/logbooks/" + lbB + "/qsos"
	answers := cloud.answersTo(http.MethodPut, crossPath)
	if len(answers) == 0 {
		t.Fatalf("no PUT reached %s", crossPath)
	}
	last := answers[len(answers)-1]
	var refusal struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal([]byte(last.body), &refusal)
	if last.status != http.StatusConflict || refusal.Code != "archive_conflict" {
		t.Fatalf("the cross-archive PUT was answered %d %s; want 409 archive_conflict", last.status, last.body)
	}
	if after := cloud.manifest(t, ids[0], lbA); len(after.Entries) != 1 || after.Entries[0] != mA.Entries[0] {
		t.Fatalf("archive A's manifest changed: %+v → %+v", mA.Entries, after.Entries)
	}
	if after := cloud.manifest(t, ids[1], lbB); len(after.Entries) != 1 || after.Entries[0] != mB.Entries[0] {
		t.Fatalf("archive B's manifest changed: %+v → %+v", mB.Entries, after.Entries)
	}
}
