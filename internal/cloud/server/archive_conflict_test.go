package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// W-0021 5F.1, codex P1 on a6affeed (ruled 2026-10-07): the name-only wire
// writes only the legacy archive. A push that would overwrite (and so move) a
// QSO stored in another archive is refused 409 archive_conflict naming the
// UUID — at a newer, equal or older revision — and the whole batch, the
// logbook it would have created included, writes nothing. A deliberate move
// between archives belongs to an identity-aware operation, never to this wire.
//
//	X1  newer, equal and older revisions of a managed QSO are each refused;
//	    its payload, revision and placement stay as they were.
//	X2  a mixed batch to a NEW logbook name: the valid QSO is not stored and
//	    the logbook is not created.
//	X3  a legacy QSO still moves between legacy logbooks on a newer revision.
//	X4  a managed insert committed WHILE the push waits on it is still
//	    refused: the check is the upsert's own, not an earlier read.

const managedUUID = "0197f9a0-0000-7000-8000-0000000000e1"

var quietLog = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

// plantManagedLogbook creates a managed archive and its logbook; returns the logbook id.
func plantManagedLogbook(t *testing.T, db *sql.DB, tenant int64) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(`
WITH a AS (
    INSERT INTO archives (tenant_id, archive_uuid, label) VALUES ($1, '01920000-0000-7000-8000-0000000000aa', 'Contest') RETURNING id
)
INSERT INTO logbooks (tenant_id, archive_id, uuid, label)
SELECT $1, id, '01920000-0000-7000-8000-0000000000bb', 'main' FROM a RETURNING id`, tenant).Scan(&id)
	if err != nil {
		t.Fatalf("plant the managed logbook: %v", err)
	}
	return id
}

const plantQso = `INSERT INTO qsos (uuid, tenant_id, logbook_id, modified_at, revision, payload)
VALUES ($1, $2, $3, $4, 3, '{"uuid":"` + managedUUID + `","call":"OH2BH"}'::jsonb)`

var plantedAt = time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)

type storedQso struct {
	logbook  int64
	revision int64
	call     string
}

func readStored(t *testing.T, db *sql.DB, uuid string) storedQso {
	t.Helper()
	var s storedQso
	if err := db.QueryRow(`SELECT logbook_id, revision, payload->>'call' FROM qsos WHERE uuid = $1`, uuid).
		Scan(&s.logbook, &s.revision, &s.call); err != nil {
		t.Fatalf("read %s: %v", uuid, err)
	}
	return s
}

func pushRaw(t *testing.T, url string, logbook string, ups []QsoUpload) (int, errorResponse) {
	t.Helper()
	var body errorResponse
	resp := do(t, http.MethodPut, url+"/v1/qsos", testToken, PutQsosRequest{Logbook: logbook, Qsos: ups}, &body)
	return resp.StatusCode, body
}

func TestArchiveConflict_X1_EveryRevisionOfAManagedQsoIsRefused(t *testing.T) {
	ts, _, tenant, db := newTestServer(t, quietLog)
	managed := plantManagedLogbook(t, db, tenant)
	if _, err := db.Exec(plantQso, managedUUID, tenant, managed, plantedAt); err != nil {
		t.Fatal(err)
	}
	q := fixtureQso(managedUUID)
	q.Call = "W1AW"
	p, _ := json.Marshal(q)
	for _, c := range []struct {
		name     string
		revision int64
		at       time.Time
	}{
		{"newer", 4, plantedAt.Add(time.Hour)},
		{"equal", 3, plantedAt},
		{"older", 2, plantedAt.Add(-time.Hour)},
	} {
		status, body := pushRaw(t, ts.URL, "main", []QsoUpload{{ModifiedAt: c.at, Revision: c.revision, Qso: p}})
		if status != http.StatusConflict || body.Code != "archive_conflict" || !strings.Contains(body.Message, managedUUID) {
			t.Fatalf("%s revision: %d %+v; want 409 archive_conflict naming %s", c.name, status, body, managedUUID)
		}
		if got := readStored(t, db, managedUUID); got != (storedQso{managed, 3, "OH2BH"}) {
			t.Fatalf("%s revision: stored = %+v; want it untouched in logbook %d", c.name, got, managed)
		}
	}
}

func TestArchiveConflict_X2_AMixedBatchWritesNothing(t *testing.T) {
	ts, _, tenant, db := newTestServer(t, quietLog)
	managed := plantManagedLogbook(t, db, tenant)
	if _, err := db.Exec(plantQso, managedUUID, tenant, managed, plantedAt); err != nil {
		t.Fatal(err)
	}
	const valid = "0197f9a0-0000-7000-8000-0000000000e2"
	pv, _ := json.Marshal(fixtureQso(valid))
	pm, _ := json.Marshal(fixtureQso(managedUUID))
	status, body := pushRaw(t, ts.URL, "fresh", []QsoUpload{
		{ModifiedAt: plantedAt, Qso: pv}, // valid, and written first
		{ModifiedAt: plantedAt.Add(time.Hour), Revision: 9, Qso: pm},
	})
	if status != http.StatusConflict || body.Code != "archive_conflict" {
		t.Fatalf("mixed batch: %d %+v; want 409 archive_conflict", status, body)
	}
	if n := scalarRow(t, db, `SELECT count(*) FROM qsos WHERE uuid = $1`, valid); n != 0 {
		t.Fatal("the batch's valid QSO was stored")
	}
	if n := scalarRow(t, db, `SELECT count(*) FROM logbooks WHERE legacy_name = 'fresh'`); n != 0 {
		t.Fatal("the refused batch created its logbook")
	}
	if got := readStored(t, db, managedUUID); got != (storedQso{managed, 3, "OH2BH"}) {
		t.Fatalf("stored = %+v; want it untouched", got)
	}
}

func TestArchiveConflict_X3_ALegacyQsoStillMovesBetweenLegacyLogbooks(t *testing.T) {
	ts, _, _, db := newTestServer(t, quietLog)
	const uuid = "0197f9a0-0000-7000-8000-0000000000e3"
	p, _ := json.Marshal(fixtureQso(uuid))
	putQsos(t, ts, testToken, "main", []QsoUpload{{ModifiedAt: plantedAt, Revision: 1, Qso: p}})
	out := putQsos(t, ts, testToken, "portable", []QsoUpload{{ModifiedAt: plantedAt.Add(time.Hour), Revision: 2, Qso: p}})
	if out.Applied != 1 {
		t.Fatalf("move push applied = %d; want 1", out.Applied)
	}
	var books struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks", testToken, nil, &books)
	var portable int64
	for _, b := range books.Logbooks {
		if b.Name == "portable" {
			portable = b.ID
		}
	}
	if got := readStored(t, db, uuid); got.logbook != portable || got.revision != 2 {
		t.Fatalf("stored = %+v; want moved to portable (%d) at revision 2", got, portable)
	}
}

func TestArchiveConflict_X4_AConcurrentManagedInsertIsSeen(t *testing.T) {
	ts, _, tenant, db := newTestServer(t, quietLog)
	managed := plantManagedLogbook(t, db, tenant)
	ctx := context.Background()
	other, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Rollback() }()
	if _, err := other.Exec(plantQso, managedUUID, tenant, managed, plantedAt); err != nil {
		t.Fatal(err)
	}
	q := fixtureQso(managedUUID)
	q.Call = "W1AW"
	p, _ := json.Marshal(q)
	type result struct {
		status int
		body   errorResponse
	}
	done := make(chan result, 1)
	go func() {
		var body errorResponse
		req := PutQsosRequest{Logbook: "main", Qsos: []QsoUpload{{ModifiedAt: plantedAt.Add(time.Hour), Revision: 9, Qso: p}}}
		resp := doNoFatal(ts.URL+"/v1/qsos", req, &body)
		done <- result{resp, body}
	}()
	// Barrier: the push is blocked on the uncommitted managed row.
	deadline := time.Now().Add(10 * time.Second)
	for scalarRow(t, db, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the push never waited on the uncommitted managed row")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := other.Commit(); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.status != http.StatusConflict || r.body.Code != "archive_conflict" {
		t.Fatalf("push racing a managed insert: %d %+v; want 409 archive_conflict", r.status, r.body)
	}
	if got := readStored(t, db, managedUUID); got != (storedQso{managed, 3, "OH2BH"}) {
		t.Fatalf("stored = %+v; want the managed row untouched", got)
	}
}

// doNoFatal is do() for a goroutine: no t.Fatal off the test goroutine.
func doNoFatal(url string, body any, out any) int {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(string(b)))
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
