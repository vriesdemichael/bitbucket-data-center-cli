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
	// Details carries machine-readable handles that belong with the failure --
	// an identifier the caller needs to act on it, not a restatement of the
	// message.
	//
	// It exists because the alternative is putting the handle in the message
	// and telling callers to scrape it back out, which is the sentence-parsing
	// the machine contract exists to end (#474). ADR-046 forbids data beside
	// error; this sits inside the error object, so which key is present still
	// decides success from failure.
	Details map[string]string
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

// WithDetail returns a copy of err carrying one machine-readable detail.
//
// The copy is the point. Writing into the error's own map would contaminate
// every other holder of it -- a package-level sentinel, a cached value, an
// error already handed to another goroutine -- and two calls would race on the
// map. Nothing shares an *AppError today, which is exactly when a helper that
// reads as functional and mutates is cheapest to fix.
//
// Only *AppError can carry details, because only a classified error reaches the
// failure envelope with a kind. Anything else is returned unchanged rather than
// wrapped: wrapping here would silently reclassify it as internal.
//
// It is the error itself that must be the *AppError, not something it wraps.
// Reaching inside a wrapper would mean returning the inner error and dropping
// the context the wrapping added, so attach the detail to the error you are
// about to return, before anything wraps it.
func WithDetail(err error, key, value string) error {
	appError, ok := err.(*AppError)
	if !ok || appError == nil {
		return err
	}
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return err
	}

	details := make(map[string]string, len(appError.Details)+1)
	for existingKey, existingValue := range appError.Details {
		details[existingKey] = existingValue
	}
	details[key] = value

	return &AppError{
		Kind:    appError.Kind,
		Message: appError.Message,
		Cause:   appError.Cause,
		Details: details,
	}
}

// DetailsOf returns the machine-readable details attached to err, if any.
func DetailsOf(err error) map[string]string {
	var appError *AppError
	if !errors.As(err, &appError) || len(appError.Details) == 0 {
		return nil
	}

	details := make(map[string]string, len(appError.Details))
	for key, value := range appError.Details {
		details[key] = value
	}

	return details
}
