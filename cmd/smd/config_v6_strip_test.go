package main

// Config v6 at startup (W-0021 5C, ADR 0082 part 4; rulings R3, 2026-10-06).
// Once the adopted Home archive's binding seed has committed, startup strips
// the deprecated v5 fields — `name`, `enabled` and logbook-scoped credentials —
// from config.json, file-first. No copy of the file is taken first (W-0027 C2e;
// operator ruling 2026-11-18, "remove the downgrade tooling"): the once-only
// config.v5.json of earlier builds is no longer written, read or required, and
// one an install already has is left exactly as it is.
//
//   T1  Home seeded → config.json holds station accounts only; bindings and
//       routes are unchanged; no copy is made. From a file at the head version
//       and from one an older build left at version 5.
//   T2  another archive active → nothing is stripped, and the log says the
//       fields wait for Home (operator, 2026-10-06: the wait was silent). T2b:
//       once stripped, a start with another archive active does not log it.
//   T4  a failed strip write leaves the file as it was; startup still succeeds;
//       the next start strips. An existing config.v5.json, or anything else
//       under that name, is never touched.
//   T5  the two shapes that used to wait for a copy that v5 could not accept
//       are stripped like any other.
//   T7  a nameless account still seeds under its type's name, and the
//       registry's default accounts carry no name or enabled state.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// stripFixture is the station's legacy shape: an enabled entry with no
// logbook keys, a disabled SM Cloud entry and a disabled entry of the seed-scope
// test type, both carrying logbook-scoped keys.
func stripFixture(t *testing.T) func(*config.Config) {
	return func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{
			{Name: "qrz", Type: stub.Type, Enabled: true, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1},
			{Name: "smcloud", Type: smcloud.Type, Enabled: false, TickIntervalSec: 1, BatchSize: 1,
				Credentials: json.RawMessage(`{"url":"http://127.0.0.1:9","token":"T1-TOKEN","logbook":"t1-book"}`)},
			{Name: "scope", Type: "seedscope-test", Enabled: false,
				Credentials: json.RawMessage(`{"url":"https://scope.example.test","token":"T1-SCOPE","logbook":"t1-scope-book","callsign":"T1CALL"}`)},
		}
	}
}

// asV5 re-stamps a written config.json as version 5: what an upgrading
// station's file held before this build first persisted it.
func asV5(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["version"] = 5
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diskForwarders(t *testing.T, path string) (int, []map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version    int              `json:"version"`
		Forwarders []map[string]any `json:"forwarders"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Version, doc.Forwarders
}

func assertStripped(t *testing.T, label, path string) {
	t.Helper()
	v, fwds := diskForwarders(t, path)
	if v != config.CurrentSchemaVersion() {
		t.Fatalf("%s: config.json version %d, want the head %d", label, v, config.CurrentSchemaVersion())
	}
	want := map[string]string{stub.Type: "mode", smcloud.Type: "token,url", "seedscope-test": "token,url"}
	for _, f := range fwds {
		typ, _ := f["type"].(string)
		for _, k := range []string{"name", "enabled"} {
			if _, ok := f[k]; ok {
				t.Fatalf("%s: the %s account still holds %q: %v", label, typ, k, f)
			}
		}
		creds, _ := f["credentials"].(map[string]any)
		keys := make([]string, 0, len(creds))
		for k := range creds {
			keys = append(keys, k)
		}
		sortStrings(keys)
		if w, ok := want[typ]; ok && strings.Join(keys, ",") != w {
			t.Fatalf("%s: the %s account's credentials hold %v; want the station keys %s only", label, typ, keys, w)
		}
	}
}

func assertUnstripped(t *testing.T, label, path string) {
	t.Helper()
	_, fwds := diskForwarders(t, path)
	for _, f := range fwds {
		if f["type"] == smcloud.Type {
			creds, _ := f["credentials"].(map[string]any)
			if f["name"] != "smcloud" || creds["logbook"] != "t1-book" {
				t.Fatalf("%s: the SM Cloud entry lost its legacy fields: %v", label, f)
			}
			return
		}
	}
	t.Fatalf("%s: no SM Cloud entry on disk", label)
}

func copyPath(cfgSvc *config.Service) string {
	return filepath.Join(filepath.Dir(cfgSvc.Path), "config.v5.json")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestConfigV6Strip_T1_HomeSeededStripsAndMakesNoCopy(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, stripFixture(t))
	if v, _ := diskForwarders(t, d.cfgSvc.Path); v != config.CurrentSchemaVersion() {
		t.Fatalf("the fixture: config.json is version %d, want the head", v)
	}
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := d.db.ListLogbookDestinationsWithContext(ctx)
	if err != nil || len(rows) != 3 {
		t.Fatalf("T1: bindings = %+v (%v); want the three seeded", rows, err)
	}
	assertStripped(t, "T1", d.cfgSvc.Path)
	if routes := d.qso.DestinationRoutes(); len(routes) != 3 || routes[0].Config.Name != "qrz" || !routes[0].Config.Enabled {
		t.Fatalf("T1: routes = %+v; want the seeded qrz binding enabled, routes unchanged by the strip", routes)
	}
	if _, err := os.Lstat(copyPath(d.cfgSvc)); !os.IsNotExist(err) {
		t.Fatalf("T1: a recovery copy was made (stat err %v); want none", err)
	}
}

// The same from a file an older build left at version 5: the load migrates it, the seed
// commits, and it is stripped at this start with no copy.
//
// The daemon is built on what config.Load returns for that file, as run() builds it, and
// not on the service that seeded the fixture: that one was made from a typed Config at the
// head version and never read the file, so it would pass with the version 5 step missing
// (review, 2026-11-18).
func TestConfigV6Strip_T1b_AVersion5FileIsStrippedAtItsFirstStart(t *testing.T) {
	fixture := stripFixture(t)
	path := seedOrchestratedConfig(t, func(c *config.Config) {
		fixture(c)
		// The load adds a nameless default account for every registered type the file has
		// no entry of, and a nameless account seeds under its type's name. The shared
		// fixture names its stub account "qrz", which is the name the default qrz account
		// seeds under, so this file's account is named as no type is.
		c.Forwarders[0].Name = "station-stub"
	}).Path
	if err := os.WriteFile(path, asV5(t, path), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("T1b: loading the version 5 file: %v", err)
	}
	// The load migrates in memory only: the file is still the older build's.
	if v, _ := diskForwarders(t, path); v != 5 {
		t.Fatalf("the fixture: config.json is version %d after the load, want 5", v)
	}
	assertUnstripped(t, "T1b before the start", path)
	cfgSvc := config.New(loaded)
	cfgSvc.SetPath(path)
	d, orch := buildOrchestratedDaemon(t, cfgSvc, cfgSvc.Snapshot())
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertStripped(t, "T1b", cfgSvc.Path)
	if _, err := os.Lstat(copyPath(cfgSvc)); !os.IsNotExist(err) {
		t.Fatalf("T1b: a recovery copy was made (stat err %v); want none", err)
	}
}

func TestConfigV6Strip_T2_AnotherArchiveActiveStripsNothing(t *testing.T) {
	const managed = "019fd5c5-efcc-7193-be4f-1fee532ee319"
	var managedPath string
	fixture := stripFixture(t)
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		fixture(c)
		managedPath = filepath.Join(c.DataDir, "db", "qso-archives", managed+".db")
		c.QsoArchives = []types.QsoArchiveConfig{{ID: managed, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged}}
		c.ActiveQsoArchiveID = managed
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
		t.Fatal(err)
	}
	_ = raw.Close()
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertUnstripped(t, "T2", d.cfgSvc.Path)
	if n := waitLines(t, d); n != 1 {
		t.Fatalf("T2: %d 'kept until Home' log line(s); want 1 naming the wait", n)
	}
}

// waitLines counts the startup line that says the deprecated fields wait for
// Home to be active.
func waitLines(t *testing.T, d *daemon) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(d.cfgSvc.WorkingDir(), "log", "smd.log"))
	if err != nil {
		t.Fatalf("read smd.log: %v", err)
	}
	return strings.Count(string(data), "config v6: config.json keeps its deprecated forwarder fields until the Home archive is active")
}

func TestConfigV6Strip_T2b_AlreadyStrippedWithAnotherArchiveActiveIsQuiet(t *testing.T) {
	const managed = "019fd5c5-efcc-7193-be4f-1fee532ee31c"
	var managedPath string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		// Already stripped: station accounts only.
		c.Forwarders = []types.ForwarderConfig{{Type: stub.Type, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1}}
		managedPath = filepath.Join(c.DataDir, "db", "qso-archives", managed+".db")
		c.QsoArchives = []types.QsoArchiveConfig{{ID: managed, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged}}
		c.ActiveQsoArchiveID = managed
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
		t.Fatal(err)
	}
	_ = raw.Close()
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if n := waitLines(t, d); n != 0 {
		t.Fatalf("T2b: a stripped file logged the wait %d time(s); want none", n)
	}
}

func TestConfigV6Strip_T4_FailedStripWriteIsRetriedAndAnExistingCopyIsLeftAlone(t *testing.T) {
	cfgSvc := seedOrchestratedConfig(t, stripFixture(t))
	dir := filepath.Dir(cfgSvc.Path)
	// A copy an earlier build made is never read, changed or removed.
	const earlier = `{"version":5,"earlier":"copy"}`
	if err := os.WriteFile(copyPath(cfgSvc), []byte(earlier), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	d, orch := buildOrchestratedDaemon(t, cfgSvc, cfgSvc.Snapshot())
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("T4: a failed strip write must not fail the start: %v", err)
	}
	assertUnstripped(t, "T4", cfgSvc.Path)
	orch.Shutdown(2*time.Second, nil)

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	d2, orch2 := buildOrchestratedDaemon(t, cfgSvc, cfgSvc.Snapshot())
	if err := orch2.Start(d2.workerCtx); err != nil {
		t.Fatalf("T4: second start: %v", err)
	}
	assertStripped(t, "T4 retry", cfgSvc.Path)
	if got, _ := os.ReadFile(copyPath(cfgSvc)); string(got) != earlier {
		t.Fatalf("T4: the existing copy was changed: %q", got)
	}
	if info, err := os.Stat(copyPath(cfgSvc)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("T4: the existing copy's mode changed: %v (%v)", info.Mode().Perm(), err)
	}
}

// Whatever holds the copy's name, a directory here, is no obstacle and is left where it is:
// the strip no longer asks about that name at all.
func TestConfigV6Strip_T4b_SomethingElseUnderTheCopysNameIsNoObstacle(t *testing.T) {
	cfgSvc := seedOrchestratedConfig(t, stripFixture(t))
	if err := os.Mkdir(copyPath(cfgSvc), 0o700); err != nil {
		t.Fatal(err)
	}
	d, orch := buildOrchestratedDaemon(t, cfgSvc, cfgSvc.Snapshot())
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertStripped(t, "T4b", cfgSvc.Path)
	if info, err := os.Lstat(copyPath(cfgSvc)); err != nil || !info.IsDir() {
		t.Fatalf("T4b: the directory under the copy's name was disturbed (%v)", err)
	}
}

// Two shapes used to wait: no version 5 copy could be made of them, so the strip was
// deferred. With no copy to make, they are stripped like any other. The facts they hold
// are in Home's bindings by then, which is the strip's one condition.
func TestConfigV6Strip_T5_ShapesThatWaitedForACopyAreStripped(t *testing.T) {
	for name, mutate := range map[string]func(*config.Config){
		"an entry with no name, which v5 could not represent": func(c *config.Config) {
			c.Forwarders[0].Name = ""
		},
		"evidence sync on with SM Cloud disabled, which v5 refused": func(c *config.Config) {
			c.Evidence = types.EvidenceConfig{Capture: true, Sync: true, CapBytes: 524288000}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := stripFixture(t)
			d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
				fixture(c)
				mutate(c)
			})
			if err := orch.Start(d.workerCtx); err != nil {
				t.Fatalf("start: %v", err)
			}
			assertStripped(t, "T5", d.cfgSvc.Path)
			if _, err := os.Lstat(copyPath(d.cfgSvc)); !os.IsNotExist(err) {
				t.Fatalf("T5: a recovery copy was made (stat err %v); want none", err)
			}
		})
	}
}

func TestConfigV6Strip_T7_NamelessAccountsSeedUnderTheirType(t *testing.T) {
	seeds, err := destinationSeeds([]types.ForwarderConfig{{Type: stub.Type, Credentials: stubCreds(t)}})
	if err != nil || len(seeds) != 1 || seeds[0].LegacyName != stub.Type {
		t.Fatalf("T7: seeds = %+v (%v); want the account seeded under its type's name", seeds, err)
	}
	for _, fc := range forwarding.DefaultForwarderConfigs() {
		if fc.Name != "" || fc.Enabled {
			t.Fatalf("T7: default account %q carries name %q / enabled %v; want neither", fc.Type, fc.Name, fc.Enabled)
		}
	}
}
