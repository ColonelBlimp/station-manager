package api

import (
	"encoding/base64"
	"encoding/json"
	stderr "errors"
	"net/http"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

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
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	// Verify the logbook exists. Without this, a bad id silently returns
	// an empty page, which is indistinguishable from an empty logbook —
	// surprising for clients.
	if _, err = s.db.FetchLogbookByIDWithContext(r.Context(), logbookID); err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			writeError(w, http.StatusNotFound, "logbook_not_found", "logbook does not exist", op)
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	// ---- limit ----
	limit := s.defaultPageLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid_limit",
				"limit must be a positive integer", op)
			return
		}
		if n > s.maxPageLimit {
			n = s.maxPageLimit
		}
		limit = n
	}

	// ---- cursor ----
	var afterDate, afterTime string
	var afterID int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		c, err := decodeQsoCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor",
				"after cursor is malformed", op)
			return
		}
		afterDate, afterTime, afterID = c.QsoDate, c.TimeOn, c.ID
	}

	// Fetch limit+1 so we can detect "has more" without a second query.
	rows, err := s.db.FetchQsoPageByLogbookWithContext(
		r.Context(), logbookID, afterDate, afterTime, afterID, limit,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	items := rows
	var nextCursor *string
	if len(rows) > limit {
		items = rows[:limit]
		c := encodeQsoCursor(items[len(items)-1])
		nextCursor = &c
	}

	writeJSON(w, http.StatusOK, struct {
		Items      types.QsoSlice `json:"items"`
		NextCursor *string        `json:"next_cursor"`
	}{
		Items:      items,
		NextCursor: nextCursor,
	})
}
