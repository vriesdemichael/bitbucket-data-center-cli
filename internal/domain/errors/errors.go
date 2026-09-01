package errors

import (
	"errors"
	"fmt"
	"strings"
)

type Kind string

const (
	KindAuthentication Kind = "authentication"
	KindAuthorization  Kind = "authorization"
	KindValidation     Kind = "validation"
	KindNotFound       Kind = "not_found"
	KindConflict       Kind = "conflict"
	KindTransient      Kind = "transient"
	KindPermanent      Kind = "permanent"
	KindNotImplemented Kind = "not_implemented"
	KindInternal       Kind = "internal"
	// KindCancelled is work the operator stopped, or a deadline that expired,
	// before it finished.
	//
	// It is deliberately not transient. Transient is documented as "retry
	// later", and a caller that retries a Ctrl-C re-runs the very thing
	// somebody just interrupted -- for `bb bulk apply` that means replaying
	// mutations across every repository in the plan.
	KindCancelled Kind = "cancelled"
)

type AppError struct {
	Kind    Kind
	Message string
	Cause   error
}

func New(kind Kind, message string, cause error) *AppError {
	return &AppError{
		Kind:    kind,
		Message: message,
		Cause:   cause,
	}
}

func (appError *AppError) Error() string {
	if appError.Cause == nil {
		return fmt.Sprintf("%s: %s", appError.Kind, appError.Message)
	}

	return fmt.Sprintf("%s: %s (%v)", appError.Kind, appError.Message, appError.Cause)
}

func (appError *AppError) Unwrap() error {
	return appError.Cause
}

func IsKind(err error, kind Kind) bool {
	var appError *AppError
	if errors.As(err, &appError) {
		return appError.Kind == kind
	}
	return false
}

func KindOf(err error) Kind {
	var appError *AppError
	if errors.As(err, &appError) {
		return appError.Kind
	}

	if err == nil {
		return ""
	}

	return KindInternal
}

// Kinds returns every kind in the taxonomy, in declaration order.
//
// The JSON error envelope schema enumerates these, so a kind added here without
// a schema update would publish a contract the CLI can violate. A test asserts
// the two stay in step.
func Kinds() []Kind {
	return []Kind{
		KindAuthentication,
		KindAuthorization,
		KindValidation,
		KindNotFound,
		KindConflict,
		KindTransient,
		KindPermanent,
		KindNotImplemented,
		KindCancelled,
		KindInternal,
	}
}

// MessageOf returns the human-readable message with the leading kind prefix
// that Error() adds removed.
//
// Machine consumers get the kind as its own field, so repeating it inside the
// message is noise. The prefix is only stripped when it is literally at the
// front, which leaves wrapped errors carrying extra context intact.
func MessageOf(err error) string {
	if err == nil {
		return ""
	}

	message := err.Error()
	if kind := KindOf(err); kind != "" {
		message = strings.TrimPrefix(message, string(kind)+": ")
	}

	return message
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}

	var appError *AppError
	if errors.As(err, &appError) {
		switch appError.Kind {
		case KindValidation:
			return 2
		case KindAuthentication, KindAuthorization:
			return 3
		case KindNotFound:
			return 4
		case KindConflict:
			return 5
		case KindTransient:
			return 10
		case KindNotImplemented:
			return 11
		case KindCancelled:
			return 12
		default:
			return 1
		}
	}

	return 1
}
