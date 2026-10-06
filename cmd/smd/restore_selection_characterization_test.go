package main

// How `smd restore` chooses its SM Cloud credentials and its cloud logbook.
// Characterized under config v5 (W-0021 5C, commit a); the cases marked
// CHANGED took their 5C form with config v6 (ruling R2, 2026-10-06). --dry-run
// stops after the export is fetched and counted, so each case reads which
// account the fetch was given and which cloud logbook was counted. The SM
// Cloud station account always supplies url and token; the binding supplies
// the cloud logbook name, and the fixture gives the binding a name the config
// entry does not hold, so reading the wrong source fails visibly.
//
//   S1  CHANGED  — with no flags, the cloud logbook comes from Home's
//                  default-logbook SM Cloud binding (v5: the config entry's
//                  `logbook`). A disabled binding serves: restoring needs no
//                  upload consent.
//   S2  CHANGED  — --forwarder names an SM Cloud BINDING in Home,
//                  case-insensitively, a disabled one included (v5: the config
//                  entry's name). A name no SM Cloud binding carries is refused
//                  and nothing is fetched.
//   S3  KEPT     — --cloud-logbook overrides — and works with Home's database
//                  unavailable, opening nothing.
//   S4  KEPT     — a binding whose logbook is empty restores the cloud's "main".
//   S5  NEW      — when the default binding cannot be resolved, restore
//                  requires --cloud-logbook rather than guessing; nothing fetched.
//   S6  NEW      — --forwarder and --cloud-logbook together are refused: both
//                  name the cloud logbook.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const (
	restoreCharURL   = "https://smc-restore-char.example.test"
	restoreCharToken = "SMC-RESTORE-TOKEN-41"
)

// restoreCharTestbed writes a station-shaped config — an SM Cloud account still
// carrying a legacy `logbook` the binding does not share, beside a QRZ entry —
// and stubs the export fetch. bindings are inserted into Home's file (the
// testbed's datastore, no catalogue). It returns the config path, the Home
// path and a pointer to the account the fetch was last given.
func restoreCharTestbed(t *testing.T, bindings ...string) (string, string, *types.ForwarderConfig) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"url": restoreCharURL, "token": restoreCharToken, "logbook": "config-legacy-book"})
	if err != nil {
		t.Fatal(err)
	}
	var homePath string
	tmp := setupImportTestbed(t, func(c *config.Config) {
		homePath = c.Datastore.Path
		c.Forwarders = []types.ForwarderConfig{
			{Name: "qrz-char", Type: "qrz", Enabled: true,
				Credentials: json.RawMessage(`{"api_key":"QRZ-RESTORE-KEY-43"}`)},
			{Name: "cloud-char", Type: smcloud.Type, Enabled: false, Credentials: raw},
		}
	})
	if len(bindings) > 0 {
		db, err := sql.Open("sqlite", "file:"+homePath)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range bindings {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("insert binding: %v", err)
			}
		}
		_ = db.Close()
	}
	var got types.ForwarderConfig
	orig := fetchSMCloudExport
	fetchSMCloudExport = func(_ context.Context, fc types.ForwarderConfig) (*smcloud.Export, error) {
		got = fc
		rec := func(lb int64) smcloud.ExportRecord { return smcloud.ExportRecord{LogbookID: lb} }
		return &smcloud.Export{
			Logbooks: []smcloud.ExportLogbook{{ID: 1, Name: "binding-book"}, {ID: 2, Name: "main"}, {ID: 3, Name: "other-book"}, {ID: 4, Name: "config-legacy-book"}},
			Qsos:     []smcloud.ExportRecord{rec(1), rec(1), rec(2), rec(3), rec(3), rec(3), rec(4), rec(4), rec(4), rec(4)},
		}, nil
	}
	t.Cleanup(func() { fetchSMCloudExport = orig })
	return filepath.Join(tmp, "config.json"), homePath, &got
}

const (
	bindDefault  = `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (1, 'smcloud', 'cloud-char', 0, '{"logbook":"binding-book"}')`
	bindEmpty    = `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (1, 'smcloud', 'cloud-char', 0, '{}')`
	secondLogbk  = `INSERT INTO logbook (name, callsign) VALUES ('Second', 'M0TEST')`
	bindSecondLb = `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (2, 'smcloud', 'smcloud.second', 0, '{"logbook":"other-book"}')`
)

func runRestoreDry(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStderr(t, func() { err = runRestore(append(args, "--dry-run")) })
	return out, err
}

// assertRestoreSource: the fetch was given the SM Cloud station account's url
// and token.
func assertRestoreSource(t *testing.T, label string, fc *types.ForwarderConfig) {
	t.Helper()
	var creds map[string]string
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		t.Fatalf("%s: fetched account credentials: %v", label, err)
	}
	if fc.Type != smcloud.Type || creds["url"] != restoreCharURL || creds["token"] != restoreCharToken {
		t.Fatalf("%s: fetch given %q (%s); want the SM Cloud account's url and token", label, fc.Name, fc.Type)
	}
}

func TestCharacterize_RestoreSelection(t *testing.T) {
	t.Run("S1 default: Home's default-logbook SM Cloud binding, disabled", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t, bindDefault)
		out, err := runRestoreDry(t, "--config", cfgPath)
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		assertRestoreSource(t, "S1", got)
		if !strings.Contains(out, `2 record(s) in cloud logbook "binding-book"`) {
			t.Fatalf("S1: output %q; want the binding's cloud logbook counted, not the config entry's", out)
		}
	})

	t.Run("S2 --forwarder names a Home SM Cloud binding case-insensitively", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t, bindDefault, secondLogbk, bindSecondLb)
		out, err := runRestoreDry(t, "--config", cfgPath, "--forwarder", "SMCLOUD.SECOND")
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		assertRestoreSource(t, "S2", got)
		if !strings.Contains(out, `3 record(s) in cloud logbook "other-book"`) {
			t.Fatalf("S2: output %q; want the named binding's cloud logbook counted", out)
		}
		*got = types.ForwarderConfig{}
		_, err = runRestoreDry(t, "--config", cfgPath, "--forwarder", "qrz-char")
		if err == nil || !strings.Contains(err.Error(), "qrz-char") {
			t.Fatalf("S2: --forwarder naming no SM Cloud binding: err = %v; want refused by name", err)
		}
		if got.Type != "" {
			t.Fatalf("S2: a refused selection still fetched")
		}
	})

	t.Run("S3 --cloud-logbook overrides, even with Home's database unavailable", func(t *testing.T) {
		cfgPath, homePath, got := restoreCharTestbed(t, bindDefault)
		if err := os.Rename(homePath, homePath+".away"); err != nil {
			t.Fatal(err)
		}
		out, err := runRestoreDry(t, "--config", cfgPath, "--cloud-logbook", "other-book")
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		assertRestoreSource(t, "S3", got)
		if !strings.Contains(out, `3 record(s) in cloud logbook "other-book"`) {
			t.Fatalf("S3: output %q; want the override counted", out)
		}
		if _, err := os.Stat(homePath); !os.IsNotExist(err) {
			t.Fatalf("S3: the override opened (and created) Home's database file (stat err %v)", err)
		}
	})

	t.Run("S4 an empty binding logbook restores the cloud's main", func(t *testing.T) {
		cfgPath, _, _ := restoreCharTestbed(t, bindEmpty)
		out, err := runRestoreDry(t, "--config", cfgPath)
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		if !strings.Contains(out, `1 record(s) in cloud logbook "main"`) {
			t.Fatalf("S4: output %q; want the cloud's main counted", out)
		}
	})

	t.Run("S5 no default binding: the override is required", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t)
		_, err := runRestoreDry(t, "--config", cfgPath)
		if err == nil || !strings.Contains(err.Error(), "--cloud-logbook") {
			t.Fatalf("S5: err = %v; want restore to require --cloud-logbook", err)
		}
		if got.Type != "" {
			t.Fatal("S5: an unresolved default still fetched")
		}
	})

	t.Run("S6 --forwarder with --cloud-logbook is refused", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t, bindDefault)
		_, err := runRestoreDry(t, "--config", cfgPath, "--forwarder", "cloud-char", "--cloud-logbook", "other-book")
		if err == nil || !strings.Contains(err.Error(), "--forwarder") {
			t.Fatalf("S6: err = %v; want the pair refused", err)
		}
		if got.Type != "" {
			t.Fatal("S6: a refused selection still fetched")
		}
	})
}
