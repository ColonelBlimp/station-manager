package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// GET /v1/ft8/state (W-0027 A3, section I): what the desktop app reads when a
// close is asked for. These go through the whole handler chain, as the app does.

func getFt8State(t *testing.T, srv *Server) (int, map[string]any) {
	t.Helper()
	ts := appSocket(t, srv, nil)
	res, err := appGet(t, ts, "/v1/ft8/state")
	if err != nil {
		t.Fatalf("GET /v1/ft8/state: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("status %d, body %q: %v", res.StatusCode, raw, err)
	}
	return res.StatusCode, body
}

// "FT8 is not served" is an answer of its own, from a route that exists whether
// or not FT8 is enabled: a missing route must never be what says FT8 is off.
func TestFt8State_NotServedIsAnAnswerOfItsOwn(t *testing.T) {
	cases := map[string]*ft8.Service{
		"no FT8 service":      nil,
		"FT8 configured, off": ft8.NewService(types.Ft8Config{Enabled: false}, logging.Noop(), t.TempDir()),
	}
	for name, svc := range cases {
		t.Run(name, func(t *testing.T) {
			status, body := getFt8State(t, appTestServer(t, nil, svc))
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %v)", status, body)
			}
			if len(body) != 1 || body["served"] != false {
				t.Fatalf("body = %v, want only served:false", body)
			}
		})
	}
}

// Served and idle: every field is present and false, so that an answer missing
// one can be told from an idle one.
func TestFt8State_ServedAndIdleNamesEveryField(t *testing.T) {
	svc := ft8.NewService(types.Ft8Config{Enabled: true}, logging.Noop(), t.TempDir())
	status, body := getFt8State(t, appTestServer(t, nil, svc))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %v)", status, body)
	}
	want := map[string]any{
		"served": true, "would_stop": false,
		"tx_armed": false, "tx_in_flight": false, "run_active": false, "auto_work_armed": false,
	}
	if len(body) != len(want) {
		t.Fatalf("body = %v, want exactly %v", body, want)
	}
	for k, v := range want {
		if got, ok := body[k]; !ok || got != v {
			t.Errorf("%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
}

// What the daemon says would stop is the daemon's own rule, carried whole: the
// app reads would_stop and decides nothing from the parts. An armed service is
// not reachable from this package (its keyer is the FT8 package's own), so each
// state is shown on the body the handler builds; internal/ft8 shows the states.
func TestFt8State_WouldStopIsTheDaemonsRule(t *testing.T) {
	cases := []struct {
		name string
		st   ft8.OperatingState
		want bool
	}{
		{"idle", ft8.OperatingState{}, false},
		{"armed, nothing running", ft8.OperatingState{TxArmed: true}, true},
		{"a transmission in flight", ft8.OperatingState{TxInFlight: true}, true},
		{"a run under way", ft8.OperatingState{RunActive: true}, true},
		{"a run waiting between contacts", ft8.OperatingState{AutoWorkArmed: true}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := ft8StateBody(c.st)
			if body.WouldStop == nil || body.TxArmed == nil || body.TxInFlight == nil ||
				body.RunActive == nil || body.AutoWorkArmed == nil {
				t.Fatalf("body = %+v: a served answer names every field", body)
			}
			if !body.Served || *body.WouldStop != c.want {
				t.Fatalf("body = %+v, want served and would_stop=%v", body, c.want)
			}
			if *body.TxArmed != c.st.TxArmed || *body.TxInFlight != c.st.TxInFlight ||
				*body.RunActive != c.st.RunActive || *body.AutoWorkArmed != c.st.AutoWorkArmed {
				t.Fatalf("body = %+v does not carry %+v", body, c.st)
			}
		})
	}
}

// The read needs no admission: it authorises nothing, and the app asks it while
// closing, when it may no longer be the current lifetime. The credential check of
// every route still applies.
func TestFt8State_NeedsNoCurrentLifetimeButStillTheCredential(t *testing.T) {
	svc := ft8.NewService(types.Ft8Config{Enabled: true}, logging.Noop(), t.TempDir())
	srv := appTestServer(t, nil, svc)
	ts := appSocket(t, srv, nil)

	get := func(mutate func(h http.Header)) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/ft8/state", nil)
		if err != nil {
			t.Fatal(err)
		}
		appHeaders(req.Header)
		mutate(req.Header)
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}

	if got := get(func(h http.Header) { h.Set(appLifetimeHeader, "UUUUUUUUUUUUUUUUUUUUUA") }); got != http.StatusOK {
		t.Errorf("a lifetime that is not current: status = %d, want 200", got)
	}
	if got := get(func(h http.Header) { h.Del(appLifetimeHeader) }); got != http.StatusOK {
		t.Errorf("no lifetime: status = %d, want 200", got)
	}
	if got := get(func(h http.Header) { h.Set(operatingCredentialHeader, "wrong") }); got == http.StatusOK {
		t.Errorf("a wrong credential was served the state")
	}
}
