package api

import (
	"context"
	stderrs "errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// maxSessionQsoUUIDs caps how many QSO UUIDs one session email/export request
// may carry (review 2026-07-22 #6). Without it the only bound is the request
// body, so a request could force thousands of DB fetches and a hugely amplified
// ADIF attachment. A session is one operating sitting (tens to low hundreds);
// 1000 is generous headroom. Shared by the export and email handlers.
const maxSessionQsoUUIDs = 1000

// SessionExportRequest is the SPA's POST body for a download: the canonical
// UUIDs of the session QSOs to export, plus an optional filename. Same
// daemon-rebuilds-from-DB contract as the email path — the client sends
// identifiers, never a blob — so the downloaded ADIF carries the fully
// enriched stored record (MY_* block, DXCC, zones, lat/lon), not the SPA's
// pre-submit subset.
type SessionExportRequest struct {
	UUIDs    []string `json:"uuids"`
	Filename string   `json:"filename,omitempty"`
}

// fetchSessionQsos loads QSOs by UUID in request order, skipping any that no
// longer resolve (soft-deleted since the SPA snapshotted the session) with a
// warning. A real fetch failure returns an error for the caller to map to a
// 500. Shared by the email and export session handlers so both rebuild from
// the same battle-tested path. `what` labels the log lines for the caller.
//
// One indexed point-lookup per UUID is deliberate: callers cap the list at
// maxSessionQsoUUIDs, so the round-trip count is bounded, and against
// in-process SQLite a batched `WHERE uuid IN (…)` would trade this simple
// skip-missing/preserve-order loop for extra complexity with no meaningful win
// at this scale (review 2026-07-22 #6, bulk-fetch sub-point — accepted, not a
// perf concern at the capped size).
func (s *Server) fetchSessionQsos(
	ctx context.Context, uuids []string, what string,
) (types.QsoSlice, error) {
	qsos := make(types.QsoSlice, 0, len(uuids))
	for _, uuid := range uuids {
		q, err := s.db.FetchQsoByUUIDWithContext(ctx, uuid)
		if err != nil {
			if stderrs.Is(err, errors.ErrNotFound) {
				s.logger.WarnWith().Str("uuid", uuid).
					Msg(what + ": QSO not found, skipping")
				continue
			}
			s.logger.ErrorWith().Err(err).Str("uuid", uuid).
				Msg(what + ": failed to fetch QSO")
			return nil, err
		}
		qsos = append(qsos, q)
	}
	return qsos, nil
}

// handleSessionExport rebuilds the session's ADIF from the live DB rows and
// returns it as a downloadable attachment. Mirrors handleSessionEmail's
// fetch+compose+archive, minus the SMTP send — so a download backs up to the
// same exports/sent-adif/ dir (best-effort) and carries identical, fully
// enriched records. Always-on route (no mailer gating); export never marks
// rows (only an email stamps "forwarded").
//
//	200  application/x-adif attachment
//	400  missing_required_field (no uuids) / invalid_field_value (bad filename) / no_qsos
//	500  fetch_failed / adif_compose_failed
func (s *Server) handleSessionExport(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleSessionExport"

	var req SessionExportRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	req.Filename = strings.TrimSpace(req.Filename)

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
	// Dedupe (first-occurrence order) so a repeated UUID isn't emitted twice.
	reqUUIDs := dedupeStrings(req.UUIDs)

	qsos, ferr := s.fetchSessionQsos(r.Context(), reqUUIDs, "session export")
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

	adifBody, err := adif.ComposeToAdifString(qsos)
	if err != nil {
		s.logger.ErrorWith().Err(err).Msg("session export: failed to compose ADIF")
		s.writeError(w, http.StatusInternalServerError, "adif_compose_failed",
			"failed to build ADIF for the session QSOs", op)
		return
	}

	// Filename: daemon default (UTC) unless the SPA supplied a bare name.
	now := time.Now().UTC()
	if req.Filename == "" {
		req.Filename = fmt.Sprintf("session-%s.adi", now.Format("20060102-150405"))
	} else {
		clean, cerr := safeArchiveFilename(req.Filename)
		if cerr != nil {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value", cerr.Error(), op)
			return
		}
		req.Filename = clean
	}

	// Backup-on-export: archive under the working dir before responding, the
	// same best-effort local copy the email path writes (operator ask — a
	// download should leave a backup too). Reuses the email handler's helper.
	s.archiveSessionAdif(req.Filename, adifBody)

	w.Header().Set("Content-Type", "application/x-adif")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", req.Filename))
	w.WriteHeader(http.StatusOK)
	if _, werr := w.Write([]byte(adifBody)); werr != nil {
		// Response already committed; nothing to do but log.
		s.logger.WarnWith().Err(werr).Msg("session export: failed to write ADIF response")
	}
}
