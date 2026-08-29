package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// STATION IDENTITY: a broken database must not be reported as unset config.
// Finding A7 of the 2026-08-01 internal/api logging audit, the highest-ranked
// item across all five audit files.
//
// THE FAULT. currentStationIdentity fails CLOSED on a database error — correct,
// and deliberate: any error other than not-found yields an empty callsign so the
// caller starts no session and keys no PTT. But the error was discarded with no log
// call, and all three FT8 handlers emitted the same 400 no_station_callsign,
// telling the operator to "set your station callsign in My Station".
//
// So a failing datastore was reported as unset configuration. The two demand
// OPPOSITE actions: one sends the operator to a Settings screen to fix a field
// that is already correct, while the real fault goes uninvestigated. A wrong
// instruction is worse than no instruction, and at 7Q8AC the person reading it is
// not the person who built the station.
//
// ACCEPTANCE CRITERION (operator, 2026-08-01):
//
//	When the logbook database cannot be read, starting an FT8 session is refused with
//	"database is not reachable" and a log record naming the cause — and I can
//	tell that apart from my station callsign genuinely not being configured,
//	which is a different refusal carrying a different instruction.
//
// OPERATOR'S RULING on scope, because the finding as filed said "log only, do not
// change the fail-closed behaviour": fail closed means never fall back to another
// callsign and never transmit after a DB error. It does NOT require preserving
// the misleading 400. So the split is:
//
//	logbook missing/empty AND config callsign empty -> 400 no_station_callsign
//	unexpected DB error                             -> 503 db_unavailable + Error log
//	in BOTH cases                                   -> no sequencer start, no PTT keyed,
//	                                                   TX-arm state unchanged
//
// 503 db_unavailable is not new vocabulary: handler_health.go already answers
// exactly this fault with that status and that code, so the two agree rather than
// the operator learning a second name for one condition.
//
// THE TRAP, called out by the operator before the build: writeServerError
// hardcodes 500 (httpkit.go:99). Reaching for it here would report a client-
// visible 500 for a 503 condition, so this path logs explicitly and then writes
// its own status.
//
// THE DB FAULT IS REAL, not mocked: closing the sqlite service makes
// getOpenHandle fail, which is the error shape a genuinely unreadable datastore
// produces. House style is integration tests against a real &sqlite.Service{},
// and an injected error would prove only that the test double works.
//
// A RULE DELIBERATELY NOT WRITTEN, stated so its absence is not mistaken for an
// oversight. The criterion's third clause — neither refusal starts the sequencer
// or keys TX — has no test of its own, because no fixture here could tell the two
// implementations apart: TX is never armed in these tests, so a session could not
// start whatever the handler did, and a test whose fixture makes both paths agree
// proves nothing. What DOES pin it is S1 and S2 asserting the exact response
// code: reaching s.ft8.StartQso would answer with the arm-gate refusal instead,
// so the codes below are evidence the handler returned before the sequencer was
// consulted at all.

// ft8TxRoute is one of the three handlers that resolve the station identity
// before starting a session. All three are covered because the fault is in shared code, and a
// fix applied to one would leave the other two lying — the operator asked for it
// consistently across QSO start, Call CQ and work-a-caller.
type ft8TxRoute struct {
	name, path, body string
	handler          func(*Server) http.HandlerFunc
}

var ft8TxRoutes = []ft8TxRoute{
	{
		"qso start", "/v1/ft8/qso/start",
		`{"their_call":"K1ABC","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`,
		func(s *Server) http.HandlerFunc { return s.handleFt8QsoStart },
	},
	{
		"call cq", "/v1/ft8/cq/start",
		`{"offset_hz":1500,"operating_freq_mhz":14.074}`,
		func(s *Server) http.HandlerFunc { return s.handleFt8CqStart },
	},
	{
		"work caller", "/v1/ft8/qso/work",
		`{"their_call":"K1ABC","their_grid":"FN42","their_snr":-12,"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`,
		func(s *Server) http.HandlerFunc { return s.handleFt8QsoWork },
	},
}

// identityTestServer builds an FT8-enabled server whose log is captured, so the
// rules can assert on the diagnostic record and not merely on the status code.
func identityTestServer(t *testing.T, callsign string) (*Server, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))
	srv.ft8 = ft8.NewService(types.Ft8Config{Enabled: true}, &logging.Service{}, "")
	if callsign != "" {
		if _, err := srv.cfg.Update(func(c *config.Config) error {
			c.LoggingStation.StationCallsign = callsign
			return nil
		}); err != nil {
			t.Fatalf("set callsign: %v", err)
		}
	}
	return srv, buf
}

// breakTheDatabase makes every subsequent read fail the way an unreadable
// datastore does.
func breakTheDatabase(t *testing.T, srv *Server) {
	t.Helper()
	if err := srv.db.Close(); err != nil {
		t.Fatalf("fixture: closing the db must succeed, or the fault is not injected: %v", err)
	}
}

// logRecordsFor returns decoded log records whose message contains sub.
func logRecordsFor(t *testing.T, buf *bytes.Buffer, sub string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		if msg, _ := rec["message"].(string); strings.Contains(msg, sub) {
			out = append(out, rec)
		}
	}
	return out
}

// identityLogMsg is the diagnostic the rules key on. A constant so a wording
// change breaks compilation rather than silently emptying the assertions.
const identityLogMsg = "station identity unreadable"

// S1 — A BROKEN DATABASE IS A 503, ON ALL THREE TX ENTRY POINTS. The status is
// the operator-visible half of the criterion: 400 says "your request was wrong",
// which sends them to Settings, and this request was fine.
//
// The config callsign IS set in this fixture, deliberately. That is what makes it
// discriminating: under the old code the config fallback could not be reached
// either (it fails closed), so both a set and an unset callsign produced the same
// misleading 400 — and a fixture with no callsign would agree with the unset case
// and prove nothing.
func TestStationIdentity_DatabaseFailureRefusesWith503(t *testing.T) {
	for _, route := range ft8TxRoutes {
		t.Run(route.name, func(t *testing.T) {
			srv, _ := identityTestServer(t, "7Q5MLV")
			breakTheDatabase(t, srv)

			w := postFt8Qso(t, srv, route.path, route.body, route.handler(srv))

			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503 — the datastore is unreadable, which is a "+
					"server condition, not a malformed request", w.Code)
			}
			if code := decodeErrCode(t, w); code != "db_unavailable" {
				t.Errorf("code = %q, want db_unavailable — the code handler_health already "+
					"uses for this fault", code)
			}
		})
	}
}

// S2 — GENUINELY UNSET CONFIGURATION IS STILL A 400. The other half of the
// criterion, and the rule that stops S1 being satisfied by answering 503 for
// everything. Same fixture minus the DB fault.
func TestStationIdentity_UnsetCallsignStillRefusesWith400(t *testing.T) {
	for _, route := range ft8TxRoutes {
		t.Run(route.name, func(t *testing.T) {
			srv, _ := identityTestServer(t, "") // no callsign anywhere, DB healthy

			w := postFt8Qso(t, srv, route.path, route.body, route.handler(srv))

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 — nothing is broken, the station is "+
					"unconfigured, and the operator CAN fix it from Settings", w.Code)
			}
			if code := decodeErrCode(t, w); code != "no_station_callsign" {
				t.Errorf("code = %q, want no_station_callsign", code)
			}
		})
	}
}

// S3 — THE CAUSE REACHES THE LOG. The status tells the operator what to do; this
// is what lets anyone work out WHY. The underlying error, the logbook_id being
// read and the operation, per the operator's instruction — without them the
// record says only that something failed, which is where A7 found things.
func TestStationIdentity_DatabaseFailureLogsTheCause(t *testing.T) {
	srv, buf := identityTestServer(t, "7Q5MLV")
	breakTheDatabase(t, srv)
	route := ft8TxRoutes[0]

	postFt8Qso(t, srv, route.path, route.body, route.handler(srv))

	got := logRecordsFor(t, buf, identityLogMsg)
	if len(got) != 1 {
		t.Fatalf("diagnostic records = %d, want exactly 1", len(got))
	}
	rec := got[0]
	if lvl, _ := rec["level"].(string); lvl != "error" {
		t.Errorf("level = %q, want error — an unreadable datastore is a fault, not a "+
			"degradation", lvl)
	}
	if _, ok := rec["logbook_id"]; !ok {
		t.Error("record carries no logbook_id — which logbook could not be read is the " +
			"first thing anyone diagnosing this needs")
	}
	if op, _ := rec["op"].(string); op == "" {
		t.Error("record carries no op — the operation is what locates the failure")
	}
	// The underlying cause, not merely the fact of failure. zerolog puts it under
	// "error"; an empty value means the error was dropped on the way here, which is
	// the very defect this rule exists to close.
	if cause, _ := rec["error"].(string); strings.TrimSpace(cause) == "" {
		t.Error("record carries no underlying error — recording that something failed " +
			"without recording WHAT failed leaves the operator exactly where A7 found them")
	}
}

// S4 — A HEALTHY DATABASE LOGS NOTHING. The diagnostic must fire on the fault and
// only on the fault: a record on every refusal would put an Error in the log for
// an ordinary unconfigured station, and Error is the level that means something
// is broken.
func TestStationIdentity_UnsetCallsignLogsNoDatabaseFault(t *testing.T) {
	srv, buf := identityTestServer(t, "")
	route := ft8TxRoutes[0]

	postFt8Qso(t, srv, route.path, route.body, route.handler(srv))

	if got := logRecordsFor(t, buf, identityLogMsg); len(got) != 0 {
		t.Errorf("diagnostic records = %d on a healthy database, want 0 — an unconfigured "+
			"station is not a fault", len(got))
	}
}
