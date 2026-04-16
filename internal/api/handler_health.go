package api

import "net/http"

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable", "database is not reachable", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
