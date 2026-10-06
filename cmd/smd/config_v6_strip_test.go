package main

// Config v6 at startup (W-0021 5C, ADR 0082 part 4; rulings R3, 2026-10-06).
// Once the adopted Home archive's binding seed has committed, startup strips
// the deprecated v5 fields — `name`, `enabled` and logbook-scoped credentials —
// from config.json, file-first, after writing a once-only owner-only recovery
// copy, config.v5.json.
//
//   T1  Home seeded → config.json holds station accounts only; bindings and
//       routes are unchanged; config.v5.json is the v5 bytes from before
//       startup's persist, 0600.
//   T2  another archive active → nothing is stripped and no copy is written, and
//       the log says the fields wait for Home (operator, 2026-10-06: the wait
//       was silent). T2b: once stripped, a start with another archive active
//       does not log it.
//   T3  a failed copy defers the strip; startup still succeeds; the next start
//       copies and strips. T3b: staging files an interrupted copy left behind
//       (secret-bearing) never block a retry and are removed by it.
//   T4  a failed strip write leaves the file as it was; startup still succeeds;
//       the next start strips. An existing copy is never overwritten.
//   T5  with no v5 bytes from this start, the copy is rebuilt from the
//       unstripped v6 file re-stamped as v5 — only when that validates as a
//       complete v5 document; otherwise the strip is deferred.
//   T6  the copy takes persistResolvedConfig's ClubLog scrub: `credentials.api`
//       goes only when the build carries a nonblank injected key; the logbook
//       credentials stay. Keyed and keyless builds, synthetic values.
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
	"github.com/ColonelBlimp/station-manager/internal/forwarding/clublog"
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

// asV5 re-stamps a written v6 config.json as version 5: what an upgrading
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
	if v != 6 {
		t.Fatalf("%s: config.json version %d, want 6", label, v)
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

func TestConfigV6Strip_T1_HomeSeededStripsAfterTheCopy(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, stripFixture(t))
	v5 := asV5(t, d.cfgSvc.Path)
	d.configAtStart = v5
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
	got, err := os.ReadFile(copyPath(d.cfgSvc))
	if err != nil || string(got) != string(v5) {
		t.Fatalf("T1: config.v5.json = %q (%v); want the v5 bytes from before startup's persist", got, err)
	}
	if info, err := os.Stat(copyPath(d.cfgSvc)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("T1: config.v5.json mode = %v (%v); want 0600", info.Mode().Perm(), err)
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
	d.configAtStart = asV5(t, d.cfgSvc.Path)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertUnstripped(t, "T2", d.cfgSvc.Path)
	if _, err := os.Stat(copyPath(d.cfgSvc)); !os.IsNotExist(err) {
		t.Fatalf("T2: a copy was written with no strip due (stat err %v)", err)
	}
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

func TestConfigV6Strip_T3_FailedCopyDefersTheStrip(t *testing.T) {
	cfgSvc := seedOrchestratedConfig(t, stripFixture(t))
	v5 := asV5(t, cfgSvc.Path)
	// Something that is not a file holds the copy's name: no copy can be
	// confirmed, so none is written and nothing is stripped.
	blocker := copyPath(cfgSvc)
	if err := os.Mkdir(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	d, orch := buildOrchestratedDaemon(t, cfgSvc, cfgSvc.Snapshot())
	d.configAtStart = v5
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("T3: a failed copy must not fail the start: %v", err)
	}
	assertUnstripped(t, "T3", cfgSvc.Path)
	orch.Shutdown(2*time.Second, nil)

	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	d2, orch2 := buildOrchestratedDaemon(t, cfgSvc, cfgSvc.Snapshot())
	d2.configAtStart = v5
	if err := orch2.Start(d2.workerCtx); err != nil {
		t.Fatalf("T3: second start: %v", err)
	}
	assertStripped(t, "T3 retry", cfgSvc.Path)
	if got, err := os.ReadFile(copyPath(cfgSvc)); err != nil || string(got) != string(v5) {
		t.Fatalf("T3: the retried copy = %q (%v); want the v5 bytes", got, err)
	}
}

func TestConfigV6Strip_T4_FailedStripWriteIsRetriedAndTheCopyKept(t *testing.T) {
	cfgSvc := seedOrchestratedConfig(t, stripFixture(t))
	dir := filepath.Dir(cfgSvc.Path)
	// An existing copy is never overwritten.
	const earlier = `{"version":5,"earlier":"copy"}`
	if err := os.WriteFile(copyPath(cfgSvc), []byte(earlier), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	d, orch := buildOrchestratedDaemon(t, cfgSvc, cfgSvc.Snapshot())
	d.configAtStart = asV5(t, cfgSvc.Path)
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
		t.Fatalf("T4: the existing copy was overwritten: %q", got)
	}
}

func TestConfigV6Strip_T5_CopyRecoveredFromTheUnstrippedFileOrDeferred(t *testing.T) {
	t.Run("recovered", func(t *testing.T) {
		d, orch := newOrchestratedDaemon(t, stripFixture(t))
		d.configAtStart = nil // this start read no v5 file: the on-disk v6 one is the source
		if err := orch.Start(d.workerCtx); err != nil {
			t.Fatalf("start: %v", err)
		}
		assertStripped(t, "T5", d.cfgSvc.Path)
		got, err := os.ReadFile(copyPath(d.cfgSvc))
		if err != nil {
			t.Fatalf("T5: no copy: %v", err)
		}
		v, fwds := diskForwardersFromBytes(t, got)
		if v != 5 || len(fwds) != 3 || fwds[1]["name"] != "smcloud" || !strings.Contains(string(got), "t1-book") {
			t.Fatalf("T5: recovered copy = %s; want the unstripped file as v5", got)
		}
	})
	t.Run("not representable: deferred", func(t *testing.T) {
		fixture := stripFixture(t)
		d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
			fixture(c)
			c.Forwarders[0].Name = "" // a hand-edited entry v5 could not represent
		})
		d.configAtStart = nil
		if err := orch.Start(d.workerCtx); err != nil {
			t.Fatalf("start: %v", err)
		}
		assertUnstripped(t, "T5 deferred", d.cfgSvc.Path)
		if _, err := os.Stat(copyPath(d.cfgSvc)); !os.IsNotExist(err) {
			t.Fatalf("T5: a copy was written from a document v5 cannot represent (stat err %v)", err)
		}
	})
}

func diskForwardersFromBytes(t *testing.T, raw []byte) (int, []map[string]any) {
	t.Helper()
	var doc struct {
		Version    int              `json:"version"`
		Forwarders []map[string]any `json:"forwarders"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Version, doc.Forwarders
}

func TestConfigV6Strip_T6_CopyTakesTheGuardedClubLogScrub(t *testing.T) {
	withClubLog := func(c *config.Config) {
		stripFixture(t)(c)
		c.Forwarders = append(c.Forwarders, types.ForwarderConfig{Name: "clublog", Type: clublog.Type, Enabled: false,
			Credentials: json.RawMessage(`{"api":"T6-LEGACY-APP-KEY","email":"t6@example.test","password":"T6-PASS","callsign":"T6CALL"}`)})
	}
	for _, c := range []struct {
		name, injected string
		wantAPI        bool
	}{{"keyed build", "T6-INJECTED", false}, {"keyless build", "", true}} {
		t.Run(c.name, func(t *testing.T) {
			orig := clublog.InjectedAPIKey
			clublog.InjectedAPIKey = c.injected
			t.Cleanup(func() { clublog.InjectedAPIKey = orig })
			d, orch := newOrchestratedDaemon(t, withClubLog)
			d.configAtStart = asV5(t, d.cfgSvc.Path)
			if err := orch.Start(d.workerCtx); err != nil {
				t.Fatalf("start: %v", err)
			}
			got, err := os.ReadFile(copyPath(d.cfgSvc))
			if err != nil {
				t.Fatalf("no copy: %v", err)
			}
			if has := strings.Contains(string(got), "T6-LEGACY-APP-KEY"); has != c.wantAPI {
				t.Fatalf("T6 %s: copy holds credentials.api = %v, want %v", c.name, has, c.wantAPI)
			}
			for _, keep := range []string{"t6@example.test", "T6-PASS", "T6CALL"} {
				if !strings.Contains(string(got), keep) {
					t.Fatalf("T6 %s: the copy lost the logbook credential %q", c.name, keep)
				}
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

func TestConfigV6Strip_T3b_InterruptedStagingNeverBlocksARetry(t *testing.T) {
	cfgSvc := seedOrchestratedConfig(t, stripFixture(t))
	v5 := asV5(t, cfgSvc.Path)
	// What a crash mid-copy leaves: a staging file under the old fixed name and
	// one under a unique name, both holding credentials.
	leftovers := []string{copyPath(cfgSvc) + ".tmp", copyPath(cfgSvc) + ".123456.tmp"}
	for _, p := range leftovers {
		if err := os.WriteFile(p, []byte(`{"version":5,"token":"T3B-LEFTOVER"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d, orch := buildOrchestratedDaemon(t, cfgSvc, cfgSvc.Snapshot())
	d.configAtStart = v5
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertStripped(t, "T3b", cfgSvc.Path)
	if got, err := os.ReadFile(copyPath(cfgSvc)); err != nil || string(got) != string(v5) {
		t.Fatalf("T3b: the copy = %q (%v); want the v5 bytes", got, err)
	}
	for _, p := range leftovers {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("T3b: the interrupted staging file %s is still there (stat err %v)", filepath.Base(p), err)
		}
	}
}

// T8 (review 2026-10-06): the copy must be a document v5 ACCEPTS, and v5
// requires an ENABLED SM Cloud entry while evidence.sync is on (v6 asks only
// for a complete account, R4). With sync on and SM Cloud disabled no v5 copy
// can be written without changing consent or a binding's state, so the strip
// is deferred — from either source.
func TestConfigV6Strip_T8_EvidenceSyncWithoutEnabledSmcloudDefers(t *testing.T) {
	syncOn := func(c *config.Config) {
		stripFixture(t)(c)
		c.Evidence = types.EvidenceConfig{Capture: true, Sync: true, CapBytes: 524288000}
	}
	for _, src := range []string{"v5 bytes", "re-stamped v6"} {
		t.Run(src, func(t *testing.T) {
			d, orch := newOrchestratedDaemon(t, syncOn)
			if src == "v5 bytes" {
				d.configAtStart = asV5(t, d.cfgSvc.Path)
			}
			if err := orch.Start(d.workerCtx); err != nil {
				t.Fatalf("start: %v", err)
			}
			assertUnstripped(t, "T8 "+src, d.cfgSvc.Path)
			if _, err := os.Stat(copyPath(d.cfgSvc)); !os.IsNotExist(err) {
				t.Fatalf("T8 %s: a copy v5 would refuse was written (stat err %v)", src, err)
			}
		})
	}
}

// T9 (codex P2 on 25692f7a): the v5 check reads SM Cloud's credentials as v5
// does — a tagged struct, whose keys encoding/json matches case-insensitively —
// so an entry a v5 loader accepts is never refused here, and one it rejects is.
func TestConfigV6Strip_T9_V5CheckDecodesCredentialsAsV5Does(t *testing.T) {
	doc := func(creds string, enabled bool) []byte {
		return []byte(`{"evidence":{"sync":true},"forwarders":[{"name":"cloud","type":"smcloud","enabled":` +
			map[bool]string{true: "true", false: "false"}[enabled] + `,"credentials":` + creds + `}]}`)
	}
	if err := checkV5Compatible(doc(`{"URL":"https://t9.example.test","TOKEN":"T9"}`, true)); err != nil {
		t.Fatalf("T9: upper-case keys v5 accepts were refused: %v", err)
	}
	if err := checkV5Compatible(doc(`{"url":"https://t9.example.test"}`, true)); err == nil {
		t.Fatal("T9: an entry with no token, which v5 refuses for evidence sync, was accepted")
	}
	if err := checkV5Compatible(doc(`{"url":"https://t9.example.test","token":"T9"}`, false)); err == nil {
		t.Fatal("T9: a disabled entry, which v5 refuses for evidence sync, was accepted")
	}
}
