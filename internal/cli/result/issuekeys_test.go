package result

import (
	"slices"
	"testing"

	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// A pull request reports the issue keys its title and source branch mention:
// each once, in the order they appear, and none at all rather than an empty
// list when there are none. They are matched by shape, so what merely looks
// like a key is reported too.
func TestAPullRequestReportsTheIssueKeysItMentions(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		title, branch string
		want          []string
	}{
		{"PAY-12: refund twice, see PAY-7", "feature/PAY-12-refunds", []string{"PAY-12", "PAY-7"}},
		{"Refund twice", "bugfix/OPS2-301_and_PAY-7", []string{"OPS2-301", "PAY-7"}},
		{"Refund twice", "feature/refunds", nil},
		{"lower-12 and A-1 are not keys", "feature/pay-12", nil},
		{"Decode UTF-8 properly", "feature/decoding", []string{"UTF-8"}},
	} {
		reported := PullRequestFrom(pullrequestservice.PullRequest{Title: testCase.title, SourceBranch: testCase.branch})
		if !slices.Equal(reported.IssueKeys, testCase.want) {
			t.Errorf("%q on %q reports %v, want %v", testCase.title, testCase.branch, reported.IssueKeys, testCase.want)
		}
	}
}
