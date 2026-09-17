//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
)

// TestLiveMCPEveryToolReturnsAClientCompatibleResult calls every tool in the
// catalogue against a real Bitbucket and checks the two things about the
// result that the SDK does not.
//
// The SDK derives each tool's output schema from its handler's Out type and
// validates the marshalled result before it leaves the process, so schema
// conformance is the framework's job. What it does not check is what issue
// #416 was filed about:
//
//   - structuredContent must be a JSON object. A schema saying "array" and a
//     result that is an array both satisfy the SDK, and a pre-SEP-2106 client
//     rejects the response with "expected record, received array" before it can
//     read the text fallback.
//   - a text fallback must exist, for clients that do not read
//     structuredContent at all.
//
// This is also the only place every handler body runs, which is why it belongs
// here rather than against a stub. The stub it replaces answered every route
// with the thinnest valid shape, so each handler ran against a payload written
// to make it succeed: an empty page for every listing, one echoed entity for
// every write. What a handler does with a pull request that has reviewers, a
// diff with real hunks, or a build status Bitbucket actually stored was never
// reached. Every argument below names something the harness created.
//
// A result of the right shape is not a result about the right thing, and an
// answer of 2xx is not a write that was stored: Bitbucket ignores a property it
// does not know. So a read's answer is checked against what it asked for, and
// every write is read back after the sweep.
func TestLiveMCPEveryToolReturnsAClientCompatibleResult(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fixture := seedMCPToolArguments(t, ctx, harness)
	arguments := fixture.arguments

	// Every spec needs an entry, or a tool added to the catalogue would never
	// be called at all -- which is how the shape bug reached a client.
	specs := bbmcp.AllSpecs()
	named := make(map[string]bool, len(specs))
	for _, spec := range specs {
		named[spec.Tool.Name] = true
		if _, ok := arguments[spec.Tool.Name]; !ok {
			t.Errorf("tool %q has no live arguments; add a fixture so the conformance sweep calls it", spec.Tool.Name)
		}
	}
	for name := range arguments {
		if !named[name] {
			t.Errorf("live arguments name %q, which is not in AllSpecs", name)
		}
	}
	if t.Failed() {
		return
	}

	// Sorted, so the sweep runs in the same order whatever order AllSpecs
	// returns. Nothing below depends on another tool having run -- each
	// mutating tool has its own pull request -- but a fixed order makes a
	// failure reproducible.
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Tool.Name)
	}
	sort.Strings(names)

	// What each tool answered, for reading back what the writes stored once
	// the server is gone.
	answers := make(map[string]map[string]any, len(names))

	// --yolo, because half the catalogue is withheld without it, and a tool
	// that is never called is a tool whose result shape is never checked.
	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()

		for _, name := range names {
			t.Run(name, func(t *testing.T) {
				result, callErr := session.CallTool(callCtx, &mcp.CallToolParams{
					Name:      name,
					Arguments: arguments[name],
				})
				if callErr != nil {
					t.Fatalf("tools/call returned a protocol error: %v", callErr)
				}
				if result.IsError {
					t.Fatalf("tools/call returned an error result: %s", mcpResultText(result))
				}
				if result.StructuredContent == nil {
					t.Fatal("result carries no structuredContent")
				}

				// Re-encode and decode so the check runs against the JSON a
				// client actually receives, not a Go value that might marshal
				// differently.
				encoded, marshalErr := json.Marshal(result.StructuredContent)
				if marshalErr != nil {
					t.Fatalf("marshal structuredContent: %v", marshalErr)
				}
				var decoded any
				if err := json.Unmarshal(encoded, &decoded); err != nil {
					t.Fatalf("structuredContent is not valid JSON: %v", err)
				}
				answer, ok := decoded.(map[string]any)
				if !ok {
					t.Fatalf("structuredContent is %T, want a JSON object: %s", decoded, encoded)
				}

				if len(result.Content) == 0 {
					t.Error("result carries no text content fallback")
				}

				answers[name] = answer
				assertMCPToolAnswer(t, fixture, name, answer)
			})
		}
	}, "ai", "mcp", "serve", "--yolo")

	assertMCPToolWritesStored(t, fixture, answers)
}

// mcpToolFixture is what the conformance sweep calls the tools against: the
// arguments for each tool, and what the checks on their answers and writes
// need to know.
type mcpToolFixture struct {
	arguments map[string]map[string]any

	repoRef, autoMergeRef string
	// username is the account the server runs as.
	username string

	mainPR, mergePR, reviewPR, armPR, cancelPR string
	// mainTip is the one commit feature/mcp-main adds to master.
	mainTip string
	// commitID is master's tip and olderCommit its parent. Tags are made at the
	// parent, because a tag made at the tip is what a start point Bitbucket
	// never received would produce too.
	commitID, olderCommit string
}

// seedMCPToolArguments creates everything the sweep calls the tools against,
// reads it back, and returns the argument set for each tool.
//
// Each mutating tool gets its own pull request, so no call depends on another
// having run first. Three of them could not share one anyway: merging closes
// the pull request it is given, arming auto-merge on one nothing blocks merges
// it on the spot, and Bitbucket refuses the author's own review outright --
// "Authors may not update their status", 400 -- so the pull request
// submit_pr_review is given has to be somebody else's.
func seedMCPToolArguments(t *testing.T, ctx context.Context, harness *liveHarness) mcpToolFixture {
	t.Helper()

	seeded, err := harness.seedRepo(ctx, repoSeed{Repos: 2, Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	autoMergeRepo := seeded.Repos[1]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	fixture := mcpToolFixture{
		repoRef:      seeded.Key + "/" + repo.Slug,
		autoMergeRef: seeded.Key + "/" + autoMergeRepo.Slug,
		username:     harness.username(),
		olderCommit:  repo.CommitIDs[1],
	}

	pushBranch := func(slug, branch, file string) {
		t.Helper()
		if err := harness.pushCommitOnBranch(seeded.Key, slug, branch, file); err != nil {
			t.Fatalf("push %s on %s failed: %v", branch, slug, err)
		}
	}

	// The pull request the read tools and the in-place writes work on.
	pushBranch(repo.Slug, "feature/mcp-main", "mcp-main.txt")
	fixture.mainPR, err = harness.createPullRequest(ctx, seeded.Key, repo.Slug, "feature/mcp-main", "master")
	if err != nil {
		t.Fatalf("create the main pull request failed: %v", err)
	}
	assertLifecyclePRHarnessStored(t, fixture.mainPR, "feature/mcp-main", "master")
	fixture.mainTip = mutatedBranchTip(t, "feature/mcp-main")
	mainVersion := livePRVersion(t, ctx, harness, seeded.Key, repo.Slug, fixture.mainPR)

	// A branch with no pull request on it, for create_pull_request to open one,
	// and a branch other than the default for it to target: a target the tool
	// dropped would be the default branch.
	pushBranch(repo.Slug, "feature/mcp-create", "mcp-create.txt")
	pushBranch(repo.Slug, "mcp-create-base", "mcp-create-base.txt")

	// A pull request to merge, and a branch of its own to merge into.
	//
	// Not master: merging into a branch another pull request targets moves that
	// pull request forward and bumps its version, which would make the version
	// update_pull_request was handed stale by the time the sweep reaches it.
	pushBranch(repo.Slug, "mcp-merge-base", "mcp-merge-base.txt")
	pushBranch(repo.Slug, "feature/mcp-merge", "mcp-merge.txt")
	fixture.mergePR, err = harness.createPullRequest(ctx, seeded.Key, repo.Slug, "feature/mcp-merge", "mcp-merge-base")
	if err != nil {
		t.Fatalf("create the pull request to merge failed: %v", err)
	}
	assertLifecyclePRHarnessStored(t, fixture.mergePR, "feature/mcp-merge", "mcp-merge-base")

	// Somebody else's pull request, so the server's own account can review it.
	author, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the pull request author failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, author.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant the author write access failed: %v", err)
	}
	pushBranch(repo.Slug, "feature/mcp-review", "mcp-review.txt")
	authored, err := harness.liveJSONAs(ctx, author, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", seeded.Key, repo.Slug),
		map[string]any{
			"title":     "Reviewed by the MCP conformance sweep",
			"fromRef":   map[string]any{"id": "refs/heads/feature/mcp-review"},
			"toRef":     map[string]any{"id": "refs/heads/master"},
			"reviewers": []map[string]any{{"user": map[string]any{"name": fixture.username}}},
		})
	if err != nil {
		t.Fatalf("create the authored pull request failed: %v", err)
	}
	fixture.reviewPR = fmt.Sprintf("%v", int64(authored["id"].(float64)))
	reviewed := mcpLivePullRequest(t, fixture.repoRef, fixture.reviewPR)
	assertLifecyclePRStored(t, reviewed, map[string]any{
		"title": "Reviewed by the MCP conformance sweep", "sourceBranch": "feature/mcp-review", "targetBranch": "master",
	})
	// Unapproved, so an approval that was not stored cannot pass for one.
	if reviewer := mcpLiveReviewer(t, reviewed, fixture.username); reviewer["role"] != "REVIEWER" || reviewer["status"] != "UNAPPROVED" {
		t.Fatalf("%s is %v/%v on pull request %s, want an UNAPPROVED REVIEWER", fixture.username, reviewer["role"], reviewer["status"], fixture.reviewPR)
	}

	// Auto-merge lives in its own repository so the approver requirement that
	// keeps a pull request from merging on the spot does not also block the
	// merge_pull_request call above.
	mustLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", fixture.autoMergeRef)
	if settings := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "auto-merge", "get", "--repo", fixture.autoMergeRef)); settings["enabled"] != true {
		t.Fatalf("auto-merge on %s reads back as enabled=%v, want true", fixture.autoMergeRef, settings["enabled"])
	}
	mustLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", fixture.autoMergeRef)
	if got := approverCountFrom(t, decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get", "--repo", fixture.autoMergeRef))); got != "1" {
		t.Fatalf("requiredApprovers on %s reads back as %s, want 1", fixture.autoMergeRef, got)
	}

	pushBranch(autoMergeRepo.Slug, "feature/mcp-arm", "mcp-arm.txt")
	fixture.armPR, err = harness.createPullRequest(ctx, seeded.Key, autoMergeRepo.Slug, "feature/mcp-arm", "master")
	if err != nil {
		t.Fatalf("create the pull request to arm failed: %v", err)
	}
	assertLifecyclePRStored(t, readLifecyclePRIn(t, fixture.autoMergeRef, fixture.armPR), map[string]any{
		"sourceBranch": "feature/mcp-arm", "targetBranch": "master",
	})

	// disable_auto_merge cancels a request, so there has to be one to cancel.
	// Arming it here rather than in the sweep keeps the two calls independent.
	pushBranch(autoMergeRepo.Slug, "feature/mcp-cancel", "mcp-cancel.txt")
	fixture.cancelPR, err = harness.createPullRequest(ctx, seeded.Key, autoMergeRepo.Slug, "feature/mcp-cancel", "master")
	if err != nil {
		t.Fatalf("create the pull request to cancel failed: %v", err)
	}
	mustLiveCLI(t, "pr", "auto-merge", "enable", fixture.cancelPR, "--repo", fixture.autoMergeRef)
	if autoMerge := mcpLiveAutoMerge(t, fixture.autoMergeRef, fixture.cancelPR); autoMerge["enabled"] != true {
		t.Fatalf("auto-merge on pull request %s reads back as %v before the sweep, so cancelling it would show nothing", fixture.cancelPR, autoMerge)
	}

	// A commit the build tools can hang a status on, read out of the
	// repository rather than invented.
	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("list commit ids failed: %v (%d commits)", err, len(commits))
	}
	fixture.commitID = commits[0]
	mustLiveCLI(t, "build", "status", "set", fixture.commitID,
		"--key", "mcp-conformance", "--state", "SUCCESSFUL", "--url", "https://ci.example.com/1")
	commandCoverageAssertFields(t, "the build status get_build_status reads", mcpLiveBuildStatuses(t, fixture.commitID)["mcp-conformance"],
		map[string]any{"state": "SUCCESSFUL", "url": "https://ci.example.com/1"})

	// A tag, so list_tags answers with one rather than an empty page.
	mustLiveCLI(t, "tag", "create", "v0.0.1-mcp", "--start-point", fixture.olderCommit, "--repo", fixture.repoRef)
	commandCoverageAssertFields(t, "the tag list_tags reads", decodeJSONMap(t, mustLiveCLI(t, "tag", "view", "v0.0.1-mcp")),
		map[string]any{"displayId": "v0.0.1-mcp", "latestCommit": fixture.olderCommit})

	repoArgs := func(extra map[string]any) map[string]any {
		args := map[string]any{"project": seeded.Key, "repo": repo.Slug}
		for key, value := range extra {
			args[key] = value
		}
		return args
	}

	fixture.arguments = map[string]map[string]any{
		"get_pull_request":   repoArgs(map[string]any{"id": fixture.mainPR}),
		"list_pull_requests": repoArgs(nil),
		"create_pull_request": repoArgs(map[string]any{
			"from_ref": "feature/mcp-create", "to_ref": "mcp-create-base", "title": "Opened by the conformance sweep",
			"description": "Opened with a description of its own",
		}),
		"update_pull_request": repoArgs(map[string]any{
			"pr_id": fixture.mainPR, "version": mainVersion, "title": "Renamed by the conformance sweep",
		}),
		"list_pr_comments":          repoArgs(map[string]any{"pr_id": fixture.mainPR}),
		"get_pr_diff":               repoArgs(map[string]any{"pr_id": fixture.mainPR}),
		"get_file_content":          repoArgs(map[string]any{"path": "mcp-main.txt", "at": "feature/mcp-main"}),
		"add_pr_comment":            repoArgs(map[string]any{"pr_id": fixture.mainPR, "text": "left by the conformance sweep"}),
		"submit_pr_review":          repoArgs(map[string]any{"pr_id": fixture.reviewPR, "action": "approve"}),
		"merge_pull_request":        repoArgs(map[string]any{"pr_id": fixture.mergePR}),
		"enable_auto_merge":         {"project": seeded.Key, "repo": autoMergeRepo.Slug, "pr_id": fixture.armPR},
		"disable_auto_merge":        {"project": seeded.Key, "repo": autoMergeRepo.Slug, "pr_id": fixture.cancelPR},
		"search_repositories":       {"project": seeded.Key},
		"get_repository_clone_info": repoArgs(nil),
		"list_branches":             repoArgs(nil),
		"resolve_ref":               repoArgs(map[string]any{"ref": "master"}),
		"list_tags":                 repoArgs(nil),
		"create_tag":                repoArgs(map[string]any{"name": "v0.0.2-mcp", "start_point": fixture.olderCommit}),
		"get_build_status":          {"commit_id": fixture.commitID},
		"set_build_status": {
			"commit_id": fixture.commitID, "key": "mcp-conformance-2", "state": "SUCCESSFUL", "url": "https://ci.example.com/2",
		},
		"list_required_builds": repoArgs(nil),
		"list_commits":         repoArgs(nil),
		"get_commit":           repoArgs(map[string]any{"commit_id": fixture.commitID}),
		"compare_refs":         repoArgs(map[string]any{"from": "feature/mcp-main", "to": "master"}),
	}

	return fixture
}

// assertMCPToolAnswer checks that a read tool answered about what its arguments
// named. Only the tools whose answer can show that are here: a listing of the
// whole repository has nothing to narrow.
func assertMCPToolAnswer(t *testing.T, fixture mcpToolFixture, name string, answer map[string]any) {
	t.Helper()

	switch name {
	case "get_pull_request":
		pullRequest, _ := answer["pull_request"].(map[string]any)
		if id, _ := numericOrStringID(pullRequest["id"]); id != fixture.mainPR || pullRequest["source_branch"] != "feature/mcp-main" {
			t.Errorf("get_pull_request %s answered with pull request %v from %v", fixture.mainPR, pullRequest["id"], pullRequest["source_branch"])
		}
	case "get_pr_diff":
		diff, _ := answer["diff"].(map[string]any)
		if files := commandCoverageDiffFiles(asString(diff["patch"])); !slices.Equal(files, []string{"mcp-main.txt"}) {
			t.Errorf("get_pr_diff %s changes %v, want [mcp-main.txt]", fixture.mainPR, files)
		}
	case "get_file_content":
		// The file is on feature/mcp-main alone, so a read that lost the ref
		// would not have found it.
		if answer["path"] != "mcp-main.txt" || answer["content"] != "branch=feature/mcp-main\n" {
			t.Errorf("get_file_content answered with %v holding %q, want mcp-main.txt holding %q", answer["path"], answer["content"], "branch=feature/mcp-main\n")
		}
	case "get_commit":
		if commit, _ := answer["commit"].(map[string]any); commit["id"] != fixture.commitID {
			t.Errorf("get_commit %s answered with commit %v", fixture.commitID, commit["id"])
		}
	case "compare_refs":
		// What feature/mcp-main has that master does not: its one commit.
		commits, _ := answer["commits"].([]any)
		ids := make([]string, 0, len(commits))
		for _, entry := range commits {
			commit, _ := entry.(map[string]any)
			ids = append(ids, asString(commit["id"]))
		}
		if !slices.Equal(ids, []string{fixture.mainTip}) {
			t.Errorf("compare_refs from feature/mcp-main to master listed %v, want [%s]", ids, fixture.mainTip)
		}
	case "resolve_ref":
		refs, _ := answer["refs"].([]any)
		names := make([]string, 0, len(refs))
		for _, entry := range refs {
			ref, _ := entry.(map[string]any)
			names = append(names, asString(ref["displayId"]))
		}
		if !slices.Equal(names, []string{"master"}) {
			t.Errorf("resolve_ref master resolved %v, want [master]", names)
		}
	case "get_build_status":
		statuses, _ := answer["build_statuses"].([]any)
		commandCoverageAssertFields(t, "the build status get_build_status answered with", mcpAnswerEntry(t, statuses, "key", "mcp-conformance"),
			map[string]any{"state": "SUCCESSFUL", "url": "https://ci.example.com/1"})
	case "list_tags":
		tags, _ := answer["tags"].([]any)
		commandCoverageAssertFields(t, "the tag list_tags answered with", mcpAnswerEntry(t, tags, "displayId", "v0.0.1-mcp"),
			map[string]any{"latestCommit": fixture.olderCommit})
	}
}

// assertMCPToolWritesStored reads back, through bb, what each write the sweep
// made stored. A write whose call failed has failed its own subtest already and
// is not read back.
func assertMCPToolWritesStored(t *testing.T, fixture mcpToolFixture, answers map[string]map[string]any) {
	t.Helper()

	called := func(name string) bool {
		_, ok := answers[name]
		return ok
	}

	// answeredID is the id of the entity a write answered with, which is the
	// only way to find what it created.
	answeredID := func(name, entity string) (string, bool) {
		fields, _ := answers[name][entity].(map[string]any)
		id, ok := numericOrStringID(fields["id"])
		if !ok {
			t.Errorf("%s answered with no %s id to read back: %v", name, entity, answers[name])
		}
		return id, ok
	}

	if called("create_pull_request") {
		if id, ok := answeredID("create_pull_request", "pull_request"); ok {
			assertLifecyclePRStored(t, readLifecyclePR(t, id), map[string]any{
				"title": "Opened by the conformance sweep", "description": "Opened with a description of its own",
				"sourceBranch": "feature/mcp-create", "targetBranch": "mcp-create-base",
			})
		}
	}

	if called("update_pull_request") {
		// The description was not sent, and leaving it as it was is the tool's
		// promise.
		assertLifecyclePRStored(t, readLifecyclePR(t, fixture.mainPR), map[string]any{
			"title": "Renamed by the conformance sweep", "description": "PR seeded by live harness",
		})
	}

	if called("add_pr_comment") {
		if id, ok := answeredID("add_pr_comment", "comment"); ok {
			if stored := repoCLIPRComment(t, fixture.mainPR, id); stored["text"] != "left by the conformance sweep" {
				t.Errorf("comment %s on pull request %s reads back as %q, want %q", id, fixture.mainPR, stored["text"], "left by the conformance sweep")
			}
		}
	}

	if called("submit_pr_review") {
		if reviewer := mcpLiveReviewer(t, mcpLivePullRequest(t, fixture.repoRef, fixture.reviewPR), fixture.username); reviewer["status"] != "APPROVED" {
			t.Errorf("%s's review of pull request %s reads back as %v after submit_pr_review approve", fixture.username, fixture.reviewPR, reviewer["status"])
		}
	}

	if called("merge_pull_request") {
		assertLifecyclePRStored(t, readLifecyclePR(t, fixture.mergePR), map[string]any{"state": "MERGED", "targetBranch": "mcp-merge-base"})
	}

	if called("enable_auto_merge") {
		// Armed and waiting on the approval the repository requires.
		if autoMerge := mcpLiveAutoMerge(t, fixture.autoMergeRef, fixture.armPR); autoMerge["enabled"] != true {
			t.Errorf("auto-merge on pull request %s reads back as %v after enable_auto_merge", fixture.armPR, autoMerge)
		}
		assertLifecyclePRStored(t, readLifecyclePRIn(t, fixture.autoMergeRef, fixture.armPR), map[string]any{"state": "OPEN"})
	}

	if called("disable_auto_merge") {
		if autoMerge := mcpLiveAutoMerge(t, fixture.autoMergeRef, fixture.cancelPR); autoMerge["enabled"] != false {
			t.Errorf("auto-merge on pull request %s reads back as %v after disable_auto_merge", fixture.cancelPR, autoMerge)
		}
	}

	if called("create_tag") {
		commandCoverageAssertFields(t, "the tag create_tag made", decodeJSONMap(t, mustLiveCLI(t, "tag", "view", "v0.0.2-mcp")),
			map[string]any{"displayId": "v0.0.2-mcp", "latestCommit": fixture.olderCommit})
	}

	if called("set_build_status") {
		commandCoverageAssertFields(t, "the build status set_build_status set", mcpLiveBuildStatuses(t, fixture.commitID)["mcp-conformance-2"],
			map[string]any{"key": "mcp-conformance-2", "state": "SUCCESSFUL", "url": "https://ci.example.com/2"})
	}
}

// mcpAnswerEntry finds the entry of a tool's listing whose field holds value.
func mcpAnswerEntry(t *testing.T, entries []any, field, value string) map[string]any {
	t.Helper()

	for _, entry := range entries {
		if fields, ok := entry.(map[string]any); ok && fields[field] == value {
			return fields
		}
	}
	t.Fatalf("no entry with %s=%s in the answer: %v", field, value, entries)

	return nil
}

// livePRVersion reads the version Bitbucket currently holds for a pull request,
// which update_pull_request has to send back.
func livePRVersion(t *testing.T, ctx context.Context, harness *liveHarness, projectKey, slug, pullRequestID string) int {
	t.Helper()

	payload, err := harness.liveJSON(ctx, http.MethodGet,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s", projectKey, slug, pullRequestID), nil)
	if err != nil {
		t.Fatalf("read pull request %s failed: %v", pullRequestID, err)
	}
	version, ok := payload["version"].(float64)
	if !ok {
		t.Fatalf("pull request %s carries no version: %v", pullRequestID, payload)
	}
	return int(version)
}
