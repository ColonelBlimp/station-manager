package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func ft8GetConfig(t *testing.T, srv *Server) ConfigResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/config = %d, body %s", w.Code, w.Body.String())
	}
	var resp ConfigResponse
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return resp
}

func ft8PutConfig(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	return w
}

// GET always returns ft8_display resolved with defaults, even on a fresh config
// (so the SPA Settings tab has values to render without a prior save).
func TestConfig_Ft8Display_GetReturnsResolvedDefaults(t *testing.T) {
	srv := testServer(t)
	resp := ft8GetConfig(t, srv)
	if resp.Ft8Display == nil {
		t.Fatal("ft8_display absent on GET; want resolved defaults")
	}
	if resp.Ft8Display.HistoryMax != types.DefaultFt8HistoryMax {
		t.Errorf("HistoryMax = %d, want default %d", resp.Ft8Display.HistoryMax, types.DefaultFt8HistoryMax)
	}
	if resp.Ft8Display.FeedMode != types.DefaultFt8FeedMode {
		t.Errorf("FeedMode = %q, want default %q", resp.Ft8Display.FeedMode, types.DefaultFt8FeedMode)
	}
	if resp.Ft8Display.HighlightUnworked != types.DefaultFt8HighlightUnworked {
		t.Errorf("HighlightUnworked = %q, want default", resp.Ft8Display.HighlightUnworked)
	}
}

// A PUT carrying ft8_display persists it; the response (and a follow-up GET)
// reflect the normalised values.
func TestConfig_Ft8Display_PutUpdates(t *testing.T) {
	srv := testServer(t)
	w := ft8PutConfig(t, srv,
		`{"ft8_display":{"history_max":250,"feed_mode":"single","highlight_unworked":"#abcdef","highlight_worked":"#123456"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT = %d, body %s", w.Code, w.Body.String())
	}
	resp := ft8GetConfig(t, srv)
	got := resp.Ft8Display
	if got == nil || got.HistoryMax != 250 || got.FeedMode != "single" ||
		got.HighlightUnworked != "#abcdef" || got.HighlightWorked != "#123456" {
		t.Fatalf("after PUT, ft8_display = %+v", got)
	}
}

// A PUT that omits ft8_display (a My Station-style save) must NOT clobber a
// previously-stored ft8_display — the presence-aware contract.
func TestConfig_Ft8Display_OmittedPutDoesNotClobber(t *testing.T) {
	srv := testServer(t)
	if w := ft8PutConfig(t, srv, `{"ft8_display":{"feed_mode":"single","history_max":250}}`); w.Code != http.StatusOK {
		t.Fatalf("seed PUT = %d, body %s", w.Code, w.Body.String())
	}
	// Simulate a My Station save: logging_station only, no ft8_display.
	if w := ft8PutConfig(t, srv, `{"logging_station":{"station_callsign":"G0TST"}}`); w.Code != http.StatusOK {
		t.Fatalf("station PUT = %d, body %s", w.Code, w.Body.String())
	}
	resp := ft8GetConfig(t, srv)
	if resp.Ft8Display == nil || resp.Ft8Display.FeedMode != "single" || resp.Ft8Display.HistoryMax != 250 {
		t.Fatalf("ft8_display clobbered by an omitted PUT: %+v", resp.Ft8Display)
	}
}

// GET resolves the caller-answer mode (default auto_first on a fresh config) so
// the Settings-tab dropdown has a value to render without a prior save.
func TestConfig_Ft8CallerAnswerMode_GetReturnsDefault(t *testing.T) {
	srv := testServer(t)
	resp := ft8GetConfig(t, srv)
	if resp.Ft8CallerAnswerMode == nil {
		t.Fatal("ft8_caller_answer_mode absent on GET; want resolved default")
	}
	if *resp.Ft8CallerAnswerMode != types.Ft8CallerAnswerAutoFirst {
		t.Errorf("mode = %q, want %q", *resp.Ft8CallerAnswerMode, types.Ft8CallerAnswerAutoFirst)
	}
}

// A PUT carrying ft8_caller_answer_mode persists it; a follow-up GET reflects it.
func TestConfig_Ft8CallerAnswerMode_PutUpdates(t *testing.T) {
	srv := testServer(t)
	if w := ft8PutConfig(t, srv, `{"ft8_caller_answer_mode":"auto_strongest"}`); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d, body %s", w.Code, w.Body.String())
	}
	resp := ft8GetConfig(t, srv)
	if resp.Ft8CallerAnswerMode == nil || *resp.Ft8CallerAnswerMode != types.Ft8CallerAnswerAutoStrongest {
		t.Fatalf("after PUT, ft8_caller_answer_mode = %v", resp.Ft8CallerAnswerMode)
	}
}

// A PUT that omits ft8_caller_answer_mode must NOT reset a previously-stored value
// — the presence-aware contract (a My Station / display-only save leaves it alone).
func TestConfig_Ft8CallerAnswerMode_OmittedPutDoesNotClobber(t *testing.T) {
	srv := testServer(t)
	if w := ft8PutConfig(t, srv, `{"ft8_caller_answer_mode":"auto_strongest"}`); w.Code != http.StatusOK {
		t.Fatalf("seed PUT = %d, body %s", w.Code, w.Body.String())
	}
	if w := ft8PutConfig(t, srv, `{"ft8_display":{"feed_mode":"single"}}`); w.Code != http.StatusOK {
		t.Fatalf("display PUT = %d, body %s", w.Code, w.Body.String())
	}
	resp := ft8GetConfig(t, srv)
	if resp.Ft8CallerAnswerMode == nil || *resp.Ft8CallerAnswerMode != types.Ft8CallerAnswerAutoStrongest {
		t.Fatalf("caller-answer mode clobbered by an omitted PUT: %v", resp.Ft8CallerAnswerMode)
	}
}

// The SPA wire surface accepts only the two attended auto modes; operator_pick (a
// config.json-only literal, rejected at Call-CQ start) and any junk are a 400.
func TestConfig_Ft8CallerAnswerMode_InvalidValue400(t *testing.T) {
	srv := testServer(t)
	for _, bad := range []string{"operator_pick", "bogus"} {
		w := ft8PutConfig(t, srv, `{"ft8_caller_answer_mode":"`+bad+`"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("PUT %q = %d, want 400 (body %s)", bad, w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_field_value" {
			t.Errorf("PUT %q code = %q, want invalid_field_value", bad, code)
		}
	}
}

// An invalid feed_mode is a 400 (the one enum gets a friendly error; row
// cap / colours are normalised, not rejected).
func TestConfig_Ft8Display_InvalidFeedMode400(t *testing.T) {
	srv := testServer(t)
	w := ft8PutConfig(t, srv, `{"ft8_display":{"feed_mode":"bogus"}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w); code != "invalid_field_value" {
		t.Errorf("code = %q, want invalid_field_value", code)
	}
}
