package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// App admission (W-0027 C4a). With the check on, the desktop app's main process tells smd
// its lifetime before it loads the workspace: a read, then an admit against the
// generation it read. Only the admitted lifetime operates FT8.

const (
	// Sixteen bytes as base64url: 22 characters, the last carrying only two bits, so it
	// is A, Q, g or w. Any other last character is another spelling and is refused.
	lifeA = "AAAAAAAAAAAAAAAAAAAAAA"
	lifeB = "BBBBBBBBBBBBBBBBBBBBBA"
	lifeC = "CCCCCCCCCCCCCCCCCCCCCQ"
	lifeD = "DDDDDDDDDDDDDDDDDDDDDg"
)

type admissionState struct {
	Required   bool   `json:"required"`
	Instance   string `json:"daemon_instance"`
	Generation uint64 `json:"generation"`
	Current    bool   `json:"current"`
}

// appDo sends one credentialed request on the app listener, as a lifetime ("" for none).
func (l checked) appDo(t *testing.T, c *http.Client, lifetime, method, path, body string) (int, []byte, http.Header) {
	t.Helper()
	r := l.acceptable(t, true, method, path, l.cred)
	if lifetime != "" {
		r.Header.Set(appLifetimeHeader, lifetime)
	}
	// The default logbook the live routes ask a page to confirm, so that guard passes
	// and an FT8 command reaches the scope.
	r.Header.Set(defaultLogbookHeader, ensureDefaultLogbook(t, l.srv))
	if body != "" {
		r.Body = io.NopCloser(strings.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("%s %s as %q: %v", method, path, lifetime, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, b, resp.Header
}

func (l checked) readAdmission(t *testing.T, c *http.Client, lifetime string) admissionState {
	t.Helper()
	status, body, _ := l.appDo(t, c, lifetime, http.MethodGet, "/v1/app/lifetime", "")
	if status != http.StatusOK {
		t.Fatalf("read as %q: %d %s", lifetime, status, body)
	}
	var st admissionState
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("read as %q: %v in %s", lifetime, err, body)
	}
	return st
}

// admit sends the admit for what was read, and returns the status and the error code.
func (l checked) admit(t *testing.T, c *http.Client, lifetime string, read admissionState) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"daemon_instance":%q,"generation":%d}`, read.Instance, read.Generation)
	status, resp, _ := l.appDo(t, c, lifetime, http.MethodPost, "/v1/app/lifetime", body)
	if status == http.StatusOK {
		return status, ""
	}
	return status, errorCode(t, resp)
}

// mustAdmit reads and admits, and fails the test if that is refused.
func (l checked) mustAdmit(t *testing.T, c *http.Client, lifetime string) {
	t.Helper()
	if status, code := l.admit(t, c, lifetime, l.readAdmission(t, c, lifetime)); status != http.StatusOK {
		t.Fatalf("admit %q: %d %s", lifetime, status, code)
	}
}

func TestAppAdmission_ReadThenAdmit(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)

	first := l.readAdmission(t, c, lifeA)
	if !first.Required || first.Instance != processInstance || first.Generation != 0 || first.Current {
		t.Fatalf("the first read: %+v; want required, this process, generation 0, not current", first)
	}
	if status, code := l.admit(t, c, lifeA, first); status != http.StatusOK {
		t.Fatalf("the first admit: %d %s", status, code)
	}
	if st := l.readAdmission(t, c, lifeA); !st.Current || st.Generation != 1 {
		t.Fatalf("after the first admit: %+v; want current at generation 1", st)
	}
	if st := l.readAdmission(t, c, lifeB); st.Current || st.Generation != 1 {
		t.Fatalf("another lifetime's read: %+v; want not current at generation 1", st)
	}
	if st := l.readAdmission(t, c, ""); st.Current {
		t.Fatalf("a read with no lifetime is current: %+v", st)
	}

	// A repeat, or a retry after a lost answer, with what was read the first time: the
	// caller is current already, so it succeeds and nothing moves.
	if status, code := l.admit(t, c, lifeA, first); status != http.StatusOK {
		t.Fatalf("a repeated admit of the current lifetime: %d %s", status, code)
	}
	if st := l.readAdmission(t, c, lifeA); !st.Current || st.Generation != 1 {
		t.Fatalf("a repeated admit moved the generation: %+v", st)
	}
}

// codex-125 finding 1 and codex-126 finding 1: a replaced lifetime never comes back,
// whether by a late duplicate, a retry of an admit whose answer was lost, or a fresh
// read; and the current app is not touched by the attempt.
func TestAppAdmission_AReplacedLifetimeIsRetiredForGood(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)

	stale := l.readAdmission(t, c, lifeA)
	l.mustAdmit(t, c, lifeA)
	l.mustAdmit(t, c, lifeB) // B replaces A

	for name, read := range map[string]admissionState{
		"a late duplicate, with what A first read": stale,
		"a fresh read by A":                        l.readAdmission(t, c, lifeA),
	} {
		status, code := l.admit(t, c, lifeA, read)
		if status != http.StatusConflict || code != "app_lifetime_retired" {
			t.Errorf("%s: %d %s; want 409 app_lifetime_retired", name, status, code)
		}
		if st := l.readAdmission(t, c, lifeB); !st.Current || st.Generation != 2 {
			t.Errorf("%s: the current app was disturbed: %+v", name, st)
		}
	}
}

// A lifetime that has never been admitted and reads before another's admit is stale, and
// may read again: that is how the running app gets past a dead app's late admission.
func TestAppAdmission_AStaleAdmitIsRefusedAndMayReadAgain(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)
	l.mustAdmit(t, c, lifeA)

	readC := l.readAdmission(t, c, lifeC)
	l.mustAdmit(t, c, lifeD) // lands between C's read and C's admit
	status, code := l.admit(t, c, lifeC, readC)
	if status != http.StatusConflict || code != "app_admission_stale" {
		t.Fatalf("C's admit against an old generation: %d %s; want 409 app_admission_stale", status, code)
	}
	if st := l.readAdmission(t, c, lifeD); !st.Current {
		t.Fatalf("a stale admit disturbed the current app: %+v", st)
	}

	l.mustAdmit(t, c, lifeC) // read again, admit again
	if st := l.readAdmission(t, c, lifeC); !st.Current || st.Generation != 3 {
		t.Fatalf("after C read again: %+v; want current at generation 3", st)
	}
	if status, code := l.admit(t, c, lifeD, l.readAdmission(t, c, lifeD)); code != "app_lifetime_retired" {
		t.Fatalf("D, replaced by C, asks again: %d %s; want app_lifetime_retired", status, code)
	}
}

// A marker from another daemon process is stale whatever the generation.
func TestAppAdmission_AnotherProcessMarkerIsStale(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)
	read := l.readAdmission(t, c, lifeA)
	read.Instance = "an-earlier-process"
	if status, code := l.admit(t, c, lifeA, read); status != http.StatusConflict || code != "app_admission_stale" {
		t.Fatalf("an admit carrying another process's marker: %d %s; want 409 app_admission_stale", status, code)
	}
	if st := l.readAdmission(t, c, lifeA); st.Current || st.Generation != 0 {
		t.Fatalf("a stale admit took effect: %+v", st)
	}
}

// Two shells that read the same generation: exactly one replaces it.
func TestAppAdmission_OneAdmitWinsAGeneration(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)
	lifetimes := []string{lifeA, lifeB, lifeC, lifeD}
	reads := make([]admissionState, len(lifetimes))
	for i, lt := range lifetimes {
		reads[i] = l.readAdmission(t, c, lt)
	}
	statuses := make([]int, len(lifetimes))
	var wg sync.WaitGroup
	for i, lt := range lifetimes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i], _ = l.admit(t, c, lt, reads[i])
		}()
	}
	wg.Wait()
	won := 0
	for _, s := range statuses {
		if s == http.StatusOK {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d of %d admits against one generation succeeded; want exactly 1 (%v)", won, len(lifetimes), statuses)
	}
}

func TestAppAdmission_TheIdentityMustBeWellFormed(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)

	for name, bad := range map[string]string{
		"too short":            "AAAA",
		"too long":             lifeA + "A",
		"not base64url":        "AAAAAAAAAAAAAAAAAAAA+/",
		"padded":               "AAAAAAAAAAAAAAAAAAAA==",
		"another spelling":     "BBBBBBBBBBBBBBBBBBBBBB", // stray low bits in the last character
		"fifteen bytes' worth": "AAAAAAAAAAAAAAAAAAAA",
	} {
		status, body, _ := l.appDo(t, c, bad, http.MethodGet, "/v1/version", "")
		if status != http.StatusBadRequest || errorCode(t, body) != "invalid_app_lifetime" {
			t.Errorf("%s: %d %s; want 400 invalid_app_lifetime", name, status, body)
		}
	}

	// Two headers are refused, as two credentials are.
	r := l.acceptable(t, true, http.MethodGet, "/v1/version", l.cred)
	r.Header.Add(appLifetimeHeader, lifeA)
	r.Header.Add(appLifetimeHeader, lifeA)
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_app_lifetime" {
		t.Errorf("two lifetime headers: %d %s; want 400 invalid_app_lifetime", resp.StatusCode, body)
	}

	// An admit needs an identity.
	read := l.readAdmission(t, c, "")
	if status, code := l.admit(t, c, "", read); status != http.StatusBadRequest || code != "app_lifetime_required" {
		t.Errorf("an admit with no lifetime: %d %s; want 400 app_lifetime_required", status, code)
	}
}

// The daemon's process marker rides on what it serves to the app, so the app's main
// process can see a restart; and a served request is never handed the lifetime header.
func TestAppAdmission_TheMarkerIsOnResponsesToTheApp(t *testing.T) {
	l := startChecked(t, true)
	mainC, c := l.clients(t)

	_, _, h := l.appDo(t, c, lifeA, http.MethodGet, "/v1/version", "")
	if got := h.Get(daemonInstanceHeader); got != processInstance {
		t.Errorf("a served request on the app listener carries marker %q; want %q", got, processInstance)
	}
	// Beside it, the admission generation, so an app that has been replaced in this same
	// process learns of it from its next response (codex-129, finding 2).
	gen := func(lifetime string) string {
		_, _, h := l.appDo(t, c, lifetime, http.MethodGet, "/v1/version", "")
		return h.Get(admissionGenerationHeader)
	}
	if got := gen(lifeA); got != "0" {
		t.Errorf("before any admission the generation header is %q; want 0", got)
	}
	l.mustAdmit(t, c, lifeA)
	if got := gen(lifeA); got != "1" {
		t.Errorf("after the first admission the generation header is %q; want 1", got)
	}
	l.mustAdmit(t, c, lifeB)
	if got := gen(lifeA); got != "2" {
		t.Errorf("a replaced app's next response carries generation %q; want 2", got)
	}
	// Not on a refusal, and not on the main listener.
	r := l.acceptable(t, true, http.MethodGet, "/v1/version")
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := resp.Header.Get(daemonInstanceHeader); got != "" {
		t.Errorf("a refusal carries the marker %q", got)
	}
	if got := resp.Header.Get(admissionGenerationHeader); got != "" {
		t.Errorf("a refusal carries the generation %q", got)
	}
	resp, err = mainC.Get(l.mainURL("/v1/healthz"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := resp.Header.Get(daemonInstanceHeader); got != "" {
		t.Errorf("the main listener's health check carries the marker %q", got)
	}
}

// With the check off the header and admission do nothing.
func TestAppAdmission_OffDoesNothing(t *testing.T) {
	l := startChecked(t, false)
	_, c := l.clients(t)

	st := l.readAdmission(t, c, lifeA)
	if st.Required || st.Current {
		t.Fatalf("with the check off the read says %+v; want not required and not current", st)
	}
	if status, code := l.admit(t, c, lifeA, st); status != http.StatusOK {
		t.Fatalf("with the check off an admit is refused: %d %s", status, code)
	}
	if st := l.readAdmission(t, c, lifeA); st.Current || st.Generation != 0 {
		t.Fatalf("with the check off an admit took effect: %+v", st)
	}
	// A malformed identity is ignored, and FT8 asks nobody who they are.
	if status, body, _ := l.appDo(t, c, "junk", http.MethodGet, "/v1/version", ""); status != http.StatusOK {
		t.Fatalf("with the check off a malformed lifetime is refused: %d %s", status, body)
	}
	status, body, _ := l.appDo(t, c, "", http.MethodPost, "/v1/ft8/qso/next", `{}`)
	if code := errorCode(t, body); code == "app_lifetime_not_current" {
		t.Fatalf("with the check off an FT8 command asked for a lifetime: %d %s", status, code)
	}
}

// recordingScope stands for a scope at a replacement and writes down the order of events.
type recordingScope struct {
	mu     sync.Mutex
	events []string
}

func (s *recordingScope) RetireOperating() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "retire")
}

func (s *recordingScope) PublishOperating(lifetime string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "publish "+lifetime)
}

// A replacement stops every scope before it publishes, and only a replacement does.
func TestAppAdmission_AReplacementRetiresThenPublishesEveryScope(t *testing.T) {
	a := &appAdmission{}
	one, two := &recordingScope{}, &recordingScope{}
	a.scopes = []operatingScope{one, two}

	if _, err := a.admit(lifeA, processInstance, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.admit(lifeA, processInstance, 0); err != nil { // a repeat
		t.Fatal(err)
	}
	if _, err := a.admit(lifeB, processInstance, 0); err == nil { // stale
		t.Fatal("a stale admit succeeded")
	}
	if _, err := a.admit(lifeB, processInstance, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := a.admit(lifeA, processInstance, 2); err == nil { // retired
		t.Fatal("a retired lifetime was admitted")
	}
	want := []string{"retire", "publish " + lifeA, "retire", "publish " + lifeB}
	for i, s := range []*recordingScope{one, two} {
		if strings.Join(s.events, ",") != strings.Join(want, ",") {
			t.Errorf("scope %d saw %v; want %v", i, s.events, want)
		}
	}
}

// ft8Bodies is a request body, for each FT8 command route, that the handler's own
// validation accepts, so the request reaches the scope. Keyed by the declared pattern.
var ft8Bodies = map[string]string{
	"POST /v1/ft8/tx/arm":        `{"armed":true}`,
	"POST /v1/ft8/tx/send":       `{"message":"CQ G0XYZ IO91","offset_hz":1500}`,
	"POST /v1/ft8/qso/start":     `{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`,
	"POST /v1/ft8/qso/work":      `{"their_call":"K1ABC","their_grid":"FN42","their_snr":-10,"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`,
	"POST /v1/ft8/qso/path":      `{"path":"long"}`,
	"POST /v1/ft8/qso/abandon":   `{}`,
	"POST /v1/ft8/autowork/stop": `{}`,
	"POST /v1/ft8/qso/skip":      `{"armed":true}`,
	"POST /v1/ft8/qso/next":      `{}`,
	"POST /v1/ft8/cq/start":      `{"offset_hz":1500,"operating_freq_mhz":14.074}`,
	"POST /v1/ft8/cq/pick":       `{"call":"K1ABC"}`,
	"POST /v1/ft8/cq/bag":        `{"call":"K1ABC"}`,
	"POST /v1/ft8/cq/unbag":      `{"call":"K1ABC"}`,
	"POST /v1/ft8/cq/resume":     `{}`,
}

const notCurrent = "app_lifetime_not_current"

// The sweep: every FT8 command route but the two that are not gated carries the caller's
// lifetime to the scope. Each is refused for a caller with no lifetime (a tool, or a
// shell from before C4), for a lifetime that was never admitted, and for one that has
// been replaced; and not refused for that reason when the current app asks.
func TestAppAdmission_EveryFt8CommandNeedsTheCurrentLifetime(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)
	// A station callsign, so the starts get past their own validation and reach the scope.
	if _, err := l.srv.cfg.Update(func(c *config.Config) error {
		c.LoggingStation.StationCallsign = "G0XYZ"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	l.mustAdmit(t, c, lifeA)
	l.mustAdmit(t, c, lifeB) // A is replaced; B is current

	var ft8Routes []string
	for _, p := range l.srv.DeclaredRoutes() {
		if strings.HasPrefix(p, "POST /v1/ft8/") {
			ft8Routes = append(ft8Routes, p)
		}
	}
	if len(ft8Routes) != 15 {
		t.Fatalf("%d FT8 command routes are declared; this sweep was written for 15: %v", len(ft8Routes), ft8Routes)
	}

	for _, pattern := range ft8Routes {
		if pattern == "POST /v1/ft8/claim" {
			continue // not gated: see TestAppAdmission_TheClaimAndTheDisarmAreNotGated
		}
		body, ok := ft8Bodies[pattern]
		if !ok {
			t.Errorf("%s: no body in this sweep: a new FT8 command must be gated and added here", pattern)
			continue
		}
		path := strings.TrimPrefix(pattern, "POST ")
		for who, lifetime := range map[string]string{"no lifetime": "", "a lifetime never admitted": lifeC, "a replaced lifetime": lifeA} {
			status, resp, _ := l.appDo(t, c, lifetime, http.MethodPost, path, body)
			if status != http.StatusConflict || errorCode(t, resp) != notCurrent {
				t.Errorf("%s as %s: %d %s; want 409 %s", pattern, who, status, bytes.TrimSpace(resp), notCurrent)
			}
		}
		status, resp, _ := l.appDo(t, c, lifeB, http.MethodPost, path, body)
		if status < 500 && len(resp) > 0 && errorCode(t, resp) == notCurrent {
			t.Errorf("%s as the current app: %d %s", pattern, status, notCurrent)
		}
	}
}

// The profile claim and the disarm are served whoever asks.
func TestAppAdmission_TheClaimAndTheDisarmAreNotGated(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)
	l.mustAdmit(t, c, lifeA)
	l.mustAdmit(t, c, lifeB)

	for who, lifetime := range map[string]string{"no lifetime": "", "a replaced lifetime": lifeA, "the current app": lifeB} {
		status, resp, _ := l.appDo(t, c, lifetime, http.MethodPost, "/v1/ft8/tx/arm", `{"armed":false}`)
		if status != http.StatusAccepted {
			t.Errorf("a disarm as %s: %d %s; want 202", who, status, bytes.TrimSpace(resp))
		}
		status, resp, _ = l.appDo(t, c, lifetime, http.MethodPost, "/v1/ft8/claim", `{"mode":"ft8"}`)
		if len(resp) > 0 && status >= 400 && errorCode(t, resp) == notCurrent {
			t.Errorf("a claim as %s: %d %s", who, status, notCurrent)
		}
	}
}

// What a caller with no lifetime is told is something the operator can act on: an app
// left running across the install is the old shell (operator ruling 2026-11-17, question
// 10), and it is told to restart.
func TestAppAdmission_ACallerWithNoLifetimeIsToldToRestart(t *testing.T) {
	l := startChecked(t, true)
	_, c := l.clients(t)
	l.mustAdmit(t, c, lifeB)

	_, resp, _ := l.appDo(t, c, "", http.MethodPost, "/v1/ft8/tx/arm", `{"armed":true}`)
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(resp, &e); err != nil {
		t.Fatalf("%v in %s", err, resp)
	}
	if e.Code != notCurrent || !strings.Contains(e.Message, "Restart Station Manager") {
		t.Errorf("no lifetime: %s %q; want %s and the words to restart", e.Code, e.Message, notCurrent)
	}
	_, resp, _ = l.appDo(t, c, lifeA, http.MethodPost, "/v1/ft8/tx/arm", `{"armed":true}`)
	if err := json.Unmarshal(resp, &e); err != nil {
		t.Fatalf("%v in %s", err, resp)
	}
	if e.Code != notCurrent || strings.Contains(e.Message, "Restart") {
		t.Errorf("another lifetime: %s %q; want %s without the restart wording", e.Code, e.Message, notCurrent)
	}

	// It still reads, and a stream is still served: only FT8 operation is refused.
	if status, body, _ := l.appDo(t, c, "", http.MethodGet, "/v1/version", ""); status != http.StatusOK {
		t.Errorf("a read with no lifetime: %d %s", status, body)
	}
	if status, body, _ := l.appDo(t, c, lifeA, http.MethodGet, "/v1/config", ""); status != http.StatusOK {
		t.Errorf("a read as a replaced lifetime: %d %s", status, body)
	}
}

// Structure: no handler reaches an FT8 command except as the request's caller. A direct
// s.ft8.<Command>(…) in this package would skip the rule for that route.
func TestSource_Ft8CommandsAreCalledAsTheRequestsCaller(t *testing.T) {
	gated := map[string]bool{}
	for _, m := range []string{
		"ArmTx", "TransmitNext", "ClaimProfile",
		"StartQso", "StartQsoAddressee", "StartQsoFd", "StartQsoT4", "StartCallCq",
		"StartWorkCaller", "StartWorkCallerFd", "StartWorkCallerT4",
		"SetExchangePath", "AbandonQso", "StopAutoWorkRun", "SetQsoSkip",
		"NextAnswerer", "PickAnswerer", "BagAnswerer", "UnbagAnswerer", "ResumeDrain",
	} {
		gated[m] = true
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	viaCaller := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !gated[sel.Sel.Name] {
				return true
			}
			// The receiver must be s.ft8As(r), never s.ft8 (or anything else).
			recv, ok := sel.X.(*ast.CallExpr)
			if ok {
				if fn, ok := recv.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "ft8As" {
					viaCaller++
					return true
				}
			}
			t.Errorf("%s: %s is called on something other than s.ft8As(r)", fset.Position(call.Pos()), sel.Sel.Name)
			return true
		})
	}
	if viaCaller < len(gated) {
		t.Errorf("%d FT8 command calls go through ft8As; want at least %d: the guard is looking at too little", viaCaller, len(gated))
	}
}

var _ operatingScope = (*ft8.Service)(nil)

// openFt8Stream opens the FT8 event stream as a lifetime ("" for none) and returns its
// close. The stream is open, and counted, once its headers have arrived.
func (l checked) openFt8Stream(t *testing.T, c *http.Client, lifetime string) (closeStream func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := l.acceptable(t, true, http.MethodGet, "/v1/ft8/events", l.cred).WithContext(ctx)
	if lifetime != "" {
		r.Header.Set(appLifetimeHeader, lifetime)
	}
	resp, err := (&http.Client{Transport: c.Transport}).Do(r) // no client timeout: a stream has no end
	if err != nil {
		cancel()
		t.Fatalf("the FT8 stream as %q: %v", lifetime, err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("the FT8 stream as %q: %d", lifetime, resp.StatusCode)
	}
	return func() {
		cancel()
		_ = resp.Body.Close()
	}
}

// armCode asks to arm TX as a lifetime and returns the refusal's code.
func (l checked) armCode(t *testing.T, c *http.Client, lifetime string) string {
	t.Helper()
	_, body, _ := l.appDo(t, c, lifetime, http.MethodPost, "/v1/ft8/tx/arm", `{"armed":true}`)
	return errorCode(t, body)
}

// Presence over the real listeners (W-0027 C4b): an arm needs the current lifetime's own
// FT8 stream open. In this fixture nothing can key, so an arm that gets past presence is
// refused next for having no transmitter (ft8_tx_unavailable): that is how the two are
// told apart.
func TestAppAdmission_AnArmNeedsTheCurrentLifetimesFt8StreamOpen(t *testing.T) {
	const notPresent, pastPresence = "ft8_not_present", "ft8_tx_unavailable"
	l := startChecked(t, true)
	_, c := l.clients(t)
	l.mustAdmit(t, c, lifeA)

	if code := l.armCode(t, c, lifeA); code != notPresent {
		t.Fatalf("an arm with no FT8 stream open: %s; want %s", code, notPresent)
	}

	closeTool := l.openFt8Stream(t, c, "") // a tool reading the stream, naming no lifetime
	defer closeTool()
	closeOther := l.openFt8Stream(t, c, lifeC) // a lifetime that was never admitted
	defer closeOther()
	if code := l.armCode(t, c, lifeA); code != notPresent {
		t.Fatalf("an arm with only other callers' streams open: %s; want %s", code, notPresent)
	}

	closeApp := l.openFt8Stream(t, c, lifeA)
	if code := l.armCode(t, c, lifeA); code != pastPresence {
		t.Fatalf("an arm with the app's own stream open: %s; want %s (it got past presence)", code, pastPresence)
	}

	closeApp()
	deadline := time.Now().Add(5 * time.Second)
	for l.armCode(t, c, lifeA) != notPresent {
		if time.Now().After(deadline) {
			t.Fatal("the app's stream was closed and an arm is still not refused for presence")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// With the check off an arm asks for no stream, as before C4.
func TestAppAdmission_OffAnArmNeedsNoStream(t *testing.T) {
	l := startChecked(t, false)
	_, c := l.clients(t)
	if code := l.armCode(t, c, lifeA); code != "ft8_tx_unavailable" {
		t.Fatalf("with the check off an arm with no stream: %s; want ft8_tx_unavailable", code)
	}
}
