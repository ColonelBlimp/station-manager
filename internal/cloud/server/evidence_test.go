package server

/*
   §5 sync slice — the HTTP half of SMC evidence ingest (transport rules
   over store.UpsertEvidence, whose row semantics E1–E8 are pinned in
   internal/cloud/store/evidence_test.go). Real Postgres via testServer.

   H1  Auth wall: a missing or wrong bearer answers 401 before any parsing.
   H2  A valid mixed batch answers 200 with exactly one outcome per record,
       positionally, uuid echoed — the store contract carried through HTTP.
   H3  Envelope faults are REQUEST faults (400): malformed JSON, trailing
       content, an empty records array, a batch over the row cap. A
       ROW-level fault is deliberately not among them: it stays a 200 with
       a per-row permanent_reject (the store's E6), because turning one bad
       row into a request failure would block its batch-mates.
   H4  Tenant scoping holds through the full stack: two tokens land the
       same (kind, uuid) under their own tenants without conflict.
*/

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

func putEvidence(t *testing.T, ts *httptest.Server, token, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/evidence", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /v1/evidence: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b
}

func coverageRecJSON(t *testing.T, uuid string, decodeCount int) string {
	t.Helper()
	payload := fmt.Sprintf(`{"slot_start_utc":"2026-08-10T12:00:00Z","outcome":"decoded","dial_mhz":14.074,"dial_tracked":true,"decode_count":%d}`, decodeCount)
	digest, err := evidencewire.DigestV1Hex([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"kind":"coverage","uuid":%q,"digest_v":1,"digest":%q,"payload":%s}`, uuid, digest, payload)
}

func TestEvidenceHTTP_AuthWall(t *testing.T) {
	ts, _, _ := testServer(t)
	body := `{"records":[` + coverageRecJSON(t, utils.NewUUIDv7At(time.Now()), 1) + `]}`
	if status, _ := putEvidence(t, ts, "", body); status != http.StatusUnauthorized {
		t.Fatalf("H1: no token = %d, want 401", status)
	}
	if status, _ := putEvidence(t, ts, "wrong-token", body); status != http.StatusUnauthorized {
		t.Fatalf("H1: wrong token = %d, want 401", status)
	}
}

func TestEvidenceHTTP_MixedBatchOutcomes(t *testing.T) {
	ts, _, _ := testServer(t)
	now := time.Now()
	goodUUID := utils.NewUUIDv7At(now)
	body := `{"records":[` +
		coverageRecJSON(t, goodUUID, 1) + `,` +
		`{"kind":"retention","uuid":"` + utils.NewUUIDv7At(now) + `","digest_v":1,"digest":"` + strings.Repeat("a", 64) + `","payload":{"x":1}}` +
		`]}`
	status, respBody := putEvidence(t, ts, testToken, body)
	if status != http.StatusOK {
		t.Fatalf("H2: status = %d (%s), want 200 — a ROW fault must not fail the request", status, respBody)
	}
	var resp evidencewire.PutResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("H2: response not a PutResponse: %v (%s)", err, respBody)
	}
	if len(resp.Outcomes) != 2 {
		t.Fatalf("H2: %d outcomes for 2 records, want exactly one per record", len(resp.Outcomes))
	}
	if resp.Outcomes[0].UUID != goodUUID || resp.Outcomes[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("H2: good row = %+v, want accepted with uuid echoed", resp.Outcomes[0])
	}
	if resp.Outcomes[1].Outcome != evidencewire.OutcomePermanentReject {
		t.Fatalf("H2/H3: reserved-kind row = %+v, want per-row permanent_reject, not a request failure", resp.Outcomes[1])
	}
}

// C1 — the per-row outcome breakdown is logged. A batch with rejects/tombstones/
// already-present returns 200 exactly like a fully-accepted one; without this line
// the server (and the proxy) cannot tell "all stored" from "some quarantined".
func TestEvidenceHTTP_LogsOutcomeBreakdown(t *testing.T) {
	var logBuf bytes.Buffer
	ts, _, _ := testServerLogged(t, slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	now := time.Now()
	// One accepted (coverage) + one permanent_reject (reserved "retention" kind), the
	// same mix as the H2 outcome test.
	body := `{"records":[` +
		coverageRecJSON(t, utils.NewUUIDv7At(now), 1) + `,` +
		`{"kind":"retention","uuid":"` + utils.NewUUIDv7At(now) + `","digest_v":1,"digest":"` + strings.Repeat("a", 64) + `","payload":{"x":1}}` +
		`]}`
	if status, respBody := putEvidence(t, ts, testToken, body); status != http.StatusOK {
		t.Fatalf("fixture: status = %d (%s), want 200", status, respBody)
	}

	line := ""
	for _, l := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(l, "evidence batch stored") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("no 'evidence batch stored' log line; log:\n%s", logBuf.String())
	}
	for _, want := range []string{`"rows":2`, `"accepted":1`, `"permanent_reject":1`} {
		if !strings.Contains(line, want) {
			t.Errorf("outcome-breakdown line missing %s; line:\n%s", want, line)
		}
	}
}

func TestEvidenceHTTP_EnvelopeFaultsAre400(t *testing.T) {
	ts, _, _ := testServer(t)
	cases := []struct{ name, body string }{
		{"malformed JSON", `{"records":`},
		{"trailing content", `{"records":[]} trailing`},
		{"empty records", `{"records":[]}`},
	}
	for _, c := range cases {
		if status, _ := putEvidence(t, ts, testToken, c.body); status != http.StatusBadRequest {
			t.Fatalf("H3: %s = %d, want 400", c.name, status)
		}
	}
	var b bytes.Buffer
	b.WriteString(`{"records":[`)
	for i := 0; i <= 1000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(coverageRecJSON(t, utils.NewUUIDv7At(time.Now()), i))
	}
	b.WriteString(`]}`)
	if status, _ := putEvidence(t, ts, testToken, b.String()); status != http.StatusBadRequest {
		t.Fatal("H3: a batch over the row cap must answer 400")
	}
}

func TestEvidenceHTTP_TenantScopingThroughTokens(t *testing.T) {
	ts, _, _ := testServer(t)
	uuid := utils.NewUUIDv7At(time.Now())
	if status, body := putEvidence(t, ts, testToken, `{"records":[`+coverageRecJSON(t, uuid, 1)+`]}`); status != http.StatusOK {
		t.Fatalf("tenant A: %d (%s)", status, body)
	}
	status, respBody := putEvidence(t, ts, otherToken, `{"records":[`+coverageRecJSON(t, uuid, 9)+`]}`)
	if status != http.StatusOK {
		t.Fatalf("tenant B: %d (%s)", status, respBody)
	}
	var resp evidencewire.PutResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Outcomes[0].Outcome != evidencewire.OutcomeAccepted {
		t.Fatalf("H4: tenant B same-uuid different-content = %+v, want accepted — tenants must not share digest space", resp.Outcomes[0])
	}
}
