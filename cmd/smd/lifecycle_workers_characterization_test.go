package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/golang-migrate/migrate/v4"
)

// W-0021 slice 5A (ADR 0082 part 10) — CHARACTERIZATION PIN of the workers
// node on the adopted Home archive, taken before per-logbook bindings replace
// the config list as the worker source. For the station's shape (`qrz`,
// `clublog`, `smcloud` enabled; `qrzcq` present, disabled) it records: which
// worker names start, that the disabled name's queued rows are discarded and
// an enabled name's rows are kept, that an enabled name's auth-failed rows are
// re-armed, and that the SM Cloud entry gets its reconciler. The 5B seed must
// reproduce all of it from bindings named identically.
//
// The three enabled stub-typed entries carry the station's NAMES: every rule
// pinned here is keyed by forwarder_name, not by type. SM Cloud is the real
// type (its reconciler is type-keyed); a loopback http URL builds without a
// network and the reconciler's first run is two minutes out.

// seedStationQueue migrates the log file ahead of orch.Start and plants three
// rows: a credential-rejected `qrz` row, a pending `qrzcq` row and a pending
// `clublog` row. Plain SQL, like seedFailedUploads: a prior run wrote these.
func seedStationQueue(t *testing.T, logDB string) (qrzQso, qrzcqQso, clublogQso int64) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+logDB+"?_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer func() { _ = db.Close() }()
	src, drv, err := sqlite.GetMigrationDrivers(db, sqlite.MigrationSetLog)
	if err != nil {
		t.Fatalf("migration drivers: %v", err)
	}
	defer func() { _ = src.Close() }()
	m, err := migrate.NewWithInstance("iofs", src, sqlite.SqliteDriver, drv)
	if err != nil {
		t.Fatalf("migrate instance: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO logbook (id, callsign, name) VALUES (1,'G4ABC','L')`); err != nil {
		t.Fatalf("seed logbook: %v", err)
	}
	for id, call := range map[int64]string{1: "M0CMC", 2: "EA1B", 3: "K1AAA"} {
		if _, err := db.Exec(`INSERT INTO qso
			(id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
			 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?,?,?,'40m','SSB',7050000,'20250508','0845','0845','59','59','Test',?,1)`,
			id, fmt.Sprintf("01920000-0000-7000-8000-%012d", id), call, fmt.Sprintf("%064d", id)); err != nil {
			t.Fatalf("seed qso %d: %v", id, err)
		}
	}
	seed := func(qsoID int64, name, typ, status string, class any) {
		if _, err := db.Exec(`INSERT INTO qso_upload
			(qso_id, forwarder_name, forwarder_type, action, status, origin, attempts, last_error, failure_class)
			VALUES (?, ?, ?, 'insert', ?, 'live', 1, 'seeded', ?)`, qsoID, name, typ, status, class); err != nil {
			t.Fatalf("seed upload %s: %v", name, err)
		}
	}
	seed(1, "qrz", stub.Type, "failed", "auth")
	seed(2, "qrzcq", stub.Type, "pending", nil)
	seed(3, "clublog", stub.Type, "pending", nil)
	return 1, 2, 3
}

// workerNamesStarted reads smd.log and returns the sorted forwarder names on
// "forwarder worker started" lines, plus the names on "forwarder disabled" lines.
func workerNamesStarted(t *testing.T, logPath string) (started, skipped []string) {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read smd.log: %v", err)
	}
	for _, ln := range strings.Split(string(data), "\n") {
		var rec struct {
			Message   string `json:"message"`
			Forwarder string `json:"forwarder"`
		}
		if json.Unmarshal([]byte(ln), &rec) != nil {
			continue
		}
		switch rec.Message {
		case "forwarder worker started":
			started = append(started, rec.Forwarder)
		case "forwarder disabled, skipping":
			skipped = append(skipped, rec.Forwarder)
		}
	}
	sort.Strings(started)
	sort.Strings(skipped)
	return started, skipped
}

func TestCharacterization_HomeStartsStationWorkerSet(t *testing.T) {
	var logDB string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		logDB = c.Datastore.Path
		cloud, err := json.Marshal(map[string]string{"url": "http://127.0.0.1:9", "token": "t"})
		if err != nil {
			t.Fatal(err)
		}
		c.Forwarders = []types.ForwarderConfig{
			{Name: "qrz", Type: stub.Type, Enabled: true, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1},
			{Name: "clublog", Type: stub.Type, Enabled: true, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1},
			{Name: "smcloud", Type: smcloud.Type, Enabled: true, Credentials: cloud, TickIntervalSec: 1, BatchSize: 1},
			{Name: "qrzcq", Type: stub.Type, Enabled: false, Credentials: stubCreds(t), TickIntervalSec: 1, BatchSize: 1},
		}
	})
	qrzQso, qrzcqQso, clublogQso := seedStationQueue(t, logDB)

	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}

	started, skipped := workerNamesStarted(t, filepath.Join(d.cfgSvc.WorkingDir(), "log", "smd.log"))
	if want := []string{"clublog", "qrz", "smcloud"}; strings.Join(started, ",") != strings.Join(want, ",") {
		t.Fatalf("workers started = %v; want %v", started, want)
	}
	if want := []string{"qrzcq"}; strings.Join(skipped, ",") != strings.Join(want, ",") {
		t.Fatalf("workers skipped = %v; want %v", skipped, want)
	}
	if d.smcloudRec == nil {
		t.Fatal("no SM Cloud reconciler for the enabled smcloud entry")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Disabled name: its queued row is discarded at start.
	if rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, qrzcqQso); err != nil || len(rows) != 0 {
		t.Fatalf("qrzcq rows after start = %d (%v); want 0 (discarded)", len(rows), err)
	}
	// Enabled name: its queued row is kept (the stub worker may already have sent it).
	if rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, clublogQso); err != nil || len(rows) != 1 {
		t.Fatalf("clublog rows after start = %d (%v); want 1 (kept)", len(rows), err)
	}
	// Enabled name: its credential-rejected row is re-armed.
	rows, err := d.db.FetchUploadsByQsoIDWithContext(ctx, qrzQso)
	if err != nil || len(rows) != 1 {
		t.Fatalf("qrz rows: %v (%d)", err, len(rows))
	}
	if rows[0].Status == "failed" || rows[0].FailureClass != "" {
		t.Fatalf("qrz auth row = status %q class %q; want re-armed", rows[0].Status, rows[0].FailureClass)
	}
}
