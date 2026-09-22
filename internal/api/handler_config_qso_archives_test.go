package api

import (
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The QSO archive catalogue (config v4, ADR 0071) is startup-critical and
// server-managed: PUT /v1/config carries no field for it, and a save of any
// writable block must leave it exactly as adoption wrote it — a clobbered
// catalogue is a daemon that opens the wrong file on its next start.
func TestPutConfig_LeavesTheQsoArchiveCatalogueUntouched(t *testing.T) {
	const id = "019fd5c5-efcc-7193-be4f-1fee532ee315"
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.QsoArchives = []types.QsoArchiveConfig{{
			ID: id, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/data/db/station-manager.db",
		}}
		cfg.ActiveQsoArchiveID = id
	})

	w := putConfig(t, srv, `{"station":{"amp_enabled":false}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d; body = %s", w.Code, w.Body.String())
	}
	after := srv.cfg.Snapshot()
	if after.ActiveQsoArchiveID != id || len(after.QsoArchives) != 1 || after.QsoArchives[0].Path != "/data/db/station-manager.db" {
		t.Fatalf("catalogue after PUT = active %q archives %+v; want untouched", after.ActiveQsoArchiveID, after.QsoArchives)
	}
}
