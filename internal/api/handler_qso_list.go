package api

import (
	"encoding/base64"
	"encoding/json"
	stderr "errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// resolveMissingFromPrefix maps a ?missing_from= forwarder NAME to its ADIF
// upload-status prefix (uppercase, e.g. "QRZCOM") for the stamp-based "not yet
// uploaded to X" logbook filter (ADR 0039). ok=false when the name matches no
// configured forwarder, or that forwarder's type stamps no upload status (so
// "missing from" is undefined for it — e.g. a custom webhook).
func (s *Server) resolveMissingFromPrefix(name string) (string, bool) {
	for _, fc := range s.cfg.Forwarders() {
		if strings.EqualFold(fc.Name, name) {
			return forwarding.AdifPrefixForType(fc.Type)
		}
	}
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
	var missingPrefix string
	if raw := r.URL.Query().Get("missing_from"); raw != "" {
		p, ok := s.resolveMissingFromPrefix(raw)
		if !ok {
			s.writeError(w, http.StatusBadRequest, "invalid_missing_from",
				"missing_from must name a configured forwarder with an upload-status stamp", op)
			return
		}
		missingPrefix = p
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
