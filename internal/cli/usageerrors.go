package cli

import (
	"errors"
	"strings"

	"github.com/spf13/pflag"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// cobraUsageErrorMarkers are the message fragments Cobra uses for the usage
// errors it raises itself.
//
// Cobra returns plain fmt.Errorf values for these, with no sentinel or type to
// match on, so recognising them means matching text. That coupling is pinned by
// TestClassifyUsageErrorMatchesCobrasRealMessages, which drives the real
// conditions through a command tree and fails if a Cobra upgrade changes the
// wording — a silent reclassification back to internal would be far worse than
// a failing build.
//
// pflag's errors need none of this: since v1.0.10 they are typed, and are
// matched with errors.As below.
var cobraUsageErrorMarkers = []string{
	"unknown command",
	"unknown shorthand flag",
	"arg(s), received",
	"arg(s), only received",
	"invalid argument",
}

// ClassifyUsageError maps an error caused by a malformed invocation onto the
// validation kind.
//
// An unknown flag is the caller's mistake, not a CLI defect, but Cobra and
// pflag raise these outside the taxonomy, so KindOf fell through to internal
// and the exit code to 1. A consumer branching on kind would then retry or
// escalate a failure it should have fixed in its own invocation — which is the
// opposite of what ADR-011 exists for.
//
// Errors already carrying a kind are returned untouched, so a command's own
// classification always wins.
func ClassifyUsageError(err error) error {
	if err == nil {
		return nil
	}

	var appError *apperrors.AppError
	if errors.As(err, &appError) {
		return err
	}

	if !isUsageError(err) {
		return err
	}

	return apperrors.New(apperrors.KindValidation, err.Error(), nil)
}

func isUsageError(err error) bool {
	var (
		notExist      *pflag.NotExistError
		valueRequired *pflag.ValueRequiredError
		invalidValue  *pflag.InvalidValueError
		invalidSyntax *pflag.InvalidSyntaxError
	)

	if errors.As(err, &notExist) ||
		errors.As(err, &valueRequired) ||
		errors.As(err, &invalidValue) ||
		errors.As(err, &invalidSyntax) {
		return true
	}

	message := err.Error()
	for _, marker := range cobraUsageErrorMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}
