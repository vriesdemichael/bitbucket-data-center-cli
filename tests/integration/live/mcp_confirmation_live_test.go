//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// liveAnswers is a live client's side of the confirmations the tools that ask
// put to the person: it answers each with answer, and keeps what it was asked.
type liveAnswers struct {
	mu     sync.Mutex
	asked  []*mcp.ElicitParams
	answer func(*mcp.ElicitParams) *mcp.ElicitResult
}

// answeringClient is a client that can show a confirmation and answers every
// one with answer: what an MCP client with a person at it does.
func answeringClient(answer func(*mcp.ElicitParams) *mcp.ElicitResult) (*mcp.ClientOptions, *liveAnswers) {
	answers := &liveAnswers{answer: answer}

	return &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, request *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			answers.mu.Lock()
			answers.asked = append(answers.asked, request.Params)
			answers.mu.Unlock()

			return answers.answer(request.Params), nil
		},
	}, answers
}

func (a *liveAnswers) questions() []*mcp.ElicitParams {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]*mcp.ElicitParams(nil), a.asked...)
}

func acceptConfirmation(*mcp.ElicitParams) *mcp.ElicitResult {
	return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}
}

func declineConfirmation(*mcp.ElicitParams) *mcp.ElicitResult {
	return &mcp.ElicitResult{Action: "decline"}
}

// confirmationLabel is the title of the one checkbox a confirmation carries.
func confirmationLabel(t *testing.T, asked *mcp.ElicitParams) string {
	t.Helper()

	encoded, err := json.Marshal(asked.RequestedSchema)
	if err != nil {
		t.Fatalf("encode the confirmation's schema: %v", err)
	}
	var schema struct {
		Properties map[string]struct {
			Title string `json:"title"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("decode the confirmation's schema %s: %v", encoded, err)
	}

	return schema.Properties["confirm"].Title
}

// TestLiveMCPToolsThatAskChangeNothingUnlessAccepted proves the confirmation
// stands between an agent and a merge.
//
// A client that cannot show one is refused with -32021 for every tool that
// asks, though each is listed to it, and a person who declines stops the
// call. In neither case may anything change in Bitbucket: a refusal is only a
// refusal if the read back after it finds everything as it was. The calls
// name things they would really change, so a server that let a call through
// would show up in the read back rather than in a refusal Bitbucket gave.
func TestLiveMCPToolsThatAskChangeNothingUnlessAccepted(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	var asking []string
	for _, spec := range bbmcp.AllSpecs() {
		if spec.Asks != bbmcp.AsksNever {
			asking = append(asking, spec.Tool.Name)
		}
	}

	// A pull request somebody else wrote, with the caller reviewing it:
	// Bitbucket refuses an author's own review.
	author, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the pull request author failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, author.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant the author write access failed: %v", err)
	}
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/mcp-ask", "mcp-ask.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	authored, err := harness.liveJSONAs(ctx, author, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", seeded.Key, repo.Slug),
		map[string]any{
			"title":     "Held back by the confirmation",
			"fromRef":   map[string]any{"id": "refs/heads/feature/mcp-ask"},
			"toRef":     map[string]any{"id": "refs/heads/master"},
			"reviewers": []map[string]any{{"user": map[string]any{"name": harness.username()}}},
		})
	if err != nil {
		t.Fatalf("create the authored pull request failed: %v", err)
	}
	pullRequestID := fmt.Sprintf("%d", int64(authored["id"].(float64)))

	// Auto-merge on, and one approval required, so a pull request with
	// auto-merge set waits for it rather than merging at once.
	mustLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", repoRef)
	if settings := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "auto-merge", "get", "--repo", repoRef)); settings["enabled"] != true {
		t.Fatalf("auto-merge on %s reads back as %v, want enabled", repoRef, settings)
	}
	mustLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", repoRef)
	if got := approverCountFrom(t, decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get", "--repo", repoRef))); got != "1" {
		t.Fatalf("required approvers on %s read back as %s, want 1", repoRef, got)
	}

	// A second pull request with auto-merge set, for disable_auto_merge to
	// cancel.
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/mcp-armed", "mcp-armed.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	armedID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "feature/mcp-armed", "master")
	if err != nil {
		t.Fatalf("create the pull request to arm failed: %v", err)
	}
	mustLiveCLI(t, "pr", "auto-merge", "enable", armedID, "--repo", repoRef)
	if autoMerge := mcpLiveAutoMerge(t, repoRef, armedID); autoMerge["enabled"] != true {
		t.Fatalf("auto-merge on pull request %s reads back as %v before the calls, so a refusal to cancel it would show nothing", armedID, autoMerge)
	}

	before := mcpLivePullRequest(t, repoRef, pullRequestID)
	if reviewer := mcpLiveReviewer(t, before, harness.username()); reviewer["role"] != "REVIEWER" || reviewer["status"] != "UNAPPROVED" {
		t.Fatalf("the caller is %v/%v on pull request %s, want an UNAPPROVED REVIEWER", reviewer["role"], reviewer["status"], pullRequestID)
	}
	version, err := strconv.Atoi(currentLivePRVersion(t, pullRequestID))
	if err != nil {
		t.Fatalf("the pull request's version is not a number: %v", err)
	}
	commitID := asString(before["sourceCommit"])
	buildKey := testsupport.UniqueName("mcp-ask-")
	tagName := testsupport.UniqueName("mcp-ask-")

	arguments := map[string]map[string]any{
		"submit_pr_review":    {"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID, "action": "approve"},
		"merge_pull_request":  {"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID},
		"enable_auto_merge":   {"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID},
		"disable_auto_merge":  {"project": seeded.Key, "repo": repo.Slug, "pr_id": armedID},
		"set_build_status":    {"commit_id": commitID, "key": buildKey, "state": "FAILED", "url": "https://ci.example.com/ask"},
		"create_tag":          {"project": seeded.Key, "repo": repo.Slug, "name": tagName, "start_point": "master"},
		"update_pull_request": {"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID, "version": version, "draft": true},
	}
	for _, name := range asking {
		if _, ok := arguments[name]; !ok {
			t.Errorf("tool %q asks but has no arguments here; give it ones it would act on, and read back below that it did not", name)
		}
	}
	if t.Failed() {
		return
	}

	t.Run("a client that cannot ask is refused with -32021", func(t *testing.T) {
		executeLiveMCPServer(t, func(session *mcp.ClientSession) {
			listed := make(map[string]bool)
			for _, name := range listedToolNames(t, session) {
				listed[name] = true
			}

			for _, name := range asking {
				// Listed to every client, whatever it can show: tools/list
				// must not vary per connection.
				if !listed[name] {
					t.Errorf("tool %q is not listed to a client that cannot ask", name)
				}

				result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments[name]})
				var wire *jsonrpc.Error
				if !errors.As(callErr, &wire) || wire.Code != mcp.CodeMissingRequiredClientCapabilities {
					t.Errorf("%s: want error -32021, got %v (result %q)", name, callErr, mcpResultText(result))
				}
			}
		}, "ai", "mcp", "serve")
	})

	t.Run("a person who declines stops the call", func(t *testing.T) {
		person, answers := answeringClient(declineConfirmation)
		executeLiveMCPServerAs(t, person, func(session *mcp.ClientSession) {
			for _, name := range asking {
				result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments[name]})
				if callErr != nil {
					t.Errorf("%s: a declined call is a tool error, got the protocol error %v", name, callErr)
					continue
				}
				if text := mcpResultText(result); !result.IsError || !strings.Contains(text, "the person declined") {
					t.Errorf("%s: want the call refused as declined, got %q", name, text)
				}
			}
		}, "ai", "mcp", "serve")

		if asked := answers.questions(); len(asked) != len(asking) {
			t.Errorf("the person was asked %d times, want once for each of the %d tools that ask", len(asked), len(asking))
		}
	})

	// A refusal is only one if nothing changed.
	after := mcpLivePullRequest(t, repoRef, pullRequestID)
	if after["state"] != "OPEN" {
		t.Errorf("pull request %s is %v after refused merge_pull_request and enable_auto_merge calls", pullRequestID, after["state"])
	}
	if after["draft"] == true {
		t.Errorf("pull request %s is a draft after a refused update_pull_request", pullRequestID)
	}
	if reviewer := mcpLiveReviewer(t, after, harness.username()); reviewer["status"] != "UNAPPROVED" {
		t.Errorf("the caller's review of pull request %s is %v after a refused submit_pr_review", pullRequestID, reviewer["status"])
	}
	if autoMerge := mcpLiveAutoMerge(t, repoRef, pullRequestID); autoMerge["enabled"] != false {
		t.Errorf("auto-merge on pull request %s is %v after a refused enable_auto_merge", pullRequestID, autoMerge)
	}
	if autoMerge := mcpLiveAutoMerge(t, repoRef, armedID); autoMerge["enabled"] != true {
		t.Errorf("auto-merge on pull request %s is %v after a refused disable_auto_merge", armedID, autoMerge)
	}
	if status, found := mcpLiveBuildStatuses(t, commitID)[buildKey]; found {
		t.Errorf("a refused set_build_status reported build %s on %s: %v", buildKey, commitID, status)
	}
	if output, err := executeLiveCLI(t, "tag", "view", tagName); err == nil {
		t.Errorf("a refused create_tag created %s:\n%s", tagName, output)
	}
}

// TestLiveMCPMergeAsksAboutWhatItMerges holds the merge and auto-merge
// confirmations to the Bitbucket facts a person needs and the call does not
// carry.
func TestLiveMCPMergeAsksAboutWhatItMerges(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	openPullRequest := func(t *testing.T, branch string) string {
		t.Helper()
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, strings.ReplaceAll(branch, "/", "-")+".txt"); err != nil {
			t.Fatalf("push commit on %s failed: %v", branch, err)
		}
		id, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
		if err != nil {
			t.Fatalf("create the pull request from %s failed: %v", branch, err)
		}
		return id
	}
	call := func(t *testing.T, person *mcp.ClientOptions, tool, pullRequestID string) *mcp.CallToolResult {
		t.Helper()
		var result *mcp.CallToolResult
		executeLiveMCPServerAs(t, person, func(session *mcp.ClientSession) {
			var callErr error
			result, callErr = session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      tool,
				Arguments: map[string]any{"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID},
			})
			if callErr != nil {
				t.Fatalf("%s: %v", tool, callErr)
			}
		}, "ai", "mcp", "serve")
		return result
	}
	merge := func(t *testing.T, person *mcp.ClientOptions, pullRequestID string) *mcp.CallToolResult {
		t.Helper()
		return call(t, person, "merge_pull_request", pullRequestID)
	}
	// staleVersion is how Bitbucket refuses a change made at a version the
	// pull request has moved past, which is what holding a call to the
	// version the person was shown relies on.
	const staleVersion = "based on out-of-date information"
	// pushingPerson accepts only after pushing a commit to the branch and
	// waiting until Bitbucket has moved the pull request past version, as a
	// colleague pushing while the person reads the question would. pushed
	// reports how the push went.
	pushingPerson := func(branch, pullRequestID string, version int) (person *mcp.ClientOptions, pushed func() error) {
		// Written on the client's goroutine, read on the test's.
		var mu sync.Mutex
		var pushErr error
		person, _ = answeringClient(func(*mcp.ElicitParams) *mcp.ElicitResult {
			err := pushCommitOnTop(harness, seeded.Key, repo.Slug, branch, "pushed-while-asking.txt")
			if err == nil {
				err = waitForVersionChange(ctx, harness, seeded.Key, repo.Slug, pullRequestID, version)
			}
			mu.Lock()
			pushErr = err
			mu.Unlock()
			return acceptConfirmation(nil)
		})
		return person, func() error {
			mu.Lock()
			defer mu.Unlock()
			return pushErr
		}
	}

	t.Run("it names the pull request and the branch it merges into, and merges on an accept", func(t *testing.T) {
		pullRequestID := openPullRequest(t, "feature/mcp-merge")

		person, answers := answeringClient(acceptConfirmation)
		if result := merge(t, person, pullRequestID); result.IsError {
			t.Fatalf("an accepted merge failed: %s", mcpResultText(result))
		}

		asked := answers.questions()
		if len(asked) != 1 {
			t.Fatalf("the person was asked %d times, want once", len(asked))
		}
		if !strings.Contains(asked[0].Message, "into master") || !strings.Contains(asked[0].Message, `"Live test PR"`) {
			t.Errorf("the question does not name the target branch and the quoted title: %q", asked[0].Message)
		}
		if label := confirmationLabel(t, asked[0]); label != fmt.Sprintf("Merge #%s into master", pullRequestID) {
			t.Errorf("the checkbox says %q, want it to name the pull request and the target", label)
		}
		if state := mcpLivePullRequest(t, repoRef, pullRequestID)["state"]; state != "MERGED" {
			t.Errorf("pull request %s is %v after an accepted merge, want MERGED", pullRequestID, state)
		}
	})

	t.Run("a draft is refused before anyone is asked", func(t *testing.T) {
		const branch = "feature/mcp-merge-draft"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "mcp-merge-draft.txt"); err != nil {
			t.Fatalf("push commit on %s failed: %v", branch, err)
		}
		pullRequestID := createLifecyclePR(t, branch, "A draft nobody may merge", "--draft", "--no-default-reviewers", "--no-codeowners")
		if !livePRIsDraft(t, pullRequestID) {
			t.Fatal("expected the pull request to be created as a draft")
		}

		person, answers := answeringClient(acceptConfirmation)
		result := merge(t, person, pullRequestID)
		if text := mcpResultText(result); !result.IsError || !strings.Contains(text, "is a draft") {
			t.Errorf("want the merge of a draft refused as a draft, got %q", text)
		}
		if asked := answers.questions(); len(asked) != 0 {
			t.Errorf("the person was asked to confirm a merge Bitbucket refuses: %q", asked[0].Message)
		}
		if after := mcpLivePullRequest(t, repoRef, pullRequestID); after["state"] != "OPEN" || after["draft"] != true {
			t.Errorf("the draft reads back as %v, draft %v", after["state"], after["draft"])
		}
	})

	// The merge is held to the version the person was shown, so a pull request
	// that changes while the question is open is not merged.
	t.Run("a pull request that changed after the question is not merged", func(t *testing.T) {
		const branch = "feature/mcp-merge-moved"
		pullRequestID := openPullRequest(t, branch)
		asked, err := strconv.Atoi(currentLivePRVersion(t, pullRequestID))
		if err != nil {
			t.Fatalf("the pull request's version is not a number: %v", err)
		}

		person, pushed := pushingPerson(branch, pullRequestID, asked)
		result := merge(t, person, pullRequestID)
		if err := pushed(); err != nil {
			t.Fatalf("pushing to the pull request while it was asked about failed: %v", err)
		}
		if text := mcpResultText(result); !result.IsError || !strings.Contains(text, staleVersion) {
			t.Errorf("want the merge refused for its stale version, got %q", text)
		}
		if state := mcpLivePullRequest(t, repoRef, pullRequestID)["state"]; state != "OPEN" {
			t.Errorf("pull request %s is %v, want it left OPEN", pullRequestID, state)
		}
	})

	// Setting auto-merge may merge at once, so it is held to the version the
	// person was shown as well.
	t.Run("auto-merge is not set on a pull request that changed after the question", func(t *testing.T) {
		mustLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", repoRef)
		if settings := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "auto-merge", "get", "--repo", repoRef)); settings["enabled"] != true {
			t.Fatalf("auto-merge on %s reads back as %v, want enabled", repoRef, settings)
		}
		const branch = "feature/mcp-arm-moved"
		pullRequestID := openPullRequest(t, branch)
		asked, err := strconv.Atoi(currentLivePRVersion(t, pullRequestID))
		if err != nil {
			t.Fatalf("the pull request's version is not a number: %v", err)
		}

		person, pushed := pushingPerson(branch, pullRequestID, asked)
		result := call(t, person, "enable_auto_merge", pullRequestID)
		if err := pushed(); err != nil {
			t.Fatalf("pushing to the pull request while it was asked about failed: %v", err)
		}
		if text := mcpResultText(result); !result.IsError || !strings.Contains(text, staleVersion) {
			t.Errorf("want auto-merge refused for its stale version, got %q", text)
		}
		if state := mcpLivePullRequest(t, repoRef, pullRequestID)["state"]; state != "OPEN" {
			t.Errorf("pull request %s is %v, want it left OPEN", pullRequestID, state)
		}
		if autoMerge := mcpLiveAutoMerge(t, repoRef, pullRequestID); autoMerge["enabled"] == true {
			t.Errorf("auto-merge on pull request %s reads back as %v after the refusal", pullRequestID, autoMerge)
		}
	})
}

// TestLiveMCPTitleChangeOnADraftDoesNotAsk holds update_pull_request to asking
// only for a call that sets the draft flag. A title change leaves the flag out,
// and Bitbucket keeps a draft a draft when an update leaves it out, so the
// change runs with nobody asked and the pull request still cannot merge. The
// person declines everything, so a call that asked would be refused.
func TestLiveMCPTitleChangeOnADraftDoesNotAsk(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/mcp-draft-title"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "mcp-draft-title.txt"); err != nil {
		t.Fatalf("push commit on %s failed: %v", branch, err)
	}
	pullRequestID := createLifecyclePR(t, branch, "A draft to rename", "--draft", "--no-default-reviewers", "--no-codeowners")
	if !livePRIsDraft(t, pullRequestID) {
		t.Fatal("expected the pull request to be created as a draft")
	}
	version, err := strconv.Atoi(currentLivePRVersion(t, pullRequestID))
	if err != nil {
		t.Fatalf("the pull request's version is not a number: %v", err)
	}

	const title = "A draft renamed with nobody asked"
	person, answers := answeringClient(declineConfirmation)
	executeLiveMCPServerAs(t, person, func(session *mcp.ClientSession) {
		result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "update_pull_request",
			Arguments: map[string]any{
				"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID, "version": version, "title": title,
			},
		})
		if callErr != nil {
			t.Fatalf("update_pull_request: %v", callErr)
		}
		if result.IsError {
			t.Fatalf("the title change failed: %s", mcpResultText(result))
		}
	}, "ai", "mcp", "serve")

	if asked := answers.questions(); len(asked) != 0 {
		t.Errorf("a title change asked the person: %q", asked[0].Message)
	}
	after := mcpLivePullRequest(t, repoRef, pullRequestID)
	if after["title"] != title {
		t.Errorf("the title reads back as %q, want %q", after["title"], title)
	}
	if after["draft"] != true {
		t.Errorf("a title change promoted the draft: draft reads back as %v", after["draft"])
	}
}

// pushCommitOnTop adds a commit to a branch that already exists, as someone
// pushing to a pull request's source branch does. It reports rather than
// fails, since it runs on the client's goroutine.
func pushCommitOnTop(harness *liveHarness, projectKey, repositorySlug, branch, fileName string) error {
	directory, err := os.MkdirTemp("", "bb-live-push-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(directory) }()

	pushURL, err := repositoryPushURL(harness.config, projectKey, repositorySlug)
	if err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "bb-live-test"},
		{"config", "user.email", "bb-live-test@example.local"},
		{"remote", "add", "origin", pushURL},
		{"fetch", "origin", branch},
		{"checkout", "-b", branch, "FETCH_HEAD"},
	} {
		if err := runGit(directory, args...); err != nil {
			return fmt.Errorf("git %s: %w", args[0], err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, fileName), []byte("pushed while the merge was asked about\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", fileName},
		{"commit", "-m", "pushed while the merge was asked about"},
		{"push", "origin", branch},
	} {
		if err := runGit(directory, args...); err != nil {
			return fmt.Errorf("git %s: %w", args[0], err)
		}
	}

	return nil
}

// waitForVersionChange waits until Bitbucket has moved the pull request past
// version, which it does once it has seen a push to the source branch.
func waitForVersionChange(ctx context.Context, harness *liveHarness, projectKey, repositorySlug, pullRequestID string, version int) error {
	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s", projectKey, repositorySlug, pullRequestID)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pullRequest, err := harness.liveJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		if current, _ := pullRequest["version"].(float64); int(current) != version {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return fmt.Errorf("pull request %s stayed at version %d for 30s after the push", pullRequestID, version)
}
