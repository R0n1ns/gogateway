package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/R0n1ns/gogateway/internal/policy"
)

// APIError задаёт стабильный machine-readable contract ошибок data plane.
type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	PolicyID  string `json:"policy_id,omitempty"`
	Status    int    `json:"-"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }
func NewError(code string, status int, message, requestID string) *APIError {
	return &APIError{Code: code, Status: status, Message: message, RequestID: requestID}
}
func NewPolicyError(decision policy.Decision, requestID string) *APIError {
	code, message := "policy_denied", "запрос запрещён политикой"
	if decision.Status == http.StatusUnauthorized {
		code, message = "authentication_required", "требуется аутентификация"
	}
	return &APIError{Code: code, Status: decision.Status, Message: message, RequestID: requestID, PolicyID: decision.PolicyID}
}
func WriteError(w http.ResponseWriter, e *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}
func errorsIsTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && network.Timeout()
}

type responseRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.written {
		return
	}
	r.written = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
func (r *responseRecorder) Write(data []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(data)
}
func (r *responseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
