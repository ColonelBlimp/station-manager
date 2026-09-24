package api

import (
	"context"
	stderr "errors"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ArchiveManager is the QSO archive port (ADR 0071, W-0021 slice 3): the
// catalogue as the daemon reports it, managed provisioning, and the activation
// that the attended restart completes. Implemented by internal/archive and
// injected by cmd/smd (SetArchiveManager), the same shape as SetRestart, so
// internal/api's frozen import set (ADR 0043) gains no package. Every refusal
// is an error carrying RequestCode(), mapped to a status here.
type ArchiveManager interface {
	List() []types.QsoArchiveView
	CreateArchive(ctx context.Context, req types.QsoArchiveCreateRequest) (types.QsoArchiveCreated, error)
	Activate(ctx context.Context, id string) (types.QsoArchiveActivation, error)
}

// requestCoder is the port's error classification: a refused request names its
// code without this package naming the implementing type.
type requestCoder interface {
	RequestCode() string
}

// SetArchiveManager injects the archive port (cmd/smd). nil → the routes
// answer 503 archives_unavailable.
func (s *Server) SetArchiveManager(m ArchiveManager) { s.archives = m }

// archiveErrorStatus maps a refused archive request's code to its status.
// Anything unlisted is a 500 with the code preserved.
var archiveErrorStatus = map[string]int{
	"missing_required_field":    http.StatusBadRequest,
	"invalid_field_value":       http.StatusBadRequest,
	"archive_not_found":         http.StatusNotFound,
	"archive_active":            http.StatusConflict,
	"activation_in_progress":    http.StatusConflict,
	"tx_busy":                   http.StatusConflict,
	"archive_file_missing":      http.StatusConflict,
	"archive_file_unreadable":   http.StatusConflict,
	"archive_no_identity":       http.StatusConflict,
	"archive_identity_mismatch": http.StatusConflict,
	"restart_unavailable":       http.StatusServiceUnavailable,
	"activation_persist_failed": http.StatusInternalServerError,
	"restart_failed":            http.StatusInternalServerError,
	"pending_unclear":           http.StatusInternalServerError,
}

// writeArchiveError answers a refused or failed archive request. A coded
// refusal keeps its code and message (they are the operator's diagnostic —
// the path of the file, the busy reason); anything else is a server error
// under fallbackCode with the detail logged, not served.
func (s *Server) writeArchiveError(w http.ResponseWriter, op errors.Op, err error, fallbackCode, clientMsg string) {
	var coded requestCoder
	if stderr.As(err, &coded) {
		status, ok := archiveErrorStatus[coded.RequestCode()]
		if !ok {
			status = http.StatusInternalServerError
		}
		s.writeError(w, status, coded.RequestCode(), err.Error(), op)
		return
	}
	s.writeServerError(w, op, err, fallbackCode, clientMsg)
}

func (s *Server) archivesUnavailable(w http.ResponseWriter, op errors.Op) bool {
	if s.archives != nil {
		return false
	}
	s.writeError(w, http.StatusServiceUnavailable, "archives_unavailable", "this daemon has no archive manager wired", op)
	return true
}

type qsoArchivesResponse struct {
	Archives []types.QsoArchiveView `json:"archives"`
}

// handleListQsoArchives serves GET /v1/qso-archives: the catalogue with each
// archive's state (active | pending | inactive) — honest state per ADR 0071
// AC 4: a candidate is pending until the restart proves it, never active.
func (s *Server) handleListQsoArchives(w http.ResponseWriter, _ *http.Request) {
	const op errors.Op = "api.handleListQsoArchives"
	if s.archivesUnavailable(w, op) {
		return
	}
	s.writeJSON(w, http.StatusOK, qsoArchivesResponse{Archives: s.archives.List()})
}

// handleCreateQsoArchive serves POST /v1/qso-archives: provision a managed
// archive, inactive, named by its semantics only (no path). 201 with the new
// archive; 200 when request_key found the archive an earlier request made.
func (s *Server) handleCreateQsoArchive(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleCreateQsoArchive"
	if s.archivesUnavailable(w, op) {
		return
	}
	var req types.QsoArchiveCreateRequest
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	created, err := s.archives.CreateArchive(r.Context(), req)
	if err != nil {
		s.writeArchiveError(w, op, err, "archive_create_failed", "the archive could not be created")
		return
	}
	status := http.StatusCreated
	if created.Reused {
		status = http.StatusOK
	}
	s.writeJSON(w, status, created)
}

// handleActivateQsoArchive serves POST /v1/qso-archives/{uuid}/activate: the
// attended restart is the switch. 202 means the pending selector is written
// and the restart requested, with TX admission sealed until the process exits;
// the body says whether the write is known durable.
func (s *Server) handleActivateQsoArchive(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleActivateQsoArchive"
	if s.archivesUnavailable(w, op) {
		return
	}
	id := r.PathValue("uuid")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_param", "archive id is required", op)
		return
	}
	out, err := s.archives.Activate(r.Context(), id)
	if err != nil {
		s.writeArchiveError(w, op, err, "activation_failed", "the activation could not be requested")
		return
	}
	s.writeJSON(w, http.StatusAccepted, out)
}
