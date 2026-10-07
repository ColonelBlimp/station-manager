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

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// W-0021 5F.2: PUT /v1/archives/{archive_uuid}/logbooks/{logbook_uuid}/qsos
// (ADR 0088 scoped path; ADR 0089 rulings S1, S2, S3, S5, S6).
//
//	IP1  the first push creates the managed archive and its logbook, with their
//	     display values, and stores the QSOs; the old wire never sees them.
//	IP2  a push by an adopted Home's UUIDs reaches the adopted legacy logbook.
//	IP3  a logbook UUID living in another archive: 409 logbook_in_other_archive;
//	     nothing created or changed.
//	IP4  a QSO stored in another archive: 409 archive_conflict at newer, equal
//	     and older revisions, judged against the REQUESTED archive.
//	IP5  within one archive a newer revision still moves a QSO between logbooks.
//	IP6  non-empty display values overwrite; empty keep.
//	IP7  a refused mixed batch leaves no archive, logbook, label or QSO behind.
//	IP8  strict envelope; the QSO rows are validated as on the name wire.
//	IP9  a push waiting on an uncommitted adoption of its archive UUID lands in
//	     the adopted legacy archive; an adoption waiting on an uncommitted first
//	     push of that UUID is refused archive_uuid_in_use.
//	IP10 two claims of one logbook UUID in different archives: the waiting one
//	     is refused, creating nothing.

const (
	contestUUID = "01920000-0000-7000-8000-000000000c01"
	contestLB   = "01920000-0000-7000-8000-000000000d01"
	contestLB2  = "01920000-0000-7000-8000-000000000d02"
)

type identityPut struct {
	ArchiveLabel string      `json:"archive_label"`
	LogbookLabel string      `json:"logbook_label"`
	Callsign     string      `json:"callsign"`
	Qsos         []QsoUpload `json:"qsos"`
}

func pushIdentity(t *testing.T, ts *httptest.Server, archive, logbook string, body identityPut) (int, map[string]any) {
	t.Helper()
	var out map[string]any
	resp := do(t, http.MethodPut, ts.URL+"/v1/archives/"+archive+"/logbooks/"+logbook+"/qsos", testToken, body, &out)
	return resp.StatusCode, out
}

func oneQso(t *testing.T, uuid, call string, revision int64, at time.Time) []QsoUpload {
	t.Helper()
	q := fixtureQso(uuid)
	q.Call = call
	p, _ := json.Marshal(q)
	return []QsoUpload{{ModifiedAt: at, Revision: revision, Qso: p}}
}

var pushAt = time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)

type archiveRow struct {
	label  string
	legacy bool
}

func readArchive(t *testing.T, db *sql.DB, uuid string) (archiveRow, bool) {
	t.Helper()
	var a archiveRow
	err := db.QueryRow(`SELECT label, legacy FROM archives WHERE archive_uuid = $1`, uuid).Scan(&a.label, &a.legacy)
	if err == sql.ErrNoRows {
		return a, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return a, true
}

type logbookRow struct {
	id                 int64
	archiveUUID, label string
	callsign           string
}

func readLogbook(t *testing.T, db *sql.DB, uuid string) (logbookRow, bool) {
	t.Helper()
	var l logbookRow
	err := db.QueryRow(`SELECT l.id, a.archive_uuid::text, l.label, l.callsign FROM logbooks l JOIN archives a ON a.id = l.archive_id WHERE l.uuid = $1`, uuid).
		Scan(&l.id, &l.archiveUUID, &l.label, &l.callsign)
	if err == sql.ErrNoRows {
		return l, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return l, true
}

func TestIdentityPush_IP1_FirstPushCreatesTheManagedArchive(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	status, out := pushIdentity(t, ts, contestUUID, contestLB, identityPut{
		ArchiveLabel: "Contest", LogbookLabel: "main", Callsign: "7Q5MLV/P",
		Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000101", "DL9UW", 1, pushAt)})
	if status != http.StatusOK || out["applied"] != float64(1) {
		t.Fatalf("first identity push: %d %v", status, out)
	}
	if a, ok := readArchive(t, db, contestUUID); !ok || a.legacy || a.label != "Contest" {
		t.Fatalf("archive = %+v (%v); want a managed archive labelled Contest", a, ok)
	}
	lb, ok := readLogbook(t, db, contestLB)
	if !ok || lb.archiveUUID != contestUUID || lb.label != "main" || lb.callsign != "7Q5MLV/P" {
		t.Fatalf("logbook = %+v (%v)", lb, ok)
	}
	var books struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks", testToken, nil, &books)
	if len(books.Logbooks) != 0 {
		t.Fatalf("the old wire lists %+v; want nothing from the managed archive", books.Logbooks)
	}
}

func TestIdentityPush_IP2_AnAdoptedHomeIsReachedByUUID(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	pushMain(t, ts)
	if status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID)); status != http.StatusOK {
		t.Fatalf("adopt: %d %v", status, out)
	}
	status, out := pushIdentity(t, ts, homeUUID, defaultLBUID, identityPut{
		Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000102", "EA1B", 1, pushAt)})
	if status != http.StatusOK || out["applied"] != float64(1) {
		t.Fatalf("push to the adopted home: %d %v", status, out)
	}
	if n := scalarRow(t, db, `SELECT count(*) FROM logbooks`); n != 1 {
		t.Fatalf("logbooks = %d; want only the adopted main", n)
	}
	var books struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks", testToken, nil, &books)
	var man struct {
		Entries []store.ManifestEntry `json:"entries"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks/"+itoa(books.Logbooks[0].ID)+"/manifest", testToken, nil, &man)
	if len(man.Entries) != 2 {
		t.Fatalf("old-wire manifest of main = %d entries; want 2 (both wires' QSOs)", len(man.Entries))
	}
}

func TestIdentityPush_IP3_ALogbookOfAnotherArchiveIsRefused(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{ArchiveLabel: "Contest", LogbookLabel: "main",
		Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000103", "DL9UW", 1, pushAt)})
	status, out := pushIdentity(t, ts, otherUUID, contestLB, identityPut{ArchiveLabel: "Other", LogbookLabel: "renamed",
		Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000104", "EA1B", 1, pushAt)})
	if status != http.StatusConflict || out["code"] != "logbook_in_other_archive" {
		t.Fatalf("logbook of another archive: %d %v", status, out)
	}
	if _, ok := readArchive(t, db, otherUUID); ok {
		t.Fatal("the refused push created its archive")
	}
	if lb, _ := readLogbook(t, db, contestLB); lb.label != "main" || lb.archiveUUID != contestUUID {
		t.Fatalf("logbook after the refusal = %+v; want it untouched", lb)
	}
}

func TestIdentityPush_IP4_AQsoOfAnotherArchiveIsRefusedAtEveryRevision(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	const uuid = "0197f9a0-0000-7000-8000-000000000105"
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{Qsos: oneQso(t, uuid, "DL9UW", 3, pushAt)})
	for _, c := range []struct {
		name string
		rev  int64
		at   time.Time
	}{{"newer", 4, pushAt.Add(time.Hour)}, {"equal", 3, pushAt}, {"older", 2, pushAt.Add(-time.Hour)}} {
		status, out := pushIdentity(t, ts, otherUUID, contestLB2, identityPut{Qsos: oneQso(t, uuid, "W1AW", c.rev, c.at)})
		if status != http.StatusConflict || out["code"] != "archive_conflict" {
			t.Fatalf("%s: %d %v; want 409 archive_conflict", c.name, status, out)
		}
	}
	if got := readStored(t, db, uuid); got.call != "DL9UW" || got.revision != 3 {
		t.Fatalf("stored = %+v; want untouched", got)
	}
	if _, ok := readArchive(t, db, otherUUID); ok {
		t.Fatal("a refused push created its archive")
	}
}

func TestIdentityPush_IP5_AQsoStillMovesWithinItsArchive(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	const uuid = "0197f9a0-0000-7000-8000-000000000106"
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{Qsos: oneQso(t, uuid, "DL9UW", 1, pushAt)})
	status, out := pushIdentity(t, ts, contestUUID, contestLB2, identityPut{Qsos: oneQso(t, uuid, "DL9UW", 2, pushAt.Add(time.Hour))})
	if status != http.StatusOK || out["applied"] != float64(1) {
		t.Fatalf("move within the archive: %d %v", status, out)
	}
	lb2, _ := readLogbook(t, db, contestLB2)
	if got := readStored(t, db, uuid); got.logbook != lb2.id || got.revision != 2 {
		t.Fatalf("stored = %+v; want moved to logbook %d", got, lb2.id)
	}
}

func TestIdentityPush_IP6_DisplayValuesOverwriteOnlyWhenGiven(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{ArchiveLabel: "Contest", LogbookLabel: "main", Callsign: "7Q5MLV",
		Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000107", "DL9UW", 1, pushAt)})
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{ArchiveLabel: "CQ WW", LogbookLabel: "", Callsign: "",
		Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000108", "EA1B", 1, pushAt)})
	if a, _ := readArchive(t, db, contestUUID); a.label != "CQ WW" {
		t.Fatalf("archive label = %q; want the new non-empty CQ WW", a.label)
	}
	if lb, _ := readLogbook(t, db, contestLB); lb.label != "main" || lb.callsign != "7Q5MLV" {
		t.Fatalf("logbook = %+v; want label and callsign kept on empty", lb)
	}
}

func TestIdentityPush_IP7_ARefusedMixedBatchLeavesNothing(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	const held = "0197f9a0-0000-7000-8000-000000000109"
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{ArchiveLabel: "Contest", LogbookLabel: "main",
		Qsos: oneQso(t, held, "DL9UW", 1, pushAt)})
	const fresh = "0197f9a0-0000-7000-8000-00000000010a"
	ups := append(oneQso(t, fresh, "EA1B", 1, pushAt), oneQso(t, held, "W1AW", 9, pushAt.Add(time.Hour))...)
	status, out := pushIdentity(t, ts, otherUUID, contestLB2, identityPut{ArchiveLabel: "Other", LogbookLabel: "p", Qsos: ups})
	if status != http.StatusConflict || out["code"] != "archive_conflict" {
		t.Fatalf("mixed batch: %d %v", status, out)
	}
	if _, ok := readArchive(t, db, otherUUID); ok {
		t.Fatal("the refused batch created its archive")
	}
	if _, ok := readLogbook(t, db, contestLB2); ok {
		t.Fatal("the refused batch created its logbook")
	}
	if n := scalarRow(t, db, `SELECT count(*) FROM qsos WHERE uuid = $1`, fresh); n != 0 {
		t.Fatal("the refused batch stored its valid QSO")
	}
	// The same batch shape into the EXISTING archive refuses too and keeps its label.
	status, _ = pushIdentity(t, ts, contestUUID, contestLB, identityPut{ArchiveLabel: "Renamed",
		Qsos: append(oneQso(t, fresh, "EA1B", 1, pushAt), oneQso(t, held, "W1AW", 1, pushAt)...)})
	if status != http.StatusConflict {
		t.Fatalf("divergent tie in a known archive: %d; want 409", status)
	}
	if a, _ := readArchive(t, db, contestUUID); a.label != "Contest" {
		t.Fatalf("archive label = %q; want Contest kept by the refused batch", a.label)
	}
}

func TestIdentityPush_IP8_StrictEnvelope(t *testing.T) {
	ts, _, _, _ := newTestServer(t, quietLog)
	row := `{"modified_at":"2026-07-17T06:00:00Z","qso":` + mustJSON(t, fixtureQso("0197f9a0-0000-7000-8000-00000000010b")) + `}`
	url := ts.URL + "/v1/archives/" + contestUUID + "/logbooks/" + contestLB + "/qsos"
	for _, c := range []struct{ name, body, code string }{
		{"unknown key", `{"qsos":[` + row + `],"logbook":"main"}`, "invalid_body"},
		{"duplicate key", `{"qsos":[` + row + `],"qsos":[` + row + `]}`, "invalid_body"},
		{"trailing json", `{"qsos":[` + row + `]} {}`, "invalid_body"},
		{"no qsos", `{"archive_label":"x"}`, "invalid_field_value"},
		{"bad qso uuid", `{"qsos":[` + strings.Replace(row, "0197f9a0-0000-7000-8000-00000000010b", "nope", 1) + `]}`, "invalid_field_value"},
		{"long label", `{"archive_label":"` + strings.Repeat("x", 65) + `","qsos":[` + row + `]}`, "invalid_field_value"},
	} {
		status, out := rawPut(t, url, c.body)
		if status != http.StatusBadRequest || out["code"] != c.code {
			t.Errorf("%s: %d %v; want 400 %s", c.name, status, out, c.code)
		}
	}
	if status, out := rawPut(t, ts.URL+"/v1/archives/nope/logbooks/"+contestLB+"/qsos", `{"qsos":[`+row+`]}`); status != http.StatusBadRequest {
		t.Errorf("malformed archive uuid in the path: %d %v; want 400", status, out)
	}
}

func TestIdentityPush_IP9_AdoptionAndFirstPushRace(t *testing.T) {
	t.Run("push waits on an adoption of its uuid", func(t *testing.T) {
		ts, _, tenant, db := newTestServer(t, quietLog)
		pushMain(t, ts)
		other, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = other.Rollback() }()
		if _, err := other.Exec(`UPDATE archives SET archive_uuid = $2 WHERE tenant_id = $1 AND legacy`, tenant, homeUUID); err != nil {
			t.Fatal(err)
		}
		done := make(chan int, 1)
		var out map[string]any
		go func() {
			b, _ := json.Marshal(identityPut{ArchiveLabel: "Home", Qsos: oneQso(t, "0197f9a0-0000-7000-8000-00000000010c", "DL9UW", 1, pushAt)})
			done <- doNoFatalMethod(http.MethodPut, ts.URL+"/v1/archives/"+homeUUID+"/logbooks/"+contestLB+"/qsos", b, &out)
		}()
		waitForLockWait(t, db)
		if err := other.Commit(); err != nil {
			t.Fatal(err)
		}
		if status := <-done; status != http.StatusOK {
			t.Fatalf("push after the adoption committed: %d %v", status, out)
		}
		if a, ok := readArchive(t, db, homeUUID); !ok || !a.legacy {
			t.Fatalf("archive %s = %+v (%v); want the adopted legacy archive", homeUUID, a, ok)
		}
		if n := scalarRow(t, db, `SELECT count(*) FROM archives WHERE NOT legacy`); n != 0 {
			t.Fatalf("managed archives = %d; want none", n)
		}
	})
	t.Run("adoption waits on a first push of its uuid", func(t *testing.T) {
		ts, _, tenant, db := newTestServer(t, quietLog)
		pushMain(t, ts)
		other, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = other.Rollback() }()
		if _, err := other.Exec(`INSERT INTO archives (tenant_id, archive_uuid, label) VALUES ($1, $2, 'Home?')`, tenant, homeUUID); err != nil {
			t.Fatal(err)
		}
		done := make(chan int, 1)
		var out map[string]any
		go func() {
			b, _ := json.Marshal(adoptBody("main", homeUUID, defaultLBUID))
			done <- doNoFatalPost(ts.URL+"/v1/archives/adopt", b, &out)
		}()
		waitForLockWait(t, db)
		if err := other.Commit(); err != nil {
			t.Fatal(err)
		}
		if status := <-done; status != http.StatusConflict || out["code"] != "archive_uuid_in_use" {
			t.Fatalf("adoption after the push committed: %d %v; want 409 archive_uuid_in_use", status, out)
		}
		if got := readLegacy(t, db, "main"); got.archiveUUID.Valid || got.logbookUUID.Valid {
			t.Fatalf("legacy = %+v; want unadopted", got)
		}
	})
}

func TestIdentityPush_IP10_ConflictingLogbookClaimsRace(t *testing.T) {
	ts, _, tenant, db := newTestServer(t, quietLog)
	other, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Rollback() }()
	if _, err := other.Exec(`
WITH a AS (INSERT INTO archives (tenant_id, archive_uuid, label) VALUES ($1, $2, 'Contest') RETURNING id)
INSERT INTO logbooks (tenant_id, archive_id, uuid, label) SELECT $1, id, $3, 'main' FROM a`, tenant, contestUUID, contestLB); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	var out map[string]any
	go func() {
		b, _ := json.Marshal(identityPut{ArchiveLabel: "Other", Qsos: oneQso(t, "0197f9a0-0000-7000-8000-00000000010d", "DL9UW", 1, pushAt)})
		done <- doNoFatalMethod(http.MethodPut, ts.URL+"/v1/archives/"+otherUUID+"/logbooks/"+contestLB+"/qsos", b, &out)
	}()
	waitForLockWait(t, db)
	if err := other.Commit(); err != nil {
		t.Fatal(err)
	}
	if status := <-done; status != http.StatusConflict || out["code"] != "logbook_in_other_archive" {
		t.Fatalf("second claim of the logbook uuid: %d %v; want 409 logbook_in_other_archive", status, out)
	}
	if _, ok := readArchive(t, db, otherUUID); ok {
		t.Fatal("the refused claim created its archive")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func rawPut(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	var out map[string]any
	return doNoFatalMethod(http.MethodPut, url, []byte(body), &out), out
}

func doNoFatalMethod(method, url string, body []byte, out any) int {
	req, err := http.NewRequest(method, url, strings.NewReader(string(body)))
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
