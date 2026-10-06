package server

import (
	"encoding/json"
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"
)

type ErrorCode string

const (
	ErrCodeUnauthorized   ErrorCode = "AUTH_001"
	ErrCodeInvalidMessage ErrorCode = "MSG_001"
	ErrCodeInternal       ErrorCode = "INT_001"
	ErrCodeNotFound       ErrorCode = "NF_001"
)

type APIError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"error"`
}

func (e APIError) Error() string { return e.Message }

func writeAPIError(w http.ResponseWriter, logger *zap.Logger, status int, code ErrorCode, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(APIError{Code: code, Message: msg}); err != nil {
		logger.Error("failed to write API error response",
			zap.Error(err),
			zap.Int("status", status),
			zap.String("code", string(code)),
		)
	}
}

func RecoveryMiddleware(logger *zap.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("recovered from panic",
					zap.Any("error", rec),
					zap.String("stack", string(debug.Stack())),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
				)
				writeAPIError(w, logger, http.StatusInternalServerError, ErrCodeInternal, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
