package qsoservice

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// newTestService wires a qsoservice.Service to an in-memory SQLite DB + real
// config/logging/hub — the package-level integration harness the 2026-06-19
// review flagged as missing (the other package tests cover only pure helpers).
func newTestService(t *testing.T, forwarders ...types.ForwarderConfig) *Service {
	t.Helper()
	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"
	cfg.Forwarders = forwarders
	cfgSvc := config.New(cfg)
	require.NoError(t, cfgSvc.Initialize())

	logSvc := &logging.Service{}
	logSvc.ConfigService = cfgSvc
	logSvc.WorkingDir = cfgSvc.WorkingDir()
	require.NoError(t, logSvc.Initialize())

	dbSvc := &sqlite.Service{}
	dbSvc.ConfigService = cfgSvc
	dbSvc.LoggerService = logSvc
	require.NoError(t, dbSvc.Initialize())
	dbSvc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      ":memory:",
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	require.NoError(t, dbSvc.Open())
	require.NoError(t, dbSvc.Migrate())

	hub := events.NewHub()
	t.Cleanup(func() {
		hub.Close()
		_ = dbSvc.Close()
		_ = logSvc.Close()
	})

	return &Service{DB: dbSvc, Logger: logSvc, Config: cfgSvc, Hub: hub}
}

func seedLogbook(t *testing.T, s *Service, name, callsign string) int64 {
	t.Helper()
	id, err := s.DB.InsertLogbook(types.Logbook{Name: name, Callsign: callsign})
	require.NoError(t, err)
	return id
}

// TestInitialize_RequiresDependencies guards review 2026-06-19 L1: Initialize
// fails fast on a missing dependency instead of nil-deref'ing on the first QSO
// action.
func TestInitialize_RequiresDependencies(t *testing.T) {
	require.Error(t, (&Service{}).Initialize(), "no deps")
	require.Error(t, (&Service{DB: &sqlite.Service{}, Logger: &logging.Service{}, Config: &config.Service{}}).Initialize(), "missing Hub")

	full := &Service{DB: &sqlite.Service{}, Logger: &logging.Service{}, Config: &config.Service{}, Hub: events.NewHub()}
	require.NoError(t, full.Initialize(), "all deps present")
}

// TestSubmit_EnforcesLogbookCallsign guards review 2026-06-19 M3: the
// logbook-exists + callsign-match invariant lives in the shared service path, so
// FT8/direct callers can't bypass it. Live Submit enforces the callsign match;
// SubmitImport relaxes it (historical/mixed logs) but still requires the logbook.
func TestSubmit_EnforcesLogbookCallsign(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()
	rec := func(station, contact string) adif.Record {
		return adif.Record{
			ContactedStation: types.ContactedStation{Call: contact},
			QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
			LoggingStation:   types.LoggingStation{StationCallsign: station},
		}
	}

	res, err := s.Submit(ctx, lbID, rec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)

	_, err = s.Submit(ctx, lbID, rec("G0XYZ", "K2ABC"), false)
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "callsign_mismatch", se.Code)

	_, err = s.Submit(ctx, 9999, rec("M0ABC", "K3ABC"), false)
	se = IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "logbook_not_found", se.Code)

	res, err = s.SubmitImport(ctx, lbID, rec("G0XYZ", "K4ABC"), false, nil)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status, "import relaxes the callsign match")

	_, err = s.SubmitImport(ctx, 9999, rec("M0ABC", "K5ABC"), false, nil)
	se = IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "logbook_not_found", se.Code, "import still requires the logbook to exist")
}

// TestSubmitImport_ForwardSelection guards the dogfood-2026-06-23 fix at the
// service boundary: a public Submit enqueues every configured forwarder, but
// SubmitImport enqueues NOTHING unless the forwarder is named in forwardTo
// (retrospective backfill is operator-driven, never automatic — ADR 0022).
func TestSubmitImport_ForwardSelection(t *testing.T) {
	// Two unregistered-type forwarders; an unregistered type isn't action-gated
	// by validateForwarders, so a bare insert filter exercises the enqueue loop.
	s := newTestService(t,
		types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert"}},
		types.ForwarderConfig{Name: "clublog", Type: "clublog", Enabled: true, ActionFilter: []string{"insert"}},
	)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()
	rec := func(contact string) adif.Record {
		return adif.Record{
			ContactedStation: types.ContactedStation{Call: contact},
			QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
			LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
		}
	}
	countByForwarder := func(qsoID int64) map[string]int {
		rows, ferr := s.DB.FetchUploadsByQsoIDWithContext(ctx, qsoID)
		require.NoError(t, ferr)
		m := map[string]int{}
		for _, r := range rows {
			m[r.ForwarderName]++
		}
		return m
	}

	// Public submit → both forwarders enqueued.
	live, err := s.Submit(ctx, lbID, rec("K1ABC"), false)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"qrz": 1, "clublog": 1}, countByForwarder(live.ID),
		"public Submit enqueues all configured forwarders")

	// Import, default (no forwardTo) → nothing enqueued.
	imp, err := s.SubmitImport(ctx, lbID, rec("K2ABC"), false, nil)
	require.NoError(t, err)
	require.Empty(t, countByForwarder(imp.ID), "default import enqueues nothing")

	// Import with --forward qrz (case-insensitive) → only qrz enqueued.
	sel, err := s.SubmitImport(ctx, lbID, rec("K3ABC"), false, []string{"QRZ"})
	require.NoError(t, err)
	require.Equal(t, map[string]int{"qrz": 1}, countByForwarder(sel.ID),
		"import enqueues only the named forwarder, case-insensitively")
}

// TestUpdate_FT8EmptyReportEditable guards review 2026-06-19 M1: an FT8 QSO
// logged with an empty (bare-roger) report can still be edited — a no-op edit
// must not fail on the empty report the way phone/CW validation would.
func TestUpdate_FT8EmptyReportEditable(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "FT8", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err)

	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)
	require.Empty(t, existing.QsoDetails.RstRcvd, "FT8 submit leaves rst_rcvd empty")

	updated, err := s.Update(ctx, existing, []byte(`{}`), source.API)
	require.NoError(t, err, "a no-op edit of a bare-roger FT8 QSO must succeed")
	require.Empty(t, updated.QsoDetails.RstRcvd, "empty FT8 report preserved across the edit")
}

// TestUpdate_FreqEditDerivesBand guards review 2026-06-19 M2: a cross-band
// frequency edit derives BAND from the new FREQ, so a stale band in the patch
// (the overlay sends the old band on a VFO freq change) can't persist an
// impossible BAND/FREQ pair.
func TestUpdate_FreqEditDerivesBand(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "40m", Mode: "SSB", Freq: "7.050", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)

	// Move to 14.250 (20m) but send the stale band "40m" — the derived band wins.
	updated, err := s.Update(ctx, existing, []byte(`{"freq":"14.250","band":"40m"}`), source.API)
	require.NoError(t, err)
	require.Equal(t, "20m", updated.QsoDetails.Band, "band derived from freq, not the stale patch band")
	require.Equal(t, "14.250", updated.QsoDetails.Freq)
}

// TestSubmit_HHMMSSStoredAsHHMM guards review 2026-06-19 M2: an ADIF body with
// HHMMSS times validates and is then narrowed to HHMM for storage (the schema
// requires length 4), so it stores cleanly instead of failing at insert as a
// generic 500.
func TestSubmit_HHMMSSStoredAsHHMM(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails: types.QsoDetails{
			Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101",
			TimeOn: "084500", TimeOff: "085030",
		},
		LoggingStation: types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err, "HHMMSS must not fail at insert")

	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "0845", existing.QsoDetails.TimeOn, "TIME_ON narrowed to HHMM")
	require.Equal(t, "0850", existing.QsoDetails.TimeOff, "TIME_OFF narrowed to HHMM")
}

// TestSubmit_AcceptsVHFBand guards review 2026-06-19 (frontend M2): a 2m QSO —
// the band the SPA + utils.FrequencyToBand map for a 144 MHz dial — is now an
// accepted band, so it stores instead of being rejected as invalid_field_value.
func TestSubmit_AcceptsVHFBand(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "2m", Mode: "SSB", Freq: "144.174", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err, "a 2m QSO must be accepted")

	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "2m", existing.QsoDetails.Band)
	require.Equal(t, "144.174", existing.QsoDetails.Freq)
}

// TestSubmit_RejectsNonPositiveFreq guards review 2026-06-19 M3: FREQ=0 is a
// clean invalid_field_value at the service boundary, not a late insert failure.
func TestSubmit_RejectsNonPositiveFreq(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "0", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	_, err := s.Submit(ctx, lbID, rec, false)
	require.Error(t, err)
	var se *SubmitError
	require.ErrorAs(t, err, &se, "must be a SubmitError (→ 400), not a late storage failure")
	require.Equal(t, "invalid_field_value", se.Code)
}
