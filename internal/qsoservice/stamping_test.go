package qsoservice

// Ordinary attribution stamping on a live submit (config.md §10; ADR 0087 keeps
// it unchanged when the recovered-submit check goes):
//
//   S1  MY_RIG is the pinned startup rig's per-rig override, even after a
//       runtime "Set as default" moves default_rig_id.
//   S2  OPERATOR is the supplied value, else default_operator.
//   S3  MY_NAME is the roster name of the effective OPERATOR.

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

func withRoster(s *Service) {
	override := "FTdx10 (home)"
	s.Config.Cfg.Rigs = []types.RigConfig{
		{ID: 1, Model: "yaesu-ftdx10", MyRig: &override},
		{ID: 2, Model: "yaesu-ftdx10"},
	}
	s.Config.Cfg.DefaultRigID = 1
	s.SetActiveRig(1)
	s.Config.Cfg.Operators = []types.Operator{
		{Callsign: "M0ABC", Name: "Marc"},
		{Callsign: "G0XYZ", Name: "Guest"},
	}
	s.Config.Cfg.DefaultOperator = "M0ABC"
}

func TestSubmit_StampsAttribution(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	withRoster(s)
	ctx := context.Background()

	stamped := func(rec types.Qso) [3]string {
		return [3]string{rec.LoggingStation.MyRig, rec.LoggingStation.Operator, rec.LoggingStation.MyName}
	}
	submit := func(contact, operator string) types.Qso {
		rec := ssbRec("M0ABC", contact)
		rec.LoggingStation.Operator = operator
		res, err := s.Submit(ctx, lbID, rec, false)
		require.NoError(t, err)
		stored, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
		require.NoError(t, err)
		return stored
	}

	require.Equal(t, [3]string{"FTdx10 (home)", "M0ABC", "Marc"}, stamped(submit("K1A", "")), "S2 default operator")
	require.Equal(t, [3]string{"FTdx10 (home)", "G0XYZ", "Guest"}, stamped(submit("K1B", "G0XYZ")), "S2/S3 supplied operator")

	// S1: a runtime "Set as default" moves default_rig_id, not the connected rig.
	s.Config.Cfg.DefaultRigID = 2
	require.Equal(t, "FTdx10 (home)", submit("K9PIN", "").LoggingStation.MyRig, "S1 pinned rig")
}
