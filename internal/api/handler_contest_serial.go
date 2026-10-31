package api

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// contestIDPattern: an ADIF CONTEST_ID as the SPA sends it — no spaces, so it
// can sit in a Cabrillo CONTEST: header unchanged.
var contestIDPattern = regexp.MustCompile(`^[A-Z0-9_-]{1,40}$`)

// contestOccurrence reads and checks a request's contest occurrence: contest
// (an ADIF CONTEST_ID, upper-cased) and start (the event's start date,
// YYYYMMDD). msg is the refusal, "" when both are valid.
func contestOccurrence(q url.Values) (contest, start, msg string) {
	contest = strings.ToUpper(strings.TrimSpace(q.Get("contest")))
	if !contestIDPattern.MatchString(contest) {
		return "", "", "contest must be 1-40 letters, digits, '-' or '_'"
	}
	start = strings.TrimSpace(q.Get("start"))
	if _, err := time.Parse("20060102", start); err != nil || len(start) != 8 {
		return "", "", "start must be a date, YYYYMMDD"
	}
	return contest, start, ""
}

// handleContestSerial hands out the next sent serial for a contest in a
// logbook (W-0023 weekend slice). The Phone form calls it when a contest QSO
// starts; the number is consumed whether or not that QSO is ever logged, and
// is never handed out again. draft names the form's draft so a retry after a
// lost response gets the same number.
//
// start names the occurrence — the event's start date, YYYYMMDD — so each
// edition of an annual contest (or a dry run under another date) is its own
// sequence.
//
// Params (query): logbook, contest, start, draft. Response: {"serial": n}.
func (s *Server) handleContestSerial(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleContestSerial"
	q := r.URL.Query()

	logbookID, err := strconv.ParseInt(q.Get("logbook"), 10, 64)
	if err != nil || logbookID < 1 {
		s.writeError(w, http.StatusBadRequest, "invalid_id", "logbook must be a positive integer", op)
		return
	}
	contest, start, msg := contestOccurrence(q)
	if msg != "" {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", msg, op)
		return
	}
	draft := strings.TrimSpace(q.Get("draft"))
	if draft == "" || len(draft) > 64 {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "draft must be 1-64 characters", op)
		return
	}

	exists, err := s.db.LogbookExistsByIDWithContext(r.Context(), logbookID)
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}
	if !exists {
		s.writeError(w, http.StatusNotFound, "logbook_not_found", "logbook does not exist", op)
		return
	}

	serial, err := s.db.AllocateContestSerialWithContext(r.Context(), logbookID, contest, start, draft)
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}
	s.writeJSON(w, http.StatusOK, struct {
		Serial int64 `json:"serial"`
	}{Serial: serial})
}
