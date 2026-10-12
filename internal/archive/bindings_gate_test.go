package archive

import (
	"context"
	stderr "errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.4 commit 4 (ADR 0092; rulings F4, F5, J1–J6, 2026-10-12): the
   SM Cloud enable gate lifted to the identity rules. A save that newly enables
   a binding which may only upload by identity stamps identity_since; the order
   is account → claim/conflict → history → running generation → support →
   collision (J6).

     AG1  support: under supported and unverified a new identity enable is
          stamped; under checking, unsupported, error and changed it is refused
          with its own reason and nothing is written. The SM Cloud card carries
          the state, with the unverified line only under unverified; each
          refusing state also shows on every eligible row and as
          new_logbook_reason.
     AG2  a clean born-identity binding re-enables under any support state and
          keeps its stamp time; a binding with mixed markers keeps H2's
          diagnosis and is never offered "make it the default".
     FH1  an empty non-default Home logbook (no binding, or a disabled unmarked
          one) is enabled and stamped; its row shows no reason.
     FH2  one SM Cloud upload record on a live QSO, at any status and under any
          worker name, refuses with F5's text; nothing is written.
     FH3  a record on a deleted QSO, and a tombstone's delete record, refuse
          the same way.
     FH4  QSOs without any SM Cloud record (live only; deleted only) refuse
          with J1's second text.
     FH5  a history read that fails refuses; nothing is stamped.
     FH6  another destination's records on a logbook with QSOs: J1's second
          text (the QSOs decide, not the destination).
     FH7  a QSO written after validation fails the whole save; nothing written.
     FH8  a kept enabled legacy binding re-saved enabled: no stamp, no refusal,
          its legacy field shown.
     FH9  a managed logbook holding QSOs and other destinations' records is
          enabled and stamped: no history gate there (E3).
     FH10 Home's default enables with no stamp: the adoption path, as before.
     FH11 a clean reserved or adopted binding off the default refuses
          re-enable with J4's text and is never stamped; one already enabled
          stays as it is.
     FH12 a binding the running generation started enabled on the name wire
          refuses conversion after a saved disable until a restart; after the
          restart, empty, it qualifies.
     SC1  an eligible row whose stored legacy name equals the default's
          reservation is enabled and stamped: the proposed stamp counts.
     SC2  the same row with history reports the history refusal, never
          binding_name_reserved.
     SC3  a mixed-marker row in the candidate is still checked: the save is
          refused binding_name_reserved and nothing is stamped.
     SC4  Home's reserved default enables with no stamp.
     AF1  a typed or cleared legacy name on a row that hides it refuses
          binding_field_unused; nothing written.
     AF2  hidden_fields per row kind, with marker precedence: a stamped binding
          stays hidden after its logbook becomes the default; a claimed one
          stays locked, never hidden.
     DIS  disabling stays possible under every refusing condition.
*/

const (
	wantHistoryRecords   = "previous SM Cloud upload history exists; automatic conversion needs manual recovery"
	wantHistoryQsos      = "this logbook already holds QSOs and the archive cannot show that none reached SM Cloud; automatic conversion needs manual recovery"
	wantRunning          = "this binding still uploads by its Cloud logbook name until the daemon restarts; restart, then turn it on"
	wantChecking         = "SM Cloud server support is still being checked; try again shortly"
	wantUnsupported      = "the SM Cloud server does not support per-logbook backups; upgrade it"
	wantSupportError     = "SM Cloud server support could not be checked; the daemon log has the cause"
	wantChanged          = "the SM Cloud server changed since the daemon started; restart to check it"
	wantClaimed          = "this binding belongs to an SM Cloud adoption; make its logbook the default to use it"
	wantConflict         = "this binding carries conflicting identity and adoption markers; manual recovery is required"
	supportUnverifiedMsg = "SM Cloud server support is not verified: the server could not be reached at startup. Uploads wait and retry until a supporting server is available."
)

const managedID = "019fd5c5-efcc-7193-be4f-1fee532ee3b2"

var refusingSupport = map[smcloud.SupportState]string{
	smcloud.SupportChecking:    wantChecking,
	smcloud.SupportUnsupported: wantUnsupported,
	smcloud.SupportError:       wantSupportError,
	smcloud.SupportChanged:     wantChanged,
}

func supportIs(m *Manager, s smcloud.SupportState) {
	m.SetSmcloudSupport(func(config.Config) smcloud.SupportState { return s })
}

// gateHome: Home active with its default logbook a and an empty second
// logbook b, no SM Cloud binding on either, support supported, and the
// running generation's bindings as stored now.
func gateHome(t *testing.T) (*sqlite.Service, *Manager, int64, int64) {
	t.Helper()
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	m, _ := homeManagerCfg(t, db, a)
	supportIs(m, smcloud.SupportSupported)
	startAsStored(t, m, db)
	return db, m, a, b
}

// startAsStored makes the running generation's start set the bindings stored
// now (a restart).
func startAsStored(t *testing.T, m *Manager, db BindingsDB) {
	t.Helper()
	rows, err := db.ListLogbookDestinationsWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.SetActiveBindings(db, rows)
}

// gateManaged: a managed archive active, with logbook l, support supported.
func gateManaged(t *testing.T) (*sqlite.Service, *Manager, int64) {
	t.Helper()
	db := bindingsDB(t)
	ctx := context.Background()
	l, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Drill", Callsign: "M0ABC"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureArchiveIdentityWithContext(ctx, l); err != nil {
		t.Fatal(err)
	}
	m, cfgSvc, _ := testManager(t)
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = []types.QsoArchiveConfig{
			{ID: homeID, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/x/home.db"},
			{ID: managedID, Label: "Drill", Ownership: types.QsoArchiveOwnershipManaged},
		}
		c.ActiveQsoArchiveID = managedID
		c.DefaultLogbookID = l
		c.Forwarders = homeSmcloudCfg(l).Forwarders
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	supportIs(m, smcloud.SupportSupported)
	startAsStored(t, m, db)
	return db, m, l
}

func putVia(m *Manager, id string, rows ...types.LogbookBindingEdit) (types.ArchiveBindingsView, error) {
	return m.ApplyBindings(context.Background(), id, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "smcloud", Logbooks: rows}}})
}

func gateView(t *testing.T, m *Manager, id string) types.ArchiveBindingsView {
	t.Helper()
	v, err := m.Bindings(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func rowOf(t *testing.T, v types.ArchiveBindingsView, logbookID int64) types.LogbookBindingView {
	t.Helper()
	for _, row := range destView(t, v, "smcloud").Logbooks {
		if row.LogbookID == logbookID {
			return row
		}
	}
	t.Fatalf("no smcloud row for logbook %d", logbookID)
	return types.LogbookBindingView{}
}

func wantStamped(t *testing.T, db *sqlite.Service, logbookID int64) {
	t.Helper()
	r, ok := smcloudRow(t, db, logbookID)
	if !ok || !r.Enabled || r.IdentitySince == nil {
		t.Fatalf("binding on logbook %d = %+v (stored %v); want enabled and stamped", logbookID, r, ok)
	}
}

func wantNotStamped(t *testing.T, db *sqlite.Service, logbookID int64) {
	t.Helper()
	if r, ok := smcloudRow(t, db, logbookID); ok && r.IdentitySince != nil {
		t.Fatalf("binding on logbook %d was stamped: %+v", logbookID, r)
	}
}

// wantRefused: the PUT is refused binding_not_enableable carrying reason, and
// the stored binding on logbookID is exactly as before.
func wantRefused(t *testing.T, db *sqlite.Service, logbookID int64, err error, reason string) {
	t.Helper()
	if bindingCode(err) != "binding_not_enableable" || !strings.Contains(err.Error(), reason) {
		t.Fatalf("PUT = %v; want binding_not_enableable with %q", err, reason)
	}
}

func snapshotRow(t *testing.T, db *sqlite.Service, logbookID int64) string {
	t.Helper()
	r, ok := smcloudRow(t, db, logbookID)
	return fmt.Sprintf("%v %+v", ok, r)
}

var qsoSeq atomic.Int64

// addQso stores a QSO in logbookID, deleted or live, and returns its id.
func addQso(t *testing.T, db *sqlite.Service, logbookID int64, deleted bool) int64 {
	t.Helper()
	n := qsoSeq.Add(1) + 1000
	del := "NULL"
	if deleted {
		del = "datetime('now')"
	}
	execArchive(t, db, `INSERT INTO qso
		(id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
		 rst_sent, rst_rcvd, country, dedupe_key, logbook_id, deleted_at)
		VALUES (?,?,'EA1B','40m','SSB',7050000,'20250508','0845','0845','59','59','Test',?,?,`+del+`)`,
		n, fmt.Sprintf("01920000-0000-7000-8000-%012d", n), fmt.Sprintf("%064d", n), logbookID)
	return n
}

func addUpload(t *testing.T, db *sqlite.Service, qsoID int64, name, typ, act, status string) {
	t.Helper()
	execArchive(t, db, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin)
		VALUES (?, ?, ?, ?, ?, 'live')`, qsoID, name, typ, act, status)
}

func TestGate_AG1_SupportDecidesANewIdentityEnable(t *testing.T) {
	type place struct {
		setup func(t *testing.T) (*sqlite.Service, *Manager, string, int64)
	}
	places := map[string]place{
		"a managed logbook": {func(t *testing.T) (*sqlite.Service, *Manager, string, int64) {
			db, m, l := gateManaged(t)
			return db, m, managedID, l
		}},
		"an empty non-default Home logbook": {func(t *testing.T) (*sqlite.Service, *Manager, string, int64) {
			db, m, _, b := gateHome(t)
			return db, m, homeID, b
		}},
	}
	for pname, p := range places {
		for _, s := range []smcloud.SupportState{smcloud.SupportSupported, smcloud.SupportUnverified} {
			t.Run(pname+" under "+string(s), func(t *testing.T) {
				db, m, id, l := p.setup(t)
				supportIs(m, s)
				v := gateView(t, m, id)
				if r := rowOf(t, v, l).Reason; r != "" {
					t.Fatalf("row reason = %q; want none", r)
				}
				dv := destView(t, v, "smcloud")
				if dv.NewLogbookReason != "" {
					t.Fatalf("new_logbook_reason = %q; want none", dv.NewLogbookReason)
				}
				wantMsg := ""
				if s == smcloud.SupportUnverified {
					wantMsg = supportUnverifiedMsg
				}
				if dv.Support == nil || dv.Support.State != string(s) || dv.Support.Message != wantMsg {
					t.Fatalf("support = %+v; want state %q, message %q", dv.Support, s, wantMsg)
				}
				if _, err := putVia(m, id, types.LogbookBindingEdit{LogbookID: l, Enabled: true}); err != nil {
					t.Fatalf("enable under %s: %v", s, err)
				}
				wantStamped(t, db, l)
			})
		}
		for s, reason := range refusingSupport {
			t.Run(pname+" under "+string(s), func(t *testing.T) {
				db, m, id, l := p.setup(t)
				supportIs(m, s)
				v := gateView(t, m, id)
				if r := rowOf(t, v, l).Reason; r != reason {
					t.Fatalf("row reason = %q; want %q", r, reason)
				}
				dv := destView(t, v, "smcloud")
				if dv.NewLogbookReason != reason {
					t.Fatalf("new_logbook_reason = %q; want %q", dv.NewLogbookReason, reason)
				}
				if dv.Support == nil || dv.Support.State != string(s) || dv.Support.Message != "" {
					t.Fatalf("support = %+v; want state %q with no line", dv.Support, s)
				}
				before := snapshotRow(t, db, l)
				_, err := putVia(m, id, types.LogbookBindingEdit{LogbookID: l, Enabled: true})
				wantRefused(t, db, l, err, reason)
				if after := snapshotRow(t, db, l); after != before {
					t.Fatalf("a refused save wrote: %s → %s", before, after)
				}
			})
		}
	}
}

func TestGate_AG2_ACleanBornBindingReEnablesUnderAnySupport(t *testing.T) {
	for _, s := range []smcloud.SupportState{smcloud.SupportChecking, smcloud.SupportUnsupported, smcloud.SupportError, smcloud.SupportChanged} {
		t.Run(string(s), func(t *testing.T) {
			db, m, _, b := gateHome(t)
			execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, identity_since)
				VALUES (?, 'smcloud', 'smcloud.second', 0, '2026-10-01 10:00:00')`, b)
			addUpload(t, db, addQso(t, db, b, false), "smcloud.second", "smcloud", "insert", "uploaded")
			startAsStored(t, m, db)
			supportIs(m, s)
			if r := rowOf(t, gateView(t, m, homeID), b).Reason; r != "" {
				t.Fatalf("row reason = %q; want none for a clean born binding", r)
			}
			if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true}); err != nil {
				t.Fatalf("re-enable a born binding under %s: %v", s, err)
			}
			r, _ := smcloudRow(t, db, b)
			if !r.Enabled || r.IdentitySince == nil || r.IdentitySince.UTC().Format(time.DateTime) != "2026-10-01 10:00:00" {
				t.Fatalf("after re-enable: %+v; want enabled with its first stamp time", r)
			}
		})
	}
	t.Run("mixed markers keep H2's diagnosis", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, identity_since, adoption_reserved_at)
			VALUES (?, 'smcloud', 'smcloud.second', 0, '{"logbook":"portable"}', datetime('now'), datetime('now'))`, b)
		startAsStored(t, m, db)
		if r := rowOf(t, gateView(t, m, homeID), b).Reason; r != wantConflict {
			t.Fatalf("row reason = %q; want %q", r, wantConflict)
		}
		_, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
		wantRefused(t, db, b, err, wantConflict)
		if strings.Contains(err.Error(), "make its logbook the default") {
			t.Fatalf("a conflicting binding was offered the default as its repair: %v", err)
		}
	})
}

func TestGate_FH1_AnEmptyNonDefaultHomeLogbookIsStamped(t *testing.T) {
	t.Run("no binding yet", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		if r := rowOf(t, gateView(t, m, homeID), b).Reason; r != "" {
			t.Fatalf("row reason = %q; want none", r)
		}
		if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		wantStamped(t, db, b)
	})
	t.Run("a disabled unmarked binding", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'smcloud.second', 0, '{"logbook":"main"}')`, b)
		startAsStored(t, m, db)
		if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		wantStamped(t, db, b)
	})
}

// historyRefusal: logbook b of gateHome, prepared by seed, refuses a new enable
// with reason, in the view and on PUT, writing nothing.
func historyRefusal(t *testing.T, reason string, seed func(t *testing.T, db *sqlite.Service, b int64)) {
	t.Helper()
	db, m, _, b := gateHome(t)
	seed(t, db, b)
	startAsStored(t, m, db)
	if r := rowOf(t, gateView(t, m, homeID), b).Reason; r != reason {
		t.Fatalf("row reason = %q; want %q", r, reason)
	}
	before := snapshotRow(t, db, b)
	_, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
	wantRefused(t, db, b, err, reason)
	if after := snapshotRow(t, db, b); after != before {
		t.Fatalf("a refused save wrote: %s → %s", before, after)
	}
}

func TestGate_FH2_AnySmcloudUploadRecordRefuses(t *testing.T) {
	for _, status := range []string{"pending", "in_progress", "uploaded", "failed"} {
		for _, name := range []string{"smcloud", "smcloud.second", "cloud-retired"} {
			t.Run(status+" under "+name, func(t *testing.T) {
				historyRefusal(t, wantHistoryRecords, func(t *testing.T, db *sqlite.Service, b int64) {
					addUpload(t, db, addQso(t, db, b, false), name, "smcloud", "insert", status)
				})
			})
		}
	}
}

func TestGate_FH2_HistoryRefusesBeforeSupport(t *testing.T) {
	for s := range refusingSupport {
		t.Run(string(s), func(t *testing.T) {
			db, m, _, b := gateHome(t)
			addUpload(t, db, addQso(t, db, b, false), "smcloud", "smcloud", "insert", "uploaded")
			supportIs(m, s)
			if r := rowOf(t, gateView(t, m, homeID), b).Reason; r != wantHistoryRecords {
				t.Fatalf("row reason = %q; want the history refusal first (J6)", r)
			}
		})
	}
}

func TestGate_FH3_DeletedQsosAndTombstonesCount(t *testing.T) {
	t.Run("a record on a deleted QSO", func(t *testing.T) {
		historyRefusal(t, wantHistoryRecords, func(t *testing.T, db *sqlite.Service, b int64) {
			addUpload(t, db, addQso(t, db, b, true), "smcloud", "smcloud", "insert", "uploaded")
		})
	})
	t.Run("a tombstone's delete record", func(t *testing.T) {
		historyRefusal(t, wantHistoryRecords, func(t *testing.T, db *sqlite.Service, b int64) {
			addUpload(t, db, addQso(t, db, b, true), "smcloud", "smcloud", "delete", "pending")
		})
	})
}

func TestGate_FH4_QsosWithoutRecordsAreUncertain(t *testing.T) {
	t.Run("live QSOs", func(t *testing.T) {
		historyRefusal(t, wantHistoryQsos, func(t *testing.T, db *sqlite.Service, b int64) { addQso(t, db, b, false) })
	})
	t.Run("deleted QSOs only", func(t *testing.T) {
		historyRefusal(t, wantHistoryQsos, func(t *testing.T, db *sqlite.Service, b int64) { addQso(t, db, b, true) })
	})
}

type historyFailsDB struct{ *sqlite.Service }

func (historyFailsDB) LogbookSmcloudHistoryWithContext(context.Context, int64) (sqlite.LogbookHistory, error) {
	return sqlite.LogbookHistory{}, stderr.New("disk I/O error")
}

func TestGate_FH5_AFailedHistoryReadRefuses(t *testing.T) {
	t.Run("control: the same save with a readable history", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		wantStamped(t, db, b)
	})
	db, m, _, b := gateHome(t)
	failing := historyFailsDB{db}
	rows, _ := db.ListLogbookDestinationsWithContext(context.Background())
	m.SetActiveBindings(failing, rows)
	_, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "disk I/O error") {
		t.Fatalf("PUT = %v; want the history read's failure, never a clean history", err)
	}
	if _, ok := smcloudRow(t, db, b); ok {
		t.Fatal("a save whose history could not be read wrote the binding")
	}
}

func TestGate_FH6_AnotherDestinationsRecordsStillMeanQsos(t *testing.T) {
	historyRefusal(t, wantHistoryQsos, func(t *testing.T, db *sqlite.Service, b int64) {
		addUpload(t, db, addQso(t, db, b, false), "qrz", "qrz", "insert", "uploaded")
	})
}

// qsoBeforeWriteDB logs a QSO into logbook into just before the bindings
// write: after the save was validated, before its transaction.
type qsoBeforeWriteDB struct {
	*sqlite.Service
	t    *testing.T
	into int64
}

func (d qsoBeforeWriteDB) UpsertLogbookDestinationsWithContext(ctx context.Context, rows []sqlite.DestinationUpsert) error {
	addQso(d.t, d.Service, d.into, false)
	return d.Service.UpsertLogbookDestinationsWithContext(ctx, rows)
}

func TestGate_FH7_AQsoWrittenAfterValidationFailsTheSave(t *testing.T) {
	t.Run("control: the same save with no QSO written", func(t *testing.T) {
		db, m, a, b := gateHome(t)
		if _, err := putVia(m, homeID,
			types.LogbookBindingEdit{LogbookID: a, Enabled: true},
			types.LogbookBindingEdit{LogbookID: b, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		wantStamped(t, db, b)
	})
	db, m, a, b := gateHome(t)
	racing := qsoBeforeWriteDB{Service: db, t: t, into: b}
	m.SetActiveBindings(racing, nil)
	_, err := putVia(m, homeID,
		types.LogbookBindingEdit{LogbookID: a, Enabled: true},
		types.LogbookBindingEdit{LogbookID: b, Enabled: true})
	if err == nil {
		t.Fatal("the save stamped a logbook that gained a QSO after validation")
	}
	if !stderr.Is(err, sqlite.ErrLogbookNotEmpty) {
		t.Fatalf("PUT = %v; want the stamping transaction's empty-logbook guard", err)
	}
	if h, herr := db.LogbookSmcloudHistoryWithContext(context.Background(), b); herr != nil || h.Qsos != 1 {
		t.Fatalf("history after the race = %+v, %v; want the injected QSO stored", h, herr)
	}
	for _, l := range []int64{a, b} {
		if _, ok := smcloudRow(t, db, l); ok {
			t.Fatalf("the failed save wrote the binding on logbook %d", l)
		}
	}
}

func TestGate_FH8_AKeptLegacyBindingIsNotConverted(t *testing.T) {
	db, m, _, b := gateHome(t)
	addUpload(t, db, addQso(t, db, b, false), "smcloud.second", "smcloud", "insert", "uploaded")
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
		VALUES (?, 'smcloud', 'smcloud.second', 1, '{"logbook":"main"}')`, b)
	startAsStored(t, m, db)
	for _, s := range []smcloud.SupportState{smcloud.SupportSupported, smcloud.SupportChecking} {
		supportIs(m, s)
		v, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
		if err != nil {
			t.Fatalf("re-saving a kept legacy binding under %s: %v", s, err)
		}
		wantNotStamped(t, db, b)
		if row := rowOf(t, v, b); len(row.HiddenFields) != 0 || row.Reason != "" {
			t.Fatalf("kept legacy row = %+v; want its legacy field shown and no reason", row)
		}
	}
}

func TestGate_FH9_AManagedLogbookHasNoHistoryGate(t *testing.T) {
	db, m, l := gateManaged(t)
	addQso(t, db, l, false)
	addQso(t, db, l, true)
	addUpload(t, db, addQso(t, db, l, false), "qrz", "qrz", "insert", "uploaded")
	startAsStored(t, m, db)
	if _, err := putVia(m, managedID, types.LogbookBindingEdit{LogbookID: l, Enabled: true}); err != nil {
		t.Fatalf("enable a managed logbook holding QSOs: %v", err)
	}
	wantStamped(t, db, l)
}

func TestGate_FH10_HomesDefaultKeepsTheAdoptionPath(t *testing.T) {
	db, m, a, _ := gateHome(t)
	addQso(t, db, a, false)
	for _, s := range []smcloud.SupportState{smcloud.SupportChecking, smcloud.SupportSupported} {
		supportIs(m, s)
		if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: a, Enabled: true}); err != nil {
			t.Fatalf("enable Home's default under %s: %v", s, err)
		}
		r, _ := smcloudRow(t, db, a)
		if !r.Enabled || r.IdentitySince != nil {
			t.Fatalf("Home's default = %+v; want enabled, never stamped", r)
		}
		if row := rowOf(t, gateView(t, m, homeID), a); len(row.HiddenFields) != 0 {
			t.Fatalf("Home's default hides %v; want its legacy field shown", row.HiddenFields)
		}
	}
}

func TestGate_FH11_AClaimedBindingOffTheDefault(t *testing.T) {
	for name, marker := range map[string]string{
		"reserved": `adoption_reserved_at = datetime('now')`,
		"adopted":  `adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = 'fp'`,
	} {
		t.Run(name+", disabled, refuses", func(t *testing.T) {
			db, m, _, b := gateHome(t)
			execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
				VALUES (?, 'smcloud', 'smcloud.second', 0, '{"logbook":"portable"}')`, b)
			execArchive(t, db, `UPDATE logbook_destination SET `+marker+` WHERE logbook_id = ?`, b)
			startAsStored(t, m, db)
			if r := rowOf(t, gateView(t, m, homeID), b).Reason; r != wantClaimed {
				t.Fatalf("row reason = %q; want %q", r, wantClaimed)
			}
			_, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
			wantRefused(t, db, b, err, wantClaimed)
			wantNotStamped(t, db, b)
		})
		t.Run(name+", already enabled, stays", func(t *testing.T) {
			db, m, _, b := gateHome(t)
			execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
				VALUES (?, 'smcloud', 'smcloud.second', 1, '{"logbook":"portable"}')`, b)
			execArchive(t, db, `UPDATE logbook_destination SET `+marker+` WHERE logbook_id = ?`, b)
			startAsStored(t, m, db)
			if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true}); err != nil {
				t.Fatalf("re-saving an enabled claimed binding: %v", err)
			}
			wantNotStamped(t, db, b)
		})
	}
}

func TestGate_FH12_TheRunningGenerationStillUploadsByName(t *testing.T) {
	db, m, _, b := gateHome(t)
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
		VALUES (?, 'smcloud', 'smcloud.second', 1, '{"logbook":"main"}')`, b)
	startAsStored(t, m, db) // this generation started it enabled, on the name wire
	if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: false}); err != nil {
		t.Fatalf("saving the disable: %v", err)
	}
	if r := rowOf(t, gateView(t, m, homeID), b).Reason; r != wantRunning {
		t.Fatalf("row reason before the restart = %q; want %q", r, wantRunning)
	}
	_, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
	wantRefused(t, db, b, err, wantRunning)
	wantNotStamped(t, db, b)
	if r, _ := smcloudRow(t, db, b); r.Enabled {
		t.Fatal("the refused re-enable was written")
	}

	startAsStored(t, m, db) // the restart: the disable has taken effect
	if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true}); err != nil {
		t.Fatalf("after the restart, an empty binding: %v", err)
	}
	wantStamped(t, db, b)
}

// FH12, continued: the restart does not bypass history, and history refuses
// first. (Separate tests: the in-memory archive is shared while one is open.)
func TestGate_FH12_AfterTheRestartHistoryIsCheckedAgain(t *testing.T) {
	{
		db, m, _, b := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'smcloud.second', 0, '{"logbook":"main"}')`, b)
		addQso(t, db, b, false)
		startAsStored(t, m, db)
		_, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
		wantRefused(t, db, b, err, wantHistoryQsos)
	}
}

func TestGate_FH12_HistoryRefusesBeforeTheRunningGeneration(t *testing.T) {
	{
		db, m, _, b := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'smcloud.second', 1, '{"logbook":"main"}')`, b)
		addUpload(t, db, addQso(t, db, b, false), "smcloud.second", "smcloud", "insert", "uploaded")
		startAsStored(t, m, db)
		if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: false}); err != nil {
			t.Fatal(err)
		}
		_, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
		wantRefused(t, db, b, err, wantHistoryRecords)
	}
}

// reservedDefaultWithNamesake: Home's default a reserves "shack"; logbook b
// holds a disabled unmarked binding whose stored legacy name is also "shack".
func reservedDefaultWithNamesake(t *testing.T) (*sqlite.Service, *Manager, int64, int64) {
	t.Helper()
	db, m, a, b := gateHome(t)
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, adoption_reserved_at)
		VALUES (?, 'smcloud', 'cloud', 1, '{"logbook":"shack"}', datetime('now'))`, a)
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
		VALUES (?, 'smcloud', 'smcloud.second', 0, '{"logbook":"shack"}')`, b)
	startAsStored(t, m, db)
	return db, m, a, b
}

func TestGate_SC1_AProposedStampLeavesTheNameCheck(t *testing.T) {
	db, m, _, b := reservedDefaultWithNamesake(t)
	if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true}); err != nil {
		t.Fatalf("enable an eligible namesake: %v; want the proposed stamp to exempt it", err)
	}
	wantStamped(t, db, b)
}

func TestGate_SC2_HistoryRefusesBeforeTheNameCheck(t *testing.T) {
	db, m, _, b := reservedDefaultWithNamesake(t)
	addUpload(t, db, addQso(t, db, b, false), "smcloud.second", "smcloud", "insert", "uploaded")
	_, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
	wantRefused(t, db, b, err, wantHistoryRecords)
}

func TestGate_SC3_AMixedRowIsStillChecked(t *testing.T) {
	db, m, a, b := gateHome(t)
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, adoption_reserved_at)
		VALUES (?, 'smcloud', 'cloud', 1, '{"logbook":"shack"}', datetime('now'))`, a)
	c, err := db.InsertLogbookWithContext(context.Background(), types.Logbook{Name: "Third", Callsign: "M0QQQ"})
	if err != nil {
		t.Fatal(err)
	}
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, identity_since, adoption_reserved_at)
		VALUES (?, 'smcloud', 'smcloud.third', 0, '{"logbook":"portable"}', datetime('now'), datetime('now'))`, c)
	d, err := db.InsertLogbookWithContext(context.Background(), types.Logbook{Name: "Fourth", Callsign: "M0QQQ"})
	if err != nil {
		t.Fatal(err)
	}
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, adoption_reserved_at)
		VALUES (?, 'smcloud', 'smcloud.fourth', 0, '{"logbook":"portable"}', datetime('now'))`, d)
	startAsStored(t, m, db)
	_, err = putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
	if bindingCode(err) != "binding_name_reserved" {
		t.Fatalf("PUT = %v; want the mixed row still checked", err)
	}
	if _, ok := smcloudRow(t, db, b); ok {
		t.Fatal("the refused save wrote the eligible row")
	}
}

func TestGate_SC4_HomesReservedDefaultIsNeverStamped(t *testing.T) {
	db, m, a, _ := gateHome(t)
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, adoption_reserved_at)
		VALUES (?, 'smcloud', 'cloud', 0, '{"logbook":"shack"}', datetime('now'))`, a)
	startAsStored(t, m, db)
	if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: a, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r, _ := smcloudRow(t, db, a)
	if !r.Enabled || r.IdentitySince != nil {
		t.Fatalf("Home's reserved default = %+v; want enabled, never stamped", r)
	}
}

func TestGate_AF1_AHiddenFieldIsRefused(t *testing.T) {
	for name, edit := range map[string]types.LogbookBindingEdit{
		"typed":   {Enabled: true, Credentials: map[string]string{"logbook": "portable"}},
		"cleared": {Enabled: false, CredentialsClear: []string{"logbook"}},
	} {
		t.Run(name+", eligible row", func(t *testing.T) {
			db, m, _, b := gateHome(t)
			edit := edit
			edit.LogbookID = b
			_, err := putVia(m, homeID, edit)
			if bindingCode(err) != "binding_field_unused" {
				t.Fatalf("PUT = %v; want binding_field_unused", err)
			}
			if _, ok := smcloudRow(t, db, b); ok {
				t.Fatal("the refused save wrote the binding")
			}
		})
		t.Run(name+", stamped row", func(t *testing.T) {
			db, m, _, b := gateHome(t)
			execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, identity_since)
				VALUES (?, 'smcloud', 'smcloud.second', 0, '{"logbook":"old"}', datetime('now'))`, b)
			startAsStored(t, m, db)
			before := snapshotRow(t, db, b)
			edit := edit
			edit.LogbookID = b
			_, err := putVia(m, homeID, edit)
			if bindingCode(err) != "binding_field_unused" {
				t.Fatalf("PUT = %v; want binding_field_unused", err)
			}
			if after := snapshotRow(t, db, b); after != before {
				t.Fatalf("the refused save wrote: %s → %s", before, after)
			}
		})
	}
}

func TestGate_AF2_HiddenFieldsPerRowKind(t *testing.T) {
	hidden := []string{"logbook"}
	check := func(t *testing.T, row types.LogbookBindingView, wantHidden, wantLocked []string) {
		t.Helper()
		if !slices.Equal(row.HiddenFields, wantHidden) || !slices.Equal(row.LockedFields, wantLocked) {
			t.Fatalf("logbook %d: hidden %v, locked %v; want hidden %v, locked %v",
				row.LogbookID, row.HiddenFields, row.LockedFields, wantHidden, wantLocked)
		}
	}
	t.Run("managed, unbound", func(t *testing.T) {
		_, m, l := gateManaged(t)
		check(t, rowOf(t, gateView(t, m, managedID), l), hidden, nil)
	})
	t.Run("Home non-default, unbound and empty", func(t *testing.T) {
		_, m, _, b := gateHome(t)
		check(t, rowOf(t, gateView(t, m, homeID), b), hidden, nil)
	})
	t.Run("Home non-default with history", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		addQso(t, db, b, false)
		check(t, rowOf(t, gateView(t, m, homeID), b), hidden, nil)
	})
	t.Run("a kept enabled legacy binding", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'smcloud.second', 1, '{"logbook":"main"}')`, b)
		startAsStored(t, m, db)
		check(t, rowOf(t, gateView(t, m, homeID), b), nil, nil)
	})
	t.Run("Home's default, unmarked", func(t *testing.T) {
		_, m, a, _ := gateHome(t)
		check(t, rowOf(t, gateView(t, m, homeID), a), nil, nil)
	})
	t.Run("Home's default, reserved: locked, not hidden", func(t *testing.T) {
		db, m, a, _ := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, adoption_reserved_at)
			VALUES (?, 'smcloud', 'cloud', 1, '{"logbook":"shack"}', datetime('now'))`, a)
		startAsStored(t, m, db)
		check(t, rowOf(t, gateView(t, m, homeID), a), nil, hidden)
	})
	t.Run("stamped, then its logbook becomes the default: still hidden", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, identity_since)
			VALUES (?, 'smcloud', 'smcloud.second', 1, '{"logbook":"old"}', datetime('now'))`, b)
		m2, _ := homeManagerCfg(t, db, b) // b is now Home's default
		supportIs(m2, smcloud.SupportSupported)
		startAsStored(t, m2, db)
		check(t, rowOf(t, gateView(t, m2, homeID), b), hidden, nil)
		_ = m
	})
	t.Run("mixed markers: locked, not hidden", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, identity_since, adoption_reserved_at)
			VALUES (?, 'smcloud', 'smcloud.second', 0, '{"logbook":"portable"}', datetime('now'), datetime('now'))`, b)
		startAsStored(t, m, db)
		check(t, rowOf(t, gateView(t, m, homeID), b), nil, hidden)
	})
}

func TestGate_DIS_DisablingStaysPossible(t *testing.T) {
	for s := range refusingSupport {
		t.Run("under "+string(s)+", with history, started enabled", func(t *testing.T) {
			db, m, _, b := gateHome(t)
			execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
				VALUES (?, 'smcloud', 'smcloud.second', 1, '{"logbook":"main"}')`, b)
			addUpload(t, db, addQso(t, db, b, false), "smcloud.second", "smcloud", "insert", "uploaded")
			startAsStored(t, m, db)
			supportIs(m, s)
			if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: false}); err != nil {
				t.Fatalf("disable: %v", err)
			}
			if r, _ := smcloudRow(t, db, b); r.Enabled {
				t.Fatal("the disable was not written")
			}
		})
	}
	t.Run("a conflicting binding", func(t *testing.T) {
		db, m, _, b := gateHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, identity_since, adoption_reserved_at)
			VALUES (?, 'smcloud', 'smcloud.second', 1, '{"logbook":"portable"}', datetime('now'), datetime('now'))`, b)
		startAsStored(t, m, db)
		if _, err := putVia(m, homeID, types.LogbookBindingEdit{LogbookID: b, Enabled: false}); err != nil {
			t.Fatalf("disable: %v", err)
		}
	})
}
