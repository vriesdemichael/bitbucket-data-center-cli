//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// The ids inside a repository or a project are the slots where a completion
// that merely succeeds has done nothing. "1, 2, 3" is not an answer to "which
// webhook am I deleting", so every assertion here reads the description as
// well as the value -- and the ids are seeded, so a source that offered
// whatever the instance happened to hold would be caught by the negative
// halves rather than passing on the positive ones.

// TestLiveCompletionIdsInsideARepository covers the kinds that exist at one
// level only: a pull request's comments, a commit's comments, the required
// build checks and the labels of one repository.
func TestLiveCompletionIdsInsideARepository(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	selector := seeded.Key + "/" + repo.Slug

	if len(repo.CommitIDs) == 0 {
		t.Fatalf("the seeded repository reported no commit ids")
	}
	commitID := repo.CommitIDs[0]

	branch := testsupport.UniqueName("lt-completion-ids-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "completion-ids.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	// Three comments that differ in the ways the verbs care about: an ordinary
	// one, one carrying a suggestion, and a task that has been resolved.
	const plainText = "The completion has to name this one."
	plainComment := addPullRequestComment(t, selector, pullRequestID, plainText)

	suggestionComment := addPullRequestComment(t, selector, pullRequestID,
		"Try this instead:\n```suggestion\nreturn nil\n```")

	resolvedComment := addPullRequestComment(t, selector, pullRequestID,
		"Resolve this before merging.", "--blocker")
	mustLiveCLIUnscoped(t, "pr", "comment", "resolve", pullRequestID, resolvedComment, "--repo", selector)

	t.Run("a comment id is offered with its author and its first line", func(t *testing.T) {
		candidates, directive := completeLive(t, "pr", "comment", "get", pullRequestID, "--repo", selector, "")

		description, offered := candidates[plainComment]
		if !offered {
			t.Fatalf("comment %s was not offered to `bb pr comment get`; got %v", plainComment, candidates)
		}
		// Both halves, because either alone leaves a list nobody can read: the
		// author without the text is three identical rows, and the text
		// without the author does not say whose review is being answered.
		//
		// The display name rather than the username, read from the instance
		// rather than assumed: it is what a person recognises, and asserting
		// on "admin" would pass on a description that said nothing at all.
		if !strings.Contains(description, displayNameOf(t, harness, ctx, harness.username())) {
			t.Errorf("expected the comment's author beside its id, got %q", description)
		}
		if !strings.Contains(description, plainText) {
			t.Errorf("expected the comment's first line beside its id, got %q", description)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the shell to be told not to fall back to file names, got directive %d", directive)
		}
	})

	t.Run("replying offers the comment being replied to", func(t *testing.T) {
		// --parent-id hangs off the pull request named as an argument rather
		// than off a flag, which is the binding the Environment does.
		candidates, _ := completeLive(t,
			"pr", "comment", "add", pullRequestID, "--repo", selector, "--parent-id", "")

		if _, offered := candidates[plainComment]; !offered {
			t.Fatalf("comment %s was not offered to `--parent-id`; got %v", plainComment, candidates)
		}
	})

	t.Run("applying a suggestion is offered only comments that have one", func(t *testing.T) {
		candidates, _ := completeLive(t,
			"pr", "comment", "apply-suggestion", pullRequestID, "--repo", selector, "")

		if _, offered := candidates[suggestionComment]; !offered {
			t.Fatalf("the comment carrying a suggestion was not offered to `apply-suggestion`; got %v", candidates)
		}
		// The proof that the narrowing narrowed. The plain comment is in the
		// listing the subtest above reads, so its absence here is the filter
		// rather than an empty answer.
		if _, offered := candidates[plainComment]; offered {
			t.Errorf("comment %s has no suggestion to apply and was offered anyway: %v", plainComment, candidates)
		}
	})

	t.Run("reopening is offered only the resolved comment", func(t *testing.T) {
		candidates, _ := completeLive(t, "pr", "comment", "reopen", pullRequestID, "--repo", selector, "")

		if _, offered := candidates[resolvedComment]; !offered {
			t.Fatalf("the resolved comment %s was not offered to `reopen`; got %v", resolvedComment, candidates)
		}
		if _, offered := candidates[plainComment]; offered {
			t.Errorf("comment %s is not resolved and was offered to `reopen`: %v", plainComment, candidates)
		}
	})

	t.Run("resolving is offered everything that is not resolved", func(t *testing.T) {
		candidates, _ := completeLive(t, "pr", "comment", "resolve", pullRequestID, "--repo", selector, "")

		if _, offered := candidates[plainComment]; !offered {
			t.Fatalf("comment %s is unresolved and was not offered to `resolve`; got %v", plainComment, candidates)
		}
		if _, offered := candidates[resolvedComment]; offered {
			t.Errorf("comment %s is already resolved and was offered to `resolve`: %v", resolvedComment, candidates)
		}
	})

	t.Run("a commit comment is offered to the commit on the line", func(t *testing.T) {
		// Bitbucket refuses to list a commit's comments without a path, so the
		// source discovers the files the commit touched. Seeding through bb is
		// the point: a comment bb wrote has to be one bb can complete.
		const commitText = "This line is the one the completion must name."
		created := mustLiveCLIUnscoped(t, "repo", "comment", "create",
			"--repo", selector, "--commit", commitID,
			"--path", "seed.txt", "--line", "1", "--line-type", "ADDED",
			"--text", commitText)

		commentID := commentIDFromOutput(t, created)

		candidates, directive := completeLive(t,
			"repo", "comment", "delete", "--repo", selector, "--commit", commitID, "--id", "")

		description, offered := candidates[commentID]
		if !offered {
			t.Fatalf("commit comment %s was not offered to `bb repo comment delete --id`; got %v", commentID, candidates)
		}
		if !strings.Contains(description, commitText) {
			t.Errorf("expected the commit comment's text beside its id, got %q", description)
		}
		if _, offered := candidates[plainComment]; offered {
			t.Errorf("a pull request comment was offered for a commit-scoped slot: %v", candidates)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}

		// Replying takes the same ids through a different route: --path is on
		// the line here, so the source is handed the file rather than reading
		// the commit to find it.
		replyCandidates, _ := completeLive(t, "repo", "comment", "create",
			"--repo", selector, "--commit", commitID, "--path", "seed.txt", "--parent", "")

		if _, offered := replyCandidates[commentID]; !offered {
			t.Fatalf("commit comment %s was not offered to `--parent` with a path on the line; got %v",
				commentID, replyCandidates)
		}
	})

	t.Run("a required build check is offered with its refs and its checks", func(t *testing.T) {
		created, err := harness.liveJSON(ctx, http.MethodPost,
			fmt.Sprintf("/rest/required-builds/latest/projects/%s/repos/%s/condition", seeded.Key, repo.Slug),
			map[string]any{
				"buildParentKeys": []string{"completion-ci"},
				"refMatcher":      map[string]any{"id": "refs/heads/master", "type": map[string]any{"id": "BRANCH"}},
			})
		if err != nil {
			t.Fatalf("create required build check failed: %v", err)
		}

		checkID, ok := numericOrStringID(created["id"])
		if !ok {
			t.Fatalf("the created required build check has no id: %v", created)
		}

		candidates, _ := completeLive(t, "build", "required", "delete", "--repo", selector, "")

		description, offered := candidates[checkID]
		if !offered {
			t.Fatalf("required build check %s was not offered; got %v", checkID, candidates)
		}
		if !strings.Contains(description, "master") {
			t.Errorf("expected the guarded ref beside the id, got %q", description)
		}
		if !strings.Contains(description, "completion-ci") {
			t.Errorf("expected the build key beside the id, got %q", description)
		}
	})

	t.Run("removing a label offers the labels the repository has", func(t *testing.T) {
		label := strings.ToLower(testsupport.UniqueName("lt-completion-label-"))
		if _, err := harness.liveJSON(ctx, http.MethodPost,
			fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/labels", seeded.Key, repo.Slug),
			map[string]any{"name": label}); err != nil {
			t.Fatalf("add repository label failed: %v", err)
		}

		candidates, directive := completeLive(t, "repo", "label", "remove", "--repo", selector, "")

		if _, offered := candidates[label]; !offered {
			t.Fatalf("label %s was not offered to `bb repo label remove`; got %v", label, candidates)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}

		// Adding one names a label that need not exist yet, so it is declared
		// free and must not be handed the ones that already do.
		adding, _ := completeLive(t, "repo", "label", "add", "--repo", selector, "")
		if _, offered := adding[label]; offered {
			t.Errorf("`bb repo label add` was offered an existing label: %v", adding)
		}
	})
}

// TestLiveCompletionIdsChooseTheScopeTheCommandNames is the half that cannot
// be proved one scope at a time.
//
// Webhooks, branch restrictions, default tasks, reviewer conditions and SSH
// access keys are all configured on a repository and on a project, through two
// routes that answer with different ids. A source reading the wrong one offers
// a list where every entry is a 404, and the only way to see that is to seed
// both and assert that neither slot offers the other's.
func TestLiveCompletionIdsChooseTheScopeTheCommandNames(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	selector := seeded.Key + "/" + repo.Slug

	repositoryPath := func(prefix, suffix string) string {
		return fmt.Sprintf("%s/projects/%s/repos/%s%s", prefix, seeded.Key, repo.Slug, suffix)
	}
	projectPath := func(prefix, suffix string) string {
		return fmt.Sprintf("%s/projects/%s%s", prefix, seeded.Key, suffix)
	}

	seed := func(t *testing.T, what, path string, payload any) string {
		t.Helper()

		created, err := harness.liveJSON(ctx, http.MethodPost, path, payload)
		if err != nil {
			t.Fatalf("create %s failed: %v", what, err)
		}

		id, ok := numericOrStringID(created["id"])
		if !ok {
			t.Fatalf("the created %s has no id: %v", what, created)
		}

		return id
	}

	t.Run("a webhook slot offers its own scope's hooks, named and addressed", func(t *testing.T) {
		repositoryHook := seed(t, "repository webhook", repositoryPath("/rest/api/latest", "/webhooks"),
			map[string]any{
				"name":   "completion-repository-hook",
				"url":    "https://ci.example.local/repository",
				"events": []string{"repo:refs_changed"},
				"active": true,
			})
		projectHook := seed(t, "project webhook", projectPath("/rest/api/latest", "/webhooks"),
			map[string]any{
				"name":   "completion-project-hook",
				"url":    "https://ci.example.local/project",
				"events": []string{"repo:refs_changed"},
				"active": true,
			})

		candidates, directive := completeLive(t, "webhook", "delete", "--repo", selector, "")

		description, offered := candidates[repositoryHook]
		if !offered {
			t.Fatalf("repository webhook %s was not offered to `bb webhook delete`; got %v", repositoryHook, candidates)
		}
		if !strings.Contains(description, "completion-repository-hook") {
			t.Errorf("expected the webhook's name beside its id, got %q", description)
		}
		if !strings.Contains(description, "https://ci.example.local/repository") {
			t.Errorf("expected the webhook's URL beside its id, got %q", description)
		}
		if _, offered := candidates[projectHook]; offered {
			t.Errorf("the project's webhook %s was offered to a repository slot: %v", projectHook, candidates)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}

		// The second route to the same repository webhook. bb spells it twice
		// -- once at the top level and once under repo settings -- and the
		// scope has to follow the command rather than the nesting.
		settingsCandidates, _ := completeLive(t,
			"repo", "settings", "workflow", "webhooks", "delete", "--repo", selector, "")

		if _, offered := settingsCandidates[repositoryHook]; !offered {
			t.Fatalf("repository webhook %s was not offered to `bb repo settings workflow webhooks delete`; got %v",
				repositoryHook, settingsCandidates)
		}
		if _, offered := settingsCandidates[projectHook]; offered {
			t.Errorf("the project's webhook %s was offered under repo settings: %v", projectHook, settingsCandidates)
		}

		projectCandidates, _ := completeLive(t, "project", "webhook", "delete", seeded.Key, "")

		projectDescription, offered := projectCandidates[projectHook]
		if !offered {
			t.Fatalf("project webhook %s was not offered to `bb project webhook delete`; got %v",
				projectHook, projectCandidates)
		}
		if !strings.Contains(projectDescription, "completion-project-hook") {
			t.Errorf("expected the project webhook's name beside its id, got %q", projectDescription)
		}
		if _, offered := projectCandidates[repositoryHook]; offered {
			t.Errorf("the repository's webhook %s was offered to a project slot: %v", repositoryHook, projectCandidates)
		}
	})

	t.Run("a branch restriction slot offers its own scope's restrictions", func(t *testing.T) {
		repositoryRestriction := seed(t, "repository restriction",
			repositoryPath("/rest/branch-permissions/latest", "/restrictions"),
			map[string]any{
				"type":    "read-only",
				"matcher": map[string]any{"id": "refs/heads/completion-repo", "type": map[string]any{"id": "BRANCH"}},
				"users":   []string{},
				"groups":  []string{},
			})
		projectRestriction := seed(t, "project restriction",
			projectPath("/rest/branch-permissions/latest", "/restrictions"),
			map[string]any{
				"type":    "read-only",
				"matcher": map[string]any{"id": "refs/heads/completion-project", "type": map[string]any{"id": "BRANCH"}},
				"users":   []string{},
				"groups":  []string{},
			})

		candidates, _ := completeLive(t, "branch", "restriction", "delete", "--repo", selector, "")

		description, offered := candidates[repositoryRestriction]
		if !offered {
			t.Fatalf("repository restriction %s was not offered; got %v", repositoryRestriction, candidates)
		}
		if !strings.Contains(description, "read-only") {
			t.Errorf("expected the restriction's type beside its id, got %q", description)
		}
		if !strings.Contains(description, "completion-repo") {
			t.Errorf("expected the restriction's matcher beside its id, got %q", description)
		}
		if _, offered := candidates[projectRestriction]; offered {
			t.Errorf("the project's restriction %s was offered to a repository slot: %v",
				projectRestriction, candidates)
		}

		projectCandidates, _ := completeLive(t, "project", "branch-restriction", "delete", seeded.Key, "")

		projectDescription, offered := projectCandidates[projectRestriction]
		if !offered {
			t.Fatalf("project restriction %s was not offered; got %v", projectRestriction, projectCandidates)
		}
		if !strings.Contains(projectDescription, "completion-project") {
			t.Errorf("expected the project restriction's matcher beside its id, got %q", projectDescription)
		}
		if _, offered := projectCandidates[repositoryRestriction]; offered {
			t.Errorf("the repository's restriction %s was offered to a project slot: %v",
				repositoryRestriction, projectCandidates)
		}
	})

	t.Run("a default task slot offers its own scope's tasks, by their text", func(t *testing.T) {
		const repositoryTaskText = "Completion repository task"
		const projectTaskText = "Completion project task"

		// Both matchers are required on a default task: without them the API
		// answers "A sourceMatcher with ID and type is required".
		task := func(text string) map[string]any {
			return map[string]any{
				"description":   text,
				"sourceMatcher": anyRefMatcherPayload,
				"targetMatcher": anyRefMatcherPayload,
			}
		}

		repositoryTask := seed(t, "repository default task",
			repositoryPath("/rest/default-tasks/latest", "/tasks"), task(repositoryTaskText))
		projectTask := seed(t, "project default task",
			projectPath("/rest/default-tasks/latest", "/tasks"), task(projectTaskText))

		candidates, _ := completeLive(t, "repo", "default-task", "delete", "--repo", selector, "")

		description, offered := candidates[repositoryTask]
		if !offered {
			t.Fatalf("repository default task %s was not offered; got %v", repositoryTask, candidates)
		}
		// The whole description, not a substring of it: an any-ref matcher
		// comes back spelled ANY_REF_MATCHER_ID, and a description reading
		// "Completion repository task (from ANY_REF_MATCHER_ID to
		// ANY_REF_MATCHER_ID)" would pass a Contains check while saying the
		// task is scoped to a branch nobody has.
		if description != repositoryTaskText {
			t.Errorf("expected the task's own text and nothing else beside its id, got %q", description)
		}
		if _, offered := candidates[projectTask]; offered {
			t.Errorf("the project's default task %s was offered to a repository slot: %v", projectTask, candidates)
		}

		projectCandidates, _ := completeLive(t, "project", "default-task", "delete", seeded.Key, "")

		projectDescription, offered := projectCandidates[projectTask]
		if !offered {
			t.Fatalf("project default task %s was not offered; got %v", projectTask, projectCandidates)
		}
		if !strings.Contains(projectDescription, projectTaskText) {
			t.Errorf("expected the project task's own text beside its id, got %q", projectDescription)
		}
		if _, offered := projectCandidates[repositoryTask]; offered {
			t.Errorf("the repository's default task %s was offered to a project slot: %v",
				repositoryTask, projectCandidates)
		}
	})

	t.Run("a reviewer condition slot follows the flag the command reads", func(t *testing.T) {
		// The administrator: the endpoint takes a reviewer by numeric id and
		// 404s on anybody who cannot see the project, so a fresh user would
		// have to be granted both scopes before it would take them.
		reviewerID, err := harness.userID(ctx, harness.username())
		if err != nil {
			t.Fatalf("look up the reviewer id: %v", err)
		}

		condition := func(target string) map[string]any {
			return map[string]any{
				"sourceMatcher":     anyRefMatcherPayload,
				"targetMatcher":     map[string]any{"id": target, "type": map[string]any{"id": "BRANCH"}},
				"reviewers":         []map[string]any{{"id": reviewerID}},
				"requiredApprovals": 1,
			}
		}

		repositoryCondition := seed(t, "repository reviewer condition",
			repositoryPath("/rest/default-reviewers/latest", "/condition"),
			condition("refs/heads/master"))
		projectCondition := seed(t, "project reviewer condition",
			projectPath("/rest/default-reviewers/latest", "/condition"),
			condition("refs/heads/completion-project-condition"))

		candidates, _ := completeLive(t, "reviewer", "condition", "delete", "--repo", selector, "")

		description, offered := candidates[repositoryCondition]
		if !offered {
			t.Fatalf("repository reviewer condition %s was not offered; got %v", repositoryCondition, candidates)
		}
		if !strings.Contains(description, "master") {
			t.Errorf("expected the refs the condition matches beside its id, got %q", description)
		}
		// The source matcher of both conditions is the any-ref one, which
		// Bitbucket echoes back as a constant rather than as a ref.
		if strings.Contains(description, "ANY_REF") {
			t.Errorf("the any-ref matcher leaked into the description as a ref name: %q", description)
		}
		if _, offered := candidates[projectCondition]; offered {
			t.Errorf("the project's condition %s was offered to a --repo slot: %v", projectCondition, candidates)
		}

		// The same command with --project instead. bb reads --repo first and
		// falls back to the project, so the flag is the whole difference.
		projectCandidates, _ := completeLive(t, "reviewer", "condition", "delete", "--project", seeded.Key, "")

		projectDescription, offered := projectCandidates[projectCondition]
		if !offered {
			t.Fatalf("project reviewer condition %s was not offered to a --project slot; got %v",
				projectCondition, projectCandidates)
		}
		if !strings.Contains(projectDescription, "completion-project-condition") {
			t.Errorf("expected the project condition's refs beside its id, got %q", projectDescription)
		}
		if _, offered := projectCandidates[repositoryCondition]; offered {
			t.Errorf("the repository's condition %s was offered to a --project slot: %v",
				repositoryCondition, projectCandidates)
		}
	})

	t.Run("an access key slot follows the flag bb repo ssh-key reads", func(t *testing.T) {
		repositoryLabel := testsupport.UniqueName("lt-completion-repo-key-")
		projectLabel := testsupport.UniqueName("lt-completion-project-key-")

		repositoryKey := seededAccessKeyID(t, harness, ctx,
			repositoryPath("/rest/keys/latest", "/ssh"),
			generateSSHPublicKey(t, repositoryLabel), "REPO_READ")
		projectKey := seededAccessKeyID(t, harness, ctx,
			projectPath("/rest/keys/latest", "/ssh"),
			generateSSHPublicKey(t, projectLabel), "PROJECT_READ")

		candidates, _ := completeLive(t, "repo", "ssh-key", "remove", "--repo", selector, "")

		description, offered := candidates[repositoryKey]
		if !offered {
			t.Fatalf("repository access key %s was not offered; got %v", repositoryKey, candidates)
		}
		if !strings.Contains(description, repositoryLabel) {
			t.Errorf("expected the key's label beside its id, got %q", description)
		}
		if !strings.Contains(description, "REPO_READ") {
			t.Errorf("expected the key's permission beside its id, got %q", description)
		}
		if _, offered := candidates[projectKey]; offered {
			t.Errorf("the project's access key %s was offered to a --repo slot: %v", projectKey, candidates)
		}

		projectCandidates, _ := completeLive(t, "repo", "ssh-key", "remove", "--project", seeded.Key, "")

		projectDescription, offered := projectCandidates[projectKey]
		if !offered {
			t.Fatalf("project access key %s was not offered to a --project slot; got %v",
				projectKey, projectCandidates)
		}
		if !strings.Contains(projectDescription, projectLabel) {
			t.Errorf("expected the project key's label beside its id, got %q", projectDescription)
		}
		if _, offered := projectCandidates[repositoryKey]; offered {
			t.Errorf("the repository's access key %s was offered to a --project slot: %v",
				repositoryKey, projectCandidates)
		}

		// The same keys reached through the other slot that takes them: a
		// branch restriction lets an access key past, and the restriction
		// commands are scoped the same way the webhooks are.
		restrictionCandidates, _ := completeLive(t,
			"branch", "restriction", "create", "--repo", selector, "--access-key-id", "")

		if _, offered := restrictionCandidates[repositoryKey]; !offered {
			t.Fatalf("repository access key %s was not offered to `--access-key-id`; got %v",
				repositoryKey, restrictionCandidates)
		}
		if _, offered := restrictionCandidates[projectKey]; offered {
			t.Errorf("the project's access key %s was offered to a repository restriction: %v",
				projectKey, restrictionCandidates)
		}
	})
}

// anyRefMatcherPayload is the matcher that covers every ref, which both the
// default-task and the default-reviewer endpoints require rather than infer.
var anyRefMatcherPayload = map[string]any{"id": "ANY_REF", "type": map[string]any{"id": "ANY_REF"}}

// displayNameOf is the name a person is shown by, asked of the instance.
//
// Asked rather than assumed: the administrator's display name is not their
// username, and a description asserted against the username would pass on one
// that named nobody.
func displayNameOf(t *testing.T, harness *liveHarness, ctx context.Context, username string) string {
	t.Helper()

	user, err := harness.liveJSON(ctx, http.MethodGet, "/rest/api/latest/users/"+username, nil)
	if err != nil {
		t.Fatalf("look up %s failed: %v", username, err)
	}

	displayName := asString(user["displayName"])
	if strings.TrimSpace(displayName) == "" {
		t.Fatalf("%s has no display name to assert on: %v", username, user)
	}

	return displayName
}

// addPullRequestComment writes a comment through bb and returns its id.
//
// Through bb rather than the REST API because the round trip is the point: a
// comment bb created has to be one bb completes.
func addPullRequestComment(t *testing.T, selector, pullRequestID, text string, extra ...string) string {
	t.Helper()

	args := append([]string{"pr", "comment", "add", pullRequestID, "--repo", selector, "--text", text}, extra...)

	return commentIDFromOutput(t, mustLiveCLIUnscoped(t, args...))
}

// commentIDFromOutput reads the id out of whichever envelope wrote the comment.
func commentIDFromOutput(t *testing.T, output string) string {
	t.Helper()

	comment, ok := decodeJSONMap(t, output)["comment"].(map[string]any)
	if !ok {
		t.Fatalf("expected a comment in the output: %s", output)
	}

	id, ok := numericOrStringID(comment["id"])
	if !ok {
		t.Fatalf("the created comment has no id: %s", output)
	}

	return id
}

// seededAccessKeyID adds an SSH access key and returns the id the remove
// command takes, which is nested under key rather than at the top level.
func seededAccessKeyID(
	t *testing.T,
	harness *liveHarness,
	ctx context.Context,
	path string,
	publicKey string,
	permission string,
) string {
	t.Helper()

	created, err := harness.liveJSON(ctx, http.MethodPost, path, map[string]any{
		"key":        map[string]any{"text": publicKey},
		"permission": permission,
	})
	if err != nil {
		t.Fatalf("create access key at %s failed: %v", path, err)
	}

	key, ok := created["key"].(map[string]any)
	if !ok {
		t.Fatalf("the created access key has no key object: %v", created)
	}

	id, ok := numericOrStringID(key["id"])
	if !ok {
		t.Fatalf("the created access key has no id: %v", created)
	}

	return id
}
