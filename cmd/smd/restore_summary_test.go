package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// ADR 0084 slice 2c: an offline restore rebuilds its target archive's summary,
// then closes/checkpoints the database before stamping its signature.
func TestRestore_RebuildsTheTargetArchiveSummaryAfterClose(t *testing.T) {
	qso := types.Qso{UUID: utils.NewUUIDv7()}
	qso.Call = "DL9UW"
	qso.Band = "20m"
	qso.Mode = "SSB"
	qso.Freq = "14.255"
	qso.QsoDate = "20260601"
	qso.TimeOn = "142559"
	rawQso, err := json.Marshal(qso)
	if err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC)
	origFetch := fetchSMCloudExport
	fetchSMCloudExport = func(context.Context, types.ForwarderConfig) (*smcloud.Export, error) {
		return &smcloud.Export{
			Logbooks: []smcloud.ExportLogbook{{ID: 7, Name: "main"}},
			Qsos: []smcloud.ExportRecord{{
				UUID: qso.UUID, LogbookID: 7, ModifiedAt: modified, Qso: rawQso,
			}},
		}, nil
	}
	t.Cleanup(func() { fetchSMCloudExport = origFetch })

	var archivePath string
	tmp := setupImportTestbed(t, func(c *config.Config) {
		archivePath = c.Datastore.Path
		c.QsoArchives = []types.QsoArchiveConfig{{
			ID: archA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: archivePath,
		}}
		c.ActiveQsoArchiveID = archA
		creds, marshalErr := json.Marshal(map[string]string{
			"url": "https://cloud.example.test", "token": "test-token", "logbook": "main",
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		c.Forwarders = []types.ForwarderConfig{{
			Name: "cloud", Type: smcloud.Type, Enabled: true, Credentials: creds,
		}}
	})
	cfg, err := config.Load(filepath.Join(tmp, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	giveIdentity(t, cfg, archivePath, archA)

	if err := runRestore(nil); err != nil {
		t.Fatalf("restore: %v", err)
	}
	sums, miss := archive.ReadSummaries(archive.SummariesPath(cfg))
	if miss != "" {
		t.Fatalf("read summaries: %s", miss)
	}
	got, ok := sums[archA]
	if !ok || len(got.Logbooks) != 1 || got.Logbooks[0].Name != "Test" || got.Logbooks[0].QsoCount != 1 {
		t.Fatalf("restored target summary = %+v (present %v); want Test with 1 QSO", got, ok)
	}
	if _, err := os.Stat(archivePath + "-wal"); err == nil {
		t.Fatal("target archive's WAL survived the restore close")
	}
	now, err := archive.SignatureOf(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Current(now) {
		t.Fatalf("restored target summary was not stamped after close: saved %+v now %+v", got.Signature, now)
	}
}
