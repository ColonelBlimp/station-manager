package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestStress_20Clients_50QSOs launches 20 concurrent clients, each submitting
// 50 unique QSOs to a single logbook. Clients 0-9 use CW (RST 599), clients
// 10-19 use SSB/USB (RST 59). Each QSO includes non-promoted fields (comment,
// gridsquare, name, qth, my_gridsquare) to exercise the additional_data JSON
// blob storage in sqlite. Total: 1000 QSOs, all must store with zero errors.
func TestStress_20Clients_50QSOs(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Stress Log", "G4ABC")

	const numClients = 20
	const qsosPerClient = 50
	const totalQSOs = numClients * qsosPerClient

	var stored atomic.Int64
	var errCount atomic.Int64
	var wg sync.WaitGroup

	start := time.Now()

	for client := 0; client < numClients; client++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			for i := 0; i < qsosPerClient; i++ {
				callsign := fmt.Sprintf("T%dST%03d", clientID, i)
				// Spread times to avoid dedupe collisions.
				minute := (clientID*qsosPerClient + i) % 1440
				timeOn := fmt.Sprintf("%02d%02d", minute/60, minute%60)

				var modeTag, submodeTag, rstSentTag, rstRcvdTag string
				if clientID < 10 {
					modeTag = "<MODE:2>CW"
					submodeTag = ""
					rstSentTag = "<RST_SENT:3>599"
					rstRcvdTag = "<RST_RCVD:3>599"
				} else {
					modeTag = "<MODE:3>SSB"
					submodeTag = "<SUBMODE:3>USB"
					rstSentTag = "<RST_SENT:2>59"
					rstRcvdTag = "<RST_RCVD:2>59"
				}

				// Non-promoted fields — these exercise the additional_data JSON blob.
				comment := fmt.Sprintf("Stress test client %d qso %d", clientID, i)
				name := fmt.Sprintf("Op %d-%d", clientID, i)
				qth := fmt.Sprintf("City-%d", clientID)
				gridsquare := fmt.Sprintf("JO%02d%02d", clientID%90, i%90)
				myGridsquare := "IO91wm"

				body := fmt.Sprintf(
					"<CALL:%d>%s<BAND:3>40m%s%s<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>%s<TIME_OFF:4>%s%s%s<STATION_CALLSIGN:5>G4ABC<COUNTRY:4>Test"+
						"<COMMENT:%d>%s<NAME:%d>%s<QTH:%d>%s<GRIDSQUARE:%d>%s<MY_GRIDSQUARE:%d>%s<EOR>",
					len(callsign), callsign,
					modeTag, submodeTag,
					timeOn, timeOn,
					rstSentTag, rstRcvdTag,
					len(comment), comment,
					len(name), name,
					len(qth), qth,
					len(gridsquare), gridsquare,
					len(myGridsquare), myGridsquare,
				)

				w := submitQso(t, srv, lbID, body, false)

				if w.Code == http.StatusCreated && strings.Contains(w.Body.String(), `"status":"stored"`) {
					stored.Add(1)
				} else {
					errCount.Add(1)
					t.Logf("client %d qso %d: status=%d body=%s", clientID, i, w.Code, w.Body.String())
				}
			}
		}(client)
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("--- Stress Test Results ---")
	t.Logf("Clients:       %d", numClients)
	t.Logf("QSOs/client:   %d", qsosPerClient)
	t.Logf("Total QSOs:    %d", totalQSOs)
	t.Logf("Stored:        %d", stored.Load())
	t.Logf("Errors:        %d", errCount.Load())
	t.Logf("Elapsed:       %s", elapsed)
	t.Logf("Avg latency:   %s", elapsed/time.Duration(totalQSOs))
	t.Logf("Throughput:    %.1f QSOs/sec", float64(totalQSOs)/elapsed.Seconds())

	if errCount.Load() > 0 {
		t.Fatalf("expected 0 errors, got %d", errCount.Load())
	}
	if stored.Load() != totalQSOs {
		t.Fatalf("expected %d stored, got %d", totalQSOs, stored.Load())
	}
}
