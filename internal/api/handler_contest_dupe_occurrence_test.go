package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// W-0023 weekend slice: with contest and start, /v1/contest-dupe asks about
// this contest occurrence only — a contact worked before the contest is not a
// contest duplicate.
func TestContestDupe_ScopedToTheOccurrence(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Main", "7Q5MLV")
	submitQso(t, srv, lbID, testQsoADIF, false) // M0CMC 40m SSB, no contest fields
	member := strings.Replace(testQsoADIF, "<EOR>",
		"<CONTEST_ID:9>UKEIDXSSB<APP_SM_CONTEST_START:8>20261031<EOR>", 1)
	member = strings.Replace(member, "M0CMC", "G4ABC", 1)
	submitQso(t, srv, lbID, member, false) // G4ABC 40m, a member of the 2026 event

	get := func(q string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/contest-dupe?logbook=%d&band=40m&%s", lbID, q), nil)
		w := httptest.NewRecorder()
		srv.handleContestDupe(w, req)
		return w
	}
	for _, c := range []struct {
		q    string
		want string
	}{
		{"call=G4ABC&contest=UKEIDXSSB&start=20261031", `"duplicate":true`},
		{"call=M0CMC&contest=UKEIDXSSB&start=20261031", `"duplicate":false`}, // worked before the contest
		{"call=G4ABC&contest=UKEIDXSSB&start=20271023", `"duplicate":false`}, // another edition
		{"call=M0CMC", `"duplicate":true`},                                   // unscoped: unchanged
	} {
		w := get(c.q)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %s, want %s", c.q, w.Code, w.Body.String(), c.want)
		}
	}
	for _, q := range []string{"call=G4ABC&contest=UKEIDXSSB", "call=G4ABC&start=20261031",
		"call=G4ABC&contest=UK%20EI&start=20261031", "call=G4ABC&contest=UKEIDXSSB&start=2026-10-31"} {
		if w := get(q); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, w.Code)
		}
	}
}
