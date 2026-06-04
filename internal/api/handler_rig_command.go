package api

import (
	stderr "errors"
	"net/http"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// rigCommandRequest is the POST /v1/rig/command body (ADR 0026): a semantic
// op naming a rigdef command, and a single scalar value (a number for
// set_freq, a string for set_mode). Value is left untyped so one generic
// conversion feeds every op — no per-op param extraction in the handler.
type rigCommandRequest struct {
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// handleRigCommand drives the connected rig via the inbound command path
// (ADR 0026). Registered only when the bridge is enabled. The resulting state
// change is confirmed out-of-band by the rig's AUTO-mode push over
// /v1/rig/events, so a 2xx here means "written to the rig", not "the rig is
// now at X" — hence 202 Accepted rather than 200.
func (s *Server) handleRigCommand(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleRigCommand"

	var req rigCommandRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	if req.Op == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_param", "op is required", op)
		return
	}
	value, ok := scalarToString(req.Value)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"value must be a string or number", op)
		return
	}

	if err := s.bridge.SendCommand(r.Context(), req.Op, value); err != nil {
		s.writeRigCommandError(w, op, req.Op, value, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// writeRigCommandError maps the bridge/cat error taxonomy to HTTP. Unknown and
// not-exposed collapse to one client-visible outcome — "this rig doesn't offer
// that op" — so the command path never reveals which internal commands exist.
// ErrUnmappedValue is a bad argument (e.g. an unsupported mode); the value_map
// gate gives set_mode this validation for free. ErrRigNotConnected is
// transient (rig off / reconnecting); anything else is an internal failure.
func (s *Server) writeRigCommandError(w http.ResponseWriter, op errors.Op, rigOp, value string, err error) {
	switch {
	case stderr.Is(err, cat.ErrUnknownCommand), stderr.Is(err, cat.ErrCommandNotExposed):
		s.writeError(w, http.StatusBadRequest, "rig_unsupported_command",
			"rig does not support op "+strconv.Quote(rigOp), op)
	case stderr.Is(err, cat.ErrUnmappedValue):
		s.writeError(w, http.StatusBadRequest, "rig_invalid_value",
			"value "+strconv.Quote(value)+" is not valid for op "+strconv.Quote(rigOp), op)
	case stderr.Is(err, bridge.ErrRigNotConnected):
		s.writeError(w, http.StatusServiceUnavailable, "rig_not_connected",
			"no rig is currently connected", op)
	default:
		s.writeServerError(w, op, err, "rig_command_failed", "failed to send rig command")
	}
}

// scalarToString renders a decoded JSON scalar to the string EncodeCommand
// expects. JSON numbers decode to float64; rig frequencies are integers well
// inside float64's exact range, so the conversion is lossless and emits no
// decimal point. Objects, arrays, null, and bool are rejected — no current op
// needs them.
func scalarToString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	default:
		return "", false
	}
}
