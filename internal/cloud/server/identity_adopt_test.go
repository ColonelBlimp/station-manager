package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// W-0021 5F.2: POST /v1/archives/adopt (ADR 0082 part 7, ADR 0088, ADR 0089).
// The client stamps the tenant's legacy archive and one legacy logbook with its
// local UUIDs, in one transaction. Rulings Q4, S2, S5 and S6 (2026-10-07).
//
//	AD1  first adoption stamps the archive and the logbook, applies the display
//	     values, keeps the legacy name, moves no QSO; the old wire still works.
//	AD2  a replay with the same mapping is a no-op, metadata included, even
//	     after a later label change.
//	AD3  a different archive UUID: 409 legacy_archive_adopted_elsewhere.
//	AD4  a conflicting logbook mapping (the logbook stamped otherwise, or the
//	     UUID already another logbook's): 409 logbook_mapping_conflict.
//	AD5  a legacy name the cloud never saw: the logbook is created, stamped.
//	AD6  the archive UUID already a managed archive's: 409 archive_uuid_in_use.
//	AD7  a refusal in the logbook part undoes the archive stamp (one transaction).
//	AD8  strict envelope: unknown, duplicate or trailing content is 400
//	     invalid_body; bad values are 400 invalid_field_value.
//	AD9  a second adoption waiting on the first sees its stamp (row lock),
//	     never overwrites it.

const (
	homeUUID     = "01920000-0000-7000-8000-000000000a01"
	otherUUID    = "01920000-0000-7000-8000-000000000a02"
	defaultLBUID = "01920000-0000-7000-8000-000000000b01"
	otherLBUID   = "01920000-0000-7000-8000-000000000b02"
)

func adoptBody(legacyName, archive, logbook string) map[string]string {
	return map[string]string{
		"legacy_name": legacyName, "archive_uuid": archive, "archive_label": "Home",
		"logbook_uuid": logbook, "logbook_name": "Default", "callsign": "7Q5MLV",
	}
}

func adopt(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	var out map[string]any
	resp := do(t, http.MethodPost, url+"/v1/archives/adopt", testToken, body, &out)
	return resp.StatusCode, out
}

func adoptRaw(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/v1/archives/adopt", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

type legacyState struct {
	archiveUUID, archiveLabel             sql.NullString
	logbookUUID, label, callsign, legName sql.NullString
}

func readLegacy(t *testing.T, db *sql.DB, legacyName string) legacyState {
	t.Helper()
	var s legacyState
	err := db.QueryRow(`SELECT a.archive_uuid::text, a.label, l.uuid::text, l.label, l.callsign, l.legacy_name
		FROM archives a LEFT JOIN logbooks l ON l.archive_id = a.id AND l.legacy_name = $1
		WHERE a.legacy AND a.tenant_id = (SELECT id FROM tenants WHERE callsign = '7Q5MLV')`, legacyName).
		Scan(&s.archiveUUID, &s.archiveLabel, &s.logbookUUID, &s.label, &s.callsign, &s.legName)
	if err != nil {
		t.Fatalf("read legacy %q: %v", legacyName, err)
	}
	return s
}

func pushMain(t *testing.T, ts *httptest.Server) {
	t.Helper()
	p, _ := json.Marshal(fixtureQso("0197f9a0-0000-7000-8000-0000000000f1"))
	putQsos(t, ts, testToken, "main", []QsoUpload{{ModifiedAt: time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC), Qso: p}})
}

func TestAdopt_AD1_FirstAdoptionStampsAndKeepsTheOldWire(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID))
	if status != http.StatusOK || out["changed"] != true || out["archive_uuid"] != homeUUID || out["logbook_uuid"] != defaultLBUID {
		t.Fatalf("first adoption: %d %v", status, out)
	}
	got := readLegacy(t, db, "main")
	if got.archiveUUID.String != homeUUID || got.archiveLabel.String != "Home" || got.logbookUUID.String != defaultLBUID ||
		got.label.String != "Default" || got.callsign.String != "7Q5MLV" || got.legName.String != "main" {
		t.Fatalf("after adoption: %+v", got)
	}
	if n := scalarRow(t, db, `SELECT count(*) FROM qsos`); n != 1 {
		t.Fatalf("qsos after adoption = %d; want 1", n)
	}
	pushMain(t, ts) // the old wire still reaches the adopted legacy logbook by name
	if n := scalarRow(t, db, `SELECT count(*) FROM logbooks`); n != 1 {
		t.Fatalf("logbooks after an old-wire push = %d; want the one adopted", n)
	}
}

func TestAdopt_AD2_ReplayIsANoOpEvenAfterARename(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	if status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID)); status != http.StatusOK {
		t.Fatalf("first adoption: %d %v", status, out)
	}
	if _, err := db.Exec(`UPDATE archives SET label = 'Home (renamed)' WHERE archive_uuid = $1`, homeUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE logbooks SET label = 'Renamed', callsign = '7Q5MLV/P' WHERE uuid = $1`, defaultLBUID); err != nil {
		t.Fatal(err)
	}
	status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID))
	if status != http.StatusOK || out["changed"] != false {
		t.Fatalf("replay: %d %v; want 200, changed false", status, out)
	}
	got := readLegacy(t, db, "main")
	if got.archiveLabel.String != "Home (renamed)" || got.label.String != "Renamed" || got.callsign.String != "7Q5MLV/P" {
		t.Fatalf("replay rewrote the metadata: %+v", got)
	}
}

func TestAdopt_AD3_ADifferentArchiveUUIDIsRefused(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID))
	status, out := adopt(t, ts.URL, adoptBody("main", otherUUID, defaultLBUID))
	if status != http.StatusConflict || out["code"] != "legacy_archive_adopted_elsewhere" {
		t.Fatalf("different archive uuid: %d %v", status, out)
	}
	if got := readLegacy(t, db, "main"); got.archiveUUID.String != homeUUID {
		t.Fatalf("archive uuid = %q; want %s kept", got.archiveUUID.String, homeUUID)
	}
}

func TestAdopt_AD4_AConflictingLogbookMappingIsRefused(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	p, _ := json.Marshal(fixtureQso("0197f9a0-0000-7000-8000-0000000000f2"))
	putQsos(t, ts, testToken, "portable", []QsoUpload{{ModifiedAt: time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC), Qso: p}})
	adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID))

	status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, otherLBUID))
	if status != http.StatusConflict || out["code"] != "logbook_mapping_conflict" {
		t.Fatalf("main re-adopted with another uuid: %d %v", status, out)
	}
	status, out = adopt(t, ts.URL, adoptBody("portable", homeUUID, defaultLBUID))
	if status != http.StatusConflict || out["code"] != "logbook_mapping_conflict" {
		t.Fatalf("portable adopted with main's uuid: %d %v", status, out)
	}
	if got := readLegacy(t, db, "main"); got.logbookUUID.String != defaultLBUID {
		t.Fatalf("main uuid = %q; want %s kept", got.logbookUUID.String, defaultLBUID)
	}
	if got := readLegacy(t, db, "portable"); got.logbookUUID.Valid {
		t.Fatalf("portable uuid = %q; want none", got.logbookUUID.String)
	}
}

func TestAdopt_AD5_AnUnseenLegacyNameIsCreated(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID))
	if status != http.StatusOK || out["changed"] != true {
		t.Fatalf("adopt an unseen name: %d %v", status, out)
	}
	if got := readLegacy(t, db, "main"); got.logbookUUID.String != defaultLBUID || got.label.String != "Default" {
		t.Fatalf("created logbook: %+v", got)
	}
}

func TestAdopt_AD6_AManagedArchiveCannotAbsorbTheLegacyArchive(t *testing.T) {
	ts, _, tenant, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	if _, err := db.Exec(`INSERT INTO archives (tenant_id, archive_uuid, label) VALUES ($1, $2, 'Home')`, tenant, homeUUID); err != nil {
		t.Fatal(err)
	}
	status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID))
	if status != http.StatusConflict || out["code"] != "archive_uuid_in_use" {
		t.Fatalf("adopt onto a managed archive's uuid: %d %v", status, out)
	}
	if got := readLegacy(t, db, "main"); got.archiveUUID.Valid || got.logbookUUID.Valid {
		t.Fatalf("legacy after the refusal: %+v; want unadopted", got)
	}
}

func TestAdopt_AD7_ALogbookRefusalUndoesTheArchiveStamp(t *testing.T) {
	ts, _, tenant, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	plantManagedLogbook(t, db, tenant) // its logbook takes uuid ...bb
	const managedLB = "01920000-0000-7000-8000-0000000000bb"
	status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, managedLB))
	if status != http.StatusConflict || out["code"] != "logbook_mapping_conflict" {
		t.Fatalf("adopt with a managed logbook's uuid: %d %v", status, out)
	}
	if got := readLegacy(t, db, "main"); got.archiveUUID.Valid || got.archiveLabel.String != "" {
		t.Fatalf("archive after the logbook refusal: %+v; want unstamped", got)
	}
}

func TestAdopt_AD8_StrictEnvelope(t *testing.T) {
	ts, _, _, _ := newTestServer(t, quietLog)
	good := `"legacy_name":"main","archive_uuid":"` + homeUUID + `","archive_label":"Home","logbook_uuid":"` + defaultLBUID + `","logbook_name":"Default","callsign":"7Q5MLV"`
	for _, c := range []struct{ name, body, code string }{
		{"unknown key", `{` + good + `,"extra":1}`, "invalid_body"},
		{"case-variant key", `{` + strings.Replace(good, `"callsign"`, `"Callsign"`, 1) + `}`, "invalid_body"},
		{"duplicate key", `{` + good + `,"callsign":"X"}`, "invalid_body"},
		{"trailing json", `{` + good + `}{}`, "invalid_body"},
		{"not an object", `[]`, "invalid_body"},
		{"missing legacy name", `{` + strings.Replace(good, `"legacy_name":"main"`, `"legacy_name":""`, 1) + `}`, "invalid_field_value"},
		{"bad archive uuid", `{` + strings.Replace(good, homeUUID, "nope", 1) + `}`, "invalid_field_value"},
		{"long callsign", `{` + strings.Replace(good, `"7Q5MLV"`, `"`+strings.Repeat("A", 33)+`"`, 1) + `}`, "invalid_field_value"},
	} {
		status, out := adoptRaw(t, ts.URL, c.body)
		if status != http.StatusBadRequest || out["code"] != c.code {
			t.Errorf("%s: %d %v; want 400 %s", c.name, status, out, c.code)
		}
	}
}

func TestAdopt_AD9_AWaitingAdoptionSeesTheFirstStamp(t *testing.T) {
	ts, _, tenant, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	ctx := context.Background()
	first, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback() }()
	// Another adoption holds the legacy archive row and stamps it.
	if _, err := first.Exec(`UPDATE archives SET archive_uuid = $2 WHERE tenant_id = $1 AND legacy`, tenant, homeUUID); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	var out map[string]any
	go func() {
		b, _ := json.Marshal(adoptBody("main", otherUUID, defaultLBUID))
		done <- doNoFatalPost(ts.URL+"/v1/archives/adopt", b, &out)
	}()
	waitForLockWait(t, db)
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	if status := <-done; status != http.StatusConflict || out["code"] != "legacy_archive_adopted_elsewhere" {
		t.Fatalf("waiting adoption: %d %v; want 409 legacy_archive_adopted_elsewhere", status, out)
	}
	if got := readLegacy(t, db, "main"); got.archiveUUID.String != homeUUID {
		t.Fatalf("archive uuid = %q; want the first stamp %s", got.archiveUUID.String, homeUUID)
	}
}

func doNoFatalPost(url string, body []byte, out any) int {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return -1
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1
	}
	defer func() { _ = resp.Body.Close() }()
	_ = json.NewDecoder(resp.Body).Decode(out)
	return resp.StatusCode
}

// waitForLockWait is the barrier: some backend is waiting on a row lock.
func waitForLockWait(t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for scalarRow(t, db, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no backend ever waited on the held lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// AD10 (codex P2 on 0bb0754c): an accepted UUID in uppercase hex is the same
// UUID — an identical replay is still a no-op, and the stored form is
// canonical lowercase.
func TestAdopt_AD10_UppercaseUUIDsReplayIdempotently(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	body := adoptBody("main", strings.ToUpper(homeUUID), strings.ToUpper(defaultLBUID))
	if status, out := adopt(t, ts.URL, body); status != http.StatusOK || out["changed"] != true {
		t.Fatalf("first uppercase adoption: %d %v", status, out)
	}
	status, out := adopt(t, ts.URL, body)
	if status != http.StatusOK || out["changed"] != false {
		t.Fatalf("identical uppercase replay: %d %v; want 200, changed false", status, out)
	}
	mixed := adoptBody("main", homeUUID, strings.ToUpper(defaultLBUID))
	if status, out := adopt(t, ts.URL, mixed); status != http.StatusOK || out["changed"] != false {
		t.Fatalf("mixed-case replay: %d %v; want 200, changed false", status, out)
	}
	if out["archive_uuid"] != homeUUID {
		t.Fatalf("response archive_uuid = %v; want the canonical %s", out["archive_uuid"], homeUUID)
	}
	if got := readLegacy(t, db, "main"); got.archiveUUID.String != homeUUID || got.logbookUUID.String != defaultLBUID {
		t.Fatalf("stored = %+v; want canonical lowercase", got)
	}
}
