package api

// W-0027 C2a: the operating-client check. With server.require_desktop_app on, a request is
// served only if it arrived on the desktop app's listener and carries the per-install
// operating credential; the one exception is GET /v1/healthz. With it off, nothing changes.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/desktopid"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const refusedCode = "desktop_app_required"

// wrongCredential has the shape of a credential and is recognisable in a log.
var wrongCredential = strings.Repeat("W", 43)

// --- the rule itself, against a stub handler -------------------------------------------

// stubNext records whether the check let a request through, and what it carried.
type stubNext struct {
	called bool
	header http.Header
}

func (n *stubNext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.called = true
	n.header = r.Header.Clone()
	w.WriteHeader(http.StatusNoContent)
}

func checkRequest(method, path string, app bool, creds ...string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	for _, c := range creds {
		r.Header.Add(operatingCredentialHeader, c)
	}
	if app {
		r = r.WithContext(context.WithValue(r.Context(), appConnKey{}, true))
	}
	return r
}

func TestOperatingClient_TheRule(t *testing.T) {
	const good = "the-good-credential"
	matches := func(p string) bool { return p == good }
	type c struct {
		name   string
		on     bool
		match  func(string) bool
		method string
		path   string
		app    bool
		creds  []string
		pass   bool
	}
	cases := []c{
		// On: only the app listener with exactly the one right credential.
		{"on, app, the credential", true, matches, "GET", "/v1/version", true, []string{good}, true},
		{"on, app, the credential, a mutation", true, matches, "POST", "/v1/qso", true, []string{good}, true},
		{"on, app, the credential, a page", true, matches, "GET", "/operate", true, []string{good}, true},
		{"on, app, none", true, matches, "GET", "/v1/version", true, nil, false},
		{"on, app, empty", true, matches, "GET", "/v1/version", true, []string{""}, false},
		{"on, app, wrong", true, matches, "GET", "/v1/version", true, []string{"wrong"}, false},
		{"on, app, repeated", true, matches, "GET", "/v1/version", true, []string{good, good}, false},
		{"on, app, right then wrong", true, matches, "GET", "/v1/version", true, []string{good, "wrong"}, false},
		{"on, app, a page, none", true, matches, "GET", "/operate", true, nil, false},
		// On: the main listener serves nothing, a valid credential included (criterion 4).
		{"on, main, the credential", true, matches, "GET", "/v1/version", false, []string{good}, false},
		{"on, main, none", true, matches, "GET", "/v1/version", false, nil, false},
		{"on, main, a page", true, matches, "GET", "/", false, nil, false},
		// On: the one exception, exactly.
		{"on, main, the health check", true, matches, "GET", "/v1/healthz", false, nil, true},
		{"on, app, the health check, none", true, matches, "GET", "/v1/healthz", true, nil, true},
		{"on, the health check by HEAD", true, matches, "HEAD", "/v1/healthz", false, nil, false},
		{"on, the health check by POST", true, matches, "POST", "/v1/healthz", false, nil, false},
		{"on, a path under the health check", true, matches, "GET", "/v1/healthz/", false, nil, false},
		{"on, a path that only starts like it", true, matches, "GET", "/v1/healthzz", false, nil, false},
		{"on, an unclean path to it", true, matches, "GET", "/v1/../v1/healthz", false, nil, false},
		// On, with no credential loaded: nothing matches, an absent or empty header included.
		{"on, nothing loaded, none", true, nil, "GET", "/v1/version", true, nil, false},
		{"on, nothing loaded, empty", true, nil, "GET", "/v1/version", true, []string{""}, false},
		{"on, nothing loaded, the health check", true, nil, "GET", "/v1/healthz", false, nil, true},
		// Off: as today on both listeners; a credential is ignored.
		{"off, main, none", false, matches, "POST", "/v1/qso", false, nil, true},
		{"off, app, none", false, matches, "GET", "/v1/version", true, nil, true},
		{"off, app, wrong", false, matches, "GET", "/v1/version", true, []string{"wrong"}, true},
		{"off, main, the credential", false, matches, "GET", "/v1/version", false, []string{good}, true},
		{"off, nothing loaded", false, nil, "GET", "/v1/version", false, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServerWithCfg(t, func(cfg *config.Config) { cfg.Server.RequireDesktopApp = tc.on })
			if tc.match != nil {
				srv.SetOperatingCredential(tc.match)
			}
			next := &stubNext{}
			w := httptest.NewRecorder()
			srv.requireOperatingClient(next).ServeHTTP(w, checkRequest(tc.method, tc.path, tc.app, tc.creds...))
			if next.called != tc.pass {
				t.Fatalf("let through = %v, want %v (status %d, body %s)", next.called, tc.pass, w.Code, w.Body)
			}
			if tc.pass {
				// Whatever was presented never travels on to a handler, in either state.
				if got := next.header.Values(operatingCredentialHeader); len(got) != 0 {
					t.Fatalf("the handler was handed the credential header: %d value(s)", len(got))
				}
				return
			}
			if w.Code != http.StatusForbidden || errorCode(t, w.Body.Bytes()) != refusedCode {
				t.Fatalf("refusal = %d %s; want 403 %s", w.Code, w.Body, refusedCode)
			}
			if strings.Contains(w.Body.String(), good) {
				t.Fatal("the refusal prints the credential")
			}
		})
	}
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return "(not the JSON error envelope: " + strings.TrimSpace(string(body[:min(len(body), 80)])) + ")"
	}
	return e.Code
}

// The header is canonicalised by net/http, so any letter case a client sends is the one header.
func TestOperatingClient_TheHeaderNameIsCaseInsensitive(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) { cfg.Server.RequireDesktopApp = true })
	srv.SetOperatingCredential(func(p string) bool { return p == "good" })
	for _, name := range []string{"x-sm-operating-credential", "X-SM-OPERATING-CREDENTIAL", "X-Sm-Operating-Credential"} {
		r := checkRequest("GET", "/v1/version", true)
		r.Header.Set(name, "good")
		next := &stubNext{}
		srv.requireOperatingClient(next).ServeHTTP(httptest.NewRecorder(), r)
		if !next.called {
			t.Errorf("a credential sent as %q was not accepted", name)
		}
	}
	// Two spellings are two values of the one header: refused.
	r := checkRequest("GET", "/v1/version", true)
	r.Header.Add("x-sm-operating-credential", "good")
	r.Header.Add("X-SM-Operating-Credential", "good")
	next := &stubNext{}
	srv.requireOperatingClient(next).ServeHTTP(httptest.NewRecorder(), r)
	if next.called {
		t.Fatal("two spellings of the header were accepted as one credential")
	}
}

// --- the assembled server, over its real listeners --------------------------------------

type checked struct {
	twoListeners
	ft8  *ft8.Service
	cred string // the published credential, as a client reads it
	log  *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startChecked serves a station with every optional surface registered (rig, FT8, profiling,
// the SPA and the manual) on both listeners, with the check on or off and a real credential
// loaded from a private working directory. The rig and FT8 services are enabled but never
// started: no hardware and no capture.
func startChecked(t *testing.T, on bool) checked {
	t.Helper()
	logBuf := &syncBuffer{}
	base := testServerWithLogger(t, func(cfg *config.Config) {
		cfg.Server.RequireDesktopApp = on
		cfg.Server.EnableProfiling = true
		serve := true
		cfg.Server.ServeSPA = &serve
	}, nil, logging.NewForWriter(logBuf))
	f := ft8.NewService(types.Ft8Config{Enabled: true}, base.logger, t.TempDir())
	br := bridge.New(types.BridgeConfig{Enabled: true, Cat: &types.BridgeCatConfig{Driver: "yaesu-ftdx10"}}, base.logger)
	srv := New(base.cfg.Snapshot(), "test", base.cfg, base.qso, base.db, base.logger, base.hub, base.enrich, base.mailer, br, f)
	srv.SetServedArchive(testServedArchive)

	wd := filepath.Join(t.TempDir(), "wd")
	if err := os.Mkdir(wd, 0o700); err != nil {
		t.Fatal(err)
	}
	id, _, err := desktopid.Ensure(wd)
	if err != nil {
		t.Fatal(err)
	}
	cred, _, err := desktopid.EnsureCredential(wd)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetOperatingCredential(cred.Matches)
	value, err := os.ReadFile(filepath.Join(wd, desktopid.CredentialDirName, desktopid.CredentialFileName))
	if err != nil {
		t.Fatal(err)
	}
	return checked{twoListeners: startTwoWith(t, srv, id), ft8: f, cred: string(value), log: logBuf}
}

func (l checked) mainURL(path string) string { return "http://" + l.mainAddr + path }

// clients returns one client per listener; neither follows a redirect.
func (l checked) clients(t *testing.T) (mainC, appC *http.Client) {
	t.Helper()
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	appC = pinnedClient(t, l.id, 0)
	appC.CheckRedirect = noRedirect
	mainC = &http.Client{Timeout: 5 * time.Second, CheckRedirect: noRedirect,
		Transport: &http.Transport{DisableKeepAlives: true}}
	return mainC, appC
}

// fillPattern turns a declared pattern into a method and a concrete path.
func fillPattern(t *testing.T, pattern string) (method, path string) {
	t.Helper()
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		t.Fatalf("declared route %q has no method", pattern)
	}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if !strings.HasPrefix(p, "{") {
			continue
		}
		switch strings.TrimSuffix(strings.Trim(p, "{}"), "...") {
		case "uuid":
			parts[i] = testServedArchive
		case "id":
			parts[i] = "1"
		default:
			parts[i] = "x"
		}
	}
	return method, strings.Join(parts, "/")
}

// acceptable builds a request no other guard would refuse: the listener's own Host, its own
// Origin on an unsafe method, and a complete upgrade on a WebSocket route.
func (l checked) acceptable(t *testing.T, app bool, method, path string, creds ...string) *http.Request {
	t.Helper()
	url, origin := l.mainURL(path), "http://"+l.mainAddr
	if app {
		url, origin = l.appURL(path), fmt.Sprintf("https://127.0.0.1:%d", l.appPort)
	}
	r, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", origin)
	r.Header.Set(archiveIDHeader, testServedArchive)
	if strings.HasSuffix(path, "/ws") {
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	}
	for _, c := range creds {
		r.Header.Add(operatingCredentialHeader, c)
	}
	return r
}

func refusedByTheCheck(t *testing.T, c *http.Client, r *http.Request, what string) {
	t.Helper()
	resp, err := c.Do(r)
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	// Anything but a 403 is reported without reading its body: a served stream or an upgraded
	// WebSocket has no end to read to, and the client's timeout does not cover an upgrade.
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("%s: %d; want 403 %s", what, resp.StatusCode, refusedCode)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if code := errorCode(t, body); code != refusedCode {
		t.Errorf("%s: 403 %s; want 403 %s", what, code, refusedCode)
	}
}

// Criterion 2: every declared route, the ones a search of the source misses included, is
// refused with the check's own code on both listeners unless the credential is presented on
// the app listener; and criterion 4, a valid credential on the main listener is refused.
func TestOperatingClient_EveryDeclaredRouteIsRefusedWithoutTheCredential(t *testing.T) {
	l := startChecked(t, true)
	mainC, appC := l.clients(t)
	routes := l.srv.DeclaredRoutes()

	// The inventory is the server's own, so a route added later is swept without being
	// listed here. These prove it includes what a literal search of the source would miss.
	for _, want := range []string{
		"GET /v1/healthz",
		"POST /v1/default-logbook/confirm", // registered by a helper
		"GET " + wsEventsRoute,             // paths built from constants
		"GET " + wsRigEventsRoute,
		"GET /v1/rig/events", "POST /v1/rig/tune", "POST /v1/rig/command", // only with a rig
		"GET /v1/ft8/events", "POST /v1/ft8/tx/arm", "POST /v1/ft8/cq/start", // only with FT8
		"GET /debug/pprof/profile", "POST /debug/pprof/symbol", // only with profiling
		"GET /", "GET /manual/", "GET /app/", // the pages
	} {
		if !slices.Contains(routes, want) {
			t.Fatalf("the declared routes do not include %q; the sweep would miss it", want)
		}
	}

	swept := 0
	for _, pattern := range routes {
		if pattern == "GET /v1/healthz" {
			continue // the exception, tested on its own
		}
		method, path := fillPattern(t, pattern)
		for _, tc := range []struct {
			name  string
			app   bool
			creds []string
		}{
			{"main, no credential", false, nil},
			{"main, the valid credential", false, []string{l.cred}},
			{"main, a wrong credential", false, []string{wrongCredential}},
			{"app, no credential", true, nil},
			{"app, an empty credential", true, []string{""}},
			{"app, a wrong credential", true, []string{wrongCredential}},
			{"app, the valid credential twice", true, []string{l.cred, l.cred}},
		} {
			c := mainC
			if tc.app {
				c = appC
			}
			refusedByTheCheck(t, c, l.acceptable(t, tc.app, method, path, tc.creds...), pattern+" ("+tc.name+")")
		}
		swept++
	}
	t.Logf("swept %d of %d declared routes, 7 requests each", swept, len(routes))

	// A path no route declares gets the same answer: the check does not reveal what exists.
	refusedByTheCheck(t, mainC, l.acceptable(t, false, "GET", "/v1/no-such-route"), "an undeclared API path")
	refusedByTheCheck(t, appC, l.acceptable(t, true, "DELETE", "/v1/no-such-route"), "an undeclared API path on the app listener")

	// Neither the published credential nor a presented one is ever written to the log.
	log := l.log.String()
	if strings.Contains(log, l.cred) {
		t.Fatal("the credential was written to the log")
	}
	if strings.Contains(log, wrongCredential) {
		t.Fatal("a presented credential was written to the log")
	}
}

// The exception is exactly GET /v1/healthz, answering as it does with the check off.
func TestOperatingClient_TheHealthCheckStaysOpen(t *testing.T) {
	on, off := startChecked(t, true), startChecked(t, false)
	get := func(l checked, app bool) (int, string) {
		mainC, appC := l.clients(t)
		c := mainC
		if app {
			c = appC
		}
		resp, err := c.Do(l.acceptable(t, app, "GET", "/v1/healthz"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	for _, app := range []bool{false, true} {
		wantCode, wantBody := get(off, app)
		gotCode, gotBody := get(on, app)
		if wantCode != http.StatusOK {
			t.Fatalf("with the check off the health check answers %d; the comparison has no baseline", wantCode)
		}
		if gotCode != wantCode || gotBody != wantBody {
			t.Errorf("app=%v: the health check answers %d %s with the check on; want it unchanged: %d %s",
				app, gotCode, gotBody, wantCode, wantBody)
		}
	}
	// Only GET: a HEAD (which the route would also answer) is refused. It has no body to read.
	mainC, _ := on.clients(t)
	resp, err := mainC.Do(on.acceptable(t, false, "HEAD", "/v1/healthz"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("HEAD /v1/healthz = %d with the check on; want 403", resp.StatusCode)
	}
}

// Criterion 1, at the server: with the credential, the app listener serves pages, requests,
// a mutation, an SSE stream and a WebSocket.
func TestOperatingClient_TheCredentialOnTheAppListenerIsServed(t *testing.T) {
	l := startChecked(t, true)
	_, appC := l.clients(t)

	resp, err := appC.Do(l.acceptable(t, true, "GET", "/v1/version", l.cred))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/version with the credential = %d, want 200", resp.StatusCode)
	}

	resp, err = appC.Do(l.acceptable(t, true, "GET", "/", l.cred))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET / with the credential = %d %s; want the app's page", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if len(page) == 0 {
		t.Fatal("the app's page is empty")
	}

	// A mutation reaches its handler: whatever it answers, it is not the check's refusal.
	resp, err = appC.Do(l.acceptable(t, true, "POST", "/v1/notifications", l.cred))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if code := errorCode(t, body); code == refusedCode || code == "cross_origin" {
		t.Fatalf("a mutation with the credential was refused: %d %s", resp.StatusCode, code)
	}

	// An SSE stream.
	ctx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()
	stream := &http.Client{Transport: appC.Transport} // no overall timeout: the context bounds it
	resp, err = stream.Do(l.acceptable(t, true, "GET", "/v1/events", l.cred).WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("GET /v1/events with the credential = %d %s; want an event stream", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if line, rerr := bufio.NewReader(resp.Body).ReadString('\n'); rerr != nil || strings.TrimSpace(line) == "" {
		t.Fatalf("the event stream delivered nothing: %q %v", line, rerr)
	}
	_ = resp.Body.Close()

	// A WebSocket: the upgrade carries the credential like any request.
	own := fmt.Sprintf("https://127.0.0.1:%d", l.appPort)
	ws, wsResp, err := websocket.Dial(ctx, fmt.Sprintf("wss://127.0.0.1:%d%s", l.appPort, wsEventsPath), &websocket.DialOptions{
		HTTPClient: pinnedClient(t, l.id, 0),
		HTTPHeader: http.Header{"Origin": []string{own}, operatingCredentialHeader: []string{l.cred}},
	})
	if err != nil {
		t.Fatalf("WebSocket with the credential: %v (resp %v)", err, wsResp)
	}
	defer func() { _ = ws.CloseNow() }()
	if f := libRead(t, ws); f.Event != "connected" {
		t.Fatalf("first frame %q, want connected", f.Event)
	}

	// And without it, the same upgrade never switches protocols.
	_, wsResp, err = websocket.Dial(ctx, fmt.Sprintf("wss://127.0.0.1:%d%s", l.appPort, wsEventsPath), &websocket.DialOptions{
		HTTPClient: pinnedClient(t, l.id, 0),
		HTTPHeader: http.Header{"Origin": []string{own}},
	})
	if err == nil {
		t.Fatal("a WebSocket without the credential was upgraded")
	}
	if wsResp == nil || wsResp.StatusCode != http.StatusForbidden {
		t.Fatalf("a WebSocket without the credential: %v; want 403", wsResp)
	}
}

// Criterion 3: a refused FT8 subscription adds no subscriber. The subscriber count is what
// starts capture and what holds off the linger that disarms TX, so a client without the
// credential can do neither. The count is read through the profile claim, which refuses a
// switch while any subscriber is connected.
func TestOperatingClient_ARefusedFt8StreamAddsNoSubscriber(t *testing.T) {
	l := startChecked(t, true)
	mainC, appC := l.clients(t)

	refusedByTheCheck(t, mainC, l.acceptable(t, false, "GET", "/v1/ft8/events"), "FT8 stream on the main listener")
	refusedByTheCheck(t, mainC, l.acceptable(t, false, "GET", "/v1/ft8/events", l.cred), "FT8 stream on the main listener, with the credential")
	refusedByTheCheck(t, appC, l.acceptable(t, true, "GET", "/v1/ft8/events"), "FT8 stream on the app listener, no credential")
	if _, err := l.ft8.ClaimProfile("FT4"); err != nil {
		t.Fatalf("a refused subscription is counted as a subscriber: the profile claim says %v", err)
	}

	// The fixture can tell: a subscription that is admitted is counted.
	ctx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()
	stream := &http.Client{Transport: appC.Transport}
	resp, err := stream.Do(l.acceptable(t, true, "GET", "/v1/ft8/events", l.cred).WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the FT8 stream with the credential = %d, want 200", resp.StatusCode)
	}
	if _, err := l.ft8.ClaimProfile("FT8"); !stderr.Is(err, ft8.ErrProfileBusy) {
		t.Fatalf("with an admitted subscriber the profile claim says %v; want the busy refusal", err)
	}
}

// The other guards still run first: a foreign Host is answered by the origin guard, with its
// own code and its own log line, not by the check.
func TestOperatingClient_TheOriginGuardStillAnswersFirst(t *testing.T) {
	l := startChecked(t, true)
	mainC, _ := l.clients(t)
	r := l.acceptable(t, false, "GET", "/v1/version")
	r.Host = "attacker.example"
	resp, err := mainC.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || errorCode(t, body) != "cross_origin" {
		t.Fatalf("a foreign Host = %d %s; want 403 cross_origin", resp.StatusCode, errorCode(t, body))
	}
}

// Criterion 7: with the check off, both listeners serve as today and a credential is ignored.
func TestOperatingClient_OffChangesNothing(t *testing.T) {
	l := startChecked(t, false)
	mainC, appC := l.clients(t)
	for _, tc := range []struct {
		name  string
		app   bool
		creds []string
	}{
		{"main, no credential", false, nil},
		{"main, a wrong credential", false, []string{wrongCredential}},
		{"app, no credential", true, nil},
		{"app, a wrong credential", true, []string{wrongCredential}},
		{"app, the credential", true, []string{l.cred}},
	} {
		c := mainC
		if tc.app {
			c = appC
		}
		for _, path := range []string{"/v1/version", "/"} {
			resp, err := c.Do(l.acceptable(t, tc.app, "GET", path, tc.creds...))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s: GET %s = %d with the check off; want 200", tc.name, path, resp.StatusCode)
			}
		}
	}
	if log := l.log.String(); strings.Contains(log, wrongCredential) || strings.Contains(log, l.cred) {
		t.Fatal("a credential was written to the log with the check off")
	}
}

// A refusal writes one line: which listener, the method, the path and whether a credential
// was presented. Never a value.
func TestOperatingClient_ARefusalIsLoggedWithoutAnyCredential(t *testing.T) {
	l := startChecked(t, true)
	mainC, appC := l.clients(t)
	refusedByTheCheck(t, mainC, l.acceptable(t, false, "GET", "/v1/qso/"+testServedArchive), "main")
	refusedByTheCheck(t, appC, l.acceptable(t, true, "POST", "/v1/restart", wrongCredential), "app")

	type line struct {
		Level, Message, Listener, Method, Path string
		Present                                *bool `json:"credential_present"`
	}
	var refusals []line
	for _, raw := range strings.Split(l.log.String(), "\n") {
		var ln line
		if json.Unmarshal([]byte(raw), &ln) == nil && strings.Contains(ln.Message, "operating-client check") {
			refusals = append(refusals, ln)
		}
	}
	if len(refusals) != 2 {
		t.Fatalf("%d refusal lines, want one per refused request (2):\n%s", len(refusals), l.log.String())
	}
	want := []struct {
		listener, method, path string
		present                bool
	}{
		{"main", "GET", "/v1/qso/" + testServedArchive, false},
		{"app", "POST", "/v1/restart", true},
	}
	for i, w := range want {
		got := refusals[i]
		if got.Level != "warn" || got.Listener != w.listener || got.Method != w.method || got.Path != w.path ||
			got.Present == nil || *got.Present != w.present {
			t.Errorf("refusal line %d = %+v (credential_present %v); want %+v", i, got, got.Present, w)
		}
	}
	if log := l.log.String(); strings.Contains(log, wrongCredential) || strings.Contains(log, l.cred) {
		t.Fatal("a credential was written to the log")
	}
}
