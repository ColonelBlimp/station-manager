package hamnut

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Station-Manager/config"
	smerrors "github.com/Station-Manager/errors"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
)

// --- Test doubles ---

type testConfigService struct {
	cfg types.LookupConfig
	err error
}

func (t *testConfigService) LookupServiceConfig(_ string) (types.LookupConfig, error) {
	return t.cfg, t.err
}

// compile-time check that testConfigService satisfies the method we use on *config.Service
var _ interface {
	LookupServiceConfig(string) (types.LookupConfig, error)
} = (*testConfigService)(nil)

// --- Initialize tests ---

func TestService_Initialize_MissingLogger(t *testing.T) {
	s := &Service{ConfigService: &config.Service{}}

	if err := s.Initialize(); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestService_Initialize_MissingConfigService(t *testing.T) {
	s := &Service{LoggerService: &logging.Service{}}

	if err := s.Initialize(); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// --- Lookup tests ---

func TestService_Lookup_NotInitialized(t *testing.T) {
	s := &Service{}

	_, err := s.Lookup("K1ABC")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestService_Lookup_EmptyCallsign(t *testing.T) {
	cfg := types.LookupConfig{Enabled: true, URL: "http://example.com", UserAgent: "test"}
	s := &Service{Config: &cfg, client: &http.Client{}}
	s.isInitialized.Store(true)

	_, err := s.Lookup("   ")
	if err == nil {
		t.Fatalf("expected error for empty callsign, got nil")
	}
}

func TestService_Lookup_DisabledConfig(t *testing.T) {
	cfg := types.LookupConfig{Enabled: false}
	s := &Service{Config: &cfg, client: &http.Client{}, LoggerService: &logging.Service{}}
	s.isInitialized.Store(true)

	country, err := s.Lookup("  K1ABC  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if country.Name != "Unknown" {
		t.Fatalf("expected Name to be callsign, got %q", country.Name)
	}
}

func TestService_Lookup_InvalidBaseURL(t *testing.T) {
	cfg := types.LookupConfig{Enabled: true, URL: "://bad", UserAgent: "test"}
	s := &Service{Config: &cfg, client: &http.Client{}, LoggerService: &logging.Service{}}
	s.isInitialized.Store(true)

	_, err := s.Lookup("K1ABC")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestService_Lookup_HTTP404(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	cfg := types.LookupConfig{Enabled: true, URL: ts.URL, UserAgent: "test"}
	s := &Service{Config: &cfg, client: ts.Client(), LoggerService: &logging.Service{}}
	s.isInitialized.Store(true)

	_, err := s.Lookup("K1ABC")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, smerrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestService_Lookup_HTTPNon2xx(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	cfg := types.LookupConfig{Enabled: true, URL: ts.URL, UserAgent: "test"}
	s := &Service{Config: &cfg, client: ts.Client(), LoggerService: &logging.Service{}}
	s.isInitialized.Store(true)

	_, err := s.Lookup("K1ABC")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestService_Lookup_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","found":true,"_t":"2025-11-30T13:31:07.321Z","continent":"EU","countryName":"TestLand","cqZone":14,"ituZone":28,"prefix":"K1","primaryDXCCPrefix":"K","countryCode":"TL","timeOffset":"+02:00"}`))
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	cfg := types.LookupConfig{Enabled: true, URL: ts.URL, UserAgent: "test"}
	s := &Service{Config: &cfg, client: ts.Client(), LoggerService: &logging.Service{}}
	s.isInitialized.Store(true)

	country, err := s.Lookup("K1ABC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if country.Name != "TestLand" {
		t.Fatalf("unexpected Name: %q", country.Name)
	}
	if country.Prefix != "K1" {
		t.Fatalf("unexpected Prefix: %q", country.Prefix)
	}
	if country.Ccode != "TL" {
		t.Fatalf("unexpected Ccode: %q", country.Ccode)
	}
	if country.Continent != "EU" {
		t.Fatalf("unexpected Continent: %q", country.Continent)
	}
	if country.CQZone != "14" {
		t.Fatalf("unexpected CQZone: %q", country.CQZone)
	}
	if country.ITUZone != "28" {
		t.Fatalf("unexpected ITUZone: %q", country.ITUZone)
	}
	if country.DXCCPrefix != "K" {
		t.Fatalf("unexpected DXCC: %q", country.DXCCPrefix)
	}
	if country.TimeOffset != "+02:00" {
		t.Fatalf("unexpected TimeOffset: %q", country.TimeOffset)
	}
}

func TestService_unmarshalResponse_FoundFalse(t *testing.T) {
	s := &Service{}
	body := []byte(`{"status":"ok","found":false,"_t":"2025-11-30T13:31:07.321Z"}`)

	_, err := s.unmarshalResponse(body)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, smerrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestService_unmarshalResponse_LocalTimeFallback(t *testing.T) {
	s := &Service{}
	body := []byte(`{"status":"ok","found":true,"_t":"2025-11-30T13:31:07.321Z","countryName":"TestLand","prefix":"K1","cqZone":14,"ituZone":28,"localTime":"2025-11-30T13:31:07+02:00"}`)

	country, err := s.unmarshalResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if country.TimeOffset != "+02:00" {
		t.Fatalf("expected TimeOffset from LocalTime, got %q", country.TimeOffset)
	}
}

// --- IsNetworkError tests ---

func TestIsNetworkError_Nil(t *testing.T) {
	if IsNetworkError(nil) {
		t.Fatalf("expected false for nil error")
	}
}

func TestIsNetworkError_NonNetwork(t *testing.T) {
	if IsNetworkError(errors.New("x")) {
		t.Fatalf("expected false for non-network error")
	}
}

func TestIsNetworkError_Network(t *testing.T) {
	_, err := net.Dial("tcp", "192.0.2.1:65535")
	if err == nil {
		t.Skip("unexpectedly connected; cannot test network error")
	}
	if !IsNetworkError(err) {
		t.Fatalf("expected true for network error, got false; err=%v", err)
	}
}

func TestService_LookupWithContext_Canceled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","found":true,"_t":"2025-11-30T13:31:07.321Z","countryName":"TestLand","prefix":"K1","cqZone":14,"ituZone":28}`))
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	cfg := types.LookupConfig{Enabled: true, URL: ts.URL, UserAgent: "test"}
	s := &Service{Config: &cfg, client: ts.Client(), LoggerService: &logging.Service{}}
	s.isInitialized.Store(true)

	dialCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.LookupWithContext(dialCtx, "K1ABC")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to perform HTTP GET request") {
		t.Fatalf("expected wrapped context cancellation error, got %v", err)
	}
}

func TestService_Lookup_NoHTTPClient(t *testing.T) {
	cfg := types.LookupConfig{Enabled: true, URL: "http://example.com", UserAgent: "test"}
	s := &Service{Config: &cfg, LoggerService: &logging.Service{}}
	s.isInitialized.Store(true)

	_, err := s.Lookup("K1ABC")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "http client is not configured") {
		t.Fatalf("expected 'http client is not configured' error, got %v", err)
	}
}
