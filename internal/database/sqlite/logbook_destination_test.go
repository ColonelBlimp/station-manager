package sqlite

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0082 part 4 (W-0021 5B): the ONE-TIME seed of destination bindings from
// the legacy config entries into the adopted archive, and the listing the
// worker snapshot reads. Idempotent by the marker, atomic, insert-missing.

const (
	lbUUID1 = "01920000-0000-7000-8000-00000000000a"
	lbUUID2 = "01920000-0000-7000-8000-00000000000b"
)

// seedTwoLogbookFile: logbooks 1 (default, when defaultLogbook = 1) and 2, one
// QSO each, and one legacy `qrz` queue row per QSO — the shape config-driven
// forwarding leaves behind.
func seedTwoLogbookFile(t *testing.T, svc *Service, defaultLogbook any, uuid2 any) {
	t.Helper()
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (1, ?, 'M0ABC', 'Main')`, lbUUID1)
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (2, ?, 'M0XYZ', 'Second')`, uuid2)
	execT(t, svc, `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, '01920000-0000-7000-8000-000000000001', ?)`, defaultLogbook)
	for id, lb := range map[int64]int64{1: 1, 2: 2} {
		execT(t, svc, `INSERT INTO qso (id, uuid, call, band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd, country, dedupe_key, logbook_id)
			VALUES (?, ?, 'K1AAA', '20m', 'SSB', 14074000, '20260101', '1200', '1200', '59', '59', 'Test', ?, ?)`,
			id, fmt.Sprintf("01920000-0000-7000-8000-0000000000%02d", id+10), fmt.Sprintf("%064d", id), lb)
		execT(t, svc, `INSERT INTO qso_upload (qso_id, forwarder_name, forwarder_type, action, status, origin) VALUES (?, 'qrz', 'qrz', 'insert', 'pending', 'live')`, id)
	}
}

func stationSeeds() []DestinationSeed {
	return []DestinationSeed{
		{Destination: "qrz", LegacyName: "qrz", Enabled: true, Credentials: json.RawMessage(`{"api_key":"k"}`)},
		{Destination: "qrzcq", LegacyName: "qrzcq", Enabled: false, Credentials: json.RawMessage(`{"call":"x","key":"y"}`)},
	}
}

func bindingKeys(rows []types.LogbookDestination) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%d/%s/%s/%v/%s", r.LogbookID, r.Destination, r.ForwarderName, r.Enabled, string(r.Credentials)))
	}
	return out
}

func TestSeedLogbookDestinations_TwoLogbooks_NamesRenamesAndMarks(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)
	ctx := context.Background()

	if at, err := svc.DestinationBindingsSeededAtWithContext(ctx); err != nil || at != nil {
		t.Fatalf("marker before seed = %v (%v); want nil, no error", at, err)
	}
	res, err := svc.SeedLogbookDestinationsWithContext(ctx, stationSeeds())
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !res.Seeded || res.Inserted != 4 || res.Renamed != 1 {
		t.Fatalf("result = %+v; want seeded, 4 inserted, 1 queue row renamed", res)
	}
	rows, err := svc.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{
		`1/qrz/qrz/true/{"api_key":"k"}`,
		`1/qrzcq/qrzcq/false/{"call":"x","key":"y"}`,
		`2/qrz/qrz.` + lbUUID2 + `/true/{"api_key":"k"}`,
		`2/qrzcq/qrzcq.` + lbUUID2 + `/false/{"call":"x","key":"y"}`,
	}
	if got := bindingKeys(rows); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("bindings = %v\nwant %v", got, want)
	}
	// Every seeded row records the legacy name it derives from — the durable
	// mapping the down migration and the config downgrade collapse to.
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE (destination = 'qrz' AND legacy_name = 'qrz') OR (destination = 'qrzcq' AND legacy_name = 'qrzcq')`); n != 4 {
		t.Fatalf("rows carrying their legacy name = %d, want 4", n)
	}
	for _, r := range rows {
		if r.LegacyName != r.Destination {
			t.Fatalf("binding %s: LegacyName = %q, want %q", r.ForwarderName, r.LegacyName, r.Destination)
		}
	}
	// The default logbook's rows keep the legacy name; the second logbook's are renamed.
	if n := countT(t, svc, `SELECT COUNT(*) FROM qso_upload WHERE qso_id = 1 AND forwarder_name = 'qrz'`); n != 1 {
		t.Fatal("the default logbook's queue row was renamed")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM qso_upload WHERE qso_id = 2 AND forwarder_name = 'qrz.`+lbUUID2+`' AND status = 'pending'`); n != 1 {
		t.Fatal("the second logbook's queue row was not renamed to its binding")
	}
	at, err := svc.DestinationBindingsSeededAtWithContext(ctx)
	if err != nil || at == nil {
		t.Fatalf("marker after seed = %v (%v); want set", at, err)
	}

	// A second call — even with different seeds — changes nothing: the marker
	// makes a committed seed final.
	again, err := svc.SeedLogbookDestinationsWithContext(ctx, []DestinationSeed{{Destination: "clublog", LegacyName: "clublog", Enabled: true}})
	if err != nil || again.Seeded || again.Inserted != 0 {
		t.Fatalf("second seed = %+v (%v); want not seeded, nothing inserted", again, err)
	}
	// A logbook created after the marker stays unbound.
	execT(t, svc, `INSERT INTO logbook (id, uuid, callsign, name) VALUES (3, '01920000-0000-7000-8000-00000000000c', 'M0NEW', 'Later')`)
	rows, _ = svc.ListLogbookDestinationsWithContext(ctx)
	if len(rows) != 4 {
		t.Fatalf("bindings after a later logbook = %d, want 4 (unbound)", len(rows))
	}
}

func TestSeedLogbookDestinations_NoSeeds_MarksOnly(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)
	res, err := svc.SeedLogbookDestinationsWithContext(context.Background(), nil)
	if err != nil || !res.Seeded || res.Inserted != 0 || res.Renamed != 0 {
		t.Fatalf("result = %+v (%v); want seeded with nothing inserted", res, err)
	}
	if at, _ := svc.DestinationBindingsSeededAtWithContext(context.Background()); at == nil {
		t.Fatal("marker not set")
	}
}

func TestSeedLogbookDestinations_NoDefaultLogbook_LowestIDKeepsTheLegacyName(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, nil, lbUUID2)
	res, err := svc.SeedLogbookDestinationsWithContext(context.Background(), stationSeeds()[:1])
	if err != nil || res.Inserted != 2 || res.Renamed != 1 {
		t.Fatalf("result = %+v (%v)", res, err)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE logbook_id = 1 AND forwarder_name = 'qrz'`); n != 1 {
		t.Fatal("logbook 1 did not keep the legacy name")
	}
}

func TestSeedLogbookDestinations_RequiresIdentityAndLogbookUUIDs(t *testing.T) {
	svc := testService(t)
	if _, err := svc.SeedLogbookDestinationsWithContext(context.Background(), stationSeeds()); !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("seed without an identity row: %v, want ErrNotFound", err)
	}
	// A logbook without a uuid cannot be named: nothing commits, the marker stays NULL.
	seedTwoLogbookFile(t, svc, 1, nil)
	if _, err := svc.SeedLogbookDestinationsWithContext(context.Background(), stationSeeds()); err == nil {
		t.Fatal("seed with a uuid-less logbook must fail")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination`); n != 0 {
		t.Fatalf("rows after the failed seed = %d, want 0 (rolled back)", n)
	}
	if at, _ := svc.DestinationBindingsSeededAtWithContext(context.Background()); at != nil {
		t.Fatal("marker set by a failed seed")
	}
}

// Listing skips bindings of a soft-deleted logbook: they are inert, not a worker.
func TestListLogbookDestinations_SkipsDeletedLogbooks(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)
	if _, err := svc.SeedLogbookDestinationsWithContext(context.Background(), stationSeeds()[:1]); err != nil {
		t.Fatal(err)
	}
	execT(t, svc, `UPDATE logbook SET deleted_at = datetime('now') WHERE id = 2`)
	rows, err := svc.ListLogbookDestinationsWithContext(context.Background())
	if err != nil || len(rows) != 1 || rows[0].LogbookID != 1 {
		t.Fatalf("rows = %+v (%v); want only logbook 1's binding", rows, err)
	}
}

// A binding that already exists for a (logbook, destination) keeps its name,
// and the legacy-named queue rows of that logbook follow the EXISTING name —
// never a freshly computed one that no binding carries.
func TestSeedLogbookDestinations_ExistingBindingKeepsItsNameAndTheRowsFollowIt(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)
	execT(t, svc, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled) VALUES (2, 'qrz', 'custom', 1)`)
	res, err := svc.SeedLogbookDestinationsWithContext(context.Background(), stationSeeds()[:1])
	if err != nil || res.Inserted != 1 || res.Renamed != 1 {
		t.Fatalf("result = %+v (%v); want 1 inserted (logbook 1), 1 renamed", res, err)
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM logbook_destination WHERE logbook_id = 2 AND forwarder_name = 'custom'`); n != 1 {
		t.Fatal("the existing binding was renamed or replaced")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM qso_upload WHERE qso_id = 2 AND forwarder_name = 'custom'`); n != 1 {
		t.Fatal("logbook 2's queue row did not follow the existing binding's name")
	}
	if n := countT(t, svc, `SELECT COUNT(*) FROM qso_upload WHERE forwarder_name LIKE 'qrz.%'`); n != 0 {
		t.Fatal("a queue row carries a computed name no binding has")
	}
}
