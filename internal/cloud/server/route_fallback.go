package server

import "net/http"

// jsonRouteErrors wraps the SM Cloud API mux so an unmatched route — an unknown path or a
// method mismatch — returns the JSON error envelope instead of ServeMux's plain-text 404 /
// 405, matching the daemon (AW-4). The cloud mux has no SPA catch-all, so ServeMux's
// built-in classification (and the 405's Allow header) is already accurate; this only
// replaces the body. A matched route is served untouched.
func (s *Server) jsonRouteErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r) // matched: serve normally so ServeMux binds path wildcards
			return
		}
		// No route matched. h is ServeMux's built-in 404 (unknown path) or 405 (method
		// mismatch, with an accurate Allow). Capture which — and the Allow — without
		// letting its plain-text body through, then re-emit as the envelope.
		cap := &statusCapture{header: make(http.Header)}
		h.ServeHTTP(cap, r)
		if cap.status == http.StatusMethodNotAllowed {
			if allow := cap.header.Get("Allow"); allow != "" {
				w.Header().Set("Allow", allow)
			}
			s.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this route")
			return
		}
		s.writeError(w, http.StatusNotFound, "not_found", "no such API route")
	})
}

// statusCapture records a handler's status code and headers while discarding its body, so
// jsonRouteErrors can read ServeMux's built-in 404/405 (and the 405's Allow) without
// emitting the plain-text body those fallbacks write.
type statusCapture struct {
	header http.Header
	status int
}

func (c *statusCapture) Header() http.Header { return c.header }

func (c *statusCapture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *statusCapture) Write(b []byte) (int, error) { return len(b), nil }
