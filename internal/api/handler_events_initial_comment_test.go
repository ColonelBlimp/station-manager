package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Server contract under test: the events stream's first body bytes are a
// ": connected" comment, available immediately after the headers, written after
// Subscribe (so the "open the stream first, then fetch" contract holds the moment
// the client sees it) and before anything else. Why it exists: on the dogfood
// station (2026-09-07, acceptance record entry 29) a stream with no body bytes
// until its first keepalive saw its EventSource open 30 s after the reconnect —
// an observation, with the gating inferred. This test claims only the ordering.
func TestEvents_FirstBodyBytesAreConnectedComment(t *testing.T) {
	h := startE2E(t)
	defer h.shutdown()
	ts := httptest.NewServer(h.srv.httpServer.Handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer resp.Body.Close()

	first, blank := readFirstTwoLines(t, resp.Body, 2*time.Second)
	if first != ": connected" || blank != "" {
		t.Fatalf("first body lines = %q, %q; want \": connected\" then a blank line before any frame", first, blank)
	}
}

// readFirstTwoLines reads the first two lines of the stream within timeout. A
// stream that sends nothing until its first keepalive (the pre-change shape)
// fails here with "no body bytes" rather than hanging the test for 30 s.
func readFirstTwoLines(t *testing.T, r interface{ Read([]byte) (int, error) }, timeout time.Duration) (string, string) {
	t.Helper()
	type two struct{ a, b string }
	got := make(chan two, 1)
	go func() {
		br := bufio.NewReader(r)
		a, err := br.ReadString('\n')
		if err != nil {
			got <- two{a: "<read error: " + err.Error() + ">"}
			return
		}
		b, _ := br.ReadString('\n')
		got <- two{a: trimNL(a), b: trimNL(b)}
	}()
	select {
	case v := <-got:
		return v.a, v.b
	case <-time.After(timeout):
		t.Fatalf("no body bytes within %v of opening the stream", timeout)
		return "", ""
	}
}

func trimNL(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}
