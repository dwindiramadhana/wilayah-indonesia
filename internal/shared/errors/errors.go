package errors

import (
	stdErrors "errors"
	"fmt"
)

// Code enumerates well-known application error codes shared across layers.
type Code string

const (
	// CodeInvalidInput signals that the caller provided malformed or incomplete data.
	CodeInvalidInput Code = "INVALID_INPUT"
	// CodeNotFound indicates that a requested resource does not exist.
	CodeNotFound Code = "NOT_FOUND"
	// CodeDatabaseFailure captures unexpected datastore errors.
	CodeDatabaseFailure Code = "DATABASE_FAILURE"
	// CodeConfiguration signals that configuration is missing or invalid.
	CodeConfiguration Code = "CONFIGURATION_ERROR"
	// CodeNotImplemented represents functionality that has not been wired yet.
	CodeNotImplemented Code = "NOT_IMPLEMENTED"
)

// ErrNotImplemented is a sentinel error returned by scaffolding placeholders.
var ErrNotImplemented = stdErrors.New("not implemented")

// AppError standardises how errors propagate between architecture layers.
type AppError struct {
	Code    Code
	Message string
	Err     error
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("application error: %s", e.Code)
}

// Unwrap exposes the underlying error for errors.Is / errors.As checks.
func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// New creates a new application error with the provided code and message.
func New(code Code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// Wrap decorates an existing error with an AppError, preserving the code.
func Wrap(code Code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

// Is reports whether err is an AppError carrying the target code.
func Is(err error, code Code) bool {
	if err == nil {
		return false
	}
	var appErr *AppError
	if stdErrors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}
