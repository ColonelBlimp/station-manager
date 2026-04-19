//go:build manual

// Package qrz live-integration harness. Hits the real QRZ.com Logbook
// API, so it is gated behind the "manual" build tag and only runs when
// QRZ_TEST_API_KEY and QRZ_TEST_CALLSIGN are set in the environment
// (typically via .env loaded by Taskfile). See `task test:qrz-live`.
//
// The harness exercises one full round-trip on the operator's test
// logbook:
//
//  1. Insert a unique QSO (TIME_ON is wall-clock to avoid dedupe
//     collisions on repeat runs).
//  2. Update the same record by QRZ LOGID using ACTION=INSERT +
//     OPTION=REPLACE.
//  3. t.Cleanup issues a raw DELETE directly against QRZ so the
//     test record doesn't accumulate in the logbook. (Once stage 5
//     lands, cleanup can switch to Submit(..., Delete, ...).)
//
// Not part of the standard go-test suite: this talks to a real
// service, costs bandwidth, and requires credentials. Run via:
//
//	task test:qrz-live
package qrz

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// liveCreds pulls the test credentials from the environment and skips
// if either is unset — the build tag gates compilation, this gates
// execution.
func liveCreds(t *testing.T) (apiKey, callsign string) {
	t.Helper()
	apiKey = os.Getenv("QRZ_TEST_API_KEY")
	callsign = os.Getenv("QRZ_TEST_CALLSIGN")
	if apiKey == "" || callsign == "" {
		t.Skip("QRZ_TEST_API_KEY and/or QRZ_TEST_CALLSIGN not set — skipping live QRZ test")
	}
	return apiKey, callsign
}

// liveQso builds a valid QSO targeting the test logbook. TIME_ON is
// built from the current wall-clock so each run has a unique dedupe
// key (CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON) and QRZ won't reject on
// "duplicate QSO".
func liveQso(stationCallsign string) types.Qso {
	now := time.Now().UTC()
	return types.Qso{
		ContactedStation: types.ContactedStation{
			// ARRL HQ station's documented portable-temporary suffix —
			// syntactically valid and stable against upstream callsign
			// validation. Not a real contact; the live harness only
			// exercises the HTTP plumbing, not the QSO semantics.
			Call:    "W1AW/T",
			Country: "United States",
		},
		QsoDetails: types.QsoDetails{
			QsoDate: now.Format("20060102"),
			TimeOn:  now.Format("150405"),
			TimeOff: now.Format("150405"),
			Band:    "20m",
			Mode:    "SSB",
			Freq:    "14.250",
			RstSent: "59",
			RstRcvd: "59",
		},
		LoggingStation: types.LoggingStation{
			StationCallsign: stationCallsign,
		},
	}
}

func TestLive_InsertThenUpdate(t *testing.T) {
	apiKey, stationCallsign := liveCreds(t)

	fwd := newWithEndpoint(apiKey, DefaultEndpoint, &http.Client{
		Timeout: DefaultHTTPTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// --- Phase 1: INSERT ---
	qso := liveQso(stationCallsign)
	ins := fwd.Submit(ctx, qso, action.Insert, "")
	if ins.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("insert outcome = %q, err = %v", ins.Outcome, ins.Err)
	}
	if ins.UpstreamID == "" {
		t.Fatal("insert returned success but no UpstreamID (LOGID)")
	}
	logID := ins.UpstreamID
	t.Logf("live insert OK — LOGID=%s", logID)

	// Cleanup registered immediately so a mid-test failure still
	// removes the record. Exercises the stage-5 delete path via
	// Submit(..., Delete, logID) rather than a raw HTTP call.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(), 30*time.Second,
		)
		defer cleanupCancel()
		del := fwd.Submit(cleanupCtx, types.Qso{}, action.Delete, logID)
		if del.Outcome != forwarding.OutcomeSuccess {
			t.Logf("warning: cleanup delete for LOGID=%s failed: outcome=%s err=%v "+
				"(record may need manual removal)", logID, del.Outcome, del.Err)
		} else {
			t.Logf("cleanup delete OK — LOGID=%s removed", logID)
		}
	})

	// --- Phase 2: UPDATE ---
	// Same QSO but with a different RST — QRZ's OPTION=REPLACE
	// should overwrite the record in place and return RESULT=REPLACE
	// (or RESULT=OK if it decides the record didn't exist yet).
	updated := qso
	updated.QsoDetails.RstSent = "58"
	updated.QsoDetails.RstRcvd = "58"

	upd := fwd.Submit(ctx, updated, action.Update, "")
	if upd.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("update outcome = %q, err = %v", upd.Outcome, upd.Err)
	}
	if upd.UpstreamID == "" {
		t.Fatal("update returned success but no UpstreamID (LOGID)")
	}
	t.Logf("live update OK — LOGID=%s", upd.UpstreamID)
}

// TestLive_InteractiveFlow is the inspection-oriented live harness.
// Unlike TestLive_InsertThenUpdate (quick round-trip, no pauses),
// this test pauses for [Enter] after each operation so the operator
// can open QRZ.com in a browser and verify what was actually written.
//
// Flow:
//
//	INSERT   → prints LOGID → wait for Enter
//	UPDATE   → prints LOGID → wait for Enter
//	DELETE   → prints confirmation → wait for Enter (then exits)
//
// The update reuses the insert's dedupe fields (CALL|BAND|MODE|FREQ|
// QSO_DATE|TIME_ON) so QRZ's OPTION=REPLACE targets the exact record
// we just created; the returned LOGID should match. A Ctrl+C between
// steps leaves the record in the logbook — use TestLive_Delete or
// the QRZ UI to clean up.
func TestLive_InteractiveFlow(t *testing.T) {
	apiKey, stationCallsign := liveCreds(t)

	fwd := newWithEndpoint(apiKey, DefaultEndpoint, &http.Client{
		Timeout: DefaultHTTPTimeout,
	})

	// No overall timeout: the operator paces this manually.
	ctx := context.Background()

	// go test pipes the test binary's stdin from a closed/null source,
	// so bufio.Scanner on os.Stdin returns EOF immediately and the
	// pauses don't pause. Opening /dev/tty reads the controlling
	// terminal directly, which works regardless of what stdin is.
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/tty for interactive prompts: %v "+
			"(interactive test requires a controlling terminal)", err)
	}
	defer func() { _ = tty.Close() }()
	stdin := bufio.NewScanner(tty)

	// --- INSERT ---
	qso := liveQso(stationCallsign)
	qso.QsoDetails.Notes = "station-manager live test — INSERT phase"

	ins := fwd.Submit(ctx, qso, action.Insert, "")
	if ins.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("insert outcome = %q, err = %v", ins.Outcome, ins.Err)
	}
	logID := ins.UpstreamID
	fmt.Printf("\n>>> INSERT OK — LOGID=%s\n"+
		"    QSO: CALL=%s BAND=%s MODE=%s DATE=%s TIME=%s RST=%s/%s\n"+
		"    Inspect on QRZ.com, then press [Enter] to UPDATE... ",
		logID,
		qso.ContactedStation.Call, qso.QsoDetails.Band, qso.QsoDetails.Mode,
		qso.QsoDetails.QsoDate, qso.QsoDetails.TimeOn,
		qso.QsoDetails.RstSent, qso.QsoDetails.RstRcvd,
	)
	_ = os.Stdout.Sync()
	stdin.Scan()

	// --- UPDATE ---
	// Same dedupe fields, visibly-different RST and NOTES so the
	// operator can confirm online that the record's content changed.
	updated := qso
	updated.QsoDetails.RstSent = "58"
	updated.QsoDetails.RstRcvd = "58"
	updated.QsoDetails.Notes = "station-manager live test — UPDATE phase"

	upd := fwd.Submit(ctx, updated, action.Update, "")
	if upd.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("update outcome = %q, err = %v", upd.Outcome, upd.Err)
	}
	fmt.Printf("\n>>> UPDATE OK — LOGID=%s (same as INSERT LOGID=%s: %v)\n"+
		"    Expected changes on QRZ.com: RST=58/58, NOTES='...UPDATE phase'\n"+
		"    Inspect on QRZ.com, then press [Enter] to DELETE... ",
		upd.UpstreamID, logID, upd.UpstreamID == logID,
	)
	_ = os.Stdout.Sync()
	stdin.Scan()

	// --- DELETE (stage 5: via Submit) ---
	del := fwd.Submit(ctx, types.Qso{}, action.Delete, logID)
	if del.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("delete outcome = %q, err = %v", del.Outcome, del.Err)
	}
	fmt.Printf("\n>>> DELETE OK — LOGID=%s removed from logbook\n"+
		"    Verify on QRZ.com that the record is gone.\n"+
		"    Press [Enter] to exit... ",
		logID,
	)
	_ = os.Stdout.Sync()
	stdin.Scan()
}
