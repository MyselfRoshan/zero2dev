// Package apperr defines the error type that carries an HTTP status and an
// API error code (see docs/API.md "Errors"). Domain packages return these so
// the API layer can render them without knowing about HTTP details.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is an API-visible error.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func mk(status int, code, f string, a ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(f, a...)}
}

func BadRequest(f string, a ...any) *Error { return mk(http.StatusBadRequest, "bad_request", f, a...) }
func Unauthorized(f string, a ...any) *Error {
	return mk(http.StatusUnauthorized, "unauthorized", f, a...)
}
func NotFound(f string, a ...any) *Error { return mk(http.StatusNotFound, "not_found", f, a...) }
func Conflict(f string, a ...any) *Error { return mk(http.StatusConflict, "conflict", f, a...) }
func Internal(f string, a ...any) *Error {
	return mk(http.StatusInternalServerError, "internal", f, a...)
}

// From converts any error into an *Error (unknown errors become 500 internal).
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal("%v", err)
}
