package api

import (
	"encoding/json"
	"net/http"
)

// apiError is the only response envelope: errors are wrapped, successes
// are returned raw. The SPA switches on Error.Code (a stable machine
// string); Error.Message is human text.
type apiError struct {
	Error apiErrorBody `json:"error"`
}

type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON encodes v as the response body with the given HTTP status and
// the application/json content type. Headers other than Content-Type
// (e.g. Cache-Control) must be set on w BEFORE calling this, since it
// writes the status line.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeAPIError emits {"error":{"code","message"}} with the given HTTP
// status. code is a stable machine string (e.g. "unauthorized",
// "not_found", "bad_request", "internal").
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Error: apiErrorBody{Code: code, Message: message}})
}
