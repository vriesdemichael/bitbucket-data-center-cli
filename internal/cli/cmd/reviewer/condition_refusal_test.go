package reviewercmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// The rules each release applies to a condition body, as the live suite and raw
// requests found them on 9.3.2 to 10.5.0 (TestLiveDryRunRefusalsFailAsTheRealRunDoes
// proves them against the release under test). These pin the reading of a body
// against each set of rules, which needs no server.
func TestConditionRefusalAppliesEachReleasesRules(t *testing.T) {
	t.Parallel()

	const matchers = `"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"targetMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}}`
	cases := []struct {
		name   string
		body   string
		older  bool
		want   apperrors.Kind
		saying string
	}{
		{name: "no target matcher", body: `{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"requiredApprovals":1}`,
			want: apperrors.KindValidation, saying: "A targetMatcher with an ID and type is required"},
		{name: "approvals before reviewers", body: `{` + matchers + `}`,
			want: apperrors.KindValidation, saying: "Required approvals must be >= 0."},
		{name: "neither reviewers nor groups", body: `{` + matchers + `,"requiredApprovals":1}`,
			want: apperrors.KindValidation, saying: "Reviewers or reviewer groups are required."},
		{name: "both empty", body: `{` + matchers + `,"reviewers":[],"reviewerGroups":[],"requiredApprovals":1}`,
			want: apperrors.KindValidation, saying: "Reviewers or reviewer groups are required."},
		{name: "reviewers empty on their own", body: `{` + matchers + `,"reviewers":[],"requiredApprovals":0}`},
		{name: "groups empty on their own", body: `{` + matchers + `,"reviewerGroups":[],"requiredApprovals":0}`},
		{name: "a reviewer by name", body: `{` + matchers + `,"reviewers":[{"name":"admin"}],"requiredApprovals":1}`,
			want: apperrors.KindNotFound, saying: "User with ID -1 does not exist"},
		{name: "older: no reviewers, an approval", body: `{` + matchers + `,"requiredApprovals":1}`, older: true},
		{name: "older: no reviewers, no approvals", body: `{` + matchers + `,"reviewers":[],"requiredApprovals":0}`, older: true,
			want: apperrors.KindValidation, saying: "Required approvals must be > 0 if no reviewers are provided"},
		{name: "older: a reviewer by name", body: `{` + matchers + `,"reviewers":[{"id":2},{"name":"admin"}],"requiredApprovals":0}`, older: true,
			want: apperrors.KindValidation, saying: "No user exists for identifier -1"},
		{name: "older: reviewers by id", body: `{` + matchers + `,"reviewers":[{"id":2}],"requiredApprovals":0}`, older: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			older := func() (bool, error) { return testCase.older, nil }
			refusal, failure := conditionRefusal(rawCondition(testCase.body), older)
			if failure != nil {
				t.Fatalf("no verdict: %v", failure)
			}
			if testCase.want == "" {
				if refusal != nil {
					t.Fatalf("refused %s: %v", testCase.body, refusal)
				}
				return
			}
			if refusal == nil || apperrors.KindOf(refusal) != testCase.want || !strings.Contains(refusal.Error(), testCase.saying) {
				t.Fatalf("refusal of %s = %v, want %s saying %q", testCase.body, refusal, testCase.want, testCase.saying)
			}
		})
	}
}

// The release is asked only once the checks every release makes have passed,
// and a failure to ask is no verdict.
func TestConditionRefusalAsksTheReleaseLast(t *testing.T) {
	t.Parallel()

	asked := false
	refusal, failure := conditionRefusal(rawCondition(`{"requiredApprovals":1}`), func() (bool, error) {
		asked = true
		return false, nil
	})
	if asked || failure != nil || apperrors.KindOf(refusal) != apperrors.KindValidation {
		t.Fatalf("a body without matchers: asked %t, refusal %v, failure %v", asked, refusal, failure)
	}

	unreachable := errors.New("the release could not be read")
	failing := func() (bool, error) { return false, unreachable }
	body := `{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"targetMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"requiredApprovals":1}`
	if _, failure := conditionRefusal(rawCondition(body), failing); !errors.Is(failure, unreachable) {
		t.Fatalf("failure = %v, want the error asking the release gave", failure)
	}

	var condition openapigenerated.RestDefaultReviewersRequest
	if err := json.Unmarshal([]byte(body), &condition); err != nil {
		t.Fatalf("decode the condition: %v", err)
	}
	if _, err := conditionCreateOutcome(nil, condition, failing); !errors.Is(err, unreachable) {
		t.Fatalf("create outcome error = %v, want the error asking the release gave", err)
	}
	if _, err := conditionUpdateOutcome(nil, "1", rawCondition(body), "project", failing); !errors.Is(err, unreachable) {
		t.Fatalf("update outcome error = %v, want the error asking the release gave", err)
	}
}

func TestConditionRefusalOfABodyItCannotReadIsNoVerdict(t *testing.T) {
	t.Parallel()

	never := func() (bool, error) {
		t.Fatal("the release was asked about a body that could not be read")
		return false, nil
	}
	for _, body := range []any{make(chan int), "not an object"} {
		if refusal, failure := conditionRefusal(body, never); refusal != nil || apperrors.KindOf(failure) != apperrors.KindInternal {
			t.Errorf("conditionRefusal(%T) = %v, %v; want an internal failure and no verdict", body, refusal, failure)
		}
	}
}

// rawCondition is a body sent exactly as written.
type rawCondition string

func (body rawCondition) MarshalJSON() ([]byte, error) { return []byte(body), nil }
