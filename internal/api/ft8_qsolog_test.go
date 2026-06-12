package api

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestFt8CompletedQsoLogsToDB is the integration regression for the FT8 e4 logging
// path (the SetQsoLogger sink wired in cmd/smd/main.go): a completed FT8 exchange
// must assemble via ft8.BuildQso, convert through adif.QsoToRecord, and INSERT via
// qsoservice.Submit without tripping a qso-table CHECK constraint.
//
// It exists because the unit test (internal/ft8/qsolog_test.go) froze BuildQso's
// output in isolation and never performed an insert, so a time_on emitted as HHMMSS
// (6 chars) — and a time_off that was never set — silently failed EVERY real FT8 QSO
// write until 2026-06-12, surfacing only on-air from the daemon log. This runs the
// constraints for real. Lives in package api because that is where the in-memory
// sqlite + qsoservice harness lives and api already imports ft8 in production
// (api.New takes *ft8.Service); the e4 sink itself sits in cmd/smd and isn't unit-
// addressable.
func TestFt8CompletedQsoLogsToDB(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "FT8", "7Q5MLV")

	// Mirrors the first completed on-air answer-a-CQ exchange (DL9UW on 10 m,
	// 2026-06-12): we sent R-10, they sent us -15; our offset 2900 Hz on the
	// 28.074 MHz dial. now is the completion instant (09:10:15 UTC).
	c := ft8.CompletedQso{
		TheirCall:      "DL9UW",
		TheirGrid:      "JO41",
		OurReport:      -10,
		HasOurReport:   true,
		TheirReport:    -15,
		HasTheirReport: true,
		OffsetHz:       2900,
		DialFreqMHz:    28.074,
	}
	station := types.LoggingStation{StationCallsign: "7Q5MLV", Operator: "7Q5MLV", MyGridsquare: "KH78"}
	now := time.Date(2026, 6, 12, 9, 10, 15, 0, time.UTC)

	// The exact chain cmd/smd's SetQsoLogger sink runs.
	q := ft8.BuildQso(c, station, lbID, now)
	rec := adif.QsoToRecord(q)
	res, err := srv.qso.Submit(context.Background(), lbID, rec, false)
	if err != nil {
		t.Fatalf("e4 submit failed (the time_on/time_off CHECK-constraint regression): %v", err)
	}
	if res.Status != "stored" || res.ID < 1 {
		t.Fatalf("submit result = %+v, want stored with a positive id", res)
	}

	// Round-trip: both times persist as HHMM (the schema constraint), from now.
	got, err := srv.db.FetchQsoById(res.ID)
	if err != nil {
		t.Fatalf("fetch stored qso: %v", err)
	}
	if got.TimeOn != "0910" || got.TimeOff != "0910" {
		t.Fatalf("stored time_on/time_off = %q/%q, want 0910/0910", got.TimeOn, got.TimeOff)
	}
	if got.Call != "DL9UW" || got.Band != "10m" || got.Mode != "FT8" {
		t.Fatalf("stored qso = call %q / band %q / mode %q, want DL9UW / 10m / FT8",
			got.Call, got.Band, got.Mode)
	}
}
