package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.0 (ruled 2026-10-07): until per-binding SM Cloud identity and
   reconciliation land (5F.4), a NEW SM Cloud enable in the adopted Home archive
   is allowed only on Home's default logbook. Every other Home logbook's SM Cloud
   binding pushes to the same cloud logbook name, so a new enable there would
   create a fresh merge. A binding already enabled (seeded from v5, or enabled
   before 5F.0) is KEPT: it is not repaired, only no new one is made.

     H1  a new enable on the default logbook succeeds.
     H2  a new enable on a non-default logbook is refused — absent row or stored
         disabled row — and the whole PUT writes nothing.
     H3  an existing enabled non-default binding survives an unchanged
         submission and unrelated edits, its name and queue untouched.
     H4  disabling it is allowed; turning it back on is a NEW enable, refused.
     H5  the view names the refusal on each non-default row that is not enabled,
         and for a logbook created now (new_logbook_reason).
*/

func homeSmcloudCfg(defaultID int64) config.Config {
	return config.Config{
		DefaultLogbookID: defaultID,
		Forwarders: []types.ForwarderConfig{
			{Type: "smcloud", Credentials: json.RawMessage(`{"url":"https://c","token":"t"}`)},
			{Name: "qrz", Type: "bindview-qrz", Enabled: true},
		},
	}
}

var homeArchive = &types.QsoArchiveConfig{ID: "h", Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy}

func putSmcloud(ctx context.Context, db BindingsDB, cfg config.Config, rows ...types.LogbookBindingEdit) (types.ArchiveBindingsView, error) {
	return applyBindings(ctx, nil, db, cfg, homeArchive, nil, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
		Type: "smcloud", Logbooks: rows}}})
}

func smcloudRow(t *testing.T, db *sqlite.Service, logbookID int64) (types.LogbookDestination, bool) {
	t.Helper()
	rows, err := db.ListLogbookDestinationsWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.LogbookID == logbookID && r.Destination == "smcloud" {
			return r, true
		}
	}
	return types.LogbookDestination{}, false
}

// existingEnabledSmcloud stores an enabled SM Cloud binding on logbook b as the
// v5 seed (or a pre-5F.0 enable) left it, with two queued rows under its name.
func existingEnabledSmcloud(t *testing.T, db *sqlite.Service, b int64) string {
	t.Helper()
	ctx := context.Background()
	const name = "smcloud.second"
	if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{{
		LogbookID: b, Destination: "smcloud", ForwarderName: name, Enabled: true, Credentials: json.RawMessage(`{"logbook":"main"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	tx, cancel, err := db.BeginTxContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	for i := int64(1); i <= 2; i++ {
		if _, err := tx.ExecContext(ctx, `INSERT INTO qso
			(id, uuid, call, band, mode, freq, qso_date, time_on, time_off,
			 rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?,?,'EA1B','40m','SSB',7050000,'20250508','0845','0845','59','59','Test',?,?)`,
			i, fmt.Sprintf("01920000-0000-7000-8000-%012d", i), fmt.Sprintf("%064d", i), b); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin)
			VALUES (?, ?, 'smcloud', 'insert', 'pending', 'live')`, i, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestHomeSmcloud_H1_NewEnableOnTheDefaultLogbookSucceeds(t *testing.T) {
	db := bindingsDB(t)
	a, _ := twoLogbooks(t, db)
	if _, err := putSmcloud(context.Background(), db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: true}); err != nil {
		t.Fatalf("enable on the default logbook: %v", err)
	}
	if r, ok := smcloudRow(t, db, a); !ok || !r.Enabled {
		t.Fatalf("default-logbook binding = %+v (%v); want enabled", r, ok)
	}
}

func TestHomeSmcloud_H2_NewEnableOnANonDefaultLogbookIsRefusedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	t.Run("absent row, beside a valid default enable", func(t *testing.T) {
		db := bindingsDB(t)
		a, b := twoLogbooks(t, db)
		_, err := putSmcloud(ctx, db, homeSmcloudCfg(a),
			types.LogbookBindingEdit{LogbookID: a, Enabled: true},
			types.LogbookBindingEdit{LogbookID: b, Enabled: true})
		if bindingCode(err) != "binding_not_enableable" || !strings.Contains(err.Error(), `"Second"`) {
			t.Fatalf("non-default enable: %v; want binding_not_enableable naming the logbook", err)
		}
		if _, ok := smcloudRow(t, db, a); ok {
			t.Fatal("the default row was written though the PUT was refused")
		}
		if _, ok := smcloudRow(t, db, b); ok {
			t.Fatal("the refused row was written")
		}
	})
	t.Run("stored disabled row", func(t *testing.T) {
		db := bindingsDB(t)
		a, b := twoLogbooks(t, db)
		if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{{
			LogbookID: b, Destination: "smcloud", ForwarderName: "smcloud.second", Enabled: false, Credentials: json.RawMessage(`{"logbook":"main"}`),
		}}); err != nil {
			t.Fatal(err)
		}
		_, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: b, Enabled: true})
		if bindingCode(err) != "binding_not_enableable" {
			t.Fatalf("enable of a stored disabled non-default row: %v; want binding_not_enableable", err)
		}
		if r, _ := smcloudRow(t, db, b); r.Enabled {
			t.Fatal("the stored disabled row was turned on")
		}
	})
}

func TestHomeSmcloud_H3_ExistingEnabledNonDefaultBindingSurvives(t *testing.T) {
	ctx := context.Background()
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	name := existingEnabledSmcloud(t, db, b)
	cfg := homeSmcloudCfg(a)
	before, err := db.ForwarderQueueCountsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before[name].Waiting != 2 {
		t.Fatalf("fixture queue = %+v; want 2 waiting", before[name])
	}
	steps := []struct {
		what string
		req  types.ArchiveBindingsRequest
	}{
		{"unchanged submission", types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "smcloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: b, Enabled: true}}}}}},
		{"its own field edited while on", types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "smcloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: b, Enabled: true, Credentials: map[string]string{"logbook": "main"}}}}}}},
		{"the default logbook enabled beside it", types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "smcloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true}, {LogbookID: b, Enabled: true}}}}}},
		{"another destination edited", types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "bindview-qrz", Logbooks: []types.LogbookBindingEdit{{LogbookID: b, Enabled: true, Credentials: map[string]string{"api_key": "k"}}}}}}},
	}
	for _, s := range steps {
		if _, err := applyBindings(ctx, nil, db, cfg, homeArchive, nil, s.req); err != nil {
			t.Fatalf("%s: %v; an existing enabled binding must stay editable", s.what, err)
		}
		r, ok := smcloudRow(t, db, b)
		if !ok || !r.Enabled || r.ForwarderName != name {
			t.Fatalf("after %s: binding = %+v; want %s still enabled", s.what, r, name)
		}
	}
	after, err := db.ForwarderQueueCountsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after[name] != before[name] {
		t.Fatalf("queue after the edits = %+v; want %+v unchanged", after[name], before[name])
	}
	v, err := BindingsView(ctx, db, cfg, homeArchive, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range destView(t, v, "smcloud").Logbooks {
		if row.LogbookID == b && (row.Reason != "" || !row.Enabled) {
			t.Fatalf("existing enabled row in the view = %+v; want enabled, no refusal", row)
		}
	}
}

func TestHomeSmcloud_H4_DisableAllowedThenReEnableIsANewEnable(t *testing.T) {
	ctx := context.Background()
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	name := existingEnabledSmcloud(t, db, b)
	cfg := homeSmcloudCfg(a)
	if _, err := putSmcloud(ctx, db, cfg, types.LogbookBindingEdit{LogbookID: b, Enabled: false}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if r, _ := smcloudRow(t, db, b); r.Enabled || r.ForwarderName != name {
		t.Fatalf("after disable = %+v; want %s off", r, name)
	}
	_, err := putSmcloud(ctx, db, cfg, types.LogbookBindingEdit{LogbookID: b, Enabled: true})
	if bindingCode(err) != "binding_not_enableable" {
		t.Fatalf("re-enable after a saved disable: %v; want binding_not_enableable", err)
	}
	if r, _ := smcloudRow(t, db, b); r.Enabled {
		t.Fatal("the re-enable was written")
	}
}

func TestHomeSmcloud_H5_ViewNamesTheRefusalPerRowAndForANewLogbook(t *testing.T) {
	ctx := context.Background()
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	c, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Third", Callsign: "M0DEF"})
	if err != nil {
		t.Fatal(err)
	}
	existingEnabledSmcloud(t, db, b)
	v, err := BindingsView(ctx, db, homeSmcloudCfg(a), homeArchive, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := destView(t, v, "smcloud")
	if d.Reason != "" {
		t.Fatalf("destination reason = %q; want none (the default logbook can still be turned on)", d.Reason)
	}
	if !strings.Contains(d.NewLogbookReason, "default logbook") {
		t.Fatalf("new_logbook_reason = %q; want the default-logbook-only reason", d.NewLogbookReason)
	}
	for _, row := range d.Logbooks {
		switch row.LogbookID {
		case a, b:
			if row.Reason != "" {
				t.Fatalf("row %q reason = %q; want none", row.LogbookName, row.Reason)
			}
		case c:
			if !strings.Contains(row.Reason, "default logbook") {
				t.Fatalf("row %q reason = %q; want the default-logbook-only reason", row.LogbookName, row.Reason)
			}
		}
	}
	// Other destinations are unaffected.
	q := destView(t, v, "bindview-qrz")
	if q.NewLogbookReason != "" {
		t.Fatalf("QRZ-like new_logbook_reason = %q; want none", q.NewLogbookReason)
	}
	for _, row := range q.Logbooks {
		if row.Reason != "" {
			t.Fatalf("QRZ-like row reason = %q; want none", row.Reason)
		}
	}
}
