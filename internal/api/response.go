package api

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the JSON error envelope per api.md Section 4.6.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Op      string `json:"op,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message, op string) {
	writeJSON(w, status, ErrorResponse{
		Code:    code,
		Message: message,
		Op:      op,
	})
}
