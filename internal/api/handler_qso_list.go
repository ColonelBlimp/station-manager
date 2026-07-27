package api

import (
	"encoding/base64"
	"encoding/json"
	stderr "errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// parseMissingFrom resolves the optional ?missing_from= param to an ADIF stamp
// prefix, writing the client error itself and returning ok=false when it cannot.
// Absent/empty → ("", true), i.e. no filter.
//
// The two ways this fails are DIFFERENT problems and now say so. They shared one
// message — "must name a configured forwarder with an upload-status stamp" —
// which reads as "you got the name wrong" even when the name is perfectly good
// and the destination simply has nothing to filter on. A row mirror like SM Cloud
// keeps a full copy of every QSO instead of a derived record, so it stamps
// nothing (no RegisterAdifPrefix) and "which QSOs are missing from it?" is not a
// question this table can answer. The operator hit exactly that, saw an
// unexplained 400, and had no way to tell which of the two it was (dogfood
// 2026-07-27).
func (s *Server) parseMissingFrom(w http.ResponseWriter, r *http.Request, op errors.Op) (string, bool) {
	raw := r.URL.Query().Get("missing_from")
	if raw == "" {
		return "", true
	}
	for _, fc := range s.cfg.Forwarders() {
		if !strings.EqualFold(fc.Name, raw) {
			continue
		}
		// Name/type come from config, not from the request — safe to name in the
		// response, and naming them is the whole point. The unmatched arm below
		// deliberately does NOT echo the raw param back.
		prefix, stamps := forwarding.AdifPrefixForType(fc.Type)
		if !stamps {
			s.writeError(w, http.StatusBadRequest, "missing_from_unsupported",
				fmt.Sprintf("%q (type %s) keeps a full copy of every QSO rather than stamping "+
					"each one, so it has no per-QSO upload status to filter on",
					fc.Name, fc.Type), op)
			return "", false
		}
		return prefix, true
	}
	s.writeError(w, http.StatusBadRequest, "invalid_missing_from",
		"missing_from does not name a configured forwarder", op)
	return "", false
}

// parseBoolQuery reads an optional boolean query param. Absent/empty → (false,
// nil). A present value is parsed with strconv.ParseBool (1/t/T/true/0/f/false/…);
// an unrecognised value is a client error, matching the strict validation the
// other list filters use. Shared by the QSO-list and logbook-count handlers.
func parseBoolQuery(r *http.Request, key string) (bool, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

// qsoPageCursor is the decoded form of the opaque ?after= token. It
// represents the sort-key tuple (qso_date, time_on, id) of the last QSO
// returned on the previous page, per api.md §4.4.
type qsoPageCursor struct {
	QsoDate string `json:"d"`
	TimeOn  string `json:"t"`
	ID      int64  `json:"i"`
}

func encodeQsoCursor(q types.Qso) string {
	c := qsoPageCursor{
		QsoDate: q.QsoDetails.QsoDate,
		TimeOn:  q.QsoDetails.TimeOn,
		ID:      q.ID,
	}
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeQsoCursor(s string) (qsoPageCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return qsoPageCursor{}, err
	}
	var c qsoPageCursor
	if err = json.Unmarshal(b, &c); err != nil {
		return qsoPageCursor{}, err
	}
	if c.QsoDate == "" || c.TimeOn == "" || c.ID < 1 {
		return qsoPageCursor{}, stderr.New("cursor missing required fields")
	}
	return c, nil
}

func (s *Server) handleListQsoByLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleListQsoByLogbook"

	logbookID, err := parsePathID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	// Verify the logbook exists. Without this, a bad id silently returns
	// an empty page, which is indistinguishable from an empty logbook —
	// surprising for clients.
	exists, err := s.db.LogbookExistsByIDWithContext(r.Context(), logbookID)
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}
	if !exists {
		s.writeError(w, http.StatusNotFound, "logbook_not_found", "logbook does not exist", op)
		return
	}

	// ---- limit ----
	limit := s.defaultPageLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			s.writeError(w, http.StatusBadRequest, "invalid_limit",
				"limit must be a positive integer", op)
			return
		}
		if n > s.maxPageLimit {
			n = s.maxPageLimit
		}
		limit = n
	}

	// ---- missing_from (ADR 0039): page only QSOs not yet uploaded to this
	// destination, by its durable ADIF stamp ----
	missingPrefix, ok := s.parseMissingFrom(w, r, op)
	if !ok {
		return // parseMissingFrom wrote the specific reason
	}

	// ---- not_emailed: page only QSOs not yet forwarded by email (the "Not
	// emailed only" logbook toggle), by the durable sm_fwrd_by_email_status
	// stamp ----
	notEmailed, err := parseBoolQuery(r, "not_emailed")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_not_emailed",
			"not_emailed must be a boolean", op)
		return
	}

	// ---- cursor ----
	var afterDate, afterTime string
	var afterID int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		c, err := decodeQsoCursor(raw)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid_cursor",
				"after cursor is malformed", op)
			return
		}
		afterDate, afterTime, afterID = c.QsoDate, c.TimeOn, c.ID
	}

	// Fetch limit+1 so we can detect "has more" without a second query.
	rows, err := s.db.FetchQsoPageByLogbookWithContext(
		r.Context(), logbookID, afterDate, afterTime, afterID, limit, missingPrefix, notEmailed,
	)
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	items := rows
	var nextCursor *string
	if len(rows) > limit {
		items = rows[:limit]
		c := encodeQsoCursor(items[len(items)-1])
		nextCursor = &c
	}

	s.writeJSON(w, http.StatusOK, struct {
		Items      types.QsoSlice `json:"items"`
		NextCursor *string        `json:"next_cursor"`
	}{
		Items:      items,
		NextCursor: nextCursor,
	})
}
