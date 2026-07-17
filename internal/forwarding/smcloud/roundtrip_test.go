package smcloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/ColonelBlimp/station-manager/internal/cloud/server"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// End-to-end wire-contract test: this forwarder's Submit against the REAL
// smcloud service (internal/cloud/server over a real Postgres store) — the
// sm-cloud-p1.md S3 integration check. The cloud packages import nothing
// daemon-side; this TEST-ONLY import in the other direction is what pins the
// forwarder's locally-declared envelope against the server's at compile+run
// time. Skips without the dev Postgres (task db:pg:up), like the cloud suites.

const roundtripTestDSN = "postgres://smcloud:smcloud@localhost:5432/smcloud?sslmode=disable"

func TestSubmit_AgainstRealCloudServer(t *testing.T) {
	dsn := os.Getenv("SMCLOUD_TEST_DSN")
	if dsn == "" {
		dsn = roundtripTestDSN
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("needs a dev Postgres (task db:pg:up): open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("needs a dev Postgres (task db:pg:up): ping: %v", err)
	}
	// Serialise against the cloud test suites (same advisory lock).
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("lock conn: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, 0x534d434c); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, 0x534d434c)
		_ = conn.Close()
	})
	// Clean schema via the runtime applier.
	if _, err := db.Exec(`DROP TABLE IF EXISTS qsos; DROP TABLE IF EXISTS logbooks;
DROP TABLE IF EXISTS tenants; DROP TABLE IF EXISTS schema_migrations`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS qsos; DROP TABLE IF EXISTS logbooks;
DROP TABLE IF EXISTS tenants; DROP TABLE IF EXISTS schema_migrations`)
		_ = db.Close()
	})

	st := store.New(db)
	tenant, err := st.EnsureTenant(context.Background(), "7Q5MLV", "")
	if err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cloud := httptest.NewServer(
		server.New(st, db, quiet, map[string]int64{"tok-e2e": tenant}, "test").Handler())
	defer cloud.Close()

	creds, _ := json.Marshal(map[string]string{"url": cloud.URL, "token": "tok-e2e", "logbook": "dogfood"})
	f, err := New(types.ForwarderConfig{Name: "smcloud", Type: Type, Credentials: creds})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Rich QSO — the fields lossy paths flatten (seconds, enrichment, app ext).
	q := types.Qso{
		UUID:            "0197f9a0-2222-7000-8000-000000000001",
		LogbookID:       3,
		AppSmRequestQsl: true,
		ModifiedAt:      time.Date(2026, 7, 17, 7, 4, 3, 123456000, time.UTC),
	}
	q.Call = "DL9UW"
	q.Gridsquare = "JO41"
	q.Band = "20m"
	q.Mode = "SSB"
	q.Submode = "USB"
	q.QsoDate = "20260717"
	q.TimeOn = "070459"
	q.CountryDetails = types.Country{Name: "Germany"}

	// Insert → stored.
	if res := f.Submit(context.Background(), q, action.Insert, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("insert outcome = %s (%v)", res.Outcome, res.Err)
	}
	rec, err := st.Get(context.Background(), q.UUID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !rec.ModifiedAt.Equal(q.ModifiedAt) {
		t.Errorf("stored modified_at = %v, want %v (µs-canonical)", rec.ModifiedAt, q.ModifiedAt)
	}
	var restored types.Qso
	if err := json.Unmarshal(rec.Payload, &restored); err != nil {
		t.Fatalf("unmarshal stored payload: %v", err)
	}
	// ModifiedAt/DeletedAt are json:"-" so the restored struct's zero values
	// are expected — compare with them zeroed on the original.
	want := q
	want.ModifiedAt = time.Time{}
	want.DeletedAt = time.Time{}
	if !reflect.DeepEqual(want, restored) {
		t.Errorf("payload round-trip not deep-equal:\n want %+v\n got  %+v", want, restored)
	}

	// Stale re-push (older modified_at) → success, cloud copy untouched.
	stale := q
	stale.ModifiedAt = q.ModifiedAt.Add(-time.Hour)
	stale.Name = "should not land"
	if res := f.Submit(context.Background(), stale, action.Update, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("stale update outcome = %s (%v)", res.Outcome, res.Err)
	}
	rec2, _ := st.Get(context.Background(), q.UUID)
	if !rec2.ModifiedAt.Equal(q.ModifiedAt) {
		t.Errorf("stale push clobbered modified_at: %v", rec2.ModifiedAt)
	}

	// Delete → tombstone lands.
	del := q
	del.ModifiedAt = q.ModifiedAt.Add(time.Minute)
	del.DeletedAt = del.ModifiedAt
	if res := f.Submit(context.Background(), del, action.Delete, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("delete outcome = %s (%v)", res.Outcome, res.Err)
	}
	rec3, _ := st.Get(context.Background(), q.UUID)
	if rec3.DeletedAt == nil || !rec3.DeletedAt.Equal(del.DeletedAt) {
		t.Errorf("tombstone not stored: %v", rec3.DeletedAt)
	}
}
