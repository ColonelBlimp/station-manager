package api

// W-0027 C1c, ADR 0103 points 2 and 5: the desktop app's listener. One station behind two
// listeners: the existing one, and HTTPS on loopback with smd's own identity, whose Host and
// Origin are bound to its port.

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ColonelBlimp/station-manager/internal/desktopid"
)

// identity makes a synthetic desktop identity in a private temporary working directory.
func identity(t *testing.T) desktopid.Identity {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wd")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	id, _, err := desktopid.Ensure(root)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// pinnedClient trusts exactly id's certificate, as the desktop shell does.
func pinnedClient(t *testing.T, id desktopid.Identity, maxTLS uint16) *http.Client {
	t.Helper()
	cert, err := x509.ParseCertificate(id.CertDER)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{RootCAs: pool, MaxVersion: maxTLS},
			DisableKeepAlives: true,
		},
	}
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

type twoListeners struct {
	srv      *Server
	id       desktopid.Identity
	mainAddr string
	appPort  int
	mainErr  chan error
	appErr   chan error
}

func (l twoListeners) appURL(path string) string {
	return fmt.Sprintf("https://127.0.0.1:%d%s", l.appPort, path)
}

func startTwo(t *testing.T) twoListeners {
	t.Helper()
	srv := testServer(t)
	l := twoListeners{srv: srv, id: identity(t), mainErr: make(chan error, 1), appErr: make(chan error, 1)}
	// Each serve goroutine is waited for at cleanup: a server still logging when the next test
	// builds its server races on zerolog's process-global time format, which every new logging
	// service writes (internal/logging/service.go).
	mainDone, appDone := make(chan struct{}), make(chan struct{})
	go func() { l.mainErr <- srv.ListenAndServe("127.0.0.1:0"); close(mainDone) }()
	if err := srv.BindApp(0, l.id.TLS); err != nil {
		t.Fatalf("BindApp: %v", err)
	}
	go func() { l.appErr <- srv.ServeApp(); close(appDone) }()
	for i := 0; i < 400 && l.mainAddr == ""; i++ {
		srv.listenerMu.Lock()
		if srv.listener != nil {
			l.mainAddr = srv.listener.Addr().String()
		}
		srv.listenerMu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if l.mainAddr == "" {
		t.Fatal("main listener never bound")
	}
	l.appPort = srv.AppPort()
	if l.appPort == 0 {
		t.Fatal("the app listener reports no port")
	}
	t.Cleanup(func() {
		srv.StopAccepting()
		ctx, cancel := contextWithTimeout(5 * time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		for name, done := range map[string]chan struct{}{"main": mainDone, "app": appDone} {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Errorf("the %s serve goroutine did not return after shutdown", name)
			}
		}
	})
	return l
}

func TestApp_ServesTheSameStationOverPinnedTLS(t *testing.T) {
	l := startTwo(t)
	c := pinnedClient(t, l.id, 0)
	resp, err := c.Get(l.appURL("/v1/healthz"))
	if err != nil {
		t.Fatalf("pinned GET on the app listener: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("app listener /v1/healthz = %d, want 200", resp.StatusCode)
	}
	if resp.TLS == nil || resp.TLS.Version != tls.VersionTLS13 {
		t.Errorf("negotiated TLS = %+v, want TLS 1.3", resp.TLS)
	}
	plain, err := http.Get("http://" + l.mainAddr + "/v1/healthz")
	if err != nil {
		t.Fatalf("GET on the main listener: %v", err)
	}
	_ = plain.Body.Close()
	if plain.StatusCode != http.StatusOK {
		t.Fatalf("main listener /v1/healthz = %d, want 200", plain.StatusCode)
	}
	if reflect.ValueOf(l.srv.appServer.Handler).Pointer() != reflect.ValueOf(l.srv.httpServer.Handler).Pointer() {
		t.Fatal("the app listener does not share the station's handler chain")
	}
}

func TestApp_RefusesAnotherCertificateAnOldTLSAndPlainHTTP(t *testing.T) {
	l := startTwo(t)
	if _, err := pinnedClient(t, identity(t), 0).Get(l.appURL("/v1/healthz")); err == nil {
		t.Error("a client pinning a different certificate completed the handshake")
	}
	if _, err := pinnedClient(t, l.id, tls.VersionTLS12).Get(l.appURL("/v1/healthz")); err == nil {
		t.Error("a TLS 1.2 client was served; the app listener requires TLS 1.3")
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/healthz", l.appPort))
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Error("plain HTTP to the app port was served")
		}
	}
}

// request sends method to the app listener with the given Host and Origin ("" = absent).
func appRequest(t *testing.T, l twoListeners, method, path, host, origin string) int {
	t.Helper()
	req, err := http.NewRequest(method, l.appURL(path), strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := pinnedClient(t, l.id, 0).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestApp_HostAndOriginAreBoundToItsPort(t *testing.T) {
	l := startTwo(t)
	own := fmt.Sprintf("https://127.0.0.1:%d", l.appPort)
	host := fmt.Sprintf("127.0.0.1:%d", l.appPort)
	other := fmt.Sprintf("127.0.0.1:%d", l.appPort+1)

	// A wrong Host is refused for every method (the rebinding defence), even a read.
	if got := appRequest(t, l, http.MethodGet, "/v1/healthz", other, ""); got != http.StatusForbidden {
		t.Errorf("GET with Host %s = %d, want 403", other, got)
	}
	if got := appRequest(t, l, http.MethodGet, "/v1/healthz", "localhost:"+fmt.Sprint(l.appPort), ""); got != http.StatusForbidden {
		t.Errorf("GET with Host localhost = %d, want 403 (only 127.0.0.1:<app port>)", got)
	}
	// Unsafe method: only the app's own Origin, or none (a non-browser client).
	for _, c := range []struct {
		origin string
		ok     bool
	}{
		{own, true},
		{"", true},
		{"http://" + l.mainAddr, false}, // the browser listener's origin
		{fmt.Sprintf("https://127.0.0.1:%d", l.appPort+1), false}, // another port
		{"http://" + host, false},                                 // the wrong scheme
		{fmt.Sprintf("https://localhost:%d", l.appPort), false},   // another loopback name
	} {
		got := appRequest(t, l, http.MethodPost, "/v1/notifications", host, c.origin)
		if refused := got == http.StatusForbidden; refused == c.ok {
			t.Errorf("POST with Origin %q = %d; want refused=%v", c.origin, got, !c.ok)
		}
	}
}

func TestApp_TheBrowserListenerIsUnchanged(t *testing.T) {
	// The HTTP listener still accepts any loopback Origin, the app's included: tightening it
	// is a separate change (codex-056).
	l := startTwo(t)
	req, _ := http.NewRequest(http.MethodPost, "http://"+l.mainAddr+"/v1/notifications", strings.NewReader("{}"))
	req.Header.Set("Origin", fmt.Sprintf("https://127.0.0.1:%d", l.appPort))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("the browser listener changed: it now refuses a loopback Origin")
	}
}

// upgrade attempts a WebSocket upgrade of the event stream on the app listener and returns
// the HTTP status of the response.
func upgrade(t *testing.T, l twoListeners, origin string) int {
	t.Helper()
	cert, _ := x509.ParseCertificate(l.id.CertDER)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", l.appPort), &tls.Config{RootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := "GET " + wsEventsRoute + " HTTP/1.1\r\n" +
		fmt.Sprintf("Host: 127.0.0.1:%d\r\n", l.appPort) +
		"Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	if _, err := conn.Write([]byte(req + "\r\n")); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestApp_AnUpgradeNeedsTheAppsOwnOrigin(t *testing.T) {
	l := startTwo(t)
	if got := upgrade(t, l, fmt.Sprintf("https://127.0.0.1:%d", l.appPort)); got != http.StatusSwitchingProtocols {
		t.Errorf("upgrade with the app's Origin = %d, want 101", got)
	}
	if got := upgrade(t, l, ""); got != http.StatusForbidden {
		t.Errorf("upgrade without an Origin = %d, want 403 (absent Origin is allowed only for non-upgrade requests)", got)
	}
	if got := upgrade(t, l, "http://"+l.mainAddr); got != http.StatusForbidden {
		t.Errorf("upgrade with the browser listener's Origin = %d, want 403", got)
	}
}

func TestApp_StopAcceptingAndShutdownCloseBothListeners(t *testing.T) {
	l := startTwo(t)
	l.srv.StopAccepting()
	for name, ch := range map[string]chan error{"main": l.mainErr, "app": l.appErr} {
		select {
		case err := <-ch:
			if err != nil {
				t.Errorf("%s serve returned %v after StopAccepting, want a clean stop", name, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s serve did not return after StopAccepting", name)
		}
	}
	ctx, cancel := contextWithTimeout(3 * time.Second)
	defer cancel()
	if err := l.srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	for _, addr := range []string{l.mainAddr, fmt.Sprintf("127.0.0.1:%d", l.appPort)} {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			t.Errorf("%s still held after shutdown: %v", addr, err)
			continue
		}
		_ = ln.Close()
	}
}

func TestApp_BindFailureIsReturnedNotFatal(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	srv := testServer(t)
	port := busy.Addr().(*net.TCPAddr).Port
	if err := srv.BindApp(port, identity(t).TLS); err == nil {
		t.Fatal("binding a busy app port reported no error")
	}
	if srv.AppPort() != 0 {
		t.Error("a failed bind left an app listener recorded")
	}
	// Shutdown with no app listener is fine.
	ctx, cancel := contextWithTimeout(time.Second)
	defer cancel()
	srv.StopAccepting()
	_ = srv.Shutdown(ctx)
}

func TestApp_OneStationBehindBothListeners(t *testing.T) {
	l := startTwo(t)
	own := fmt.Sprintf("https://127.0.0.1:%d", l.appPort)
	req, _ := http.NewRequest(http.MethodPost, l.appURL("/v1/logbook"),
		strings.NewReader(`{"name":"Made through the app","callsign":"7Q5MLV"}`))
	req.Header.Set("Origin", own)
	req.Header.Set("Content-Type", "application/json")
	resp, err := pinnedClient(t, l.id, 0).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var made struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&made)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || made.ID < 1 {
		t.Fatalf("create through the app listener = %d (id %d), want 201", resp.StatusCode, made.ID)
	}
	got, err := http.Get(fmt.Sprintf("http://%s/v1/logbook/%d", l.mainAddr, made.ID))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(got.Body)
	_ = got.Body.Close()
	if got.StatusCode != http.StatusOK || !strings.Contains(string(body), "Made through the app") {
		t.Fatalf("the browser listener does not see the app's logbook: %d %s", got.StatusCode, body)
	}

	// The SSE stream is served over the app listener too.
	ctx, cancel := contextWithTimeout(3 * time.Second)
	defer cancel()
	sreq, _ := http.NewRequestWithContext(ctx, http.MethodGet, l.appURL("/v1/events"), nil)
	sresp, err := pinnedClient(t, l.id, 0).Do(sreq)
	if err != nil {
		t.Fatalf("open the event stream on the app listener: %v", err)
	}
	defer sresp.Body.Close()
	if first, _ := readFirstTwoLines(t, sresp.Body, 2*time.Second); first != ": connected" {
		t.Fatalf("app listener event stream first line = %q, want \": connected\"", first)
	}
}

// dialListener opens a raw connection to the main ("main") or app ("app") listener.
func dialListener(t *testing.T, l twoListeners, which string) (net.Conn, string) {
	t.Helper()
	if which == "main" {
		c, err := net.Dial("tcp", l.mainAddr)
		if err != nil {
			t.Fatal(err)
		}
		return c, l.mainAddr
	}
	cert, _ := x509.ParseCertificate(l.id.CertDER)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	host := fmt.Sprintf("127.0.0.1:%d", l.appPort)
	c, err := tls.Dial("tcp", host, &tls.Config{RootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	return c, host
}

func TestApp_AStalledDrainNeverHoldsTheOtherServer(t *testing.T) {
	for _, c := range []struct{ stalled, other string }{{"app", "main"}, {"main", "app"}} {
		t.Run("stalled "+c.stalled, func(t *testing.T) {
			l := startTwo(t)
			// A request whose body never arrives keeps its server's drain busy.
			stall, host := dialListener(t, l, c.stalled)
			defer stall.Close()
			origin := "http://" + host
			if c.stalled == "app" {
				origin = "https://" + host
			}
			if _, err := fmt.Fprintf(stall, "POST /v1/logbook HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\n"+
				"Content-Type: application/json\r\nContent-Length: 1000\r\n\r\n{", host, origin); err != nil {
				t.Fatal(err)
			}
			// An idle keep-alive connection on the other server: its drain closes it at once.
			idle, ihost := dialListener(t, l, c.other)
			defer idle.Close()
			if _, err := fmt.Fprintf(idle, "GET /v1/healthz HTTP/1.1\r\nHost: %s\r\n\r\n", ihost); err != nil {
				t.Fatal(err)
			}
			br := bufio.NewReader(idle)
			resp, err := http.ReadResponse(br, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			time.Sleep(100 * time.Millisecond) // let the stalled request reach its handler

			// Shutdown alone, not after StopAccepting: StopAccepting already closes idle connections
			// on both servers (SetKeepAlivesEnabled(false) does), which would hide the drain order.
			ctx, cancel := contextWithTimeout(2 * time.Second)
			defer cancel()
			start := time.Now()
			done := make(chan error, 1)
			go func() { done <- l.srv.Shutdown(ctx) }()

			_ = idle.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, _ = br.ReadByte() // returns when the other server closes the idle connection
			if took := time.Since(start); took > time.Second {
				t.Errorf("the %s server's idle connection closed after %v: its drain waited for the stalled %s drain",
					c.other, took, c.stalled)
			}
			if err := <-done; err == nil {
				t.Error("Shutdown reported success with a drain still stalled")
			}
		})
	}
}

func TestApp_AWebSocketDeliversTheStationsEvents(t *testing.T) {
	l := startTwo(t)
	ctx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()
	own := fmt.Sprintf("https://127.0.0.1:%d", l.appPort)
	c, resp, err := websocket.Dial(ctx, fmt.Sprintf("wss://127.0.0.1:%d%s", l.appPort, wsEventsPath), &websocket.DialOptions{
		HTTPClient: pinnedClient(t, l.id, 0),
		HTTPHeader: http.Header{"Origin": []string{own}},
	})
	if err != nil {
		t.Fatalf("WebSocket on the app listener: %v (resp %v)", err, resp)
	}
	defer func() { _ = c.CloseNow() }()
	if f := libRead(t, c); f.Event != "connected" {
		t.Fatalf("first frame %q, want connected", f.Event)
	}
	l.srv.hub.Publish("qso.stored", map[string]any{"qso_uuid": "through-the-app"})
	if f := libRead(t, c); f.Event != "qso.stored" || !strings.Contains(string(f.Data), "through-the-app") {
		t.Fatalf("frame = %q %s, want the station's qso.stored event", f.Event, f.Data)
	}
}

func TestApp_BindAttemptedSignalsTheBrowserBindEitherWay(t *testing.T) {
	// Both servers are built before either serves: building one while another logs races on
	// zerolog's process-global time format (see startTwo).
	ok, failed := testServer(t), testServer(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	okDone := make(chan struct{})
	go func() { _ = ok.ListenAndServe("127.0.0.1:0"); close(okDone) }()
	select {
	case <-ok.BindAttempted():
	case <-time.After(3 * time.Second):
		t.Fatal("BindAttempted not signalled after a successful bind")
	}
	if !ok.Listening() {
		t.Error("Listening() = false after a successful bind")
	}
	ok.StopAccepting()
	waitClosed(t, okDone, "the successful server's ListenAndServe")

	failedDone := make(chan struct{})
	go func() { _ = failed.ListenAndServe(busy.Addr().String()); close(failedDone) }()
	select {
	case <-failed.BindAttempted():
	case <-time.After(3 * time.Second):
		t.Fatal("BindAttempted not signalled after a failed bind")
	}
	if failed.Listening() {
		t.Error("Listening() = true after a failed bind")
	}
	waitClosed(t, failedDone, "the failed server's ListenAndServe")
}

func waitClosed(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", what)
	}
}

func TestApp_NoAppListenerOnceTheStopHasBegun(t *testing.T) {
	for name, stop := range map[string]func(*Server){
		"after StopAccepting": func(s *Server) { s.StopAccepting() },
		"after Shutdown": func(s *Server) {
			ctx, cancel := contextWithTimeout(time.Second)
			defer cancel()
			_ = s.Shutdown(ctx)
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := testServer(t)
			stop(srv)
			port := freePort(t)
			if err := srv.BindApp(port, identity(t).TLS); err == nil {
				t.Error("BindApp succeeded after the stop began: nothing would ever close it")
			}
			if srv.AppPort() != 0 {
				t.Error("an app listener is recorded after the stop began")
			}
			assertPortFree(t, port)
		})
	}
}

func TestApp_ShutdownAloneClosesABoundListenerNeverServed(t *testing.T) {
	srv := testServer(t)
	if err := srv.BindApp(0, identity(t).TLS); err != nil {
		t.Fatal(err)
	}
	port := srv.AppPort()
	ctx, cancel := contextWithTimeout(time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	assertPortFree(t, port)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func assertPortFree(t *testing.T, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port %d is still held: %v", port, err)
	}
	_ = ln.Close()
}

// Browsers omit a URL's default port: for the app listener on 443, Chromium (so the desktop
// shell) sends Host "127.0.0.1" and Origin "https://127.0.0.1" (clean-room review of a27adce7).
// Only that exact equivalence is accepted; every other port keeps its literal form.
func TestApp_AppAddressForms(t *testing.T) {
	for _, c := range []struct {
		port             int
		host, origin     string
		hostOK, originOK bool
	}{
		{8443, "127.0.0.1:8443", "https://127.0.0.1:8443", true, true},
		{8443, "127.0.0.1", "https://127.0.0.1", false, false}, // a portless form means 443, not 8443
		{443, "127.0.0.1:443", "https://127.0.0.1:443", true, true},
		{443, "127.0.0.1", "https://127.0.0.1", true, true},
		{443, "127.0.0.1:8443", "https://127.0.0.1:8443", false, false},
		{443, "localhost", "https://localhost", false, false},
		{443, "127.0.0.1:", "https://127.0.0.1:", false, false},
		{443, "127.0.0.1", "http://127.0.0.1", true, false}, // the scheme still matters
	} {
		if got := appHostAllowed(c.host, c.port); got != c.hostOK {
			t.Errorf("port %d: appHostAllowed(%q) = %v, want %v", c.port, c.host, got, c.hostOK)
		}
		if got := appOriginAllowed(c.origin, c.port); got != c.originOK {
			t.Errorf("port %d: appOriginAllowed(%q) = %v, want %v", c.port, c.origin, got, c.originOK)
		}
	}
}

// The request path on 443 (codex-062): requests marked as arriving on the app listener go
// through the real handler chain with the bound port set to 443, with no socket on 443 (tests
// cannot bind it). The portless forms browsers send pass the guard; other forms are refused.
func TestApp_Port443PortlessFormsThroughTheGuard(t *testing.T) {
	srv := testServer(t)
	srv.appMu.Lock()
	srv.appPort = 443
	srv.appMu.Unlock()
	h := srv.httpServer.Handler
	send := func(method, path, host, origin string, upgrade bool) int {
		t.Helper()
		req := httptest.NewRequest(method, "https://"+host+path, strings.NewReader("{}"))
		req.Host = host
		req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		req = req.WithContext(context.WithValue(req.Context(), appConnKey{}, true))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if upgrade {
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	for _, c := range []struct {
		name                 string
		method, path         string
		host, origin         string
		upgrade, wantRefused bool
	}{
		{"portless GET", http.MethodGet, "/v1/healthz", "127.0.0.1", "", false, false},
		{"explicit GET", http.MethodGet, "/v1/healthz", "127.0.0.1:443", "", false, false},
		{"portless POST, portless Origin", http.MethodPost, "/v1/notifications", "127.0.0.1", "https://127.0.0.1", false, false},
		{"portless POST, explicit Origin", http.MethodPost, "/v1/notifications", "127.0.0.1", "https://127.0.0.1:443", false, false},
		{"another port", http.MethodGet, "/v1/healthz", "127.0.0.1:8443", "", false, true},
		{"http Origin", http.MethodPost, "/v1/notifications", "127.0.0.1", "http://127.0.0.1", false, true},
		{"WebSocket, portless pair", http.MethodGet, wsEventsPath, "127.0.0.1", "https://127.0.0.1", true, false},
		{"WebSocket, no Origin", http.MethodGet, wsEventsPath, "127.0.0.1", "", true, true},
		{"WebSocket, http Origin", http.MethodGet, wsEventsPath, "127.0.0.1", "http://127.0.0.1", true, true},
	} {
		got := send(c.method, c.path, c.host, c.origin, c.upgrade)
		if refused := got == http.StatusForbidden; refused != c.wantRefused {
			t.Errorf("%s: status %d, want refused=%v", c.name, got, c.wantRefused)
		}
	}
}
