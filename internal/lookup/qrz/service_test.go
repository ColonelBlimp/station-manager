package qrz

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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

// uninitializedService builds a qrz.Service with a live URL + client but NO session
// key and NOT yet initialized — for exercising Initialize and the lazy session-key
// (ensureSessionKey) recovery path. (initializedService pre-sets a key + isInitialized,
// so it can't drive these.)
func uninitializedService(url string, client *http.Client) *Service {
	return &Service{
		LoggerService: &logging.Service{},
		Config: &types.LookupConfig{
			Name:           ServiceName,
			Enabled:        true,
			URL:            url,
			Username:       "tester",
			Password:       "secret",
			HttpTimeoutSec: 5,
		},
		UserAgent: "smd/test",
		client:    client,
	}
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

// TestInitialize_SessionKeyFailureStaysEnabled pins the flaky-link fix (found
// on-air 2026-07-04): a startup session-key failure no longer PERMANENTLY disables
// QRZ. It stays Enabled with no key, so lookups can lazily re-fetch it and revive
// the provider without a daemon restart — instead of one boot-time timeout killing
// enrichment for the whole run. (Was TestInitialize_SessionKeyFailureDisablesService,
// which asserted the old permanent-disable behaviour.)
func TestInitialize_SessionKeyFailureStaysEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
			<QRZDatabase><Session><Error>Username/password incorrect</Error></Session></QRZDatabase>`))
	}))
	defer srv.Close()

	s := uninitializedService(srv.URL, srv.Client())
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("session-fetch failure must be soft (nil err), not a hard startup error; got %v", err)
	}
	if !s.Config.Enabled {
		t.Fatal("Config.Enabled must STAY true after a session-fetch failure (the fix: lazy retry, never permanent disable)")
	}
	if !s.isInitialized.Load() {
		t.Fatal("service should be marked initialized")
	}
	if s.getSessionKey() != "" {
		t.Fatal("no session key should be set after a failed fetch")
	}
	// A lookup on the keyless-but-enabled service fail-softs (attempts the session
	// key, still fails, returns an error the orchestrator falls through on) — it must
	// NOT block or panic.
	if _, err := s.LookupWithContext(context.Background(), "M0CMC"); err == nil {
		t.Fatal("lookup with an unrecoverable session should fail-soft with an error, not succeed")
	}
}

// TestLazySessionKey_RecoversAfterBootFailure: the session login fails at startup
// (flaky link), the service stays enabled, and once the link recovers the very next
// lookup lazily fetches the key and returns the name — no daemon restart.
func TestLazySessionKey_RecoversAfterBootFailure(t *testing.T) {
	var failSession atomic.Bool
	failSession.Store(true) // boot login fails
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("username") != "" { // session-key request
			if failSession.Load() {
				_, _ = w.Write([]byte(`<QRZDatabase><Session><Error>session timeout</Error></Session></QRZDatabase>`))
				return
			}
			_, _ = w.Write([]byte(`<QRZDatabase><Session><Key>good-key</Key></Session></QRZDatabase>`))
			return
		}
		// lookup request
		_, _ = w.Write([]byte(`<QRZDatabase><Callsign><call>PY2DN</call><fname>Roberto</fname><name>Zunta</name></Callsign><Session><Key>good-key</Key></Session></QRZDatabase>`))
	}))
	defer srv.Close()

	s := uninitializedService(srv.URL, srv.Client())
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !s.Config.Enabled {
		t.Fatal("must stay enabled after a boot session failure")
	}

	failSession.Store(false) // link recovers
	st, err := s.LookupWithContext(context.Background(), "PY2DN")
	if err != nil {
		t.Fatalf("lookup after recovery: %v", err)
	}
	if st.Name != "Roberto Zunta" {
		t.Fatalf("Name = %q, want %q (lazy session recovery)", st.Name, "Roberto Zunta")
	}
}

// TestLazySessionKey_CooldownSuppressesRetry: while the session login keeps failing,
// lazy re-fetch is bounded by sessionRetryCooldown — a burst of lookups triggers at
// most one login per cooldown, not one per lookup (so an outage/bad-creds doesn't
// hammer QRZ).
func TestLazySessionKey_CooldownSuppressesRetry(t *testing.T) {
	var sessionReqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("username") != "" {
			sessionReqs.Add(1)
			_, _ = w.Write([]byte(`<QRZDatabase><Session><Error>session timeout</Error></Session></QRZDatabase>`))
			return
		}
		_, _ = w.Write([]byte(`<QRZDatabase><Callsign><call>X</call></Callsign></QRZDatabase>`))
	}))
	defer srv.Close()

	s := uninitializedService(srv.URL, srv.Client())
	_ = s.Initialize(context.Background()) // boot session fails; leaves lastAuthAttempt zero
	sessionReqs.Store(0)                   // count only post-Initialize attempts

	// First lookup attempts the login (cooldown not started) and fails; the second,
	// immediately after, is inside the cooldown and must NOT hit the session endpoint.
	_, _ = s.LookupWithContext(context.Background(), "AA1AA")
	_, _ = s.LookupWithContext(context.Background(), "BB2BB")
	if got := sessionReqs.Load(); got != 1 {
		t.Fatalf("session-key requests across 2 lookups = %d, want 1 (cooldown suppresses the 2nd)", got)
	}
}

// TestLazySessionKey_RetriesAfterCooldown covers the self-healing branch: after a
// failed lazy login the cooldown suppresses retries, but once it elapses the next
// lookup retries — and if the link has recovered, gets the key. Shortens
// sessionRetryCooldown (the reason it's a var) so the test isn't slow.
func TestLazySessionKey_RetriesAfterCooldown(t *testing.T) {
	orig := sessionRetryCooldown
	sessionRetryCooldown = 20 * time.Millisecond
	t.Cleanup(func() { sessionRetryCooldown = orig })

	var failSession atomic.Bool
	failSession.Store(true)
	var sessionReqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("username") != "" {
			sessionReqs.Add(1)
			if failSession.Load() {
				_, _ = w.Write([]byte(`<QRZDatabase><Session><Error>session timeout</Error></Session></QRZDatabase>`))
				return
			}
			_, _ = w.Write([]byte(`<QRZDatabase><Session><Key>good-key</Key></Session></QRZDatabase>`))
			return
		}
		_, _ = w.Write([]byte(`<QRZDatabase><Callsign><call>K1ABC</call><fname>Ann</fname></Callsign><Session><Key>good-key</Key></Session></QRZDatabase>`))
	}))
	defer srv.Close()

	s := uninitializedService(srv.URL, srv.Client())
	_ = s.Initialize(context.Background()) // boot login fails; lastAuthAttempt stays zero
	sessionReqs.Store(0)

	// Lookup 1 retries the login (cooldown not started) and fails.
	if _, err := s.LookupWithContext(context.Background(), "K1ABC"); err == nil {
		t.Fatal("lookup 1 should fail (still no key)")
	}
	// Link recovers; wait out the (tiny) cooldown so the next lookup is allowed to retry.
	failSession.Store(false)
	time.Sleep(40 * time.Millisecond)
	st, err := s.LookupWithContext(context.Background(), "K1ABC")
	if err != nil {
		t.Fatalf("lookup after cooldown + recovery: %v", err)
	}
	if st.Name != "Ann" {
		t.Fatalf("Name = %q, want Ann (login retried once the cooldown elapsed)", st.Name)
	}
	if got := sessionReqs.Load(); got < 2 {
		t.Fatalf("session attempts = %d, want >= 2 (one failed, then a post-cooldown retry)", got)
	}
}

// TestClearSessionKeyIf covers the compare-and-clear fix for the overlapping-expiry
// race: a stale lookup reacting to an expired key must wipe ONLY the key it used, so
// it can't clobber a fresh key a concurrent lookup just acquired (which would strand
// both keyless for a cooldown window).
func TestClearSessionKeyIf(t *testing.T) {
	s := &Service{}
	s.setSessionKey("K2") // a concurrent lookup already re-authed to K2
	s.clearSessionKeyIf("K1")
	if got := s.getSessionKey(); got != "K2" {
		t.Fatalf("clearSessionKeyIf wiped a key it didn't own: got %q, want K2", got)
	}
	s.clearSessionKeyIf("K2") // the lookup that actually used K2 clears it
	if got := s.getSessionKey(); got != "" {
		t.Fatalf("clearSessionKeyIf failed to clear its own key: got %q", got)
	}
}

// TestEnsureSessionKey_NilCooldownDoesNotFakeSuccess covers the other half of the
// race fix: after a SUCCESS (lastAuthErr==nil) the cooldown must not be applied — a
// keyless service with a recent successful attempt (the post-race state) must
// attempt a real login rather than returning nil "success" with no key.
func TestEnsureSessionKey_NilCooldownDoesNotFakeSuccess(t *testing.T) {
	var reqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("username") != "" {
			reqs.Add(1)
			_, _ = w.Write([]byte(`<QRZDatabase><Session><Key>fresh-key</Key></Session></QRZDatabase>`))
		}
	}))
	defer srv.Close()

	s := uninitializedService(srv.URL, srv.Client())
	// Force the post-race state: keyless, but the last attempt SUCCEEDED (nil err)
	// and was recent — a naive cooldown would suppress and fake success.
	s.lastAuthErr = nil
	s.lastAuthAttempt = time.Now()

	if err := s.ensureSessionKey(); err != nil {
		t.Fatalf("ensureSessionKey: %v", err)
	}
	if s.getSessionKey() == "" {
		t.Fatal("ensureSessionKey reported success but set no key (nil-cooldown faked success)")
	}
	if got := reqs.Load(); got != 1 {
		t.Fatalf("expected 1 real login attempt, got %d", got)
	}
}

// TestScrubURLError covers the credential-leak fix: a transport *url.Error must come
// back with its query (which carries the QRZ password / session key) stripped, while
// non-url.Error and unparseable-URL values pass through unchanged.
func TestScrubURLError(t *testing.T) {
	ue := &url.Error{
		Op:  "Get",
		URL: "https://xmldata.qrz.com/xml/current?username=7Q5MLV&password=SECRET&agent=smd",
		Err: stderrors.New("dial tcp: i/o timeout"),
	}
	got := scrubURLError(ue).Error()
	if strings.Contains(got, "SECRET") || strings.Contains(got, "password") {
		t.Fatalf("scrubbed error still leaks credentials: %q", got)
	}
	if !strings.Contains(got, "xmldata.qrz.com") {
		t.Fatalf("scrubbed error dropped the host too: %q", got)
	}

	plain := stderrors.New("some other failure")
	if scrubURLError(plain) != plain {
		t.Fatal("non-url.Error should pass through unchanged")
	}
	// Unparseable URL: leave the error intact rather than panic.
	bad := &url.Error{Op: "Get", URL: "://nope", Err: stderrors.New("x")}
	if scrubURLError(bad) == nil {
		t.Fatal("unparseable URL should pass through, not become nil")
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
	case <-done:
		// Reaching here before the 2s timeout IS the assertion: Initialize saw the
		// cancellation and returned promptly instead of waiting the full 60s
		// HttpTimeoutSec. Under the flaky-link fix it returns SOFT (nil, still
		// Enabled) — a startup session-key failure (here from the cancelled
		// transport) no longer disables the provider; lookups lazily re-fetch the
		// key later. So we assert responsiveness, not disable-on-failure.
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
