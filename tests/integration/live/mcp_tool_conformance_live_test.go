//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
func TestLiveMCPEveryToolReturnsAClientCompatibleResult(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	arguments := seedMCPToolArguments(t, ctx, harness)

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
				if _, ok := decoded.(map[string]any); !ok {
					t.Fatalf("structuredContent is %T, want a JSON object: %s", decoded, encoded)
				}

				if len(result.Content) == 0 {
					t.Error("result carries no text content fallback")
				}
			})
		}
	}, "ai", "mcp", "serve", "--yolo")
}

// seedMCPToolArguments creates everything the sweep calls the tools against and
// returns the argument set for each one.
//
// Each mutating tool gets its own pull request, so no call depends on another
// having run first. Three of them could not share one anyway: merging closes
// the pull request it is given, arming auto-merge on one nothing blocks merges
// it on the spot, and Bitbucket refuses the author's own review outright --
// "Authors may not update their status", 400 -- so the pull request
// submit_pr_review is given has to be somebody else's.
func seedMCPToolArguments(t *testing.T, ctx context.Context, harness *liveHarness) map[string]map[string]any {
	t.Helper()

	seeded, err := harness.seedProjectWithRepositories(ctx, 2, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	autoMergeRepo := seeded.Repos[1]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	pushBranch := func(slug, branch, file string) {
		t.Helper()
		if err := harness.pushCommitOnBranch(seeded.Key, slug, branch, file); err != nil {
			t.Fatalf("push %s on %s failed: %v", branch, slug, err)
		}
	}

	// The pull request the read tools and the in-place writes work on.
	pushBranch(repo.Slug, "feature/mcp-main", "mcp-main.txt")
	mainPR, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "feature/mcp-main", "master")
	if err != nil {
		t.Fatalf("create the main pull request failed: %v", err)
	}
	mainVersion := livePRVersion(t, ctx, harness, seeded.Key, repo.Slug, mainPR)

	// A branch with no pull request on it, for create_pull_request to open one.
	pushBranch(repo.Slug, "feature/mcp-create", "mcp-create.txt")

	// A pull request to merge, and a branch of its own to merge into.
	//
	// Not master: merging into a branch another pull request targets moves that
	// pull request forward and bumps its version, which would make the version
	// update_pull_request was handed stale by the time the sweep reaches it.
	pushBranch(repo.Slug, "mcp-merge-base", "mcp-merge-base.txt")
	pushBranch(repo.Slug, "feature/mcp-merge", "mcp-merge.txt")
	mergePR, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "feature/mcp-merge", "mcp-merge-base")
	if err != nil {
		t.Fatalf("create the pull request to merge failed: %v", err)
	}

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
			"reviewers": []map[string]any{{"user": map[string]any{"name": harness.username()}}},
		})
	if err != nil {
		t.Fatalf("create the authored pull request failed: %v", err)
	}
	reviewPR := fmt.Sprintf("%v", int64(authored["id"].(float64)))

	// Auto-merge lives in its own repository so the approver requirement that
	// keeps a pull request from merging on the spot does not also block the
	// merge_pull_request call above.
	autoMergeRef := seeded.Key + "/" + autoMergeRepo.Slug
	mustLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", autoMergeRef)
	mustLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", autoMergeRef)

	pushBranch(autoMergeRepo.Slug, "feature/mcp-arm", "mcp-arm.txt")
	armPR, err := harness.createPullRequest(ctx, seeded.Key, autoMergeRepo.Slug, "feature/mcp-arm", "master")
	if err != nil {
		t.Fatalf("create the pull request to arm failed: %v", err)
	}

	// disable_auto_merge cancels a request, so there has to be one to cancel.
	// Arming it here rather than in the sweep keeps the two calls independent.
	pushBranch(autoMergeRepo.Slug, "feature/mcp-cancel", "mcp-cancel.txt")
	cancelPR, err := harness.createPullRequest(ctx, seeded.Key, autoMergeRepo.Slug, "feature/mcp-cancel", "master")
	if err != nil {
		t.Fatalf("create the pull request to cancel failed: %v", err)
	}
	mustLiveCLI(t, "pr", "auto-merge", "enable", cancelPR, "--repo", autoMergeRef)

	// A commit the build tools can hang a status on, read out of the
	// repository rather than invented.
	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("list commit ids failed: %v (%d commits)", err, len(commits))
	}
	commitID := commits[0]
	mustLiveCLI(t, "build", "status", "set", commitID,
		"--key", "mcp-conformance", "--state", "SUCCESSFUL", "--url", "https://ci.example.com/1")

	// A tag, so list_tags answers with one rather than an empty page.
	mustLiveCLI(t, "tag", "create", "v0.0.1-mcp", "--start-point", commitID, "--repo", seeded.Key+"/"+repo.Slug)

	repoArgs := func(extra map[string]any) map[string]any {
		args := map[string]any{"project": seeded.Key, "repo": repo.Slug}
		for key, value := range extra {
			args[key] = value
		}
		return args
	}

	return map[string]map[string]any{
		"get_pull_request":   repoArgs(map[string]any{"id": mainPR}),
		"list_pull_requests": repoArgs(nil),
		"create_pull_request": repoArgs(map[string]any{
			"from_ref": "feature/mcp-create", "to_ref": "master", "title": "Opened by the conformance sweep",
		}),
		"update_pull_request": repoArgs(map[string]any{
			"pr_id": mainPR, "version": mainVersion, "title": "Renamed by the conformance sweep",
		}),
		"list_pr_comments":          repoArgs(map[string]any{"pr_id": mainPR}),
		"get_pr_diff":               repoArgs(map[string]any{"pr_id": mainPR}),
		"get_file_content":          repoArgs(map[string]any{"path": "mcp-main.txt", "at": "feature/mcp-main"}),
		"add_pr_comment":            repoArgs(map[string]any{"pr_id": mainPR, "text": "left by the conformance sweep"}),
		"submit_pr_review":          repoArgs(map[string]any{"pr_id": reviewPR, "action": "approve"}),
		"merge_pull_request":        repoArgs(map[string]any{"pr_id": mergePR}),
		"enable_auto_merge":         {"project": seeded.Key, "repo": autoMergeRepo.Slug, "pr_id": armPR},
		"disable_auto_merge":        {"project": seeded.Key, "repo": autoMergeRepo.Slug, "pr_id": cancelPR},
		"search_repositories":       {"project": seeded.Key},
		"get_repository_clone_info": repoArgs(nil),
		"list_branches":             repoArgs(nil),
		"resolve_ref":               repoArgs(map[string]any{"ref": "master"}),
		"list_tags":                 repoArgs(nil),
		"create_tag":                repoArgs(map[string]any{"name": "v0.0.2-mcp", "start_point": commitID}),
		"get_build_status":          {"commit_id": commitID},
		"set_build_status": {
			"commit_id": commitID, "key": "mcp-conformance-2", "state": "SUCCESSFUL", "url": "https://ci.example.com/2",
		},
		"list_required_builds": repoArgs(nil),
		"list_commits":         repoArgs(nil),
		"get_commit":           repoArgs(map[string]any{"commit_id": commitID}),
		"compare_refs":         repoArgs(map[string]any{"from": "feature/mcp-main", "to": "master"}),
	}
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
