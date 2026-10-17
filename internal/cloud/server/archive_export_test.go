package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"
)

// W-0021 Q7 (ADR 0071 refinement, 2026-10-16): the reads whole-archive
// provisioning needs. PQ1: a listing, and the whole archive in one snapshot
// behind a new capability, reusing the export admission and cancellation.
// PQ5: an authoritative legacy marker.
//
//	AX1 GET /v1/archives lists the tenant's identity archives, equal labels
//	    apart, each with its logbooks (UUID, label, callsign, row and
//	    tombstone counts), the legacy marker on the adopted legacy archive,
//	    the not-adopted legacy logbooks with no UUID, each by its stored
//	    legacy name, and nothing of another tenant's.
//	AX2 GET /v1/archives/{uuid}/export is the complete archive: the head
//	    names the archive and every logbook, an empty one included, with
//	    counts that agree with the rows; every row names its logbook's UUID
//	    and keeps revision, microsecond modified_at, tombstone and payload;
//	    no row of the equal-labelled archive.
//	AX3 the adopted legacy archive's export carries legacy: true and every
//	    row resolves to exactly one head logbook: the adopted one by UUID,
//	    each not-adopted one (equal display labels, one stored name empty)
//	    by uuid null plus its exact legacy_name, its rows, tombstones
//	    included, by logbook_uuid null plus logbook_legacy_name. A UUID-
//	    bearing logbook or row carries neither fallback field.
//	AX4 404 for another tenant's archive, an unknown archive or a malformed
//	    UUID, against the control that the tenant's own archive exports.
//	AX5 the export is admitted through the export gate: 503 overloaded with
//	    Retry-After when every slot is taken, before any store access.
//	AX6 a client gone while the export waits in the store cancels the
//	    store's query and frees its slot, without the wait ending.
//	AX7 GET /v1/version advertises archive_export 1.
//	AX8 a logbook with neither a UUID nor a legacy name fails the listing
//	    and the export before the export's head is written.
//
// Snapshot consistency under a barrier-controlled move is AS1, in the store.

const (
	sameLabelUUID = "01920000-0000-7000-8000-000000000c02"
	sameLabelLB   = "01920000-0000-7000-8000-000000000d03"
	emptyLB       = "01920000-0000-7000-8000-000000000d04"
	otherTenantA  = "01920000-0000-7000-8000-000000000c09"
	otherTenantLB = "01920000-0000-7000-8000-000000000d09"
	axLive        = "0197f9a0-0000-7000-8000-000000000301"
	axGone        = "0197f9a0-0000-7000-8000-000000000302"
	axSame        = "0197f9a0-0000-7000-8000-000000000303"
	axOther       = "0197f9a0-0000-7000-8000-000000000304"
	axPortable    = "0197f9a0-0000-7000-8000-000000000305"
	axFieldLive   = "0197f9a0-0000-7000-8000-000000000306"
	axFieldGone   = "0197f9a0-0000-7000-8000-000000000307"
	axBlank       = "0197f9a0-0000-7000-8000-000000000308"
)

// axPushAt carries microseconds: the export must not round them away.
var axPushAt = time.Date(2026, 7, 17, 6, 0, 0, 123456000, time.UTC)

type axLogbook struct {
	UUID       *string `json:"uuid"`
	LegacyName *string `json:"legacy_name"`
	Label      string  `json:"label"`
	Callsign   string  `json:"callsign"`
	Count      int     `json:"count"`
	Tombstones int     `json:"tombstones"`
	keys       map[string]json.RawMessage
}

// UnmarshalJSON also keeps the keys present, so an omitted field is told
// from a null one.
func (lb *axLogbook) UnmarshalJSON(b []byte) error {
	type plain axLogbook
	if err := json.Unmarshal(b, (*plain)(lb)); err != nil {
		return err
	}
	return json.Unmarshal(b, &lb.keys)
}

type axRow struct {
	UUID              string          `json:"uuid"`
	LogbookUUID       *string         `json:"logbook_uuid"`
	LogbookLegacyName *string         `json:"logbook_legacy_name"`
	ModifiedAt        time.Time       `json:"modified_at"`
	Revision          int64           `json:"revision"`
	DeletedAt         *time.Time      `json:"deleted_at"`
	Qso               json.RawMessage `json:"qso"`
	keys              map[string]json.RawMessage
}

func (r *axRow) UnmarshalJSON(b []byte) error {
	type plain axRow
	if err := json.Unmarshal(b, (*plain)(r)); err != nil {
		return err
	}
	return json.Unmarshal(b, &r.keys)
}

type axArchive struct {
	UUID     string      `json:"uuid"`
	Label    string      `json:"label"`
	Legacy   bool        `json:"legacy"`
	Logbooks []axLogbook `json:"logbooks"`
}

type axListing struct {
	Archives []axArchive `json:"archives"`
}

type axExport struct {
	Archive struct {
		UUID   string `json:"uuid"`
		Label  string `json:"label"`
		Legacy bool   `json:"legacy"`
	} `json:"archive"`
	Logbooks []axLogbook `json:"logbooks"`
	Qsos     []axRow     `json:"qsos"`
}

// mainUUID is pushMain's row, in the legacy main logbook.
const mainUUID = "0197f9a0-0000-7000-8000-0000000000f1"

// axFixture is what axTenant stored: each row's pushed payload, and the
// tombstone's deletion time.
type axFixture struct {
	payloads map[string]json.RawMessage
	deleted  time.Time
}

// axPayload is a modelled QSO plus a field this build does not model, nested,
// carrying integers float64 cannot hold (2^53+1) and beyond int64: an exporter
// that drops unknown keys or decodes numbers through float64 changes it.
func axPayload(t *testing.T, uuid, call string) json.RawMessage {
	t.Helper()
	q := fixtureQso(uuid)
	q.Call = call
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	top["x_future"] = json.RawMessage(`{"deep":{"big":9007199254740993,"huge":123456789012345678901234567890,"list":[1,{"k":null}]}}`)
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// pgJSONEqual is PostgreSQL's JSONB equality, the store's own oracle.
func pgJSONEqual(t *testing.T, db *sql.DB, a, b json.RawMessage) bool {
	t.Helper()
	var eq bool
	if err := db.QueryRow(`SELECT $1::jsonb = $2::jsonb`, string(a), string(b)).Scan(&eq); err != nil {
		t.Fatalf("jsonb equality: %v", err)
	}
	return eq
}

// floatCollapsed is raw decoded through float64 and re-encoded.
func floatCollapsed(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// axTenant seeds the tenant: the managed "Contest" (contestLB with a live row
// and a tombstone, emptyLB with none), an equal-labelled managed "Contest"
// with one row, the adopted legacy archive, and another tenant's archive. The
// legacy archive has main adopted and three logbooks not adopted, all
// labelled "Field": portable (one row), contest (a live row and a tombstone)
// and one whose stored legacy name is empty (one row).
func axTenant(t *testing.T) (*httptest.Server, *sql.DB, axFixture) {
	t.Helper()
	ts, _, _, db := newTestServer(t, quietLog)
	fx := axFixture{payloads: map[string]json.RawMessage{}, deleted: axPushAt.Add(time.Hour)}
	push := func(token, archive, logbook string, ups []QsoUpload) {
		t.Helper()
		var out map[string]any
		resp := do(t, http.MethodPut, ts.URL+"/v1/archives/"+archive+"/logbooks/"+logbook+"/qsos", token,
			identityPut{ArchiveLabel: "Contest", LogbookLabel: "main", Callsign: "7Q5MLV/P", Qsos: ups}, &out)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("seed push %s/%s: %d %v", archive, logbook, resp.StatusCode, out)
		}
	}
	live := axPayload(t, axLive, "DL9UW")
	fx.payloads[axLive] = live
	push(testToken, contestUUID, contestLB, []QsoUpload{{ModifiedAt: axPushAt, Revision: 1, Qso: live}})
	push(testToken, contestUUID, contestLB, oneQso(t, axGone, "EA1B", 1, axPushAt))
	tomb := oneQso(t, axGone, "EA1B", 2, fx.deleted)
	tomb[0].DeletedAt = &fx.deleted
	fx.payloads[axGone] = tomb[0].Qso
	push(testToken, contestUUID, contestLB, tomb)
	if _, err := db.Exec(`INSERT INTO logbooks (tenant_id, archive_id, uuid, label, callsign)
		SELECT tenant_id, id, $1, 'empty', '7Q5MLV' FROM archives WHERE archive_uuid = $2`, emptyLB, contestUUID); err != nil {
		t.Fatalf("plant the empty logbook: %v", err)
	}
	push(testToken, sameLabelUUID, sameLabelLB, oneQso(t, axSame, "OH2BH", 1, axPushAt))
	push(otherToken, otherTenantA, otherTenantLB, oneQso(t, axOther, "K1ABC", 1, axPushAt))

	pushMain(t, ts)
	if status, out := adopt(t, ts.URL, adoptBody("main", homeUUID, defaultLBUID)); status != http.StatusOK {
		t.Fatalf("adopt: %d %v", status, out)
	}
	legacy := func(name, uuid string, deletedAt *time.Time) {
		t.Helper()
		p, _ := json.Marshal(fixtureQso(uuid))
		fx.payloads[uuid] = p
		at := axPushAt
		if deletedAt != nil {
			putQsos(t, ts, testToken, name, []QsoUpload{{ModifiedAt: at, Qso: p}})
			at = *deletedAt
		}
		putQsos(t, ts, testToken, name, []QsoUpload{{ModifiedAt: at, DeletedAt: deletedAt, Qso: p}})
	}
	legacy("portable", axPortable, nil)
	legacy("contest", axFieldLive, nil)
	legacy("contest", axFieldGone, &fx.deleted)
	legacy("blank", axBlank, nil)
	if _, err := db.Exec(`UPDATE logbooks SET label = 'Field', legacy_name = CASE legacy_name WHEN 'blank' THEN '' ELSE legacy_name END
		WHERE legacy_name IN ('portable', 'contest', 'blank') AND uuid IS NULL`); err != nil {
		t.Fatalf("label the not-adopted logbooks: %v", err)
	}
	return ts, db, fx
}

// byUUID and byName are the keys a logbook resolves by: its UUID, or, with
// none, its stored legacy name.
func byUUID(uuid string) string { return "uuid " + uuid }
func byName(name string) string { return "legacy_name " + strconv.Quote(name) }

// axKey is the one logbook a head entry or a row names. With a UUID the
// fallback field must be absent; without one the stored legacy name must be
// present.
func axKey(t *testing.T, where string, uuid, legacyName *string, fallback string, keys map[string]json.RawMessage) string {
	t.Helper()
	if uuid != nil {
		if _, ok := keys[fallback]; ok {
			t.Fatalf("%s: carries %s %s beside its UUID %s; want it omitted", where, fallback, keys[fallback], *uuid)
		}
		return byUUID(*uuid)
	}
	if legacyName == nil {
		t.Fatalf("%s: neither a UUID nor a %s (keys %v): its logbook cannot be told from another without a UUID",
			where, fallback, slices.Sorted(maps.Keys(keys)))
	}
	return byName(*legacyName)
}

// axLogbooks indexes logbooks by the one key each resolves by, refusing a
// duplicate.
func axLogbooks(t *testing.T, where string, lbs []axLogbook) map[string]axLogbook {
	t.Helper()
	out := map[string]axLogbook{}
	for _, lb := range lbs {
		key := axKey(t, where+" logbook "+lb.Label, lb.UUID, lb.LegacyName, "legacy_name", lb.keys)
		if _, dup := out[key]; dup {
			t.Fatalf("%s: logbook %q listed twice: %+v", where, key, lbs)
		}
		out[key] = lb
	}
	return out
}

func wantLogbook(t *testing.T, where string, got map[string]axLogbook, key, label, callsign string, count, tombstones int) {
	t.Helper()
	lb, ok := got[key]
	if !ok || lb.Label != label || lb.Callsign != callsign || lb.Count != count || lb.Tombstones != tombstones {
		t.Fatalf("%s: logbook %s = %+v (present %v); want %s, %q, %d rows, %d tombstones", where, key, lb, ok, label, callsign, count, tombstones)
	}
}

// wantHomeLogbooks is the adopted legacy archive's logbooks: main by UUID,
// the three not adopted by their exact stored names under one label.
func wantHomeLogbooks(t *testing.T, where string, lbs []axLogbook) {
	t.Helper()
	h := axLogbooks(t, where, lbs)
	if len(h) != 4 {
		t.Fatalf("%s: logbooks = %+v; want exactly main and the three not adopted", where, lbs)
	}
	wantLogbook(t, where, h, byUUID(defaultLBUID), "Default", "7Q5MLV", 1, 0)
	wantLogbook(t, where, h, byName("portable"), "Field", "", 1, 0)
	wantLogbook(t, where, h, byName("contest"), "Field", "", 2, 1)
	wantLogbook(t, where, h, byName(""), "Field", "", 1, 0)
}

func TestArchiveExport_AX1_TheListing(t *testing.T) {
	ts, _, _ := axTenant(t)
	var l axListing
	resp := do(t, http.MethodGet, ts.URL+"/v1/archives", testToken, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/archives = %d; want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&l); err != nil {
		t.Fatalf("decode the listing: %v", err)
	}
	got := map[string]axArchive{}
	for _, a := range l.Archives {
		if _, dup := got[a.UUID]; dup {
			t.Fatalf("archive %s listed twice", a.UUID)
		}
		got[a.UUID] = a
	}
	if len(got) != 3 {
		t.Fatalf("listed archives %v; want exactly Contest, the equal-labelled Contest and the adopted legacy archive", slices.Collect(maps.Keys(got)))
	}
	contest, same, home := got[contestUUID], got[sameLabelUUID], got[homeUUID]
	if contest.Label != "Contest" || same.Label != "Contest" || contest.Legacy || same.Legacy {
		t.Fatalf("the two Contest archives = %+v / %+v; want both labelled Contest, neither legacy", contest, same)
	}
	if !home.Legacy || home.Label != "Home" {
		t.Fatalf("the adopted legacy archive = %+v; want Home, legacy true", home)
	}
	c := axLogbooks(t, "Contest", contest.Logbooks)
	wantLogbook(t, "Contest", c, byUUID(contestLB), "main", "7Q5MLV/P", 2, 1)
	wantLogbook(t, "Contest", c, byUUID(emptyLB), "empty", "7Q5MLV", 0, 0)
	sm := axLogbooks(t, "equal-labelled", same.Logbooks)
	wantLogbook(t, "equal-labelled", sm, byUUID(sameLabelLB), "main", "7Q5MLV/P", 1, 0)
	if len(c) != 2 || len(sm) != 1 {
		t.Fatalf("logbooks: Contest %d, equal-labelled %d; want 2 and 1", len(c), len(sm))
	}
	wantHomeLogbooks(t, "Home", home.Logbooks)

	var other axListing
	do(t, http.MethodGet, ts.URL+"/v1/archives", otherToken, nil, &other)
	if len(other.Archives) != 1 || other.Archives[0].UUID != otherTenantA {
		t.Fatalf("the other tenant's listing = %+v; want only its own archive", other.Archives)
	}
}

type axWant struct {
	logbook    string
	revision   int64
	modifiedAt time.Time
	deletedAt  *time.Time
	payload    json.RawMessage
}

// axGet exports archive and indexes its rows by UUID, refusing a duplicate.
func axGet(t *testing.T, ts *httptest.Server, archive string) (axExport, map[string]int) {
	t.Helper()
	resp := do(t, http.MethodGet, ts.URL+"/v1/archives/"+archive+"/export", testToken, nil, nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/archives/%s/export = %d %s; want 200", archive, resp.StatusCode, body)
	}
	if !bytes.HasSuffix(bytes.TrimSpace(body), []byte("]}")) {
		t.Fatalf("the export is not a complete document: …%s", body[max(0, len(body)-40):])
	}
	var e axExport
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("decode the export: %v", err)
	}
	idx := map[string]int{}
	for i, q := range e.Qsos {
		if _, dup := idx[q.UUID]; dup {
			t.Fatalf("row %s exported twice", q.UUID)
		}
		idx[q.UUID] = i
	}
	return e, idx
}

func wantRows(t *testing.T, db *sql.DB, e axExport, idx map[string]int, want map[string]axWant) {
	t.Helper()
	if len(idx) != len(want) {
		t.Fatalf("exported rows %v; want exactly %v", slices.Collect(maps.Keys(idx)), slices.Collect(maps.Keys(want)))
	}
	for uuid, w := range want {
		i, ok := idx[uuid]
		if !ok {
			t.Fatalf("row %s missing from the export", uuid)
		}
		q := e.Qsos[i]
		if got := axKey(t, "row "+uuid, q.LogbookUUID, q.LogbookLegacyName, "logbook_legacy_name", q.keys); got != w.logbook {
			t.Fatalf("row %s is in logbook %s; want %s", uuid, got, w.logbook)
		}
		if q.Revision != w.revision || !q.ModifiedAt.Equal(w.modifiedAt) {
			t.Fatalf("row %s revision %d modified_at %s; want %d at %s", uuid, q.Revision,
				q.ModifiedAt.Format(time.RFC3339Nano), w.revision, w.modifiedAt.Format(time.RFC3339Nano))
		}
		if (q.DeletedAt == nil) != (w.deletedAt == nil) || (q.DeletedAt != nil && !q.DeletedAt.Equal(*w.deletedAt)) {
			t.Fatalf("row %s deleted_at %v; want %v", uuid, q.DeletedAt, w.deletedAt)
		}
		if !pgJSONEqual(t, db, q.Qso, w.payload) {
			t.Fatalf("row %s payload %s; want %s", uuid, q.Qso, w.payload)
		}
	}
}

func TestArchiveExport_AX2_TheCompleteArchive(t *testing.T) {
	ts, db, fx := axTenant(t)
	// The oracle sees what the float64 decode loses (control).
	if pgJSONEqual(t, db, fx.payloads[axLive], floatCollapsed(t, fx.payloads[axLive])) {
		t.Fatal("control: JSONB equality did not tell a float64-collapsed payload from the original")
	}
	e, idx := axGet(t, ts, contestUUID)
	if e.Archive.UUID != contestUUID || e.Archive.Label != "Contest" || e.Archive.Legacy {
		t.Fatalf("head archive = %+v; want Contest by UUID, not legacy", e.Archive)
	}
	lbs := axLogbooks(t, "head", e.Logbooks)
	if len(lbs) != 2 {
		t.Fatalf("head logbooks = %+v; want exactly main and the empty logbook", e.Logbooks)
	}
	wantLogbook(t, "head", lbs, byUUID(contestLB), "main", "7Q5MLV/P", 2, 1)
	wantLogbook(t, "head", lbs, byUUID(emptyLB), "empty", "7Q5MLV", 0, 0)
	main := byUUID(contestLB)
	wantRows(t, db, e, idx, map[string]axWant{
		axLive: {main, 1, axPushAt, nil, fx.payloads[axLive]},
		axGone: {main, 2, fx.deleted, &fx.deleted, fx.payloads[axGone]},
	})
}

func TestArchiveExport_AX3_TheLegacyMarker(t *testing.T) {
	ts, db, fx := axTenant(t)
	e, idx := axGet(t, ts, homeUUID)
	if !e.Archive.Legacy || e.Archive.UUID != homeUUID {
		t.Fatalf("head archive = %+v; want the legacy marker set", e.Archive)
	}
	wantHomeLogbooks(t, "head", e.Logbooks)
	mainPayload, _ := json.Marshal(fixtureQso(mainUUID))
	wantRows(t, db, e, idx, map[string]axWant{
		mainUUID:    {byUUID(defaultLBUID), 0, pushAt, nil, mainPayload},
		axPortable:  {byName("portable"), 0, axPushAt, nil, fx.payloads[axPortable]},
		axFieldLive: {byName("contest"), 0, axPushAt, nil, fx.payloads[axFieldLive]},
		axFieldGone: {byName("contest"), 0, fx.deleted, &fx.deleted, fx.payloads[axFieldGone]},
		axBlank:     {byName(""), 0, axPushAt, nil, fx.payloads[axBlank]},
	})
}

func TestArchiveExport_AX4_NotFoundOutsideTheTenant(t *testing.T) {
	ts, _, _ := axTenant(t)
	// The control: without it a missing route's 404 would pass every case.
	if resp := do(t, http.MethodGet, ts.URL+"/v1/archives/"+contestUUID+"/export", testToken, nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("control: the tenant's own archive exports %d; want 200", resp.StatusCode)
	}
	for _, c := range []struct{ name, archive, token string }{
		{"another tenant's archive", otherTenantA, testToken},
		{"this tenant's archive for another tenant", contestUUID, otherToken},
		{"unknown archive", "01920000-0000-7000-8000-000000000cff", testToken},
		{"malformed uuid", "nope", testToken},
	} {
		resp := do(t, http.MethodGet, ts.URL+"/v1/archives/"+c.archive+"/export", c.token, nil, nil)
		var e struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if resp.StatusCode != http.StatusNotFound || e.Code != "not_found" {
			t.Errorf("%s: %d %q; want 404 not_found", c.name, resp.StatusCode, e.Code)
		}
	}
}

func TestArchiveExport_AX5_AdmittedThroughTheExportGate(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(nil, nil, quiet, map[string]int64{"tok-export-gate": 7}, "test", 0)
	for i := 0; i < maxConcurrentExports; i++ {
		srv.exportSlots <- struct{}{}
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp := do(t, http.MethodGet, ts.URL+"/v1/archives/"+contestUUID+"/export", "tok-export-gate", nil, nil)
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusServiceUnavailable || body.Code != "overloaded" || resp.Header.Get("Retry-After") != exportRetryAfterSeconds {
		t.Fatalf("a gated archive export = %d %q Retry-After %q; want 503 overloaded with Retry-After %s",
			resp.StatusCode, body.Code, resp.Header.Get("Retry-After"), exportRetryAfterSeconds)
	}
}

func TestArchiveExport_AX6_AGoneClientCancelsTheStoreRead(t *testing.T) {
	ts, srv, _, _, db := newTestServerFull(t, quietLog)
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{Qsos: oneQso(t, axLive, "DL9UW", 1, axPushAt)})

	// The barrier: an ACCESS EXCLUSIVE lock on qsos holds the export inside
	// the store until this transaction ends.
	lock, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err := lock.Exec(`LOCK TABLE qsos IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/archives/"+contestUUID+"/export", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- 0
			return
		}
		_ = resp.Body.Close()
		done <- resp.StatusCode
	}()

	waiting := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pg_locks WHERE NOT granted AND relation = 'qsos'::regclass`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("%s within 5s", what)
			}
			select {
			case status := <-done:
				if status != 0 {
					t.Fatalf("the export answered %d without waiting in the store; want it held by the lock (%s)", status, what)
				}
				done <- status
			default:
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	until("the export never waited on the qsos lock", func() bool { return waiting() == 1 })
	if len(srv.exportSlots) != 1 {
		t.Fatalf("export slots in use = %d while the export waits; want 1 (admitted through the gate)", len(srv.exportSlots))
	}
	cancel()
	until("the store's query was not cancelled", func() bool { return waiting() == 0 })
	until("the export slot was not freed", func() bool { return len(srv.exportSlots) == 0 })
	// Released only now: the wait ended by cancellation, not by the lock.
	_ = lock.Rollback()
}

func TestArchiveExport_AX7_TheCapability(t *testing.T) {
	ts := httptest.NewServer(versionServer().Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/version")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if v["archive_export"] != float64(1) || v["identity_protocol"] != float64(1) {
		t.Fatalf("GET /v1/version = %v; want archive_export 1 beside identity_protocol 1", v)
	}
}

func TestArchiveExport_AX8_ALogbookWithNoIdentityFails(t *testing.T) {
	ts, db, _ := axTenant(t)
	// The control: the archive lists and exports while every logbook has one.
	axGet(t, ts, homeUUID)
	if _, err := db.Exec(`UPDATE logbooks SET legacy_name = NULL WHERE legacy_name = 'portable' AND uuid IS NULL`); err != nil {
		t.Fatalf("strip portable's legacy name: %v", err)
	}
	resp := do(t, http.MethodGet, ts.URL+"/v1/archives/"+homeUUID+"/export", testToken, nil, nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError || bytes.Contains(body, []byte(`"archive"`)) {
		t.Fatalf("export with a logbook of no identity = %d %s; want 500 before the head is written", resp.StatusCode, body)
	}
	resp = do(t, http.MethodGet, ts.URL+"/v1/archives", testToken, nil, nil)
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError || bytes.Contains(body, []byte(`"archives"`)) {
		t.Fatalf("listing with a logbook of no identity = %d %s; want 500", resp.StatusCode, body)
	}
}
