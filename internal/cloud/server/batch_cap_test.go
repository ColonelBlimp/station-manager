package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// AW-6 P2 (codex f25cf768): the streaming decoder's single-document check must surface a
// body-limit error. A valid QSO request followed by trailing whitespace over the byte cap
// is 413 body_too_large, not a silent accept — dec.More() would suppress the read error.
func TestPutQsos_TrailingWhitespaceOversize_Is413(t *testing.T) {
	s := quietServer()
	body := io.MultiReader(
		strings.NewReader(`{"logbook":"main","qsos":[{}]}`),
		&filler{n: maxBodyBytes, b: ' '}, // trailing whitespace past the cap
	)
	w := httptest.NewRecorder()
	s.handlePutQsos(w, httptest.NewRequest(http.MethodPut, "/v1/qsos", body))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("trailing oversize: status = %d, want 413; body %s", w.Code, w.Body.String())
	}
	if code := errCode(t, w); code != "body_too_large" {
		t.Errorf("code = %q, want body_too_large", code)
	}
}

// AW-6: the SM Cloud QSO batch has a maximum row count. A batch over the cap is rejected
// with batch_too_large BEFORE the record slice is allocated, the logbook is provisioned,
// a store method is called, or a transaction begins — so a Server with no store can drive
// it: a correct cap returns before touching either. The 32 MiB byte cap is a separate,
// independent bound (a minimal row lets that cap admit far more than maxQsoBatchRows).
func TestPutQsos_BatchRowCap(t *testing.T) {
	s := quietServer() // no store/db: a correct cap rejects before touching either

	// Over the cap → batch_too_large, before validation / provisioning / store.
	over := httptest.NewRecorder()
	s.handlePutQsos(over, httptest.NewRequest(http.MethodPut, "/v1/qsos",
		strings.NewReader(putQsosBatchBody("main", maxQsoBatchRows+1))))
	if over.Code != http.StatusBadRequest {
		t.Fatalf("over-cap: status = %d, want 400; body %s", over.Code, over.Body.String())
	}
	if code := errCode(t, over); code != "batch_too_large" {
		t.Errorf("over-cap: code = %q, want batch_too_large", code)
	}

	// At the cap → NOT rejected as too large. It fails later on the empty rows' content,
	// which proves maxQsoBatchRows is admitted (the boundary is cap+1, not cap).
	atCap := httptest.NewRecorder()
	s.handlePutQsos(atCap, httptest.NewRequest(http.MethodPut, "/v1/qsos",
		strings.NewReader(putQsosBatchBody("main", maxQsoBatchRows))))
	if code := errCode(t, atCap); code == "batch_too_large" {
		t.Errorf("at-cap (%d rows) must be within the boundary, got batch_too_large", maxQsoBatchRows)
	}

	// The cap stops the DECODE, not just the DB work: a batch whose (cap+1)th element is
	// malformed still returns batch_too_large, because streaming refuses that element
	// before parsing it — the unbounded array is never fully decoded (codex e7534e37 P1).
	// A whole-array decode would instead hit the malformed element and return a parse
	// error (invalid_body).
	early := httptest.NewRecorder()
	earlyBody := `{"logbook":"main","qsos":[` +
		strings.TrimSuffix(strings.Repeat("{},", maxQsoBatchRows), ",") + `,not-valid-json]}`
	s.handlePutQsos(early, httptest.NewRequest(http.MethodPut, "/v1/qsos", strings.NewReader(earlyBody)))
	if code := errCode(t, early); code != "batch_too_large" {
		t.Errorf("cap must stop the decode before the malformed (cap+1)th element, got %q", code)
	}
}

// putQsosBatchBody builds a PUT /v1/qsos body with n empty QSO upload objects — enough to
// exercise the row-count boundary without valid content, since the cap is checked first.
func putQsosBatchBody(logbook string, n int) string {
	return `{"logbook":"` + logbook + `","qsos":[` + strings.TrimSuffix(strings.Repeat("{},", n), ",") + `]}`
}

// AW-6 P2 (codex f0b32395): a duplicate qsos member is ambiguous under the streaming cap
// (the struct decode this replaced took the LAST value); reject it — case-insensitively,
// matching encoding/json's field matching — rather than silently change which rows are
// written.
func TestPutQsos_DuplicateQsosMemberRejected(t *testing.T) {
	s := quietServer()
	for _, body := range []string{
		`{"logbook":"main","qsos":[{}],"qsos":[]}`,
		`{"logbook":"main","qsos":[{}],"QSOS":[{}]}`,
		// A Unicode simple-fold spelling (U+017F LONG S) is recognized as qsos —
		// encoding/json's field folding, which strings.ToLower would miss — so this is a
		// duplicate, not an ignored unknown key (codex 5be97c03).
		`{"logbook":"main","qsos":[{}],"qſoſ":[{}]}`,
	} {
		w := httptest.NewRecorder()
		s.handlePutQsos(w, httptest.NewRequest(http.MethodPut, "/v1/qsos", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest || errCode(t, w) != "invalid_body" {
			t.Errorf("duplicate qsos %q: status=%d code=%q, want 400 invalid_body",
				body, w.Code, errCode(t, w))
		}
	}

	// A duplicate logbook (a scalar) or repeated unknown field is NOT a cap ambiguity —
	// the prior struct decode's last-value-wins / ignore-unknown behavior is preserved,
	// so the request reaches validation rather than being rejected as a duplicate.
	for _, body := range []string{
		`{"logbook":"old","logbook":"main","qsos":[{}]}`,
		`{"logbook":"main","extra":1,"extra":2,"qsos":[{}]}`,
	} {
		w := httptest.NewRecorder()
		s.handlePutQsos(w, httptest.NewRequest(http.MethodPut, "/v1/qsos", strings.NewReader(body)))
		if code := errCode(t, w); code == "invalid_body" {
			t.Errorf("non-qsos duplicate %q must be tolerated, got invalid_body: %s", body, w.Body.String())
		}
	}
}
