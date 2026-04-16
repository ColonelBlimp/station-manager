package qrz

//
//import (
//	"errors"
//	"io"
//	"net/http"
//	"net/url"
//	"strings"
//	"testing"
//	"time"
//
//	"github.com/7Q-Station-Manager/config"
//	"github.com/7Q-Station-Manager/logging"
//	"github.com/7Q-Station-Manager/types"
//)
//
//type rtFunc func(*http.Request) (*http.Response, error)
//
//func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
//
//func newTestHTTPClient(fn rtFunc) *http.Client { return &http.Client{Transport: fn} }
//
//func baseClient(t *testing.T, cfg types.ForwarderConfig, httpClient *http.Client) *Service {
//	t.Helper()
//	c := &Service{
//		LoggerService: &logging.Service{},
//		Config:        &types.ForwarderConfig{Name: cfg.Name, Enabled: cfg.Enabled, URL: cfg.URL, APIKey: cfg.APIKey, UserAgent: cfg.UserAgent, HttpTimeout: cfg.HttpTimeout},
//		client:        httpClient,
//	}
//	c.initialized.Store(false)
//	return c
//}
//
//func TestInitialise_ErrorsWhenServicesMissing(t *testing.T) {
//	c := &Service{}
//	if err := c.Initialise(); err == nil || !strings.Contains(err.Error(), "logger has not been set") {
//		t.Fatalf("expected logger missing error, got %v", err)
//	}
//	c.LoggerService = &logging.Service{}
//	if err := c.Initialise(); err == nil || !strings.Contains(err.Error(), "application config has not been set") {
//		t.Fatalf("expected config missing error, got %v", err)
//	}
//	c.ConfigService = &config.Service{}
//	if err := c.Initialise(); err == nil || !strings.Contains(err.Error(), "database has not been set") {
//		t.Fatalf("expected database missing error, got %v", err)
//	}
//}
//
//func TestIsEnabled_ReflectsConfig(t *testing.T) {
//	cfg := types.ForwarderConfig{Name: "qrz", Enabled: true}
//	c := baseClient(t, cfg, newTestHTTPClient(rtFunc(func(r *http.Request) (*http.Response, error) {
//		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("RESULT=OK&LOGID=1"))}, nil
//	})))
//	c.initialized.Store(true)
//	if !c.IsEnabled() {
//		t.Fatalf("expected enabled true")
//	}
//}
//
//func TestForward_NotInitialized(t *testing.T) {
//	cfg := types.ForwarderConfig{Name: "qrz"}
//	c := baseClient(t, cfg, nil)
//	if err := c.Forward(types.Qso{}); err == nil || !strings.Contains(err.Error(), "client not initialized") {
//		t.Fatalf("expected not initialized error, got %v", err)
//	}
//}
//
//func TestForward_HTTPError(t *testing.T) {
//	cfg := types.ForwarderConfig{Name: "qrz", URL: "https://example.invalid", APIKey: "k", HttpTimeout: time.Second}
//	client := newTestHTTPClient(rtFunc(func(r *http.Request) (*http.Response, error) {
//		return nil, errors.New("dial failed")
//	}))
//	c := baseClient(t, cfg, client)
//	c.initialized.Store(true)
//	if err := c.Forward(types.Qso{}); err == nil || !strings.Contains(err.Error(), "dial failed") {
//		t.Fatalf("expected http error, got %v", err)
//	}
//}
//
//func TestForward_ParseError(t *testing.T) {
//	cfg := types.ForwarderConfig{Name: "qrz", URL: "https://example", APIKey: "k", HttpTimeout: time.Second}
//	client := newTestHTTPClient(rtFunc(func(r *http.Request) (*http.Response, error) {
//		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("%bad%encoding%"))}, nil
//	}))
//	c := baseClient(t, cfg, client)
//	c.initialized.Store(true)
//	// Provide minimal QSO for adif conversion
//	q := types.Qso{}
//	if err := c.Forward(q); err == nil || !strings.Contains(err.Error(), "parsing response data") {
//		t.Fatalf("expected parse error, got %v", err)
//	}
//}
//
//func TestForward_FailResult_ReturnsError(t *testing.T) {
//	cfg := types.ForwarderConfig{Name: "qrz", URL: "https://example", APIKey: "k", HttpTimeout: time.Second}
//	client := newTestHTTPClient(rtFunc(func(r *http.Request) (*http.Response, error) {
//		// minimal valid urlencoded body
//		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("RESULT=FAIL&REASON=bad%20api%20key"))}, nil
//	}))
//	c := baseClient(t, cfg, client)
//	c.initialized.Store(true)
//	q := types.Qso{}
//	if err := c.Forward(q); err == nil || !strings.Contains(err.Error(), "insert failed for QRZ.com") {
//		t.Fatalf("expected fail result error, got %v", err)
//	}
//}
//
//func TestForward_SendsFormAndHeaders_AndSuccess(t *testing.T) {
//	var captured *http.Request
//	cfg := types.ForwarderConfig{Name: "qrz", URL: "https://example/ingest", APIKey: "api-key", HttpTimeout: time.Second, UserAgent: "UA/1.0"}
//	client := newTestHTTPClient(rtFunc(func(r *http.Request) (*http.Response, error) {
//		captured = r
//		// success response
//		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("RESULT=OK&LOGID=123"))}, nil
//	}))
//	c := baseClient(t, cfg, client)
//	c.initialized.Store(true)
//
//	q := types.Qso{}
//	if err := c.Forward(q); err != nil {
//		t.Fatalf("unexpected error: %v", err)
//	}
//
//	if captured == nil {
//		t.Fatalf("request not captured")
//	}
//	if captured.Method != http.MethodPost {
//		t.Errorf("expected POST, got %s", captured.Method)
//	}
//	if captured.URL.String() != cfg.URL {
//		t.Errorf("expected URL %s got %s", cfg.URL, captured.URL.String())
//	}
//	if ua := captured.Header.Get("User-Agent"); ua != cfg.UserAgent {
//		t.Errorf("expected UA %s got %s", cfg.UserAgent, ua)
//	}
//	if ct := captured.Header.Get("Content-Type"); !strings.Contains(ct, "application/x-www-form-urlencoded") {
//		t.Errorf("expected form content type, got %s", ct)
//	}
//
//	// Check form body
//	bodyBytes, _ := io.ReadAll(captured.Body)
//	vals, _ := url.ParseQuery(string(bodyBytes))
//	if vals.Get("KEY") != cfg.APIKey {
//		t.Errorf("expected KEY=%s got %s", cfg.APIKey, vals.Get("KEY"))
//	}
//	if vals.Get("ACTION") != "INSERT" {
//		t.Errorf("expected ACTION=INSERT got %s", vals.Get("ACTION"))
//	}
//	if vals.Get("ADIF") == "" {
//		t.Errorf("expected ADIF present")
//	}
//}
