package main

// Characterization (W-0021 5C, commit a): how `smd restore` chooses its SM Cloud
// credentials and its cloud logbook TODAY, under config v5. --dry-run stops
// after the export is fetched and counted, so each case reads which entry the
// fetch was given and which cloud logbook was counted. These pass against the
// current code and pin what 5C keeps or deliberately changes (ruling R2):
//
//   S1  CHANGES  — with no flags, the config's SM Cloud entry supplies url and
//                  token, DISABLED or not, and its `logbook` credential names the
//                  cloud logbook. 5C: the default comes from Home's
//                  default-logbook SM Cloud binding; still no upload consent needed.
//   S2  CHANGES  — --forwarder matches the config entry's name, case-insensitively;
//                  an unknown name is refused. 5C: it selects a named SM Cloud
//                  binding in Home, a disabled one included.
//   S3  KEPT     — --cloud-logbook overrides the configured logbook.
//   S4  KEPT     — an entry whose logbook is empty restores the cloud's "main".

import (
	"context"
	"encoding/json"
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

// restoreCharTestbed writes a station-shaped config whose SM Cloud entry is
// DISABLED, beside an enabled QRZ entry, and stubs the export fetch. It returns
// the config path and a pointer to the entry the fetch was last given.
func restoreCharTestbed(t *testing.T, cloudLogbook string) (string, *types.ForwarderConfig) {
	t.Helper()
	creds := map[string]string{"url": restoreCharURL, "token": restoreCharToken}
	if cloudLogbook != "" {
		creds["logbook"] = cloudLogbook
	}
	raw, err := json.Marshal(creds)
	if err != nil {
		t.Fatal(err)
	}
	tmp := setupImportTestbed(t, func(c *config.Config) {
		c.Forwarders = []types.ForwarderConfig{
			{Name: "qrz-char", Type: "qrz", Enabled: true,
				Credentials: json.RawMessage(`{"api_key":"QRZ-RESTORE-KEY-43"}`)},
			{Name: "cloud-char", Type: smcloud.Type, Enabled: false, Credentials: raw},
		}
	})
	var got types.ForwarderConfig
	orig := fetchSMCloudExport
	fetchSMCloudExport = func(_ context.Context, fc types.ForwarderConfig) (*smcloud.Export, error) {
		got = fc
		rec := func(lb int64) smcloud.ExportRecord { return smcloud.ExportRecord{LogbookID: lb} }
		return &smcloud.Export{
			Logbooks: []smcloud.ExportLogbook{{ID: 1, Name: "cloud-book-char"}, {ID: 2, Name: "main"}, {ID: 3, Name: "other-book"}},
			Qsos:     []smcloud.ExportRecord{rec(1), rec(1), rec(2), rec(3), rec(3), rec(3)},
		}, nil
	}
	t.Cleanup(func() { fetchSMCloudExport = orig })
	return filepath.Join(tmp, "config.json"), &got
}

func runRestoreDry(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStderr(t, func() { err = runRestore(append(args, "--dry-run")) })
	return out, err
}

func assertRestoreSource(t *testing.T, label string, fc *types.ForwarderConfig) {
	t.Helper()
	var creds map[string]string
	if err := json.Unmarshal(fc.Credentials, &creds); err != nil {
		t.Fatalf("%s: fetched entry credentials: %v", label, err)
	}
	if fc.Name != "cloud-char" || fc.Type != smcloud.Type || creds["url"] != restoreCharURL || creds["token"] != restoreCharToken {
		t.Fatalf("%s: fetch given %q (%s); want the SM Cloud entry's url and token", label, fc.Name, fc.Type)
	}
}

func TestCharacterize_RestoreSelection(t *testing.T) {
	t.Run("S1 default: the disabled SM Cloud entry and its logbook", func(t *testing.T) {
		cfgPath, got := restoreCharTestbed(t, "cloud-book-char")
		out, err := runRestoreDry(t, "--config", cfgPath)
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		assertRestoreSource(t, "S1", got)
		if !strings.Contains(out, `2 record(s) in cloud logbook "cloud-book-char"`) {
			t.Fatalf("S1: output %q; want the configured cloud logbook counted", out)
		}
	})

	t.Run("S2 --forwarder matches the entry name case-insensitively", func(t *testing.T) {
		cfgPath, got := restoreCharTestbed(t, "cloud-book-char")
		if _, err := runRestoreDry(t, "--config", cfgPath, "--forwarder", "CLOUD-CHAR"); err != nil {
			t.Fatalf("restore: %v", err)
		}
		assertRestoreSource(t, "S2", got)
		*got = types.ForwarderConfig{}
		_, err := runRestoreDry(t, "--config", cfgPath, "--forwarder", "qrz-char")
		if err == nil || !strings.Contains(err.Error(), "no smcloud forwarder") {
			t.Fatalf("S2: --forwarder naming a non-SM Cloud entry: err = %v; want refused", err)
		}
		if got.Name != "" {
			t.Fatalf("S2: a refused selection still fetched via %q", got.Name)
		}
	})

	t.Run("S3 --cloud-logbook overrides the configured logbook", func(t *testing.T) {
		cfgPath, got := restoreCharTestbed(t, "cloud-book-char")
		out, err := runRestoreDry(t, "--config", cfgPath, "--cloud-logbook", "other-book")
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		assertRestoreSource(t, "S3", got)
		if !strings.Contains(out, `3 record(s) in cloud logbook "other-book"`) {
			t.Fatalf("S3: output %q; want the override counted", out)
		}
	})

	t.Run("S4 an empty logbook restores the cloud's main", func(t *testing.T) {
		cfgPath, _ := restoreCharTestbed(t, "")
		out, err := runRestoreDry(t, "--config", cfgPath)
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		if !strings.Contains(out, `1 record(s) in cloud logbook "main"`) {
			t.Fatalf("S4: output %q; want the cloud's main counted", out)
		}
	})
}
