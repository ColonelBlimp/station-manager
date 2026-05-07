package hamnut

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// initializedService builds a hamnut.Service that's already past
// Initialize, with caller-supplied URL and HTTP client. Bypasses the
// DI / ConfigService path (not wired until task #62) and lets each
// test point at its own httptest.Server.
func initializedService(t *testing.T, url string, client *http.Client) *Service {
	t.Helper()
	s := &Service{
		LoggerService: &logging.Service{},
		Config: &types.LookupConfig{
			Name:           ServiceName,
			Enabled:        true,
			URL:            url,
			UserAgent:      "smd/test",
			HttpTimeoutSec: 5,
		},
		client: client,
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

func TestInitialize_DirectConfig_DisabledIsValid(t *testing.T) {
	s := &Service{
		LoggerService: &logging.Service{},
		Config:        &types.LookupConfig{Enabled: false},
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("disabled config should initialize cleanly: %v", err)
	}
	// Idempotent.
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
}

func TestInitialize_RejectsBrokenConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  *types.LookupConfig
	}{
		{"empty URL", &types.LookupConfig{Enabled: true, URL: "", UserAgent: "a", HttpTimeoutSec: 1}},
		{"invalid URL", &types.LookupConfig{Enabled: true, URL: "::nope", UserAgent: "a", HttpTimeoutSec: 1}},
		{"empty UserAgent", &types.LookupConfig{Enabled: true, URL: "http://x", UserAgent: "", HttpTimeoutSec: 1}},
		{"zero timeout", &types.LookupConfig{Enabled: true, URL: "http://x", UserAgent: "a", HttpTimeoutSec: 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Service{LoggerService: &logging.Service{}, Config: c.cfg}
			if err := s.Initialize(context.Background()); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

// ---- Lookup happy path ----

func TestLookup_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("prefix") != "M0CMC" {
			t.Errorf("unexpected prefix query: %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("User-Agent"); got != "smd/test" {
			t.Errorf("User-Agent = %q, want smd/test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "OK",
			"found": true,
			"continent": "EU",
			"countryName": "England",
			"cqZone": 14,
			"ituZone": 27,
			"prefix": "M",
			"primaryDXCCPrefix": "G",
			"countryCode": "GBR"
		}`))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())

	got, err := s.Lookup("M0CMC")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Name != "England" {
		t.Errorf("Name = %q, want England", got.Name)
	}
	if got.Continent != "EU" {
		t.Errorf("Continent = %q, want EU", got.Continent)
	}
	if got.CQZone != "14" {
		t.Errorf("CQZone = %q, want \"14\"", got.CQZone)
	}
	if got.ITUZone != "27" {
		t.Errorf("ITUZone = %q, want \"27\"", got.ITUZone)
	}
	if got.DXCCPrefix != "G" {
		t.Errorf("DXCCPrefix = %q, want G", got.DXCCPrefix)
	}
	if got.Ccode != "GBR" {
		t.Errorf("Ccode = %q, want GBR", got.Ccode)
	}
	// Per ADR 0017's M0CMC example — hamnut should NOT be telling us
	// this English call is in CQ Zone 37 (Malawi). The test fixture
	// returns 14 (correct for England); this assertion guards the
	// upstream-mapping path against accidentally swapping fields.
	if got.CQZone == "37" {
		t.Fatal("CQZone = 37 — that's the QRZ-bug-for-M0CMC value; hamnut should never return it")
	}
}

// ---- Not-found semantics ----

func TestLookup_HamnutFoundFalse_ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"OK","found":false}`))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("ZZ9XYZ")
	if !stderrors.Is(err, errors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLookup_404_ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("ZZ9XYZ")
	if !stderrors.Is(err, errors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// ---- Failure surfacing for orchestrator's implicit-fall-through ----

func TestLookup_Non2xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream broken"))
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("M0CMC")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if stderrors.Is(err, errors.ErrNotFound) {
		t.Fatal("500 must not be classified as ErrNotFound — orchestrator's implicit-fall-through depends on the distinction")
	}
}

func TestLookup_TransportError_ReturnsError(t *testing.T) {
	// Server that immediately closes the connection — simulates a
	// flaky upstream the orchestrator must treat as "internet down,
	// fall through to local DB" per ADR 0017 #7.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // close before any request

	s := initializedService(t, srv.URL, srv.Client())
	_, err := s.Lookup("M0CMC")
	if err == nil {
		t.Fatal("expected transport error")
	}
}

// ---- Cancellation ----

func TestLookupWithContext_RespectsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hold until cancelled
	}))
	defer srv.Close()

	s := initializedService(t, srv.URL, srv.Client())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call
	if _, err := s.LookupWithContext(ctx, "M0CMC"); err == nil {
		t.Fatal("expected error from cancelled ctx")
	}
}

// ---- Disabled sentinel ----

func TestLookup_Disabled_ReturnsUnknownSentinel(t *testing.T) {
	s := &Service{
		LoggerService: &logging.Service{},
		Config:        &types.LookupConfig{Enabled: false},
	}
	s.isInitialized.Store(true)

	got, err := s.Lookup("M0CMC")
	if err != nil {
		t.Fatalf("disabled lookup must not error: %v", err)
	}
	if got.Name != "Unknown" {
		t.Fatalf("Name = %q, want Unknown sentinel", got.Name)
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
	s := &Service{Config: &types.LookupConfig{Enabled: true, URL: "http://x", UserAgent: "a", HttpTimeoutSec: 1}}
	if _, err := s.Lookup("M0CMC"); err == nil {
		t.Fatal("expected error when not initialized")
	}
}

// ---- Name() identity ----

func TestName_StableIdentifier(t *testing.T) {
	s := &Service{}
	if got := s.Name(); got != "hamnutlookupservice" {
		t.Fatalf("Name = %q, want hamnutlookupservice (stable across refactors)", got)
	}
	if ServiceName != types.HamNutLookupServiceName {
		t.Fatalf("ServiceName drift: %q vs %q", ServiceName, types.HamNutLookupServiceName)
	}
}

// ---- deriveTimeOffset unit cases ----

func TestDeriveTimeOffset(t *testing.T) {
	cases := []struct {
		name string
		in   PrefixLookupResponse
		want string
	}{
		{"explicit field wins", PrefixLookupResponse{TimeOffset: "+01:00", LocalTime: "2026-05-07T12:00:00+05:30"}, "+01:00"},
		{"RFC3339 positive", PrefixLookupResponse{LocalTime: "2026-05-07T12:00:00+05:30"}, "+05:30"},
		{"RFC3339 negative", PrefixLookupResponse{LocalTime: "2026-05-07T12:00:00-08:00"}, "-08:00"},
		{"empty", PrefixLookupResponse{}, ""},
		{"legacy 6-char fallback", PrefixLookupResponse{LocalTime: "garbage+02:00"}, "+02:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveTimeOffset(c.in)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// ---- unmarshalResponse direct ----

func TestUnmarshalResponse_PropagatesNotFound(t *testing.T) {
	s := &Service{}
	body := []byte(`{"status":"OK","found":false}`)
	_, err := s.unmarshalResponse(body)
	if !stderrors.Is(err, errors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUnmarshalResponse_BadJSON(t *testing.T) {
	s := &Service{}
	_, err := s.unmarshalResponse([]byte("not json"))
	if err == nil || strings.Contains(err.Error(), "ErrNotFound") {
		t.Fatalf("err = %v, want bare decode error (not ErrNotFound)", err)
	}
}
