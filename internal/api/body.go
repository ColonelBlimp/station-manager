package api

import (
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// readBody reads the request body subject to the configured size cap
// (Server.MaxBodyBytes). Returns the body bytes and true on success.
//
// On failure it writes the appropriate error envelope (413 body_too_large
// or 400 read_error) directly and returns nil, false — the caller should
// just return without further writes. Close errors on the limit reader
// are logged at warn but don't fail the handler.
//
// One place knows about *http.MaxBytesError so string-matching the stdlib
// "http: request body too large" message (fragile across Go upgrades)
// stays out of handler code.
func (s *Server) readBody(w http.ResponseWriter, r *http.Request, op errors.Op) ([]byte, bool) {
	lr := http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	defer func() {
		if err := lr.Close(); err != nil {
			s.logger.WarnWith().Err(err).Msg("failed to close request body reader")
		}
	}()

	body, err := io.ReadAll(lr)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if stderr.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				"request body exceeds maximum size", op)
			return nil, false
		}
		writeError(w, http.StatusBadRequest, "read_error",
			"failed to read request body", op)
		return nil, false
	}
	return body, true
}

// readJSONBody reads the request body with the size cap, then unmarshals
// it into dst. Returns true on success. On failure it writes the
// appropriate error envelope (413, 400 read_error, or 400 invalid_json)
// and returns false — the caller should just return.
//
// An empty body unmarshals as `{}` (fields stay at their zero values).
// Callers that need "empty body is an error" should test for it before
// calling this helper.
func (s *Server) readJSONBody(w http.ResponseWriter, r *http.Request, op errors.Op, dst any) bool {
	body, ok := s.readBody(w, r, op)
	if !ok {
		return false
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json",
			"failed to parse request body", op)
		return false
	}
	return true
}
