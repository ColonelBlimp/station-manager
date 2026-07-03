// Package httpkit holds the transport-level mechanics shared by every HTTP
// handler surface: the JSON error envelope, the response writers, and the
// size-capped body readers. It hides those mechanics behind a small Kit API so
// that as internal/api is split into per-surface packages (ADR 0043), each
// surface depends on this leaf rather than on a shared god-struct — and the
// envelope shape / body-limit policy lives in exactly one place.
//
// Kit deliberately owns only stateless-per-request transport concerns. The
// access-log middleware, panic recovery, and the load limiter stay in the
// root api package for now: they are wired once at composition time, not
// per-surface, so they don't yet need to move behind this boundary.
package httpkit

import (
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// Kit carries the per-response dependencies the writers and readers need:
// the structured logger (for write/encode diagnostics) and the request-body
// size cap.
type Kit struct {
	log     *logging.Service
	maxBody int64
}

// New builds a Kit. maxBodyBytes is the cap applied by ReadBody/ReadJSONBody.
func New(log *logging.Service, maxBodyBytes int64) *Kit {
	return &Kit{log: log, maxBody: maxBodyBytes}
}

// MaxBody returns the configured request-body size cap.
func (k *Kit) MaxBody() int64 { return k.maxBody }

// SetMaxBody overrides the request-body size cap. Primarily for tests that
// exercise the body-too-large path with a small cap.
func (k *Kit) SetMaxBody(n int64) { k.maxBody = n }

// ErrorResponse is the JSON error envelope per api.md Section 4.6.
type ErrorResponse struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	Op      errors.Op `json:"op,omitempty"`
}

// ErrorNoter is implemented by a response writer that wants to record the
// error envelope's classification for the access log. The root api package's
// responseRecorder satisfies it (structurally, via its exported NoteError),
// so WriteError can stash the code/message/op without httpkit importing api —
// the one seam this package boundary needs.
type ErrorNoter interface {
	NoteError(code, message, op string)
}

// WriteJSON marshals v as JSON and writes it with the given status. Encode
// errors are logged at warn — the status is already on the wire by the time a
// write can fail, so the error cannot be signalled to the client, but a silent
// drop makes mid-response failures invisible server-side. The warn line
// restores the breadcrumb without pretending we can recover.
func (k *Kit) WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		k.log.WarnWith().Err(err).Int("status", status).Msg("failed to encode JSON response")
	}
}

// WriteError writes the error envelope and, when the writer is the access-log
// recorder, stashes the classification so the access-log line can name what
// the failure was.
func (k *Kit) WriteError(w http.ResponseWriter, status int, code, message string, op errors.Op) {
	if n, ok := w.(ErrorNoter); ok {
		n.NoteError(code, message, string(op))
	}
	k.WriteJSON(w, status, ErrorResponse{
		Code:    code,
		Message: message,
		Op:      op,
	})
}

// WriteServerError handles 5xx outcomes consistently: logs the underlying
// error with full op-tagged context for the operator, then emits a
// deliberately generic envelope to the client. Raw err.Error() is never
// returned over the wire because internal error chains can carry SQL
// constraint snippets, file paths, or driver text the client has no business
// seeing.
//
// code and clientMsg are the wire-visible classification; pass stable,
// low-cardinality values ("db_error", "internal_error") so clients can switch
// on them. The full err goes to the log line.
func (k *Kit) WriteServerError(w http.ResponseWriter, op errors.Op, err error, code, clientMsg string) {
	k.log.ErrorWith().Err(err).Str("op", string(op)).Str("code", code).Msg("server error")
	k.WriteError(w, http.StatusInternalServerError, code, clientMsg, op)
}

// ReadBody reads the request body subject to the configured size cap. Returns
// the body bytes and true on success. On failure it writes the appropriate
// error envelope (413 body_too_large or 400 read_error) directly and returns
// nil, false — the caller should just return without further writes.
//
// One place knows about *http.MaxBytesError so string-matching the stdlib
// "http: request body too large" message (fragile across Go upgrades) stays
// out of handler code.
func (k *Kit) ReadBody(w http.ResponseWriter, r *http.Request, op errors.Op) ([]byte, bool) {
	lr := http.MaxBytesReader(w, r.Body, k.maxBody)
	defer func() {
		if err := lr.Close(); err != nil {
			k.log.WarnWith().Err(err).Msg("failed to close request body reader")
		}
	}()

	body, err := io.ReadAll(lr)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if stderr.As(err, &maxBytesErr) {
			k.WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				"request body exceeds maximum size", op)
			return nil, false
		}
		k.WriteError(w, http.StatusBadRequest, "read_error",
			"failed to read request body", op)
		return nil, false
	}
	return body, true
}

// ReadJSONBody reads the request body with the size cap, then unmarshals it
// into dst. Returns true on success. On failure it writes the appropriate
// error envelope (413, 400 read_error, or 400 invalid_json) and returns false.
//
// An empty body unmarshals as `{}` (fields stay at their zero values). Callers
// that need "empty body is an error" should test for it before calling this.
func (k *Kit) ReadJSONBody(w http.ResponseWriter, r *http.Request, op errors.Op, dst any) bool {
	body, ok := k.ReadBody(w, r, op)
	if !ok {
		return false
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		k.WriteError(w, http.StatusBadRequest, "invalid_json",
			"failed to parse request body", op)
		return false
	}
	return true
}
