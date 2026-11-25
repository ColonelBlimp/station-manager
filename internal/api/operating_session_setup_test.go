package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Setup's first save and the operating session (ADR 0100, Update 2026-11-19;
// W-0029 part 2, group C; operator ruling 2026-11-20, question 5). The save
// that creates the default logbook opens the first session for the lifetime
// the admission holds. If it cannot, the save does not succeed, and it can be
// made again, by the same lifetime, with no restart.

// setupBed is a server before setup: an archive identity, no logbook, and a
// configuration that names configuredID as its default logbook. With
// configuredID 1 that is an ordinary first run; any other id is the case where
// the configuration names a logbook the database will not allocate.
type setupBed struct {
	t   *testing.T
	srv *Server
	raw *sql.DB
}

func newSetupBed(t *testing.T, configuredID int64) *setupBed {
	t.Helper()
	srv := testServerWithCfg(t, func(cfg *config.Config) { cfg.DefaultLogbookID = configuredID })
	if snap := srv.cfg.Snapshot(); snap.SetupComplete || snap.DefaultLogbookID != configuredID {
		t.Fatalf("the bed's configuration is %+v; want setup incomplete and default id %d", snap.SetupComplete, configuredID)
	}
	srv.sessionClock = func() time.Time { return sessionMorning }
	raw, err := sql.Open(sqlite.SqliteDriver, "file:"+srv.db.DatabaseConfig.Path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return &setupBed{t: t, srv: srv, raw: raw}
}

func (b *setupBed) exec(q string, args ...any) {
	b.t.Helper()
	if _, err := b.raw.Exec(q, args...); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
}

func (b *setupBed) count(q string, args ...any) int {
	b.t.Helper()
	var n int
	if err := b.raw.QueryRow(q, args...).Scan(&n); err != nil {
		b.t.Fatalf("%s: %v", q, err)
	}
	return n
}

// admit admits a lifetime through the admission itself. The routes are not
// used here: their test helper seeds a default logbook, which a first run does
// not have.
func (b *setupBed) admit(lifetime string) {
	b.t.Helper()
	gen, _, _ := b.srv.admission.read(context.Background(), lifetime)
	if _, err := b.srv.admission.admit(lifetime, processInstance, gen); err != nil {
		b.t.Fatalf("admit %q: %v", lifetime, err)
	}
}

// save is setup's first save, and returns the status and the error code.
func (b *setupBed) save() (int, string) {
	b.t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(`{"logging_station": {"station_callsign": "M0XYZ"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b.srv.handlePutConfig(w, req)
	if w.Code == http.StatusOK {
		return w.Code, ""
	}
	return w.Code, errorCode(b.t, w.Body.Bytes())
}

// stored is what the file and the configuration hold.
type setupStored struct {
	logbooks, defaults, sessions int
	pointer                      int // the file's default pointer, 0 for none
	configured                   int64
	complete                     bool
}

func (b *setupBed) stored() setupStored {
	b.t.Helper()
	snap := b.srv.cfg.Snapshot()
	return setupStored{
		logbooks:   b.count(`SELECT COUNT(*) FROM logbook`),
		defaults:   b.count(`SELECT COUNT(*) FROM logbook WHERE name = 'Default'`),
		sessions:   b.count(`SELECT COUNT(*) FROM operating_session`),
		pointer:    b.count(`SELECT COALESCE(default_logbook_id, 0) FROM archive_metadata WHERE singleton = 1`),
		configured: snap.DefaultLogbookID,
		complete:   snap.SetupComplete,
	}
}

// wantComplete is the end every save must reach: exactly one default logbook,
// the file's pointer on it, the configuration naming it, setup complete, and
// one session, held by the lifetime and bound to that logbook.
func (b *setupBed) wantComplete(lifetime string) {
	b.t.Helper()
	got := b.stored()
	if got.logbooks != 1 || got.defaults != 1 || got.sessions != 1 || got.pointer == 0 ||
		got.configured != int64(got.pointer) || !got.complete {
		b.t.Fatalf("after the save: %+v; want one default logbook, the pointer and the configuration on it, setup complete, one session", got)
	}
	st, err := b.srv.db.OperatingSessionStateWithContext(context.Background(), lifetime)
	if err != nil {
		b.t.Fatal(err)
	}
	var uuid string
	if err := b.raw.QueryRow(`SELECT uuid FROM logbook WHERE id = ?`, got.pointer).Scan(&uuid); err != nil {
		b.t.Fatal(err)
	}
	if st.Open == nil || st.Open.Holder != lifetime || st.Open.LogbookUUID != uuid {
		b.t.Fatalf("the session is %+v; want one held by %q for logbook %s", st.Open, lifetime, uuid)
	}
}

func TestSetupSave_OpensTheFirstSessionForTheHeldLifetime(t *testing.T) {
	b := newSetupBed(t, 1)
	ch, cancel := b.srv.hub.Subscribe()
	defer cancel()
	b.admit(lifeA)
	if st := b.stored(); st.sessions != 0 || st.logbooks != 0 {
		t.Fatalf("before the save: %+v; want setup pending, nothing stored", st)
	}
	if status, code := b.save(); status != http.StatusOK {
		t.Fatalf("the save: %d %s", status, code)
	}
	b.wantComplete(lifeA)
	changes := 0
	for {
		select {
		case ev := <-ch:
			if ev.Name == "operating_session.changed" {
				changes++
			}
			continue
		default:
		}
		break
	}
	if changes != 1 {
		t.Fatalf("the save's open published %d events, want 1", changes)
	}
}

func TestSetupSave_WithNoLifetimeOpensNothing_AndTheNextAdmissionDoes(t *testing.T) {
	b := newSetupBed(t, 1)
	if status, code := b.save(); status != http.StatusOK {
		t.Fatalf("the save: %d %s", status, code)
	}
	if st := b.stored(); st.sessions != 0 || !st.complete || st.logbooks != 1 || st.pointer == 0 {
		t.Fatalf("after a save with nobody admitted: %+v; want setup complete and no session", st)
	}
	b.admit(lifeA)
	b.wantComplete(lifeA)
}

// The session belongs to the lifetime the admission holds when the save's step
// runs, and to none that has gone.
func TestSetupSave_TheSessionIsForWhoeverTheAdmissionHolds(t *testing.T) {
	t.Run("after a replacement, the new lifetime", func(t *testing.T) {
		b := newSetupBed(t, 1)
		b.admit(lifeA)
		b.admit(lifeB)
		if status, code := b.save(); status != http.StatusOK {
			t.Fatalf("the save: %d %s", status, code)
		}
		b.wantComplete(lifeB)
	})
	t.Run("a lifetime whose exit is recorded gets none", func(t *testing.T) {
		b := newSetupBed(t, 1)
		b.admit(lifeA)
		// The file records the exit; the admission's memory has not caught up:
		// the moment between an exit's commit and its retirement.
		if _, _, err := b.srv.db.RecordOperatingAppExitWithContext(context.Background(), lifeA, 0, true, sessionMorning); err != nil {
			t.Fatal(err)
		}
		if status, code := b.save(); status != http.StatusOK {
			t.Fatalf("the save: %d %s", status, code)
		}
		if st := b.stored(); st.sessions != 0 || !st.complete {
			t.Fatalf("after the save: %+v; want setup complete and no session for the exited lifetime", st)
		}
	})
}

// A logbook that was there before setup is left alone: the save adopts it, as
// it always has, and makes no other.
func TestSetupSave_AdoptsALogbookThatWasAlreadyThere(t *testing.T) {
	b := newSetupBed(t, 1)
	id, err := b.srv.db.InsertLogbookWithContext(context.Background(), types.Logbook{Name: "Mine", Callsign: "M0XYZ"})
	if err != nil || id != 1 {
		t.Fatalf("the logbook that was there: id %d, %v", id, err)
	}
	b.admit(lifeA)
	if status, code := b.save(); status != http.StatusOK {
		t.Fatalf("the save: %d %s", status, code)
	}
	if st := b.stored(); st.logbooks != 1 || st.defaults != 0 || st.pointer != 1 || st.configured != 1 || !st.complete || st.sessions != 1 {
		t.Fatalf("after the save: %+v; want the one logbook that was there, adopted, and a session", st)
	}
}

// The save can be made again after each place it can fail once the logbook is
// made, by the same lifetime and with no restart. The configuration names
// logbook 42, which the database will not allocate, so the repeat cannot find
// the earlier logbook by the configured id (codex-193).
func TestSetupSave_CanBeMadeAgainAfterEachFailure(t *testing.T) {
	const configured = 42

	t.Run("recording the default in the file fails", func(t *testing.T) {
		b := newSetupBed(t, configured)
		b.admit(lifeA)
		b.exec(`CREATE TRIGGER injected_failure BEFORE UPDATE OF default_logbook_id ON archive_metadata
			BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
		if status, code := b.save(); status < 500 {
			t.Fatalf("the save: %d %s; want a failure", status, code)
		}
		if st := b.stored(); st.logbooks != 0 || st.pointer != 0 || st.sessions != 0 || st.complete || st.configured != configured {
			t.Fatalf("after the failure: %+v; want no logbook, no pointer, no session, setup incomplete", st)
		}
		b.exec(`DROP TRIGGER injected_failure`)
		if status, code := b.save(); status != http.StatusOK {
			t.Fatalf("the repeated save: %d %s", status, code)
		}
		b.wantComplete(lifeA)
	})
	t.Run("the session cannot be opened", func(t *testing.T) {
		b := newSetupBed(t, configured)
		b.admit(lifeA)
		b.exec(`CREATE TRIGGER injected_failure BEFORE UPDATE OF session_generation ON archive_metadata
			BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
		if status, code := b.save(); status != http.StatusServiceUnavailable || code != "operating_session_unavailable" {
			t.Fatalf("the save: %d %s; want 503 operating_session_unavailable", status, code)
		}
		if st := b.stored(); st.logbooks != 1 || st.pointer == 0 || st.sessions != 0 || st.complete || st.configured != configured {
			t.Fatalf("after the failure: %+v; want the logbook and its pointer, no session, setup incomplete", st)
		}
		b.exec(`DROP TRIGGER injected_failure`)
		if status, code := b.save(); status != http.StatusOK {
			t.Fatalf("the repeated save: %d %s", status, code)
		}
		b.wantComplete(lifeA)
	})
	t.Run("the configuration cannot be written", func(t *testing.T) {
		b := newSetupBed(t, configured)
		b.admit(lifeA)
		good := b.srv.cfg.Path
		blocker := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		b.srv.cfg.SetPath(filepath.Join(blocker, "config.json"))
		if status, code := b.save(); status < 500 {
			t.Fatalf("the save: %d %s; want a failure", status, code)
		}
		st := b.stored()
		if st.logbooks != 1 || st.pointer == 0 || st.sessions != 1 || st.complete || st.configured != configured {
			t.Fatalf("after the failure: %+v; want the logbook, its pointer and the session, setup incomplete", st)
		}
		var held string
		if err := b.raw.QueryRow(`SELECT id FROM operating_session`).Scan(&held); err != nil {
			t.Fatal(err)
		}
		b.srv.cfg.SetPath(good)
		if status, code := b.save(); status != http.StatusOK {
			t.Fatalf("the repeated save: %d %s", status, code)
		}
		b.wantComplete(lifeA)
		if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ?`, held); n != 1 {
			t.Fatal("the repeated save replaced the session the first one opened")
		}
	})
}

// The configuration's last validation runs after the logbook, its pointer and
// the session are stored, and can still refuse the save (codex-198). This is
// what is stored then, and that the save made again completes. It is not a
// rollback of the database with the configuration: the two are separate
// stores, and what the database holds stays.
//
// The refusal is forced the way a second window would cause it: between the
// save's first validation and its last, the rig this save selects is removed.
func TestSetupSave_ALateRefusalKeepsWhatWasStored_AndTheRepeatCompletes(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.DefaultLogbookID = 42
		cfg.Rigs = []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10", Port: "/dev/ttyUSB0"}}
	})
	b := &setupBed{t: t, srv: srv}
	srv.sessionClock = func() time.Time { return sessionMorning }
	raw, err := sql.Open(sqlite.SqliteDriver, "file:"+srv.db.DatabaseConfig.Path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	b.raw = raw
	b.admit(lifeA)

	removed := 0
	srv.SetArchiveSummaryNotifier(func() {
		if removed++; removed > 1 {
			return
		}
		if _, err := srv.cfg.Update(func(cfg *config.Config) error { cfg.Rigs = nil; return nil }); err != nil {
			t.Errorf("remove the rig between the two validations: %v", err)
		}
	})
	put := func(body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.handlePutConfig(w, req)
		if w.Code == http.StatusOK {
			return w.Code, ""
		}
		return w.Code, errorCode(t, w.Body.Bytes())
	}

	status, code := put(`{"logging_station": {"station_callsign": "M0XYZ"}, "default_rig_id": 1}`)
	if status != http.StatusBadRequest || removed != 1 {
		t.Fatalf("the save: %d %s, the rig removed %d time(s); want the last validation's 400", status, code, removed)
	}
	st := b.stored()
	if st.logbooks != 1 || st.pointer == 0 || st.sessions != 1 || st.complete || st.configured != 42 {
		t.Fatalf("after the refusal: %+v; want the logbook, its pointer and the session stored, setup incomplete", st)
	}
	var held string
	if err := raw.QueryRow(`SELECT id FROM operating_session`).Scan(&held); err != nil {
		t.Fatal(err)
	}

	if status, code := put(`{"logging_station": {"station_callsign": "M0XYZ"}}`); status != http.StatusOK {
		t.Fatalf("the corrected save: %d %s", status, code)
	}
	b.wantComplete(lifeA)
	if n := b.count(`SELECT COUNT(*) FROM operating_session WHERE id = ?`, held); n != 1 {
		t.Fatal("the corrected save replaced the session the refused one opened")
	}
}

// After a setup save that failed before the configuration was committed, the
// configuration does not yet name the logbook the save made, so the delete
// route's check of the configured default does not cover it. It must still be
// refused: the archive file names it as the default, and if it were deleted the
// repeated save could neither reuse it nor make another "Default", the name
// being taken by the deleted row (the clean-room review of 3f2f08aa).
func TestSetupSave_TheLogbookItMadeCannotBeDeletedBeforeTheRepeat(t *testing.T) {
	b := newSetupBed(t, 42)
	b.admit(lifeA)
	b.exec(`CREATE TRIGGER injected_failure BEFORE UPDATE OF session_generation ON archive_metadata
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	if status, code := b.save(); status != http.StatusServiceUnavailable {
		t.Fatalf("the save: %d %s; want 503", status, code)
	}
	b.exec(`DROP TRIGGER injected_failure`)
	made := b.stored().pointer
	if made == 0 || int64(made) == b.srv.cfg.Snapshot().DefaultLogbookID {
		t.Fatalf("the fixture: the file points at %d and the configuration names %d; want them to differ", made, b.srv.cfg.Snapshot().DefaultLogbookID)
	}

	req := httptest.NewRequest(http.MethodDelete, "/v1/logbook/1", nil)
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	b.srv.handleDeleteLogbook(w, req)
	if w.Code != http.StatusConflict || errorCode(t, w.Body.Bytes()) != "default_logbook" {
		t.Fatalf("deleting the logbook the failed save made: %d %s; want 409 default_logbook", w.Code, w.Body.String())
	}
	if n := b.count(`SELECT COUNT(*) FROM logbook WHERE id = ? AND deleted_at IS NULL`, made); n != 1 {
		t.Fatal("the logbook the failed save made was deleted")
	}

	if status, code := b.save(); status != http.StatusOK {
		t.Fatalf("the repeated save: %d %s", status, code)
	}
	b.wantComplete(lifeA)
}
