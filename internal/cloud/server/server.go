package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/reconcile"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
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
	tokens        map[string]int64
	version       string
	maxConcurrent int
	// exportSlots caps concurrently-running exports BELOW the DB pool size —
	// see maxConcurrentExports.
	exportSlots chan struct{}
}

// maxConcurrentExports caps concurrently-running /v1/export requests. An
// export streams from an open snapshot transaction for as long as the client
// takes to drain (up to exportWriteDeadline, 15 min), so each one PINS a pool
// connection — and cmd/smcloud caps the pool at 5 while the request semaphore
// admits 16 (review 2026-07-20 #1): five slow authenticated exports would
// exhaust the pool and starve health checks, uploads, and reconciliation.
// 2 leaves 3 connections free in the worst case. An over-limit export gets an
// immediate 503 + Retry-After. NB the restore client does NOT auto-retry —
// forwarding/smcloud/export.go returns any non-200 as an error and ignores
// Retry-After (only the push-path worker retries 5xx) — so a gated export
// surfaces to the operator-run restore/drill as a failure to re-run, which is
// acceptable for an operator-driven flow (review 2026-07-20 #1, round 12).
const maxConcurrentExports = 2

// exportRetryAfterSeconds is the Retry-After hint on a gated export — sized to
// a plausible export duration, not the 2 s request-semaphore hint.
const exportRetryAfterSeconds = "60"

// New builds a Server. tokens maps bearer tokens to tenant ids (P1: one entry,
// provisioned at boot). db is used only for the /v1/health ping. maxConcurrent
// caps in-flight requests (<= 0 → defaultMaxConcurrent; see limit.go).
func New(st *store.Store, db *sql.DB, log *slog.Logger, tokens map[string]int64, version string, maxConcurrent int) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		store: st, db: db, log: log, tokens: tokens, version: version, maxConcurrent: maxConcurrent,
		exportSlots: make(chan struct{}, maxConcurrentExports),
	}
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/version", s.handleVersion)
	mux.HandleFunc("PUT /v1/qsos", s.auth(s.handlePutQsos))
	mux.HandleFunc("PUT /v1/evidence", s.auth(s.handlePutEvidence))
	mux.HandleFunc("GET /v1/logbooks", s.auth(s.handleLogbooks))
	mux.HandleFunc("GET /v1/logbooks/{id}/reconcile", s.auth(s.handleReconcile))
	mux.HandleFunc("GET /v1/logbooks/{id}/manifest", s.auth(s.handleManifest))
	mux.HandleFunc("GET /v1/export", s.auth(s.handleExport))
	// Middleware, inside-out: gzip compresses negotiated responses (the
	// manifest and export payloads are the bandwidth-heavy ones — see
	// gzip.go); the concurrency limiter sits OUTERMOST so a rejected request
	// costs a 503 write and nothing else — no gzip writer, no negotiation,
	// no handler goroutine pile-up (see limit.go).
	return limitMiddleware(gzipMiddleware(s.log, mux), s.maxConcurrent)
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
// DeletedAt set = tombstone (soft delete; the cloud is retentive). Revision
// is the row's monotonic edit counter (ADR 0050); absent decodes as 0 =
// legacy client, which the store guard handles via its modified_at fallback.
type QsoUpload struct {
	ModifiedAt time.Time       `json:"modified_at"`
	Revision   int64           `json:"revision,omitempty"`
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
	// Exactly one JSON document: trailing content after the request object is
	// a malformed (or smuggled) body, not extra data to silently ignore.
	if err := dec.Decode(new(json.RawMessage)); !stderr.Is(err, io.EOF) {
		s.writeError(w, http.StatusBadRequest, "invalid_body", "body must be a single JSON document")
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

	// Validate the whole batch BEFORE the EnsureLogbook side effect, so a
	// rejected request provisions nothing.
	tenant := tenantID(r)
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
		// UUIDv7 exactly, not just any UUID: Postgres's uuid column would
		// accept every RFC 4122 version, but restore (qsoservice.Restore)
		// admits only v7 — a looser gate here stores backups that can never
		// come back, and malformed text would surface as a retryable 500
		// from the column type instead of this 400. Validated RAW — no
		// trimming — because the payload is stored verbatim: a padded UUID
		// that validated after TrimSpace was 200-accepted yet failed the
		// local qso table's 36-char CHECK at restore time, the worst failure
		// class for a backup (review 2026-07-20 #1).
		uuid := q.UUID
		if !utils.IsValidUUIDv7(uuid) {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"qsos["+strconv.Itoa(i)+"].qso.uuid must be a UUIDv7")
			return
		}
		if u.ModifiedAt.IsZero() {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"qsos["+strconv.Itoa(i)+"].modified_at is required")
			return
		}
		if u.Revision < 0 {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"qsos["+strconv.Itoa(i)+"].revision must be >= 0")
			return
		}
		recs = append(recs, store.Record{
			UUID:       uuid,
			TenantID:   tenant,
			ModifiedAt: u.ModifiedAt,
			Revision:   u.Revision,
			DeletedAt:  u.DeletedAt,
			Payload:    u.Qso,
		})
	}

	logbookID, err := s.store.EnsureLogbook(r.Context(), tenant, req.Logbook)
	if err != nil {
		s.log.Error("ensure logbook failed", "logbook", req.Logbook, "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "logbook provisioning failed")
		return
	}
	for i := range recs {
		recs[i].LogbookID = logbookID
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
		entries = append(entries, reconcile.Entry{UUID: m.UUID, ModifiedAt: m.ModifiedAt, Revision: m.Revision})
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
// the storage-row facts restore needs (logbook routing, recency, revision
// continuity, tombstone).
type ExportQso struct {
	UUID       string          `json:"uuid"`
	LogbookID  int64           `json:"logbook_id"`
	ModifiedAt time.Time       `json:"modified_at"`
	Revision   int64           `json:"revision,omitempty"`
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
	// Export concurrency gate — try-acquire, never queue (see
	// maxConcurrentExports): a gated export must fail fast, not hold a request
	// slot waiting for a 15-minute stream to finish.
	select {
	case s.exportSlots <- struct{}{}:
		defer func() { <-s.exportSlots }()
	default:
		w.Header().Set("Retry-After", exportRetryAfterSeconds)
		s.writeError(w, http.StatusServiceUnavailable, "overloaded",
			"too many concurrent exports; retry shortly")
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(exportWriteDeadline)); err != nil {
		// Fail open: an unsupported ResponseWriter keeps the server-wide
		// deadline, which is only a problem on a link slow enough to notice.
		s.log.Warn("export: extend write deadline failed", "err", err)
	}
	tenant := tenantID(r)
	// One snapshot for both reads — a logbook + its first QSOs committing
	// between separate queries would dump QSOs whose logbook_id is missing
	// from the same export's logbook list. Rows are STREAMED from the
	// snapshot transaction straight onto the wire (review 2026-07-20 #3):
	// the export set is unbounded (live rows + tombstones, per tenant), so
	// buffering it — as slices here plus json.Encoder's full marshal — made
	// every export an O(logbook) heap spike an authenticated caller could
	// aim at the service. Peak memory is now one row. The accepted trade:
	// the read-only tx (and its pool connection) stays open while the
	// response drains, so a slow client pins one of the 5 pool conns for up
	// to exportWriteDeadline — bounded, and cheaper than the O(logbook)
	// heap it replaces. The export gate above keeps concurrent exports
	// BELOW the pool size (the 16-slot request semaphore alone would let 5
	// slow exports drain the whole pool — review 2026-07-20 #1).
	count := 0
	started := false
	err := s.store.ExportSnapshot(r.Context(), tenant,
		func(books []store.LogbookInfo) error {
			if books == nil {
				books = []store.LogbookInfo{}
			}
			head, err := json.Marshal(books)
			if err != nil {
				return err
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			started = true
			if _, err := w.Write([]byte(`{"logbooks":`)); err != nil {
				return err
			}
			if _, err := w.Write(head); err != nil {
				return err
			}
			_, err = w.Write([]byte(`,"qsos":[`))
			return err
		},
		func(rec store.Record) error {
			row, err := json.Marshal(ExportQso{
				UUID:       rec.UUID,
				LogbookID:  rec.LogbookID,
				ModifiedAt: rec.ModifiedAt,
				Revision:   rec.Revision,
				DeletedAt:  rec.DeletedAt,
				Qso:        rec.Payload,
			})
			if err != nil {
				return err
			}
			if count > 0 {
				if _, err := w.Write([]byte{','}); err != nil {
					return err
				}
			}
			count++
			_, err = w.Write(row)
			return err
		})
	if err != nil {
		if !started {
			// Nothing written yet — a normal error response still works.
			s.log.Error("export: snapshot read failed", "err", err)
			s.writeError(w, http.StatusInternalServerError, "internal_error", "export failed")
			return
		}
		// Mid-stream failure: the 200 is already on the wire, so the only
		// honest signal is a truncated body — the missing "]}" terminator
		// makes it invalid JSON, which the restore client rejects as corrupt
		// rather than silently restoring a partial dump.
		s.log.Error("export: aborted mid-stream", "tenant_id", tenant, "written", count, "err", err)
		return
	}
	// Trailing newline matches the pre-streaming json.Encoder framing.
	if _, err := w.Write([]byte("]}\n")); err != nil {
		s.log.Error("export: aborted mid-stream", "tenant_id", tenant, "written", count, "err", err)
		return
	}
	s.log.Info("export served", "tenant_id", tenant, "qsos", count)
}
