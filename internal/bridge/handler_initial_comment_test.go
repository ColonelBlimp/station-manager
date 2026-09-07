package bridge

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Server contract under test: the rig stream's first body bytes are a
// ": connected" comment, written after Subscribe and before the bootstrap poll,
// whose frames follow in their existing order. The motivating observation is in
// the acceptance record (entry 29, 2026-09-07); this test claims only the ordering.
func TestHTTPHandler_FirstBodyBytesAreConnectedComment(t *testing.T) {
	s, _, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	first := make(chan string, 1)
	go func() {
		br := bufio.NewReader(resp.Body)
		a, err := br.ReadString('\n')
		if err != nil {
			first <- "<read error: " + err.Error() + ">"
			return
		}
		b, _ := br.ReadString('\n')
		first <- strings.TrimRight(a, "\n") + "|" + strings.TrimRight(b, "\n")
	}()
	select {
	case got := <-first:
		if got != ": connected|" {
			t.Fatalf("first body lines = %q, want \": connected\" then a blank line before any frame", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no body bytes within 2s of opening the stream")
	}
}
