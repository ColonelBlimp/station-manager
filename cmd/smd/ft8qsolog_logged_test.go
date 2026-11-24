package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

/*
   The completed-contact sink publishes ONE ft8-logged frame for each contact it
   stores, naming the row it stored (invariant 1's observable: "one QSO row and
   one ft8-logged event"). The sink's other tests read the row and its upload
   row; none read the frame, so a sink that stored and published nothing, or
   twice, passed them all (codex-245, reviewing W-0030).

   It is read as the app reads it: from the FT8 event stream, served by the
   Service's own handler. FT8 is OFF in this daemon's configuration, which is
   what makes that safe: a subscriber starts audio capture only on an enabled,
   started service, and the test refuses to run otherwise.
*/

// loggedFrames opens the FT8 event stream and returns the ft8-logged frames it
// carries, in order. It returns once the stream is subscribed: the handler
// writes ": connected" after it has subscribed, so a frame published after
// this returns cannot be missed.
func loggedFrames(t *testing.T, d *daemon) <-chan map[string]any {
	t.Helper()
	if d.ft8.Enabled() {
		t.Fatal("fixture: FT8 must be off in this daemon: an enabled service starts audio capture for a subscriber")
	}
	stop := make(chan struct{})
	ts := httptest.NewServer(d.ft8.HTTPHandler(stop))
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("open the FT8 event stream: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the FT8 event stream answered %d", res.StatusCode)
	}
	frames := make(chan map[string]any, 16)
	connected := make(chan struct{})
	var once sync.Once
	go func() {
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		event := ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, ": connected"):
				once.Do(func() { close(connected) })
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == "ft8-logged":
				var f map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f) == nil {
					frames <- f
				}
			case line == "":
				event = ""
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = res.Body.Close()
		close(stop)
		ts.Close()
	})
	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("the FT8 event stream never said it was connected")
	}
	return frames
}

func nextLogged(t *testing.T, frames <-chan map[string]any, what string) map[string]any {
	t.Helper()
	select {
	case f := <-frames:
		return f
	case <-time.After(5 * time.Second):
		t.Fatalf("no ft8-logged frame for %s", what)
		return nil
	}
}

// One contact, one row, one frame, and the frame names that row. That each
// contact is published exactly once is shown without waiting to see nothing: the
// frame after a contact's must be the NEXT contact's, since a second frame for
// the earlier one would have been published, and so delivered, before it. The
// last contact is only that sentinel: nothing is claimed about how often it is
// published (the clean-room review of 9acd5ce2: a look at an empty channel
// proves nothing about a frame still on its way).
func TestFt8QsoLog_EachStoredContactPublishesOneLoggedFrameNamingItsRow(t *testing.T) {
	eachFtMode(t, func(t *testing.T, m ftMode) {
		d, _ := newFt8LogDaemon(t)
		frames := loggedFrames(t, d)

		m.complete(t, d, 1, "JA1XYZ")
		m.stored(t, d, "JA1XYZ") // exactly one row, and its upload row

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rows, err := d.db.FetchQsoSliceByLogbookIdWithContext(ctx, 1)
		if err != nil || len(rows) != 1 {
			t.Fatalf("rows = %d, err %v", len(rows), err)
		}
		first := nextLogged(t, frames, "JA1XYZ")
		if first["callsign"] != "JA1XYZ" || first["uuid"] != rows[0].UUID || rows[0].UUID == "" {
			t.Fatalf("the frame = %v; want JA1XYZ's, naming the stored row %q", first, rows[0].UUID)
		}
		if first["mode"] != m.adifMode || first["band"] != "20m" || first["freq_hz"] != float64(14074000) {
			t.Fatalf("the frame = %v; want mode %q on 20m at the exchange's dial, 14.074 MHz", first, m.adifMode)
		}
		if sub, _ := first["submode"].(string); sub != m.adifSubmode {
			t.Fatalf("the frame's submode = %q; want %q", sub, m.adifSubmode)
		}

		m.complete(t, d, 2, "DL9UW")
		second := nextLogged(t, frames, "DL9UW")
		if second["callsign"] != "DL9UW" {
			t.Fatalf("the next frame = %v; want DL9UW's: JA1XYZ was published more than once", second)
		}
		if second["uuid"] == first["uuid"] {
			t.Fatal("two contacts were published under one row")
		}

		m.complete(t, d, 3, "K1ABC") // the sentinel
		if third := nextLogged(t, frames, "K1ABC"); third["callsign"] != "K1ABC" {
			t.Fatalf("the next frame = %v; want K1ABC's: DL9UW was published more than once", third)
		}
	})
}
