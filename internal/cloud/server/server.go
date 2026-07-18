package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/reconcile"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// maxBodyBytes caps a PUT /v1/qsos body. A batch of a few hundred QSOs is
// ~1 MB; 32 MB comfortably covers a full-logbook first backup pushed in big
// batches while still bounding a hostile body.
const maxBodyBytes = 32 << 20

// Server is the SM Cloud HTTP API over the Postgres store. Construct with New,
// mount via Handler.
type Server struct {
	store *store.Store
	db    *sql.DB // health ping only; the store owns all queries
	log   *slog.Logger
	// tokens maps a bearer token to the tenant it authenticates. P1 holds one
	// entry (single-tenant); the map shape keeps multi-tenant a data change.
	tokens  map[string]int64
	version string
}

// New builds a Server. tokens maps bearer tokens to tenant ids (P1: one entry,
// provisioned at boot). db is used only for the /v1/health ping.
func New(st *store.Store, db *sql.DB, log *slog.Logger, tokens map[string]int64, version string) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: st, db: db, log: log, tokens: tokens, version: version}
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/version", s.handleVersion)
	mux.HandleFunc("PUT /v1/qsos", s.auth(s.handlePutQsos))
	mux.HandleFunc("GET /v1/logbooks", s.auth(s.handleLogbooks))
	mux.HandleFunc("GET /v1/logbooks/{id}/reconcile", s.auth(s.handleReconcile))
	mux.HandleFunc("GET /v1/logbooks/{id}/manifest", s.auth(s.handleManifest))
	mux.HandleFunc("GET /v1/export", s.auth(s.handleExport))
	return mux
}

// ---- transport helpers ------------------------------------------------------

// errorResponse mirrors the daemon's {code, message} envelope so every SM HTTP
// surface reads the same on the wire.
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Warn("response encode failed", "err", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, errorResponse{Code: code, Message: message})
}

// tenantKey is the request-context key carrying the authenticated tenant id.
type tenantKey struct{}

// auth wraps a handler with bearer-token authentication. The token is compared
// in constant time against each provisioned token; a match stores the tenant id
// in the request context.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		h := r.Header.Get("Authorization")
		if len(h) > len(prefix) && h[:len(prefix)] == prefix {
			presented := []byte(h[len(prefix):])
			for token, tenantID := range s.tokens {
				if subtle.ConstantTimeCompare(presented, []byte(token)) == 1 {
					ctx := context.WithValue(r.Context(), tenantKey{}, tenantID)
					next(w, r.WithContext(ctx))
					return
				}
			}
		}
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
	}
}

// tenantID reads the authenticated tenant from the request context (set by auth).
func tenantID(r *http.Request) int64 {
	id, _ := r.Context().Value(tenantKey{}).(int64)
	return id
}

// ownedLogbook resolves the {id} path value to a logbook owned by the
// authenticated tenant. Any failure — bad id, unknown logbook, other tenant's
// logbook — is a 404 (existence is not leaked) and a false return; the
// response is already written.
func (s *Server) ownedLogbook(w http.ResponseWriter, r *http.Request) (store.LogbookInfo, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		s.writeError(w, http.StatusNotFound, "not_found", "no such logbook")
		return store.LogbookInfo{}, false
	}
	lb, err := s.store.Logbook(r.Context(), id)
	if stderr.Is(err, store.ErrNotFound) || (err == nil && lb.TenantID != tenantID(r)) {
		s.writeError(w, http.StatusNotFound, "not_found", "no such logbook")
		return store.LogbookInfo{}, false
	}
	if err != nil {
		s.log.Error("logbook lookup failed", "logbook_id", id, "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "logbook lookup failed")
		return store.LogbookInfo{}, false
	}
	return lb, true
}

// ---- handlers ---------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		s.log.Warn("health: db ping failed", "err", err)
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "db": "unreachable"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "db": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}

// QsoUpload is one QSO on the PUT /v1/qsos wire: the full types.Qso as sent
// (stored verbatim) plus the storage-row facts that are not ADIF fields.
// DeletedAt set = tombstone (soft delete; the cloud is retentive).
type QsoUpload struct {
	ModifiedAt time.Time       `json:"modified_at"`
	DeletedAt  *time.Time      `json:"deleted_at,omitempty"`
	Qso        json.RawMessage `json:"qso"`
}

// PutQsosRequest is the PUT /v1/qsos body. Logbook is the target logbook's
// NAME — the server ensures it exists under the authenticated tenant
// (idempotent), so the client never has to pre-provision or learn ids to push.
type PutQsosRequest struct {
	Logbook string      `json:"logbook"`
	Qsos    []QsoUpload `json:"qsos"`
}

// PutQsosResponse reports the batch outcome. Applied < Received means the
// stale-push guard rejected some rows (sync telemetry, ADR 0040).
type PutQsosResponse struct {
	Received int `json:"received"`
	Applied  int `json:"applied"`
}

func (s *Server) handlePutQsos(w http.ResponseWriter, r *http.Request) {
	var req PutQsosRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_body", "body must be JSON: "+err.Error())
		return
	}
	if req.Logbook == "" || len(req.Logbook) > 64 {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "logbook name must be 1..64 characters")
		return
	}
	if len(req.Qsos) == 0 {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "qsos must be a non-empty array")
		return
	}

	tenant := tenantID(r)
	logbookID, err := s.store.EnsureLogbook(r.Context(), tenant, req.Logbook)
	if err != nil {
		s.log.Error("ensure logbook failed", "logbook", req.Logbook, "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "logbook provisioning failed")
		return
	}

	recs := make([]store.Record, 0, len(req.Qsos))
	for i, u := range req.Qsos {
		// Unmarshal ONLY to validate shape + extract the UUID; the stored
		// payload stays the caller's bytes verbatim (full-fidelity restore).
		var q types.Qso
		if err := json.Unmarshal(u.Qso, &q); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"qsos["+strconv.Itoa(i)+"].qso is not a QSO: "+err.Error())
			return
		}
		if q.UUID == "" {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"qsos["+strconv.Itoa(i)+"].qso.uuid is required")
			return
		}
		if u.ModifiedAt.IsZero() {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"qsos["+strconv.Itoa(i)+"].modified_at is required")
			return
		}
		recs = append(recs, store.Record{
			UUID:       q.UUID,
			TenantID:   tenant,
			LogbookID:  logbookID,
			ModifiedAt: u.ModifiedAt,
			DeletedAt:  u.DeletedAt,
			Payload:    u.Qso,
		})
	}

	applied, err := s.store.Upsert(r.Context(), recs)
	if err != nil {
		s.log.Error("upsert failed", "logbook_id", logbookID, "count", len(recs), "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "store write failed")
		return
	}
	s.log.Info("qsos upserted", "tenant_id", tenant, "logbook_id", logbookID,
		"received", len(recs), "applied", applied)
	s.writeJSON(w, http.StatusOK, PutQsosResponse{Received: len(recs), Applied: applied})
}

func (s *Server) handleLogbooks(w http.ResponseWriter, r *http.Request) {
	books, err := s.store.Logbooks(r.Context(), tenantID(r))
	if err != nil {
		s.log.Error("logbooks list failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "logbook list failed")
		return
	}
	if books == nil {
		books = []store.LogbookInfo{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"logbooks": books})
}

// ReconcileResponse is the per-logbook drift summary: the live-row count and
// the reconcile.Summary hash the daemon compares its local computation to.
type ReconcileResponse struct {
	LogbookID int64  `json:"logbook_id"`
	Count     int    `json:"count"`
	Hash      string `json:"hash"`
}

func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	lb, ok := s.ownedLogbook(w, r)
	if !ok {
		return
	}
	manifest, err := s.store.Manifest(r.Context(), lb.ID)
	if err != nil {
		s.log.Error("manifest read failed", "logbook_id", lb.ID, "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "manifest read failed")
		return
	}
	entries := make([]reconcile.Entry, 0, len(manifest))
	for _, m := range manifest {
		if m.Deleted {
			continue // tombstones reconcile via the manifest, not the summary
		}
		entries = append(entries, reconcile.Entry{UUID: m.UUID, ModifiedAt: m.ModifiedAt})
	}
	count, hash := reconcile.Summary(entries)
	s.writeJSON(w, http.StatusOK, ReconcileResponse{LogbookID: lb.ID, Count: count, Hash: hash})
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	lb, ok := s.ownedLogbook(w, r)
	if !ok {
		return
	}
	manifest, err := s.store.Manifest(r.Context(), lb.ID)
	if err != nil {
		s.log.Error("manifest read failed", "logbook_id", lb.ID, "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "manifest read failed")
		return
	}
	if manifest == nil {
		manifest = []store.ManifestEntry{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"logbook_id": lb.ID, "entries": manifest})
}

// ExportQso is one row of the export dump: the verbatim stored payload plus
// the storage-row facts restore needs (logbook routing, recency, tombstone).
type ExportQso struct {
	UUID       string          `json:"uuid"`
	LogbookID  int64           `json:"logbook_id"`
	ModifiedAt time.Time       `json:"modified_at"`
	DeletedAt  *time.Time      `json:"deleted_at,omitempty"`
	Qso        json.RawMessage `json:"qso"`
}

// exportWriteDeadline bounds writing the /v1/export response. It must safely
// exceed the restore client's own 10-minute timeout (forwarding/smcloud
// export.go) — the server-wide 2-minute WriteTimeout is sized for the small
// routes and would truncate a full-logbook dump mid-JSON on a slow or
// proxy-backpressured link, which the client would see as corrupt, not slow.
const exportWriteDeadline = 15 * time.Minute

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(exportWriteDeadline)); err != nil {
		// Fail open: an unsupported ResponseWriter keeps the server-wide
		// deadline, which is only a problem on a link slow enough to notice.
		s.log.Warn("export: extend write deadline failed", "err", err)
	}
	tenant := tenantID(r)
	books, err := s.store.Logbooks(r.Context(), tenant)
	if err != nil {
		s.log.Error("export: logbooks failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "export failed")
		return
	}
	recs, err := s.store.Export(r.Context(), tenant)
	if err != nil {
		s.log.Error("export: read failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "export failed")
		return
	}
	if books == nil {
		books = []store.LogbookInfo{}
	}
	qsos := make([]ExportQso, 0, len(recs))
	for _, rec := range recs {
		qsos = append(qsos, ExportQso{
			UUID:       rec.UUID,
			LogbookID:  rec.LogbookID,
			ModifiedAt: rec.ModifiedAt,
			DeletedAt:  rec.DeletedAt,
			Qso:        rec.Payload,
		})
	}
	s.log.Info("export served", "tenant_id", tenant, "qsos", len(qsos))
	s.writeJSON(w, http.StatusOK, map[string]any{"logbooks": books, "qsos": qsos})
}
