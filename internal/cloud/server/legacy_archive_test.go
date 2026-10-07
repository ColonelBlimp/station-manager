package server

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// W-0021 5F.1 (ruled 2026-10-07): after the server upgrade, an OLD name-only
// client's push, listing, reconcile, manifest and export all reach the
// tenant's LEGACY archive only — even when another archive holds a logbook
// with the same display name, created first so its id sorts first (the old
// client takes the first listed name match).
//
//	L1  the push by name lands in a legacy logbook, never the managed one.
//	L2  the listing shows only legacy logbooks.
//	L3  reconcile and manifest of the managed logbook's id are 404.
//	L4  the export carries only the legacy archive's logbooks and QSOs.
func TestLegacyArchive_OldClientNeverSeesAnotherArchive(t *testing.T) {
	ts, _, tenant, db := newTestServer(t, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	const (
		managedQso = "0197f9a0-0000-7000-8000-0000000000d1"
		legacyQso  = "0197f9a0-0000-7000-8000-0000000000d2"
	)
	var managedID int64
	err := db.QueryRow(`
WITH a AS (
    INSERT INTO archives (tenant_id, archive_uuid, label) VALUES ($1, '01920000-0000-7000-8000-0000000000aa', 'Contest') RETURNING id
), l AS (
    INSERT INTO logbooks (tenant_id, archive_id, uuid, label)
    SELECT $1, id, '01920000-0000-7000-8000-0000000000bb', 'main' FROM a RETURNING id
)
INSERT INTO qsos (uuid, tenant_id, logbook_id, modified_at, payload)
SELECT $2::uuid, $1, id, now(), '{"call":"OH2BH"}'::jsonb FROM l RETURNING logbook_id`, tenant, managedQso).Scan(&managedID)
	if err != nil {
		t.Fatalf("plant the managed archive: %v", err)
	}

	at := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	p, _ := json.Marshal(fixtureQso(legacyQso))
	putQsos(t, ts, testToken, "main", []QsoUpload{{ModifiedAt: at, Qso: p}})

	var books struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
	}
	resp := do(t, http.MethodGet, ts.URL+"/v1/logbooks", testToken, nil, &books)
	if resp.StatusCode != http.StatusOK || len(books.Logbooks) != 1 || books.Logbooks[0].Name != "main" || books.Logbooks[0].ID == managedID {
		t.Fatalf("L2: listing (status %d) = %+v; want only the legacy main (managed id %d)", resp.StatusCode, books.Logbooks, managedID)
	}
	legacyID := books.Logbooks[0].ID

	var man struct {
		Entries []store.ManifestEntry `json:"entries"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks/"+itoa(legacyID)+"/manifest", testToken, nil, &man)
	if len(man.Entries) != 1 || man.Entries[0].UUID != legacyQso {
		t.Fatalf("L1: legacy main manifest = %+v; want only %s", man.Entries, legacyQso)
	}
	if n := scalarRow(t, db, `SELECT count(*) FROM qsos WHERE logbook_id = $1`, managedID); n != 1 {
		t.Fatalf("L1: managed logbook rows = %d; want its 1, untouched", n)
	}

	for _, path := range []string{"/reconcile", "/manifest"} {
		r := do(t, http.MethodGet, ts.URL+"/v1/logbooks/"+itoa(managedID)+path, testToken, nil, nil)
		if r.StatusCode != http.StatusNotFound {
			t.Fatalf("L3: GET managed %s = %d; want 404", path, r.StatusCode)
		}
	}

	var export struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
		Qsos     []ExportQso         `json:"qsos"`
	}
	resp = do(t, http.MethodGet, ts.URL+"/v1/export", testToken, nil, &export)
	if resp.StatusCode != http.StatusOK || len(export.Logbooks) != 1 || export.Logbooks[0].ID != legacyID {
		t.Fatalf("L4: export logbooks (status %d) = %+v; want only the legacy main", resp.StatusCode, export.Logbooks)
	}
	if len(export.Qsos) != 1 || export.Qsos[0].UUID != legacyQso {
		t.Fatalf("L4: export qsos = %+v; want only %s", export.Qsos, legacyQso)
	}
}

func scalarRow(t *testing.T, db interface {
	QueryRow(string, ...any) *sql.Row
}, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}
