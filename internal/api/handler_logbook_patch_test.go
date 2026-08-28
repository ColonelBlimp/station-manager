package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// blockingBody gates a request body: the first Read signals `reached` (the handler
// has already fetched the row and is now reading the body) and then blocks until
// `release`. That reproduces the exact fetch→write interleave PT-4 is about, using
// only the request the client controls — no production test hook, no synchronization
// state on Server.
type blockingBody struct {
	reached chan struct{}
	release <-chan struct{}
	once    sync.Once
	r       *strings.Reader
}

func (b *blockingBody) Read(p []byte) (int, error) {
	b.once.Do(func() {
		close(b.reached)
		<-b.release
	})
	return b.r.Read(p)
}

func (b *blockingBody) Close() error { return nil }

func patchLogbook(t *testing.T, srv *Server, id int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/logbook/%d", id), strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleUpdateLogbook(w, req)
	return w
}

func deleteLogbook(t *testing.T, srv *Server, id int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/logbook/%d", id), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", id))
	w := httptest.NewRecorder()
	srv.handleDeleteLogbook(w, req)
	return w
}

// blockedPatch starts a logbook PATCH whose body read blocks, and returns its
// recorder, the release channel, and a done channel. On return the handler has
// fetched the target row (its stale snapshot) and is parked at the body read, so a
// second request can commit before this one resumes — the fetch→write interleave,
// driven entirely from the client side.
func blockedPatch(srv *Server, id int64, body string) (
	w *httptest.ResponseRecorder, release chan struct{}, done chan struct{},
) {
	release = make(chan struct{})
	bb := &blockingBody{reached: make(chan struct{}), release: release, r: strings.NewReader(body)}
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/logbook/%d", id), bb)
	req.SetPathValue("id", fmt.Sprintf("%d", id))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	done = make(chan struct{})
	go func() {
		defer close(done)
		srv.handleUpdateLogbook(w, req)
	}()
	<-bb.reached // the handler has fetched the row and is now blocked at the body read
	return w, release, done
}

func decodeLogbook(t *testing.T, w *httptest.ResponseRecorder) types.Logbook {
	t.Helper()
	var lb types.Logbook
	if err := unmarshalJSON(w.Body.String(), &lb); err != nil {
		t.Fatalf("decode logbook body %q: %v", w.Body.String(), err)
	}
	return lb
}

// TestUpdateLogbook_OverlappingPartialPatchesBothSurvive (PT-4, W-0008). Two
// overlapping partial PATCHes to DISJOINT fields must both survive. Before PT-4 the
// handler fetched the whole row, set the present fields on that copy, and wrote the
// WHOLE copy — so a description-only PATCH holding a pre-rename snapshot wrote back the
// stale name, silently reverting a concurrent rename while returning 200. This blocks
// req2's body read after it has fetched its snapshot, runs req1's rename to completion,
// then releases req2 and asserts both changes stand and each response reflects what it
// committed.
func TestUpdateLogbook_OverlappingPartialPatchesBothSurvive(t *testing.T) {
	srv := testServer(t)
	id := createTestLogbook(t, srv, "Alpha", "G4ABC")

	// req2 fetches {Alpha, ""} then parks at the body read.
	w2, release, done2 := blockedPatch(srv, id, `{"description":"updated"}`)

	// req1 renames to completion while req2 holds its pre-rename snapshot.
	w1 := patchLogbook(t, srv, id, `{"name":"Bravo"}`)
	if w1.Code != http.StatusOK {
		t.Fatalf("req1 rename: status = %d; body = %s", w1.Code, w1.Body.String())
	}
	// req1's response reflects its own commit: the rename, description still empty.
	got1 := decodeLogbook(t, w1)
	if got1.Name != "Bravo" {
		t.Errorf("req1 response name = %q, want Bravo", got1.Name)
	}
	if got1.Callsign != "G4ABC" {
		t.Errorf("req1 response callsign = %q, want unchanged G4ABC", got1.Callsign)
	}

	close(release) // req2 resumes and commits its description change
	<-done2

	if w2.Code != http.StatusOK {
		t.Fatalf("req2 status = %d, want 200; body = %s", w2.Code, w2.Body.String())
	}
	// req2's response reflects what it COMMITTED, not its stale snapshot: it changed
	// only the description, so the current name (Bravo) stands.
	got2 := decodeLogbook(t, w2)
	if got2.Name != "Bravo" || got2.Description != "updated" {
		t.Errorf("req2 response = {name:%q, desc:%q}, want {Bravo, updated} — a stale full-row write restores the old name",
			got2.Name, got2.Description)
	}

	// The committed row carries BOTH changes; callsign is untouched.
	final, err := srv.db.FetchLogbookByIDWithContext(context.Background(), id)
	if err != nil {
		t.Fatalf("fetch final: %v", err)
	}
	if final.Name != "Bravo" {
		t.Errorf("final name = %q, want Bravo — req1's rename was silently lost", final.Name)
	}
	if final.Description != "updated" {
		t.Errorf("final description = %q, want updated", final.Description)
	}
	if final.Callsign != "G4ABC" {
		t.Errorf("final callsign = %q, want unchanged G4ABC", final.Callsign)
	}
}

// Two logbooks racing for one name, INTERLEAVED: req2 fetches its snapshot and parks,
// req1 takes the name and commits, then req2 resumes — the UNIQUE(name) constraint
// makes it the loser with 409 duplicate_name.
func TestUpdateLogbook_ConcurrentRenames_LoserGets409(t *testing.T) {
	srv := testServer(t)
	l1 := createTestLogbook(t, srv, "Alpha", "G4ABC")
	l2 := createTestLogbook(t, srv, "Bravo", "G4ABC")

	// req2 (rename L2 → Zulu) fetches, then parks at the body read.
	w2, release, done2 := blockedPatch(srv, l2, `{"name":"Zulu"}`)

	// req1 (rename L1 → Zulu) commits first and takes the name.
	if w1 := patchLogbook(t, srv, l1, `{"name":"Zulu"}`); w1.Code != http.StatusOK {
		t.Fatalf("winner rename: status = %d; body = %s", w1.Code, w1.Body.String())
	}

	close(release) // req2 resumes; the name is already taken
	<-done2
	if w2.Code != http.StatusConflict {
		t.Fatalf("loser rename: status = %d, want 409; body = %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "duplicate_name") {
		t.Errorf("body = %q, want duplicate_name", w2.Body.String())
	}
}

// A PATCH whose target is soft-deleted between the handler's fetch and its write must
// return 404 not_found (the active-row predicate matches zero rows).
func TestUpdateLogbook_PatchVersusConcurrentDelete_404(t *testing.T) {
	srv := testServer(t)
	// The first logbook is the configured default (undeletable); the target of the
	// patch-vs-delete race must be a non-default one.
	_ = createTestLogbook(t, srv, "Keeper", "G4ABC")
	id := createTestLogbook(t, srv, "Alpha", "G4ABC")

	w, release, done := blockedPatch(srv, id, `{"description":"updated"}`)

	if dw := deleteLogbook(t, srv, id); dw.Code != http.StatusNoContent {
		t.Fatalf("concurrent delete: status = %d; body = %s", dw.Code, dw.Body.String())
	}
	close(release)
	<-done

	if w.Code != http.StatusNotFound {
		t.Fatalf("patch-of-deleted status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not_found") {
		t.Errorf("body = %q, want not_found", w.Body.String())
	}
}

// Same-field edits, INTERLEAVED: both requests read the same snapshot, req1 commits
// first, then req2 — the LAST committed writer wins that field (this is correct
// last-writer-wins, not a lost update, because both wrote the same field).
func TestUpdateLogbook_SameFieldLastWriterWins(t *testing.T) {
	srv := testServer(t)
	id := createTestLogbook(t, srv, "Alpha", "G4ABC")

	// req2 (name → Charlie) fetches {Alpha}, then parks at the body read.
	w2, release, done2 := blockedPatch(srv, id, `{"name":"Charlie"}`)

	// req1 (name → Bravo) commits first.
	if w1 := patchLogbook(t, srv, id, `{"name":"Bravo"}`); w1.Code != http.StatusOK {
		t.Fatalf("req1: status = %d; body = %s", w1.Code, w1.Body.String())
	}

	// req2 commits second — it is the last committed writer.
	close(release)
	<-done2
	if w2.Code != http.StatusOK {
		t.Fatalf("req2: status = %d, want 200; body = %s", w2.Code, w2.Body.String())
	}
	if got2 := decodeLogbook(t, w2); got2.Name != "Charlie" {
		t.Errorf("req2 response name = %q, want Charlie", got2.Name)
	}
	final, err := srv.db.FetchLogbookByIDWithContext(context.Background(), id)
	if err != nil {
		t.Fatalf("fetch final: %v", err)
	}
	if final.Name != "Charlie" {
		t.Errorf("final name = %q, want Charlie (last committed writer)", final.Name)
	}
}
