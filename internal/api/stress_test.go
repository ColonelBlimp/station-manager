package api

import (
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
	var fetched atomic.Int64
	var patched atomic.Int64
	var deleted atomic.Int64
	var errCount atomic.Int64
	var fetchErrCount atomic.Int64
	var patchErrCount atomic.Int64
	var deleteErrCount atomic.Int64
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

				if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"status":"stored"`) {
					errCount.Add(1)
					t.Logf("client %d qso %d submit: status=%d body=%s", clientID, i, w.Code, w.Body.String())
					continue
				}
				stored.Add(1)

				// Parse the QSO ID from the submit response and fetch it back
				// to exercise the read path under concurrent write load.
				var r qsoservice.SubmitResult
				if err := unmarshalJSON(w.Body.String(), &r); err != nil || r.ID < 1 {
					fetchErrCount.Add(1)
					t.Logf("client %d qso %d: failed to decode id from %s (err=%v)", clientID, i, w.Body.String(), err)
					continue
				}
				qsoID := r.ID

				getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d", qsoID), nil)
				getReq.SetPathValue("id", fmt.Sprintf("%d", qsoID))
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

				// Capture the pre-patch dedupe key so we can verify FREQ
				// change triggers recompute.
				var fetched1 struct {
					DedupeKey string `json:"dedupe_key"`
				}
				if err := unmarshalJSON(getW.Body.String(), &fetched1); err != nil || fetched1.DedupeKey == "" {
					patchErrCount.Add(1)
					t.Logf("client %d qso %d: cannot decode pre-patch dedupe_key", clientID, i)
					continue
				}

				// PATCH the freq to something unique per (clientID, i) so
				// neighbouring QSOs don't collide. Original is 7.050;
				// spread new freqs across 7.100–7.199 MHz so the patched
				// value is always different from the pre-patch value.
				newFreqMHz := fmt.Sprintf("7.%03d", 100+((clientID*qsosPerClient+i)%100))
				patchBody := fmt.Sprintf(`{"freq":"%s","comment":"edited"}`, newFreqMHz)
				patchReq := httptest.NewRequest(http.MethodPatch,
					fmt.Sprintf("/v1/qso/%d", qsoID), strings.NewReader(patchBody))
				patchReq.SetPathValue("id", fmt.Sprintf("%d", qsoID))
				patchReq.Header.Set("Content-Type", "application/json")
				patchW := httptest.NewRecorder()
				srv.handleUpdateQso(patchW, patchReq)

				if patchW.Code != http.StatusOK {
					patchErrCount.Add(1)
					t.Logf("client %d qso %d patch: status=%d body=%s", clientID, i, patchW.Code, patchW.Body.String())
					continue
				}
				var fetched2 struct {
					DedupeKey string `json:"dedupe_key"`
					Comment   string `json:"comment"`
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
				if fetched2.DedupeKey == fetched1.DedupeKey {
					patchErrCount.Add(1)
					t.Logf("client %d qso %d patch: dedupe_key not recomputed after FREQ change", clientID, i)
					continue
				}
				patched.Add(1)

				// DELETE the QSO, then confirm a subsequent GET returns
				// 404 — the soft-delete path must hide the row from reads.
				delReq := httptest.NewRequest(http.MethodDelete,
					fmt.Sprintf("/v1/qso/%d", qsoID), nil)
				delReq.SetPathValue("id", fmt.Sprintf("%d", qsoID))
				delW := httptest.NewRecorder()
				srv.handleDeleteQso(delW, delReq)

				if delW.Code != http.StatusNoContent {
					deleteErrCount.Add(1)
					t.Logf("client %d qso %d delete: status=%d body=%s", clientID, i, delW.Code, delW.Body.String())
					continue
				}

				verifyReq := httptest.NewRequest(http.MethodGet,
					fmt.Sprintf("/v1/qso/%d", qsoID), nil)
				verifyReq.SetPathValue("id", fmt.Sprintf("%d", qsoID))
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
