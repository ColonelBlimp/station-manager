package qrz

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// initializedService builds a qrz.Service that's already past
// Initialize, with caller-supplied URL and HTTP client. Bypasses the
// session-key fetch (sets a synthetic key) so each Lookup-flavoured
// test only has to mock the lookup endpoint, not the session
// endpoint as well.
func initializedService(t *testing.T, url string, client *http.Client) *Service {
	t.Helper()
	s := &Service{
		LoggerService: &logging.Service{},
		Config: &types.LookupConfig{
			Name:           ServiceName,
			Enabled:        true,
			URL:            url,
			Username:       "tester",
			Password:       "secret",
			HttpTimeoutSec: 5,
		},
		UserAgent:  "smd/test",
		client:     client,
		sessionKey: "test-session-key",
	}
	s.isInitialized.Store(true)
	return s
}

// ---- Initialize ----

func TestInitialize_MissingLogger(t *testing.T) {
	s := &Service{Config: &types.LookupConfig{Enabled: false}}
	if err := s.Initialize(context.Background()); err == nil {
		t.Fatal("expected error when logger is nil")
	}
}

func TestInitialize_DisabledIsValid(t *testing.T) {
	s := &Service{
		LoggerService: &logging.Service{},
		Config:        &types.LookupConfig{Enabled: false},
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("disabled config should initialize cleanly: %v", err)
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
}

func TestInitialize_RejectsBrokenConfig(t *testing.T) {
	cases := []struct {
		name      string
		cfg       *types.LookupConfig
		userAgent string
	}{
		{"empty URL", &types.LookupConfig{Enabled: true, URL: "", HttpTimeoutSec: 1, Username: "abc", Password: "abcde"}, "a"},
		{"invalid URL", &types.LookupConfig{Enabled: true, URL: "::nope", HttpTimeoutSec: 1, Username: "abc", Password: "abcde"}, "a"},
		// http to a non-loopback host is rejected — QRZ creds travel in the URL (M2).
		{"insecure http URL", &types.LookupConfig{Enabled: true, URL: "http://xml.qrz.com/", HttpTimeoutSec: 1, Username: "abc", Password: "abcde"}, "a"},
		// The remaining cases use https:// so they exercise their intended check,
		// not the transport gate.
		{"empty UserAgent", &types.LookupConfig{Enabled: true, URL: "https://x", HttpTimeoutSec: 1, Username: "abc", Password: "abcde"}, ""},
		{"zero timeout", &types.LookupConfig{Enabled: true, URL: "https://x", HttpTimeoutSec: 0, Username: "abc", Password: "abcde"}, "a"},
		{"short username", &types.LookupConfig{Enabled: true, URL: "https://x", HttpTimeoutSec: 1, Username: "ab", Password: "abcde"}, "a"},
		{"short password", &types.LookupConfig{Enabled: true, URL: "https://x", HttpTimeoutSec: 1, Username: "abc", Password: "abc"}, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Service{LoggerService: &logging.Service{}, Config: c.cfg, UserAgent: c.userAgent}
			if err := s.Initialize(context.Background()); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

func TestInitialize_SessionKeyFailureDisablesService(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
			<QRZDatabase>
				<Session>
					<Error>Username/password incorrect</Error>
				</Session>
			</QRZDatabase>`))
	}))
	defer srv.Close()

	s := &Service{
		LoggerService: &logging.Service{},
		Config: &types.LookupConfig{
			Enabled:        true,
			URL:            srv.URL,
			Username:       "tester",
			Password:       "wrong",
			HttpTimeoutSec: 5,
		},
		UserAgent: "smd/test",
		client:    srv.Client(),
	}
	err := s.Initialize(context.Background())
	// New contract (2026-05-12): session-fetch failure is a soft
	// disable, not a hard error. The "Enrichment never blocks
	// logging" invariant says external-service failures (network
	// timeout, DNS failure, QRZ.com down, or bad credentials) must
	// not prevent the operator from starting the daemon or logging
	// QSOs. Initialize logs a warning, flips Enabled=false, and
	// returns nil so the cmd/smd startup path continues. The
	// orchestrator skips disabled providers in the chain.
	if err != nil {
		t.Fatalf("expected nil err on session-fetch failure (soft-disable contract); got %v", err)
	}
	if s.Config.Enabled {
		t.Fatal("Config.Enabled should be false after session-fetch failure")
	}
	// M2 fix (review 2026-06-04): a soft-disabled service must still be marked
	// initialized, so a direct/late LookupWithContext returns the disabled
	// sentinel — (ContactedStation{Call:callsign}, nil) — rather than the
	// misleading "service is not initialized" error.
	if !s.isInitialized.Load() {
		t.Fatal("soft-disabled service should be marked initialized")
	}
	st, lerr := s.LookupWithContext(context.Background(), "M0CMC")
	if lerr != nil {
		t.Fatalf("disabled LookupWithContext should return the sentinel, got err: %v", lerr)
	}
	if st.Call != "M0CMC" {
		t.Errorf("sentinel Call = %q, want M0CMC (the supplied callsign)", st.Call)
	}
}

// ---- session re-auth on expiry (M1) ----

// TestLookupWithContext_SessionExpiry_ReAuthsAndRetries pins the M1 fix: an
// expired-session error on a lookup triggers exactly one re-authentication and
// one retry, which then succeeds.
func TestLookupWithContext_SessionExpiry_ReAuthsAndRetries(t *testing.T) {
	var lookups, sessions int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("callsign") == "" {
			// Re-auth request — carries username/password, no callsign.
			sessions++
			_, _ = w.Write([]byte(`<?xml version="1.0"?><QRZDatabase><Session><Key>fresh-key</Key></Session></QRZDatabase>`))
			return
		}
		lookups++
		if lookups == 1 {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><QRZDatabase><Session><Error>Invalid session key</Error></Session></QRZDatabase>`))
			return
		}
		_, _ = w.Write([]byte(`<?xml version="1.0"?><QRZDatabase><Callsign><call>M0CMC</call><fname>Marc</fname></Callsign></QRZDatabase>`))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	st, err := s.LookupWithContext(context.Background(), "M0CMC")
	if err != nil {
		t.Fatalf("lookup after re-auth: %v", err)
	}
	if st.Call != "M0CMC" || st.Name != "Marc" {
		t.Errorf("station = %+v, want Call=M0CMC Name=Marc", st)
	}
	if sessions != 1 {
		t.Errorf("re-auth session fetches = %d, want 1", sessions)
	}
	if lookups != 2 {
		t.Errorf("lookups = %d, want 2 (initial expiry + retry)", lookups)
	}
}

// TestLookupWithContext_PersistentSessionExpiry_NoLoop pins that a session that
// stays expired after re-auth yields an error rather than looping: the retry
// happens exactly once.
func TestLookupWithContext_PersistentSessionExpiry_NoLoop(t *testing.T) {
	var lookups int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("callsign") == "" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><QRZDatabase><Session><Key>k</Key></Session></QRZDatabase>`))
			return
		}
		lookups++
		_, _ = w.Write([]byte(`<?xml version="1.0"?><QRZDatabase><Session><Error>Invalid session key</Error></Session></QRZDatabase>`))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	if _, err := s.LookupWithContext(context.Background(), "M0CMC"); err == nil {
		t.Fatal("expected an error on persistent session expiry")
	}
	if lookups != 2 {
		t.Errorf("lookups = %d, want 2 (initial + one retry, no loop)", lookups)
	}
}

// ---- Lookup happy path ----

// TestInitialize_RespectsContextCancellation pins review-finding M4
// (session-key fetch ignored ctx). A QRZ server that hangs on the
// session request must be cancellable via the ctx passed to
// Initialize so daemon shutdown isn't blocked on a stuck handshake.
//
// Pre-fix, the request was built with http.NewRequest (no context),
// and the only timeout was the absolute HttpTimeoutSec on the
// client. A cancelled ctx would have no effect — Initialize would
// only return when the per-call timeout fired or the body finished.
// Post-fix, http.NewRequestWithContext propagates ctx into the
// transport, and a cancelled ctx returns promptly.
func TestInitialize_RespectsContextCancellation(t *testing.T) {
	// Server that hangs on the session-key request — never writes
	// a body, just waits for the request's ctx to fire (which is
	// what http.NewRequestWithContext propagates from the client).
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	s := &Service{
		LoggerService: &logging.Service{},
		Config: &types.LookupConfig{
			Enabled:        true,
			URL:            srv.URL,
			Username:       "tester",
			Password:       "secret",
			HttpTimeoutSec: 60, // generous — we want the ctx to win, not the timeout
		},
		UserAgent: "smd/test",
		client:    srv.Client(),
	}

	// Cancel the ctx before Initialize even starts. A correctly
	// wired Initialize / requestAndSetSessionKey will see the
	// cancellation immediately rather than waiting the full 60s
	// HttpTimeoutSec.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- s.Initialize(ctx) }()

	select {
	case err := <-done:
		// Initialize either errors (preferred — auth failure due to
		// transport cancellation) or self-disables (also acceptable
		// — that's the existing failure-disables-the-service contract).
		if err == nil && s.Config.Enabled {
			t.Fatal("Initialize neither errored nor disabled service after ctx cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Initialize ignored ctx cancellation — would have blocked daemon shutdown")
	}
}

func TestLookup_HappyPath_ReturnsContactedStation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("callsign") != "M0CMC" {
			t.Errorf("unexpected callsign query: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("s") != "test-session-key" {
			t.Errorf("session key not propagated to query")
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
			<QRZDatabase>
				<Callsign>
					<call>M0CMC</call>
					<fname>Marc</fname>
					<name>Veary</name>
					<addr2>Lilongwe</addr2>
					<country>Malawi</country>
					<grid>KH53</grid>
					<cqzone>37</cqzone>
					<ituzone>53</ituzone>
				</Callsign>
				<Session>
					<Key>test-session-key</Key>
				</Session>
			</QRZDatabase>`))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())

	got, err := s.Lookup("M0CMC")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Call != "M0CMC" {
		t.Errorf("Call = %q, want M0CMC", got.Call)
	}
	if got.Name != "Marc Veary" {
		t.Errorf("Name = %q, want \"Marc Veary\"", got.Name)
	}
	if got.QTH != "Lilongwe" {
		t.Errorf("QTH = %q, want Lilongwe", got.QTH)
	}
	if got.Gridsquare != "KH53" {
		t.Errorf("Gridsquare = %q, want KH53 (uppercased)", got.Gridsquare)
	}

	// Per ADR 0017's M0CMC example — QRZ DOES return wrong country/zone
	// values for this call (Malawi, CQ 37, ITU 53). The provider parses
	// them faithfully (the wire shape says so); the orchestrator's
	// FilterToCallsignFields strips them at write time. Pin the parse
	// behaviour here so future-us doesn't accidentally "fix" the
	// provider by hiding the upstream lie — it's load-bearing for the
	// orchestrator's filter to be the single enforcement point.
	if got.Country != "Malawi" {
		t.Errorf("Country = %q, want Malawi (preserved from upstream; orchestrator strips later)", got.Country)
	}
	if got.CQZ != "37" {
		t.Errorf("CQZ = %q, want 37 (preserved from upstream; orchestrator strips later)", got.CQZ)
	}
	if got.ITUZ != "53" {
		t.Errorf("ITUZ = %q, want 53 (preserved from upstream; orchestrator strips later)", got.ITUZ)
	}
}

func TestLookup_PrefersNameFmt_FallsBackToFnameName(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"name_fmt wins",
			`<QRZDatabase><Callsign><call>X</call><name_fmt>Marc V</name_fmt><fname>IGNORED</fname></Callsign><Session/></QRZDatabase>`,
			"Marc V",
		},
		{
			"fname + name fallback",
			`<QRZDatabase><Callsign><call>X</call><fname>Marc</fname><name>Veary</name></Callsign><Session/></QRZDatabase>`,
			"Marc Veary",
		},
		{
			"nickname last resort",
			`<QRZDatabase><Callsign><call>X</call><nickname>nicky</nickname></Callsign><Session/></QRZDatabase>`,
			"nicky",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Service{}
			got, err := s.unmarshalResponse([]byte(c.body))
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Name != c.want {
				t.Errorf("Name = %q, want %q", got.Name, c.want)
			}
		})
	}
}

// ---- Not-found semantics ----

func TestLookup_NotFound_ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
			<QRZDatabase>
				<Callsign/>
				<Session>
					<Error>Not found: ZZ9XYZ</Error>
				</Session>
			</QRZDatabase>`))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("ZZ9XYZ")
	if !stderrors.Is(err, errors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (chain runner relies on this signal)", err)
	}
}

func TestLookup_EmptyCallElement_TreatedAsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<QRZDatabase><Callsign></Callsign><Session/></QRZDatabase>`))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("ZZ9XYZ")
	if !stderrors.Is(err, errors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLookup_NonNotFoundSessionError_ReturnsBareError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<QRZDatabase><Callsign/><Session><Error>Session expired</Error></Session></QRZDatabase>`))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("M0CMC")
	if err == nil {
		t.Fatal("expected error for session expiry")
	}
	if stderrors.Is(err, errors.ErrNotFound) {
		t.Fatal("session-expired must NOT classify as ErrNotFound — orchestrator distinguishes the two")
	}
	if !strings.Contains(err.Error(), "Session expired") {
		t.Errorf("err = %v, want it to carry the QRZ error message", err)
	}
}

// ---- Failure surfacing ----

func TestLookup_Non2xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("M0CMC")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if stderrors.Is(err, errors.ErrNotFound) {
		t.Fatal("500 must not be classified as ErrNotFound")
	}
}

func TestLookup_TransportError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("M0CMC")
	if err == nil {
		t.Fatal("expected transport error")
	}
}

// ---- Cancellation ----

func TestLookupWithContext_RespectsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.LookupWithContext(ctx, "M0CMC"); err == nil {
		t.Fatal("expected error from cancelled ctx")
	}
}

// ---- Disabled sentinel ----

func TestLookup_Disabled_ReturnsCallSentinel(t *testing.T) {
	s := &Service{
		LoggerService: &logging.Service{},
		Config:        &types.LookupConfig{Enabled: false},
	}
	s.isInitialized.Store(true)

	got, err := s.Lookup("M0CMC")
	if err != nil {
		t.Fatalf("disabled lookup must not error: %v", err)
	}
	// Matches v1's contract — caller distinguishes by the absence of
	// substantive fields. Orchestrator normally skips disabled
	// providers entirely so this is the defensive corner case.
	if got.Call != "M0CMC" {
		t.Errorf("Call = %q, want M0CMC sentinel", got.Call)
	}
	if got.Name != "" {
		t.Errorf("disabled sentinel should have empty Name, got %q", got.Name)
	}
}

// ---- Edge cases on input ----

func TestLookup_EmptyCallsign_Errors(t *testing.T) {
	s := initializedService(t, "http://x", &http.Client{})
	if _, err := s.Lookup("   "); err == nil {
		t.Fatal("expected error for empty/whitespace callsign")
	}
}

func TestService_NotInitialized_Errors(t *testing.T) {
	s := &Service{Config: &types.LookupConfig{Enabled: true, URL: "http://x"}}
	if _, err := s.Lookup("M0CMC"); err == nil {
		t.Fatal("expected error when not initialized")
	}
}

// ---- Name() identity ----

func TestName_StableIdentifier(t *testing.T) {
	s := &Service{}
	if got := s.Name(); got != "qrzlookupservice" {
		t.Fatalf("Name = %q, want qrzlookupservice", got)
	}
	if ServiceName != types.QRZLookupServiceName {
		t.Fatalf("ServiceName drift: %q vs %q", ServiceName, types.QRZLookupServiceName)
	}
}

// ---- Compile-time interface check ----
// The var _ in service.go pins this; this test makes the requirement
// surface in test output too so a future "I removed Lookup but the
// build still passes" surprise would be caught.

func TestImplementsCallsignProvider(t *testing.T) {
	// Compiles only if Service implements lookup.CallsignProvider.
	// The functional behaviour is exercised by the other tests; this
	// test exists to flag interface-shape drift loud and early.
	t.Skip("interface compliance is enforced at compile time via service.go's `var _ lookup.CallsignProvider = (*Service)(nil)`")
}

// TestReadLimitedBody_RejectsOversized guards review 2026-06-19 M1: the shared
// 2xx-body reader (used by both the session-auth and lookup reads) errors when
// the upstream sends more than successBodyLimit, rather than buffering it whole.
func TestReadLimitedBody_RejectsOversized(t *testing.T) {
	if _, err := readLimitedBody(strings.NewReader(strings.Repeat("x", successBodyLimit+1))); err == nil {
		t.Error("expected an error for a body over the limit")
	}
	b, err := readLimitedBody(strings.NewReader(strings.Repeat("x", successBodyLimit)))
	if err != nil || len(b) != successBodyLimit {
		t.Errorf("limit-sized body should pass: len=%d err=%v", len(b), err)
	}
}
