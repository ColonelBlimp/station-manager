package api

import (
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// opRouteFallback tags the synthesized 404/405 the API namespace returns for an
// unmatched or method-mismatched /v1/ route, so the access log classifies it.
const opRouteFallback errors.Op = "api.routeFallback"

// apiRouter dispatches the /v1/ API namespace to its own ServeMux (apiMux) and every
// other path to the main mux (SPA root, operator manual, pprof). The split keeps the SPA
// catch-all off the API namespace so apiMux's built-in 404/405 stay accurate; see
// registerRoutes for why that matters.
//
// For an unmatched /v1/ request apiMux would answer with a plain-text 404 (no such path)
// or 405 (method mismatch, carrying an accurate Allow header). apiRouter re-emits that as
// the JSON error envelope every documented endpoint promises — routed through writeError
// so the access log records the code/message/op — while a matched route (including a
// handler that legitimately returns its own JSON 404, e.g. an absent QSO) is served
// untouched. A HEAD request keeps the envelope's status and headers; net/http drops the
// body. Non-/v1/ paths are handed to mux with no interception at all, so the SPA, manual,
// and pprof 404s keep their existing behavior.
func (s *Server) apiRouter(apiMux, mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p != "/v1" && !strings.HasPrefix(p, "/v1/") {
			mux.ServeHTTP(w, r)
			return
		}

		h, pattern := apiMux.Handler(r)
		if pattern != "" {
			// A route matched (or method-matched): serve it normally so ServeMux sets
			// the path wildcards. Re-dispatching costs one extra table lookup; the
			// handler returned by Handler() would not have the {uuid}/{id} values bound.
			apiMux.ServeHTTP(w, r)
			return
		}

		// No /v1/ route matched. h is apiMux's built-in fallback: capture whether it is a
		// 404 or a 405 (and the 405's Allow header) without letting its plain-text body
		// through, then re-emit as the JSON envelope.
		cap := &statusCapture{header: make(http.Header)}
		h.ServeHTTP(cap, r)
		if cap.status == http.StatusMethodNotAllowed {
			if allow := cap.header.Get("Allow"); allow != "" {
				w.Header().Set("Allow", allow)
			}
			s.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this route", opRouteFallback)
			return
		}
		s.writeError(w, http.StatusNotFound, "not_found", "no such API route", opRouteFallback)
	})
}

// statusCapture records a handler's status code and headers while discarding its body. It
// lets apiRouter read ServeMux's built-in 404/405 (and the 405's Allow header) without
// emitting the plain-text body those fallbacks would write.
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
