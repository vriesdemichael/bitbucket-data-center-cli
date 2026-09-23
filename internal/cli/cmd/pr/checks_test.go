package prcmd

import (
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// TestBuildsExitStatusFollowsGhPrChecks holds the exit status to gh pr checks':
// a failure first, then anything not finished, and cancelled counts as
// neither.
func TestBuildsExitStatusFollowsGhPrChecks(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		states []string
		want   int
	}{
		{name: "no builds", want: 0},
		{name: "all successful", states: []string{"SUCCESSFUL", "SUCCESSFUL"}, want: 0},
		{name: "cancelled counts as neither", states: []string{"SUCCESSFUL", "CANCELLED"}, want: 0},
		{name: "one in progress", states: []string{"SUCCESSFUL", "INPROGRESS"}, want: 8},
		{name: "no result", states: []string{"UNKNOWN"}, want: 8},
		{name: "a state nobody has seen", states: []string{"QUEUED"}, want: 8},
		{name: "a failure wins over one in progress", states: []string{"INPROGRESS", "FAILED", "SUCCESSFUL"}, want: 1},
		{name: "lower case", states: []string{"failed"}, want: 1},
	} {
		statuses := make([]pullrequestservice.BuildStatus, 0, len(testCase.states))
		for _, state := range testCase.states {
			statuses = append(statuses, pullrequestservice.BuildStatus{Key: "build", State: state})
		}

		err := buildsExitStatus(statuses)
		if got := apperrors.ExitCode(err); got != testCase.want {
			t.Errorf("%s: exit %d, want %d (%v)", testCase.name, got, testCase.want, err)
		}
		if testCase.want != 0 && (err == nil || err.Error() == "") {
			t.Errorf("%s: a non-zero exit carries no reason to print", testCase.name)
		}
	}
}
