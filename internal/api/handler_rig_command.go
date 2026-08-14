package api

import (
	stderr "errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// maxRigCommandBatch caps how many CAT commands one atomic batch may carry
// (review 2026-07-22 #6). Without it the only bound is the request-body size, so
// a single call could enqueue thousands of hardware writes as one CAT line. Real
// batches are a handful (e.g. set freq + set mode); 32 is generous headroom.
const maxRigCommandBatch = 32

// rigCommandRequest is the POST /v1/rig/command body (ADR 0026). It accepts
// either a single op — {"op": "...", "value": <scalar>} — or an atomic batch
// — {"commands": [ {op, value}, ... ]} — but not both. value is a JSON scalar
// (number for set_freq, string for set_mode); one generic conversion feeds
// every op, so there is no per-op extraction.
type rigCommandRequest struct {
	Op       string            `json:"op,omitempty"`
	Value    any               `json:"value,omitempty"`
	Commands []rigCommandEntry `json:"commands,omitempty"`
}

type rigCommandEntry struct {
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// handleRigCommand drives the connected rig via the inbound command path
// (ADR 0026). Registered only when the bridge is enabled. A batch is written
// as one CAT line (atomic — nothing interleaves between its commands); the
// resulting state changes are confirmed out-of-band by the rig's AUTO-mode
// push over /v1/rig/events, so a 2xx means "written to the rig", not "the rig
// is now at X" — hence 202 Accepted.
func (s *Server) handleRigCommand(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleRigCommand"

	var req rigCommandRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	cmds, ok := s.buildRigCommands(w, op, req)
	if !ok {
		return
	}

	// A command that moves the rig off frequency must not be written while SM is
	// keyed: switching relays under RF is how amplifiers get damaged. So rather than
	// refusing the operator (which is what the bridge's ErrTxActive did, and which
	// left a band button behaving differently from the VFO for the same intent), we
	// perform the same teardown the dial guard performs — PTT down, session ended,
	// TX disarmed — and only then write. Dogfood 2026-07-27.
	if s.stopTxForRetune != nil && retunesTheRig(cmds) {
		if err := s.stopTxForRetune(); err != nil {
			// Could not confirm the rig is unkeyed. Leaving the operator
			// transmitting where they are beats switching a keyed rig.
			s.writeError(w, http.StatusConflict, "rig_tx_stop_failed",
				"could not stop transmitting; the rig was not retuned", op)
			return
		}
	}

	// The bridge generates an operation-id and logs the durable command outcome
	// (L4); echo the id onto this request's access-log line so the two records join.
	opID, err := s.bridge.SendCommands(r.Context(), cmds)
	if rr, ok := w.(*responseRecorder); ok {
		rr.NoteOpID(opID)
	}
	if err != nil {
		s.writeRigCommandError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// retunesTheRig reports whether a batch contains anything that moves the rig off
// frequency. Deliberately a small allow-list rather than "anything that writes":
// stopping TX is the right answer for a band change and quite wrong for a mode
// change, which would otherwise tear down a working session as a side effect.
func retunesTheRig(cmds []bridge.RigCommand) bool {
	for _, c := range cmds {
		switch c.Op {
		case "set_band", "set_freq":
			return true
		}
	}
	return false
}

// buildRigCommands resolves the request into a command list, writing the
// appropriate 400 and returning ok=false on a malformed body. It accepts a
// single {op,value} or a {commands:[...]} batch — rejecting both-at-once as
// ambiguous — and renders each scalar value to the string EncodeCommand wants.
func (s *Server) buildRigCommands(w http.ResponseWriter, op errors.Op, req rigCommandRequest) ([]bridge.RigCommand, bool) {
	var raw []rigCommandEntry
	switch {
	case len(req.Commands) > 0:
		if req.Op != "" || req.Value != nil {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"send either {op,value} or {commands:[...]}, not both", op)
			return nil, false
		}
		raw = req.Commands
	case req.Op != "":
		raw = []rigCommandEntry{{Op: req.Op, Value: req.Value}}
	default:
		s.writeError(w, http.StatusBadRequest, "missing_required_param",
			"op or commands is required", op)
		return nil, false
	}

	if len(raw) > maxRigCommandBatch {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			fmt.Sprintf("a command batch may contain at most %d commands", maxRigCommandBatch), op)
		return nil, false
	}

	cmds := make([]bridge.RigCommand, 0, len(raw))
	for _, e := range raw {
		if e.Op == "" {
			s.writeError(w, http.StatusBadRequest, "missing_required_param",
				"each command needs an op", op)
			return nil, false
		}
		value, ok := scalarToString(e.Value)
		if !ok {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"value must be a string or number", op)
			return nil, false
		}
		cmds = append(cmds, bridge.RigCommand{Op: e.Op, Value: value})
	}
	return cmds, true
}

// writeRigCommandError maps the bridge/cat error taxonomy to HTTP. Unknown and
// not-exposed collapse to one client-visible outcome — "this rig doesn't offer
// that command" — so the path never reveals which internal commands exist.
// ErrUnmappedValue is a bad argument (e.g. an unsupported mode); the value_map
// gate gives set_mode this for free. ErrRigNotConnected is transient (rig off
// / reconnecting); anything else is an internal failure.
func (s *Server) writeRigCommandError(w http.ResponseWriter, op errors.Op, err error) {
	switch {
	case stderr.Is(err, cat.ErrUnknownCommand), stderr.Is(err, cat.ErrCommandNotExposed):
		s.writeError(w, http.StatusBadRequest, "rig_unsupported_command",
			"rig does not support a requested command", op)
	case stderr.Is(err, cat.ErrUnmappedValue), stderr.Is(err, cat.ErrMissingValue),
		stderr.Is(err, cat.ErrInvalidPaddedValue):
		s.writeError(w, http.StatusBadRequest, "rig_invalid_value",
			"a command value is missing or not valid for the rig", op)
	case stderr.Is(err, bridge.ErrRigNotConnected):
		s.writeError(w, http.StatusServiceUnavailable, "rig_not_connected",
			"no rig is currently connected", op)
	case stderr.Is(err, bridge.ErrRigIdentityUnverified):
		s.writeError(w, http.StatusConflict, "rig_identity_unverified",
			"connected rig's identity is unverified; check the configured driver matches the rig", op)
	case stderr.Is(err, bridge.ErrTxActive):
		// A tune carrier or FT8 TX owns the rig; a command can't write while it's
		// transmitting. 409: conflicts with the current state, retry once TX ends.
		s.writeError(w, http.StatusConflict, "rig_tx_active",
			"the rig is transmitting (tune or FT8); try again once transmission ends", op)
	case stderr.Is(err, bridge.ErrCommandRejected):
		// The command reached the rig and it declined (e.g. out-of-band freq).
		// 422: well-formed request, but the target refused the operation.
		s.writeError(w, http.StatusUnprocessableEntity, "rig_command_rejected",
			"the rig refused the command (e.g. value out of range)", op)
	case stderr.Is(err, bridge.ErrCommandNoAck):
		// No ACK within the wait window — the rig may be wedged or mid-reconnect.
		// 504: an upstream (the rig) didn't respond in time.
		s.writeError(w, http.StatusGatewayTimeout, "rig_command_no_ack",
			"the rig did not acknowledge the command in time", op)
	default:
		s.writeServerError(w, op, err, "rig_command_failed", "failed to send rig command")
	}
}

// scalarToString renders a decoded JSON scalar to the string EncodeCommand
// expects. A missing/null value becomes "" — valueless ops (band_up) accept it,
// while value-bearing ops reject it downstream (ErrMissingValue). JSON numbers
// decode to float64; rig frequencies are integers well inside float64's exact
// range, so the conversion is lossless and emits no decimal point. Objects,
// arrays, and bool are rejected — no current op needs them.
func scalarToString(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	default:
		return "", false
	}
}
