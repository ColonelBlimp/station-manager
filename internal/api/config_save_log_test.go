package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// SHIP GATE (a) — CONFIG SAVES LEAVE A DURABLE RECORD.
//
// ACCEPTANCE CRITERION (operator-checked before any mechanism was chosen):
//
//	When I save configuration and the daemon commits it, smd.log gains one
//	record naming what changed — and I can tell that record apart from the
//	daemon's own startup rewrite of the same file, from a save the daemon
//	rejected, and from a save that committed but reported an error to the
//	browser.
//
// WHY THE THIRD CLAUSE IS THE LOAD-BEARING ONE. config.json cannot answer this
// on its own. cmd/smd/main.go:237 calls cfgSvc.Update on EVERY startup (the
// UserAgent fill + the ADR 0054 ClubLog scrub) and config.Service.Update writes
// unconditionally — no delta check — so the file's mtime moves every boot
// whether or not an operator touched anything. Before this feature the log was
// no help either: handler_config.go had exactly two log calls, :670 and :754,
// and BOTH fire only on rejection. So every config line in smd.log was a
// failure, and an operator grepping for config activity saw only failures and
// reasonably concluded nothing else had happened (api-logging-gaps.md A4).
//
// OPERATOR RULINGS, 2026-08-02. These were asked before implementing, not
// inferred — every threshold invented without asking has been wrong:
//
//  1. VALUES: non-secret fields log their value; secrets log only THAT they
//     changed, mirroring the API's existing credentials_set / password_set
//     masking idiom. Email fields (smtp.username, smtp.from,
//     default_recipient) count as non-secret and log their values. Lookup
//     provider URLs log scheme + host ONLY — a provider key can ride in the
//     query string. Classification is an ALLOWLIST: a field added later is
//     redacted by default, because a denylist fails open.
//  2. DELTA: compute before → after. This is what answers the question the
//     gate was opened for ("when did this change, and to what?").
//  3. LEVEL: Info. A successful save is normal operation, not degradation.
//  4. NO-OP SAVES: fall out of the delta — no record when nothing moved. The
//     SPA saves whole tabs, so delta-free saves are common and get commoner as
//     the five un-ported tabs land.
//  5. STARTUP REWRITE (site B) is in scope, and the delta is what makes it
//     useful rather than one noise line per boot.
//
// FIXTURE SHAPES DELIBERATELY AVOIDED HERE. Each of these has shipped a defect
// in this project before:
//
//   - CS7 moves a NON-EMPTY value to a different non-empty value. An
//     ""→"7Q5MLV" fixture passes against an implementation whose `from` is
//     hardcoded empty, so it would prove nothing about the delta.
//   - CS8 writes the state first. A single no-op PUT against a fresh config
//     agrees with a broken delta by accident; the rule needs a real change
//     committed, THEN the identical body replayed.
//   - CS6 asserts BOTH halves — that the secret's value is absent AND that the
//     change is still reported. Absence alone passes against an implementation
//     that logs nothing at all.
//   - CS3/CS8 assert the ABSENCE of a record, so each first asserts its own
//     precondition (the expected status code). A fixture that 400s for the
//     wrong reason would otherwise look like a pass.
const configSaveLogMsg = "config saved"

// configSaveRecords returns the decoded `config saved` records emitted so far.
func configSaveRecords(t *testing.T, srv *Server, buf interface{ String() string }) []map[string]any {
	t.Helper()
	_ = srv
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if msg, _ := rec["message"].(string); strings.Contains(msg, configSaveLogMsg) {
			out = append(out, rec)
		}
	}
	return out
}

// changeFor pulls the single change entry for a dotted field path out of a
// `config saved` record, or reports that the record does not mention it.
func changeFor(t *testing.T, rec map[string]any, field string) (map[string]any, bool) {
	t.Helper()
	raw, ok := rec["changes"]
	if !ok {
		return nil, false
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("changes is %T, want a list", raw)
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("change entry is %T, want an object", item)
		}
		if f, _ := m["field"].(string); f == field {
			return m, true
		}
	}
	return nil, false
}

// putConfig drives a config save through the real handler and returns the
// recorder, so every rule asserts its own precondition rather than trusting it.
func putConfig(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	return w
}

// saveLogServer builds a server whose log is captured in a buffer.
func saveLogServer(t *testing.T) (*Server, *strings.Builder) {
	t.Helper()
	buf := &strings.Builder{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))
	return srv, buf
}

// CS1 — A COMMITTED SAVE IS VISIBLE. The whole of finding A4: today a
// successful PUT writes nothing, so "did anything change?" has no answer.
func TestConfigSave_CommittedSaveIsLogged(t *testing.T) {
	srv, buf := saveLogServer(t)

	if w := putConfig(t, srv, `{"logging_station":{"station_callsign":"7Q5MLV"}}`); w.Code != http.StatusOK {
		t.Fatalf("fixture: PUT must commit, got %d: %s", w.Code, w.Body.String())
	}

	recs := configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 %q record, got %d", configSaveLogMsg, len(recs))
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "info" {
		t.Errorf("level = %q, want info (operator ruling 3)", lvl)
	}
	ch, ok := changeFor(t, recs[0], "logging_station.station_callsign")
	if !ok {
		t.Fatalf("record does not name the changed field: %v", recs[0])
	}
	if to, _ := ch["to"].(string); to != "7Q5MLV" {
		t.Errorf("to = %q, want 7Q5MLV — a non-secret field logs its value (ruling 1)", to)
	}
}

// CS2 — OPERATOR SAVE VS DAEMON REWRITE. The clause config.json cannot answer:
// its mtime moves on every boot because main.go:237 rewrites unconditionally.
func TestConfigSave_RecordIdentifiesTheApiAsSource(t *testing.T) {
	srv, buf := saveLogServer(t)

	if w := putConfig(t, srv, `{"logging_station":{"station_callsign":"7Q5MLV"}}`); w.Code != http.StatusOK {
		t.Fatalf("fixture: PUT must commit, got %d: %s", w.Code, w.Body.String())
	}

	recs := configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if src, _ := recs[0]["source"].(string); src != "api" {
		t.Errorf("source = %q, want %q — otherwise this is indistinguishable from the startup rewrite", src, "api")
	}
}

// CS3 — REJECTED SAVES DO NOT LOG A COMMIT. A4's asymmetry inverted: the log
// must not now claim a change that never reached disk.
func TestConfigSave_RejectedPutLogsNoCommit(t *testing.T) {
	srv, buf := saveLogServer(t)

	w := putConfig(t, srv, `{"logging_station":{"station_callsign":"not a callsign!!"}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("fixture: this body must be REJECTED or the rule proves nothing; got %d: %s",
			w.Code, w.Body.String())
	}

	if recs := configSaveRecords(t, srv, buf); len(recs) != 0 {
		t.Fatalf("a rejected PUT logged %d commit record(s): %v", len(recs), recs)
	}
}

// CS4 — A COMMIT SURVIVES A FAILED RESPONSE. handler_config.go:778 builds the
// response AFTER the commit; when that read fails the operator gets a 500 and
// the access log says 500, but the change is already on disk (finding A8).
// Without this record the log actively misleads.
func TestConfigSave_CommitLoggedEvenWhenResponseFails(t *testing.T) {
	srv, buf := saveLogServer(t)

	// Complete setup first: this PUT must succeed while the DB still works,
	// because the setup path writes a logbook row before the commit.
	if w := putConfig(t, srv, `{"logging_station":{"station_callsign":"7Q5MLU"}}`); w.Code != http.StatusOK {
		t.Fatalf("fixture: setup PUT must commit, got %d: %s", w.Code, w.Body.String())
	}
	buf.Reset()

	breakTheDatabase(t, srv)

	// The callsign is REPEATED unchanged and a different field carries the
	// edit. Changing it would trip the callsign-lock guard at :621, which
	// reads the DB and 500s BEFORE the commit — the same status this rule
	// asserts, reached without ever committing anything. The rule would then
	// pass against a daemon that logs nothing at all.
	w := putConfig(t, srv, `{"logging_station":{"station_callsign":"7Q5MLU","my_gridsquare":"KH72aa"}}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("fixture: the response build must FAIL for this rule to mean anything; got %d: %s",
			w.Code, w.Body.String())
	}

	recs := configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("a committed change that 500'd logged %d records, want 1 — "+
			"the log would otherwise say the change did not apply", len(recs))
	}
	if _, ok := changeFor(t, recs[0], "logging_station.my_gridsquare"); !ok {
		t.Errorf("record does not name the field that was committed: %v", recs[0])
	}
}

// CS5 — FIRST-RUN SETUP IS IDENTIFIABLE. The PUT that flips setup_complete,
// seeds the operator roster and adopts the default logbook touches the same
// block as an ordinary callsign edit but means something very different.
func TestConfigSave_SetupCompletionIsMarked(t *testing.T) {
	srv, buf := saveLogServer(t)

	if w := putConfig(t, srv, `{"logging_station":{"station_callsign":"7Q5MLV"}}`); w.Code != http.StatusOK {
		t.Fatalf("fixture: setup PUT must commit, got %d: %s", w.Code, w.Body.String())
	}

	recs := configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if done, _ := recs[0]["setup_completed"].(bool); !done {
		t.Errorf("setup_completed = %v, want true — this save is not an ordinary identity edit", recs[0]["setup_completed"])
	}

	// And an ordinary later edit is NOT marked, or the flag identifies nothing.
	// The callsign is repeated because My Station saves the whole block: omit
	// it and the overlay reads as a change to "", which the :621 guard rejects
	// 409 rather than committing.
	buf.Reset()
	body := `{"logging_station":{"station_callsign":"7Q5MLV","my_gridsquare":"KH72aa"}}`
	if w := putConfig(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("fixture: follow-up PUT must commit, got %d: %s", w.Code, w.Body.String())
	}
	recs = configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record for the follow-up edit, got %d", len(recs))
	}
	if done, _ := recs[0]["setup_completed"].(bool); done {
		t.Error("an ordinary edit is marked setup_completed; the flag then distinguishes nothing")
	}
}

// CS6 — NO SECRET VALUE REACHES THE LOG. smd.log is 0644; config.json, which
// this value came from, is 0600. Both halves are asserted: the value is absent
// AND the change is still reported, because absence alone passes against an
// implementation that logs nothing.
func TestConfigSave_SecretsAreReportedButNeverValued(t *testing.T) {
	srv, buf := saveLogServer(t)

	const secret = "SUPERSECRETAPIKEY123"
	body := fmt.Sprintf(
		`{"forwarders":[{"name":"qrz","type":"qrz","enabled":false,"credentials":{"api_key":%q}}]}`,
		secret)
	if w := putConfig(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("fixture: PUT must commit, got %d: %s", w.Code, w.Body.String())
	}

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("the credential VALUE reached the log (0644 file): %s", buf.String())
	}

	recs := configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d — silence would pass the leak check above vacuously", len(recs))
	}
	ch, ok := changeFor(t, recs[0], "forwarders[qrz].credentials.api_key")
	if !ok {
		t.Fatalf("the secret's CHANGE is not reported at all: %v", recs[0])
	}
	if sec, _ := ch["secret"].(bool); !sec {
		t.Errorf("change entry is not marked secret: %v", ch)
	}
	if to, _ := ch["to"].(string); to != "(set)" {
		t.Errorf("to = %q, want %q — presence only, never the value", to, "(set)")
	}
}

// CS7 — THE RECORD CARRIES THE PREVIOUS VALUE (ruling 2). Both values are
// non-empty on purpose: an ""→"X" fixture would pass against an implementation
// whose `from` is hardcoded empty, proving nothing about the delta.
func TestConfigSave_RecordCarriesPreviousValue(t *testing.T) {
	srv, buf := saveLogServer(t)

	if w := putConfig(t, srv, `{"logging_station":{"my_gridsquare":"KH72AA"}}`); w.Code != http.StatusOK {
		t.Fatalf("fixture: first PUT must commit, got %d: %s", w.Code, w.Body.String())
	}
	buf.Reset()

	if w := putConfig(t, srv, `{"logging_station":{"my_gridsquare":"KH72BB"}}`); w.Code != http.StatusOK {
		t.Fatalf("fixture: second PUT must commit, got %d: %s", w.Code, w.Body.String())
	}

	recs := configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	ch, ok := changeFor(t, recs[0], "logging_station.my_gridsquare")
	if !ok {
		t.Fatalf("record does not name the changed field: %v", recs[0])
	}
	// The COMMITTED values, not the request's: Normalize lower-cases a
	// Maidenhead subsquare, so "KH72AA" on the wire is "KH72aa" on disk. That
	// the record shows the stored form is the point — the log describes what
	// the daemon holds, not what the browser asked for.
	if from, _ := ch["from"].(string); from != "KH72aa" {
		t.Errorf("from = %q, want KH72aa — without the previous value the log cannot answer %q",
			from, "changed to what, from what?")
	}
	if to, _ := ch["to"].(string); to != "KH72bb" {
		t.Errorf("to = %q, want KH72bb", to)
	}
}

// CS8 — A SAVE THAT CHANGES NOTHING LOGS NOTHING (ruling 4). The state is
// written first and the IDENTICAL body replayed: a single no-op PUT against a
// fresh config would agree with a broken delta by accident.
func TestConfigSave_NoOpSaveIsSilent(t *testing.T) {
	srv, buf := saveLogServer(t)

	const body = `{"logging_station":{"my_gridsquare":"KH72AA"}}`
	if w := putConfig(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("fixture: first PUT must commit, got %d: %s", w.Code, w.Body.String())
	}
	if recs := configSaveRecords(t, srv, buf); len(recs) != 1 {
		t.Fatalf("fixture: the first PUT must really change something, got %d records", len(recs))
	}
	buf.Reset()

	if w := putConfig(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("fixture: replayed PUT must still commit, got %d: %s", w.Code, w.Body.String())
	}

	if recs := configSaveRecords(t, srv, buf); len(recs) != 0 {
		t.Fatalf("a delta-free save logged %d record(s): %v", len(recs), recs)
	}
}

// CS9 — EMAIL FIELDS LOG THEIR VALUES (ruling 1). They are the operator's own
// addressing info, not credentials, and "who am I sending as" is exactly the
// kind of thing a config audit is for.
func TestConfigSave_EmailFieldsLogTheirValues(t *testing.T) {
	srv, buf := saveLogServer(t)

	body := `{"smtp":{"enabled":false,"host":"smtp.example.com","port":587,` +
		`"username":"operator@example.com","from":"operator@example.com","default_recipient":"qsl@example.com"}}`
	if w := putConfig(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("fixture: PUT must commit, got %d: %s", w.Code, w.Body.String())
	}

	recs := configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	for field, want := range map[string]string{
		"smtp.username":          "operator@example.com",
		"smtp.from":              "operator@example.com",
		"smtp.default_recipient": "qsl@example.com",
	} {
		ch, ok := changeFor(t, recs[0], field)
		if !ok {
			t.Errorf("%s is not reported as changed", field)
			continue
		}
		if to, _ := ch["to"].(string); to != want {
			t.Errorf("%s to = %q, want %q", field, to, want)
		}
	}
}

// CS10 — LOOKUP URLS LOG SCHEME + HOST ONLY (ruling 1). A provider key can ride
// in the query string, so the tail is dropped the way csrf.go parses rather
// than trusting a raw URL.
func TestConfigSave_LookupUrlLogsSchemeAndHostOnly(t *testing.T) {
	srv, buf := saveLogServer(t)

	body := `{"lookup":{"hamnut":{"name":"hamnut","enabled":true,` +
		`"url":"https://lookup.example.com/api/v2?key=SECRETINQUERY789"},"chain":[]}}`
	if w := putConfig(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("fixture: PUT must commit, got %d: %s", w.Code, w.Body.String())
	}

	if strings.Contains(buf.String(), "SECRETINQUERY789") {
		t.Fatalf("a key in the URL query reached the log: %s", buf.String())
	}
	if strings.Contains(buf.String(), "/api/v2") {
		t.Errorf("the URL path reached the log; ruling 1 says scheme + host only")
	}

	recs := configSaveRecords(t, srv, buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d — silence would pass the checks above vacuously", len(recs))
	}
	ch, ok := changeFor(t, recs[0], "lookup.hamnut.url")
	if !ok {
		t.Fatalf("the URL change is not reported at all: %v", recs[0])
	}
	if to, _ := ch["to"].(string); to != "https://lookup.example.com" {
		t.Errorf("to = %q, want %q", to, "https://lookup.example.com")
	}
}
