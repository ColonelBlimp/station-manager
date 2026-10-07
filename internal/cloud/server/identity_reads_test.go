package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/reconcile"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// W-0021 5F.2: the scoped reads (ADR 0088; operator correction 2026-10-07:
// reconcile and manifest carry both UUIDs).
//
//	IR1  reconcile and manifest of one logbook, by UUIDs, carrying both UUIDs.
//	IR2  404 for a logbook of another archive, an unknown UUID, another
//	     tenant's archive, or a malformed UUID.
//	IR3  the scoped export: the archive and logbook display values and only
//	     that logbook's QSOs, tombstones included.
//	IR4  AC 6, server side: two archives with the same label and the same
//	     logbook name never see each other's rows.

type scopedManifest struct {
	ArchiveUUID string                `json:"archive_uuid"`
	LogbookUUID string                `json:"logbook_uuid"`
	Entries     []store.ManifestEntry `json:"entries"`
}

type scopedReconcile struct {
	ArchiveUUID string `json:"archive_uuid"`
	LogbookUUID string `json:"logbook_uuid"`
	Count       int    `json:"count"`
	Hash        string `json:"hash"`
}

type scopedExport struct {
	Archive struct {
		UUID  string `json:"uuid"`
		Label string `json:"label"`
	} `json:"archive"`
	Logbook struct {
		UUID     string `json:"uuid"`
		Label    string `json:"label"`
		Callsign string `json:"callsign"`
	} `json:"logbook"`
	Qsos []struct {
		UUID      string          `json:"uuid"`
		Revision  int64           `json:"revision"`
		DeletedAt *time.Time      `json:"deleted_at"`
		Qso       json.RawMessage `json:"qso"`
	} `json:"qsos"`
}

func scoped(ts *httptest.Server, archive, logbook, what string) string {
	return ts.URL + "/v1/archives/" + archive + "/logbooks/" + logbook + "/" + what
}

func manifestUUIDs(m scopedManifest) []string {
	var out []string
	for _, e := range m.Entries {
		out = append(out, e.UUID)
	}
	sort.Strings(out)
	return out
}

func TestIdentityReads_IR1_ReconcileAndManifestCarryBothUUIDs(t *testing.T) {
	ts, _, _, _ := newTestServer(t, quietLog)
	const q1, q2 = "0197f9a0-0000-7000-8000-000000000201", "0197f9a0-0000-7000-8000-000000000202"
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{Qsos: append(oneQso(t, q1, "DL9UW", 1, pushAt), oneQso(t, q2, "EA1B", 2, pushAt)...)})
	pushIdentity(t, ts, contestUUID, contestLB2, identityPut{Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000203", "OH2BH", 1, pushAt)})

	var m scopedManifest
	resp := do(t, http.MethodGet, scoped(ts, contestUUID, contestLB, "manifest"), testToken, nil, &m)
	if resp.StatusCode != http.StatusOK || m.ArchiveUUID != contestUUID || m.LogbookUUID != contestLB {
		t.Fatalf("manifest: %d %+v", resp.StatusCode, m)
	}
	if got := manifestUUIDs(m); len(got) != 2 || got[0] != q1 || got[1] != q2 {
		t.Fatalf("manifest entries = %v; want %s, %s only", got, q1, q2)
	}
	var rec scopedReconcile
	do(t, http.MethodGet, scoped(ts, contestUUID, contestLB, "reconcile"), testToken, nil, &rec)
	count, hash := reconcile.Summary([]reconcile.Entry{
		{UUID: q1, ModifiedAt: pushAt, Revision: 1}, {UUID: q2, ModifiedAt: pushAt, Revision: 2},
	})
	if rec.ArchiveUUID != contestUUID || rec.LogbookUUID != contestLB || rec.Count != count || rec.Hash != hash {
		t.Fatalf("reconcile = %+v; want both uuids, count %d, hash %s", rec, count, hash)
	}
}

func TestIdentityReads_IR2_ScopedReadsAre404OutsideTheirArchive(t *testing.T) {
	ts, _, _, _ := newTestServer(t, quietLog)
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000204", "DL9UW", 1, pushAt)})
	pushIdentity(t, ts, otherUUID, contestLB2, identityPut{Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000205", "EA1B", 1, pushAt)})
	for _, c := range []struct{ name, archive, logbook, token string }{
		{"logbook of another archive", otherUUID, contestLB, testToken},
		{"unknown logbook", contestUUID, "01920000-0000-7000-8000-000000000dff", testToken},
		{"unknown archive", "01920000-0000-7000-8000-000000000cff", contestLB, testToken},
		{"another tenant", contestUUID, contestLB, otherToken},
		{"malformed uuid", "nope", contestLB, testToken},
	} {
		for _, what := range []string{"reconcile", "manifest", "export"} {
			resp := do(t, http.MethodGet, scoped(ts, c.archive, c.logbook, what), c.token, nil, nil)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s = %d; want 404", c.name, what, resp.StatusCode)
			}
		}
	}
}

func TestIdentityReads_IR3_TheScopedExport(t *testing.T) {
	ts, _, _, _ := newTestServer(t, quietLog)
	const live, gone = "0197f9a0-0000-7000-8000-000000000206", "0197f9a0-0000-7000-8000-000000000207"
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{ArchiveLabel: "Contest", LogbookLabel: "main", Callsign: "7Q5MLV/P",
		Qsos: append(oneQso(t, live, "DL9UW", 1, pushAt), oneQso(t, gone, "EA1B", 1, pushAt)...)})
	del := pushAt.Add(time.Hour)
	tomb := oneQso(t, gone, "EA1B", 2, del)
	tomb[0].DeletedAt = &del
	pushIdentity(t, ts, contestUUID, contestLB, identityPut{Qsos: tomb})
	pushIdentity(t, ts, contestUUID, contestLB2, identityPut{Qsos: oneQso(t, "0197f9a0-0000-7000-8000-000000000208", "OH2BH", 1, pushAt)})

	var e scopedExport
	resp := do(t, http.MethodGet, scoped(ts, contestUUID, contestLB, "export"), testToken, nil, &e)
	if resp.StatusCode != http.StatusOK || e.Archive.UUID != contestUUID || e.Archive.Label != "Contest" ||
		e.Logbook.UUID != contestLB || e.Logbook.Label != "main" || e.Logbook.Callsign != "7Q5MLV/P" {
		t.Fatalf("export head: %d %+v %+v", resp.StatusCode, e.Archive, e.Logbook)
	}
	got := map[string]bool{}
	for _, q := range e.Qsos {
		got[q.UUID] = q.DeletedAt != nil
	}
	if len(got) != 2 || got[live] || !got[gone] {
		t.Fatalf("export rows = %v; want %s live and %s tombstoned, nothing else", got, live, gone)
	}
}

func TestIdentityReads_IR4_TwoArchivesWithEqualNamesStayApart(t *testing.T) {
	ts, _, _, _ := newTestServer(t, quietLog)
	const a1, a2 = "0197f9a0-0000-7000-8000-000000000209", "0197f9a0-0000-7000-8000-00000000020a"
	for _, c := range []struct{ archive, logbook, qso string }{{contestUUID, contestLB, a1}, {otherUUID, contestLB2, a2}} {
		status, out := pushIdentity(t, ts, c.archive, c.logbook, identityPut{ArchiveLabel: "Contest", LogbookLabel: "main",
			Qsos: oneQso(t, c.qso, "DL9UW", 1, pushAt)})
		if status != http.StatusOK {
			t.Fatalf("push into %s: %d %v", c.archive, status, out)
		}
	}
	for _, c := range []struct{ archive, logbook, want string }{{contestUUID, contestLB, a1}, {otherUUID, contestLB2, a2}} {
		var m scopedManifest
		do(t, http.MethodGet, scoped(ts, c.archive, c.logbook, "manifest"), testToken, nil, &m)
		if got := manifestUUIDs(m); len(got) != 1 || got[0] != c.want {
			t.Fatalf("archive %s manifest = %v; want only %s", c.archive, got, c.want)
		}
		var rec scopedReconcile
		do(t, http.MethodGet, scoped(ts, c.archive, c.logbook, "reconcile"), testToken, nil, &rec)
		if rec.Count != 1 {
			t.Fatalf("archive %s reconcile count = %d; want 1", c.archive, rec.Count)
		}
		var e scopedExport
		do(t, http.MethodGet, scoped(ts, c.archive, c.logbook, "export"), testToken, nil, &e)
		if len(e.Qsos) != 1 || e.Qsos[0].UUID != c.want {
			t.Fatalf("archive %s export = %+v; want only %s", c.archive, e.Qsos, c.want)
		}
	}
}
