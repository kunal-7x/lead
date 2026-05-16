package errors

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Code mirrors gRPC status codes for typed domain errors.
type Code = codes.Code

const (
	NotFound          = codes.NotFound
	AlreadyExists     = codes.AlreadyExists
	InvalidArgument   = codes.InvalidArgument
	PermissionDenied  = codes.PermissionDenied
	Unauthenticated   = codes.Unauthenticated
	Internal          = codes.Internal
	Unavailable       = codes.Unavailable
	ResourceExhausted = codes.ResourceExhausted
)

// DomainError is a typed application error that maps to a gRPC status.
type DomainError struct {
	code    Code
	message string
	cause   error
}

func (e *DomainError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

func (e *DomainError) Unwrap() error { return e.cause }

// New creates a DomainError with the given code and message.
func New(code Code, message string) *DomainError {
	return &DomainError{code: code, message: message}
}

// Wrap wraps an existing error with a code and message.
func Wrap(code Code, message string, cause error) *DomainError {
	return &DomainError{code: code, message: message, cause: cause}
}

// ToGRPC converts any error to a gRPC status error.
func ToGRPC(err error) error {
	if err == nil {
		return nil
	}
	var de *DomainError
	if errors.As(err, &de) {
		return status.Error(de.code, de.message)
	}
	return status.Error(codes.Internal, err.Error())
}

// Is satisfies errors.Is for code-based matching.
func Is(err error, code Code) bool {
	var de *DomainError
	if errors.As(err, &de) {
		return de.code == code
	}
	return false
}
