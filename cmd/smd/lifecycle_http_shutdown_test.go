package main

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

/*
   W-0022 SO6 (2026-10-13; integration characterization, no deterministic
   reversion claimed, as ruled): with a live HTTP request open — the hub's SSE
   stream — a generation's shutdown drains EVERY node under the strict stopGen.
   A failed http node would skip its dependents (eventhub, referencedb,
   qsoservice, sqliteservice, loggingservice), as in CI run 38356688161
   attempt 1; the listener's ordering is forced in internal/api (SO1–SO3).
*/

func TestHTTPShutdown_SO6_ALiveRequestStillLetsEveryNodeDrain(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) { c.SetupComplete = true; c.DefaultLogbookID = 1 })
	ownerOnlySocketDir(t, d)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatal(err)
	}
	if code, body := apiCall(t, d, http.MethodGet, "/v1/version"); code != http.StatusOK {
		t.Fatalf("version = %d %s", code, body)
	}

	sock := d.cfgSvc.Snapshot().SocketPath
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://smd/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("open the event stream: %v", err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	if !sc.Scan() || sc.Text() != ": connected" {
		t.Fatalf("event stream first line = %q (%v); want the connected comment", sc.Text(), sc.Err())
	}

	stopGen(t, orch) // strict: every node drained, none failed, timed out or skipped

	ended := make(chan struct{})
	go func() {
		for sc.Scan() {
		}
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the live event stream did not end with the generation")
	}
}
