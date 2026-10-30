package qsoservice

import (
	"context"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/stretchr/testify/require"
)

// W-0023 weekend slice: the contest exchange travels as the standard ADIF
// fields — CONTEST_ID, STX (sent serial), SRX (received serial) and
// SRX_STRING (received district) — from the Phone form's raw ADIF, through a
// live submit, into storage and back out in an ADIF export.
func TestSubmit_ContestExchangeSurvivesStorageAndExport(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "UK/EI DX 2026", "7Q5MLV")
	ctx := context.Background()

	raw := "<CALL:5>G4ABC<QSO_DATE:8>20261031<TIME_ON:6>120130<TIME_OFF:6>120215" +
		"<BAND:3>20m<MODE:3>SSB<FREQ:9>14.200000<RST_SENT:2>59<RST_RCVD:2>59" +
		"<STATION_CALLSIGN:6>7Q5MLV<CONTEST_ID:9>UKEIDXSSB<STX:3>042<SRX:2>36<SRX_STRING:2>OX" +
		"<APP_SM_CONTEST_START:8>20261031<EOR>"
	parsed, err := adif.Parse([]byte(raw))
	require.NoError(t, err)
	require.Len(t, parsed.Records, 1)

	res, err := s.Submit(ctx, lbID, parsed.Records[0], false)
	require.NoError(t, err)
	stored, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	got := [5]string{stored.QsoDetails.ContestId, stored.QsoDetails.STX, stored.QsoDetails.SRX, stored.QsoDetails.SrxString, stored.AppSmContestStart}
	require.Equal(t, [5]string{"UKEIDXSSB", "042", "36", "OX", "20261031"}, got)

	out := adif.ConvertQsoToAdifNoHeader(stored)
	for _, want := range []string{"<contest_id:9>UKEIDXSSB", "<stx:3>042", "<srx:2>36", "<srx_string:2>OX", "<app_sm_contest_start:8>20261031"} {
		require.Contains(t, strings.ToLower(out), strings.ToLower(want))
	}
}
