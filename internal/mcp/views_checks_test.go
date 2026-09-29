package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

func branchRef(name string) requiredRef {
	return requiredRef{ID: "refs/heads/" + name, DisplayID: name}
}

// Each matcher decides as Bitbucket's did when probed against its merge veto
// on 10.4.3 (TestLiveRequiredChecksAgreeWithTheMergeVeto pins the main cases),
// and as its code reads for the edges, such as a pattern's braces, and says
// when it cannot be sure.
func TestRequiredMatchersDecideAsBitbucketDoes(t *testing.T) {
	t.Parallel()

	// A model whose development branch is not the default one, and whose
	// releases go by a prefix of their own, as the live test configures it.
	defaultBranch := "refs/heads/master"
	model := &branchModel{
		Development: &requiredRef{ID: "refs/heads/develop", DisplayID: "develop"},
		Types: []modelCategory{
			{ID: "FEATURE", Prefix: "feature/"},
			{ID: "RELEASE", Prefix: "stable/"},
		},
	}
	known := matchFacts{model: model, defaultBranch: &defaultBranch}

	for _, tc := range []struct {
		name    string
		matcher refMatcher
		ref     requiredRef
		facts   matchFacts
		matches bool
		certain bool
	}{
		{"any ref", refMatcher{"ANY_REF", "ANY_REF_MATCHER_ID"}, branchRef("whatever/it/is"), known, true, true},

		{"a branch by its full id", refMatcher{"BRANCH", "refs/heads/master"}, branchRef("master"), known, true, true},
		{"a branch by its name", refMatcher{"BRANCH", "master"}, branchRef("master"), known, true, true},
		{"a branch by heads/", refMatcher{"BRANCH", "heads/master"}, branchRef("master"), known, true, true},
		{"a nested branch by its name", refMatcher{"BRANCH", "release/1.0"}, branchRef("release/1.0"), known, true, true},
		{"another branch", refMatcher{"BRANCH", "refs/heads/master"}, branchRef("develop"), known, false, true},
		{"heads/ qualifies only as written", refMatcher{"BRANCH", "HEADS/master"}, branchRef("master"), known, false, true},
		{"a branch in another case", refMatcher{"BRANCH", "refs/heads/MASTER"}, branchRef("master"), known, true, false},

		{"a pattern one level down", refMatcher{"PATTERN", "release/*"}, branchRef("release/1.0"), known, true, true},
		{"* stays within a segment", refMatcher{"PATTERN", "release/*"}, branchRef("release/1/2"), known, false, true},
		{"** crosses segments", refMatcher{"PATTERN", "release/**"}, branchRef("release/1/2"), known, true, true},
		{"a trailing separator is everything below", refMatcher{"PATTERN", "release/"}, branchRef("release/1/2"), known, true, true},
		{"a full pattern is not prefixed", refMatcher{"PATTERN", "refs/heads/release/*"}, branchRef("release/1.0"), known, true, true},
		{"a full pattern stays one level down", refMatcher{"PATTERN", "refs/heads/release/*"}, branchRef("release/1/2"), known, false, true},
		{"* alone is any branch", refMatcher{"PATTERN", "*"}, branchRef("release/1/2"), known, true, true},
		{"a prefix", refMatcher{"PATTERN", "mas*"}, branchRef("master"), known, true, true},
		{"part of the full id", refMatcher{"PATTERN", "heads/master"}, branchRef("master"), known, true, true},
		{"? is one character", refMatcher{"PATTERN", "rel?ase/1.0"}, branchRef("release/1.0"), known, true, true},
		{"a pattern at any depth", refMatcher{"PATTERN", "1/*"}, branchRef("release/1/2"), known, true, true},
		{"a pattern that does not match", refMatcher{"PATTERN", "dev*"}, branchRef("master"), known, false, true},
		{"case in what * matches decides nothing", refMatcher{"PATTERN", "feature/*"}, branchRef("feature/ABC-1"), known, true, true},
		{"a pattern in another case", refMatcher{"PATTERN", "RELEASE/*"}, branchRef("release/1.0"), known, true, false},
		{"a branch in another case than the pattern", refMatcher{"PATTERN", "release/*"}, branchRef("Release/2.0"), known, true, false},
		{"a pattern with a template variable", refMatcher{"PATTERN", "release/{version}"}, branchRef("release/1.0"), known, false, false},

		{"the development branch", refMatcher{"MODEL_BRANCH", "development"}, branchRef("develop"), known, true, true},
		{"not the development branch", refMatcher{"MODEL_BRANCH", "development"}, branchRef("master"), known, false, true},
		{"an unconfigured production branch", refMatcher{"MODEL_BRANCH", "production"}, branchRef("master"), known, false, true},
		{"a model branch Bitbucket does not know", refMatcher{"MODEL_BRANCH", "Development"}, branchRef("develop"), known, false, true},
		{"a model branch without the model", refMatcher{"MODEL_BRANCH", "development"}, branchRef("develop"), matchFacts{}, false, false},

		{"a category by its prefix", refMatcher{"MODEL_CATEGORY", "RELEASE"}, branchRef("stable/1.0"), known, true, true},
		{"a category's prefix, not its name", refMatcher{"MODEL_CATEGORY", "RELEASE"}, branchRef("release/1.0"), known, false, true},
		{"a category's prefix, case and all", refMatcher{"MODEL_CATEGORY", "RELEASE"}, branchRef("Stable/1.0"), known, false, true},
		{"a category switched off", refMatcher{"MODEL_CATEGORY", "HOTFIX"}, branchRef("hotfix/1"), known, false, true},
		{"a category Bitbucket does not know", refMatcher{"MODEL_CATEGORY", "release"}, branchRef("stable/1.0"), known, false, true},
		{"a category without the model", refMatcher{"MODEL_CATEGORY", "RELEASE"}, branchRef("stable/1.0"), matchFacts{}, false, false},

		{"the default branch", refMatcher{"DEFAULT_BRANCH", "#"}, branchRef("master"), known, true, true},
		{"not the default branch", refMatcher{"DEFAULT_BRANCH", "#"}, branchRef("develop"), known, false, true},
		{"the default branch unread", refMatcher{"DEFAULT_BRANCH", "#"}, branchRef("master"), matchFacts{}, false, false},

		{"a kind bb does not know", refMatcher{"TAG", "v1"}, branchRef("master"), known, false, false},
		{"no kind at all", refMatcher{}, branchRef("master"), known, false, false},
	} {
		matches, certain := tc.matcher.matches(tc.ref, tc.facts)
		if matches != tc.matches || certain != tc.certain {
			t.Errorf("%s: %s %q against %s matches %v, certain %v; want %v, %v",
				tc.name, tc.matcher.kind, tc.matcher.id, tc.ref.ID, matches, certain, tc.matches, tc.certain)
		}
	}
}

// antMatch follows Spring's AntPathMatcher, edges included. Every answer here
// is the one Spring 6.2.19, as Bitbucket 10.4.3 ships it, gave in jshell.
func TestAntMatchFollowsSpring(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"**", "refs/heads/a/b", true},
		{"**/release/**", "refs/heads/release", true},
		{"**/release/**", "refs/heads/releases/1", false},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "a/x/y/c", false},
		{"a/**/b/**/c", "a/x/b/y/c", true},
		{"a/**/b/**/c", "a/b/c", true},
		{"a/**/b/**/c", "a/c/b", false},
		{"**/**/x", "x", true},
		{"a", "a/b", false},
		{"a/b", "a", false},
		{"/a", "a", false},
		{"a//b", "a/b", true},
		{"a/f*o", "a/foo", true},
		{"a/f*o", "a/fob", false},
		{"a/*a*", "a/banana", true},
		{"a/??", "a/ab", true},
		{"a/??", "a/abc", false},
		{"a/a*", "a/a*b", true},
		// A last * matches the empty segment after a trailing separator,
		// which no ref has.
		{"a/*", "a/", true},
		{"a/b", "a/b/", false},
		{"**/release/*", "refs/heads/release/1.0", true},
		{"**/release/*", "refs/heads/release/1/2", false},
		{"**/release/**", "refs/heads/release/1/2", true},
		{"refs/heads/release/*", "refs/heads/release/1.0", true},
		{"refs/heads/release/*", "refs/heads/release/1/2", false},
		{"**/*", "refs/heads/release/1/2", true},
		{"**/mas*", "refs/heads/master", true},
		{"**/heads/master", "refs/heads/master", true},
		{"**/rel?ase/1.0", "refs/heads/release/1.0", true},
		{"**/1/*", "refs/heads/release/1/2", true},
		{"**/dev*", "refs/heads/master", false},
		{"**/feature/*", "refs/heads/feature/ABC-1", true},
		{`**/foo\**`, `refs/heads/foo\bar`, true},
		{"**/a*b*c", "refs/heads/axxbyyc", true},
		{"**/a*b*c", "refs/heads/axxbyy", false},
		{"refs/**/x/**", "refs/heads/x", true},
		{"refs/heads/**/x", "refs/heads/a/x/b/x", true},
		{"**/x/**/y", "refs/heads/x/y", true},
		{"**/x/**/y", "refs/heads/y/x", false},
		{"**/*.*", "refs/heads/1.0", true},
		{"**/*.*", "refs/heads/10", false},
		{"**/a/**/**/b", "refs/a/b", true},
		{"**/a/*/**/b", "refs/a/b", false},
		{"**/a/*/**/b", "refs/a/x/b", true},
		// What is not * or ? is itself, however a regular expression reads it.
		{"**/.*", "refs/heads/.x", true},
		{"**/[ab]", "refs/heads/[ab]", true},
		{"**/a+b", "refs/heads/a+b", true},
		{"**/a+b", "refs/heads/aab", false},
	} {
		if got := antMatch(tc.pattern, tc.path); got != tc.want {
			t.Errorf("antMatch(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

// A condition applies when its matcher matches the target and its exemption,
// if any, does not match the source; an exemption bb cannot decide matters
// only for a condition whose matcher matches.
func TestRequiredConditionAppliesUnlessTheSourceIsExempt(t *testing.T) {
	t.Parallel()

	master := refMatcher{"BRANCH", "refs/heads/master"}
	hotfixes := refMatcher{"PATTERN", "hotfix/*"}
	unknown := refMatcher{"SOMETHING", "else"}

	for _, tc := range []struct {
		name             string
		condition        requiredCondition
		target, source   requiredRef
		applies, certain bool
	}{
		{"no exemption", requiredCondition{target: master}, branchRef("master"), branchRef("hotfix/1"), true, true},
		{"an exempt source", requiredCondition{target: master, exempt: &hotfixes}, branchRef("master"), branchRef("hotfix/1"), false, true},
		{"a source that is not exempt", requiredCondition{target: master, exempt: &hotfixes}, branchRef("master"), branchRef("feature/1"), true, true},
		{"another target, whatever the exemption", requiredCondition{target: master, exempt: &unknown}, branchRef("develop"), branchRef("feature/1"), false, true},
		{"an exemption bb cannot decide", requiredCondition{target: master, exempt: &unknown}, branchRef("master"), branchRef("feature/1"), false, false},
		{"a matcher bb cannot decide", requiredCondition{target: unknown}, branchRef("master"), branchRef("feature/1"), false, false},
	} {
		applies, certain := tc.condition.appliesTo(tc.target, tc.source, matchFacts{})
		if applies != tc.applies || certain != tc.certain {
			t.Errorf("%s: applies %v, certain %v; want %v, %v", tc.name, applies, certain, tc.applies, tc.certain)
		}
	}
}

// Only the conditions required for pull requests are matched: one required
// for the merge queue alone never blocks a pull request, and one that states
// no scope is from a release that applies every condition to pull requests.
func TestRequiredConditionsOfKeepsThoseForPullRequests(t *testing.T) {
	t.Parallel()

	var listed []openapigenerated.RestRequiredBuildCondition
	if err := json.Unmarshal([]byte(`[
		{"buildParentKeys": ["queue"], "requiredForPullRequest": false, "requiredForMergeQueue": true,
		 "refMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}}},
		{"buildParentKeys": ["pr-a", "pr-b"], "requiredForPullRequest": true,
		 "refMatcher": {"id": "release/*", "type": {"id": "PATTERN"}},
		 "exemptRefMatcher": {"id": "HOTFIX", "type": {"id": "MODEL_CATEGORY"}}},
		{"buildParentKeys": ["old"], "refMatcher": {"id": "ANY_REF_MATCHER_ID", "type": {"id": "ANY_REF"}}},
		{"buildParentKeys": ["untyped"], "refMatcher": {"id": "master"}}
	]`), &listed); err != nil {
		t.Fatalf("decode the conditions: %v", err)
	}

	got := requiredConditionsOf(listed)
	want := []requiredCondition{
		{keys: []string{"pr-a", "pr-b"}, target: refMatcher{"PATTERN", "release/*"}, exempt: &refMatcher{"MODEL_CATEGORY", "HOTFIX"}},
		{keys: []string{"old"}, target: refMatcher{"ANY_REF", "ANY_REF_MATCHER_ID"}},
		{keys: []string{"untyped"}, target: refMatcher{"", "master"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requiredConditionsOf = %+v, want %+v", got, want)
	}
}

// The keys of every condition that applies are required together, each once;
// a condition bb cannot decide leaves nothing known.
func TestRequiredKeysAreThoseOfEveryConditionThatApplies(t *testing.T) {
	t.Parallel()

	conditions := []requiredCondition{
		{keys: []string{"build", "test"}, target: refMatcher{"BRANCH", "master"}},
		{keys: []string{"release"}, target: refMatcher{"PATTERN", "release/*"}},
		{keys: []string{"test", "lint"}, target: refMatcher{"ANY_REF", ""}},
	}
	keys, known := requiredKeys(conditions, branchRef("master"), branchRef("feature/1"), matchFacts{})
	if !known || !reflect.DeepEqual(keys, []string{"build", "test", "lint"}) {
		t.Errorf("required keys %v, known %v; want build, test, lint", keys, known)
	}

	undecided := append(conditions, requiredCondition{keys: []string{"more"}, target: refMatcher{"MODEL_BRANCH", "development"}})
	if keys, known := requiredKeys(undecided, branchRef("master"), branchRef("feature/1"), matchFacts{}); known || keys != nil {
		t.Errorf("with a condition bb cannot decide: keys %v, known %v; want nothing known", keys, known)
	}
}

// A key is satisfied by a build that names it as its parent and passed. The
// build's own key plays no part, and one passing build is enough, whatever
// its siblings did.
func TestRequiredChecksCountAParentThatPassed(t *testing.T) {
	t.Parallel()

	builds := []requiredBuild{
		{Key: "own-key", State: "SUCCESSFUL", Name: "Posted without a parent"},
		{Key: "unit", Parent: "child", State: "SUCCESSFUL", Name: "Unit tests", URL: "https://ci/unit"},
		{Key: "siblings-1", Parent: "siblings", State: "FAILED", UpdatedDate: 3},
		{Key: "siblings-2", Parent: "siblings", State: "SUCCESSFUL", Name: "The one that passed", UpdatedDate: 1},
		{Key: "running-1", Parent: "running", State: "FAILED", UpdatedDate: 3},
		{Key: "running-2", Parent: "running", State: "INPROGRESS", Name: "Still running", UpdatedDate: 1},
		{Key: "failed-1", Parent: "failed", State: "FAILED", Name: "Older", UpdatedDate: 1},
		{Key: "failed-2", Parent: "failed", State: "FAILED", Name: "Newer", UpdatedDate: 2},
		{Key: "case", Parent: "case", State: "SUCCESSFUL"},
		{Key: "other-parent", Parent: "somewhere-else", State: "SUCCESSFUL"},
	}
	got := map[string]viewRequiredCheck{}
	for _, check := range requiredChecks([]string{"own-key", "child", "siblings", "running", "failed", "CASE", "other-parent", "nothing"}, builds) {
		got[check.Key] = check
	}
	want := map[string]viewRequiredCheck{
		"own-key":      {Key: "own-key", Unparented: true},
		"child":        {Key: "child", Name: "Unit tests", State: "SUCCESSFUL", URL: "https://ci/unit"},
		"siblings":     {Key: "siblings", Name: "The one that passed", State: "SUCCESSFUL"},
		"running":      {Key: "running", Name: "Still running", State: "INPROGRESS"},
		"failed":       {Key: "failed", Name: "Newer", State: "FAILED"},
		"CASE":         {Key: "CASE"},
		"other-parent": {Key: "other-parent", Unparented: true},
		"nothing":      {Key: "nothing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requiredChecks =\n%+v\nwant\n%+v", got, want)
	}
	if checks := requiredChecks(nil, builds); checks != nil {
		t.Errorf("with nothing required: %v, want none", checks)
	}
}

// The card lists what needs the person first: missing, failed, running,
// canceled, unknown, then passed, each by key as Bitbucket lists them in its
// veto, ignoring case first.
func TestRequiredChecksListWhatNeedsAttentionFirst(t *testing.T) {
	t.Parallel()

	builds := []requiredBuild{
		{Key: "p", Parent: "passed", State: "SUCCESSFUL"},
		{Key: "u", Parent: "unknown", State: "UNKNOWN"},
		{Key: "c", Parent: "canceled", State: "CANCELLED"},
		{Key: "r", Parent: "running", State: "INPROGRESS"},
		{Key: "f", Parent: "failed", State: "FAILED"},
	}
	keys := []string{"passed", "unknown", "canceled", "running", "failed", "missing-b", "Missing-a", "missing-B"}
	var order []string
	for _, check := range requiredChecks(keys, builds) {
		order = append(order, check.Key)
	}
	want := []string{"Missing-a", "missing-B", "missing-b", "failed", "running", "canceled", "unknown", "passed"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
}

// What bb cannot read it says nothing about: a pull request without a source
// commit, or a Bitbucket that does not answer (here a port that refuses every
// connection).
func TestRequiredChecksForViewSaysNothingItCannotRead(t *testing.T) {
	t.Parallel()

	clients := testClients(t)
	pr := pullrequestservice.PullRequest{ID: 7, SourceCommit: "0123456789abcdef0123456789abcdef01234567"}
	if checks, known := requiredChecksForView(context.Background(), clients, "PROJ", "app", pr); known || checks != nil {
		t.Errorf("against an unreachable Bitbucket: %v, known %v; want nothing known", checks, known)
	}
	pr.SourceCommit = ""
	if checks, known := requiredChecksForView(context.Background(), clients, "PROJ", "app", pr); known || checks != nil {
		t.Errorf("without a source commit: %v, known %v; want nothing known", checks, known)
	}
}
