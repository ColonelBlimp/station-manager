package smcloud

import (
	"context"
	stderr "errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

/*
   W-0021 5F.5 (ADR 0088; rulings K1–K6, 2026-10-13): restore reads one cloud
   logbook through the scoped export, by the target's own archive and logbook
   UUIDs, and nothing it cannot prove complete and correctly addressed.

     SX1  FetchScopedExport asks GET /v1/archives/{a}/logbooks/{l}/export with
          the bearer token, once, and decodes the head and every row: UUID,
          modified_at, revision and a tombstone's deleted_at, payload verbatim.
     SX2  a 404 is ErrScopedLogbookNotFound; any other non-200 is an error
          naming the status that is not ErrScopedLogbookNotFound and never
          carries the token.
     SX3  the answer is refused unless its head names exactly the requested
          archive and logbook: a missing head, another archive, another
          logbook.
     SX4  a document cut off after a valid row, or with no qsos array, is
          refused; a valid empty export is accepted with no rows.
*/

const (
	sxArchive = "0197f9a0-0000-7000-8000-00000000000a"
	sxLogbook = "0197f9a0-0000-7000-8000-00000000000b"
	sxOther   = "0197f9a0-0000-7000-8000-00000000000c"
	sxPath    = "/v1/archives/" + sxArchive + "/logbooks/" + sxLogbook + "/export"
	sxRowLive = `{"uuid":"0197f9a0-0000-7000-8000-000000000101","modified_at":"2026-10-01T10:00:00.5Z","revision":3,"qso":{"call":"DL9UW","band":"20m"}}`
	sxRowGone = `{"uuid":"0197f9a0-0000-7000-8000-000000000102","modified_at":"2026-10-02T11:00:00Z","revision":5,"deleted_at":"2026-10-02T11:00:00Z","qso":{"call":"G4ABC"}}`
)

func sxHead(archive, logbook string) string {
	return `"archive":{"uuid":"` + archive + `","label":"Drill"},"logbook":{"uuid":"` + logbook + `","label":"Drill","callsign":"7Q5MLV"}`
}

// sxServe answers every request with status and body, recording the requests.
func sxServe(t *testing.T, status int, body string) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var reqs []*http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r.Clone(context.Background()))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts, &reqs
}

func TestFetchScopedExport_SX1_TheScopedPathAndEveryRow(t *testing.T) {
	ts, reqs := sxServe(t, http.StatusOK, `{`+sxHead(sxArchive, sxLogbook)+`,"qsos":[`+sxRowLive+`,`+sxRowGone+`]}`+"\n")
	got, err := FetchScopedExport(context.Background(), testConfig(ts.URL), sxArchive, sxLogbook)
	if err != nil {
		t.Fatalf("FetchScopedExport: %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("%d requests; want exactly one", len(*reqs))
	}
	r := (*reqs)[0]
	if r.Method != http.MethodGet || r.URL.Path != sxPath || r.Header.Get("Authorization") != "Bearer tok-123" {
		t.Fatalf("request %s %s (auth %q); want GET %s with the bearer token", r.Method, r.URL.Path, r.Header.Get("Authorization"), sxPath)
	}
	if got.Archive.UUID != sxArchive || got.Logbook.UUID != sxLogbook || got.Logbook.Callsign != "7Q5MLV" {
		t.Fatalf("head = %+v %+v; want the requested archive and logbook", got.Archive, got.Logbook)
	}
	if len(got.Qsos) != 2 {
		t.Fatalf("%d rows; want 2", len(got.Qsos))
	}
	live, gone := got.Qsos[0], got.Qsos[1]
	if live.UUID != "0197f9a0-0000-7000-8000-000000000101" || live.Revision != 3 || live.DeletedAt != nil ||
		!live.ModifiedAt.Equal(time.Date(2026, 10, 1, 10, 0, 0, 500_000_000, time.UTC)) || string(live.Qso) != `{"call":"DL9UW","band":"20m"}` {
		t.Fatalf("live row = %+v (payload %s)", live, live.Qso)
	}
	if gone.Revision != 5 || gone.DeletedAt == nil || !gone.DeletedAt.Equal(time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("tombstone = %+v; want revision 5 and its deleted_at", gone)
	}
}

func TestFetchScopedExport_SX2_NotFoundIsTyped(t *testing.T) {
	ts, _ := sxServe(t, http.StatusNotFound, `{"code":"not_found","message":"no such logbook"}`)
	if _, err := FetchScopedExport(context.Background(), testConfig(ts.URL), sxArchive, sxLogbook); !stderr.Is(err, ErrScopedLogbookNotFound) {
		t.Fatalf("404: err = %v; want ErrScopedLogbookNotFound", err)
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError, http.StatusBadGateway} {
		ts, _ := sxServe(t, status, `{"code":"x","message":"tok-123 echoed"}`)
		_, err := FetchScopedExport(context.Background(), testConfig(ts.URL), sxArchive, sxLogbook)
		if err == nil || stderr.Is(err, ErrScopedLogbookNotFound) {
			t.Fatalf("HTTP %d: err = %v; want an error that is not not-found", status, err)
		}
		if !strings.Contains(err.Error(), http.StatusText(status)) && !strings.Contains(err.Error(), strconv.Itoa(status)) {
			t.Fatalf("HTTP %d: err = %q; want the status named", status, err)
		}
		if strings.Contains(err.Error(), "tok-123") {
			t.Fatalf("HTTP %d: the error carries the token: %q", status, err)
		}
	}
}

func TestFetchScopedExport_SX3_TheHeadMustNameTheRequestedLogbook(t *testing.T) {
	for name, body := range map[string]string{
		"no head":         `{"qsos":[` + sxRowLive + `]}`,
		"another archive": `{` + sxHead(sxOther, sxLogbook) + `,"qsos":[` + sxRowLive + `]}`,
		"another logbook": `{` + sxHead(sxArchive, sxOther) + `,"qsos":[` + sxRowLive + `]}`,
		"no logbook":      `{"archive":{"uuid":"` + sxArchive + `"},"qsos":[` + sxRowLive + `]}`,
	} {
		t.Run(name, func(t *testing.T) {
			ts, _ := sxServe(t, http.StatusOK, body)
			got, err := FetchScopedExport(context.Background(), testConfig(ts.URL), sxArchive, sxLogbook)
			if err == nil {
				t.Fatalf("err = %v (export %+v); want the answer refused", err, got)
			}
			if got != nil {
				t.Fatalf("a refused answer still returned %d row(s)", len(got.Qsos))
			}
		})
	}
}

func TestFetchScopedExport_SX4_OnlyACompleteDocument(t *testing.T) {
	for name, body := range map[string]string{
		"cut off after a valid row": `{` + sxHead(sxArchive, sxLogbook) + `,"qsos":[` + sxRowLive + `,{"uuid":"0197f9a0-00`,
		"cut off after the array":   `{` + sxHead(sxArchive, sxLogbook) + `,"qsos":[` + sxRowLive + `]`,
		"no qsos array":             `{` + sxHead(sxArchive, sxLogbook) + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			ts, _ := sxServe(t, http.StatusOK, body)
			got, err := FetchScopedExport(context.Background(), testConfig(ts.URL), sxArchive, sxLogbook)
			if err == nil || got != nil {
				t.Fatalf("err = %v, export %v; want the incomplete document refused", err, got)
			}
		})
	}
	t.Run("a valid empty export", func(t *testing.T) {
		ts, _ := sxServe(t, http.StatusOK, `{`+sxHead(sxArchive, sxLogbook)+`,"qsos":[]}`+"\n")
		got, err := FetchScopedExport(context.Background(), testConfig(ts.URL), sxArchive, sxLogbook)
		if err != nil || got == nil || len(got.Qsos) != 0 {
			t.Fatalf("empty export: %v, %+v; want accepted with no rows", err, got)
		}
	})
}
