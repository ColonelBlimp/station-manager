package server

import (
	"encoding/json"
	stderr "errors"
	"net/http"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// acquireExport takes an export slot and extends the write deadline. The gate
// is try-acquire, never queue (see maxConcurrentExports): a gated export fails
// fast rather than holding a request slot waiting for a 15-minute stream. The
// caller defers release when ok.
func (s *Server) acquireExport(w http.ResponseWriter, r *http.Request) (release func(), ok bool) {
	select {
	case s.exportSlots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", exportRetryAfterSeconds)
		s.writeError(w, http.StatusServiceUnavailable, "overloaded",
			"too many concurrent exports; retry shortly")
		return nil, false
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(exportWriteDeadline)); err != nil {
		// Fail open: an unsupported ResponseWriter keeps the server-wide
		// deadline, which is only a problem on a link slow enough to notice.
		s.log.Warn("export: extend write deadline failed", "tenant_id", tenantID(r), "request_id", requestID(r), "err", err)
	}
	return func() { <-s.exportSlots }, true
}

// exportStream writes one streamed export document: a head that opens the
// "qsos" array, the rows comma-separated, and finishExport's "]}" terminator.
// Peak memory is one row.
type exportStream struct {
	w       http.ResponseWriter
	started bool
	count   int
}

func (e *exportStream) begin(head []byte) error {
	e.w.Header().Set("Content-Type", "application/json")
	e.w.WriteHeader(http.StatusOK)
	e.started = true
	_, err := e.w.Write(head)
	return err
}

func (e *exportStream) row(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if e.count > 0 {
		if _, err := e.w.Write([]byte{','}); err != nil {
			return err
		}
	}
	e.count++
	_, err = e.w.Write(b)
	return err
}

// finishExport ends the stream. Before anything was written an error is still
// a normal response (404 for a scoped export's unknown logbook, else 500).
// After the 200 the only honest signal is a truncated body: the missing "]}"
// makes it invalid JSON, which the restore client rejects as corrupt rather
// than silently restoring a partial dump.
func (s *Server) finishExport(w http.ResponseWriter, r *http.Request, out *exportStream, err error) {
	tenant := tenantID(r)
	if err != nil {
		if !out.started {
			if stderr.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "not_found", "no such logbook")
				return
			}
			s.log.Error("export: snapshot read failed", "tenant_id", tenant, "request_id", requestID(r), "err", err)
			s.writeError(w, http.StatusInternalServerError, "internal_error", "export failed")
			return
		}
		s.log.Error("export: aborted mid-stream", "tenant_id", tenant, "request_id", requestID(r), "written", out.count, "err", err)
		return
	}
	// Trailing newline matches the pre-streaming json.Encoder framing.
	if _, err := w.Write([]byte("]}\n")); err != nil {
		s.log.Error("export: aborted mid-stream", "tenant_id", tenant, "request_id", requestID(r), "written", out.count, "err", err)
		return
	}
	s.log.Info("export served", "tenant_id", tenant, "qsos", out.count)
}
