package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 4b2: the SM Cloud adoption node (ADR 0090 T1; the
   operator's 4b2 acceptance set).

     AW1  it starts after http (the archive manager is built there, and the
          archive is promoted before http) and the log database drains after it.
     AW2  on Home it starts the adopter at once and wires its status into the
          bindings view; off Home it starts nothing.
     AW3  shutdown cancels the request in flight and waits for the adopter
          before the node reports drained.
*/

func TestAdoptionNode_AW1_StartsAfterHTTPAndDrainsBeforeTheLogDB(t *testing.T) {
	// Registered in reverse, so only the declared edges can order the node.
	c := iocdi.New()
	registerDaemonBeans(t, c)
	nodes := lifecycleNodes()
	for i := len(nodes) - 1; i >= 0; i-- {
		if err := c.RegisterNode(nodes[i]); err != nil {
			t.Fatal(err)
		}
	}
	p, err := c.Plan()
	if err != nil {
		t.Fatal(err)
	}
	idx := orderIndex(t, p)
	if idx[nodeHTTP] >= idx[nodeAdoption] || idx[nodePromote] >= idx[nodeAdoption] {
		t.Fatalf("start order: http %d, promote %d, adoption %d; want adoption after both", idx[nodeHTTP], idx[nodePromote], idx[nodeAdoption])
	}
	var logDB []string
	for _, n := range lifecycleNodes() {
		if n.Name == nodeLogDB {
			logDB = n.DrainAfter
		}
	}
	found := false
	for _, n := range logDB {
		found = found || n == nodeAdoption
	}
	if !found {
		t.Fatalf("log-db DrainAfter = %v; want it to include %s", logDB, nodeAdoption)
	}
	rec, o := realGraphOrch(t, nil)
	rep := o.Shutdown(2*time.Second, nil)
	if oc := outcomeOf(rep, nodeAdoption); oc.Result != orchestrator.Drained {
		t.Fatalf("adoption outcome = %+v; want Drained", oc)
	}
	order := rec.snapshot()
	if ia, ib := indexIn(order, nodeAdoption), indexIn(order, nodeLogDB); ia < 0 || ia > ib {
		t.Fatalf("stop order %v: want adoption before the log DB", order)
	}
}

const awHome = "019fd5c5-efcc-7193-be4f-1fee532ee3a1"

// adoptionDaemon: Home (or a managed archive) active, its default logbook bound
// to SM Cloud and enabled, the station account pointing at cloud.
func adoptionDaemon(t *testing.T, cloud string, home bool) *daemon {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.DefaultConfig(filepath.Join(tmp, "data"))
	cfg.Logging.FileLogging = false
	if err := os.MkdirAll(filepath.Join(tmp, "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tmp, "cfg", "config.json")
	if _, err := config.WriteJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	cfgSvc := config.New(cfg)
	cfgSvc.SetPath(path)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}

	dbCfg := config.DefaultConfig(t.TempDir())
	dbCfg.Datastore.Path = ":memory:"
	dbCfg.Logging.FileLogging = false
	dbCfgSvc := config.New(dbCfg)
	if err := dbCfgSvc.Initialize(); err != nil {
		t.Fatal(err)
	}
	logger := logging.NewForWriter(&syncBuffer{})
	db := &sqlite.Service{ConfigService: dbCfgSvc, LoggerService: logger}
	if err := db.Initialize(); err != nil {
		t.Fatal(err)
	}
	db.DatabaseConfig = &types.DatastoreConfig{Driver: "sqlite", Path: ":memory:", MaxOpenConns: 1, MaxIdleConns: 1, ContextTimeout: 10, TransactionContextTimeout: 10}
	db.SetMigrationSets(sqlite.MigrationSetLog)
	if err := db.Open(); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	main, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Main", Callsign: "M0ABC"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{{
		LogbookID: main, Destination: "smcloud", ForwarderName: "cloud", Enabled: true, Credentials: json.RawMessage(`{"logbook":"shack"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	account, _ := json.Marshal(map[string]string{"url": cloud, "token": "tok"})
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		entry := types.QsoArchiveConfig{ID: awHome, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/x/home.db"}
		if !home {
			entry.Ownership, entry.Path = types.QsoArchiveOwnershipManaged, ""
		}
		c.QsoArchives = []types.QsoArchiveConfig{entry}
		c.ActiveQsoArchiveID = awHome
		c.DefaultLogbookID = main
		c.Forwarders = []types.ForwarderConfig{{Type: "smcloud", Credentials: account}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	running, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d := &daemon{cfgSvc: cfgSvc, db: db, logger: logger, bindingsAtStart: running}
	d.archives = archive.NewManager(cfgSvc, logger, func(string) bool { return true })
	d.archives.SetActiveBindings(db, running)
	return d
}

func TestAdoptionNode_AW2_StartsOnHomeOnly(t *testing.T) {
	t.Run("off Home", func(t *testing.T) {
		requests := make(chan string, 8)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests <- r.URL.Path }))
		defer ts.Close()
		d := adoptionDaemon(t, ts.URL, false)
		if err := d.startAdoption(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.adopter != nil {
			t.Fatal("an adopter was started off Home")
		}
		if err := d.stopAdoption(); err != nil {
			t.Fatal(err)
		}
		select {
		case p := <-requests:
			t.Fatalf("a request off Home: %s", p)
		case <-time.After(100 * time.Millisecond):
		}
	})
	t.Run("on Home: at once, its status on the bindings view", func(t *testing.T) {
		requests := make(chan string, 8)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests <- r.URL.Path
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer ts.Close()
		d := adoptionDaemon(t, ts.URL, true)
		if err := d.startAdoption(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.stopAdoption() }()
		select {
		case p := <-requests:
			if p != "/v1/version" {
				t.Fatalf("first request %s; want /v1/version", p)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no attempt at start")
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			v, err := d.archives.Bindings(context.Background(), awHome)
			if err != nil {
				t.Fatal(err)
			}
			if a := v.Destinations[len(v.Destinations)-1].Logbooks[0].Adoption; a != nil && a.State == "unreachable" {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the bindings view never showed the adopter's status: %+v", v.Destinations)
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}

func TestAdoptionNode_AW3_ShutdownCancelsAndDrains(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer ts.Close()
	d := adoptionDaemon(t, ts.URL, true)
	if err := d.startAdoption(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no request in flight")
	}
	d.adoptionPrepareStop()
	stopped := make(chan error, 1)
	go func() { stopped <- d.stopAdoption() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("stop returned before the request in flight was cancelled")
	}
}
