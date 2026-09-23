package prcmd

import (
	"fmt"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// Exit statuses of bb pr checks, gh pr checks' own (ADR-091).
const (
	// checksFailedExit is a build that failed. It wins over one still running:
	// a failure is final, and waiting would not change the answer.
	checksFailedExit = 1
	// checksPendingExit is a build still in progress, or one that reported no
	// result, with none failed.
	checksPendingExit = 8
)

// buildsExitStatus is what the builds of a pull request make of the exit
// status, the way gh pr checks decides it: failed first, then anything not
// finished, and a cancelled build counts as neither.
//
// A state other than the ones Bitbucket names today counts as not finished,
// as gh counts a state it does not know: a gate that passes on a state nobody
// has seen is the one that should not.
func buildsExitStatus(statuses []pullrequestservice.BuildStatus) error {
	failed, pending := 0, 0
	for _, status := range statuses {
		switch strings.ToUpper(strings.TrimSpace(status.State)) {
		case "SUCCESSFUL", "CANCELLED":
		case "FAILED":
			failed++
		default:
			pending++
		}
	}

	switch {
	case failed > 0:
		return &apperrors.StateExit{
			Code:   checksFailedExit,
			Reason: fmt.Sprintf("%d %s failed", failed, plural(failed, "build", "builds")),
		}
	case pending > 0:
		return &apperrors.StateExit{
			Code:   checksPendingExit,
			Reason: fmt.Sprintf("%d %s in progress or without a result", pending, plural(pending, "build is", "builds are")),
		}
	default:
		return nil
	}
}
