package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/ColonelBlimp/station-manager/internal/cloud/reconcile"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// INTEGRATION tests against a real Postgres, same gate as the store's own
// suite: they run against the dev DB from `task db:pg:up` (or SMCLOUD_TEST_DSN)
// and SKIP when none is reachable. The headline test is the S2 GATE from
// docs/v2-design/sm-cloud-p1.md: a full types.Qso must round-trip
// PUT → store → export → deep-equal — UUID, HH:MM:SS seconds, and
// additional_data-carried fields intact — before anything real flows.

const defaultTestDSN = "postgres://smcloud:smcloud@localhost:5432/smcloud?sslmode=disable"

const (
	testToken   = "test-token-7q5mlv"
	otherToken  = "test-token-other"
	testVersion = "test-version"
)

// testServer stands up a clean-schema store + two provisioned tenants and
// returns the HTTP test server plus the primary tenant's id. Skips without a
// reachable Postgres.
func testServer(t *testing.T) (*httptest.Server, *store.Store, int64) {
	t.Helper()
	dsn := os.Getenv("SMCLOUD_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("smcloud server tests need a dev Postgres (task db:pg:up): open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("smcloud server tests need a dev Postgres (task db:pg:up): ping: %v", err)
	}
	lockTestDatabase(t, db)
	if err := store.RefuseNonTestDatabase(db); err != nil {
		t.Fatal(err)
	}
	// Clean slate via the migration files (drop then the runtime applier —
	// which this also exercises). evidence_records (0005) references
	// tenants, so its down runs before 0001's tenant drop.
	execSQLFile(t, db, "../store/migrations/0006_retention.down.sql")
	execSQLFile(t, db, "../store/migrations/0005_evidence.down.sql")
	execSQLFile(t, db, "../store/migrations/0001_init.down.sql")
	if _, err := db.Exec(`DROP TABLE IF EXISTS schema_migrations`); err != nil {
		t.Fatalf("drop schema_migrations: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}

	st := store.New(db)
	tenant, err := st.EnsureTenant(context.Background(), "7Q5MLV", "Marc")
	if err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	other, err := st.EnsureTenant(context.Background(), "7Q8AC", "")
	if err != nil {
		t.Fatalf("EnsureTenant(other): %v", err)
	}

	srv := New(st, db, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		map[string]int64{testToken: tenant, otherToken: other}, testVersion, 0)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		execSQLFile(t, db, "../store/migrations/0006_retention.down.sql")
		execSQLFile(t, db, "../store/migrations/0005_evidence.down.sql")
		execSQLFile(t, db, "../store/migrations/0001_init.down.sql")
		_, _ = db.Exec(`DROP TABLE IF EXISTS schema_migrations`)
		_ = db.Close()
	})
	return ts, st, tenant
}

// smcloudTestLockID is the advisory-lock key every smcloud test-DB user takes
// for the duration of one test. The store and server test packages run as
// SEPARATE test binaries in parallel (`go test ./...`), both rebuilding the
// same dev database's schema — without cross-process serialisation one
// package's teardown lands mid-test in the other. Must match the store
// suite's constant.
const smcloudTestLockID = 0x534d434c // "SMCL"

// lockTestDatabase serialises this test against every other smcloud DB test
// (across packages/processes) via a session advisory lock held on a dedicated
// connection until cleanup.
func lockTestDatabase(t *testing.T, db *sql.DB) {
	t.Helper()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("advisory lock conn: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(),
		`SELECT pg_advisory_lock($1)`, smcloudTestLockID); err != nil {
		_ = conn.Close()
		t.Fatalf("advisory lock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(),
			`SELECT pg_advisory_unlock($1)`, smcloudTestLockID)
		_ = conn.Close()
	})
}

func execSQLFile(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if _, err := db.Exec(string(b)); err != nil {
		t.Fatalf("exec %s: %v", path, err)
	}
}

// do runs a request with the given bearer token ("" = no auth header) and
// decodes the JSON response into out (skipped when out is nil).
func do(t *testing.T, method, url, token string, body any, out any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s response: %v", method, url, err)
		}
	}
	return resp
}

// fixtureQso is a rich types.Qso: UUID, seconds-precision TIME_ON, enrichment
// and app-extension fields — the shapes the additional_data blob carries and a
// lossy path (ADIF re-export) would flatten.
func fixtureQso(uuid string) types.Qso {
	q := types.Qso{
		UUID:            uuid,
		LogbookID:       1,
		DedupeKey:       "abc123",
		QrzlogLogid:     "999888777",
		AppSmRequestQsl: true,
	}
	q.QsoDate = "20260717"
	q.TimeOn = "142559" // HH:MM:SS — the seconds ADIF often drops
	q.Band = "20m"
	q.Mode = "SSB"
	q.Submode = "USB"
	q.Freq = "14.255"
	q.RstSent = "59"
	q.RstRcvd = "57"
	q.Call = "DL9UW"
	q.Gridsquare = "JO41"
	q.Name = "Uwe"
	q.CountryDetails = types.Country{Name: "Germany"}
	return q
}

func putQsos(t *testing.T, ts *httptest.Server, token, logbook string, ups []QsoUpload) PutQsosResponse {
	t.Helper()
	var out PutQsosResponse
	resp := do(t, http.MethodPut, ts.URL+"/v1/qsos", token,
		PutQsosRequest{Logbook: logbook, Qsos: ups}, &out)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /v1/qsos status = %d", resp.StatusCode)
	}
	return out
}

// THE S2 GATE: types.Qso → PUT → store → export → unmarshal → deep-equal.
func TestRoundTrip_FullFidelity(t *testing.T) {
	ts, _, _ := testServer(t)

	orig := fixtureQso("0197f9a0-0000-7000-8000-000000000001")
	payload, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	modified := time.Date(2026, 7, 17, 5, 4, 3, 123456000, time.UTC)
	out := putQsos(t, ts, testToken, "main", []QsoUpload{{ModifiedAt: modified, Revision: 5, Qso: payload}})
	if out.Received != 1 || out.Applied != 1 {
		t.Fatalf("put outcome = %+v, want received/applied 1/1", out)
	}

	var export struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
		Qsos     []ExportQso         `json:"qsos"`
	}
	resp := do(t, http.MethodGet, ts.URL+"/v1/export", testToken, nil, &export)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	if len(export.Qsos) != 1 || len(export.Logbooks) != 1 || export.Logbooks[0].Name != "main" {
		t.Fatalf("export shape = %d qsos / %+v logbooks", len(export.Qsos), export.Logbooks)
	}

	got := export.Qsos[0]
	if got.UUID != orig.UUID {
		t.Errorf("uuid = %s, want %s", got.UUID, orig.UUID)
	}
	if !got.ModifiedAt.Equal(modified) {
		t.Errorf("modified_at = %v, want %v (µs-canonical)", got.ModifiedAt, modified)
	}
	if got.Revision != 5 {
		t.Errorf("revision = %d, want 5 (round-tripped for restore continuity)", got.Revision)
	}
	var restored types.Qso
	if err := json.Unmarshal(got.Qso, &restored); err != nil {
		t.Fatalf("unmarshal exported qso: %v", err)
	}
	if !reflect.DeepEqual(orig, restored) {
		t.Errorf("round-trip not deep-equal:\n orig     %+v\n restored %+v", orig, restored)
	}
}

func TestAuth_Required(t *testing.T) {
	ts, _, _ := testServer(t)
	for _, token := range []string{"", "wrong-token"} {
		for _, path := range []string{"/v1/logbooks", "/v1/export", "/v1/logbooks/1/reconcile", "/v1/logbooks/1/manifest"} {
			resp := do(t, http.MethodGet, ts.URL+path, token, nil, nil)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("GET %s token=%q status = %d, want 401", path, token, resp.StatusCode)
			}
		}
		resp := do(t, http.MethodPut, ts.URL+"/v1/qsos", token,
			PutQsosRequest{Logbook: "main"}, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("PUT /v1/qsos token=%q status = %d, want 401", token, resp.StatusCode)
		}
	}
	// Health + version stay open.
	if resp := do(t, http.MethodGet, ts.URL+"/v1/health", "", nil, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}
	var v map[string]string
	if resp := do(t, http.MethodGet, ts.URL+"/v1/version", "", nil, &v); resp.StatusCode != http.StatusOK || v["version"] != testVersion {
		t.Errorf("version = %d %v", resp.StatusCode, v)
	}
}

// Reconcile: the served hash equals a locally-computed reconcile.Summary over
// the live rows; a tombstone leaves the summary and shows in the manifest.
func TestReconcileAndManifest(t *testing.T) {
	ts, _, _ := testServer(t)

	at := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	u1, u2 := "0197f9a0-0000-7000-8000-000000000001", "0197f9a0-0000-7000-8000-000000000002"
	p1, _ := json.Marshal(fixtureQso(u1))
	p2, _ := json.Marshal(fixtureQso(u2))
	putQsos(t, ts, testToken, "main", []QsoUpload{
		{ModifiedAt: at, Qso: p1},
		{ModifiedAt: at.Add(time.Second), Revision: 4, Qso: p2},
	})

	var books struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks", testToken, nil, &books)
	if len(books.Logbooks) != 1 {
		t.Fatalf("logbooks = %+v, want one", books.Logbooks)
	}
	id := books.Logbooks[0].ID
	base := ts.URL + "/v1/logbooks/" + itoa(id)

	var rec ReconcileResponse
	do(t, http.MethodGet, base+"/reconcile", testToken, nil, &rec)
	wantCount, wantHash := reconcile.Summary([]reconcile.Entry{
		{UUID: u1, ModifiedAt: at},
		{UUID: u2, ModifiedAt: at.Add(time.Second), Revision: 4},
	})
	if rec.Count != wantCount || rec.Hash != wantHash {
		t.Fatalf("reconcile = %+v, want count %d hash %s", rec, wantCount, wantHash)
	}

	// Tombstone u2: reconcile drops to one live row; the manifest keeps both.
	// The delete bumped the local revision past the stored 4 — a tombstone at a
	// LOWER revision would (correctly) be rejected as a stale missed-delete.
	del := at.Add(time.Minute)
	putQsos(t, ts, testToken, "main", []QsoUpload{{ModifiedAt: del, Revision: 5, DeletedAt: &del, Qso: p2}})

	do(t, http.MethodGet, base+"/reconcile", testToken, nil, &rec)
	oneCount, oneHash := reconcile.Summary([]reconcile.Entry{{UUID: u1, ModifiedAt: at}})
	if rec.Count != oneCount || rec.Hash != oneHash {
		t.Fatalf("post-tombstone reconcile = %+v, want count %d hash %s", rec, oneCount, oneHash)
	}

	var man struct {
		Entries []store.ManifestEntry `json:"entries"`
	}
	do(t, http.MethodGet, base+"/manifest", testToken, nil, &man)
	if len(man.Entries) != 2 {
		t.Fatalf("manifest entries = %d, want 2 (retentive)", len(man.Entries))
	}
	deleted := 0
	for _, e := range man.Entries {
		if e.Deleted {
			deleted++
		}
	}
	if deleted != 1 {
		t.Fatalf("manifest deleted count = %d, want 1", deleted)
	}
}

// A stale re-push is received but not applied (the upsert guard) — the
// telemetry a sync client keys off.
func TestPutQsos_StalePushNotApplied(t *testing.T) {
	ts, _, _ := testServer(t)
	at := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	p, _ := json.Marshal(fixtureQso("0197f9a0-0000-7000-8000-000000000001"))
	putQsos(t, ts, testToken, "main", []QsoUpload{{ModifiedAt: at, Qso: p}})

	out := putQsos(t, ts, testToken, "main", []QsoUpload{{ModifiedAt: at.Add(-time.Hour), Qso: p}})
	if out.Received != 1 || out.Applied != 0 {
		t.Fatalf("stale push outcome = %+v, want received 1 / applied 0", out)
	}
}

// Another tenant's logbook reads as 404 — existence is not leaked.
func TestLogbookOwnership(t *testing.T) {
	ts, _, _ := testServer(t)
	p, _ := json.Marshal(fixtureQso("0197f9a0-0000-7000-8000-000000000001"))
	putQsos(t, ts, testToken, "main", []QsoUpload{
		{ModifiedAt: time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC), Qso: p},
	})
	var books struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks", testToken, nil, &books)
	id := books.Logbooks[0].ID

	for _, path := range []string{"/reconcile", "/manifest"} {
		resp := do(t, http.MethodGet, ts.URL+"/v1/logbooks/"+itoa(id)+path, otherToken, nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("other tenant GET %s status = %d, want 404", path, resp.StatusCode)
		}
	}
	// And the other tenant's export is empty — no cross-tenant reads.
	var export struct {
		Qsos []ExportQso `json:"qsos"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/export", otherToken, nil, &export)
	if len(export.Qsos) != 0 {
		t.Errorf("other tenant export = %d qsos, want 0", len(export.Qsos))
	}
}

func TestPutQsos_Validation(t *testing.T) {
	ts, _, _ := testServer(t)
	at := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	noUUID, _ := json.Marshal(types.Qso{})
	// Valid RFC 4122 v4 — Postgres's uuid column would take it, but restore
	// admits only v7, so the upload gate must refuse it (an accepted backup
	// must be restorable).
	v4UUID, _ := json.Marshal(fixtureQso("0197f9a0-0000-4000-8000-000000000001"))
	badUUID, _ := json.Marshal(fixtureQso("not-a-uuid"))
	// Valid v7 wrapped in whitespace — rejected RAW, never trimmed: the
	// payload is stored verbatim, so a trim-then-validate gate 200-accepted
	// rows whose padded payload uuid failed the local qso table's 36-char
	// CHECK at restore time (review 2026-07-20 #1).
	paddedUUID, _ := json.Marshal(fixtureQso(" 0197f9a0-0000-7000-8000-000000000001 "))
	ok, _ := json.Marshal(fixtureQso("0197f9a0-0000-7000-8000-000000000001"))

	cases := []struct {
		name string
		body PutQsosRequest
	}{
		{"empty logbook", PutQsosRequest{Logbook: "", Qsos: []QsoUpload{{ModifiedAt: at, Qso: ok}}}},
		{"no qsos", PutQsosRequest{Logbook: "main"}},
		{"missing uuid", PutQsosRequest{Logbook: "main", Qsos: []QsoUpload{{ModifiedAt: at, Qso: noUUID}}}},
		{"non-v7 uuid", PutQsosRequest{Logbook: "main", Qsos: []QsoUpload{{ModifiedAt: at, Qso: v4UUID}}}},
		{"malformed uuid", PutQsosRequest{Logbook: "main", Qsos: []QsoUpload{{ModifiedAt: at, Qso: badUUID}}}},
		{"padded uuid", PutQsosRequest{Logbook: "main", Qsos: []QsoUpload{{ModifiedAt: at, Qso: paddedUUID}}}},
		{"zero modified_at", PutQsosRequest{Logbook: "main", Qsos: []QsoUpload{{Qso: ok}}}},
	}
	for _, c := range cases {
		resp := do(t, http.MethodPut, ts.URL+"/v1/qsos", testToken, c.body, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c.name, resp.StatusCode)
		}
	}

	// A rejected batch must provision NOTHING — validation runs before the
	// EnsureLogbook side effect.
	var books struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks", testToken, nil, &books)
	if len(books.Logbooks) != 0 {
		t.Errorf("logbooks after rejected batches = %+v, want none", books.Logbooks)
	}
}

// A syntactically valid request followed by trailing JSON is one malformed
// body, not a request plus ignorable garbage.
func TestPutQsos_TrailingJSONRejected(t *testing.T) {
	ts, _, _ := testServer(t)
	p, _ := json.Marshal(fixtureQso("0197f9a0-0000-7000-8000-000000000001"))
	body, err := json.Marshal(PutQsosRequest{
		Logbook: "main",
		Qsos:    []QsoUpload{{ModifiedAt: time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC), Qso: p}},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/qsos",
		bytes.NewReader(append(body, []byte(`{"trailing":true}`)...)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("trailing JSON status = %d, want 400", resp.StatusCode)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
