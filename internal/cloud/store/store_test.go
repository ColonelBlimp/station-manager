package store

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// These are INTEGRATION tests against a real Postgres — the reconcile invariants
// they defend (the ON CONFLICT ... WHERE stale/tenant guards, µs timestamp
// canonicalisation, JSONB round-trip) are Postgres semantics that a mock would
// only re-implement wrongly. Per the project rule ("integration tests are the
// default for anything touching storage"), there are no mocks here.
//
// They run against the dev Postgres from `task db:pg:up` (user/pass/db =
// smcloud). When no DB is reachable — CI without a database, a dev box that
// hasn't started the container — every test skips rather than fails. Point it at
// a different instance with SMCLOUD_TEST_DSN.

const defaultTestDSN = "postgres://smcloud:smcloud@localhost:5432/smcloud?sslmode=disable"

// testStore connects to the dev Postgres, lays down a CLEAN schema from the
// migration files (so the test is independent of migrate:cloud:up and of any
// prior run's rows), and returns a Store. Skips when no dev DB is reachable.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("SMCLOUD_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("smcloud store tests need a dev Postgres (task db:pg:up): open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("smcloud store tests need a dev Postgres (task db:pg:up): ping: %v", err)
	}
	lockTestDatabase(t, db)
	if err := RefuseNonTestDatabase(db); err != nil {
		t.Fatal(err)
	}
	// Clean slate: downs (IF EXISTS, safe on first run) then every up in
	// order. 0001's down drops the QSO tables, taking later migrations'
	// constraints with them — but evidence_records (0005) references
	// tenants, so its own down must run FIRST or 0001's tenant drop fails.
	execSQLFile(t, db, "migrations/0006_retention.down.sql")
	execSQLFile(t, db, "migrations/0005_evidence.down.sql")
	execSQLFile(t, db, "migrations/0001_init.down.sql")
	execSQLFile(t, db, "migrations/0001_init.up.sql")
	execSQLFile(t, db, "migrations/0002_qsos_logbook_tenant_fk.up.sql")
	execSQLFile(t, db, "migrations/0003_qsos_revision.up.sql")
	execSQLFile(t, db, "migrations/0004_qsos_tenant_scoped_uuid.up.sql")
	execSQLFile(t, db, "migrations/0005_evidence.up.sql")
	execSQLFile(t, db, "migrations/0006_retention.up.sql")
	t.Cleanup(func() {
		execSQLFile(t, db, "migrations/0006_retention.down.sql")
		execSQLFile(t, db, "migrations/0005_evidence.down.sql")
		execSQLFile(t, db, "migrations/0001_init.down.sql")
		_ = db.Close()
	})
	return New(db)
}

// Package review (2026-08-10): RefuseNonTestDatabase must reject a database
// that holds application data without the test sentinel, so an ordinary
// `go test` against a real smcloud database at the default DSN cannot erase it.
func TestRefuseNonTestDatabase(t *testing.T) {
	s := testStore(t) // establishes the sentinel + a clean schema under the lock
	ctx := context.Background()

	// With the sentinel present (testStore stamped it), the guard allows.
	if err := RefuseNonTestDatabase(s.db); err != nil {
		t.Fatalf("a sentinel-marked database must be allowed: %v", err)
	}

	// Simulate a real database: drop the sentinel and seed a tenant. The guard
	// must now REFUSE. (Restored by testStore's cleanup dropping everything.)
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+testSentinelTable); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureTenant(ctx, "9Z9ZZ", "real data"); err != nil {
		t.Fatal(err)
	}
	err := RefuseNonTestDatabase(s.db)
	if err == nil {
		t.Fatal("P1: the guard must refuse a data-bearing database with no sentinel")
	}
	if !strings.Contains(err.Error(), "refusing to run destructive") {
		t.Fatalf("unexpected refusal message: %v", err)
	}
	// The refusal did NOT stamp a sentinel (it must not silently mark a real DB).
	var stamped bool
	_ = s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, testSentinelTable).Scan(&stamped)
	if stamped {
		t.Fatal("P1: a refused database must not have been marked as a test database")
	}
}

// smcloudTestLockID is the advisory-lock key every smcloud test-DB user takes
// for the duration of one test. The store and server test packages run as
// SEPARATE test binaries in parallel (`go test ./...`), both rebuilding the
// same dev database's schema — without cross-process serialisation one
// package's teardown lands mid-test in the other. Must match the server
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

// execSQLFile runs a whole migration file. lib/pq's parameterless Exec uses the
// simple query protocol, which accepts the file's multiple statements in one go.
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

func seedTenantLogbook(t *testing.T, s *Store, callsign string) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	tid, err := s.EnsureTenant(ctx, callsign, callsign)
	if err != nil {
		t.Fatalf("EnsureTenant(%q): %v", callsign, err)
	}
	lid, err := s.EnsureLogbook(ctx, tid, "main")
	if err != nil {
		t.Fatalf("EnsureLogbook: %v", err)
	}
	return tid, lid
}

func rec(uuid string, tid, lid int64, mod time.Time, deleted *time.Time, payload string) Record {
	return Record{
		UUID:       uuid,
		TenantID:   tid,
		LogbookID:  lid,
		ModifiedAt: mod,
		DeletedAt:  deleted,
		Payload:    json.RawMessage(payload),
	}
}

// A few stable UUIDs — the qsos.uuid column is a Postgres UUID, so these must parse.
const (
	uuidA = "01910d3a-7000-7abc-8def-000000000001"
	uuidB = "01910d3a-7000-7abc-8def-000000000002"
	uuidC = "01910d3a-7000-7abc-8def-000000000003"
)

var baseTime = time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

// TestUpsert_StaleGuard: the modified_at guard rejects an older push, applies an
// equal one (idempotent re-push still writes), and applies a newer one — and the
// returned applied count reflects exactly which writes landed.
func TestUpsert_StaleGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tid, lid := seedTenantLogbook(t, s, "7Q8AC")

	// Insert at baseTime.
	applied, err := s.Upsert(ctx, []Record{rec(uuidA, tid, lid, baseTime, nil, `{"v":1}`)})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if applied != 1 {
		t.Fatalf("insert applied = %d, want 1", applied)
	}

	// Older push: rejected, applied 0, stored payload unchanged.
	applied, err = s.Upsert(ctx, []Record{rec(uuidA, tid, lid, baseTime.Add(-time.Minute), nil, `{"v":2}`)})
	if err != nil {
		t.Fatalf("stale push: %v", err)
	}
	if applied != 0 {
		t.Errorf("stale push applied = %d, want 0 (older modified_at must be ignored)", applied)
	}
	if got := payloadV(t, mustGet(t, s, tid, uuidA)); got != 1 {
		t.Errorf("payload v after stale push = %d, want 1 (original preserved)", got)
	}

	// Equal push: applies (>= is inclusive so a re-push is idempotent-but-writes).
	applied, err = s.Upsert(ctx, []Record{rec(uuidA, tid, lid, baseTime, nil, `{"v":3}`)})
	if err != nil {
		t.Fatalf("equal push: %v", err)
	}
	if applied != 1 {
		t.Errorf("equal push applied = %d, want 1 (equal modified_at must apply)", applied)
	}

	// Newer push: applies.
	applied, err = s.Upsert(ctx, []Record{rec(uuidA, tid, lid, baseTime.Add(time.Minute), nil, `{"v":4}`)})
	if err != nil {
		t.Fatalf("newer push: %v", err)
	}
	if applied != 1 {
		t.Errorf("newer push applied = %d, want 1", applied)
	}
}

// TestUpsert_TenantScopedUUID: uuid uniqueness is scoped per tenant (migration
// 0004), so the same UUID coexists under two tenants — each push lands as the
// pushing tenant's OWN row and neither can touch the other's. Pre-0004 the
// global-unique uuid turned tenant B's push into a zero-row update reported as
// success (applied 0), permanently denying B a backup of that UUID (review
// 2026-07-20 #2); the hijack the old WHERE tenant guard defended against is
// now impossible structurally.
func TestUpsert_TenantScopedUUID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tidA, lidA := seedTenantLogbook(t, s, "7Q8AC")
	tidB, lidB := seedTenantLogbook(t, s, "7Q5MLV")

	if _, err := s.Upsert(ctx, []Record{rec(uuidA, tidA, lidA, baseTime, nil, `{"owner":"A"}`)}); err != nil {
		t.Fatalf("seed A: %v", err)
	}

	// Tenant B pushes the same UUID with a newer modified_at — the shape of a
	// deliberate occupation attempt AND of an innocent collision. Must apply
	// as B's own row.
	applied, err := s.Upsert(ctx, []Record{rec(uuidA, tidB, lidB, baseTime.Add(time.Hour), nil, `{"owner":"B"}`)})
	if err != nil {
		t.Fatalf("tenant B push: %v", err)
	}
	if applied != 1 {
		t.Errorf("tenant B push applied = %d, want 1 (same uuid, own tenant row)", applied)
	}

	a := mustGet(t, s, tidA, uuidA)
	if a.TenantID != tidA || a.LogbookID != lidA {
		t.Errorf("A's row = tenant %d logbook %d, want %d/%d (untouched by B's push)",
			a.TenantID, a.LogbookID, tidA, lidA)
	}
	if !a.ModifiedAt.Equal(baseTime) {
		t.Errorf("A's modified_at = %v, want %v (untouched by B's push)", a.ModifiedAt, baseTime)
	}
	b := mustGet(t, s, tidB, uuidA)
	if b.TenantID != tidB || b.LogbookID != lidB {
		t.Errorf("B's row = tenant %d logbook %d, want %d/%d", b.TenantID, b.LogbookID, tidB, lidB)
	}

	// Each tenant's export carries only its own copy.
	for _, tc := range []struct {
		tid  int64
		want string
	}{{tidA, `"A"`}, {tidB, `"B"`}} {
		recs, err := s.Export(ctx, tc.tid)
		if err != nil {
			t.Fatalf("Export(%d): %v", tc.tid, err)
		}
		if len(recs) != 1 {
			t.Fatalf("Export(%d) = %d records, want 1", tc.tid, len(recs))
		}
		var p struct {
			Owner string `json:"owner"`
		}
		if err := json.Unmarshal(recs[0].Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if `"`+p.Owner+`"` != tc.want {
			t.Errorf("Export(%d) payload owner = %q, want %s", tc.tid, p.Owner, tc.want)
		}
	}
}

// TestUpsert_RevisionGuard pins the ADR 0050 ordering: revision outranks the
// clock, and the modified_at comparison only decides revision TIES. The
// same-second case is THE review-3 P1 finding — a reconcile-goroutine push
// holding a stale fetch lands after a worker push within one second; before
// revisions, arrival order silently regressed the payload and the hash tie
// made it invisible forever.
func TestUpsert_RevisionGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tid, lid := seedTenantLogbook(t, s, "7Q8AC")

	recAt := func(rev int64, mod time.Time, payload string) Record {
		r := rec(uuidA, tid, lid, mod, nil, payload)
		r.Revision = rev
		return r
	}

	if _, err := s.Upsert(ctx, []Record{recAt(2, baseTime, `{"v":2}`)}); err != nil {
		t.Fatalf("seed rev 2: %v", err)
	}

	// Same second, LOWER revision — the stale reconcile push. Must be rejected.
	applied, err := s.Upsert(ctx, []Record{recAt(1, baseTime, `{"v":1}`)})
	if err != nil {
		t.Fatalf("stale-revision push: %v", err)
	}
	if applied != 0 {
		t.Errorf("same-second lower-revision push applied = %d, want 0", applied)
	}
	if got := payloadV(t, mustGet(t, s, tid, uuidA)); got != 2 {
		t.Errorf("payload v after stale-revision push = %d, want 2 (no regression)", got)
	}

	// Higher revision with an OLDER timestamp — an NTP step backward between
	// edits. Revision must outrank the clock.
	applied, err = s.Upsert(ctx, []Record{recAt(3, baseTime.Add(-time.Hour), `{"v":3}`)})
	if err != nil {
		t.Fatalf("clock-step push: %v", err)
	}
	if applied != 1 {
		t.Errorf("higher-revision older-clock push applied = %d, want 1", applied)
	}
	if got := payloadV(t, mustGet(t, s, tid, uuidA)); got != 3 {
		t.Errorf("payload v after clock-step push = %d, want 3", got)
	}

	// Revision TIE falls back to modified_at: older is rejected, equal applies
	// (idempotent re-push) — exactly the legacy semantics.
	applied, err = s.Upsert(ctx, []Record{recAt(3, baseTime.Add(-2*time.Hour), `{"v":4}`)})
	if err != nil {
		t.Fatalf("tie-older push: %v", err)
	}
	if applied != 0 {
		t.Errorf("revision-tie older-timestamp push applied = %d, want 0", applied)
	}
	applied, err = s.Upsert(ctx, []Record{recAt(3, baseTime.Add(-time.Hour), `{"v":5}`)})
	if err != nil {
		t.Fatalf("tie-equal push: %v", err)
	}
	if applied != 1 {
		t.Errorf("revision-tie equal-timestamp re-push applied = %d, want 1 (idempotent)", applied)
	}
}

// TestUpsert_CrossTenantLogbookRejected: the composite (logbook_id, tenant_id)
// FK (migration 0002) refuses a record filing tenant A's QSO under tenant B's
// logbook — the schema-level invariant, independent of handler discipline.
func TestUpsert_CrossTenantLogbookRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tidA, _ := seedTenantLogbook(t, s, "7Q8AC")
	_, lidB := seedTenantLogbook(t, s, "7Q5MLV")

	if _, err := s.Upsert(ctx, []Record{rec(uuidA, tidA, lidB, baseTime, nil, `{"v":1}`)}); err == nil {
		t.Fatal("upsert filing tenant A's QSO under tenant B's logbook succeeded, want composite-FK rejection")
	}
	if _, err := s.Get(ctx, tidA, uuidA); !stderr.Is(err, ErrNotFound) {
		t.Fatalf("Get after rejected upsert = %v, want ErrNotFound (nothing stored)", err)
	}
}

// TestExportSnapshot_MatchesSeparateReads: with no concurrent writer, the
// one-transaction snapshot returns exactly what the standalone reads return.
func TestExportSnapshot_MatchesSeparateReads(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tid, lid := seedTenantLogbook(t, s, "7Q8AC")
	lid2, err := s.EnsureLogbook(ctx, tid, "portable")
	if err != nil {
		t.Fatalf("EnsureLogbook(portable): %v", err)
	}
	del := baseTime.Add(time.Minute)
	if _, err := s.Upsert(ctx, []Record{
		rec(uuidA, tid, lid, baseTime, nil, `{"v":1}`),
		rec(uuidB, tid, lid2, baseTime, &del, `{"v":2}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var (
		books []LogbookInfo
		recs  []Record
	)
	err = s.ExportSnapshot(ctx, tid,
		func(b []LogbookInfo) error { books = b; return nil },
		func(r Record) error { recs = append(recs, r); return nil })
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}
	wantBooks, err := s.Logbooks(ctx, tid)
	if err != nil {
		t.Fatalf("Logbooks: %v", err)
	}
	wantRecs, err := s.Export(ctx, tid)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !reflect.DeepEqual(books, wantBooks) {
		t.Errorf("snapshot logbooks = %+v, want %+v", books, wantBooks)
	}
	if !reflect.DeepEqual(recs, wantRecs) {
		t.Errorf("snapshot records = %+v, want %+v", recs, wantRecs)
	}
	if len(books) != 2 || len(recs) != 2 {
		t.Errorf("snapshot shape = %d books / %d recs, want 2/2 (tombstone included)", len(books), len(recs))
	}
}

// TestUpsert_TombstoneRoundTripAndResurrect pins the delete semantics (ADR 0040
// retentive superset): a tombstone round-trips, a NEWER non-tombstone resurrects
// by recency (edit-after-delete wins), but a STALE missed-delete push does NOT
// resurrect (the tombstone holds).
func TestUpsert_TombstoneRoundTripAndResurrect(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tid, lid := seedTenantLogbook(t, s, "7Q8AC")

	// Live row.
	if _, err := s.Upsert(ctx, []Record{rec(uuidA, tid, lid, baseTime, nil, `{"v":1}`)}); err != nil {
		t.Fatalf("insert live: %v", err)
	}

	// Tombstone (newer).
	del := baseTime.Add(time.Minute)
	if _, err := s.Upsert(ctx, []Record{rec(uuidA, tid, lid, del, &del, `{"v":1}`)}); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if got := mustGet(t, s, tid, uuidA); got.DeletedAt == nil {
		t.Fatal("DeletedAt nil after tombstone push; want a tombstone")
	}
	if !manifestDeleted(t, s, lid, uuidA) {
		t.Error("manifest Deleted = false after tombstone; want true")
	}

	// STALE non-tombstone (older than the tombstone): must NOT resurrect.
	applied, err := s.Upsert(ctx, []Record{rec(uuidA, tid, lid, baseTime, nil, `{"v":2}`)})
	if err != nil {
		t.Fatalf("stale un-delete: %v", err)
	}
	if applied != 0 {
		t.Errorf("stale un-delete applied = %d, want 0 (tombstone must hold)", applied)
	}
	if got := mustGet(t, s, tid, uuidA); got.DeletedAt == nil {
		t.Error("row resurrected by a STALE push; the tombstone should have held")
	}

	// NEWER non-tombstone: resurrects by recency (edit-after-delete wins).
	if _, err := s.Upsert(ctx, []Record{rec(uuidA, tid, lid, baseTime.Add(2*time.Minute), nil, `{"v":3}`)}); err != nil {
		t.Fatalf("resurrect: %v", err)
	}
	if got := mustGet(t, s, tid, uuidA); got.DeletedAt != nil {
		t.Error("DeletedAt non-nil after a newer non-tombstone push; want resurrected (nil)")
	}
	if manifestDeleted(t, s, lid, uuidA) {
		t.Error("manifest Deleted = true after resurrect; want false")
	}
}

// TestUpsert_PrecisionCanonicalised: a nanosecond-precision modified_at is stored
// at the µs canonical precision, so a peer that truncates the same way sees an
// equal value (the reconcile hash doesn't churn — finding 3).
func TestUpsert_PrecisionCanonicalised(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tid, lid := seedTenantLogbook(t, s, "7Q8AC")

	nanos := time.Date(2026, 7, 5, 12, 0, 0, 123456789, time.UTC)
	if _, err := s.Upsert(ctx, []Record{rec(uuidA, tid, lid, nanos, nil, `{"v":1}`)}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got := mustGet(t, s, tid, uuidA)
	if !got.ModifiedAt.Equal(canonicalTime(nanos)) {
		t.Errorf("stored modified_at = %v, want µs-truncated %v", got.ModifiedAt.UTC(), canonicalTime(nanos))
	}
	// Sub-µs nanoseconds must be gone.
	if got.ModifiedAt.Nanosecond()%1000 != 0 {
		t.Errorf("stored modified_at carries sub-µs nanoseconds: %d", got.ModifiedAt.Nanosecond())
	}
}

// TestEnsureTenant_CanonicalisesCaseVariantRow: a database provisioned before
// boot normalised callsigns may hold a lowercase tenant. Re-ensuring with the
// canonical (uppercase) spelling must REUSE that row — renamed in place, name
// kept — never create a second empty tenant beside it (which would orphan the
// existing backup from the token's view).
func TestEnsureTenant_CanonicalisesCaseVariantRow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	oldID, err := s.EnsureTenant(ctx, "7q5mlv", "Marc") // the pre-normalisation deployment
	if err != nil {
		t.Fatalf("ensure old-style: %v", err)
	}
	newID, err := s.EnsureTenant(ctx, "7Q5MLV", "") // the upgraded boot
	if err != nil {
		t.Fatalf("ensure canonical: %v", err)
	}
	if newID != oldID {
		t.Fatalf("canonical ensure created a NEW tenant %d (existing %d) — data orphaned", newID, oldID)
	}
	var stored string
	if err := s.db.QueryRowContext(ctx,
		`SELECT callsign FROM tenants WHERE id = $1`, oldID).Scan(&stored); err != nil {
		t.Fatalf("read stored callsign: %v", err)
	}
	if stored != "7Q5MLV" {
		t.Errorf("stored callsign = %q, want canonical %q", stored, "7Q5MLV")
	}
	if got := tenantName(t, s, oldID); got != "Marc" {
		t.Errorf("name after canonicalise = %q, want %q (rename must not wipe)", got, "Marc")
	}
}

// TestEnsureTenant_MultipleCaseVariantsFail: two rows differing only in case
// mean corrupt provisioning — refusing is the safe move (guessing which row
// owns the backup could canonicalise the wrong one and orphan the data).
func TestEnsureTenant_MultipleCaseVariantsFail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, cs := range []string{"7q5mlv", "7Q5mlv"} {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO tenants (callsign, name) VALUES ($1, '')`, cs); err != nil {
			t.Fatalf("seed %q: %v", cs, err)
		}
	}
	if _, err := s.EnsureTenant(ctx, "7Q5MLV", ""); err == nil {
		t.Fatal("ensure over two case-variant rows succeeded — want a refuse-to-guess error")
	}
}

// TestEnsureTenant_IdempotentPreservesName: re-ensuring is idempotent (same id)
// and an EMPTY name on re-ensure does NOT wipe the stored name; a non-empty name
// updates it (finding 5).
func TestEnsureTenant_IdempotentPreservesName(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id1, err := s.EnsureTenant(ctx, "7Q8AC", "Marc")
	if err != nil {
		t.Fatalf("ensure 1: %v", err)
	}
	id2, err := s.EnsureTenant(ctx, "7Q8AC", "")
	if err != nil {
		t.Fatalf("ensure 2 (empty name): %v", err)
	}
	if id1 != id2 {
		t.Errorf("ids differ: %d vs %d (must be idempotent)", id1, id2)
	}
	if got := tenantName(t, s, id1); got != "Marc" {
		t.Errorf("name after empty re-ensure = %q, want %q (empty must not wipe)", got, "Marc")
	}
	if _, err := s.EnsureTenant(ctx, "7Q8AC", "Updated"); err != nil {
		t.Fatalf("ensure 3 (new name): %v", err)
	}
	if got := tenantName(t, s, id1); got != "Updated" {
		t.Errorf("name after non-empty re-ensure = %q, want %q", got, "Updated")
	}
}

// TestEnsureLogbook_Idempotent: same (tenant, name) → same id.
func TestEnsureLogbook_Idempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tid, _ := seedTenantLogbook(t, s, "7Q8AC")
	id1, err := s.EnsureLogbook(ctx, tid, "contest")
	if err != nil {
		t.Fatalf("ensure 1: %v", err)
	}
	id2, err := s.EnsureLogbook(ctx, tid, "contest")
	if err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	if id1 != id2 {
		t.Errorf("logbook ids differ: %d vs %d", id1, id2)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := testStore(t)
	tid, _ := seedTenantLogbook(t, s, "7Q8AC")
	_, err := s.Get(context.Background(), tid, uuidC)
	if !stderr.Is(err, ErrNotFound) {
		t.Fatalf("Get(absent) err = %v, want ErrNotFound", err)
	}
}

// TestUpsert_EmptyPayload: a nil payload is caught with a typed error before it
// hits the NOT NULL constraint (finding 7).
func TestUpsert_EmptyPayload(t *testing.T) {
	s := testStore(t)
	tid, lid := seedTenantLogbook(t, s, "7Q8AC")
	applied, err := s.Upsert(context.Background(), []Record{
		{UUID: uuidA, TenantID: tid, LogbookID: lid, ModifiedAt: baseTime, Payload: nil},
	})
	if !stderr.Is(err, ErrEmptyPayload) {
		t.Fatalf("empty-payload Upsert err = %v, want ErrEmptyPayload", err)
	}
	if applied != 0 {
		t.Errorf("applied = %d on error, want 0", applied)
	}
}

// TestManifest returns the logbook's rows ordered by uuid with correct deleted flags.
func TestManifest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tid, lid := seedTenantLogbook(t, s, "7Q8AC")
	del := baseTime.Add(time.Minute)
	_, err := s.Upsert(ctx, []Record{
		rec(uuidB, tid, lid, baseTime, nil, `{"v":1}`),
		rec(uuidA, tid, lid, baseTime, &del, `{"v":1}`),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	entries, err := s.Manifest(ctx, lid)
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("manifest len = %d, want 2", len(entries))
	}
	// Ordered by uuid: A before B.
	if entries[0].UUID != uuidA || entries[1].UUID != uuidB {
		t.Errorf("manifest order = %s,%s, want %s,%s", entries[0].UUID, entries[1].UUID, uuidA, uuidB)
	}
	if !entries[0].Deleted {
		t.Error("uuidA Deleted = false, want true (it's a tombstone)")
	}
	if entries[1].Deleted {
		t.Error("uuidB Deleted = true, want false")
	}
}

func mustGet(t *testing.T, s *Store, tenantID int64, uuid string) Record {
	t.Helper()
	r, err := s.Get(context.Background(), tenantID, uuid)
	if err != nil {
		t.Fatalf("Get(%d, %s): %v", tenantID, uuid, err)
	}
	return r
}

func manifestDeleted(t *testing.T, s *Store, logbookID int64, uuid string) bool {
	t.Helper()
	entries, err := s.Manifest(context.Background(), logbookID)
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	for _, e := range entries {
		if e.UUID == uuid {
			return e.Deleted
		}
	}
	t.Fatalf("uuid %s not in manifest", uuid)
	return false
}

// payloadV unmarshals {"v":N} from a record's JSONB payload (Postgres reformats
// JSONB whitespace, so compare on the parsed value, not the raw bytes).
func payloadV(t *testing.T, r Record) int {
	t.Helper()
	var p struct {
		V int `json:"v"`
	}
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload %s: %v", r.Payload, err)
	}
	return p.V
}

func tenantName(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var name string
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT name FROM tenants WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("read tenant name: %v", err)
	}
	return name
}
