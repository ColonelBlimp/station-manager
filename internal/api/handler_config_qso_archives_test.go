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

// The identities of an archive activation (config v8; ADR 0100 §4, W-0029 part
// 3, A1) are server-managed too. The archive served is compared with the
// activation in effect to know that it has been arrived at, so a settings save
// that dropped or changed either would end a sitting, or hide a switch. A save
// of any writable block leaves both as they are, and a body that names them
// changes neither.
func TestPutConfig_LeavesTheActivationIdentitiesUntouched(t *testing.T) {
	const (
		home     = "019fd5c5-efcc-7193-be4f-1fee532ee315"
		contest  = "019fd5c5-efcc-7193-be4f-1fee532ee316"
		inEffect = "0190cccc-0000-7000-8000-000000000001"
		pending  = "0190cccc-0000-7000-8000-000000000002"
	)
	for name, tc := range map[string]struct {
		body     string
		mustSave bool // the save must succeed and change the setting it names
	}{
		"a save of a writable block": {`{"station":{"amp_enabled":false}}`, true},
		// Accepted or refused, as the handler decides: either way neither identity may change.
		"a body that names the two identities": {`{"station":{"amp_enabled":false},"qso_archive_activation_id":"x","pending_qso_archive_activation_id":"y"}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			srv := testServerWithCfg(t, func(cfg *config.Config) {
				cfg.QsoArchives = []types.QsoArchiveConfig{
					{ID: home, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/data/db/station-manager.db"},
					{ID: contest, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged},
				}
				cfg.ActiveQsoArchiveID = home
				cfg.PendingQsoArchiveID = contest
				cfg.PendingQsoArchiveActivationID = pending
				cfg.QsoArchiveActivationID = inEffect
				cfg.Station.AmpEnabled = true
			})

			w := putConfig(t, srv, tc.body)

			after := srv.cfg.Snapshot()
			if tc.mustSave {
				// A save that was refused outright would leave the identities alone
				// and prove nothing: this one must have been made.
				if w.Code != http.StatusOK {
					t.Fatalf("PUT status = %d; body = %s", w.Code, w.Body.String())
				}
				if after.Station.AmpEnabled {
					t.Fatal("the save answered 200 and did not change the setting it named")
				}
			}
			if after.QsoArchiveActivationID != inEffect || after.PendingQsoArchiveActivationID != pending || after.PendingQsoArchiveID != contest {
				t.Fatalf("after the save: in effect %q, pending identity %q, selector %q; want %q, %q, %q untouched",
					after.QsoArchiveActivationID, after.PendingQsoArchiveActivationID, after.PendingQsoArchiveID, inEffect, pending, contest)
			}
		})
	}
}
