package smcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0021 Q7 commit 5: provisioning's round-trip check builds each row's upload
// record with the function Submit sends, so "what the worker would push" is
// the forwarder's own code, not a copy of it.
//
//	UR1 UploadFor(q, act) is exactly the record Submit puts on the wire, for
//	    an insert, an update, a tombstone with its deleted_at, and a tombstone
//	    without one (deleted_at falls back to modified_at). Since Submit is
//	    built on UploadFor, the record is pinned by value too: modified_at,
//	    revision, deleted_at, and a payload with its UUID and without the
//	    local identifiers.
//	UR2 UploadFor refuses what Submit refuses: no UUID, no modified_at.

// submitted returns the one record Submit sent for q.
func submitted(t *testing.T, q types.Qso, act forwarding.Action) UploadRecord {
	t.Helper()
	var body []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"received":1,"applied":1}`))
	}))
	defer ts.Close()
	f, err := New(testConfig(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res := f.Submit(context.Background(), q, act, ""); res.Err != nil {
		t.Fatalf("submit: %v", res.Err)
	}
	var req struct {
		Qsos []UploadRecord `json:"qsos"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Qsos) != 1 {
		t.Fatalf("the PUT body %s: %v; want one record", body, err)
	}
	return req.Qsos[0]
}

func sameRecord(a, b UploadRecord) bool {
	if !a.ModifiedAt.Equal(b.ModifiedAt) || a.Revision != b.Revision || (a.DeletedAt == nil) != (b.DeletedAt == nil) {
		return false
	}
	if a.DeletedAt != nil && !a.DeletedAt.Equal(*b.DeletedAt) {
		return false
	}
	var ca, cb bytes.Buffer
	return json.Compact(&ca, a.Qso) == nil && json.Compact(&cb, b.Qso) == nil && bytes.Equal(ca.Bytes(), cb.Bytes())
}

func TestUploadFor_UR1_IsWhatSubmitSends(t *testing.T) {
	live := testQso("0197f9a0-0000-7000-8000-0000000d0001")
	live.ID, live.LogbookID, live.Revision = 41, 7, 3
	gone := testQso("0197f9a0-0000-7000-8000-0000000d0002")
	gone.Revision = 5
	gone.DeletedAt = time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	bare := testQso("0197f9a0-0000-7000-8000-0000000d0003")
	for name, c := range map[string]struct {
		q   types.Qso
		act forwarding.Action
		// deleted is the record's deleted_at: none for an upsert, the row's
		// own for a tombstone, its modified_at when it has none.
		deleted *time.Time
	}{
		"insert":                       {live, action.Insert, nil},
		"update":                       {live, action.Update, nil},
		"tombstone":                    {gone, action.Delete, &gone.DeletedAt},
		"tombstone without deleted_at": {bare, action.Delete, &bare.ModifiedAt},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := UploadFor(c.q, c.act)
			if err != nil {
				t.Fatalf("UploadFor: %v", err)
			}
			if want := submitted(t, c.q, c.act); !sameRecord(got, want) {
				t.Fatalf("UploadFor = %+v (qso %s); Submit sent %+v (qso %s)", got, got.Qso, want, want.Qso)
			}
			// Submit uses UploadFor, so the record is also pinned by value.
			if !got.ModifiedAt.Equal(c.q.ModifiedAt) || got.Revision != c.q.Revision ||
				(got.DeletedAt == nil) != (c.deleted == nil) || (c.deleted != nil && !got.DeletedAt.Equal(*c.deleted)) {
				t.Fatalf("record = modified %s revision %d deleted %v; want %s, %d, %v", got.ModifiedAt, got.Revision, got.DeletedAt, c.q.ModifiedAt, c.q.Revision, c.deleted)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(got.Qso, &payload); err != nil || string(payload["uuid"]) != `"`+c.q.UUID+`"` {
				t.Fatalf("payload %s (%v); want the QSO with its UUID", got.Qso, err)
			}
			for _, local := range []string{"id", "logbook_id", "dedupe_key"} {
				if _, ok := payload[local]; ok {
					t.Fatalf("payload carries the local %q; the projection strips it", local)
				}
			}
		})
	}
}

func TestUploadFor_UR2_RefusesWhatSubmitRefuses(t *testing.T) {
	noUUID := testQso("")
	noTime := testQso("0197f9a0-0000-7000-8000-0000000d0004")
	noTime.ModifiedAt = time.Time{}
	// The control: the same QSO with both is accepted.
	if _, err := UploadFor(testQso("0197f9a0-0000-7000-8000-0000000d0005"), action.Insert); err != nil {
		t.Fatalf("control: UploadFor refused a QSO with a UUID and modified_at: %v", err)
	}
	for name, q := range map[string]types.Qso{"no uuid": noUUID, "no modified_at": noTime} {
		if _, err := UploadFor(q, action.Insert); err == nil {
			t.Fatalf("%s: UploadFor accepted it; Submit refuses it", name)
		}
	}
}
