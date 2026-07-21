package qsoservice

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

func ssbRec(station, contact string) adif.Record {
	return adif.Record{
		ContactedStation: types.ContactedStation{Call: contact},
		QsoDetails: types.QsoDetails{
			Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200",
		},
		LoggingStation: types.LoggingStation{StationCallsign: station},
	}
}

// TestSubmit_PinsMyRigToStartupRig guards codex e539a080 P1: a runtime "Set as
// default" moves default_rig_id, but the bridge stays bound to the rig it
// connected to AT STARTUP until a restart. MY_RIG must follow that startup
// (pinned) rig, not the live default — otherwise a QSO still made on the
// connected rig gets stamped with a rig that isn't on the air yet.
func TestSubmit_PinsMyRigToStartupRig(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	// Startup: rig 1 active (the bridge connects to it), with a distinctive MY_RIG
	// override so it can't be confused with rig 2's derived rigdef name.
	override := "FTdx10 (startup rig)"
	s.Config.Cfg.Rigs = []types.RigConfig{
		{ID: 1, Model: "yaesu-ftdx10", MyRig: &override},
		{ID: 2, Model: "yaesu-ftdx10"},
	}
	s.Config.Cfg.DefaultRigID = 1
	s.SetActiveRig(1) // cmd/smd pins attribution to the bridge's startup rig

	// Operator clicks "Set as default" → rig 2. The bridge is NOT reconnected until
	// a restart, so the on-air rig is still rig 1.
	s.Config.Cfg.DefaultRigID = 2

	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)

	qso, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.Equal(t, override, qso.MyRig,
		"MY_RIG must be the pinned startup rig 1, not the runtime default rig 2")
}

// TestSubmit_MyRigUnpinnedFollowsLiveDefault confirms the unpinned path (tests /
// no cmd/smd wiring) still follows the live default rig — the pin is opt-in.
func TestSubmit_MyRigUnpinnedFollowsLiveDefault(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	s.Config.Cfg.Rigs = []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10"}}
	s.Config.Cfg.DefaultRigID = 1
	// No SetActiveRig → unpinned → MY_RIG follows the live default.

	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)

	qso, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.Equal(t, "Yaesu FTdx10", qso.MyRig, "unpinned MY_RIG follows the live default rig")
}
