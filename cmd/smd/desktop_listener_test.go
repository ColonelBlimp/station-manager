package main

// W-0027 C1c: starting the desktop app's listener never stops the station before C2
// (operator rulings 2026-11-10 and 2026-11-11). Each failure is reported (smd.log) and smd
// carries on serving the browser.

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/desktopid"
)

type fakeAppListener struct {
	mu       sync.Mutex
	bindErr  error
	serveErr error
	bound    []int
	served   chan struct{}
}

func newFake() *fakeAppListener { return &fakeAppListener{served: make(chan struct{}, 1)} }

func (f *fakeAppListener) BindApp(port int, _ tls.Certificate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bound = append(f.bound, port)
	return f.bindErr
}

func (f *fakeAppListener) ServeApp() error {
	f.served <- struct{}{}
	return f.serveErr
}

type appReport struct {
	msg string
	err error
}

type appReports struct {
	mu   sync.Mutex
	logs []appReport
	done chan struct{}
}

func newAppReports() *appReports { return &appReports{done: make(chan struct{}, 4)} }

func (r *appReports) report(msg string, err error) {
	r.mu.Lock()
	r.logs = append(r.logs, appReport{msg, err})
	r.mu.Unlock()
	r.done <- struct{}{}
}

func (r *appReports) all() []appReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]appReport(nil), r.logs...)
}

func ensureReturning(d desktopid.Durability, err error) func(string) (desktopid.Identity, desktopid.Durability, error) {
	return func(string) (desktopid.Identity, desktopid.Durability, error) {
		return desktopid.Identity{}, d, err
	}
}

func waitSignal(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestDesktopListener_StartsAndServes(t *testing.T) {
	f, r := newFake(), newAppReports()
	startDesktopListener(f, ensureReturning(desktopid.Durable, nil), "/wd", 8443, r.report, nil)
	waitSignal(t, f.served, "ServeApp")
	if len(f.bound) != 1 || f.bound[0] != 8443 {
		t.Fatalf("bound = %v, want [8443]", f.bound)
	}
	if got := r.all(); len(got) != 0 {
		t.Fatalf("a clean start reported %+v", got)
	}
}

func TestDesktopListener_RefusedIdentity_NoListenerReported(t *testing.T) {
	f, r := newFake(), newAppReports()
	refusal := errors.New("desktopid.Load: /wd/desktop-identity/key.pem has mode 0644, want 0600")
	startDesktopListener(f, ensureReturning(desktopid.Durable, refusal), "/wd", 8443, r.report, nil)
	got := r.all()
	if len(got) != 1 || !strings.Contains(got[0].msg, "identity") || got[0].err != refusal {
		t.Fatalf("reports = %+v; want one naming the identity, carrying the refusal (which names the file)", got)
	}
	if len(f.bound) != 0 {
		t.Fatal("the app listener was bound despite a refused identity")
	}
}

func TestDesktopListener_UnconfirmedDurability_ReportedAndServes(t *testing.T) {
	f, r := newFake(), newAppReports()
	startDesktopListener(f, ensureReturning(desktopid.DurabilityUnconfirmed, nil), "/wd", 8443, r.report, nil)
	waitSignal(t, f.served, "ServeApp")
	got := r.all()
	if len(got) != 1 || !strings.Contains(got[0].msg, "durab") {
		t.Fatalf("reports = %+v; want one about unconfirmed durability", got)
	}
}

func TestDesktopListener_BusyPort_ReportedNoServe(t *testing.T) {
	f, r := newFake(), newAppReports()
	f.bindErr = errors.New("listen tcp 127.0.0.1:8443: bind: address already in use")
	startDesktopListener(f, ensureReturning(desktopid.Durable, nil), "/wd", 8443, r.report, nil)
	got := r.all()
	if len(got) != 1 || !strings.Contains(got[0].msg, "8443") || got[0].err != f.bindErr {
		t.Fatalf("reports = %+v; want one naming port 8443 with the bind error", got)
	}
	select {
	case <-f.served:
		t.Fatal("ServeApp ran after a failed bind")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDesktopListener_ServeError_ReportedNotFatal(t *testing.T) {
	f, r := newFake(), newAppReports()
	f.serveErr = errors.New("api.ServeApp: accept failure")
	startDesktopListener(f, ensureReturning(desktopid.Durable, nil), "/wd", 8443, r.report, nil)
	waitSignal(t, r.done, "the serve error report")
	got := r.all()
	if len(got) != 1 || got[0].err != f.serveErr {
		t.Fatalf("reports = %+v; want the serve error reported", got)
	}
}

// pinnedVersion GETs /v1/version from the app listener on port, accepting only want's
// certificate (the shell's pin, ADR 0103).
func pinnedVersion(t *testing.T, port int, want desktopid.Identity) (int, error) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // replaced by the pin below, as in the shell
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 || !bytes.Equal(cs.PeerCertificates[0].Raw, want.CertDER) {
				return errors.New("not smd's desktop identity")
			}
			return nil
		},
	}}}
	defer client.CloseIdleConnections()
	resp, err := client.Get("https://127.0.0.1:" + strconv.Itoa(port) + "/v1/version")
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func TestDesktopListener_TheHTTPNodeServesTheAppWithItsIdentity(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) { c.SetupComplete = true; c.DefaultLogbookID = 1 })
	ownerOnlySocketDir(t, d)
	port := d.cfgSvc.Snapshot().Server.EffectiveAppPort()
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	if got := d.server.AppPort(); got != port {
		t.Fatalf("app listener port = %d, want the configured %d", got, port)
	}
	id, err := desktopid.Load(d.cfgSvc.WorkingDir())
	if err != nil {
		t.Fatalf("the http node did not publish the desktop identity: %v", err)
	}
	if code, err := pinnedVersion(t, port, id); err != nil || code != http.StatusOK {
		t.Fatalf("pinned GET /v1/version = %d, %v; want 200", code, err)
	}
	stopGen(t, orch) // strict: the http node drains both listeners
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second); err == nil {
		t.Fatal("the app listener still accepts after the generation stopped")
	}
}

func TestDesktopListener_ABusyAppPortNeverStopsTheStation(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.Server.AppPort = port
	})
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("a busy app port stopped the station: %v", err)
	}
	if code, body := apiCall(t, d, http.MethodGet, "/v1/version"); code != http.StatusOK {
		t.Fatalf("browser listener: version = %d %s", code, body)
	}
	if got := d.server.AppPort(); got != 0 {
		t.Fatalf("app listener port = %d, want none", got)
	}
	select {
	case err := <-d.errCh:
		t.Fatalf("a busy app port reached errCh: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	stopGen(t, orch)
}

// A failed browser-listener bind stays fatal, as today, and no app listener starts to outlive it
// (the app listener binds only after the browser's bind succeeds, codex-058).
func TestDesktopListener_AFailedBrowserBindLeavesNoAppListener(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
	})
	ownerOnlySocketDir(t, d)
	sock := d.cfgSvc.Snapshot().SocketPath
	if err := os.WriteFile(sock, []byte("not a socket"), 0o600); err != nil { // the bind refuses a regular file
		t.Fatal(err)
	}
	port := d.cfgSvc.Snapshot().Server.EffectiveAppPort()
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-d.errCh:
		if err == nil || !strings.Contains(err.Error(), sock) {
			t.Fatalf("errCh = %v; want the browser listener's bind failure on %s", err, sock)
		}
		t.Logf("fatal, as today: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("a failed browser bind did not reach the fatal channel")
	}
	if got := d.server.AppPort(); got != 0 {
		t.Fatalf("an app listener started (port %d) although the browser bind failed", got)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("the app port is held although the browser bind failed: %v", err)
	}
	_ = ln.Close()
	stopGen(t, orch)
}

type fakeBrowserBind struct {
	attempted chan struct{}
	listening bool
}

func (f fakeBrowserBind) BindAttempted() <-chan struct{} { return f.attempted }
func (f fakeBrowserBind) Listening() bool                { return f.listening }

func TestAfterBrowserBind(t *testing.T) {
	done := make(chan struct{})
	close(done)
	for _, c := range []struct {
		name      string
		bind      fakeBrowserBind
		cancelled bool
		want      bool
		reports   int
	}{
		{"the browser listener is bound", fakeBrowserBind{done, true}, false, true, 0},
		// Its failure is already on errCh, fatal as today; no app listener to outlive it.
		{"the browser bind failed", fakeBrowserBind{done, false}, false, false, 0},
		{"start-up cancelled before the browser bind", fakeBrowserBind{make(chan struct{}), false}, true, false, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.cancelled {
				cancel()
			}
			r := newAppReports()
			if got := afterBrowserBind(ctx, c.bind, r.report); got != c.want {
				t.Fatalf("afterBrowserBind = %v, want %v", got, c.want)
			}
			if got := r.all(); len(got) != c.reports {
				t.Fatalf("reports = %+v, want %d", got, c.reports)
			}
		})
	}
}

// codex-058 P1: with the app port equal to the browser listener's (an accepted warning), the
// app listener must never win the port. The browser's bind is held back for as long as the app
// listener might bind first; the station must still start and serve the browser.
func TestDesktopListener_NeverTakesTheBrowserListenersPort(t *testing.T) {
	port := freeLoopbackPort(t)
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.Server.Protocol = "tcp"
		c.SocketPath = "127.0.0.1:" + strconv.Itoa(port)
		c.Server.AppPort = port
	})
	real := serveBrowser
	t.Cleanup(func() { serveBrowser = real })
	serveBrowser = func(d *daemon) error {
		// The app-first schedule: wait until the app listener has bound, or 500 ms.
		for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline) && d.server.AppPort() == 0; {
			time.Sleep(5 * time.Millisecond)
		}
		return real(d)
	}
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("the station did not start: %v", err)
	}
	select {
	case err := <-d.errCh:
		t.Fatalf("the browser listener failed (fatal): %v", err)
	case <-time.After(700 * time.Millisecond):
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/v1/version")
	if err != nil {
		t.Fatalf("the browser listener does not serve: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("browser /v1/version = %d, want 200", resp.StatusCode)
	}
	if got := d.server.AppPort(); got != 0 {
		t.Fatalf("the app listener holds port %d", got)
	}
	stopGen(t, orch)
}

// fakeStation is a browser listener plus the app listener's server, for the start decision.
type fakeStation struct {
	*fakeAppListener
	fakeBrowserBind
}

// codex-058/059: a cancelled start-up never starts the app listener, even when the browser bind
// has also completed (a select with both ready picks either), and not when cancelled mid-wait.
func TestStartAppAfterBrowser_ACancelledStartNeverBindsTheApp(t *testing.T) {
	done := make(chan struct{})
	close(done)
	for i := 0; i < 256; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f := fakeStation{newFake(), fakeBrowserBind{done, true}}
		startAppAfterBrowser(ctx, f, ensureReturning(desktopid.Durable, nil), "/wd", 8443, newAppReports().report, nil)
		if len(f.bound) != 0 {
			t.Fatalf("call %d: the app listener was bound although start-up was already cancelled", i)
		}
	}

	pending := make(chan struct{})
	f := fakeStation{newFake(), fakeBrowserBind{pending, true}}
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() {
		startAppAfterBrowser(ctx, f, ensureReturning(desktopid.Durable, nil), "/wd", 8443, newAppReports().report, nil)
		close(returned)
	}()
	cancel()
	waitSignal(t, returned, "the start decision after cancellation")
	close(pending) // the browser bind completes afterwards
	if len(f.bound) != 0 {
		t.Fatal("the app listener was bound although start-up was cancelled during the wait")
	}
}

func TestStartAppAfterBrowser_BindsOnlyAfterTheBrowser(t *testing.T) {
	done := make(chan struct{})
	close(done)
	f := fakeStation{newFake(), fakeBrowserBind{done, true}}
	startAppAfterBrowser(context.Background(), f, ensureReturning(desktopid.Durable, nil), "/wd", 8443, newAppReports().report, nil)
	waitSignal(t, f.served, "ServeApp")
	if len(f.bound) != 1 {
		t.Fatalf("bound = %v, want one bind after the browser's", f.bound)
	}
}
