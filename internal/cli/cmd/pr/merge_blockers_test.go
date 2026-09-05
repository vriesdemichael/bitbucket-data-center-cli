package prcmd

import (
	"reflect"
	"testing"

	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// TestMergeBlockerLines covers how a veto becomes a line of output.
//
// The version this replaces ran `bb pr get` against a mocked Bitbucket that
// answered with one hand-written veto, and read the bullet back out of the
// command's output. That put a socket and a whole command between a string
// function and its assertion, and the interesting input -- a veto with no
// summary -- was the author's invention either way.
//
// Whether Bitbucket sends a veto shaped like any of these is a question for the
// server and TestLivePullRequestMergeability asks it. What is pinned here is
// that every shape produces a line, because the defect was an empty bullet.
func TestMergeBlockerLines(t *testing.T) {
	cases := []struct {
		name     string
		blockers []pullrequestservice.MergeBlocker
		want     []string
	}{
		{
			name:     "summary and detail are joined",
			blockers: []pullrequestservice.MergeBlocker{{Summary: "Not enough approvals", Detail: "1 of 2"}},
			want:     []string{"Not enough approvals (1 of 2)"},
		},
		{
			// The case the mocked test existed for: an empty summary printed an
			// empty bullet, which reads as a blocker with no name.
			name:     "a detail with no summary still says something",
			blockers: []pullrequestservice.MergeBlocker{{Detail: "detail only blocker"}},
			want:     []string{"detail only blocker"},
		},
		{
			name:     "a summary with no detail is left alone",
			blockers: []pullrequestservice.MergeBlocker{{Summary: "summary only blocker"}},
			want:     []string{"summary only blocker"},
		},
		{
			// Several of Bitbucket's own merge checks repeat the summary as the
			// detail, and "X (X)" is noise.
			name:     "a repeated detail is not appended",
			blockers: []pullrequestservice.MergeBlocker{{Summary: "Same", Detail: "same"}},
			want:     []string{"Same"},
		},
		{
			name:     "a veto with nothing in it contributes no line",
			blockers: []pullrequestservice.MergeBlocker{{Summary: "  ", Detail: ""}},
			want:     []string{},
		},
		{
			name:     "no vetoes",
			blockers: nil,
			want:     []string{},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := mergeBlockerLines(testCase.blockers)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("mergeBlockerLines() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}
