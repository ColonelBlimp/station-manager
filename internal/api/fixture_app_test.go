package api

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// The shared fixtures' desktop app (W-0027 C2e, part 2).
//
// FT8 is operated only by the app's current lifetime with its FT8 stream open. A test that
// calls an FT8 handler directly, past the operating-client check, therefore needs two
// things the check and the admission would have established: a service operated by a
// lifetime that is present, and that lifetime on the request, already validated. The
// tests of the check and of the admission themselves do not use these helpers: they
// present a real credential to real listeners (operating_client_test.go,
// app_admission_test.go).

// testAppLifetime is the app lifetime the shared fixtures run as: 16 bytes in the strict
// unpadded base64url the admission accepts (its last character can only be A, Q, g or w;
// codex-154). TestFixture_TheAppLifetimeIsOneTheParserAccepts holds it to that.
const testAppLifetime = "TTTTTTTTTTTTTTTTTTTTTA"

// testAppCredential is the credential the fixture app presents and the fixture server accepts.
const testAppCredential = "fixture-operating-credential"

// testAppPort stands for the app listener's port where no socket is bound. The app's Host
// must name it.
const testAppPort = 18443

func TestFixture_TheAppLifetimeIsOneTheParserAccepts(t *testing.T) {
	if !validAppLifetime(testAppLifetime) {
		t.Fatalf("testAppLifetime %q is not a lifetime the admission would accept", testAppLifetime)
	}
}

// ownFt8 makes f a service operated by testAppLifetime with its stream open. f must be
// unstarted and have no keyer, as the fixtures build it: the stream then opens no capture
// and no audio device. It is for a service assigned to a server after New, which the
// server's own wiring never saw; a service passed to New is published to by the admission
// (prepareApp).
func ownFt8(t testing.TB, f *ft8.Service) *ft8.Service {
	t.Helper()
	f.RequireOperatingLifetime()
	f.PublishOperating(testAppLifetime)
	openFt8Stream(t, f)
	return f
}

func openFt8Stream(t testing.TB, f *ft8.Service) {
	t.Helper()
	_, leave, err := f.SubscribeModeAs("", testAppLifetime)
	if err != nil {
		t.Fatalf("the fixture app's FT8 stream: %v", err)
	}
	t.Cleanup(leave)
}

// asAdmittedApp returns r carrying testAppLifetime as the operating-client check hands a
// validated lifetime to a handler (requireOperatingClient). For a handler called directly.
func asAdmittedApp(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), appLifetimeKey{}, testAppLifetime))
}

// checkOn is a config mutation for a test server: the operating-client check on, then the
// caller's own changes, so a fixture that means to run with it off can still say so.
func checkOn(mutate func(cfg *config.Config)) func(cfg *config.Config) {
	return func(cfg *config.Config) {
		cfg.Server.RequireDesktopApp = true
		if mutate != nil {
			mutate(cfg)
		}
	}
}

// prepareApp makes a check-on server one the fixture app can ask in process: it accepts
// the fixture's credential, has an app port, and has admitted the fixture's lifetime by
// the admission's own read and admit, which publishes it to an FT8 service passed to New.
// The app's FT8 stream is opened after that. Tests of the check and of the admission do
// not use it: they start from an empty admission on real listeners.
func prepareApp(t testing.TB, srv *Server) {
	t.Helper()
	if !srv.RequiresDesktopApp() {
		t.Fatal("prepareApp: the server was not built with the check on (use checkOn)")
	}
	srv.SetOperatingCredential(func(presented string) bool { return presented == testAppCredential })
	srv.appMu.Lock()
	if srv.appPort == 0 {
		srv.appPort = testAppPort
	}
	srv.appMu.Unlock()
	generation, current := srv.admission.read(testAppLifetime)
	if !current {
		if _, err := srv.admission.admit(testAppLifetime, processInstance, generation); err != nil {
			t.Fatalf("admit the fixture app: %v", err)
		}
	}
	if srv.ft8 != nil {
		openFt8Stream(t, srv.ft8)
	}
}

// asApp stamps r as a request the app's listener received from the fixture app: the
// listener's mark, TLS, the app listener's Host, one credential and the lifetime. It is for
// a test of what a served request DOES. A test of what the guards refuse (a foreign Host,
// an Origin, a second credential, no lifetime) builds its own request and does not use it:
// stamping would overwrite the very input under test (codex-154).
func asApp(srv *Server, r *http.Request) *http.Request {
	r = r.WithContext(context.WithValue(r.Context(), appConnKey{}, true))
	r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	r.Host = "127.0.0.1:" + strconv.Itoa(srv.AppPort())
	r.Header.Set(operatingCredentialHeader, testAppCredential)
	r.Header.Set(appLifetimeHeader, testAppLifetime)
	return r
}

// serveAsApp sends r through the server's whole handler chain as the fixture app.
func serveAsApp(srv *Server, w http.ResponseWriter, r *http.Request) {
	srv.httpServer.Handler.ServeHTTP(w, asApp(srv, r))
}

// appTestServer is testServerWithFt8 as the fixture app's daemon: the check on, the caller's
// config changes after it, and the app prepared (prepareApp).
func appTestServer(t *testing.T, mutate func(cfg *config.Config), ft8Svc *ft8.Service) *Server {
	t.Helper()
	srv := testServerWithFt8(t, checkOn(mutate), ft8Svc)
	prepareApp(t, srv)
	return srv
}

// appSocket serves srv's whole handler chain on a TLS socket whose connections count as the
// app listener's, and makes that socket's port the app port, so the Host a client sends by
// itself is the app's. It supplies nothing else: a client presents the credential and the
// lifetime itself (appHeaders), and a test of a refusal presents what it means to. The
// scheme is real TLS, which the WebSocket origin rule reads from the connection (codex-154).
// srv comes from appTestServer. tune, if given, can adjust the server before it starts.
func appSocket(t testing.TB, srv *Server, tune func(ts *httptest.Server)) *httptest.Server {
	t.Helper()
	chain := srv.httpServer.Handler
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chain.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), appConnKey{}, true)))
	}))
	if tune != nil {
		tune(ts)
	}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	srv.appMu.Lock()
	srv.appPort = ts.Listener.Addr().(*net.TCPAddr).Port
	srv.appMu.Unlock()
	return ts
}

// appSocketPlain is appSocket for the one kind of test that cannot have TLS on the wire: a
// connection double that must read the server's own plaintext writes sits directly under
// the HTTP server, and TLS would hand it ciphertext. The socket is plain TCP and each
// request's TLS state is modelled, as asApp models it in process. Nothing here tests TLS,
// the origin rule's scheme or the listener; those are appSocket's and the real listeners'.
func appSocketPlain(t testing.TB, srv *Server, tune func(ts *httptest.Server)) *httptest.Server {
	t.Helper()
	chain := srv.httpServer.Handler
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), appConnKey{}, true))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		chain.ServeHTTP(w, r)
	}))
	if tune != nil {
		tune(ts)
	}
	ts.Start()
	t.Cleanup(ts.Close)
	srv.appMu.Lock()
	srv.appPort = ts.Listener.Addr().(*net.TCPAddr).Port
	srv.appMu.Unlock()
	return ts
}

// appHeaders puts the fixture app's credential and lifetime on a request's headers.
func appHeaders(h http.Header) {
	h.Set(operatingCredentialHeader, testAppCredential)
	h.Set(appLifetimeHeader, testAppLifetime)
}

// appGet asks ts (an appSocket) for path as the fixture app.
func appGet(t testing.TB, ts *httptest.Server, path string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	appHeaders(req.Header)
	return ts.Client().Do(req)
}
