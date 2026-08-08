package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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

// GET resolves the caller-answer mode — default operator_pick on a fresh
// config (operator-ratified 2026-08-08: automation is an explicit opt-in; a
// clean install must not auto-work anyone). Since ADR 0066 this field is the
// session selector's SEED; PUT accepts all three literals (fork 4).
func TestConfig_Ft8CallerAnswerMode_GetReturnsDefault(t *testing.T) {
	srv := testServer(t)
	resp := ft8GetConfig(t, srv)
	if resp.Ft8CallerAnswerMode == nil {
		t.Fatal("ft8_caller_answer_mode absent on GET; want resolved default")
	}
	if *resp.Ft8CallerAnswerMode != types.Ft8CallerAnswerOperatorPick {
		t.Errorf("mode = %q, want %q", *resp.Ft8CallerAnswerMode, types.Ft8CallerAnswerOperatorPick)
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

// ADR 0066 fork 4: the PUT edits the DEFAULT (the session selector is the live
// control now), so all THREE literals are accepted — including operator_pick,
// which the retired ADR 0065 fence used to 400. Junk still 400s: silently
// resolving a typo to a default the operator never chose is the confusable
// state. The GET/PUT asymmetry this retires is documented at the validation
// site's history comment.
func TestConfig_Ft8CallerAnswerMode_AllThreeLiteralsAccepted(t *testing.T) {
	srv := testServer(t)
	for _, mode := range []string{"auto_first", "auto_strongest", "operator_pick"} {
		if w := ft8PutConfig(t, srv, `{"ft8_caller_answer_mode":"`+mode+`"}`); w.Code != http.StatusOK {
			t.Fatalf("PUT %q = %d, want 200 (body %s)", mode, w.Code, w.Body.String())
		}
		resp := ft8GetConfig(t, srv)
		if resp.Ft8CallerAnswerMode == nil || *resp.Ft8CallerAnswerMode != mode {
			t.Fatalf("after PUT %q, GET = %v", mode, resp.Ft8CallerAnswerMode)
		}
	}
}

func TestConfig_Ft8CallerAnswerMode_JunkValue400(t *testing.T) {
	srv := testServer(t)
	w := ft8PutConfig(t, srv, `{"ft8_caller_answer_mode":"bogus"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT bogus = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w); code != "invalid_field_value" {
		t.Fatalf("code = %q, want invalid_field_value", code)
	}
}

// GET serves the resolved default repeat cap (6) on a fresh config, so the Settings-
// tab field has a value to render without a prior save.
func TestConfig_Ft8MaxRepeats_GetReturnsDefault(t *testing.T) {
	srv := testServer(t)
	resp := ft8GetConfig(t, srv)
	if resp.Ft8MaxRepeats == nil {
		t.Fatal("ft8_max_repeats absent on GET; want resolved default")
	}
	if *resp.Ft8MaxRepeats != types.DefaultFt8MaxRepeats {
		t.Errorf("max_repeats = %d, want default %d", *resp.Ft8MaxRepeats, types.DefaultFt8MaxRepeats)
	}
}

// A PUT carrying ft8_max_repeats persists it; a follow-up GET reflects it.
func TestConfig_Ft8MaxRepeats_PutUpdates(t *testing.T) {
	srv := testServer(t)
	if w := ft8PutConfig(t, srv, `{"ft8_max_repeats":3}`); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d, body %s", w.Code, w.Body.String())
	}
	resp := ft8GetConfig(t, srv)
	if resp.Ft8MaxRepeats == nil || *resp.Ft8MaxRepeats != 3 {
		t.Fatalf("after PUT, ft8_max_repeats = %v, want 3", resp.Ft8MaxRepeats)
	}
}

// Out-of-range values are a loud 400 (strict-wire contract), not silently clamped:
// below 1 and above the hard ceiling both reject.
func TestConfig_Ft8MaxRepeats_InvalidValue400(t *testing.T) {
	srv := testServer(t)
	for _, bad := range []int{0, types.Ft8MaxRepeatsCeiling + 1} {
		w := ft8PutConfig(t, srv, `{"ft8_max_repeats":`+strconv.Itoa(bad)+`}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("PUT %d = %d, want 400 (body %s)", bad, w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_field_value" {
			t.Errorf("PUT %d code = %q, want invalid_field_value", bad, code)
		}
	}
}

// A PUT that omits ft8_max_repeats leaves a previously-stored value untouched —
// the presence-aware contract.
func TestConfig_Ft8MaxRepeats_OmittedPutDoesNotClobber(t *testing.T) {
	srv := testServer(t)
	if w := ft8PutConfig(t, srv, `{"ft8_max_repeats":4}`); w.Code != http.StatusOK {
		t.Fatalf("seed PUT = %d, body %s", w.Code, w.Body.String())
	}
	if w := ft8PutConfig(t, srv, `{"ft8_display":{"feed_mode":"single"}}`); w.Code != http.StatusOK {
		t.Fatalf("display PUT = %d, body %s", w.Code, w.Body.String())
	}
	resp := ft8GetConfig(t, srv)
	if resp.Ft8MaxRepeats == nil || *resp.Ft8MaxRepeats != 4 {
		t.Fatalf("ft8_max_repeats clobbered by an omitted PUT: %v", resp.Ft8MaxRepeats)
	}
}

// GET serves an empty Field Day block on a fresh config (no defaults — FD is once
// a year, so unset is the normal state), giving the SPA a stable {class, section}.
func TestConfig_Ft8FieldDay_GetReturnsEmpty(t *testing.T) {
	srv := testServer(t)
	resp := ft8GetConfig(t, srv)
	if resp.Ft8FieldDay == nil {
		t.Fatal("ft8_field_day absent on GET; want an empty block")
	}
	if resp.Ft8FieldDay.Class != "" || resp.Ft8FieldDay.Section != "" {
		t.Errorf("ft8_field_day = %+v, want empty", resp.Ft8FieldDay)
	}
}

// A PUT persists the Field Day exchange + RST_RCVD default; class/section upper-case,
// the RST default kept verbatim (case meaningless for a report).
func TestConfig_Ft8FieldDay_PutUpdatesAndUpperCases(t *testing.T) {
	srv := testServer(t)
	if w := ft8PutConfig(t, srv,
		`{"ft8_field_day":{"class":"2a","section":"dx","default_rst_rcvd":"59"}}`); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d, body %s", w.Code, w.Body.String())
	}
	resp := ft8GetConfig(t, srv)
	if resp.Ft8FieldDay == nil || resp.Ft8FieldDay.Class != "2A" ||
		resp.Ft8FieldDay.Section != "DX" || resp.Ft8FieldDay.DefaultRstRcvd != "59" {
		t.Fatalf("after PUT, ft8_field_day = %+v (want 2A/DX/59)", resp.Ft8FieldDay)
	}
}

// A PUT that omits ft8_field_day leaves a previously-stored value untouched.
func TestConfig_Ft8FieldDay_OmittedPutDoesNotClobber(t *testing.T) {
	srv := testServer(t)
	if w := ft8PutConfig(t, srv, `{"ft8_field_day":{"class":"3A","section":"EMA"}}`); w.Code != http.StatusOK {
		t.Fatalf("seed PUT = %d, body %s", w.Code, w.Body.String())
	}
	if w := ft8PutConfig(t, srv, `{"ft8_display":{"feed_mode":"single"}}`); w.Code != http.StatusOK {
		t.Fatalf("display PUT = %d, body %s", w.Code, w.Body.String())
	}
	resp := ft8GetConfig(t, srv)
	if resp.Ft8FieldDay == nil || resp.Ft8FieldDay.Class != "3A" || resp.Ft8FieldDay.Section != "EMA" {
		t.Fatalf("ft8_field_day clobbered by an omitted PUT: %+v", resp.Ft8FieldDay)
	}
}

// A malformed class (and a malformed section) are each a 400 via the shared Validate.
func TestConfig_Ft8FieldDay_InvalidValue400(t *testing.T) {
	srv := testServer(t)
	for _, body := range []string{
		`{"ft8_field_day":{"class":"99Z"}}`,                    // bad category
		`{"ft8_field_day":{"class":"2A","section":"TOOLONG"}}`, // section too long
	} {
		w := ft8PutConfig(t, srv, body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s = %d, want 400 (body %s)", body, w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_field_value" {
			t.Errorf("PUT %s code = %q, want invalid_field_value", body, code)
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
