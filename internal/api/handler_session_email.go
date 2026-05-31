package api

import (
	"encoding/json"
	stderrs "errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// sessionAdifArchiveDir is the working-dir-relative location where each
// emailed session ADIF is archived. Nested under exports/ so future
// export kinds (full-logbook, migration) can sit alongside.
var sessionAdifArchiveDir = filepath.Join("exports", "sent-adif")

// SessionEmailRequest is the SPA's POST body. `to` is the recipient
// (typically the operator's QSL manager); `uuids` are the canonical
// ids of the session QSOs to email. The daemon rebuilds the ADIF from
// the live DB rows rather than trusting a client-built blob, so the
// mail carries current data (an edit-after-logging goes out fresh, not
// the submit-time snapshot) and the daemon stamps exactly what it sent.
// `subject` and `filename` are optional; the daemon stamps reasonable
// defaults from the current UTC time when absent so the SPA doesn't
// need timezone-aware formatting code.
type SessionEmailRequest struct {
	To       string   `json:"to"`
	Subject  string   `json:"subject,omitempty"`
	UUIDs    []string `json:"uuids"`
	Filename string   `json:"filename,omitempty"`
}

// SessionEmailResponse reports the send outcome plus which QSOs were
// durably stamped "forwarded by email". emailed lists the UUIDs whose
// sm_fwrd_by_email_* fields were written; date is the UTC YYYYMMDD
// stamp. On a stamp-write failure (after a successful send) emailed is
// empty — the mail still went out, and the SPA shows those rows
// unsent so a re-send or a Logbook-SPA fix can reconcile them.
type SessionEmailResponse struct {
	Status  string   `json:"status"`
	Emailed []string `json:"emailed"`
	Date    string   `json:"date"`
}

// handleSessionEmail packages the SPA-supplied ADIF as an email
// attachment and dispatches via the daemon's mailer.
//
// POST /v1/session/email
//
// Status codes:
//
//	200  body: {"status":"sent"} on successful send.
//	400  validation: missing to / adif, malformed JSON, etc.
//	503  mailer disabled (smtp.host empty in config) — operator
//	     hasn't configured SMTP; SPA toasts "email not configured".
//	502  SMTP transport failure (DNS / TLS / auth / RCPT rejected)
//	     — wraps the underlying error so logs carry the cause.
//
// The body part is a one-line "ADIF for session ... attached."
// summary; future versions can let the operator type a free-text
// note from the panel before send.
func (s *Server) handleSessionEmail(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleSessionEmail"

	if !s.mailer.Enabled() {
		s.writeError(w, http.StatusServiceUnavailable, "mailer_disabled",
			"SMTP is not configured (smtp.host is empty)", op)
		return
	}

	var req SessionEmailRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.maxBodyBytes)).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json",
			fmt.Sprintf("could not decode request body: %v", err), op)
		return
	}

	req.To = strings.TrimSpace(req.To)
	req.Subject = strings.TrimSpace(req.Subject)
	req.Filename = strings.TrimSpace(req.Filename)

	if req.To == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_field",
			"to is required", op)
		return
	}
	if !strings.Contains(req.To, "@") {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"to must be an email address", op)
		return
	}
	if len(req.UUIDs) == 0 {
		s.writeError(w, http.StatusBadRequest, "missing_required_field",
			"uuids is required", op)
		return
	}

	// Rebuild the ADIF from the live DB rows. The daemon is the source
	// of truth: it emails exactly the rows it will stamp, current as of
	// now. A UUID that no longer resolves (QSO soft-deleted since the
	// SPA snapshotted the session) is skipped with a warning — the send
	// proceeds with the rows that remain. Order follows the request so
	// the QSL manager sees the operator's submit order.
	qsos := make(types.QsoSlice, 0, len(req.UUIDs))
	stampIDs := make([]int64, 0, len(req.UUIDs))
	emailed := make([]string, 0, len(req.UUIDs))
	for _, uuid := range req.UUIDs {
		q, err := s.db.FetchQsoByUUIDWithContext(r.Context(), strings.TrimSpace(uuid))
		if err != nil {
			if stderrs.Is(err, errors.ErrNotFound) {
				s.logger.WarnWith().Str("uuid", uuid).
					Msg("session email: QSO not found, skipping")
				continue
			}
			s.logger.ErrorWith().Err(err).Str("uuid", uuid).
				Msg("session email: failed to fetch QSO")
			s.writeError(w, http.StatusInternalServerError, "fetch_failed",
				"failed to load session QSOs", op)
			return
		}
		qsos = append(qsos, q)
		stampIDs = append(stampIDs, q.ID)
		emailed = append(emailed, q.UUID)
	}
	if len(qsos) == 0 {
		s.writeError(w, http.StatusBadRequest, "no_qsos",
			"none of the supplied QSOs could be found", op)
		return
	}

	adifBody, err := adif.ComposeToAdifString(qsos)
	if err != nil {
		s.logger.ErrorWith().Err(err).Msg("session email: failed to compose ADIF")
		s.writeError(w, http.StatusInternalServerError, "adif_compose_failed",
			"failed to build ADIF for the session QSOs", op)
		return
	}

	// Daemon-side defaults for subject + filename. Operator-supplied
	// values pass through untouched. UTC stamp for reproducibility
	// across operators in different timezones.
	now := time.Now().UTC()
	if req.Subject == "" {
		req.Subject = fmt.Sprintf("Station Manager session ADIF — %s", now.Format("2006-01-02 15:04 UTC"))
	}
	if req.Filename == "" {
		req.Filename = fmt.Sprintf("session-%s.adi", now.Format("20060102-150405"))
	}

	// Archive the composed ADIF under the working dir BEFORE attempting
	// the send, so a failed / flaky SMTP delivery (the operator's
	// network is unreliable) still leaves a usable local export. This is
	// best-effort: a write failure is logged and does not fail the
	// request — the email path is what the operator is actually invoking.
	s.archiveSessionAdif(req.Filename, adifBody)

	msg := email.Message{
		To:      req.To,
		Subject: req.Subject,
		Body:    fmt.Sprintf("ADIF for this session attached.\nGenerated by Station Manager at %s.\n", now.Format(time.RFC1123)),
		Attachments: []email.Attachment{{
			Filename:    req.Filename,
			ContentType: "application/x-adif",
			Body:        []byte(adifBody),
		}},
	}

	if err := s.mailer.Send(r.Context(), msg); err != nil {
		if stderrs.Is(err, email.ErrMailerDisabled) {
			// Race: operator cleared SMTP config between the
			// Enabled() check above and Send. Same UX as the
			// pre-check path.
			s.writeError(w, http.StatusServiceUnavailable, "mailer_disabled",
				"SMTP is not configured", op)
			return
		}
		if stderrs.Is(err, email.ErrInvalidMessage) {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				err.Error(), op)
			return
		}
		s.logger.ErrorWith().Err(err).Str("to", req.To).Msg("session email send failed")
		s.writeError(w, http.StatusBadGateway, "smtp_failure",
			"SMTP send failed; check daemon logs for the cause", op)
		return
	}

	// Stamp the durable "forwarded by email" flag AFTER a successful
	// send. A failure here does not fail the request — the mail has
	// already left — so we log it and report an empty emailed list,
	// which the SPA renders as still-unsent. The Logbook SPA (or a
	// re-send) reconciles the gap. UTC YYYYMMDD matches the upload
	// stamp's granularity.
	stampDate := now.Format("20060102")
	n, err := s.db.MarkSessionEmailedWithContext(r.Context(), stampIDs)
	if err != nil {
		s.logger.ErrorWith().Err(err).Int("qso_count", len(stampIDs)).
			Msg("session email sent but stamping forwarded-by-email flag failed")
		emailed = emailed[:0]
		stampDate = ""
	} else if int(n) != len(stampIDs) {
		// Some rows vanished between fetch and stamp (soft-deleted).
		// The mail still carried them; we just couldn't mark them. The
		// count mismatch is logged for visibility; emailed stays as the
		// full set we attempted, since the SPA's optimistic mark is
		// harmless and a re-send re-stamps.
		s.logger.WarnWith().Int64("stamped", n).Int("expected", len(stampIDs)).
			Msg("session email: fewer rows stamped than sent")
	}

	s.writeJSON(w, http.StatusOK, SessionEmailResponse{
		Status:  "sent",
		Emailed: emailed,
		Date:    stampDate,
	})
}

// archiveSessionAdif writes the composed ADIF to
// <workingDir>/exports/sent-adif/<filename> as a durable local copy of
// what was (or will be) emailed. Best-effort: every failure is logged
// and swallowed so it can never block or fail the send. Skipped with a
// warning if the working dir is unresolved (which would otherwise drop
// the file into the daemon's cwd — a stray-file trap per the
// working-dir-resolution rule).
func (s *Server) archiveSessionAdif(filename, body string) {
	wd := s.cfg.WorkingDir()
	if wd == "" {
		s.logger.WarnWith().Msg("session email: working dir unresolved, skipping local ADIF archive")
		return
	}
	dir := filepath.Join(wd, sessionAdifArchiveDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.logger.WarnWith().Err(err).Str("dir", dir).
			Msg("session email: could not create ADIF archive dir")
		return
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		s.logger.WarnWith().Err(err).Str("path", path).
			Msg("session email: could not write local ADIF archive")
		return
	}
	s.logger.InfoWith().Str("path", path).Msg("session email: archived ADIF locally")
}
