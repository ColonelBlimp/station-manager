package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

// The api test binary doesn't import the qrz package, so register its ADIF stamp
// prefix here — parseMissingFrom maps a forwarder's type → this prefix.
func init() {
	forwarding.RegisterAdifPrefix("qrz", "QRZCOM")
}

// stampUploaded drives a QSO's qrz upload row to uploaded WITH the ADIF stamp —
// the durable "uploaded to qrz" signal the missing_from filter keys on.
func stampUploaded(t *testing.T, db *sqlite.Service, qsoID int64) {
	t.Helper()
	claimed, err := db.ClaimPendingUploadsWithContext(t.Context(), "qrz", 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	for _, c := range claimed {
		if c.QsoID == qsoID {
			if err := db.MarkUploadSuccessWithAdifStampWithContext(t.Context(), c.ID, "up-1", qsoID, "QRZCOM"); err != nil {
				t.Fatalf("mark+stamp: %v", err)
			}
			return
		}
	}
	t.Fatalf("no pending qrz row for qso %d", qsoID)
}

func listMissingFrom(t *testing.T, srv *Server, lbID int64, dest string) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/v1/logbook/%d/qso?missing_from=%s", lbID, dest)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("id", strconv.FormatInt(lbID, 10))
	w := httptest.NewRecorder()
	srv.handleListQsoByLogbook(w, req)
	return w
}

func countMissingFrom(t *testing.T, srv *Server, lbID int64, dest string) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/v1/logbook/%d/count?missing_from=%s", lbID, dest)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("id", strconv.FormatInt(lbID, 10))
	w := httptest.NewRecorder()
	srv.handleLogbookCount(w, req)
	return w
}

// A second QSO distinct from testQsoADIF (different CALL + TIME) so it's not a dupe.
const testQsoADIF2 = `<CALL:5>K9XYZ<BAND:3>40m<MODE:3>SSB<FREQ:5>7.060<QSO_DATE:8>20250508<TIME_ON:4>0900<TIME_OFF:4>0905<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:13>United States<EOR>`

func TestListQsoByLogbook_MissingFromFiltersUploaded(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"))
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	uploadedID, _ := submitAndGetID(t, srv, lbID, testQsoADIF)
	_, missingUUID := submitAndGetID(t, srv, lbID, testQsoADIF2)

	// Mark the first uploaded (with stamp); the second stays a gap.
	stampUploaded(t, srv.db, uploadedID)

	// List filtered to "missing from qrz" → only the unstamped QSO.
	w := listMissingFrom(t, srv, lbID, "qrz")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var got struct {
		Items []struct {
			UUID string `json:"uuid"`
		} `json:"items"`
	}
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].UUID != missingUUID {
		t.Fatalf("items = %+v, want only the unstamped QSO %s", got.Items, missingUUID)
	}

	// Count reflects the same filter.
	cw := countMissingFrom(t, srv, lbID, "qrz")
	if cw.Code != http.StatusOK {
		t.Fatalf("count status = %d, want 200; body = %s", cw.Code, cw.Body.String())
	}
	var cgot struct {
		Count int64 `json:"count"`
	}
	if err := unmarshalJSON(cw.Body.String(), &cgot); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	if cgot.Count != 1 {
		t.Fatalf("count = %d, want 1", cgot.Count)
	}
}

func TestListQsoByLogbook_MissingFromUnknownForwarder(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert"))
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w := listMissingFrom(t, srv, lbID, "nope")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	var e struct {
		Code string `json:"code"`
	}
	if err := unmarshalJSON(w.Body.String(), &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "invalid_missing_from" {
		t.Fatalf("code = %q, want invalid_missing_from", e.Code)
	}
}

// TestListQsoByLogbook_MissingFromRowMirror covers the destination that EXISTS
// and is spelled correctly but stamps nothing — SM Cloud holds a full copy of
// every QSO rather than a derived record, so "which QSOs are missing from it?"
// is not a question the logbook table can answer. It must not share the
// "no such forwarder" code, and the message must say why: the operator picked
// this from a dropdown, hit a bare 400, and had no way to tell the two apart
// (dogfood 2026-07-27).
func TestListQsoByLogbook_MissingFromRowMirror(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("cloud-backup", "smcloud", true, "insert"))
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w := listMissingFrom(t, srv, lbID, "cloud-backup")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := unmarshalJSON(w.Body.String(), &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "missing_from_unsupported" {
		t.Fatalf("code = %q, want missing_from_unsupported (NOT the unknown-name code)", e.Code)
	}
	// The operator must be able to tell which destination and why.
	if !strings.Contains(e.Message, "cloud-backup") || !strings.Contains(e.Message, "smcloud") {
		t.Errorf("message names neither the forwarder nor its type: %q", e.Message)
	}
	if !strings.Contains(e.Message, "full copy") {
		t.Errorf("message does not explain WHY there is nothing to filter on: %q", e.Message)
	}
}
