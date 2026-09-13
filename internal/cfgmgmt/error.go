package cfgmgmt

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
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

// ErrServiceTypeNameTaken is returned when renaming a type to a name that
// already exists.
var ErrServiceTypeNameTaken = statusErr(409, "service type name already exists")

var ErrConnectionTypeInUse = statusErr(409, "connection type in use")
var ErrConnectionTypeNameTaken = statusErr(409, "connection type name already exists")
var ErrConnectionTypeNotFound = statusErr(404, "connection type not found")

func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint") ||
		strings.Contains(s, "duplicate key value")
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

func uniqueConflict(err error, msg string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return statusErr(409, msg)
	}
	s := err.Error()
	if strings.Contains(s, "UNIQUE constraint failed") ||
		strings.Contains(s, "duplicate key") ||
		strings.Contains(s, "UNIQUE constraint") {
		return statusErr(409, msg)
	}
	return err
}
