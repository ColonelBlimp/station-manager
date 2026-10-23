package main

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The http node hands the archive guard the identity of the file this
// generation has OPEN (ADR 0094 update): a request naming that archive passes
// the guard, one naming any other archive is refused before the handler runs.
// An absent QSO is used on purpose: past the guard it answers 404, so the
// two outcomes cannot be confused.
func TestInitHTTP_GuardServesTheOpenFilesArchive(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) { c.SetupComplete = true; c.DefaultLogbookID = 1 })
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopGen(t, orch) })

	identity, err := d.db.ArchiveIdentityWithContext(context.Background())
	if err != nil {
		t.Fatalf("read the open file's identity: %v", err)
	}
	path := "/v1/qso/" + utils.NewUUIDv7()

	code, body := apiCallHeaders(t, d, http.MethodDelete, path, "", map[string]string{"X-SM-Archive-Id": identity.ArchiveUUID})
	if code != http.StatusNotFound {
		t.Fatalf("DELETE naming the open archive: %d %s; want 404 (past the guard)", code, body)
	}
	code, body = apiCallHeaders(t, d, http.MethodDelete, path, "", map[string]string{"X-SM-Archive-Id": utils.NewUUIDv7()})
	if code != http.StatusConflict || !strings.Contains(string(body), "archive_mismatch") {
		t.Fatalf("DELETE naming another archive: %d %s; want 409 archive_mismatch", code, body)
	}
}

// The FT8 stream's identity frame names the same archive: the open file's.
func TestInitHTTP_Ft8StreamNamesTheOpenFilesArchive(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) { c.SetupComplete = true; c.DefaultLogbookID = 1 })
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopGen(t, orch) })
	identity, err := d.db.ArchiveIdentityWithContext(context.Background())
	if err != nil {
		t.Fatalf("read the open file's identity: %v", err)
	}

	srv := httptest.NewServer(d.ft8.HTTPHandler(make(chan struct{})))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("open the FT8 stream: %v", err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for len(lines) < 4 && sc.Scan() {
		lines = append(lines, sc.Text())
	}
	want := `data: {"archive_id":"` + identity.ArchiveUUID + `"}`
	if len(lines) < 4 || lines[2] != "event: identity" || lines[3] != want {
		t.Fatalf("FT8 stream opened with %q; want the identity frame %q", lines, want)
	}
}
