package main

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0082 part 4 (W-0021 5B): the adopted Home archive's ONE-TIME seed of
// destination bindings from the legacy config entries, run at start once the
// file's identity is in place, and the routing snapshot built from them; a
// managed archive records an empty seed and routes nothing.

func init() {
	forwarding.RegisterForwarderType("seedscope-test", "Seed Scope",
		[]forwarding.Action{action.Insert},
		[]forwarding.CredentialField{
			{Key: "url", Label: "URL", Kind: "text", Scope: forwarding.ScopeStation},
			{Key: "token", Label: "Token", Kind: "password", Scope: forwarding.ScopeStation},
			{Key: "logbook", Label: "Logbook", Kind: "text", Scope: forwarding.ScopeLogbook},
			{Key: "callsign", Label: "Callsign", Kind: "text", Scope: forwarding.ScopeLogbook},
		})
}

// Only a type's logbook-scoped keys travel into the seed; the station-scoped
// rest stays in config.json. Enabled state and the durable name are carried.
func TestDestinationSeeds_CopiesOnlyLogbookScopedKeys(t *testing.T) {
	seeds, err := destinationSeeds([]types.ForwarderConfig{
		{Name: "cloud", Type: "seedscope-test", Enabled: false,
			Credentials: json.RawMessage(`{"url":"https://c","token":"t","logbook":"main","callsign":"M0ABC"}`)},
		{Name: "stub-one", Type: stub.Type, Enabled: true, Credentials: json.RawMessage(`{"mode":"always_success"}`)},
		{Name: "bare", Type: stub.Type, Enabled: true},
	})
	if err != nil || len(seeds) != 3 {
		t.Fatalf("seeds = %d (%v), want one per entry", len(seeds), err)
	}
	var got map[string]string
	if err := json.Unmarshal(seeds[0].Credentials, &got); err != nil || len(got) != 2 || got["logbook"] != "main" || got["callsign"] != "M0ABC" {
		t.Fatalf("seed[0].Credentials = %s (%v); want only the logbook-scoped keys", seeds[0].Credentials, err)
	}
	if seeds[0].Destination != "seedscope-test" || seeds[0].LegacyName != "cloud" || seeds[0].Enabled {
		t.Fatalf("seed[0] = %+v", seeds[0])
	}
	// The stub's only field is station-scoped → no binding credentials at all.
	if seeds[1].Credentials != nil || !seeds[1].Enabled || seeds[1].LegacyName != "stub-one" {
		t.Fatalf("seed[1] = %+v; want enabled, no credentials", seeds[1])
	}
	if seeds[2].Credentials != nil {
		t.Fatalf("seed[2] = %+v; want no credentials for an entry without a blob", seeds[2])
	}
}

// A credential blob that is not a JSON object — valid JSON such as a string or
// an array can sit inside a valid config file — and a type with no descriptor
// are refused BY NAME before the seed transaction: the marker would make a
// silent "no credentials" final (operator review of 5B(a), finding 2).
func TestDestinationSeeds_RefusesNonObjectBlobsAndUnknownTypes(t *testing.T) {
	cases := []struct {
		name string
		fc   types.ForwarderConfig
	}{
		{"string blob", types.ForwarderConfig{Name: "s", Type: "seedscope-test", Credentials: json.RawMessage(`"abc"`)}},
		{"array blob", types.ForwarderConfig{Name: "a", Type: "seedscope-test", Credentials: json.RawMessage(`[1]`)}},
		{"malformed blob", types.ForwarderConfig{Name: "m", Type: "seedscope-test", Credentials: json.RawMessage(`{not json`)}},
		{"unknown type", types.ForwarderConfig{Name: "u", Type: "no-such-type", Credentials: json.RawMessage(`{}`)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := destinationSeeds([]types.ForwarderConfig{c.fc})
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", c.fc.Name)) {
				t.Fatalf("%s: err = %v; want a refusal naming forwarder %q", c.name, err, c.fc.Name)
			}
		})
	}
}

func TestLifecycle_LegacyArchiveSeedsBindingsFromConfigOnceAndRoutesByThem(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		// The station's shape: setup complete, one default logbook (row 1 is
		// created by ensureDefaultLogbook from the station callsign).
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		cloud, err := json.Marshal(map[string]string{"url": "http://127.0.0.1:9", "token": "t", "logbook": "home"})
		if err != nil {
			t.Fatal(err)
		}
		c.Forwarders = []types.ForwarderConfig{
			{Name: "qrz", Type: stub.Type, Enabled: true, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1},
			{Name: "smcloud", Type: smcloud.Type, Enabled: false, Credentials: cloud, TickIntervalSec: 1, BatchSize: 1},
		}
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := d.db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{ // config order, kept by the listing
		fmt.Sprintf("%d/stub/qrz/true//qrz", d.cfg.DefaultLogbookID),
		fmt.Sprintf("%d/smcloud/smcloud/false/{\"logbook\":\"home\"}/smcloud", d.cfg.DefaultLogbookID),
	}
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%d/%s/%s/%v/%s/%s", r.LogbookID, r.Destination, r.ForwarderName, r.Enabled, string(r.Credentials), r.LegacyName))
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("bindings after the first start = %v\nwant %v", got, want)
	}
	if at, err := d.db.DestinationBindingsSeededAtWithContext(ctx); err != nil || at == nil {
		t.Fatalf("marker = %v (%v); want set", at, err)
	}
	// The routing snapshot: both bindings resolved against their accounts, the
	// station's URL and token merged into the smcloud route.
	routes := d.qso.DestinationRoutes()
	if len(routes) != 2 || routes[0].Config.Name != "qrz" || !routes[0].Config.Enabled || routes[1].Config.Name != "smcloud" || routes[1].Config.Enabled {
		t.Fatalf("routes = %+v", routes)
	}
	var creds map[string]string
	if err := json.Unmarshal(routes[1].Config.Credentials, &creds); err != nil || creds["url"] != "http://127.0.0.1:9" || creds["token"] != "t" || creds["logbook"] != "home" {
		t.Fatalf("smcloud route credentials = %s (%v); want the account's url/token and the binding's logbook", routes[1].Config.Credentials, err)
	}
	// The seed is final: a later start with a different config changes nothing.
	later := d.cfg
	later.Forwarders = []types.ForwarderConfig{{Name: "clublog", Type: stub.Type, Enabled: true}}
	if err := seedDestinationBindings(ctx, d.db, later, d.paths, d.logger); err != nil {
		t.Fatal(err)
	}
	if again, _ := d.db.ListLogbookDestinationsWithContext(ctx); len(again) != 2 || again[0].ForwarderName != "qrz" {
		t.Fatalf("bindings after a second seed = %+v; want the first two unchanged", again)
	}
}

// A legacy archive whose config carries a non-object credential blob does not
// start: the seed refuses by name, nothing is marked.
func TestLifecycle_LegacyArchiveRefusesToSeedFromAMalformedBlob(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{{Name: "qrz", Type: stub.Type, Enabled: false, Credentials: json.RawMessage(`"not-an-object"`)}}
	})
	err := orch.Start(d.workerCtx)
	if err == nil || !strings.Contains(err.Error(), `"qrz"`) || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("start = %v; want a refusal naming forwarder \"qrz\" and the non-object blob", err)
	}
	if at, _ := d.db.DestinationBindingsSeededAtWithContext(context.Background()); at != nil {
		t.Fatal("marker set although the seed was refused")
	}
}

// A managed archive never seeds from config: its start records the empty
// decision (the marker), leaves no binding behind and routes nothing.
func TestLifecycle_ManagedArchiveStartRecordsAnEmptySeed(t *testing.T) {
	const managed = "019fd5c5-efcc-7193-be4f-1fee532ee318"
	var managedPath string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		managedPath = filepath.Join(c.DataDir, "db", "qso-archives", managed+".db")
		c.QsoArchives = []types.QsoArchiveConfig{{ID: managed, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged}}
		c.ActiveQsoArchiveID = managed
		c.Forwarders = []types.ForwarderConfig{{
			Name: "qrz", Type: stub.Type, Enabled: true, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1,
		}}
	})
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	seedFailedUploads(t, managedPath, "qrz")
	raw, err := sql.Open("sqlite", "file:"+managedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, ?, 1)`, managed); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	_ = raw.Close()

	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if at, err := d.db.DestinationBindingsSeededAtWithContext(ctx); err != nil || at == nil {
		t.Fatalf("marker = %v (%v); want set with no rows", at, err)
	}
	if rows, err := d.db.ListLogbookDestinationsWithContext(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("bindings = %+v (%v); want none in a managed archive", rows, err)
	}
	if routes := d.qso.DestinationRoutes(); len(routes) != 0 {
		t.Fatalf("routes = %+v; want none", routes)
	}
}

// Operator review of 5B(b), P1: a binding whose station account is missing
// resolves to no route and no worker, but its queued rows are KEPT — only a
// name that no binding carries at all is discarded.
func TestLifecycle_UnresolvedBindingKeepsItsQueuedRows(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = nil // the account of the seeded binding below is gone
	})
	// A prior start seeded a `qrz` binding of type stub on logbook 1 and left a
	// pending row under it; a row under a name no binding carries sits beside it.
	seedFailedUploads(t, logDB, "qrz")
	raw, err := sql.Open("sqlite", "file:"+logDB)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id, destination_bindings_seeded_at) VALUES (1, '01920000-0000-7000-8000-000000000001', 1, datetime('now'))`,
		`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, legacy_name) VALUES (1, 'stub', 'qrz', 1, 'qrz')`,
		`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (1, 'orphan', 'stub', 'insert', 'pending', 'live')`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_ = raw.Close()

	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	if routes := d.qso.DestinationRoutes(); len(routes) != 0 {
		t.Fatalf("routes = %+v; want none (no station account for the binding)", routes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]int{}
	for _, r := range rows {
		names[r.ForwarderName]++
	}
	if names["qrz"] != 1 || names["orphan"] != 0 {
		t.Fatalf("rows after start = %v; want the unresolved binding's row kept and the orphan discarded", names)
	}
}

// Operator review of 5B(b), second round, P1: the per-binding discard rule
// applies to a DISABLED binding whether or not its account resolves; only an
// ENABLED binding without an account keeps its rows.
func TestLifecycle_DisabledUnresolvedBindingIsDiscardedEnabledOneKept(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = nil // no station account for either binding
	})
	seedFailedUploads(t, logDB, "keep") // qso 1 (auth-failed) + qso 2 (failed) under `keep`
	raw, err := sql.Open("sqlite", "file:"+logDB)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id, destination_bindings_seeded_at) VALUES (1, '01920000-0000-7000-8000-000000000001', 1, datetime('now'))`,
		`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, legacy_name) VALUES (1, 'stub', 'keep', 1, 'keep')`,
		`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, legacy_name) VALUES (1, 'stub2', 'drop', 0, 'drop')`,
		`INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (2, 'drop', 'stub2', 'update', 'pending', 'edit')`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_ = raw.Close()

	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	names := map[string]int{}
	for _, id := range []int64{1, 2} {
		rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			names[r.ForwarderName]++
		}
	}
	if names["keep"] != 2 || names["drop"] != 0 {
		t.Fatalf("rows after start = %v; want the enabled-unresolved binding's 2 rows kept and the disabled-unresolved binding's row discarded", names)
	}
	// The operator evidence says the same thing the daemon did (second review
	// round, P2): "kept" for the enabled binding, "discarded" for the disabled one.
	data, err := os.ReadFile(filepath.Join(d.cfgSvc.WorkingDir(), "log", "smd.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if !strings.Contains(log, `"forwarder":"keep"`) || !strings.Contains(log, "its queued rows are kept") {
		t.Fatalf("no 'kept' line for the enabled unresolved binding in:\n%s", log)
	}
	if !strings.Contains(log, `"forwarder":"drop"`) || !strings.Contains(log, "is disabled; its queued rows are discarded") {
		t.Fatalf("no 'discarded' line for the disabled unresolved binding in:\n%s", log)
	}
	if strings.Contains(log, `"forwarder":"drop","destination":"stub2","logbook_id":1,"error":`) &&
		strings.Contains(log[strings.Index(log, `"forwarder":"drop"`):], "its queued rows are kept)") {
		t.Fatal("the disabled unresolved binding was logged as keeping rows it then discarded")
	}
}

// Codex P1 on 7990011b: an ENABLED station entry the daemon cannot construct is
// refused BEFORE the permanent seed, so config.json stays the thing to fix —
// the marker stays NULL and no binding is written; a corrected config on the
// same file then seeds.
func TestLifecycle_UnbuildableEnabledEntryIsRefusedBeforeTheSeed(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{{Name: "qrz", Type: stub.Type, Enabled: true, Credentials: json.RawMessage(`{"mode":"bogus"}`), TickIntervalSec: 1, BatchSize: 1}}
	})
	err := orch.Start(d.workerCtx)
	if err == nil || !strings.Contains(err.Error(), `"qrz"`) || !strings.Contains(err.Error(), "before the archive's bindings are seeded") {
		t.Fatalf("start = %v; want a refusal naming forwarder \"qrz\" before the seed", err)
	}
	ctx := context.Background()
	if at, _ := d.db.DestinationBindingsSeededAtWithContext(ctx); at != nil {
		t.Fatal("marker set although the enabled entry could not be constructed")
	}
	if rows, _ := d.db.ListLogbookDestinationsWithContext(ctx); len(rows) != 0 {
		t.Fatalf("bindings written by a refused seed: %+v", rows)
	}

	// The operator fixes config.json; the next generation on the same file seeds.
	if _, err := d.cfgSvc.Update(func(c *config.Config) error {
		c.Forwarders[0].Credentials = stubCreds(t)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	d2, orch2 := buildOrchestratedDaemon(t, d.cfgSvc, d.cfgSvc.Snapshot())
	if err := orch2.Start(d2.workerCtx); err != nil {
		t.Fatalf("start after the fix: %v", err)
	}
	rows, err := d2.db.ListLogbookDestinationsWithContext(ctx)
	if err != nil || len(rows) != 1 || rows[0].ForwarderName != "qrz" || !rows[0].Enabled {
		t.Fatalf("bindings after the fix = %+v (%v); want the qrz binding seeded enabled", rows, err)
	}
}

// Converse of the pre-seed check (operator review): once the seed is decided,
// the binding owns the logbook-scoped credential and config's copy is a
// deprecated shadow — a later start with a config entry that can no longer be
// constructed on its own (the QRZ key gone from config.json) still succeeds,
// because the seeded binding supplies the key.
func TestLifecycle_SeededBindingStaysAuthoritativeOverAShadowedConfigEntry(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: true,
			Credentials: json.RawMessage(`{"api_key":"KEY-IN-THE-BINDING"}`), TickIntervalSec: 1, BatchSize: 1}}
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("first start (seeds): %v", err)
	}
	ctx := context.Background()
	if at, _ := d.db.DestinationBindingsSeededAtWithContext(ctx); at == nil {
		t.Fatal("first start did not seed")
	}
	orch.Shutdown(2*time.Second, nil)

	// config.json loses the key; on its own the entry no longer constructs.
	if _, err := d.cfgSvc.Update(func(c *config.Config) error {
		c.Forwarders[0].Credentials = json.RawMessage(`{}`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := forwarding.Build(d.cfgSvc.Snapshot().Forwarders[0]); err == nil {
		t.Fatal("fixture: the shadowed config entry must not construct on its own")
	}
	d2, orch2 := buildOrchestratedDaemon(t, d.cfgSvc, d.cfgSvc.Snapshot())
	if err := orch2.Start(d2.workerCtx); err != nil {
		t.Fatalf("start after the seed with a shadowed config entry: %v; the binding must be authoritative", err)
	}
	routes := d2.qso.DestinationRoutes()
	if len(routes) != 1 || !routes[0].Config.Enabled || !strings.Contains(string(routes[0].Config.Credentials), "KEY-IN-THE-BINDING") {
		t.Fatalf("routes = %+v; want the seeded qrz binding, enabled, carrying its own key", routes)
	}
	// Both generations log into the same smd.log: the second start must have
	// added a qrz worker line of its own (two in all).
	if started, _ := workerNamesStarted(t, filepath.Join(d2.cfgSvc.WorkingDir(), "log", "smd.log")); len(started) != 2 || started[1] != "qrz" {
		t.Fatalf("workers started across both generations = %v; want [qrz qrz]", started)
	}
}

// 5D review, P1, end to end with the real QRZ constructor: a DISABLED legacy
// entry whose key is not a string is supported — the daemon starts and seeds
// the binding disabled with the blob as it is (disabled entries are never
// constructed) — but the bindings port refuses to turn it on as it is, with no
// row changed; retyping a valid key while enabling succeeds.
func TestLifecycle_DisabledMalformedSeedIsKeptAndCannotBeEnabledAsIs(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: false,
			Credentials: json.RawMessage(`{"api_key":123}`), TickIntervalSec: 1, BatchSize: 1}}
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start with a disabled malformed entry: %v; want it supported", err)
	}
	ctx := context.Background()
	before, err := d.db.ListLogbookDestinationsWithContext(ctx)
	if err != nil || len(before) != 1 || before[0].Enabled || string(before[0].Credentials) != `{"api_key":123}` {
		t.Fatalf("seeded = %+v (%v); want one disabled qrz binding holding the blob as it is", before, err)
	}
	id := d.cfgSvc.Snapshot().ActiveQsoArchiveID
	enable := func(creds map[string]string) error {
		_, err := d.archives.ApplyBindings(ctx, id, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: before[0].LogbookID, Enabled: true, Credentials: creds}}}}})
		return err
	}
	err = enable(nil)
	var re *archive.RequestError
	if !stderr.As(err, &re) || re.Code != "binding_unusable" {
		t.Fatalf("enabling as it is: %v; want binding_unusable", err)
	}
	after, _ := d.db.ListLogbookDestinationsWithContext(ctx)
	if fmt.Sprintf("%+v", after) != fmt.Sprintf("%+v", before) {
		t.Fatalf("a refused enable changed rows:\nbefore %+v\nafter  %+v", before, after)
	}
	if err := enable(map[string]string{"api_key": "A-VALID-KEY"}); err != nil {
		t.Fatalf("enabling with a valid key: %v", err)
	}
	rows, _ := d.db.ListLogbookDestinationsWithContext(ctx)
	if len(rows) != 1 || !rows[0].Enabled || !strings.Contains(string(rows[0].Credentials), "A-VALID-KEY") {
		t.Fatalf("after the valid enable = %+v", rows)
	}
}
