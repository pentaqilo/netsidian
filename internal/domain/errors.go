package domain

import "fmt"

// ErrorCode enumerates the stable API error codes.
type ErrorCode string

const (
	ErrCodeInvalid         ErrorCode = "invalid"
	ErrCodeUnauthenticated ErrorCode = "unauthenticated"
	ErrCodeForbidden       ErrorCode = "forbidden"
	ErrCodeNotFound        ErrorCode = "not_found"
	ErrCodeConflict        ErrorCode = "conflict"
	ErrCodePrecondition    ErrorCode = "precondition_failed"
	ErrCodePreconditionReq ErrorCode = "precondition_required"
	ErrCodeTooLarge        ErrorCode = "too_large"
	ErrCodeUnavailable     ErrorCode = "unavailable"
	ErrCodeRateLimited     ErrorCode = "rate_limited"
	ErrCodeUnsupported     ErrorCode = "unsupported"
)

// Error is the typed domain error transported to both HTTP and native
// adapters. CurrentRevision is safe to expose only when the caller can
// read the document.
type Error struct {
	Code            ErrorCode
	Status          int
	Message         string
	Retryable       bool
	CurrentRevision string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewError(code ErrorCode, status int, msg string) *Error {
	return &Error{Code: code, Status: status, Message: msg}
}

func Invalid(msg string) *Error {
	return &Error{Code: ErrCodeInvalid, Status: 400, Message: msg}
}

func Unauthenticated(msg string) *Error {
	return &Error{Code: ErrCodeUnauthenticated, Status: 401, Message: msg}
}

func Forbidden(msg string) *Error {
	return &Error{Code: ErrCodeForbidden, Status: 403, Message: msg}
}

func NotFound(msg string) *Error {
	return &Error{Code: ErrCodeNotFound, Status: 404, Message: msg}
}

func Conflict(msg string) *Error {
	return &Error{Code: ErrCodeConflict, Status: 409, Message: msg, Retryable: false}
}

func PreconditionFailed(currentRevision string) *Error {
	return &Error{
		Code: ErrCodePrecondition, Status: 412,
		Message: "expected revision no longer current", Retryable: false,
		CurrentRevision: currentRevision,
	}
}

func PreconditionRequired(msg string) *Error {
	return &Error{Code: ErrCodePreconditionReq, Status: 428, Message: msg}
}

func TooLarge(msg string) *Error {
	return &Error{Code: ErrCodeTooLarge, Status: 413, Message: msg}
}

func Unavailable(msg string) *Error {
	return &Error{Code: ErrCodeUnavailable, Status: 503, Message: msg, Retryable: true}
}

func Unsupported(msg string) *Error {
	return &Error{Code: ErrCodeUnsupported, Status: 501, Message: msg}
}

func RateLimited(msg string) *Error {
	return &Error{Code: ErrCodeRateLimited, Status: 429, Message: msg, Retryable: true}
}
