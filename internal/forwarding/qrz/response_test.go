package qrz

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

// ---------- parseResponse ----------

func TestParseResponse_Empty_Errors(t *testing.T) {
	_, err := parseResponse(nil)
	if err == nil {
		t.Fatal("expected error for empty body")
	}

	_, err = parseResponse([]byte("   \n\t"))
	if err == nil {
		t.Fatal("expected error for whitespace-only body")
	}
}

func TestParseResponse_MissingRESULT_Errors(t *testing.T) {
	_, err := parseResponse([]byte("LOGID=1&COUNT=1"))
	if err == nil {
		t.Fatal("expected error for body without RESULT")
	}
	if !strings.Contains(err.Error(), "missing RESULT") {
		t.Fatalf("err = %q, want 'missing RESULT' substring", err.Error())
	}
}

func TestParseResponse_OKInsert(t *testing.T) {
	resp, err := parseResponse([]byte("RESULT=OK&LOGID=12345&COUNT=1"))
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if resp.Result != "OK" {
		t.Fatalf("Result = %q, want OK", resp.Result)
	}
	if resp.LogID != "12345" {
		t.Fatalf("LogID = %q, want 12345", resp.LogID)
	}
	if resp.Count != "1" {
		t.Fatalf("Count = %q, want 1", resp.Count)
	}
	if resp.Reason != "" {
		t.Fatalf("Reason = %q, want empty", resp.Reason)
	}
}

func TestParseResponse_FailWithReason(t *testing.T) {
	// Reason can contain spaces (URL-encoded as + or %20).
	resp, err := parseResponse([]byte("RESULT=FAIL&REASON=Invalid+LOGID"))
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if resp.Result != "FAIL" {
		t.Fatalf("Result = %q, want FAIL", resp.Result)
	}
	if resp.Reason != "Invalid LOGID" {
		t.Fatalf("Reason = %q, want 'Invalid LOGID'", resp.Reason)
	}
}

func TestParseResponse_ReasonPercentEncoded(t *testing.T) {
	resp, err := parseResponse([]byte("RESULT=FAIL&REASON=unknown%20field%3A%20CALL"))
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if resp.Reason != "unknown field: CALL" {
		t.Fatalf("Reason = %q, want 'unknown field: CALL'", resp.Reason)
	}
}

func TestParseResponse_ExtraFieldsPreserved(t *testing.T) {
	resp, err := parseResponse([]byte("RESULT=OK&LOGID=5&EXTRA=somevalue"))
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if got := resp.Fields["EXTRA"]; got != "somevalue" {
		t.Fatalf("Fields[EXTRA] = %q, want somevalue", got)
	}
}

func TestParseResponse_MalformedQuery_Errors(t *testing.T) {
	// A percent sign without two hex digits is invalid URL encoding.
	_, err := parseResponse([]byte("RESULT=OK&REASON=%ZZ"))
	if err == nil {
		t.Fatal("expected error for malformed URL-encoded body")
	}
}

// ---------- classifyResponse (AUTH, unknown action) ----------

func TestClassify_AUTH_AlwaysTerminal(t *testing.T) {
	for _, act := range []forwarding.Action{action.Insert, action.Update, action.Delete} {
		res := classifyResponse(act, response{Result: "AUTH", Reason: "bad key"})
		if res.Outcome != forwarding.OutcomeTerminal {
			t.Fatalf("act=%s: outcome = %q, want terminal", act, res.Outcome)
		}
		if res.Err == nil {
			t.Fatalf("act=%s: Err nil", act)
		}
		if !strings.Contains(res.Err.Error(), "authentication rejected") {
			t.Fatalf("act=%s: err = %q, want 'authentication rejected' substring", act, res.Err.Error())
		}
	}
}

func TestClassify_UnknownAction_Terminal(t *testing.T) {
	res := classifyResponse("hovercraft", response{Result: "OK", LogID: "1"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
}

// ---------- classifyInsert ----------

func TestClassify_Insert_OK_WithLogID_Success(t *testing.T) {
	res := classifyResponse(action.Insert, response{Result: "OK", LogID: "42"})
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if res.UpstreamID != "42" {
		t.Fatalf("UpstreamID = %q, want 42", res.UpstreamID)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil on success", res.Err)
	}
}

func TestClassify_Insert_OK_NoLogID_Terminal(t *testing.T) {
	res := classifyResponse(action.Insert, response{Result: "OK"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "without LOGID") {
		t.Fatalf("err = %q, want 'without LOGID'", res.Err.Error())
	}
}

func TestClassify_Insert_REPLACE_WithLogID_Success(t *testing.T) {
	// REPLACE is undocumented on a pure insert but end state matches
	// intent (record present upstream) — accept as success.
	res := classifyResponse(action.Insert, response{Result: "REPLACE", LogID: "77"})
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if res.UpstreamID != "77" {
		t.Fatalf("UpstreamID = %q, want 77", res.UpstreamID)
	}
}

func TestClassify_Insert_REPLACE_NoLogID_Terminal(t *testing.T) {
	res := classifyResponse(action.Insert, response{Result: "REPLACE"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
}

func TestClassify_Insert_FAIL_Terminal(t *testing.T) {
	res := classifyResponse(action.Insert, response{Result: "FAIL", Reason: "Duplicate QSO"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "Duplicate QSO") {
		t.Fatalf("err = %q, want to include reason text", res.Err.Error())
	}
}

func TestClassify_Insert_FAIL_NoReason_UsesDefault(t *testing.T) {
	res := classifyResponse(action.Insert, response{Result: "FAIL"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "no reason given") {
		t.Fatalf("err = %q, want fallback reason", res.Err.Error())
	}
}

func TestClassify_Insert_PARTIAL_Terminal(t *testing.T) {
	// PARTIAL is undocumented for insert.
	res := classifyResponse(action.Insert, response{Result: "PARTIAL"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "unexpected RESULT") {
		t.Fatalf("err = %q, want 'unexpected RESULT'", res.Err.Error())
	}
}

func TestClassify_Insert_Unknown_Terminal(t *testing.T) {
	res := classifyResponse(action.Insert, response{Result: "HOVERCRAFT"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
}

// ---------- classifyUpdate ----------

func TestClassify_Update_REPLACE_Success(t *testing.T) {
	res := classifyResponse(action.Update, response{Result: "REPLACE", LogID: "42"})
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if res.UpstreamID != "42" {
		t.Fatalf("UpstreamID = %q, want 42", res.UpstreamID)
	}
}

func TestClassify_Update_REPLACE_NoLogID_Terminal(t *testing.T) {
	res := classifyResponse(action.Update, response{Result: "REPLACE"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
}

func TestClassify_Update_OK_RecordInserted_Success(t *testing.T) {
	// OK on an update means the record didn't exist and QRZ inserted
	// it — from our side that's still "the record is now in QRZ with
	// our data" which is the intent.
	res := classifyResponse(action.Update, response{Result: "OK", LogID: "100"})
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if res.UpstreamID != "100" {
		t.Fatalf("UpstreamID = %q, want 100", res.UpstreamID)
	}
}

func TestClassify_Update_FAIL_Terminal(t *testing.T) {
	res := classifyResponse(action.Update, response{Result: "FAIL", Reason: "bad adif"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
}

func TestClassify_Update_PARTIAL_Terminal(t *testing.T) {
	res := classifyResponse(action.Update, response{Result: "PARTIAL"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
}

// ---------- classifyDelete ----------

func TestClassify_Delete_OK_Success(t *testing.T) {
	res := classifyResponse(action.Delete, response{Result: "OK", Count: "1"})
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil on success", res.Err)
	}
}

func TestClassify_Delete_FAIL_IsIdempotentSuccess(t *testing.T) {
	res := classifyResponse(action.Delete, response{Result: "FAIL", Reason: "LOGID not found"})
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success (idempotent delete)", res.Outcome)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil on idempotent-delete success", res.Err)
	}
}

func TestClassify_Delete_PARTIAL_Terminal(t *testing.T) {
	// Shouldn't happen with single-LOGID deletes; defensive guard.
	res := classifyResponse(action.Delete, response{Result: "PARTIAL"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "single-LOGID") {
		t.Fatalf("err = %q, want 'single-LOGID' substring", res.Err.Error())
	}
}

func TestClassify_Delete_REPLACE_Terminal(t *testing.T) {
	// REPLACE is undocumented for delete.
	res := classifyResponse(action.Delete, response{Result: "REPLACE"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
}

func TestClassify_Delete_Unknown_Terminal(t *testing.T) {
	res := classifyResponse(action.Delete, response{Result: "HOVERCRAFT"})
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
}
