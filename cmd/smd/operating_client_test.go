package main

// W-0027 C2a: the daemon's side of the operating-client check. The credential is made at
// every start. With server.require_desktop_app on, an unusable credential, identity or app
// port ends the start with its reason; with it off, each is only reported, as before.

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/desktopid"
)

// fatals records what a start reported as fatal.
type fatals struct {
	mu   sync.Mutex
	errs []error
}

func (f *fatals) fatal(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, err)
}

func (f *fatals) all() []error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]error(nil), f.errs...)
}

type fakeCredentialServer struct{ matches func(string) bool }

func (f *fakeCredentialServer) SetOperatingCredential(m func(string) bool) { f.matches = m }

func privateRoot(t *testing.T) string {
	t.Helper()
	r := filepath.Join(t.TempDir(), "wd")
	if err := os.Mkdir(r, 0o700); err != nil {
		t.Fatal(err)
	}
	return r
}

func publishedCredential(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, desktopid.CredentialDirName, desktopid.CredentialFileName))
	if err != nil {
		t.Fatalf("no credential was published under %s: %v", root, err)
	}
	return string(b)
}

// The credential is created and handed to the server whether or not the check is on.
func TestOperatingCredential_IsMadeAndHandedOverInEitherState(t *testing.T) {
	for _, required := range []bool{false, true} {
		root := privateRoot(t)
		srv, r, f := &fakeCredentialServer{}, newAppReports(), &fatals{}
		loadOperatingCredential(srv, desktopid.EnsureCredential, root, required, r.report, f.fatal)
		value := publishedCredential(t, root)
		if srv.matches == nil || !srv.matches(value) || srv.matches("not-"+value) {
			t.Fatalf("required=%v: the server was not handed a test for exactly the published credential", required)
		}
		if got := r.all(); len(got) != 0 {
			t.Fatalf("required=%v: a clean start reported %+v", required, got)
		}
		if got := f.all(); len(got) != 0 {
			t.Fatalf("required=%v: a clean start was fatal: %v", required, got)
		}
	}
}

func credentialEnsureReturning(d desktopid.Durability, err error) func(string) (desktopid.Credential, desktopid.Durability, error) {
	return func(root string) (desktopid.Credential, desktopid.Durability, error) {
		if err != nil {
			return desktopid.Credential{}, d, err
		}
		c, _, cerr := desktopid.EnsureCredential(root)
		return c, d, cerr
	}
}

func TestOperatingCredential_UnconfirmedDurabilityIsReportedAndUsedInEitherState(t *testing.T) {
	for _, required := range []bool{false, true} {
		root := privateRoot(t)
		srv, r, f := &fakeCredentialServer{}, newAppReports(), &fatals{}
		loadOperatingCredential(srv, credentialEnsureReturning(desktopid.DurabilityUnconfirmed, nil), root, required, r.report, f.fatal)
		if srv.matches == nil || !srv.matches(publishedCredential(t, root)) {
			t.Fatalf("required=%v: an unconfirmed credential was not used", required)
		}
		if got := r.all(); len(got) != 1 || !strings.Contains(got[0].msg, "durab") {
			t.Fatalf("required=%v: reports = %+v; want one about unconfirmed durability", required, got)
		}
		if got := f.all(); len(got) != 0 {
			t.Fatalf("required=%v: unconfirmed durability was fatal: %v", required, got)
		}
	}
}

func TestOperatingCredential_Refused(t *testing.T) {
	refusal := errors.New("desktopid.EnsureCredential: /wd/desktop-credential/credential has mode 0644, want 0600")
	t.Run("check off: reported, the station starts", func(t *testing.T) {
		srv, r, f := &fakeCredentialServer{}, newAppReports(), &fatals{}
		loadOperatingCredential(srv, credentialEnsureReturning(desktopid.Durable, refusal), "/wd", false, r.report, f.fatal)
		if got := r.all(); len(got) != 1 || !strings.Contains(got[0].msg, "credential") || got[0].err != refusal {
			t.Fatalf("reports = %+v; want one naming the credential, carrying the refusal (which names the file)", got)
		}
		if got := f.all(); len(got) != 0 {
			t.Fatalf("a refused credential was fatal with the check off: %v", got)
		}
		if srv.matches != nil {
			t.Fatal("the server was handed a credential test although none was loaded")
		}
	})
	t.Run("check on: a start-up error", func(t *testing.T) {
		srv, r, f := &fakeCredentialServer{}, newAppReports(), &fatals{}
		loadOperatingCredential(srv, credentialEnsureReturning(desktopid.Durable, refusal), "/wd", true, r.report, f.fatal)
		got := f.all()
		if len(got) != 1 || !errors.Is(got[0], refusal) || !strings.Contains(got[0].Error(), "require_desktop_app") {
			t.Fatalf("fatal = %v; want one naming server.require_desktop_app and carrying the refusal", got)
		}
		if srv.matches != nil {
			t.Fatal("the server was handed a credential test although none was loaded")
		}
	})
}

// With the check on, the two ruled listener failures end the start (ADR 0103 point 2).
func TestDesktopListener_CheckOn_RefusedIdentityIsFatal(t *testing.T) {
	fk, r, f := newFake(), newAppReports(), &fatals{}
	refusal := errors.New("desktopid.Load: /wd/desktop-identity/key.pem has mode 0644, want 0600")
	startDesktopListener(fk, ensureReturning(desktopid.Durable, refusal), "/wd", 8443, r.report, f.fatal)
	got := f.all()
	if len(got) != 1 || !errors.Is(got[0], refusal) || !strings.Contains(got[0].Error(), "require_desktop_app") {
		t.Fatalf("fatal = %v; want one naming server.require_desktop_app and carrying the refusal", got)
	}
	if len(fk.bound) != 0 {
		t.Fatal("the app listener was bound despite a refused identity")
	}
}

func TestDesktopListener_CheckOn_BusyPortIsFatal(t *testing.T) {
	fk, r, f := newFake(), newAppReports(), &fatals{}
	fk.bindErr = errors.New("listen tcp 127.0.0.1:8443: bind: address already in use")
	startDesktopListener(fk, ensureReturning(desktopid.Durable, nil), "/wd", 8443, r.report, f.fatal)
	got := f.all()
	if len(got) != 1 || !errors.Is(got[0], fk.bindErr) || !strings.Contains(got[0].Error(), "8443") ||
		!strings.Contains(got[0].Error(), "require_desktop_app") {
		t.Fatalf("fatal = %v; want one naming port 8443 and server.require_desktop_app with the bind error", got)
	}
	select {
	case <-fk.served:
		t.Fatal("ServeApp ran after a failed bind")
	case <-time.After(100 * time.Millisecond):
	}
}

// Unconfirmed durability of the identity stays a report in both states, and the listener runs.
func TestDesktopListener_CheckOn_UnconfirmedDurabilityIsStillOnlyReported(t *testing.T) {
	fk, r, f := newFake(), newAppReports(), &fatals{}
	startDesktopListener(fk, ensureReturning(desktopid.DurabilityUnconfirmed, nil), "/wd", 8443, r.report, f.fatal)
	waitSignal(t, fk.served, "ServeApp")
	if got := f.all(); len(got) != 0 {
		t.Fatalf("unconfirmed durability was fatal: %v", got)
	}
	if got := r.all(); len(got) != 1 || !strings.Contains(got[0].msg, "durab") {
		t.Fatalf("reports = %+v; want one about unconfirmed durability", got)
	}
}

// --- through the real http node ---------------------------------------------------------

// pinnedGet GETs path from the app listener, accepting only id's certificate, and presents
// cred when it is not empty.
func pinnedGet(t *testing.T, port int, id desktopid.Identity, path, cred string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // replaced by the pin below, as in the shell
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 || !bytes.Equal(cs.PeerCertificates[0].Raw, id.CertDER) {
				return errors.New("not smd's desktop identity")
			}
			return nil
		},
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1:"+strconv.Itoa(port)+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cred != "" {
		req.Header.Set("X-SM-Operating-Credential", cred)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("pinned GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func checkedDaemonConfig(on bool) func(*config.Config) {
	return func(c *config.Config) {
		c.SetupComplete = true
		c.DefaultLogbookID = 1
		c.Server.RequireDesktopApp = on
	}
}

func TestOperatingClient_TheHTTPNodeEnforcesTheCheckWithThisInstallsCredential(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, checkedDaemonConfig(true))
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	port := d.server.AppPort()
	if port == 0 {
		t.Fatal("no app listener")
	}
	id, err := desktopid.Load(d.cfgSvc.WorkingDir())
	if err != nil {
		t.Fatal(err)
	}
	cred := publishedCredential(t, d.cfgSvc.WorkingDir())

	if code, body := pinnedGet(t, port, id, "/v1/version", cred); code != http.StatusOK {
		t.Fatalf("the app listener with this install's credential: %d %s; want 200", code, body)
	}
	if code, body := pinnedGet(t, port, id, "/v1/version", ""); code != http.StatusForbidden || !strings.Contains(body, "desktop_app_required") {
		t.Fatalf("the app listener without the credential: %d %s; want 403 desktop_app_required", code, body)
	}
	if code, body := pinnedGet(t, port, id, "/v1/version", strings.Repeat("W", 43)); code != http.StatusForbidden {
		t.Fatalf("the app listener with another credential: %d %s; want 403", code, body)
	}
	// The main listener (a Unix socket here, refused as TCP is) serves the health check only.
	if code, body := apiCall(t, d, http.MethodGet, "/v1/version"); code != http.StatusForbidden || !strings.Contains(string(body), "desktop_app_required") {
		t.Fatalf("the main listener: version = %d %s; want 403 desktop_app_required", code, body)
	}
	if code, body := apiCall(t, d, http.MethodGet, "/v1/healthz"); code != http.StatusOK {
		t.Fatalf("the main listener: healthz = %d %s; want 200", code, body)
	}
	select {
	case err := <-d.errCh:
		t.Fatalf("a clean start with the check on reached errCh: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	stopGen(t, orch)
}

// Check off: the credential is still made at start, and nothing asks for it.
func TestOperatingClient_OffTheCredentialIsStillMadeAndNothingAsksForIt(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, checkedDaemonConfig(false))
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := desktopid.LoadCredential(d.cfgSvc.WorkingDir()); err != nil {
		t.Fatalf("no usable credential after a start with the check off: %v", err)
	}
	id, err := desktopid.Load(d.cfgSvc.WorkingDir())
	if err != nil {
		t.Fatal(err)
	}
	if code, body := pinnedGet(t, d.server.AppPort(), id, "/v1/version", ""); code != http.StatusOK {
		t.Fatalf("the app listener without a credential, check off: %d %s; want 200", code, body)
	}
	if code, body := apiCall(t, d, http.MethodGet, "/v1/version"); code != http.StatusOK {
		t.Fatalf("the main listener, check off: %d %s; want 200", code, body)
	}
	stopGen(t, orch)
}

// wantFatal waits for the start-up error on the daemon's fatal channel.
func wantFatal(t *testing.T, d *daemon, contains ...string) {
	t.Helper()
	select {
	case err := <-d.errCh:
		if err == nil {
			t.Fatal("errCh carried no error")
		}
		for _, c := range contains {
			if !strings.Contains(err.Error(), c) {
				t.Fatalf("start-up error %q does not name %q", err, c)
			}
		}
		t.Logf("fatal: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the start-up error never reached the fatal channel")
	}
}

func TestOperatingClient_CheckOn_ABusyAppPortEndsTheStart(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		checkedDaemonConfig(true)(c)
		c.Server.AppPort = port
	})
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	wantFatal(t, d, strconv.Itoa(port), "require_desktop_app")
	stopGen(t, orch)
}

func TestOperatingClient_CheckOn_ARefusedIdentityEndsTheStart(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, checkedDaemonConfig(true))
	ownerOnlySocketDir(t, d)
	if _, _, err := desktopid.Ensure(d.cfgSvc.WorkingDir()); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(d.cfgSvc.WorkingDir(), desktopid.DirName, desktopid.KeyName)
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	wantFatal(t, d, key, "require_desktop_app")
	if got := d.server.AppPort(); got != 0 {
		t.Fatalf("an app listener started (port %d) on a refused identity", got)
	}
	stopGen(t, orch)
}

// A refused credential with the check on ends the start, and until the daemon is down the
// server refuses everything but the health check: it never serves without a credential.
func TestOperatingClient_CheckOn_ARefusedCredentialEndsTheStartAndServesNothing(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, checkedDaemonConfig(true))
	ownerOnlySocketDir(t, d)
	root := d.cfgSvc.WorkingDir()
	if _, _, err := desktopid.EnsureCredential(root); err != nil {
		t.Fatal(err)
	}
	cred := publishedCredential(t, root)
	file := filepath.Join(root, desktopid.CredentialDirName, desktopid.CredentialFileName)
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	if code, body := apiCall(t, d, http.MethodGet, "/v1/version"); code != http.StatusForbidden {
		t.Fatalf("the main listener: version = %d %s; want 403", code, body)
	}
	if port := d.server.AppPort(); port != 0 {
		id, err := desktopid.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if code, body := pinnedGet(t, port, id, "/v1/version", cred); code != http.StatusForbidden {
			t.Fatalf("the app listener served the refused credential's value: %d %s", code, body)
		}
	}
	wantFatal(t, d, file, "require_desktop_app")
	stopGen(t, orch)
}

// Check off: a refused credential never stops the station.
func TestOperatingClient_CheckOff_ARefusedCredentialNeverStopsTheStation(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, checkedDaemonConfig(false))
	ownerOnlySocketDir(t, d)
	root := d.cfgSvc.WorkingDir()
	if _, _, err := desktopid.EnsureCredential(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, desktopid.CredentialDirName, desktopid.CredentialFileName), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("a refused credential stopped the station with the check off: %v", err)
	}
	if code, body := apiCall(t, d, http.MethodGet, "/v1/version"); code != http.StatusOK {
		t.Fatalf("the main listener: version = %d %s; want 200", code, body)
	}
	select {
	case err := <-d.errCh:
		t.Fatalf("a refused credential reached errCh with the check off: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	stopGen(t, orch)
}

// With the check on, the app listener is the only way in. If it stops with an error after
// the start, the daemon ends, so its service manager restarts it (operator ruling
// 2026-11-13: "yes, end the daemon"). With the check off it stays a report
// (TestDesktopListener_ServeError_ReportedNotFatal).
func TestDesktopListener_CheckOn_AServeErrorEndsTheDaemon(t *testing.T) {
	fk, r := newFake(), newAppReports()
	fk.serveErr = errors.New("api.ServeApp: accept failure")
	got := make(chan error, 1)
	startDesktopListener(fk, ensureReturning(desktopid.Durable, nil), "/wd", 8443, r.report, func(err error) { got <- err })
	select {
	case err := <-got:
		if !errors.Is(err, fk.serveErr) || !strings.Contains(err.Error(), "require_desktop_app") {
			t.Fatalf("fatal = %v; want one naming server.require_desktop_app and carrying the serve error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("a serve error with the check on never reached the fatal path; reports = %+v", r.all())
	}
}

// A clean stop of the app listener is not an error in either state.
func TestDesktopListener_CheckOn_ACleanStopIsNotFatal(t *testing.T) {
	fk, r, f := newFake(), newAppReports(), &fatals{}
	startDesktopListener(fk, ensureReturning(desktopid.Durable, nil), "/wd", 8443, r.report, f.fatal)
	waitSignal(t, fk.served, "ServeApp")
	time.Sleep(100 * time.Millisecond) // ServeApp has returned nil
	if got := f.all(); len(got) != 0 {
		t.Fatalf("a clean stop was fatal: %v", got)
	}
	if got := r.all(); len(got) != 0 {
		t.Fatalf("a clean stop was reported: %+v", got)
	}
}
