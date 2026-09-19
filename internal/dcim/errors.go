package dcim

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	ReasonOverlap           = "overlap"
	ReasonOutOfBounds       = "out_of_bounds"
	ReasonImportedReadOnly  = "imported_read_only"
	ReasonStaleVersion      = "stale_version"
	ReasonUnknownDimensions = "unknown_dimensions"
	ReasonNotFound          = "not_found"
	ReasonInvalid           = "invalid"
	ReasonConflict          = "conflict"
	ReasonForbidden         = "forbidden"
)

// Error is a domain error with an HTTP status and machine-readable reason.
type Error struct {
	Status  int
	Reason  string
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func errf(status int, reason, format string, args ...any) *Error {
	return &Error{Status: status, Reason: reason, Message: fmt.Sprintf(format, args...)}
}

func Is(err error, reason string) bool {
	var e *Error
	return errors.As(err, &e) && e.Reason == reason
}

func StatusOf(err error) int {
	var e *Error
	if errors.As(err, &e) && e.Status != 0 {
		return e.Status
	}
	return http.StatusInternalServerError
}

func ReasonOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}
