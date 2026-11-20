package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
)

// Setup's first save creates the default logbook and records it in the archive
// file (recordDefaultInArchive). That moves the file's default pointer from
// none to the new logbook, so the default epoch counts one move (ADR 0100 §4;
// W-0029 part 1, B1). Read from the file as stored, through a second,
// read-only connection to the test server's own database file. The server
// starts as a first run does after adoption, with an identity row that names
// no default.
func TestHandlePutConfig_FirstSetup_CountsOneMoveOfTheDefaultPointer(t *testing.T) {
	srv := testServer(t)
	path := srv.db.DatabaseConfig.Path
	if _, err := srv.db.EnsureArchiveIdentityWithContext(context.Background(), 0); err != nil {
		t.Fatalf("ensure identity: %v", err)
	}
	stored := func() (pointer, epoch int) {
		t.Helper()
		db, err := sql.Open(sqlite.SqliteDriver, "file:"+path+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		if err := db.QueryRow(`SELECT COALESCE(default_logbook_id, 0), default_epoch FROM archive_metadata WHERE singleton = 1`).
			Scan(&pointer, &epoch); err != nil {
			t.Fatalf("read the pointer and the epoch: %v", err)
		}
		return pointer, epoch
	}
	if p, e := stored(); p != 0 || e != 0 {
		t.Fatalf("before setup: pointer %d, epoch %d; want none and 0", p, e)
	}

	put := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.handlePutConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
		}
	}
	put(`{"logging_station": {"station_callsign": "M0XYZ"}}`)
	def := int(srv.cfg.Snapshot().DefaultLogbookID)
	if def == 0 {
		t.Fatal("setup left no default logbook; the test would prove nothing")
	}
	if p, e := stored(); p != def || e != 1 {
		t.Fatalf("after setup: pointer %d, epoch %d; want %d and 1", p, e, def)
	}

	// A later save of the same station does not move the pointer.
	put(`{"logging_station": {"station_callsign": "M0XYZ"}}`)
	if p, e := stored(); p != def || e != 1 {
		t.Fatalf("after a second save: pointer %d, epoch %d; want %d and 1", p, e, def)
	}
}
