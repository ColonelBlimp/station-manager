package main

// How `smd restore` chooses its SM Cloud credentials and its cloud logbook.
// Characterized under config v5 (W-0021 5C, commit a); the cases marked
// CHANGED took their 5C form with config v6 (ruling R2, 2026-10-06), and those
// marked 5F.5 their identity form (rulings K2, K3, 2026-10-13). --dry-run
// stops after the export is fetched and counted, so each case reads which
// account the fetch was given and which cloud logbook was counted. The SM
// Cloud station account always supplies url and token; the binding once
// supplied the cloud logbook name, and the fixture gives the binding a name the
// config entry does not hold, so reading the wrong source fails visibly. The
// fixture's Home is adopted (it carries its identity), and its account names a
// closed loopback port, so a restore that takes the identity path stops at its
// support preflight, unreachable, having fetched nothing.
//
//   S1  5F.5     — with no flags, no binding names the cloud logbook: restore
//                  takes the identity path (v6: Home's default-logbook
//                  binding's name; v5: the config entry's `logbook`).
//   S2  5F.5     — --forwarder is retired, refused before anything is fetched
//                  and pointing to --cloud-logbook (v6: it named a Home SM
//                  Cloud binding).
//   S3  KEPT     — --cloud-logbook overrides — and works with Home's database
//                  unavailable, opening nothing.
//   S4  5F.5     — a binding whose logbook is empty no longer selects the
//                  cloud's "main": restore takes the identity path.
//   S5  5F.5     — with no default binding, restore no longer requires
//                  --cloud-logbook: it takes the identity path (v6: refused,
//                  asking for --cloud-logbook).
//   S6  5F.5     — --forwarder with --cloud-logbook is refused as --forwarder
//                  is (v6: refused because both named the cloud logbook).

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
	restoreCharURL   = "http://127.0.0.1:9" // closed: the identity path stops at its preflight
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
		c.QsoArchives = []types.QsoArchiveConfig{{ID: archA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: homePath}}
		c.ActiveQsoArchiveID = archA
		c.Forwarders = []types.ForwarderConfig{
			{Name: "qrz-char", Type: "qrz", Enabled: true,
				Credentials: json.RawMessage(`{"api_key":"QRZ-RESTORE-KEY-43"}`)},
			{Name: "cloud-char", Type: smcloud.Type, Enabled: false, Credentials: raw},
		}
	})
	cfg, err := config.Load(filepath.Join(tmp, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	giveIdentity(t, cfg, homePath, archA)
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

// wantIdentityPath: restore went to the identity path — stopped at its
// unreachable support preflight — and never fetched the whole-tenant export.
func wantIdentityPath(t *testing.T, label string, err error, fc *types.ForwarderConfig) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "could not be reached") {
		t.Fatalf("%s: err = %v; want the identity path's unreachable preflight", label, err)
	}
	if fc.Type != "" {
		t.Fatalf("%s: the whole-tenant export was fetched", label)
	}
}

func TestCharacterize_RestoreSelection(t *testing.T) {
	t.Run("S1 no flags: the identity path, not the binding's name", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t, bindDefault)
		_, err := runRestoreDry(t, "--config", cfgPath)
		wantIdentityPath(t, "S1", err, got)
		if strings.Contains(err.Error(), "binding-book") {
			t.Fatalf("S1: %v; the binding's cloud logbook name was used", err)
		}
	})

	t.Run("S2 --forwarder is retired", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t, bindDefault, secondLogbk, bindSecondLb)
		_, err := runRestoreDry(t, "--config", cfgPath, "--forwarder", "SMCLOUD.SECOND")
		if err == nil || !strings.Contains(err.Error(), "--cloud-logbook") {
			t.Fatalf("S2: err = %v; want --forwarder refused, pointing to --cloud-logbook", err)
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

	t.Run("S4 an empty binding logbook no longer selects main", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t, bindEmpty)
		_, err := runRestoreDry(t, "--config", cfgPath)
		wantIdentityPath(t, "S4", err, got)
	})

	t.Run("S5 no default binding: the identity path, no override required", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t)
		_, err := runRestoreDry(t, "--config", cfgPath)
		wantIdentityPath(t, "S5", err, got)
	})

	t.Run("S6 --forwarder with --cloud-logbook is refused as --forwarder", func(t *testing.T) {
		cfgPath, _, got := restoreCharTestbed(t, bindDefault)
		_, err := runRestoreDry(t, "--config", cfgPath, "--forwarder", "cloud-char", "--cloud-logbook", "other-book")
		if err == nil || !strings.Contains(err.Error(), "--forwarder") || !strings.Contains(err.Error(), "retired") {
			t.Fatalf("S6: err = %v; want --forwarder refused as retired", err)
		}
		if got.Type != "" {
			t.Fatal("S6: a refused selection still fetched")
		}
	})
}
