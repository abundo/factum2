package cfgmgmt

import (
	"errors"
	"fmt"
)

// StatusError is an application-level failure with an HTTP-ish status so
// handlers don't have to string-match error text.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string { return e.Message }

func statusErr(status int, msg string) *StatusError {
	return &StatusError{Status: status, Message: msg}
}

func statusErrf(status int, format string, args ...any) *StatusError {
	return &StatusError{Status: status, Message: fmt.Sprintf(format, args...)}
}

func AsStatusError(err error) *StatusError {
	var se *StatusError
	if errors.As(err, &se) {
		return se
	}
	return nil
}
