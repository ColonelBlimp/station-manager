package smcloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 5a: the identity forwarder (ADR 0088; ADR 0090 T1, T5).

     IF1  an adopted binding PUTs to /v1/archives/{archive}/logbooks/{logbook}
          /qsos with exactly archive_label, logbook_label, callsign and qsos:
          never the cloud logbook name, never the name-only path; a delete is
          the same record with deleted_at.
     IF2  the UUIDs are sent in lower case; a target without a valid archive or
          logbook UUIDv7, or an unusable account, is refused.
     IF3  a 404 keeps the upload pending as "endpoint unavailable" (T5).
     IF4  against the real cloud server: after adoption, uploads by UUID land
          in the adopted legacy logbook, which the by-name reconciler still
          finds and judges in sync; its repairs, drained through the identity
          forwarder, land there too, after a display-label change as well.
*/

const (
	ifArchive = "019fd5c5-efcc-7193-be4f-1fee532ee3a1"
	ifLogbook = "019fd5c5-efcc-7193-be4f-1fee532ee3b2"
)

var ifTarget = IdentityTarget{
	ArchiveUUID: ifArchive, ArchiveLabel: "Home",
	LogbookUUID: ifLogbook, LogbookLabel: "Main", Callsign: "M0ABC",
}

func ifConfig(url string) types.ForwarderConfig {
	creds, _ := json.Marshal(map[string]string{"url": url, "token": "tok-123", "logbook": "shack"})
	return types.ForwarderConfig{Name: "smcloud.u1", Type: Type, Credentials: creds}
}

type seenPut struct {
	method, path, auth string
	body               map[string]json.RawMessage
}

func ifServer(t *testing.T, status int, seen *[]seenPut) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(raw, &body)
		*seen = append(*seen, seenPut{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = io.WriteString(w, `{"received":1,"applied":1}`)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestIdentityForwarder_IF1_PathAndEnvelope(t *testing.T) {
	var seen []seenPut
	ts := ifServer(t, http.StatusOK, &seen)
	f, err := NewIdentity(ifConfig(ts.URL), ifTarget)
	if err != nil {
		t.Fatal(err)
	}
	q := testQso("0197f9a0-0000-7000-8000-000000000003")
	if res := f.Submit(context.Background(), q, action.Insert, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("insert: %+v", res)
	}
	q.DeletedAt = q.ModifiedAt.Add(time.Minute)
	if res := f.Submit(context.Background(), q, action.Delete, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("delete: %+v", res)
	}
	want := "/v1/archives/" + ifArchive + "/logbooks/" + ifLogbook + "/qsos"
	if len(seen) != 2 {
		t.Fatalf("requests = %d; want 2", len(seen))
	}
	for i, s := range seen {
		if s.method != http.MethodPut || s.path != want || s.auth != "Bearer tok-123" {
			t.Fatalf("request %d: %s %s (%q); want PUT %s with the token", i, s.method, s.path, s.auth, want)
		}
		keys := make([]string, 0, len(s.body))
		for k := range s.body {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, []string{"archive_label", "callsign", "logbook_label", "qsos"}) {
			t.Fatalf("request %d body keys = %v; want exactly the identity envelope", i, keys)
		}
		for k, v := range map[string]string{"archive_label": `"Home"`, "logbook_label": `"Main"`, "callsign": `"M0ABC"`} {
			if string(s.body[k]) != v {
				t.Fatalf("request %d %s = %s; want %s", i, k, s.body[k], v)
			}
		}
	}
	var qsos []map[string]json.RawMessage
	if err := json.Unmarshal(seen[1].body["qsos"], &qsos); err != nil || len(qsos) != 1 || qsos[0]["deleted_at"] == nil {
		t.Fatalf("the delete is not a tombstone: %s (%v)", seen[1].body["qsos"], err)
	}
	if strings.Contains(string(seen[0].body["qsos"]), `"logbook":"shack"`) {
		t.Fatal("the cloud logbook name rode the identity wire")
	}
}

func TestIdentityForwarder_IF2_TargetChecked(t *testing.T) {
	var seen []seenPut
	ts := ifServer(t, http.StatusOK, &seen)
	upper := ifTarget
	upper.ArchiveUUID, upper.LogbookUUID = strings.ToUpper(ifArchive), strings.ToUpper(ifLogbook)
	f, err := NewIdentity(ifConfig(ts.URL), upper)
	if err != nil {
		t.Fatal(err)
	}
	f.Submit(context.Background(), testQso("0197f9a0-0000-7000-8000-000000000003"), action.Insert, "")
	if want := "/v1/archives/" + ifArchive + "/logbooks/" + ifLogbook + "/qsos"; len(seen) != 1 || seen[0].path != want {
		t.Fatalf("path = %v; want %s", seen, want)
	}
	for name, mutate := range map[string]func(*IdentityTarget, *types.ForwarderConfig){
		"no archive":       func(tg *IdentityTarget, _ *types.ForwarderConfig) { tg.ArchiveUUID = "" },
		"archive not a v7": func(tg *IdentityTarget, _ *types.ForwarderConfig) { tg.ArchiveUUID = "not-a-uuid" },
		"no logbook":       func(tg *IdentityTarget, _ *types.ForwarderConfig) { tg.LogbookUUID = "" },
		"logbook not a v7": func(tg *IdentityTarget, _ *types.ForwarderConfig) {
			tg.LogbookUUID = "0197f9a0-0000-4000-8000-000000000003"
		},
		"an unusable account": func(_ *IdentityTarget, fc *types.ForwarderConfig) { fc.Credentials = json.RawMessage(`{"token":"t"}`) },
	} {
		t.Run(name, func(t *testing.T) {
			tg, fc := ifTarget, ifConfig(ts.URL)
			mutate(&tg, &fc)
			if _, err := NewIdentity(fc, tg); err == nil {
				t.Fatal("built an identity forwarder from a bad target or account")
			}
		})
	}
}

func TestIdentityForwarder_IF3_404KeepsThePendingUpload(t *testing.T) {
	var seen []seenPut
	ts := ifServer(t, http.StatusNotFound, &seen)
	f, err := NewIdentity(ifConfig(ts.URL), ifTarget)
	if err != nil {
		t.Fatal(err)
	}
	res := f.Submit(context.Background(), testQso("0197f9a0-0000-7000-8000-000000000003"), action.Insert, "")
	if res.Outcome != forwarding.OutcomeEndpointUnavailable || len(seen) != 1 {
		t.Fatalf("outcome = %q after %d requests; want endpoint_unavailable after 1", res.Outcome, len(seen))
	}
}

func TestIdentityForwarder_IF4_AgainstRealCloudServer(t *testing.T) {
	cloud := newCloudStack(t)
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, cloud.URL)
	ctx := context.Background()
	lbID, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	books, err := dbSvc.FetchAllLogbooksWithContext(ctx)
	require.NoError(t, err)
	lbUUID := books[0].UUID
	bindLogbook(qsoSvc, lbID, fc)

	// Before adoption the legacy wire filled the cloud logbook "main".
	legacy, err := New(fc)
	require.NoError(t, err)
	u1 := importQso(t, qsoSvc, lbID, "DL9UW", "120000")
	drainTo(t, legacy, dbSvc, u1, action.Insert)

	// Adopt "main" for Home and this logbook.
	creds, _ := json.Marshal(map[string]string{"url": cloud.URL, "token": "tok-e2e"})
	client, err := NewAdoptionClient(types.ForwarderConfig{Type: Type, Credentials: creds})
	require.NoError(t, err)
	require.NoError(t, client.Adopt(ctx, AdoptRequest{LegacyName: "main", ArchiveUUID: ifArchive, ArchiveLabel: "Home",
		LogbookUUID: lbUUID, LogbookName: "Main", Callsign: "7Q5MLV"}))

	// After the restart: the identity forwarder, and the by-name reconciler.
	target := IdentityTarget{ArchiveUUID: ifArchive, ArchiveLabel: "Home", LogbookUUID: lbUUID, LogbookLabel: "Main", Callsign: "7Q5MLV"}
	ident, err := NewIdentity(fc, target)
	require.NoError(t, err)
	rec, err := NewReconciler(fc, lbID, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)

	u2 := importQso(t, qsoSvc, lbID, "9A4ZM", "120100")
	drainTo(t, ident, dbSvc, u2, action.Insert)
	got, err := client.CloudUUIDs(ctx, "main")
	require.NoError(t, err)
	slices.Sort(got)
	want := []string{u1, u2}
	slices.Sort(want)
	require.Equal(t, want, got, "the identity upload did not land in the adopted legacy logbook")
	sum, err := rec.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	require.True(t, sum.InSync, "the by-name reconciler after an identity upload: %+v", sum)

	// A local QSO the cloud lacks, after the logbook's display label changed:
	// the reconciler queues the repair, and the identity forwarder sends it.
	renamed := target
	renamed.LogbookLabel = "Main renamed"
	ident, err = NewIdentity(fc, renamed)
	require.NoError(t, err)
	u3 := importQso(t, qsoSvc, lbID, "K1AAA", "120200")
	sum, err = rec.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	require.Equal(t, 1, sum.EnqueuedUpserts, "%+v", sum)
	q3, err := dbSvc.FetchQsoByUUIDWithContext(ctx, u3)
	require.NoError(t, err)
	rows, err := dbSvc.FetchUploadsByQsoIDWithContext(ctx, q3.ID)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, fc.Name, rows[0].ForwarderName, "the repair did not enter the binding's queue")
	drainTo(t, ident, dbSvc, u3, action.Insert)
	sum, err = rec.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	require.True(t, sum.InSync, "after the repair through the identity forwarder: %+v", sum)
}
