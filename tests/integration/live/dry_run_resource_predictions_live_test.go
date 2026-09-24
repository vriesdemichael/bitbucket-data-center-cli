//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveResourceDryRunPredictionsReadRealState is the third of these: the
// previews for branches, branch restrictions, build statuses, required build
// checks, projects, repositories and tags.
//
// Same shape as the other two. Each of these commands asks Bitbucket what it
// currently holds and predicts from the answer, so a fixture standing in for
// Bitbucket makes the prediction a restatement of the fixture. Here the states
// are cheap to reach for real: create the branch, then ask what creating it
// again would do.
func TestLiveResourceDryRunPredictionsReadRealState(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	t.Run("branches", func(t *testing.T) {
		// master is what the seeded repository already has, so creating it is a
		// conflict and setting it as default is a no-op.
		liveRefuses(t, apperrors.KindConflict, "branch", "create", "master", "--start-point", "master")
		livePredicts(t, jsonoutput.OutcomeNoOp, "branch", "default", "set", "master")
		livePredicts(t, jsonoutput.OutcomeNoOp, "branch", "model", "update", "master")
	})

	t.Run("branch restrictions", func(t *testing.T) {
		const matcher = "refs/heads/predicted"

		livePredicts(t, jsonoutput.OutcomeNoOp, "branch", "restriction", "delete", "999999", "--repo", repoRef)

		created := mustLiveCLI(t, "branch", "restriction", "create", "--repo", repoRef,
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher)
		restriction, _ := decodeJSONMap(t, created)["restriction"].(map[string]any)
		id, ok := restriction["id"].(float64)
		if !ok {
			t.Fatalf("the created restriction has no id:\n%s", created)
		}
		restrictionID := fmt.Sprintf("%d", int(id))
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "branch", "restriction", "get", restrictionID, "--repo", repoRef)),
			storedRestriction{scope: "REPOSITORY", restrictionType: "read-only", matcherType: "BRANCH", matcherID: matcher})

		// Creating it again is not refused. Bitbucket's create is an upsert on
		// the type and matcher, answered with this restriction, and with the same
		// exemptions -- none -- it changes nothing.
		livePredicts(t, jsonoutput.OutcomeNoOp, "branch", "restriction", "create", "--repo", repoRef,
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher)
		livePredicts(t, jsonoutput.OutcomeNoOp, "branch", "restriction", "update", restrictionID, "--repo", repoRef,
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher)
	})

	t.Run("build statuses and required checks", func(t *testing.T) {
		commit := repo.CommitIDs[0]

		livePredicts(t, jsonoutput.OutcomeNoOp, "build", "required", "delete", "999999", "--repo", repoRef)
		noChecks := mustLiveCLI(t, "build", "required", "list", "--repo", repoRef)
		livePredicts(t, jsonoutput.OutcomeWouldApply, "build", "required", "create", "--repo", repoRef,
			"--body", `{"buildParentKeys":["ci"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`)
		if after := mustLiveCLI(t, "build", "required", "list", "--repo", repoRef); after != noChecks {
			t.Fatalf("the dry run created the required build check it predicted\nbefore: %s\nafter:  %s", noChecks, after)
		}

		mustLiveCLI(t, "build", "status", "set", commit, "--key", "ci",
			"--state", "SUCCESSFUL", "--url", "http://example.invalid/ci")
		stored := map[string]any{"state": "SUCCESSFUL", "url": "http://example.invalid/ci"}
		commandCoverageAssertFields(t, "the build status", commandCoverageEntry(t, mustLiveCLI(t, "build", "status", "get", commit), "key", "ci"), stored)

		// Another state, so the update it predicts would show if it were made.
		livePredictsSaying(t, "will be updated", "build", "status", "set", commit, "--key", "ci",
			"--state", "FAILED", "--url", "http://example.invalid/ci")
		commandCoverageAssertFields(t, "the build status after its dry run", commandCoverageEntry(t, mustLiveCLI(t, "build", "status", "get", commit), "key", "ci"), stored)

		created := mustLiveCLI(t, "build", "required", "create", "--repo", repoRef,
			"--body", `{"buildParentKeys":["ci"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`)
		check := decodeJSONMap(t, created)
		id, ok := check["id"].(float64)
		if !ok {
			t.Fatalf("the created required build check has no id:\n%s", created)
		}
		checkID := fmt.Sprintf("%d", int(id))
		commandCoverageAssertRequiredCheck(t, mustLiveCLI(t, "build", "required", "list", "--repo", repoRef), checkID, "ci", "refs/heads/master", "BRANCH")

		// A second build key, which the check would require if the update were
		// made.
		livePredicts(t, jsonoutput.OutcomeWouldApply, "build", "required", "update", checkID, "--repo", repoRef,
			"--body", `{"buildParentKeys":["ci","lint"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`)
		commandCoverageAssertRequiredCheck(t, mustLiveCLI(t, "build", "required", "list", "--repo", repoRef), checkID, "ci", "refs/heads/master", "BRANCH")
	})

	t.Run("projects and repositories", func(t *testing.T) {
		liveRefuses(t, apperrors.KindConflict, "project", "create", seeded.Key, "--name", "Anything")

		// Deleting a project that is not there is exit 4, not a no-op preview.
		//
		// The mocked test asserted no-op, and the code still carries the branch
		// that would produce it -- but nothing can reach it: the admin preflight
		// runs first and asks Bitbucket whether the caller administers a project
		// that does not exist, which is a 404. The mock made the branch look
		// reachable because its permission lookup answered 200 for every project
		// while only the project itself 404'd.
		output, err := executeLiveCLI(t, "--json", "--dry-run", "project", "delete", "NOSUCHPROJECTKEY", "--yes")
		if err == nil {
			t.Fatalf("expected a missing project to fail, got:\n%s", output)
		}
		if code := apperrors.ExitCode(err); code != 4 {
			t.Errorf("exit code = %d, want 4 for a missing project: %v", code, err)
		}

		// The current name and description, so there is nothing to change.
		current := decodeJSONMap(t, mustLiveCLI(t, "project", "get", seeded.Key))
		project, _ := current["project"].(map[string]any)
		name, _ := project["name"].(string)
		livePredicts(t, jsonoutput.OutcomeNoOp, "project", "update", seeded.Key, "--name", name)

		liveRefuses(t, apperrors.KindConflict, "repo", "admin", "create", "--project", seeded.Key, "--name", repo.Name)

		forkName := testsupport.UniqueName("forked-in-a-preview-")
		livePredicts(t, jsonoutput.OutcomeWouldApply, "repo", "admin", "fork", "--repo", repoRef, "--name", forkName)
		assertNoLiveRepositoryNamed(t, forkName, repo.Name)

		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "admin", "update", "--repo", repoRef)
	})

	t.Run("tags", func(t *testing.T) {
		livePredicts(t, jsonoutput.OutcomeNoOp, "tag", "delete", "no-such-tag", "--repo", repoRef)

		mustLiveCLI(t, "tag", "create", "v1", "--repo", repoRef, "--start-point", "master")
		commandCoverageAssertFields(t, "the tag", decodeJSONMap(t, mustLiveCLI(t, "tag", "view", "v1", "--repo", repoRef)),
			map[string]any{"displayId": "v1", "latestCommit": repo.CommitIDs[0]})

		liveRefuses(t, apperrors.KindConflict, "tag", "create", "v1", "--repo", repoRef, "--start-point", "master")

		// #470, against a repository with more tags than one page holds.
		//
		// The preview used to filter a capped listing, so a tag past the cap was
		// predicted as a create and the create then failed. Enough tags to cross
		// a page boundary is what tells a direct lookup from a scan; a
		// repository with one tag cannot, because both find it.
		const beyondAPage = 30
		wantTags := []string{"v1"}
		for index := range beyondAPage {
			name := fmt.Sprintf("v2.0.%d", index)
			mustLiveCLI(t, "tag", "create", name, "--repo", repoRef, "--start-point", "master")
			wantTags = append(wantTags, name)
		}
		// All of them, or the page boundary the next prediction is about is not
		// there to cross.
		if listed := commandCoverageFieldValues(t, mustLiveCLI(t, "tag", "list", "--repo", repoRef, "--limit", "100"), "displayId"); !slices.Equal(listed, slices.Sorted(slices.Values(wantTags))) {
			t.Fatalf("the repository holds tags %v, want %v", listed, slices.Sorted(slices.Values(wantTags)))
		}

		liveRefuses(t, apperrors.KindConflict, "tag", "create", fmt.Sprintf("v2.0.%d", beyondAPage-1),
			"--repo", repoRef, "--start-point", "master")
	})
}

// TestLiveResourceRefusalsFailAsTheRealRunDoes is the resource half of
// TestLiveDryRunRefusalsFailAsTheRealRunDoes: a create for a name or key that
// is taken, refused by the dry run and then by the real run, with the same
// kind from both.
func TestLiveResourceRefusalsFailAsTheRealRunDoes(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	mustLiveCLI(t, "tag", "create", "taken", "--start-point", "master")
	commandCoverageAssertFields(t, "the tag", decodeJSONMap(t, mustLiveCLI(t, "tag", "view", "taken")),
		map[string]any{"displayId": "taken", "latestCommit": repo.CommitIDs[0]})

	project, _ := decodeJSONMap(t, mustLiveCLI(t, "project", "get", seeded.Key))["project"].(map[string]any)
	projectName, _ := project["name"].(string)
	if projectName == "" {
		t.Fatalf("project %s reads back with no name: %v", seeded.Key, project)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"a branch that exists", []string{"branch", "create", "master", "--start-point", "master"}},
		{"a tag that exists", []string{"tag", "create", "taken", "--start-point", "master"}},
		{"a project key in use", []string{"project", "create", seeded.Key, "--name", "Refused " + seeded.Key}},
		// A key of its own, so that only the name is taken -- and in capitals,
		// because Bitbucket compares project names without their case.
		{"a project name in use", []string{"project", "create", strings.ToUpper(testsupport.UniqueName("LTNAME")), "--name", strings.ToUpper(projectName)}},
		{"a repository name in use", []string{"repo", "admin", "create", "--project", seeded.Key, "--name", repo.Name}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			liveVerdictHolds(t, apperrors.KindConflict, testCase.args...)
		})
	}
}

// TestLiveDryRunPredictsWhatBitbucketAccepts covers creates an earlier preview
// refused and Bitbucket performs.
//
// Each was a would-fail verdict on a run that goes through. A restriction
// created again is an upsert of the one there, a reviewer condition or a
// webhook equal to one there is stored beside it, and a repository's reviewer
// group may take a name its project's group has. A gate built on those
// verdicts stopped runs that work. Each case asks the dry run, runs the command
// for real, and reads back what it did.
func TestLiveDryRunPredictsWhatBitbucketAccepts(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Somebody who may be exempted from a restriction and be a reviewer: the
	// administrator the suite runs as holds a licence.
	user := harness.username()
	userID, err := harness.userID(ctx, user)
	if err != nil {
		t.Fatalf("look up %s's id failed: %v", user, err)
	}

	t.Run("creating a restriction the repository already has", func(t *testing.T) {
		const matcher = "refs/heads/upserted"
		readOnly := func(exempt ...string) storedRestriction {
			return storedRestriction{scope: "REPOSITORY", restrictionType: "read-only", matcherType: "BRANCH", matcherID: matcher, users: exempt}
		}

		id := restrictionID(t, mustLiveCLI(t, "branch", "restriction", "create", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher))
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "branch", "restriction", "get", id)), readOnly())

		// With an exemption this time, which the upsert gives the one there.
		upserted := liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "exemptions will be replaced",
			"branch", "restriction", "create", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher, "--user", user)
		if again := restrictionID(t, upserted); again != id {
			t.Fatalf("the create answered restriction %s, want the upsert of %s", again, id)
		}
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "branch", "restriction", "get", id)), readOnly(user))

		// And the same create again changes nothing.
		listing := mustLiveCLI(t, "branch", "restriction", "list")
		same := liveGoesThroughAsPredicted(t, jsonoutput.OutcomeNoOp, "already has this type, matcher and exemptions",
			"branch", "restriction", "create", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher, "--user", user)
		if again := restrictionID(t, same); again != id {
			t.Fatalf("the create answered restriction %s, want the upsert of %s", again, id)
		}
		if after := mustLiveCLI(t, "branch", "restriction", "list"); after != listing {
			t.Fatalf("a create predicted to change nothing changed the restrictions\nbefore: %s\nafter:  %s", listing, after)
		}
	})

	t.Run("creating in a repository a restriction its project has", func(t *testing.T) {
		const matcher = "refs/heads/inherited"
		noDeletes := func(scope string, exempt ...string) storedRestriction {
			return storedRestriction{scope: scope, restrictionType: "no-deletes", matcherType: "BRANCH", matcherID: matcher, users: exempt}
		}

		projects := restrictionID(t, mustLiveCLI(t, "project", "branch-restriction", "create", seeded.Key,
			"--type", "no-deletes", "--matcher-type", "BRANCH", "--matcher-id", matcher))
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "project", "branch-restriction", "get", seeded.Key, projects)), noDeletes("PROJECT"))

		// The repository lists the project's restriction as its own, but a
		// create through the repository is not the upsert of it: it adds the
		// repository's restriction beside it.
		created := liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "will be created",
			"branch", "restriction", "create", "--type", "no-deletes", "--matcher-type", "BRANCH", "--matcher-id", matcher, "--user", user)
		own := restrictionID(t, created)
		if own == projects {
			t.Fatalf("the repository's create answered the project's restriction %s", projects)
		}
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "branch", "restriction", "get", own)), noDeletes("REPOSITORY", user))
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "project", "branch-restriction", "get", seeded.Key, projects)), noDeletes("PROJECT"))
	})

	t.Run("creating a restriction the project already has", func(t *testing.T) {
		const matcher = "refs/heads/project-upserted"
		readOnly := func(exempt ...string) storedRestriction {
			return storedRestriction{scope: "PROJECT", restrictionType: "read-only", matcherType: "BRANCH", matcherID: matcher, users: exempt}
		}

		id := restrictionID(t, mustLiveCLI(t, "project", "branch-restriction", "create", seeded.Key,
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher))
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "project", "branch-restriction", "get", seeded.Key, id)), readOnly())

		upserted := liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "exemptions will be replaced",
			"project", "branch-restriction", "create", seeded.Key, "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher, "--user", user)
		if again := restrictionID(t, upserted); again != id {
			t.Fatalf("the create answered restriction %s, want the upsert of %s", again, id)
		}
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "project", "branch-restriction", "get", seeded.Key, id)), readOnly(user))
	})

	t.Run("a reviewer condition equal to one there", func(t *testing.T) {
		condition := fmt.Sprintf(`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
			`"targetMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"reviewers":[{"id":%d}],"requiredApprovals":1}`, userID)

		for _, scope := range []struct {
			flags []string
			name  string
		}{
			{[]string{"--project", seeded.Key}, "PROJECT"},
			{[]string{"--repo", repoRef}, "REPOSITORY"},
		} {
			first := conditionIDFrom(t, mustLiveCLI(t, append([]string{"reviewer", "condition", "create", condition}, scope.flags...)...))
			second := conditionIDFrom(t, liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "equivalent reviewer condition already exists",
				append([]string{"reviewer", "condition", "create", condition}, scope.flags...)...))
			if second == first {
				t.Fatalf("the second create answered condition %s, the first", first)
			}

			listing := mustLiveCLI(t, append([]string{"reviewer", "condition", "list"}, scope.flags...)...)
			for _, id := range []string{first, second} {
				listed, found := maskedConditionFrom(t, listing, id)
				if !found {
					t.Fatalf("condition %s is not in the %s listing:\n%s", id, scope.name, listing)
				}
				assertMaskedConditionStored(t, listed, maskedCondition{scope: scope.name, approvals: 1, reviewerID: userID, sourceType: "ANY_REF", targetType: "ANY_REF"})
			}
		}
	})

	t.Run("a workflow webhook with a name and URL already there", func(t *testing.T) {
		const name = "twice"
		const url = "http://example.invalid/twice"

		mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "create", name, url)
		liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "already exists",
			"repo", "settings", "workflow", "webhooks", "create", name, url)

		hooks := repoCLIWebhooksIn(t, mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list"))
		if len(hooks) != 2 {
			t.Fatalf("want the two webhooks created, got %d: %v", len(hooks), hooks)
		}
		ids := map[any]bool{}
		for _, entry := range hooks {
			hook, _ := entry.(map[string]any)
			repoCLIAssertWebhook(t, hook, name, url, true, "repo:refs_changed")
			ids[hook["id"]] = true
		}
		if len(ids) != 2 {
			t.Fatalf("the two webhooks share an id: %v", hooks)
		}
	})

	t.Run("a repository reviewer group named like its project's", func(t *testing.T) {
		const name = "shared-name"

		mustLiveCLI(t, "reviewer-group", "create", name, "--users", user, "--project", seeded.Key)
		if groups := liveReviewerGroupsNamed(t, mustLiveCLI(t, "reviewer-group", "list", "--project", seeded.Key), name); len(groups) != 1 || groups[0]["scope"] != "PROJECT" {
			t.Fatalf("want the project's group %q, got %v", name, groups)
		}

		liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "will be created",
			"reviewer-group", "create", name, "--users", user, "--repo", repoRef)

		scopes := []string{}
		for _, group := range liveReviewerGroupsNamed(t, mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef), name) {
			scopes = append(scopes, asString(group["scope"]))
		}
		if slices.Sort(scopes); !slices.Equal(scopes, []string{"PROJECT", "REPOSITORY"}) {
			t.Fatalf("the repository lists groups %q in scopes %v, want its own beside the project's", name, scopes)
		}
	})
}

// assertNoLiveRepositoryNamed checks that no repository the caller can see is
// named name, through the instance-wide search.
//
// A fork lands in the caller's own project, and a lookup under a project key
// that was wrong would read as the fork being absent. So the search is asked
// first for controlName, a repository that does exist: an empty answer for name
// is then about the name.
func assertNoLiveRepositoryNamed(t *testing.T, name, controlName string) {
	t.Helper()

	named := func(query string) []any {
		t.Helper()

		values, ok := decodeJSONMap(t, mustLiveCLI(t, "api", "/rest/api/latest/repos?name="+url.QueryEscape(query)))["values"].([]any)
		if !ok {
			t.Fatalf("the repository search for %q answered with no values", query)
		}

		return values
	}

	if control := named(controlName); len(control) != 1 {
		t.Fatalf("the repository search found %d repositories named %q, which exists once", len(control), controlName)
	}
	if found := named(name); len(found) != 0 {
		t.Fatalf("a dry run left repository %q behind: %v", name, found)
	}
}
