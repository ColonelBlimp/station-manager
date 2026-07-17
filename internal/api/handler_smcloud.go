package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// SmcloudReconcileFunc runs one SM Cloud reconcile pass and returns its
// JSON-serialisable summary. A func seam rather than the concrete
// *smcloud.Reconciler so the api package doesn't import the forwarding tree;
// cmd/smd injects it via SetSmcloudReconcile when an enabled smcloud
// forwarder exists.
type SmcloudReconcileFunc func(ctx context.Context) (any, error)

// SetSmcloudReconcile wires the on-demand reconcile action. Call before
// ListenAndServe (cmd/smd startup); nil leaves POST /v1/smcloud/reconcile
// answering 503.
func (s *Server) SetSmcloudReconcile(fn SmcloudReconcileFunc) { s.smcloudRec = fn }

// reconcileRunTimeout bounds the on-demand run: 2–3 small cloud GETs plus
// local reads. Kept under the server's write timeout so a slow cloud yields
// a clean 500 rather than a severed response.
const reconcileRunTimeout = 25 * time.Second

// handleSmcloudReconcile serves POST /v1/smcloud/reconcile — the on-demand
// half of ADR 0040 S4 ("back up / check now"). Runs one reconcile pass
// synchronously and returns its summary; the periodic loop is unaffected.
// 503 when no smcloud forwarder is configured+enabled (no reconciler wired).
func (s *Server) handleSmcloudReconcile(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleSmcloudReconcile"

	rec := s.smcloudRec
	if rec == nil {
		s.writeError(w, http.StatusServiceUnavailable, "smcloud_unavailable",
			"no enabled smcloud forwarder is configured", op)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), reconcileRunTimeout)
	defer cancel()
	sum, err := rec(ctx)
	if err != nil {
		s.writeServerError(w, op, err, "reconcile_failed", "smcloud reconcile run failed")
		return
	}
	s.writeJSON(w, http.StatusOK, sum)
}
