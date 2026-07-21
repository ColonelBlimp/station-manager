package api

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/buildinfo"
)

// processInstance is a marker minted ONCE per process (the daemon's start time in
// base-36), exposed in /v1/version. A restart mints a new one, so a client can
// tell the freshly-respawned daemon from the still-shutting-down old process even
// if a poll lands on the old one over a reused keep-alive connection — it waits for
// the instance to CHANGE, not merely for any 200 (codex 85997b79 P2).
var processInstance = strconv.FormatInt(time.Now().UnixNano(), 36)

// handleVersion returns daemon-build, Go-runtime, and schema-migration
// version info. Diagnostic endpoint: lets operators confirm which
// daemon build is running and which DB migration level is applied.
//
// Shape:
//
//	{
//	  "daemon":  "1.2.3" | "dev",
//	  "env":     "dev" | "release",   // build provenance (source vs packaged)
//	  "go":      "go1.24.0",
//	  "schema":  { "version": 1, "dirty": false }
//	}
//
// If the schema query fails, the schema field is omitted but the
// endpoint still responds 200 — failing to report schema version
// shouldn't mask the rest of the info.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	type schemaInfo struct {
		Version uint64 `json:"version"`
		Dirty   bool   `json:"dirty"`
	}
	type versionResponse struct {
		Daemon   string      `json:"daemon"`
		Env      string      `json:"env"`
		Go       string      `json:"go"`
		Instance string      `json:"instance"`
		Schema   *schemaInfo `json:"schema,omitempty"`
	}

	resp := versionResponse{
		Daemon:   s.daemonVersion,
		Env:      buildinfo.Env,
		Go:       runtime.Version(),
		Instance: processInstance,
	}

	if ver, dirty, err := s.db.SchemaVersionWithContext(r.Context()); err == nil {
		resp.Schema = &schemaInfo{Version: ver, Dirty: dirty}
	} else {
		s.logger.WarnWith().Err(err).Msg("failed to read schema version")
	}

	s.writeJSON(w, http.StatusOK, resp)
}
