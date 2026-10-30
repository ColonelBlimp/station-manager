package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// W-0023 weekend slice: POST /v1/contest-serial hands out the next sent serial
// for a contest in a logbook. Starting a contest QSO consumes it for good.

func contestSerial(t *testing.T, srv *Server, logbook, contest, draft string) *httptest.ResponseRecorder {
	t.Helper()
	return contestSerialAt(t, srv, logbook, contest, "20261031", draft)
}

func contestSerialAt(t *testing.T, srv *Server, logbook, contest, start, draft string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{}
	for k, v := range map[string]string{"logbook": logbook, "contest": contest, "start": start, "draft": draft} {
		if v != "" {
			q.Set(k, v)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/contest-serial?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	srv.handleContestSerial(w, req)
	return w
}

func TestContestSerial_AllocatesInSequenceAndRepeatsForTheSameDraft(t *testing.T) {
	srv := testServer(t)
	lb := fmt.Sprintf("%d", createTestLogbook(t, srv, "UK/EI DX 2026", "7Q5MLV"))

	for _, step := range []struct{ draft, want string }{
		{"a", `{"serial":1}`},
		{"a", `{"serial":1}`}, // lost-response retry
		{"b", `{"serial":2}`},
		{"c", `{"serial":3}`},
	} {
		w := contestSerial(t, srv, lb, "ukeidxssb", step.draft)
		if w.Code != http.StatusOK {
			t.Fatalf("draft %s: status = %d; body = %s", step.draft, w.Code, w.Body.String())
		}
		if got := w.Body.String(); got != step.want+"\n" && got != step.want {
			t.Fatalf("draft %s: body = %q, want %s", step.draft, got, step.want)
		}
	}
	// The contest id is case-insensitive (stored upper case, as CONTEST_ID).
	if w := contestSerial(t, srv, lb, "UKEIDXSSB", "d"); w.Body.String() != "{\"serial\":4}\n" {
		t.Fatalf("upper-case contest id: body = %q, want serial 4 in the same sequence", w.Body.String())
	}
}

func TestContestSerial_Refusals(t *testing.T) {
	srv := testServer(t)
	lb := fmt.Sprintf("%d", createTestLogbook(t, srv, "UK/EI DX 2026", "7Q5MLV"))
	for _, c := range []struct {
		name, logbook, contest, draft string
		status                        int
	}{
		{"missing logbook", "", "UKEIDXSSB", "a", http.StatusBadRequest},
		{"bad logbook", "x", "UKEIDXSSB", "a", http.StatusBadRequest},
		{"unknown logbook", "999", "UKEIDXSSB", "a", http.StatusNotFound},
		{"missing contest", lb, "", "a", http.StatusBadRequest},
		{"contest with a space", lb, "UK EI", "a", http.StatusBadRequest},
		{"missing draft", lb, "UKEIDXSSB", "", http.StatusBadRequest},
	} {
		if w := contestSerial(t, srv, c.logbook, c.contest, c.draft); w.Code != c.status {
			t.Errorf("%s: status = %d, want %d; body = %s", c.name, w.Code, c.status, w.Body.String())
		}
	}
	for _, start := range []string{"", "2026-10-31", "20261341", "2026103"} {
		if w := contestSerialAt(t, srv, lb, "UKEIDXSSB", start, "a"); w.Code != http.StatusBadRequest {
			t.Errorf("start %q: status = %d, want 400", start, w.Code)
		}
	}
	// None of the refusals consumed a number.
	if w := contestSerial(t, srv, lb, "UKEIDXSSB", "first"); w.Body.String() != "{\"serial\":1}\n" {
		t.Fatalf("first real allocation = %q, want serial 1", w.Body.String())
	}
}
