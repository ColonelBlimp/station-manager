package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// AW-2: state-control command booleans must be presence-aware. An absent,
// misspelled, duplicated, or unknown-field boolean must return a 400 that does NOT
// invoke the service — a 400 (rather than the service's 202/503) proves the handler
// returned before any bridge/ft8 call, so the daemon never executes the `false`
// operation the caller never expressed. An explicit true/false still reaches the
// service. Duplicate keys are rejected rather than resolved last-key-wins.
func TestCommandBooleanPresence(t *testing.T) {
	type endpoint struct {
		name  string
		field string
		post  func(t *testing.T, body string) *httptest.ResponseRecorder
	}
	endpoints := []endpoint{
		{"rig tune", "active", func(t *testing.T, body string) *httptest.ResponseRecorder {
			return postRigTune(t, tuneTestServer(t), body)
		}},
		{"ft8 tx arm", "armed", func(t *testing.T, body string) *httptest.ResponseRecorder {
			srv := ft8TxTestServer(t)
			return postFt8Tx(t, srv, "/v1/ft8/tx/arm", body, srv.handleFt8TxArm)
		}},
		{"ft8 qso skip", "armed", func(t *testing.T, body string) *httptest.ResponseRecorder {
			srv := ft8TxTestServer(t)
			return postFt8Tx(t, srv, "/v1/ft8/qso/skip", body, srv.handleFt8QsoSkip)
		}},
	}
	for _, e := range endpoints {
		t.Run(e.name, func(t *testing.T) {
			bad := []struct{ name, body, code string }{
				{"empty body", ``, "missing_required_field"},
				{"empty object", `{}`, "missing_required_field"},
				{"misspelled key", fmt.Sprintf(`{"%sX":true}`, e.field), "invalid_json"},
				{"unknown field", `{"totally_unknown":true}`, "invalid_json"},
				{"duplicate keys", fmt.Sprintf(`{"%s":true,"%s":false}`, e.field, e.field), "invalid_json"},
				// encoding/json field-matches case-insensitively; a differently-cased
				// alias must not slip past the duplicate check and reverse the command.
				{"case-variant duplicate", fmt.Sprintf(`{"%s":false,"%s":true}`, e.field, strings.ToUpper(e.field)), "invalid_json"},
				// Trailing content after the first JSON value must be rejected — a stray
				// closing delimiter or a second document is not a valid command body.
				{"trailing delimiter", fmt.Sprintf(`{"%s":true}}`, e.field), "invalid_json"},
				{"trailing document", fmt.Sprintf(`{"%s":true} {"x":1}`, e.field), "invalid_json"},
			}
			for _, c := range bad {
				t.Run(c.name, func(t *testing.T) {
					w := e.post(t, c.body)
					if w.Code != http.StatusBadRequest {
						t.Fatalf("status = %d, want 400 (service must not be invoked); body %s",
							w.Code, w.Body.String())
					}
					if code := decodeErrCode(t, w); code != c.code {
						t.Errorf("code = %q, want %q", code, c.code)
					}
				})
			}
			// An explicit boolean still reaches the service (a non-400 outcome).
			for _, v := range []string{"true", "false"} {
				t.Run("present "+v, func(t *testing.T) {
					w := e.post(t, fmt.Sprintf(`{"%s":%s}`, e.field, v))
					if w.Code == http.StatusBadRequest {
						t.Fatalf("present %s=%s must reach the service, got 400: %s",
							e.field, v, w.Body.String())
					}
				})
			}
		})
	}
}

// AW-2: duplicate detection must fold keys the way encoding/json matches fields, so a
// fold-equivalent alias (ſ folds to s) cannot slip a second value into the same field.
func TestTopLevelKeysUnique_FoldEquivalentDuplicate(t *testing.T) {
	if topLevelKeysUnique([]byte(`{"slot_utc":"a","ſlot_utc":"b"}`)) {
		t.Error("fold-equivalent duplicate keys must be detected (ſ folds to s)")
	}
	if topLevelKeysUnique([]byte(`{"armed":true,"ARMED":false}`)) {
		t.Error("case-variant duplicate keys must be detected")
	}
	if !topLevelKeysUnique([]byte(`{"their_call":"K1ABC","slot_utc":"x"}`)) {
		t.Error("distinct keys must be reported unique")
	}
}

// AW-2, allow-list half: the two FT8 QSO-start routes tolerate the retired ADR-0065
// auto_work key but must now reject every OTHER unknown field at decode (not the old
// blanket lenient decode), so a client schema typo can't ride in silently.
func TestFt8QsoStartWork_RejectUnknownFieldsExceptAutoWork(t *testing.T) {
	routes := []struct {
		name string
		path string
		h    func(s *Server) http.HandlerFunc
	}{
		{"qso start", "/v1/ft8/qso/start", func(s *Server) http.HandlerFunc { return s.handleFt8QsoStart }},
		{"qso work", "/v1/ft8/qso/work", func(s *Server) http.HandlerFunc { return s.handleFt8QsoWork }},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			// An unrelated unknown field is rejected at decode, before FT8 validation.
			srv := ft8QsoTestServer(t, "7Q5MLV")
			w := postFt8Qso(t, srv, rt.path, `{"totally_unknown":1}`, rt.h(srv))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("unknown field: status = %d, want 400: %s", w.Code, w.Body.String())
			}
			if code := decodeErrCode(t, w); code != "invalid_json" {
				t.Errorf("unknown field: code = %q, want invalid_json", code)
			}

			// The declared legacy auto_work alias must NOT be rejected at decode; any
			// later validation error is a different code, never invalid_json.
			srv2 := ft8QsoTestServer(t, "7Q5MLV")
			w2 := postFt8Qso(t, srv2, rt.path, `{"auto_work":"auto"}`, rt.h(srv2))
			if w2.Code == http.StatusBadRequest && decodeErrCode(t, w2) == "invalid_json" {
				t.Errorf("auto_work must be tolerated at decode, got a rejection: %s", w2.Body.String())
			}
		})
	}
}
