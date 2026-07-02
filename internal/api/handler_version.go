package api

import (
	"net/http"
	"runtime"

	"github.com/ColonelBlimp/station-manager/internal/buildinfo"
)

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
		Daemon string      `json:"daemon"`
		Env    string      `json:"env"`
		Go     string      `json:"go"`
		Schema *schemaInfo `json:"schema,omitempty"`
	}

	resp := versionResponse{
		Daemon: s.daemonVersion,
		Env:    buildinfo.Env,
		Go:     runtime.Version(),
	}

	if ver, dirty, err := s.db.SchemaVersionWithContext(r.Context()); err == nil {
		resp.Schema = &schemaInfo{Version: ver, Dirty: dirty}
	} else {
		s.logger.WarnWith().Err(err).Msg("failed to read schema version")
	}

	s.writeJSON(w, http.StatusOK, resp)
}
