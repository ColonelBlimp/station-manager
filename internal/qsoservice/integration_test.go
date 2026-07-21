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

// TestSubmit_DerivesBandFromFreq is the submit-side symmetry of
// TestUpdate_FreqEditDerivesBand (2026-07-21 review finding 2): a submit whose BAND
// contradicts FREQ stores the freq-derived band, so an impossible pair (and the
// wrong dedupe key / contradictory forwarded ADIF) can't be created.
func TestSubmit_DerivesBandFromFreq(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		// BAND says 20m but 7.050 is 40m — FREQ is authoritative.
		QsoDetails:     types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "7.050", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation: types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "40m", existing.QsoDetails.Band, "band derived from freq, not the supplied 20m")
	require.Equal(t, "7.050", existing.QsoDetails.Freq)
}

// TestSubmit_RejectsOutOfBandFreq (2026-07-21 review finding 2): a FREQ in no
// recognised band (12.000 sits between 30m and 20m) is a clean invalid_field_value,
// not a stored corrupt pair — IsValidBand and the freq→band table cover the same
// bands, so an unmapped freq is genuinely out-of-band.
func TestSubmit_RejectsOutOfBandFreq(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "12.000", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	_, err := s.Submit(ctx, lbID, rec, false)
	require.Error(t, err)
	var se *SubmitError
	require.ErrorAs(t, err, &se, "out-of-band freq must be a SubmitError (→ 400), not a stored corrupt pair")
	require.Equal(t, "invalid_field_value", se.Code)
}

// TestUpdate_RejectsOutOfBandFreq: the update-side symmetry — a freq edit to an
// out-of-band value is rejected rather than persisting a band that contradicts it.
func TestUpdate_RejectsOutOfBandFreq(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)

	_, err = s.Update(ctx, existing, []byte(`{"freq":"12.000"}`), source.API)
	require.Error(t, err)
	var se *SubmitError
	require.ErrorAs(t, err, &se)
	require.Equal(t, "invalid_field_value", se.Code)
}

// TestSubmit_Accepts60mBelowOldTableFloor guards 2026-07-21 review #2 (the strict
// rejection's follow-up): 5.100 MHz is valid 60m per ADIF (5.06–5.45) but fell below
// the old table's 5.25 floor. With the freq→band table widened to the ADIF ranges it
// derives 60m and stores, rather than being false-rejected as out-of-band.
func TestSubmit_Accepts60mBelowOldTableFloor(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "60m", Mode: "SSB", Freq: "5.100", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err, "5.100 MHz is valid 60m per ADIF and must be accepted")
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "60m", existing.QsoDetails.Band)
	require.Equal(t, "5.100", existing.QsoDetails.Freq)
}

// TestSubmit_MalformedTimeOff guards 2026-07-21 review finding 3: a PRESENT but
// malformed TIME_OFF is a clean invalid_field_value, not silently replaced with
// TIME_ON (which would store a fabricated end time). An ABSENT TIME_OFF still
// defaults to TIME_ON.
func TestSubmit_MalformedTimeOff(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	base := func() adif.Record {
		return adif.Record{
			ContactedStation: types.ContactedStation{Call: "K1ABC"},
			QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
			LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
		}
	}

	// Present but malformed → rejected, not defaulted to TIME_ON.
	bad := base()
	bad.QsoDetails.TimeOff = "99:99"
	_, err := s.Submit(ctx, lbID, bad, false)
	require.Error(t, err)
	var se *SubmitError
	require.ErrorAs(t, err, &se, "malformed TIME_OFF must be a SubmitError (→ 400), not a silent default")
	require.Equal(t, "invalid_field_value", se.Code)

	// Absent → defaults to TIME_ON (behavior preserved).
	res, err := s.Submit(ctx, lbID, base(), false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "1200", existing.QsoDetails.TimeOff, "absent TIME_OFF defaults to TIME_ON")

	// Empty/whitespace TIME_OFF is NOT rejected — after adif.Parse's right-trim it is
	// indistinguishable from an omitted tag, and "no end time given" correctly
	// defaults to TIME_ON (codex ef77a7b8 P2: rejecting empty tags would break valid
	// ADIF). Different minute keeps a distinct dedupe key from the row above.
	ws := base()
	ws.QsoDetails.TimeOn = "1201"
	ws.QsoDetails.TimeOff = "   "
	res, err = s.Submit(ctx, lbID, ws, false)
	require.NoError(t, err, "whitespace-only TIME_OFF must default, not reject")
	existing, err = s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "1201", existing.QsoDetails.TimeOff, "whitespace TIME_OFF defaults to TIME_ON")
}

// TestImportBatch_AbortsOnDedupeLookupFault guards 2026-07-21 review finding 5: a
// non-ErrNotFound fault on the batch dedupe lookup (here induced with a cancelled
// context) is an infra failure, so the import ABORTS with an error rather than
// recording the record as a per-record validation error and continuing — which
// would return a nil service error while silently skipping records.
func TestImportBatch_AbortsOnDedupeLookupFault(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}

	// prepareQso does no DB work, so a valid record reaches the dedupe lookup; the
	// cancelled context then fails that lookup with a ctx error (not ErrNotFound).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var res ImportBatchResult
	contacts := map[string]types.ContactedStation{}
	err := s.importBatch(ctx, lbID, "M0ABC", []adif.Record{rec}, 0, nil, nil, contacts, &res)
	require.Error(t, err, "an infra fault on the dedupe lookup must abort the import")
	require.Empty(t, res.Errors, "the record must not be recorded as a per-record validation error")
	require.Zero(t, res.Stored)
}

// TestSubmit_HHMMSSPreserved: an ADIF body with HHMMSS times stores at full
// second precision (the schema CHECK now accepts HHMM or HHMMSS). Seconds are no
// longer truncated — FT8's real slot seconds and imported HHMMSS survive for
// downstream OQRS/LoTW matching. Dedupe stays minute-precision (see
// TestSubmit_DedupeIgnoresSeconds).
func TestSubmit_HHMMSSPreserved(t *testing.T) {
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
	require.NoError(t, err)

	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)
	require.Equal(t, "084500", existing.QsoDetails.TimeOn, "TIME_ON keeps its seconds")
	require.Equal(t, "085030", existing.QsoDetails.TimeOff, "TIME_OFF keeps its seconds")
}

// TestSubmit_DedupeIgnoresSeconds: two submits of the same contact in the same
// minute — one HHMMSS, one seconds-stripped HHMM (as a QRZ re-import would be) —
// dedupe to ONE QSO, because the dedupe key is minute-precision. The HHMM
// re-import is caught as a duplicate and never overwrites the stored seconds.
func TestSubmit_DedupeIgnoresSeconds(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	mk := func(timeOn string) adif.Record {
		return adif.Record{
			ContactedStation: types.ContactedStation{Call: "K1ABC"},
			QsoDetails: types.QsoDetails{
				Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101",
				TimeOn: timeOn, TimeOff: timeOn,
			},
			LoggingStation: types.LoggingStation{StationCallsign: "M0ABC"},
		}
	}

	first, err := s.Submit(ctx, lbID, mk("084500"), false)
	require.NoError(t, err)
	require.Equal(t, "stored", first.Status)

	// Same minute, seconds stripped (a QRZ re-import) → duplicate, not a new row.
	second, err := s.Submit(ctx, lbID, mk("0845"), false)
	require.NoError(t, err)
	require.Equal(t, "duplicate", second.Status)
	require.Equal(t, first.UUID, second.UUID)

	// The stored QSO still carries its seconds — the re-import didn't clobber it.
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "084500", existing.QsoDetails.TimeOn)

	// A FORCED import with a colliding UUID must classify as uuid_conflict,
	// not a generic insert error (review 2026-07-05: the old `&& !force`
	// guard skipped classification entirely under force). Note force can't
	// collide on the dedupe index at all — force salts the key with a random
	// nonce — so UNIQUE(uuid) via import is the only collision force can hit.
	col := mk("0900")
	col.AppSmQsoID = first.UUID
	_, err = s.SubmitImport(ctx, lbID, col, true, nil)
	require.Error(t, err)
	var serr *SubmitError
	require.ErrorAs(t, err, &serr, "forced uuid collision must be a classified SubmitError")
	require.Equal(t, "uuid_conflict", serr.Code)
}

// TestPreserveSeconds pins the edit-time data-preservation guard: an edit that
// arrives at coarse HHMM for the same minute keeps the stored HHMMSS, so editing
// an unrelated field can't silently drop an FT8 QSO's seconds. A changed minute
// or supplied seconds wins.
func TestPreserveSeconds(t *testing.T) {
	cases := []struct {
		name               string
		incoming, existing string
		want               string
	}{
		{"same minute coarser incoming keeps seconds", "1423", "142347", "142347"},
		{"changed minute wins", "1500", "142347", "1500"},
		{"incoming carries its own seconds wins", "150030", "142347", "150030"},
		{"unchanged hhmmss", "142347", "142347", "142347"},
		{"both hhmm", "1423", "1423", "1423"},
		{"empty incoming falls through", "", "142347", ""},
		{"existing has no seconds", "1423", "1423", "1423"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := preserveSeconds(c.incoming, c.existing); got != c.want {
				t.Errorf("preserveSeconds(%q, %q) = %q, want %q", c.incoming, c.existing, got, c.want)
			}
		})
	}
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

// TestUpdate_EmptyFreqRejectedCleanly guards F1 (review 2026-07-02): a PATCH that
// clears FREQ must be a clean invalid-request, not a 500 from a deep insert
// failure (the required-field check was missing on the Update path).
func TestUpdate_EmptyFreqRejectedCleanly(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.250", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)

	_, err = s.Update(ctx, existing, []byte(`{"freq":""}`), source.API)
	require.Error(t, err)
	var se *SubmitError
	require.ErrorAs(t, err, &se, "empty freq must be a SubmitError (→ 400), not a late storage failure")
	require.Equal(t, "missing_required_field", se.Code)
}

// TestUpdate_ForwardingStampsImmutable guards F2 (review 2026-07-02): a client
// PATCH must not forge the ClubLog upload stamp (the ADR-0039 backfill skip-check
// reads clublog_qso_upload_status) or rewrite the QRZ Logbook LOGID, while a
// legitimate field still edits.
func TestUpdate_ForwardingStampsImmutable(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.250", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)

	patch := []byte(`{"clublog_qso_upload_status":"Y","clublog_qso_upload_date":"20250101","app_qrzlog_logid":"forged-123","comment":"legit edit"}`)
	updated, err := s.Update(ctx, existing, patch, source.API)
	require.NoError(t, err)
	require.Empty(t, updated.ClubLogUploadStatus, "PATCH must not forge the ClubLog upload status")
	require.Empty(t, updated.ClubLogUploadDate, "PATCH must not forge the ClubLog upload date")
	require.Empty(t, updated.QrzlogLogid, "PATCH must not rewrite the QRZ Logbook LOGID")
	require.Equal(t, "legit edit", updated.QsoDetails.Comment, "a legitimate field still edits")
}

// TestSubmit_RejectsMalformedQsoDateOff and its Update twin guard F4 (review
// 2026-07-02): a non-empty-but-malformed QSO_DATE_OFF is rejected rather than
// silently blanked (which would then mis-report an overnight QSO as missing its
// QSO_DATE_OFF).
func TestSubmit_RejectsMalformedQsoDateOff(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.250", QsoDate: "20260101", TimeOn: "1200", QsoDateOff: "not-a-date"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	_, err := s.Submit(ctx, lbID, rec, false)
	require.Error(t, err)
	var se *SubmitError
	require.ErrorAs(t, err, &se)
	require.Equal(t, "invalid_field_value", se.Code)
}

func TestUpdate_RejectsMalformedQsoDateOff(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "K1ABC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.250", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
	}
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByIdWithContext(ctx, res.ID)
	require.NoError(t, err)

	_, err = s.Update(ctx, existing, []byte(`{"qso_date_off":"not-a-date"}`), source.API)
	require.Error(t, err)
	var se *SubmitError
	require.ErrorAs(t, err, &se)
	require.Equal(t, "invalid_field_value", se.Code)
}
