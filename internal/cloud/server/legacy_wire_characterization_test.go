package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// W-0021 5F.1 characterization (tests only, passing before the change): what an
// OLD, name-only client relies on. 5F.1 moves every existing logbook into the
// tenant's legacy archive and must keep all of this unchanged for that client.
//
//	C1  a push by name creates the logbook on first use; the same name reaches
//	    the same logbook; another name makes another.
//	C2  the listing is {id, name}, and reconcile/manifest by that id serve
//	    exactly that logbook's rows.
//	C3  the export lists every logbook with its name and maps each record to
//	    its logbook's id — restore picks a logbook by name over it.
func TestCharacterize_LegacyNameWire(t *testing.T) {
	ts, _, _ := testServer(t)
	at := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	push := func(logbook, uuid string) {
		p, _ := json.Marshal(fixtureQso(uuid))
		putQsos(t, ts, testToken, logbook, []QsoUpload{{ModifiedAt: at, Qso: p}})
	}
	const (
		m1 = "0197f9a0-0000-7000-8000-0000000000c1"
		m2 = "0197f9a0-0000-7000-8000-0000000000c2"
		p1 = "0197f9a0-0000-7000-8000-0000000000c3"
	)
	push("main", m1)
	push("main", m2)
	push("portable", p1)

	var books struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/logbooks", testToken, nil, &books)
	ids := map[string]int64{}
	for _, b := range books.Logbooks {
		ids[b.Name] = b.ID
	}
	if len(books.Logbooks) != 2 || ids["main"] == 0 || ids["portable"] == 0 {
		t.Fatalf("C1: logbooks = %+v; want main and portable", books.Logbooks)
	}

	uuidsOf := func(id int64) []string {
		var man struct {
			LogbookID int64                 `json:"logbook_id"`
			Entries   []store.ManifestEntry `json:"entries"`
		}
		do(t, http.MethodGet, ts.URL+"/v1/logbooks/"+itoa(id)+"/manifest", testToken, nil, &man)
		if man.LogbookID != id {
			t.Fatalf("C2: manifest logbook_id = %d; want %d", man.LogbookID, id)
		}
		var out []string
		for _, e := range man.Entries {
			out = append(out, e.UUID)
		}
		sort.Strings(out)
		return out
	}
	if got := uuidsOf(ids["main"]); len(got) != 2 || got[0] != m1 || got[1] != m2 {
		t.Fatalf("C2: main manifest = %v; want %s, %s", got, m1, m2)
	}
	if got := uuidsOf(ids["portable"]); len(got) != 1 || got[0] != p1 {
		t.Fatalf("C2: portable manifest = %v; want %s", got, p1)
	}
	var rec ReconcileResponse
	do(t, http.MethodGet, ts.URL+"/v1/logbooks/"+itoa(ids["main"])+"/reconcile", testToken, nil, &rec)
	if rec.LogbookID != ids["main"] || rec.Count != 2 {
		t.Fatalf("C2: main reconcile = %+v; want its id and 2 rows", rec)
	}

	var export struct {
		Logbooks []store.LogbookInfo `json:"logbooks"`
		Qsos     []ExportQso         `json:"qsos"`
	}
	do(t, http.MethodGet, ts.URL+"/v1/export", testToken, nil, &export)
	if len(export.Logbooks) != 2 || len(export.Qsos) != 3 {
		t.Fatalf("C3: export = %d logbooks, %d qsos; want 2, 3", len(export.Logbooks), len(export.Qsos))
	}
	want := map[string]int64{m1: ids["main"], m2: ids["main"], p1: ids["portable"]}
	for _, q := range export.Qsos {
		if q.LogbookID != want[q.UUID] {
			t.Fatalf("C3: export maps %s to logbook %d; want %d", q.UUID, q.LogbookID, want[q.UUID])
		}
	}
}
