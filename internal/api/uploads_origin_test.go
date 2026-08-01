package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Contract 3 of Diff B (docs/reviews/forwarding-logging-gaps.md F1) — the
// deliberate public API addition. Operator's wording, 2026-08-01:
//
//	When I inspect a QSO's uploads through GET /v1/qso/{uuid}/uploads, every item
//	carries the same origin stored on its queue row, including legacy for rows
//	migrated from the previous schema; the field is never absent or empty.
//
// Additive and backward-compatible, on an endpoint that is already the
// queue-status/provenance surface. `uploadsResponse.Items` serialises
// `types.QsoUpload` directly, so the wire shape follows the struct — the
// operator's decision over a parallel response DTO, which would have duplicated
// the type without protecting anything. api-endpoints.md updates in the same
// commit as the field.
//
// Asserted on the DECODED JSON, not the Go struct: the contract is what a client
// receives, and a struct-level assertion would still pass if the field were
// tagged `omitempty` and silently vanished. That is the trap
// RigStatePayload.SplitOverride uses a *bool to avoid, and it is why "never
// absent or empty" is in the criterion rather than merely "carries origin".
//
// SPLIT OF PROOF, deliberately: this file pins that the wire mirrors the stored
// value and never omits the field. The `legacy` half of the criterion is pinned
// in internal/database/sqlite (TestMigrate0007_ExistingRowsBecomeLegacy...),
// because a migrated row can only be produced by running the migration and this
// package exports no raw-SQL handle to forge one. Together they cover the
// criterion; neither alone does, so do not delete one as redundant.

// uploadOriginItems drives GET /v1/qso/{uuid}/uploads and returns the items as
// decoded maps, so a missing key is distinguishable from an empty value.
func uploadOriginItems(t *testing.T, srv *Server, qsoUUID string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/qso/"+qsoUUID+"/uploads", nil)
	req.SetPathValue("uuid", qsoUUID)
	w := httptest.NewRecorder()
	srv.handleListQsoUploads(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	return resp.Items
}

func TestUploadsEndpoint_ExposesOriginOnEveryItem(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"),
		forwarderCfg("clublog", "clublog", true, "insert"),
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	_, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	items := uploadOriginItems(t, srv, qsoUUID)
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	for i, it := range items {
		v, present := it["origin"]
		if !present {
			t.Errorf("item %d: `origin` absent from the JSON — the field must not be "+
				"tagged omitempty; a client cannot tell absent from unknown", i)
			continue
		}
		s, _ := v.(string)
		if s == "" {
			t.Errorf("item %d: `origin` is empty; every queue row has a provenance", i)
		}
		// A QSO logged through POST /v1/qso is live traffic, so the wire must show
		// the value the producer stored — not a placeholder, and not the migration's
		// `legacy`.
		if s != "live" {
			t.Errorf("item %d: origin = %q, want live for a normally-logged QSO", i, s)
		}
	}
}

// A QSO deleted through the API enqueues a delete row, whose origin is `edit`:
// origin explains WHY the queue entry exists, and `action` already says WHAT
// mutation occurred, so a delete is the same operator-driven logbook mutation
// class as an update (operator, 2026-08-01).
//
// This is the fixture that would catch an implementation threading a single
// hard-coded origin through every producer — it would satisfy the test above and
// fail here.
func TestUploadsEndpoint_DeleteRowCarriesEditOrigin(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"),
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	_, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	req := httptest.NewRequest(http.MethodDelete, "/v1/qso/"+qsoUUID, nil)
	req.SetPathValue("uuid", qsoUUID)
	w := httptest.NewRecorder()
	srv.handleDeleteQso(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, body = %s", w.Code, w.Body.String())
	}

	var deleteRow map[string]any
	for _, it := range uploadOriginItems(t, srv, qsoUUID) {
		if it["action"] == "delete" {
			deleteRow = it
			break
		}
	}
	if deleteRow == nil {
		t.Fatal("no delete upload row returned")
	}
	if got := deleteRow["origin"]; got != "edit" {
		t.Errorf("delete row origin = %v, want edit", got)
	}
}

// Contract 2's re-enqueue half, driven through TWO REAL PRODUCERS.
//
// This replaces an earlier version living in internal/database/sqlite that
// embedded the intended `ON CONFLICT ... origin = excluded.origin` statement in
// the test body. That test would have passed against any production UPSERT at
// all — including one that never touched origin — because it exercised SQL it
// owned rather than SQL the daemon runs. A proof of test-owned SQL is not a proof.
//
// Here the row is created by live logging (POST /v1/qso) and then re-enqueued by
// the manual backfill endpoint (POST /v1/forwarder/{name}/uploads) with force, so
// the assertion runs against the real UPSERT reached through the real shared
// EnqueueUploads path. It pins two things at once: that a re-enqueue by a
// different cause REPLACES origin, and that the manual producer maps to `manual`
// — the value most easily confused with `reconcile`, since both share that call.
func TestUploadsEndpoint_ManualReEnqueueReplacesLiveOrigin(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"))
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	_, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	// The submit itself queued the row: live traffic.
	before := uploadOriginItems(t, srv, qsoUUID)
	if len(before) == 0 {
		t.Fatal("submit queued no upload row")
	}
	if got := before[0]["origin"]; got != "live" {
		t.Fatalf("origin after live logging = %v, want live", got)
	}

	// Re-enqueue the SAME (qso, forwarder, action) through the manual endpoint.
	if w := enqueueUploads(t, srv, "qrz", `{"uuids":["`+qsoUUID+`"],"force":true}`); w.Code != http.StatusOK {
		t.Fatalf("manual enqueue: status = %d, body = %s", w.Code, w.Body.String())
	}

	after := uploadOriginItems(t, srv, qsoUUID)
	if len(after) == 0 {
		t.Fatal("no upload row after manual enqueue")
	}
	if got := after[0]["origin"]; got != "manual" {
		t.Errorf("origin after a manual re-enqueue = %v, want manual — a re-enqueue by a "+
			"different cause must REPLACE the origin, not keep the original", got)
	}
}
