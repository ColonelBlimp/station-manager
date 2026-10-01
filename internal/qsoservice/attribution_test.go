package qsoservice

// Recovered-submission attribution (ADR 0085, operator ruling 2026-10-01). A
// Phone / CW draft saved across an archive switch is restored later; its
// original attribution — MY_RIG, the effective OPERATOR and MY_NAME — must be
// the attribution it is stored with, or the submit is refused.
//
//   A1  LiveAttribution reports exactly what a live submit stamps: the pinned
//       startup rig's MY_RIG (its per-rig override included), the supplied
//       OPERATOR or else default_operator, and MY_NAME from the roster.
//   A2  An expectation equal to the stamped values stores the QSO.
//   A3  An expectation differing in ANY field is refused with
//       attribution_changed, writing nothing (no QSO, no upload-queue row).
//   A4  The check is made on the values the submit is about to store, so a
//       change between reading the expectation and submitting is caught.
//   A6  The comparison is EXACT on the stored values: an override changed only
//       by whitespace is a different attribution and is refused (review
//       2026-10-01: trimming both sides let the changed value through).
//   A5  Known-empty values are values: an empty MY_RIG (a suppressing override)
//       or an empty OPERATOR / MY_NAME must match empty, and does.

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

func TestLiveAttribution_MatchesWhatSubmitStores(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	withRoster(s)
	ctx := context.Background()

	for _, supplied := range []string{"", "G0XYZ"} {
		got := s.LiveAttribution(supplied)
		rec := ssbRec("M0ABC", "K1A"+supplied)
		rec.LoggingStation.Operator = supplied
		res, err := s.Submit(ctx, lbID, rec, false)
		require.NoError(t, err)
		stored, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
		require.NoError(t, err)
		require.Equal(t, types.SubmitAttribution{
			MyRig:    stored.LoggingStation.MyRig,
			Operator: stored.LoggingStation.Operator,
			MyName:   stored.LoggingStation.MyName,
		}, got, "supplied operator %q", supplied)
	}
	require.Equal(t, types.SubmitAttribution{MyRig: "FTdx10 (home)", Operator: "M0ABC", MyName: "Marc"},
		s.LiveAttribution(""))

	// A runtime "Set as default" moves default_rig_id but not the connected
	// rig: the attribution still follows the pinned startup rig, as Submit does.
	s.Config.Cfg.DefaultRigID = 2
	got := s.LiveAttribution("")
	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K9PIN"), false)
	require.NoError(t, err)
	stored, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.Equal(t, "FTdx10 (home)", got.MyRig)
	require.Equal(t, stored.LoggingStation.MyRig, got.MyRig)
}

func TestSubmitExpecting_MatchingAttributionStores(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	withRoster(s)
	ctx := context.Background()

	expect := s.LiveAttribution("M0ABC")
	rec := ssbRec("M0ABC", "K1ABC")
	rec.LoggingStation.Operator = "M0ABC"
	res, err := s.SubmitExpecting(ctx, lbID, rec, false, expect)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
}

func TestSubmitExpecting_AnyMismatchRefusesWithoutWriting(t *testing.T) {
	s := newTestService(t,
		types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert"}},
	)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	withRoster(s)
	ctx := context.Background()

	good := s.LiveAttribution("M0ABC")
	cases := map[string]types.SubmitAttribution{
		"my_rig":   {MyRig: "Yaesu FTdx10", Operator: good.Operator, MyName: good.MyName},
		"operator": {MyRig: good.MyRig, Operator: "G0XYZ", MyName: good.MyName},
		"my_name":  {MyRig: good.MyRig, Operator: good.Operator, MyName: "Someone else"},
	}
	for field, expect := range cases {
		rec := ssbRec("M0ABC", "K1ABC")
		rec.LoggingStation.Operator = "M0ABC"
		_, err := s.SubmitExpecting(ctx, lbID, rec, false, expect)
		se := IsSubmitError(err)
		require.NotNil(t, se, field)
		require.Equal(t, "attribution_changed", se.Code, field)
	}
	n, err := s.DB.FetchQsoCountByLogbookIdWithContext(ctx, lbID, "", false)
	require.NoError(t, err)
	require.Zero(t, n, "a refused submit stores no QSO")
	pending, err := s.DB.ClaimPendingUploadsWithContext(ctx, "qrz", 100)
	require.NoError(t, err)
	require.Empty(t, pending, "a refused submit queues no upload")
}

func TestSubmitExpecting_ChangeAfterReadIsRefused(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	withRoster(s)
	ctx := context.Background()

	cases := map[string]func(){
		"roster name": func() { s.Config.Cfg.Operators[0].Name = "Renamed" },
		"my_rig override": func() {
			other := "FTdx10 (field)"
			s.Config.Cfg.Rigs[0].MyRig = &other
		},
		"default operator": func() { s.Config.Cfg.DefaultOperator = "G0XYZ" },
	}
	for what, change := range cases {
		withRoster(s)
		expect := s.LiveAttribution("") // read …
		change()                        // … then the station changes …
		_, err := s.SubmitExpecting(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false, expect)
		se := IsSubmitError(err) // … and the submit sees it
		require.NotNil(t, se, what)
		require.Equal(t, "attribution_changed", se.Code, what)
	}
	n, err := s.DB.FetchQsoCountByLogbookIdWithContext(ctx, lbID, "", false)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestSubmitExpecting_KnownEmptyValuesMatch(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()
	suppress := ""
	s.Config.Cfg.Rigs = []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10", MyRig: &suppress}}
	s.Config.Cfg.DefaultRigID = 1
	s.SetActiveRig(1)
	// No roster, no default operator: OPERATOR and MY_NAME stay empty.

	expect := s.LiveAttribution("")
	require.Equal(t, types.SubmitAttribution{}, expect)
	res, err := s.SubmitExpecting(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false, expect)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
}

func TestSubmitExpecting_ComparesStoredValuesExactly(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	withRoster(s)
	ctx := context.Background()

	expect := s.LiveAttribution("")
	padded := " FTdx10 (home)" // the same name, but not the same stored value
	s.Config.Cfg.Rigs[0].MyRig = &padded
	_, err := s.SubmitExpecting(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false, expect)
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "attribution_changed", se.Code)
	n, err := s.DB.FetchQsoCountByLogbookIdWithContext(ctx, lbID, "", false)
	require.NoError(t, err)
	require.Zero(t, n)

	// And what LiveAttribution reports for it is exactly what is stored.
	again := s.LiveAttribution("")
	require.Equal(t, padded, again.MyRig)
	res, err := s.SubmitExpecting(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false, again)
	require.NoError(t, err)
	stored, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.Equal(t, padded, stored.LoggingStation.MyRig)
}
