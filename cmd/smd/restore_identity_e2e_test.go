package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

/*
   W-0021 5F.5 EI3 (outcome 8, AC 6; against the real cloud server on the dev
   Postgres): two managed archives with equal labels, logbook names and
   callsigns each upload one QSO by identity, and a legacy client pushes a
   third, by name, to a cloud logbook of that same name in the tenant's legacy
   archive. With each archive's local QSO row removed and the daemon stopped,
   `smd restore --archive A` restores only A's QSO and `--archive B` only B's,
   each requesting nothing but the support check and its own scoped export.
   `--cloud-logbook Drill` into an unbound Home logbook restores only the
   legacy row, through the whole-tenant export.
*/

func TestRestoreIdentity_EI3_EqualNamedArchivesRestoreApart(t *testing.T) {
	cloud := newRealCloud(t)
	ctx := context.Background()
	creds, _ := json.Marshal(map[string]string{"url": cloud.srv.URL, "token": "tok-e2e"})
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.LoggingStation.StationCallsign = "7Q5MLV"
		c.Forwarders = []types.ForwarderConfig{{Type: smcloud.Type, Enabled: true, Credentials: creds,
			TickIntervalSec: 1, BatchSize: 5, ActionFilter: []string{"insert", "update", "delete"}}}
	})
	ownerOnlySocketDir(t, d)
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

	// The legacy row: an old client's push by name lands in the legacy archive.
	legacy, err := smcloud.New(types.ForwarderConfig{Name: "smcloud.legacy", Type: smcloud.Type,
		Credentials: json.RawMessage(`{"url":"` + cloud.srv.URL + `","token":"tok-e2e","logbook":"Drill"}`)})
	if err != nil {
		t.Fatal(err)
	}
	qL := types.Qso{UUID: utils.NewUUIDv7(), ModifiedAt: time.Now().UTC().Truncate(time.Millisecond), Revision: 1}
	qL.Call, qL.Band, qL.Mode, qL.Freq, qL.QsoDate, qL.TimeOn = "G4ABC", "40m", "CW", "7.020", "20261001", "090000"
	if res := legacy.Submit(ctx, qL, action.Insert, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("legacy push: %+v", res)
	}

	dA, orchA, qA, lbA := drillGeneration(t, d, ids[0])
	stopGen(t, orchA)
	dB, orchB, qB, lbB := drillGeneration(t, dA, ids[1])
	stopGen(t, orchB)

	cfg := dB.cfgSvc.Snapshot()
	cfgPath := filepath.Join(os.Getenv("SM_WORKING_DIR"), "etc", "config.json")
	pathOf := func(id string) string {
		for _, e := range cfg.QsoArchives {
			if e.ID == id {
				return archive.PathFor(cfg, e)
			}
		}
		t.Fatalf("no catalogue entry %s", id)
		return ""
	}
	pathA, pathB := pathOf(ids[0]), pathOf(ids[1])
	removeLocalQso(t, pathA, qA.UUID)
	removeLocalQso(t, pathB, qB.UUID)

	for _, tc := range []struct {
		id, path, logbook string
		want              string
		not               []string
	}{
		{ids[0], pathA, lbA, qA.UUID, []string{qB.UUID, qL.UUID}},
		{ids[1], pathB, lbB, qB.UUID, []string{qA.UUID, qL.UUID}},
	} {
		mark := len(cloud.requests())
		if err := riRestore(t, "--config", cfgPath, "--archive", tc.id); err != nil {
			t.Fatalf("restore --archive %s: %v", tc.id, err)
		}
		wantRequests(t, "EI3 "+tc.id, cloud.requests()[mark:], "GET /v1/version", riScopedPath(tc.id, tc.logbook))
		got := uuidsIn(t, tc.path)
		if !slices.Contains(got, tc.want) {
			t.Fatalf("archive %s after restore holds %q; want its own QSO %s back", tc.id, got, tc.want)
		}
		for _, other := range tc.not {
			if slices.Contains(got, other) {
				t.Fatalf("archive %s received another archive's QSO %s", tc.id, other)
			}
		}
	}

	// The explicit legacy option, into an unbound Home logbook.
	var homeID, homePath string
	for _, e := range cfg.QsoArchives {
		if e.Ownership == types.QsoArchiveOwnershipLegacy {
			homeID, homePath = e.ID, archive.PathFor(cfg, e)
		}
	}
	home := openArchiveFile(t, cfg, homePath)
	target, err := home.InsertLogbookWithContext(ctx, types.Logbook{Name: "Legacy target", Callsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	_ = home.Close()
	mark := len(cloud.requests())
	if err := riRestore(t, "--config", cfgPath, "--archive", homeID, "--logbook", itoa64(target), "--cloud-logbook", "Drill"); err != nil {
		t.Fatalf("legacy restore: %v", err)
	}
	wantRequests(t, "EI3 legacy", cloud.requests()[mark:], "GET /v1/export")
	got := uuidsIn(t, homePath)
	if !slices.Contains(got, qL.UUID) || slices.Contains(got, qA.UUID) || slices.Contains(got, qB.UUID) {
		t.Fatalf("Home after the legacy restore holds %q; want the legacy row %s and neither archive's", got, qL.UUID)
	}
}

// removeLocalQso deletes a QSO and its queue rows from a closed archive file,
// as a lost row would be.
func removeLocalQso(t *testing.T, path, uuid string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, q := range []string{
		`DELETE FROM qso_upload WHERE qso_id = (SELECT id FROM qso WHERE uuid = ?)`,
		`DELETE FROM qso_history WHERE qso_uuid = ?`,
		`DELETE FROM qso WHERE uuid = ?`,
	} {
		if _, err := db.Exec(q, uuid); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func uuidsIn(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for _, q := range riQsos(t, path) {
		out = append(out, q.UUID)
	}
	return out
}

func itoa64(n int64) string { b, _ := json.Marshal(n); return string(b) }
