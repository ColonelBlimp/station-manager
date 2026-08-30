package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

// TestStress_20Clients_50QSOs launches 20 concurrent clients, each submitting
// 50 unique QSOs to a single logbook. The first half of the clients use CW
// (RST 599), the second half SSB/USB (RST 59), so both the promoted-RST and the
// SUBMODE payloads are written under concurrent load. Each QSO includes
// non-promoted fields (comment, gridsquare, name, qth, my_gridsquare) to exercise
// the additional_data JSON blob storage in sqlite. Total: 1000 QSOs, all must
// store with zero errors. Under -short the run scales down (see below) but keeps
// both mode cohorts and genuine concurrency for the race detector.
func TestStress_20Clients_50QSOs(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Stress Log", "G4ABC")

	numClients := 20
	qsosPerClient := 50
	if testing.Short() {
		// The -race -short quick gate cannot afford the full 1000-QSO run: under the
		// race detector on CI's slower runner it pushes internal/api past the 5m
		// -timeout (the intermittent "panic: test timed out after 5m0s" that kept CI
		// red). Keep genuine concurrency so -race still exercises the parallel
		// submit/fetch/patch/delete paths, but at a fraction of the volume. The full
		// 1000-QSO stress still runs in CI's non-race step (go test -timeout 12m).
		numClients, qsosPerClient = 6, 6
	}
	totalQSOs := int64(numClients * qsosPerClient)

	var stored atomic.Int64
	var fetched atomic.Int64
	var patched atomic.Int64
	var deleted atomic.Int64
	var errCount atomic.Int64
	var fetchErrCount atomic.Int64
	var patchErrCount atomic.Int64
	var deleteErrCount atomic.Int64
	var wg sync.WaitGroup

	start := time.Now()

	for client := range numClients {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			for i := range qsosPerClient {
				callsign := fmt.Sprintf("T%dST%03d", clientID, i)
				// Spread times to avoid dedupe collisions.
				minute := (clientID*qsosPerClient + i) % 1440
				timeOn := fmt.Sprintf("%02d%02d", minute/60, minute%60)

				var modeTag, submodeTag, rstSentTag, rstRcvdTag string
				// Split the clients into two mode cohorts RELATIVE to the active
				// count (not a hardcoded 10): under -short numClients is small, and
				// a fixed <10 would put every client in the CW cohort, leaving the
				// SSB/SUBMODE path unexercised under the race detector (CI's only
				// -race run is -short). Half CW, half SSB/USB at any scale.
				if clientID < numClients/2 {
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

				if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"status":"stored"`) {
					errCount.Add(1)
					t.Logf("client %d qso %d submit: status=%d body=%s", clientID, i, w.Code, w.Body.String())
					continue
				}
				stored.Add(1)

				// Parse the QSO UUID from the submit response and fetch it
				// back to exercise the read path under concurrent write load.
				var r qsoservice.SubmitResult
				if err := unmarshalJSON(w.Body.String(), &r); err != nil || r.UUID == "" {
					fetchErrCount.Add(1)
					t.Logf("client %d qso %d: failed to decode uuid from %s (err=%v)", clientID, i, w.Body.String(), err)
					continue
				}
				qsoUUID := r.UUID

				getReq := httptest.NewRequest(http.MethodGet, "/v1/qso/"+qsoUUID, nil)
				getReq.SetPathValue("uuid", qsoUUID)
				getW := httptest.NewRecorder()
				srv.handleGetQso(getW, getReq)

				if getW.Code != http.StatusOK {
					fetchErrCount.Add(1)
					t.Logf("client %d qso %d fetch: status=%d body=%s", clientID, i, getW.Code, getW.Body.String())
					continue
				}
				if !strings.Contains(getW.Body.String(), fmt.Sprintf(`"call":"%s"`, callsign)) {
					fetchErrCount.Add(1)
					t.Logf("client %d qso %d fetch: call mismatch, body=%s", clientID, i, getW.Body.String())
					continue
				}
				fetched.Add(1)

				// Capture the pre-patch dedupe key so we can verify a FREQ change triggers
				// recompute. dedupe_key is server-internal (pruned from the public
				// projection, AW-1) — read it from the store, not the response.
				preQso, err := srv.db.FetchQsoByUUIDWithContext(context.Background(), qsoUUID)
				if err != nil {
					patchErrCount.Add(1)
					t.Logf("client %d qso %d: cannot read pre-patch dedupe_key: %v", clientID, i, err)
					continue
				}

				// PATCH the freq to something unique per (clientID, i) so
				// neighbouring QSOs don't collide. Original is 7.050;
				// spread new freqs across 7.100–7.199 MHz so the patched
				// value is always different from the pre-patch value.
				newFreqMHz := fmt.Sprintf("7.%03d", 100+((clientID*qsosPerClient+i)%100))
				patchBody := fmt.Sprintf(`{"freq":"%s","comment":"edited"}`, newFreqMHz)
				patchReq := httptest.NewRequest(http.MethodPatch,
					"/v1/qso/"+qsoUUID, strings.NewReader(patchBody))
				patchReq.SetPathValue("uuid", qsoUUID)
				patchReq.Header.Set("Content-Type", "application/json")
				patchW := httptest.NewRecorder()
				srv.handleUpdateQso(patchW, patchReq)

				if patchW.Code != http.StatusOK {
					patchErrCount.Add(1)
					t.Logf("client %d qso %d patch: status=%d body=%s", clientID, i, patchW.Code, patchW.Body.String())
					continue
				}
				var fetched2 struct {
					Comment string `json:"comment"`
				}
				if err := unmarshalJSON(patchW.Body.String(), &fetched2); err != nil {
					patchErrCount.Add(1)
					t.Logf("client %d qso %d patch: decode failed", clientID, i)
					continue
				}
				if fetched2.Comment != "edited" {
					patchErrCount.Add(1)
					t.Logf("client %d qso %d patch: comment not updated, body=%s", clientID, i, patchW.Body.String())
					continue
				}
				// dedupe_key is server-internal (AW-1) — read it from the store and confirm
				// the FREQ change recomputed it.
				postQso, err := srv.db.FetchQsoByUUIDWithContext(context.Background(), qsoUUID)
				if err != nil || postQso.DedupeKey == preQso.DedupeKey {
					patchErrCount.Add(1)
					t.Logf("client %d qso %d patch: dedupe_key not recomputed after FREQ change: %v", clientID, i, err)
					continue
				}
				patched.Add(1)

				// DELETE the QSO, then confirm a subsequent GET returns
				// 404 — the soft-delete path must hide the row from reads.
				delReq := httptest.NewRequest(http.MethodDelete,
					"/v1/qso/"+qsoUUID, nil)
				delReq.SetPathValue("uuid", qsoUUID)
				delW := httptest.NewRecorder()
				srv.handleDeleteQso(delW, delReq)

				if delW.Code != http.StatusNoContent {
					deleteErrCount.Add(1)
					t.Logf("client %d qso %d delete: status=%d body=%s", clientID, i, delW.Code, delW.Body.String())
					continue
				}

				verifyReq := httptest.NewRequest(http.MethodGet,
					"/v1/qso/"+qsoUUID, nil)
				verifyReq.SetPathValue("uuid", qsoUUID)
				verifyW := httptest.NewRecorder()
				srv.handleGetQso(verifyW, verifyReq)

				if verifyW.Code != http.StatusNotFound {
					deleteErrCount.Add(1)
					t.Logf("client %d qso %d post-delete GET: status=%d, want 404", clientID, i, verifyW.Code)
					continue
				}
				deleted.Add(1)
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
	t.Logf("Fetched:       %d", fetched.Load())
	t.Logf("Patched:       %d", patched.Load())
	t.Logf("Deleted:       %d", deleted.Load())
	t.Logf("Submit errors: %d", errCount.Load())
	t.Logf("Fetch errors:  %d", fetchErrCount.Load())
	t.Logf("Patch errors:  %d", patchErrCount.Load())
	t.Logf("Delete errors: %d", deleteErrCount.Load())
	t.Logf("Elapsed:       %s", elapsed)
	t.Logf("Avg latency:   %s (submit+fetch+patch+delete round trip)", elapsed/time.Duration(totalQSOs))
	t.Logf("Throughput:    %.1f QSOs/sec", float64(totalQSOs)/elapsed.Seconds())

	if errCount.Load() > 0 {
		t.Fatalf("expected 0 submit errors, got %d", errCount.Load())
	}
	if fetchErrCount.Load() > 0 {
		t.Fatalf("expected 0 fetch errors, got %d", fetchErrCount.Load())
	}
	if patchErrCount.Load() > 0 {
		t.Fatalf("expected 0 patch errors, got %d", patchErrCount.Load())
	}
	if deleteErrCount.Load() > 0 {
		t.Fatalf("expected 0 delete errors, got %d", deleteErrCount.Load())
	}
	if stored.Load() != totalQSOs {
		t.Fatalf("expected %d stored, got %d", totalQSOs, stored.Load())
	}
	if fetched.Load() != totalQSOs {
		t.Fatalf("expected %d fetched, got %d", totalQSOs, fetched.Load())
	}
	if patched.Load() != totalQSOs {
		t.Fatalf("expected %d patched, got %d", totalQSOs, patched.Load())
	}
	if deleted.Load() != totalQSOs {
		t.Fatalf("expected %d deleted, got %d", totalQSOs, deleted.Load())
	}
}
