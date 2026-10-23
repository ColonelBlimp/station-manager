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

// Stream identity (ADR 0094 update; W-0021 "alpha.4 hold: fix design",
// revision 3): every FT8 stream connection names the archive its daemon has
// open, as its first FRAME — right after ": connected" and BEFORE the hub's
// replay. A connection cannot outlive its process, so every later frame on it
// belongs to that archive; a client attributes ft8-logged and ft8-qso frames
// (Session rows, engagement) by it. A replayed ft8-qso arriving before the
// identity would be unattributable.

const identityTestArchive = "01a1640f-815a-75e4-bae2-b9ef8d86f8c3"

// readStreamLines opens one connection and returns its first n lines — or, if
// the stream stalls, the lines that did arrive, so a failure shows what was sent.
func readStreamLines(t *testing.T, srv *httptest.Server, n int) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	lines := make(chan string, n)
	go func() {
		br := bufio.NewReader(resp.Body)
		for range n {
			l, err := br.ReadString('\n')
			if err != nil {
				return
			}
			lines <- strings.TrimRight(l, "\n")
		}
	}()
	var out []string
	timeout := time.After(time.Second)
	for len(out) < n {
		select {
		case l := <-lines:
			out = append(out, l)
		case <-timeout:
			return append(out, "<stalled>")
		}
	}
	return out
}

func TestHTTPHandler_IdentityFramePrecedesTheReplay(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	s.SetServedArchive(identityTestArchive)
	// A cached ft8-qso: the archive-bound frame a late subscriber is replayed.
	s.hub.publish(hubEvent{name: EventQso, payload: map[string]any{"active": true, "their_call": "K1ABC"}})
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	got := readStreamLines(t, srv, 6)
	want := []string{
		": connected", "",
		"event: " + EventIdentity, `data: {"archive_id":"` + identityTestArchive + `"}`, "",
		"event: " + EventQso,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("first stream lines:\n%q\nwant the identity frame between the comment and the replay:\n%q", got, want)
	}
}

// Every connection carries its own identity frame — a reconnect is a new
// connection that must be attributed afresh, never by an earlier one.
func TestHTTPHandler_EveryConnectionNamesItsArchive(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	s.SetServedArchive(identityTestArchive)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	for i := range 2 {
		got := readStreamLines(t, srv, 4)
		if len(got) < 4 || got[2] != "event: "+EventIdentity || got[3] != `data: {"archive_id":"`+identityTestArchive+`"}` {
			t.Fatalf("connection %d: first lines %q, want the identity frame after the comment", i+1, got)
		}
	}
}

// A daemon that never recorded its archive still sends the frame in its fixed
// place, with an empty archive_id: the client treats that as no identity, so
// nothing on the connection is attributed.
func TestHTTPHandler_UnsetArchiveSendsAnEmptyIdentity(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	got := readStreamLines(t, srv, 4)
	if len(got) < 4 || got[2] != "event: "+EventIdentity || got[3] != `data: {"archive_id":""}` {
		t.Fatalf("first lines %q, want an identity frame with an empty archive_id", got)
	}
}
