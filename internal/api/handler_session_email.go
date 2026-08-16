package api

import (
	stderrs "errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/fsperm"
)

// sessionAdifArchiveDir is the working-dir-relative location where each
// emailed session ADIF is archived. Nested under exports/ so future
// export kinds (full-logbook, migration) can sit alongside.
var sessionAdifArchiveDir = filepath.Join("exports", "sent-adif")

// archiveCollisionLimit bounds the "-N" suffix search when the target archive
// filename already exists (review 2026-07-22 #4) — high enough to never bite a
// real operator, low enough that a pathological loop can't spin.
const archiveCollisionLimit = 1000

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
//	503  mailer disabled (smtp.enabled is false) — operator
//	     hasn't enabled SMTP; SPA toasts "email not configured".
//	502  SMTP transport failure (DNS / TLS / auth / RCPT rejected)
//	     — wraps the underlying error so logs carry the cause.
//
// The body part is a one-line "ADIF for session ... attached."
// summary; future versions can let the operator type a free-text
// note from the panel before send.
// sessionEmailResponseMargin is head-and-tailroom around the SMTP budget for
// the session-email response write deadline: the DB fetch, ADIF compose,
// archive write and forwarded-by-email stamp share the extended window with
// the send itself.
const sessionEmailResponseMargin = 30 * time.Second

func (s *Server) handleSessionEmail(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleSessionEmail"

	if !s.mailer.Enabled() {
		s.writeError(w, http.StatusServiceUnavailable, "mailer_disabled",
			"SMTP is not enabled (smtp.enabled is false)", op)
		return
	}

	var req SessionEmailRequest
	if !s.readJSONBody(w, r, op, &req) {
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
	// Parse the recipient at the API boundary, BEFORE any side effect (QSO
	// fetch, ADIF compose, local archive). net/mail.ParseAddress accepts a
	// single RFC 5322 address only — it rejects CR/LF and control chars (the
	// header-injection shape "a@b\r\nBcc: x@y") and comma-separated lists — so
	// an invalid recipient now fails as a 400 client error instead of reaching
	// the mailer and surfacing as a 502 after an archive was already written
	// (review 2026-06-19 M1). The normalized bare mailbox (addr.Address strips
	// any display name) is what we send, so the email package's To header and
	// SMTP envelope agree.
	addr, perr := mail.ParseAddress(req.To)
	if perr != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"to must be a single valid email address", op)
		return
	}
	req.To = addr.Address
	if len(req.UUIDs) == 0 {
		s.writeError(w, http.StatusBadRequest, "missing_required_field",
			"uuids is required", op)
		return
	}
	if len(req.UUIDs) > maxSessionQsoUUIDs {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			fmt.Sprintf("too many QSOs in one request (max %d)", maxSessionQsoUUIDs), op)
		return
	}
	// Deduplicate UUIDs (preserving first-occurrence order) before fetching:
	// a client that repeats a UUID would otherwise fetch the same QSO twice,
	// emit it twice in the ADIF attachment, and inflate the emitted list, while
	// the set-based stamp SQL counts it once — producing a spurious "fewer rows
	// stamped than sent" warning (review 2026-06-19 L1).
	reqUUIDs := dedupeStrings(req.UUIDs)

	// A successful send must always be REPORTABLE: the server's WriteTimeout
	// armed this connection's write deadline before the handler ran, while
	// the SMTP budget below can lawfully exceed it (config), so SMTP could
	// accept the message and the 200 then hit a dead connection — the client
	// retries and a REAL recipient gets the session twice (codex 2026-08-08
	// P1). Extend this response's own deadline past the mailer's budget plus
	// sessionEmailResponseMargin for the fetch/compose/archive/stamp around
	// the send. Best-effort: a recorder or a transport without deadlines
	// just keeps today's shape.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(s.mailer.Timeout() + sessionEmailResponseMargin))

	// Rebuild the ADIF from the live DB rows. The daemon is the source
	// of truth: it emails exactly the rows it will stamp, current as of
	// now. A UUID that no longer resolves (QSO soft-deleted since the
	// SPA snapshotted the session) is skipped with a warning — the send
	// proceeds with the rows that remain. Order follows the request so
	// the QSL manager sees the operator's submit order. Shared with the
	// export handler via fetchSessionQsos.
	qsos, ferr := s.fetchSessionQsos(r.Context(), reqUUIDs, "session email")
	if ferr != nil {
		s.writeError(w, http.StatusInternalServerError, "fetch_failed",
			"failed to load session QSOs", op)
		return
	}
	if len(qsos) == 0 {
		s.writeError(w, http.StatusBadRequest, "no_qsos",
			"none of the supplied QSOs could be found", op)
		return
	}
	stampIDs := make([]int64, 0, len(qsos))
	emailed := make([]string, 0, len(qsos))
	for _, q := range qsos {
		stampIDs = append(stampIDs, q.ID)
		emailed = append(emailed, q.UUID)
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
	// Prefix the logbook callsign so a QSL manager receiving logs from several
	// operators can tell whose this is at a glance — otherwise the logging callsign is
	// only visible inside the ADIF attachment. The session's QSOs share a logbook, so
	// the first QSO's logbook callsign identifies the lot. Best-effort: a fetch failure
	// leaves the callsign empty, which just omits the prefix; it never fails the send.
	lbCall, lerr := s.db.LogbookCallsignByIDWithContext(r.Context(), qsos[0].LogbookID)
	if lerr != nil {
		s.logger.WarnWith().Err(lerr).Msg("session email: could not resolve logbook callsign for subject prefix")
	}
	req.Subject = sessionEmailSubject(lbCall, req.Subject, now)
	if req.Filename == "" {
		req.Filename = fmt.Sprintf("session-%s.adi", now.Format("20060102-150405"))
	} else {
		// An operator-supplied filename is a bare attachment/archive name, never
		// a path — reject traversal so it can't escape the archive dir (H1).
		clean, ferr := safeArchiveFilename(req.Filename)
		if ferr != nil {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value", ferr.Error(), op)
			return
		}
		req.Filename = clean
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
		Kind:    "session_email", // logged in place of the operator-supplied subject (H4)
		Body:    sessionEmailBody(len(qsos), now),
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
		// H4: log the recipient DOMAIN only, never the raw address (smd.log is 0644).
		s.logger.ErrorWith().Err(err).Str("to_domain", email.Domain(req.To)).Msg("session email send failed")
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
	// ONE clock reading, taken now (not the pre-send `now`) and passed into
	// the stamp, so the stored date and the reported date cannot diverge when
	// a slow send crosses UTC midnight (codex 2026-08-08 P3).
	stampDate := time.Now().UTC().Format("20060102")
	n, err := s.db.MarkSessionEmailedWithContext(r.Context(), stampIDs, stampDate)
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
	if err == nil {
		// The stamp bumped each row's revision — re-enqueue to the row-mirror
		// forwarder(s) (SM Cloud) so their copies don't drift until the hourly
		// reconcile. Best-effort: the stamp is committed, the mail has left,
		// and the reconciler backstops a missed enqueue.
		if _, serr := s.qso.EnqueueStampSync(r.Context(), stampIDs); serr != nil {
			s.logger.WarnWith().Err(serr).Int("qso_count", len(stampIDs)).
				Msg("session email: stamp sync enqueue failed (reconcile will heal)")
		}
	}

	s.writeJSON(w, http.StatusOK, SessionEmailResponse{
		Status:  "sent",
		Emailed: emailed,
		Date:    stampDate,
	})
}

// sessionEmailSubject builds the session-email subject: the operator-supplied subject
// (or a daemon default stamped with the UTC time) prefixed with the logbook callsign,
// so a QSL manager handling multiple operators' logs sees whose log it is without
// opening the ADIF. An empty callsign omits the prefix. Hardcoded shape for now — the
// configurable-template enhancement (docs/backlog.md) will supersede it. Pure +
// now-injected for unit-testing without a clock.
func sessionEmailSubject(callsign, supplied string, now time.Time) string {
	subject := strings.TrimSpace(supplied)
	if subject == "" {
		subject = fmt.Sprintf("Station Manager session ADIF — %s", now.Format("2006-01-02 15:04 UTC"))
	}
	if c := strings.TrimSpace(callsign); c != "" {
		subject = c + ": " + subject
	}
	return subject
}

// sessionEmailBody is the plain-text body of a session email. Hardcoded for now —
// "ADIF for this session attached." then the QSO count then a generated-at line. A
// future enhancement lets the operator configure subject + body with formatting tags
// (see docs/backlog.md — "configurable session-email subject + body"). Pure +
// now-injected so it's unit-testable without a clock or a send.
func sessionEmailBody(n int, now time.Time) string {
	qsoWord := "QSOs"
	if n == 1 {
		qsoWord = "QSO"
	}
	return fmt.Sprintf("ADIF for this session attached.\nContains %d %s.\nGenerated by Station Manager at %s.\n",
		n, qsoWord, now.Format(time.RFC1123))
}

// dedupeStrings trims each element and returns the non-empty values with
// duplicates removed, preserving first-occurrence order. Used to collapse a
// session-email request's UUID list so a repeated id can't double-emit a QSO
// (review 2026-06-19 L1).
func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// safeArchiveFilename validates an operator-supplied filename. The value is used
// both as the email attachment display name AND as the final component of the
// local archive path, so it must be a bare filename — never a path. A traversal
// value such as "../../config.json" would otherwise resolve outside the archive
// dir and overwrite files under the working dir with ADIF content (review H1).
// Rejects path separators, "." / "..", and absolute paths; appends ".adi" when
// missing. The caller has already trimmed the value and defaulted the empty case.
func safeArchiveFilename(name string) (string, error) {
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." || filepath.IsAbs(name) {
		return "", fmt.Errorf("filename must be a bare name without path separators or %q", "..")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".adi") {
		name += ".adi"
	}
	return name, nil
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
	// The archive holds full exported QSO records and lives under the working dir (always
	// application-owned), so it is owner-private 0700 (ST-6). MkdirAll(0700) does not
	// tighten a pre-existing 0755 dir, so chmod it too. A permission failure skips and
	// logs this optional archive rather than failing the request.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.logger.WarnWith().Err(err).Str("dir", dir).
			Msg("session email: could not create ADIF archive dir")
		return
	}
	// AFFIRMATIVE containment: the archive dir must resolve — symlinks and all — to a path
	// INSIDE the working directory before any QSO data is written there. A bare os.Chmod
	// would follow a symlinked dir into an external target (review 52b6943a P2), and "no
	// warning" from SecureApplicationPath is NOT proof of containment — a *private* external
	// dir reached via an ancestor symlink (exports -> external) returns no warning
	// (review 88e4fc8e P2). fsperm.Contained resolves the whole path, so an escaping ancestor
	// symlink is caught here; treat any non-contained/unresolvable result as "skip".
	if contained, cerr := fsperm.Contained(wd, dir); cerr != nil || !contained {
		s.logger.WarnWith().Str("dir", dir).
			Msg("session email: ADIF archive dir resolves outside the working directory, skipping archive")
		return
	}
	// Contained + application-owned: tighten a pre-existing dir to 0700. SecureApplicationPath
	// re-verifies and never chmods through a symlink; a warning/error here means it could not
	// be made owner-private, so skip.
	if warn, err := fsperm.SecureApplicationPath(wd, dir, 0o700); err != nil || warn != "" {
		s.logger.WarnWith().Err(err).Str("dir", dir).
			Msg("session email: ADIF archive dir is not owner-private, skipping archive")
		return
	}
	path := filepath.Join(dir, filename)
	// Defence-in-depth: the handler already validates filename as a bare name,
	// but re-confirm the resolved path stays inside the archive dir before any
	// write — a traversal must never reach os.WriteFile (review H1).
	if rel, rerr := filepath.Rel(dir, path); rerr != nil || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		s.logger.WarnWith().Str("path", path).
			Msg("session email: refusing to archive outside the archive dir")
		return
	}
	written, werr := writeArchiveExclusive(dir, filename, body)
	if werr != nil {
		s.logger.WarnWith().Err(werr).Str("dir", dir).Str("filename", filename).
			Msg("session email: could not write local ADIF archive")
		return
	}
	s.logger.InfoWith().Str("path", written).Msg("session email: archived ADIF locally")
}

// writeArchiveExclusive writes body to dir/filename using O_EXCL so an existing
// archive is never truncated (review 2026-07-22 #4): the default filename has
// only one-second resolution and an explicit name can be reused, so a plain
// os.WriteFile would silently overwrite a prior backup. On a collision it
// appends "-N" before the extension (session.adi → session-1.adi → …) up to
// archiveCollisionLimit tries. Returns the path actually written. The caller has
// already validated filename as a bare name, and every "-N" candidate stays bare
// (no separators), so none can escape dir.
func writeArchiveExclusive(dir, filename, body string) (string, error) {
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	for i := 0; i <= archiveCollisionLimit; i++ {
		name := filename
		if i > 0 {
			name = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) {
				continue // name taken — try the next suffix
			}
			return "", err
		}
		_, werr := f.WriteString(body)
		cerr := f.Close()
		if werr != nil {
			return "", werr
		}
		if cerr != nil {
			return "", cerr
		}
		return path, nil
	}
	return "", fmt.Errorf("archive filename collision limit (%d) exhausted for %q",
		archiveCollisionLimit, filename)
}
