package smcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.4 commit 1: the scoped reconciler (rulings F7, 2026-10-10).

     RI1  against the real cloud server: an identity binding reconciles through
          GET /v1/archives/{a}/logbooks/{l}/reconcile and /manifest only, never
          a name path. A populated legacy-name decoy ("main") and another archive
          with identical labels but different UUIDs are invisible to it. Its
          missing cloud logbook (404) starts the empty-cloud repair: its live
          rows enqueue through its own binding only; a tombstone the cloud never
          saw needs no upload. Drained, it is in sync on its own rows; a later
          divergence goes through the scoped manifest.
     RI2  every failure other than a summary 404 is an error that enqueues
          nothing, never an empty cloud: wrong UUIDs on a successful summary or
          manifest, a server error, an authentication error, a malformed
          answer, a manifest 404 after a summary.
     RI3  an identity summary carries no cloud_logbook_id; a name summary
          still does, 0 included.
     RI4  the name reconciler is unchanged: it resolves by name and never asks
          an identity path.
     RI5  a target without valid UUIDv7s, or an unusable account, is refused.
*/

// pathRecorder proxies to the real cloud and records every request line.
type pathRecorder struct {
	mu    sync.Mutex
	paths []string
}

func (p *pathRecorder) record(r *http.Request) {
	p.mu.Lock()
	p.paths = append(p.paths, r.Method+" "+r.URL.Path)
	p.mu.Unlock()
}

func (p *pathRecorder) reset() {
	p.mu.Lock()
	p.paths = nil
	p.mu.Unlock()
}

func (p *pathRecorder) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.paths)
}

func recordingProxy(t *testing.T, target string) (*httptest.Server, *pathRecorder) {
	t.Helper()
	u, err := url.Parse(target)
	require.NoError(t, err)
	rp := httputil.NewSingleHostReverseProxy(u)
	rec := &pathRecorder{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts, rec
}

// uploadRowsFor returns the forwarder names of every upload row of a QSO,
// tombstones included.
func uploadRowsFor(t *testing.T, dbSvc *sqlite.Service, uuid string) []string {
	t.Helper()
	q, err := dbSvc.FetchQsoByUUIDIncludingDeletedWithContext(context.Background(), uuid)
	require.NoError(t, err)
	rows, err := dbSvc.FetchUploadsByQsoIDWithContext(context.Background(), q.ID)
	require.NoError(t, err)
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.ForwarderName)
	}
	return names
}

const riOtherArchive = "019fd5c5-efcc-7193-be4f-1fee532ee3c3"

func TestReconcileIdentity_RI1_ScopedAgainstRealCloudServer(t *testing.T) {
	cloud := newCloudStack(t)
	proxy, paths := recordingProxy(t, cloud.URL)
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, proxy.URL)
	ctx := context.Background()

	lbA, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	lbDecoy, err := dbSvc.InsertLogbook(types.Logbook{Name: "Decoy", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	lbOther, err := dbSvc.InsertLogbook(types.Logbook{Name: "Other", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	books, err := dbSvc.FetchAllLogbooksWithContext(ctx)
	require.NoError(t, err)
	uuidOf := map[int64]string{}
	for _, b := range books {
		uuidOf[b.ID] = b.UUID
	}

	// The decoy: the legacy cloud logbook "main", populated by name — the name
	// fc's own credentials carry.
	legacy, err := New(fc)
	require.NoError(t, err)
	d1 := importQso(t, qsoSvc, lbDecoy, "DL9UW", "110000")
	drainTo(t, legacy, dbSvc, d1, action.Insert)
	// Another archive whose labels equal A's, under different UUIDs.
	sameLabels := IdentityTarget{ArchiveUUID: riOtherArchive, ArchiveLabel: "Home",
		LogbookUUID: uuidOf[lbOther], LogbookLabel: "Main", Callsign: "7Q5MLV"}
	other, err := NewIdentity(fc, sameLabels)
	require.NoError(t, err)
	o1 := importQso(t, qsoSvc, lbOther, "9A4ZM", "110100")
	o2 := importQso(t, qsoSvc, lbOther, "OK1RR", "110200")
	drainTo(t, other, dbSvc, o1, action.Insert)
	drainTo(t, other, dbSvc, o2, action.Insert)
	creds, _ := json.Marshal(map[string]string{"url": cloud.URL, "token": "tok-e2e"})
	client, err := NewAdoptionClient(types.ForwarderConfig{Type: Type, Credentials: creds})
	require.NoError(t, err)
	decoy, err := client.CloudUUIDs(ctx, "main")
	require.NoError(t, err)
	require.Equal(t, []string{d1}, decoy, "the decoy must be populated for the fixture to discriminate")

	// A: two live rows and a tombstone the cloud never saw.
	a1 := importQso(t, qsoSvc, lbA, "K1AAA", "120000")
	a2 := importQso(t, qsoSvc, lbA, "W1AW", "120100")
	a3 := importQso(t, qsoSvc, lbA, "G4ABC", "120200")
	q3, err := dbSvc.FetchQsoByUUIDWithContext(ctx, a3)
	require.NoError(t, err)
	require.NoError(t, qsoSvc.Delete(ctx, q3, source.Source("test")))
	require.Empty(t, uploadRowsFor(t, dbSvc, a3), "the fixture's tombstone must start with no upload row")
	// Bound only now, so every upload row below is the reconciler's.
	bindLogbook(qsoSvc, lbA, fc)

	targetA := IdentityTarget{ArchiveUUID: ifArchive, ArchiveLabel: "Home",
		LogbookUUID: uuidOf[lbA], LogbookLabel: "Main", Callsign: "7Q5MLV"}
	rec, err := NewIdentityReconciler(fc, lbA, targetA, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)
	scoped := "/v1/archives/" + ifArchive + "/logbooks/" + uuidOf[lbA]

	// 1. A is not on the cloud yet: the summary 404 starts the empty-cloud repair.
	paths.reset()
	sum, err := rec.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	require.False(t, sum.InSync)
	require.Nil(t, sum.CloudLogbookID, "an identity summary has no numeric cloud id")
	require.Equal(t, 2, sum.LocalCount)
	require.Equal(t, 0, sum.CloudCount)
	require.Equal(t, 2, sum.EnqueuedUpserts, "%+v", sum)
	require.Equal(t, 0, sum.EnqueuedDeletes, "a tombstone the cloud never saw needs no upload: %+v", sum)
	require.Equal(t, []string{"GET " + scoped + "/reconcile"}, paths.snapshot())
	for _, u := range []string{a1, a2} {
		require.Equal(t, []string{fc.Name}, uploadRowsFor(t, dbSvc, u), "repair of %s", u)
	}
	for _, u := range []string{a3, d1, o1, o2} {
		require.Empty(t, uploadRowsFor(t, dbSvc, u), "no repair may be queued for %s", u)
	}

	// 2. Drained through A's identity forwarder: in sync on A's rows alone.
	ident, err := NewIdentity(fc, targetA)
	require.NoError(t, err)
	drainTo(t, ident, dbSvc, a1, action.Insert)
	drainTo(t, ident, dbSvc, a2, action.Insert)
	paths.reset()
	sum, err = rec.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	require.True(t, sum.InSync, "%+v", sum)
	require.Equal(t, 2, sum.CloudCount)
	require.Equal(t, []string{"GET " + scoped + "/reconcile"}, paths.snapshot())

	// 3. A divergence goes through the scoped manifest.
	a4 := importQso(t, qsoSvc, lbA, "JA1XYZ", "120300")
	paths.reset()
	sum, err = rec.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	require.False(t, sum.InSync)
	require.Equal(t, 1, sum.EnqueuedUpserts, "%+v", sum)
	require.Equal(t, 0, sum.CloudOnly, "the decoy and the other archive are not A's: %+v", sum)
	require.Equal(t, []string{"GET " + scoped + "/reconcile", "GET " + scoped + "/manifest"}, paths.snapshot())
	require.Equal(t, []string{fc.Name}, uploadRowsFor(t, dbSvc, a4))
	for _, u := range []string{d1, o1, o2} {
		require.Empty(t, uploadRowsFor(t, dbSvc, u), "no repair may be queued for %s", u)
	}
}

// riFake answers the scoped summary and manifest with the given status and body.
type riFake struct {
	summaryStatus  int
	summaryBody    string
	manifestStatus int
	manifestBody   string
}

func riServer(t *testing.T, f *riFake, seen *[]string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		status, body := http.StatusNotFound, `{"error":"not_found"}`
		switch {
		case strings.HasSuffix(r.URL.Path, "/reconcile"):
			status, body = f.summaryStatus, f.summaryBody
		case strings.HasSuffix(r.URL.Path, "/manifest"):
			status, body = f.manifestStatus, f.manifestBody
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// riLocal is a local stack with one bound logbook holding one live QSO, and
// the identity reconciler for it, against url.
func riLocal(t *testing.T, url string) (rec *Reconciler, dbSvc *sqlite.Service, qsoUUID, lbUUID string) {
	t.Helper()
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, url)
	lbID, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	books, err := dbSvc.FetchAllLogbooksWithContext(context.Background())
	require.NoError(t, err)
	lbUUID = books[0].UUID
	bindLogbook(qsoSvc, lbID, fc)
	qsoUUID = importQso(t, qsoSvc, lbID, "K1AAA", "120000")
	rec, err = NewIdentityReconciler(fc, lbID, IdentityTarget{ArchiveUUID: ifArchive, ArchiveLabel: "Home",
		LogbookUUID: lbUUID, LogbookLabel: "Main", Callsign: "7Q5MLV"}, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)
	return rec, dbSvc, qsoUUID, lbUUID
}

func TestReconcileIdentity_RI2_FailuresNeverBecomeAnEmptyCloud(t *testing.T) {
	const wrong = "019fd5c5-efcc-7193-be4f-1fee532eeeee"
	summary := func(archive, logbook string, count int) string {
		return fmt.Sprintf(`{"archive_uuid":%q,"logbook_uuid":%q,"count":%d,"hash":"x"}`, archive, logbook, count)
	}
	manifest := func(archive, logbook string) string {
		return fmt.Sprintf(`{"archive_uuid":%q,"logbook_uuid":%q,"entries":[]}`, archive, logbook)
	}
	cases := []struct {
		name string
		fake func(lb string) riFake
		want string
	}{
		{"summary names another logbook", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: summary(ifArchive, wrong, 0),
				manifestStatus: 200, manifestBody: manifest(ifArchive, lb)}
		}, "summary names another logbook"},
		{"summary names another archive", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: summary(wrong, lb, 0),
				manifestStatus: 200, manifestBody: manifest(ifArchive, lb)}
		}, "summary names another archive"},
		{"summary omits the UUIDs", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: `{"count":0,"hash":"x"}`,
				manifestStatus: 200, manifestBody: manifest(ifArchive, lb)}
		}, "summary names another archive"},
		{"manifest names another logbook", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: summary(ifArchive, lb, 0),
				manifestStatus: 200, manifestBody: manifest(ifArchive, wrong)}
		}, "manifest names another logbook"},
		{"manifest names another archive", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: summary(ifArchive, lb, 0),
				manifestStatus: 200, manifestBody: manifest(wrong, lb)}
		}, "manifest names another archive"},
		{"summary server error", func(lb string) riFake {
			return riFake{summaryStatus: 500, summaryBody: `{"error":"internal_error"}`}
		}, "HTTP 500"},
		{"summary authentication error", func(lb string) riFake {
			return riFake{summaryStatus: 401, summaryBody: `{"error":"unauthorized"}`}
		}, "HTTP 401"},
		{"summary malformed", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: `{"count":`}
		}, "parse"},
		{"manifest 404 after a summary", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: summary(ifArchive, lb, 0),
				manifestStatus: 404, manifestBody: `{"error":"not_found"}`}
		}, "HTTP 404"},
		{"manifest server error", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: summary(ifArchive, lb, 0),
				manifestStatus: 500, manifestBody: `{"error":"internal_error"}`}
		}, "HTTP 500"},
		{"summary omits count", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: fmt.Sprintf(`{"archive_uuid":%q,"logbook_uuid":%q,"hash":"x"}`, ifArchive, lb),
				manifestStatus: 200, manifestBody: manifest(ifArchive, lb)}
		}, "summary omits count"},
		{"summary omits hash", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: fmt.Sprintf(`{"archive_uuid":%q,"logbook_uuid":%q,"count":0}`, ifArchive, lb),
				manifestStatus: 200, manifestBody: manifest(ifArchive, lb)}
		}, "summary omits hash"},
		{"manifest omits entries", func(lb string) riFake {
			return riFake{summaryStatus: 200, summaryBody: summary(ifArchive, lb, 0),
				manifestStatus: 200, manifestBody: fmt.Sprintf(`{"archive_uuid":%q,"logbook_uuid":%q}`, ifArchive, lb)}
		}, "manifest omits entries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen []string
			fake := &riFake{}
			ts := riServer(t, fake, &seen)
			rec, dbSvc, qsoUUID, lbUUID := riLocal(t, ts.URL)
			*fake = tc.fake(lbUUID)
			_, err := rec.RunOnce(context.Background(), TriggerManual)
			require.Empty(t, uploadRowsFor(t, dbSvc, qsoUUID), "a failed run must not enqueue as if the cloud were empty")
			for _, p := range seen {
				require.True(t, strings.HasPrefix(p, "GET /v1/archives/"), "a name path was asked: %s", p)
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// The current server writes "entries": null for a logbook with no rows; that
// is an empty manifest, not an omission.
func TestReconcileIdentity_RI6_NullEntriesIsAnEmptyManifest(t *testing.T) {
	var seen []string
	fake := &riFake{}
	ts := riServer(t, fake, &seen)
	rec, dbSvc, qsoUUID, lbUUID := riLocal(t, ts.URL)
	*fake = riFake{summaryStatus: 200, summaryBody: fmt.Sprintf(`{"archive_uuid":%q,"logbook_uuid":%q,"count":0,"hash":"x"}`, ifArchive, lbUUID),
		manifestStatus: 200, manifestBody: fmt.Sprintf(`{"archive_uuid":%q,"logbook_uuid":%q,"entries":null}`, ifArchive, lbUUID)}
	sum, err := rec.RunOnce(context.Background(), TriggerManual)
	require.NoError(t, err)
	require.Equal(t, 1, sum.EnqueuedUpserts, "the live row the cloud lacks queues for upload")
	require.Len(t, uploadRowsFor(t, dbSvc, qsoUUID), 1)
	require.Contains(t, seen, "GET /v1/archives/"+ifArchive+"/logbooks/"+lbUUID+"/manifest")
}

func TestReconcileIdentity_RI3_NoNumericCloudIDOnIdentitySummaries(t *testing.T) {
	var seen []string
	ts := riServer(t, &riFake{summaryStatus: 404, summaryBody: `{"error":"not_found"}`}, &seen)
	rec, _, _, _ := riLocal(t, ts.URL)
	sum, err := rec.RunOnce(context.Background(), TriggerManual)
	require.NoError(t, err)
	raw, err := json.Marshal(sum)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "cloud_logbook_id", "identity summary: %s", raw)

	zero := int64(0)
	raw, err = json.Marshal(ReconcileSummary{CloudLogbookID: &zero})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"cloud_logbook_id":0`, "a name summary keeps its 0 = not created yet")
}

func TestReconcileIdentity_RI4_NameReconcilerUnchanged(t *testing.T) {
	var seen []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v1/logbooks":
			_, _ = io.WriteString(w, `{"logbooks":[{"id":7,"name":"main"}]}`)
		case "/v1/logbooks/7/reconcile":
			_, _ = io.WriteString(w, `{"count":0,"hash":"x"}`)
		case "/v1/logbooks/7/manifest":
			_, _ = io.WriteString(w, `{"entries":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, ts.URL)
	lbID, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	bindLogbook(qsoSvc, lbID, fc)
	u := importQso(t, qsoSvc, lbID, "K1AAA", "120000")
	rec, err := NewReconciler(fc, lbID, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)
	sum, err := rec.RunOnce(context.Background(), TriggerManual)
	require.NoError(t, err)
	require.NotNil(t, sum.CloudLogbookID)
	require.EqualValues(t, 7, *sum.CloudLogbookID)
	require.Equal(t, 1, sum.EnqueuedUpserts)
	require.Equal(t, []string{fc.Name}, uploadRowsFor(t, dbSvc, u))
	require.Equal(t, []string{"GET /v1/logbooks", "GET /v1/logbooks/7/reconcile", "GET /v1/logbooks/7/manifest"}, seen)
}

func TestReconcileIdentity_RI5_TargetAndAccountChecked(t *testing.T) {
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, "https://cloud.example")
	lbID, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	for name, target := range map[string]IdentityTarget{
		"no archive":      {LogbookUUID: ifLogbook},
		"no logbook":      {ArchiveUUID: ifArchive},
		"archive not v7":  {ArchiveUUID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8", LogbookUUID: ifLogbook},
		"logbook garbage": {ArchiveUUID: ifArchive, LogbookUUID: "main"},
	} {
		_, err := NewIdentityReconciler(fc, lbID, target, dbSvc, qsoSvc, logSvc)
		require.Error(t, err, name)
	}
	bad := fc
	bad.Credentials = json.RawMessage(`{"url":"https://cloud.example"}`)
	_, err = NewIdentityReconciler(bad, lbID, ifTarget, dbSvc, qsoSvc, logSvc)
	require.Error(t, err, "an account without a token")
}
