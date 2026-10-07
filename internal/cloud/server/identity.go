package server

import (
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The identity wire (W-0021 5F.2; ADR 0088 scoped paths, ADR 0089 behaviours).
// Its request envelopes are STRICT (ruling S6): an unknown key — a case
// variant included — a duplicate key, or trailing content is 400 invalid_body.
// A silently dropped field is the hazard ADR 0088 was written against.

// strictFields maps each allowed envelope key to the decoder of its value.
type strictFields map[string]func(dec *json.Decoder) error

// errResponded is a field decoder's report that it already wrote the error
// response (the qsos streamer writes its own 400/413).
var errResponded = stderr.New("response already written")

// decodeStrict reads one JSON object whose keys are exactly among fields, each
// at most once, and nothing after it. It writes the 400/413 and returns false
// on any violation.
func (s *Server) decodeStrict(w http.ResponseWriter, r *http.Request, fields strictFields) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if tok, err := dec.Token(); err != nil {
		return s.rejectBody(w, err)
	} else if d, ok := tok.(json.Delim); !ok || d != '{' {
		return s.invalidBody(w)
	}
	seen := make(map[string]bool, len(fields))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return s.rejectBody(w, err)
		}
		key, _ := tok.(string)
		decode, known := fields[key]
		if !known || seen[key] {
			return s.invalidBody(w)
		}
		seen[key] = true
		if err := decode(dec); err != nil {
			if stderr.Is(err, errResponded) {
				return false
			}
			return s.rejectBody(w, err)
		}
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return s.rejectBody(w, err)
	}
	if err := dec.Decode(new(json.RawMessage)); !stderr.Is(err, io.EOF) {
		if err == nil {
			return s.invalidBody(w)
		}
		return s.rejectBody(w, err)
	}
	return true
}

func stringField(dst *string) func(*json.Decoder) error {
	return func(dec *json.Decoder) error { return dec.Decode(dst) }
}

// AdoptRequest is POST /v1/archives/adopt.
type AdoptRequest struct {
	LegacyName   string `json:"legacy_name"`
	ArchiveUUID  string `json:"archive_uuid"`
	ArchiveLabel string `json:"archive_label"`
	LogbookUUID  string `json:"logbook_uuid"`
	LogbookName  string `json:"logbook_name"`
	Callsign     string `json:"callsign"`
}

// AdoptResponse answers a successful adoption; Changed is false for a replay.
type AdoptResponse struct {
	ArchiveUUID string `json:"archive_uuid"`
	LogbookUUID string `json:"logbook_uuid"`
	Changed     bool   `json:"changed"`
}

func (s *Server) handleAdopt(w http.ResponseWriter, r *http.Request) {
	var req AdoptRequest
	if !s.decodeStrict(w, r, strictFields{
		"legacy_name":   stringField(&req.LegacyName),
		"archive_uuid":  stringField(&req.ArchiveUUID),
		"archive_label": stringField(&req.ArchiveLabel),
		"logbook_uuid":  stringField(&req.LogbookUUID),
		"logbook_name":  stringField(&req.LogbookName),
		"callsign":      stringField(&req.Callsign),
	}) {
		return
	}
	if msg := validateAdopt(req); msg != "" {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", msg)
		return
	}
	req.ArchiveUUID, req.LogbookUUID = store.CanonicalUUID(req.ArchiveUUID), store.CanonicalUUID(req.LogbookUUID)
	changed, err := s.store.Adopt(r.Context(), tenantID(r), store.AdoptRequest(req))
	if err != nil {
		s.identityError(w, r, "adopt", err)
		return
	}
	s.log.Info("legacy archive adopted", "tenant_id", tenantID(r), "archive_uuid", req.ArchiveUUID,
		"logbook_uuid", req.LogbookUUID, "changed", changed, "request_id", requestID(r))
	s.writeJSON(w, http.StatusOK, AdoptResponse{ArchiveUUID: req.ArchiveUUID, LogbookUUID: req.LogbookUUID, Changed: changed})
}

func validateAdopt(req AdoptRequest) string {
	switch {
	case req.LegacyName == "" || len(req.LegacyName) > 64:
		return "legacy_name must be 1..64 characters"
	case !utils.IsValidUUIDv7(req.ArchiveUUID):
		return "archive_uuid must be a UUIDv7"
	case !utils.IsValidUUIDv7(req.LogbookUUID):
		return "logbook_uuid must be a UUIDv7"
	}
	return displayValuesInvalid(req.ArchiveLabel, req.LogbookName, req.Callsign)
}

// displayValuesInvalid checks the display values' lengths (empty keeps the stored value).
func displayValuesInvalid(archiveLabel, logbookLabel, callsign string) string {
	switch {
	case len(archiveLabel) > 64:
		return "archive_label must be at most 64 characters"
	case len(logbookLabel) > 64:
		return "the logbook label must be at most 64 characters"
	case len(callsign) > 32:
		return "callsign must be at most 32 characters"
	}
	return ""
}

// identityError maps a store refusal to its 409, anything else to a 500.
func (s *Server) identityError(w http.ResponseWriter, r *http.Request, what string, err error) {
	var ice *store.IdentityConflictError
	if stderr.As(err, &ice) {
		s.log.Warn("identity "+what+" refused", "code", ice.Code, "tenant_id", tenantID(r), "request_id", requestID(r))
		s.writeError(w, http.StatusConflict, ice.Code, ice.Message)
		return
	}
	s.log.Error("identity "+what+" failed", "tenant_id", tenantID(r), "request_id", requestID(r), "err", err)
	s.writeError(w, http.StatusInternalServerError, "internal_error", "store write failed")
}

// IdentityPutRequest is PUT /v1/archives/{archive_uuid}/logbooks/{logbook_uuid}/qsos.
type IdentityPutRequest struct {
	ArchiveLabel string      `json:"archive_label"`
	LogbookLabel string      `json:"logbook_label"`
	Callsign     string      `json:"callsign"`
	Qsos         []QsoUpload `json:"qsos"`
}

// handleIdentityPut writes a batch to the logbook named by UUID inside the
// archive named by UUID, creating either on first use (ADR 0089 S1), in one
// transaction with the batch.
func (s *Server) handleIdentityPut(w http.ResponseWriter, r *http.Request) {
	archiveUUID, logbookUUID := r.PathValue("archive_uuid"), r.PathValue("logbook_uuid")
	if !utils.IsValidUUIDv7(archiveUUID) || !utils.IsValidUUIDv7(logbookUUID) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "the archive and logbook in the path must be UUIDv7s")
		return
	}
	var req IdentityPutRequest
	if !s.decodeStrict(w, r, strictFields{
		"archive_label": stringField(&req.ArchiveLabel),
		"logbook_label": stringField(&req.LogbookLabel),
		"callsign":      stringField(&req.Callsign),
		"qsos": func(dec *json.Decoder) error {
			if !s.streamQsos(w, dec, &req.Qsos) {
				return errResponded
			}
			return nil
		},
	}) {
		return
	}
	if msg := displayValuesInvalid(req.ArchiveLabel, req.LogbookLabel, req.Callsign); msg != "" {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", msg)
		return
	}
	if len(req.Qsos) == 0 {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "qsos must be a non-empty array")
		return
	}
	tenant := tenantID(r)
	recs, ok := s.validateUploads(w, tenant, req.Qsos)
	if !ok {
		return
	}
	target := store.IdentityTarget{
		ArchiveUUID: store.CanonicalUUID(archiveUUID), ArchiveLabel: req.ArchiveLabel,
		LogbookUUID: store.CanonicalUUID(logbookUUID), LogbookLabel: req.LogbookLabel, Callsign: req.Callsign,
	}
	applied, err := s.store.UpsertIdentity(r.Context(), tenant, target, recs)
	if err != nil {
		s.writeUpsertError(w, r, target.LogbookUUID, err)
		return
	}
	s.log.Info("qsos upserted", "tenant_id", tenant, "archive_uuid", target.ArchiveUUID,
		"logbook_uuid", target.LogbookUUID, "received", len(recs), "applied", applied)
	s.writeJSON(w, http.StatusOK, PutQsosResponse{Received: len(recs), Applied: applied})
}

// scopedLogbook resolves {archive_uuid}/{logbook_uuid} to a logbook of the
// authenticated tenant's archive. Any failure — a malformed UUID, an unknown
// one, a logbook of another archive or tenant — is a 404 (existence is not
// leaked); the response is already written when ok is false.
func (s *Server) scopedLogbook(w http.ResponseWriter, r *http.Request) (store.IdentityLogbookInfo, bool) {
	archiveUUID, logbookUUID := r.PathValue("archive_uuid"), r.PathValue("logbook_uuid")
	if !utils.IsValidUUIDv7(archiveUUID) || !utils.IsValidUUIDv7(logbookUUID) {
		s.writeError(w, http.StatusNotFound, "not_found", "no such logbook")
		return store.IdentityLogbookInfo{}, false
	}
	lb, err := s.store.IdentityLogbook(r.Context(), tenantID(r), store.CanonicalUUID(archiveUUID), store.CanonicalUUID(logbookUUID))
	if stderr.Is(err, store.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, "not_found", "no such logbook")
		return store.IdentityLogbookInfo{}, false
	}
	if err != nil {
		s.log.Error("logbook lookup failed", "tenant_id", tenantID(r), "request_id", requestID(r), "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "logbook lookup failed")
		return store.IdentityLogbookInfo{}, false
	}
	return lb, true
}

// ScopedReconcileResponse is the identity wire's drift summary: the logbook by
// both UUIDs (operator correction 2026-10-07), its live-row count and hash.
type ScopedReconcileResponse struct {
	ArchiveUUID string `json:"archive_uuid"`
	LogbookUUID string `json:"logbook_uuid"`
	Count       int    `json:"count"`
	Hash        string `json:"hash"`
}

// ScopedManifestResponse is the identity wire's manifest, by both UUIDs.
type ScopedManifestResponse struct {
	ArchiveUUID string                `json:"archive_uuid"`
	LogbookUUID string                `json:"logbook_uuid"`
	Entries     []store.ManifestEntry `json:"entries"`
}

func (s *Server) handleScopedReconcile(w http.ResponseWriter, r *http.Request) {
	lb, ok := s.scopedLogbook(w, r)
	if !ok {
		return
	}
	manifest, ok := s.readManifest(w, r, lb.ID)
	if !ok {
		return
	}
	count, hash := summarize(manifest)
	s.writeJSON(w, http.StatusOK, ScopedReconcileResponse{ArchiveUUID: lb.ArchiveUUID, LogbookUUID: lb.LogbookUUID, Count: count, Hash: hash})
}

func (s *Server) handleScopedManifest(w http.ResponseWriter, r *http.Request) {
	lb, ok := s.scopedLogbook(w, r)
	if !ok {
		return
	}
	manifest, ok := s.readManifest(w, r, lb.ID)
	if !ok {
		return
	}
	s.writeJSON(w, http.StatusOK, ScopedManifestResponse{ArchiveUUID: lb.ArchiveUUID, LogbookUUID: lb.LogbookUUID, Entries: manifest})
}

// ScopedExportQso is one row of the scoped export: the verbatim stored payload
// and the storage facts restore needs. No numeric logbook id — the head names
// the logbook by UUID.
type ScopedExportQso struct {
	UUID       string          `json:"uuid"`
	ModifiedAt time.Time       `json:"modified_at"`
	Revision   int64           `json:"revision,omitempty"`
	DeletedAt  *time.Time      `json:"deleted_at,omitempty"`
	Qso        json.RawMessage `json:"qso"`
}

type scopedExportHead struct {
	Archive struct {
		UUID  string `json:"uuid"`
		Label string `json:"label"`
	} `json:"archive"`
	Logbook struct {
		UUID     string `json:"uuid"`
		Label    string `json:"label"`
		Callsign string `json:"callsign"`
	} `json:"logbook"`
}

// handleScopedExport streams one logbook, by UUID, from one snapshot: its
// identity and display values, then every record, tombstones included (ADR
// 0088: an old server 404s this path rather than over-delivering).
func (s *Server) handleScopedExport(w http.ResponseWriter, r *http.Request) {
	archiveUUID, logbookUUID := r.PathValue("archive_uuid"), r.PathValue("logbook_uuid")
	if !utils.IsValidUUIDv7(archiveUUID) || !utils.IsValidUUIDv7(logbookUUID) {
		s.writeError(w, http.StatusNotFound, "not_found", "no such logbook")
		return
	}
	release, ok := s.acquireExport(w, r)
	if !ok {
		return
	}
	defer release()
	out := &exportStream{w: w}
	err := s.store.IdentityExportSnapshot(r.Context(), tenantID(r),
		store.CanonicalUUID(archiveUUID), store.CanonicalUUID(logbookUUID),
		func(lb store.IdentityLogbookInfo) error {
			var h scopedExportHead
			h.Archive.UUID, h.Archive.Label = lb.ArchiveUUID, lb.ArchiveLabel
			h.Logbook.UUID, h.Logbook.Label, h.Logbook.Callsign = lb.LogbookUUID, lb.LogbookLabel, lb.Callsign
			head, err := json.Marshal(h)
			if err != nil {
				return err
			}
			// Reopen the head object to append the qsos array.
			return out.begin(append(head[:len(head)-1], `,"qsos":[`...))
		},
		func(rec store.Record) error {
			return out.row(ScopedExportQso{
				UUID: rec.UUID, ModifiedAt: rec.ModifiedAt, Revision: rec.Revision,
				DeletedAt: rec.DeletedAt, Qso: rec.Payload,
			})
		})
	s.finishExport(w, r, out, err)
}
