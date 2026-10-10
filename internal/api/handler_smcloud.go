package api

import (
	"context"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/api/httpkit"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// SmcloudReconcileEntry is one enabled SM Cloud binding of the running
// generation (W-0021 5F.4, ruling F6). Run is nil when the binding has no
// reconciler; Fault then says why, in a fixed text. A func seam so the api
// package doesn't import the forwarding tree.
type SmcloudReconcileEntry struct {
	ForwarderName string
	LogbookUUID   string
	Run           func(ctx context.Context) (any, error)
	Fault         string
}

// SetSmcloudReconcile wires the generation's reconcile set, built once from
// the start snapshot. Call before ListenAndServe (cmd/smd startup); an empty
// set leaves POST /v1/smcloud/reconcile answering 503.
func (s *Server) SetSmcloudReconcile(entries []SmcloudReconcileEntry) { s.smcloudRec = entries }

// reconcileRunTimeout bounds the on-demand aggregate: every binding runs
// concurrently under ONE deadline fixed when the handler starts (ruling G1).
// Kept under the server's write timeout so a slow cloud yields an answer
// rather than a severed response. A var so tests can shorten it.
var reconcileRunTimeout = 25 * time.Second

// The fixed per-binding texts of a failed run (ruling G2): the entry names
// the binding and the cause goes to the log, never to the wire.
const (
	reconcileRunFailed    = "the reconcile pass failed; the daemon log has the cause"
	reconcileRunTimedOut  = "timed out after 25 s"
	reconcileRunCancelled = "the request was cancelled before the pass finished"
)

type smcloudReconcileResult struct {
	ForwarderName string `json:"forwarder_name"`
	LogbookUUID   string `json:"logbook_uuid"`
	Summary       any    `json:"summary,omitempty"`
	Error         string `json:"error,omitempty"`
}

// handleSmcloudReconcile serves POST /v1/smcloud/reconcile — the on-demand
// half of ADR 0040 S4 ("back up / check now"), one pass per enabled SM Cloud
// binding. 200 with every binding's result when at least one reconciler runs,
// failures included; 503 smcloud_unavailable, with the results beside the
// envelope, when none does (Q6, ruling G3). The handler returns only after
// every run it launched has exited: a timeout cannot stop a Go function, and
// an abandoned run would outlive the drain that closes the log database.
func (s *Server) handleSmcloudReconcile(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleSmcloudReconcile"

	entries := s.smcloudRec
	results := make([]smcloudReconcileResult, len(entries))
	runnable := 0
	for i, e := range entries {
		results[i] = smcloudReconcileResult{ForwarderName: e.ForwarderName, LogbookUUID: e.LogbookUUID, Error: e.Fault}
		if e.Run != nil {
			results[i].Error = ""
			runnable++
		}
	}
	if runnable == 0 {
		const msg = "no SM Cloud reconciler is running"
		if n, ok := w.(httpkit.ErrorNoter); ok {
			n.NoteError("smcloud_unavailable", msg, string(op))
		}
		s.writeJSON(w, http.StatusServiceUnavailable, struct {
			httpkit.ErrorResponse
			Results []smcloudReconcileResult `json:"results"`
		}{httpkit.ErrorResponse{Code: "smcloud_unavailable", Message: msg, Op: op}, results})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), reconcileRunTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for i, e := range entries {
		if e.Run == nil {
			continue
		}
		runCtx, runCancel := context.WithCancel(ctx)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer runCancel()
			// recoverPanic guards only the request's goroutine; a panicking
			// run is recovered here, as there, and becomes the fixed failure
			// while its peers keep their results.
			defer func() {
				if pv := recover(); pv != nil {
					s.logger.ErrorWith().Str("forwarder", e.ForwarderName).Interface("panic", pv).
						Str("stack", string(debug.Stack())).Msg("smcloud reconcile: on-demand pass panicked")
					results[i] = smcloudReconcileResult{ForwarderName: e.ForwarderName, LogbookUUID: e.LogbookUUID, Error: reconcileRunFailed}
				}
			}()
			sum, err := e.Run(runCtx)
			if err == nil {
				results[i].Summary = sum
				return
			}
			s.logger.WarnWith().Err(err).Str("forwarder", e.ForwarderName).Msg("smcloud reconcile: on-demand pass failed")
			switch {
			case r.Context().Err() != nil:
				results[i].Error = reconcileRunCancelled
			case ctx.Err() == context.DeadlineExceeded:
				results[i].Error = reconcileRunTimedOut
			default:
				results[i].Error = reconcileRunFailed
			}
		}()
	}
	wg.Wait()
	s.writeJSON(w, http.StatusOK, struct {
		Results []smcloudReconcileResult `json:"results"`
	}{results})
}
