package qrzcq

import (
	"context"
	stderr "errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func responseClient(status int, body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
}

func initializedService(client *http.Client) *Service {
	s := NewService(&logging.Service{}, nil, &types.LookupConfig{
		Name: ServiceName, Enabled: true, URL: "https://example.invalid/xml",
		Username: "7Q5MLV", Password: "premium-password", HttpTimeoutSec: 5,
	}, client)
	s.UserAgent = "station-manager/test"
	s.setSessionKey("test-session-key")
	s.isInitialized.Store(true)
	return s
}

func TestInitialize_DisabledNeedsNoCredentialsOrNetwork(t *testing.T) {
	s := NewService(&logging.Service{}, nil, &types.LookupConfig{Enabled: false}, nil)
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize disabled service: %v", err)
	}
	if !s.isInitialized.Load() {
		t.Fatal("disabled service was not marked initialized")
	}
	if got, err := s.Lookup(" n0w "); err != nil || got.Call != "N0W" {
		t.Fatalf("disabled Lookup = %+v, %v; want normalized call sentinel", got, err)
	}
}

func TestInitialize_RejectsUnsafeOrIncompleteConfig(t *testing.T) {
	tests := []struct {
		name      string
		cfg       types.LookupConfig
		userAgent string
	}{
		{name: "empty URL", cfg: types.LookupConfig{Enabled: true, Username: "abc", Password: "abcde", HttpTimeoutSec: 1}, userAgent: "agent"},
		{name: "insecure remote URL", cfg: types.LookupConfig{Enabled: true, URL: "http://qrzcq.com/xml", Username: "abc", Password: "abcde", HttpTimeoutSec: 1}, userAgent: "agent"},
		{name: "empty user agent", cfg: types.LookupConfig{Enabled: true, URL: DefaultURL, Username: "abc", Password: "abcde", HttpTimeoutSec: 1}},
		{name: "zero timeout", cfg: types.LookupConfig{Enabled: true, URL: DefaultURL, Username: "abc", Password: "abcde"}, userAgent: "agent"},
		{name: "short username", cfg: types.LookupConfig{Enabled: true, URL: DefaultURL, Username: "ab", Password: "abcde", HttpTimeoutSec: 1}, userAgent: "agent"},
		{name: "short password", cfg: types.LookupConfig{Enabled: true, URL: DefaultURL, Username: "abc", Password: "abcd", HttpTimeoutSec: 1}, userAgent: "agent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService(&logging.Service{}, nil, &tc.cfg, nil)
			s.UserAgent = tc.userAgent
			if err := s.Initialize(context.Background()); err == nil {
				t.Fatal("Initialize succeeded, want configuration error")
			}
		})
	}
}

func TestLookup_ClassifiesNotFoundAndBadResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		wantNF bool
	}{
		{name: "not found", status: http.StatusOK, body: `<QRZCQDatabase><Session><Error>Not found: N0CALL</Error></Session></QRZCQDatabase>`, wantNF: true},
		{name: "missing callsign", status: http.StatusOK, body: `<QRZCQDatabase><Session><Key>k</Key></Session></QRZCQDatabase>`, wantNF: true},
		{name: "malformed XML", status: http.StatusOK, body: `<QRZCQDatabase>`},
		{name: "server failure", status: http.StatusBadGateway, body: `upstream unavailable`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := initializedService(responseClient(tc.status, tc.body))
			_, err := s.Lookup("N0CALL")
			if err == nil {
				t.Fatal("Lookup succeeded, want error")
			}
			if got := stderr.Is(err, errors.ErrNotFound); got != tc.wantNF {
				t.Fatalf("errors.Is(ErrNotFound) = %v, want %v (err=%v)", got, tc.wantNF, err)
			}
		})
	}
}

func TestLookup_RejectsOversizedSuccessBody(t *testing.T) {
	body := strings.Repeat("x", successBodyLimit+1)
	_, err := initializedService(responseClient(http.StatusOK, body)).Lookup("N0CALL")
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("Lookup error = %v, want bounded-response error", err)
	}
}

func TestScrubURLError_RemovesCredentialQuery(t *testing.T) {
	ue := &url.Error{
		Op:  "Get",
		URL: "https://ssl.qrzcq.com/xml?username=7Q5MLV&password=TOPSECRET&s=SESSIONKEY",
		Err: stderr.New("dial timeout"),
	}
	got := scrubURLError(ue).Error()
	for _, secret := range []string{"TOPSECRET", "SESSIONKEY", "password="} {
		if strings.Contains(got, secret) {
			t.Fatalf("scrubbed error leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "ssl.qrzcq.com/xml") {
		t.Fatalf("scrubbed error lost endpoint identity: %s", got)
	}
}

func TestLookup_TransportFailureDoesNotLeakSessionKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: stderr.New("offline")}
	})}
	_, err := initializedService(client).Lookup("N0CALL")
	if err == nil {
		t.Fatal("Lookup succeeded, want transport error")
	}
	if strings.Contains(err.Error(), "test-session-key") || strings.Contains(err.Error(), "s=") {
		t.Fatalf("transport error leaked session query: %v", err)
	}
}

func TestUpstreamErrorsDoNotEchoCredentials(t *testing.T) {
	t.Run("account password", func(t *testing.T) {
		s := NewService(&logging.Service{}, nil, &types.LookupConfig{
			Name: ServiceName, Enabled: true, URL: "https://example.invalid/xml",
			Username: "7Q5MLV", Password: "premium-password", HttpTimeoutSec: 5,
		}, responseClient(http.StatusOK,
			`<QRZCQDatabase><Session><Error>bad password premium-password</Error></Session></QRZCQDatabase>`))
		s.UserAgent = "station-manager/test"
		err := s.requestAndSetSessionKey(context.Background())
		if err == nil || strings.Contains(err.Error(), "premium-password") {
			t.Fatalf("session error = %v, want failure with password redacted", err)
		}
	})

	t.Run("session key", func(t *testing.T) {
		s := initializedService(responseClient(http.StatusOK,
			`<QRZCQDatabase><Session><Error>bad key test-session-key</Error></Session></QRZCQDatabase>`))
		_, err := s.Lookup("N0CALL")
		if err == nil || strings.Contains(err.Error(), "test-session-key") {
			t.Fatalf("lookup error = %v, want failure with session key redacted", err)
		}
	})
}
