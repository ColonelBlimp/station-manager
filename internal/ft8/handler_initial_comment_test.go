package ft8

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Server contract under test: the FT8 stream's first body bytes are a
// ": connected" comment, written after Subscribe and before the event loop. The
// hub's one-slot replay is handed to the subscription, so a decode published
// before the connect must still arrive — as the first FRAME, after the comment.
// The motivating observation is in the acceptance record (entry 29, 2026-09-07);
// this test claims only the ordering.
func TestHTTPHandler_FirstBodyBytesAreConnectedCommentThenReplay(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	s.hub.publish(hubEvent{name: EventDecode, payload: map[string]any{"replayed": true}})
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

	lines := make(chan []string, 1)
	go func() {
		br := bufio.NewReader(resp.Body)
		var out []string
		for len(out) < 3 {
			l, err := br.ReadString('\n')
			if err != nil {
				out = append(out, "<read error: "+err.Error()+">")
				break
			}
			out = append(out, strings.TrimRight(l, "\n"))
		}
		lines <- out
	}()
	select {
	case got := <-lines:
		if len(got) < 3 || got[0] != ": connected" || got[1] != "" {
			t.Fatalf("first body lines = %q, want \": connected\", blank, then the replayed frame", got)
		}
		if !strings.HasPrefix(got[2], "event: "+EventDecode) {
			t.Fatalf("third line = %q, want the replayed %s frame to follow the comment", got[2], EventDecode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no body bytes within 2s of opening the stream")
	}
}
