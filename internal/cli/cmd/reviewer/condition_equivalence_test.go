package reviewercmd

import (
	"encoding/json"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// serverCondition decodes a condition as Bitbucket answers it.
//
// Written as JSON rather than built as a value because that is the point: the
// generated types nest anonymous structs that are laborious to construct and
// easy to get subtly wrong, and the shapes here were copied from a running
// instance. A live probe is what they came from, and JSON is what it returned.
func serverCondition(t *testing.T, payload string) openapigenerated.RestPullRequestCondition {
	t.Helper()

	var condition openapigenerated.RestPullRequestCondition
	if err := json.Unmarshal([]byte(payload), &condition); err != nil {
		t.Fatalf("decode the server's condition: %v", err)
	}

	return condition
}

// requestedCondition decodes a condition as a caller writes it.
func requestedCondition(t *testing.T, payload string) openapigenerated.RestDefaultReviewersRequest {
	t.Helper()

	var request openapigenerated.RestDefaultReviewersRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		t.Fatalf("decode the requested condition: %v", err)
	}

	return request
}

// anyRefFromTheServer is what Bitbucket returns for a condition created with
// ANY_REF: the matcher id is its own, and the reviewer is the whole user.
const anyRefFromTheServer = `{
  "id": 6,
  "sourceRefMatcher": {"id":"ANY_REF_MATCHER_ID","displayId":"ANY_REF_MATCHER_ID","type":{"id":"ANY_REF","name":"Any branch"}},
  "targetRefMatcher": {"id":"ANY_REF_MATCHER_ID","displayId":"ANY_REF_MATCHER_ID","type":{"id":"ANY_REF","name":"Any branch"}},
  "reviewers": [{"name":"alice","emailAddress":"a@e.local","active":true,"displayName":"Alice","id":116,"slug":"alice","type":"NORMAL"}],
  "requiredApprovals": 1
}`

// anyRefAsRequested is the same condition as the caller writes it.
const anyRefAsRequested = `{
  "sourceMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
  "targetMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
  "reviewers": [{"id":116}],
  "requiredApprovals": 1
}`

// TestReviewerConditionEquivalent covers the comparison the create preview makes
// between what a caller asked for and what Bitbucket already holds.
//
// The two sides are shaped differently, which is the whole difficulty: the
// previous version compared them whole and so never matched, and an existing
// condition was predicted as a create. The live half -- that Bitbucket really
// does answer in the shape above -- is
// TestLiveGovernanceDryRunPredictionsReadRealState.
func TestReviewerConditionEquivalent(t *testing.T) {
	t.Run("a condition matches itself across the two shapes", func(t *testing.T) {
		if !reviewerConditionEquivalent(
			serverCondition(t, anyRefFromTheServer),
			requestedCondition(t, anyRefAsRequested),
		) {
			t.Error("an ANY_REF condition did not match itself")
		}
	})

	t.Run("a concrete matcher still compares its id", func(t *testing.T) {
		onMain := serverCondition(t, `{
		  "sourceRefMatcher": {"id":"refs/heads/main","type":{"id":"BRANCH","name":"Branch"}},
		  "targetRefMatcher": {"id":"refs/heads/main","type":{"id":"BRANCH","name":"Branch"}},
		  "reviewers": [{"id":116,"name":"alice"}],
		  "requiredApprovals": 1
		}`)
		onRelease := requestedCondition(t, `{
		  "sourceMatcher": {"id":"refs/heads/release","type":{"id":"BRANCH"}},
		  "targetMatcher": {"id":"refs/heads/release","type":{"id":"BRANCH"}},
		  "reviewers": [{"id":116}],
		  "requiredApprovals": 1
		}`)

		if reviewerConditionEquivalent(onMain, onRelease) {
			t.Error("two different branches matched")
		}
	})

	t.Run("a different approval count is a different condition", func(t *testing.T) {
		twoApprovals := requestedCondition(t, `{
		  "sourceMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
		  "targetMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
		  "reviewers": [{"id":116}],
		  "requiredApprovals": 2
		}`)

		if reviewerConditionEquivalent(serverCondition(t, anyRefFromTheServer), twoApprovals) {
			t.Error("conditions with different approval counts matched")
		}
	})

	t.Run("a reviewer named rather than numbered still matches", func(t *testing.T) {
		byName := requestedCondition(t, `{
		  "sourceMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
		  "targetMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
		  "reviewers": [{"name":"Alice"}],
		  "requiredApprovals": 1
		}`)

		if !reviewerConditionEquivalent(serverCondition(t, anyRefFromTheServer), byName) {
			t.Error("a reviewer given by name did not match the same reviewer by id")
		}
	})

	t.Run("a different reviewer is a different condition", func(t *testing.T) {
		somebodyElse := requestedCondition(t, `{
		  "sourceMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
		  "targetMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
		  "reviewers": [{"id":117}],
		  "requiredApprovals": 1
		}`)

		if reviewerConditionEquivalent(serverCondition(t, anyRefFromTheServer), somebodyElse) {
			t.Error("conditions with different reviewers matched")
		}
	})

	t.Run("an extra reviewer is a different condition", func(t *testing.T) {
		twoReviewers := requestedCondition(t, `{
		  "sourceMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
		  "targetMatcher": {"id":"ANY_REF","type":{"id":"ANY_REF"}},
		  "reviewers": [{"id":116},{"id":117}],
		  "requiredApprovals": 1
		}`)

		if reviewerConditionEquivalent(serverCondition(t, anyRefFromTheServer), twoReviewers) {
			t.Error("a condition with an extra reviewer matched one without it")
		}
	})

	t.Run("a condition with no reviewers matches one with none", func(t *testing.T) {
		bare := serverCondition(t, `{"requiredApprovals":0}`)
		requested := requestedCondition(t, `{"requiredApprovals":0}`)

		if !reviewerConditionEquivalent(bare, requested) {
			t.Error("two empty conditions did not match")
		}
	})
}
