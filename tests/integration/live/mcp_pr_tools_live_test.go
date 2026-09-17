//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestLiveMCPListPullRequestsModeSelection covers the choice list_pull_requests
// makes between two different Bitbucket endpoints.
//
// Given a project and a repository it asks that repository; given neither it
// asks the dashboard, which answers with the caller's pull requests across
// every repository they can see. A unit test asserted this by recording the
// path bb sent and comparing it to the path the same test expected, which says
// nothing about whether either endpoint answers that way -- and the difference
// between them is entirely in what Bitbucket returns.
func TestLiveMCPListPullRequestsModeSelection(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 2, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	first, second := seeded.Repos[0], seeded.Repos[1]
	configureLiveCLIEnv(t, harness, seeded.Key, first.Slug)

	push := func(slug, branch, file string) {
		t.Helper()
		if err := harness.pushCommitOnBranch(seeded.Key, slug, branch, file); err != nil {
			t.Fatalf("push %s on %s failed: %v", branch, slug, err)
		}
	}

	push(first.Slug, "feature/mine", "mine.txt")
	mine, err := harness.createPullRequest(ctx, seeded.Key, first.Slug, "feature/mine", "master")
	if err != nil {
		t.Fatalf("create the pull request in the first repository failed: %v", err)
	}

	push(second.Slug, "feature/elsewhere", "elsewhere.txt")
	elsewhere, err := harness.createPullRequest(ctx, seeded.Key, second.Slug, "feature/elsewhere", "master")
	if err != nil {
		t.Fatalf("create the pull request in the second repository failed: %v", err)
	}

	// A pull request in the first repository that the caller did not write, so
	// role=author has something to leave out.
	author, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the other author failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, first.Slug, author.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant the other author write access failed: %v", err)
	}
	push(first.Slug, "feature/theirs", "theirs.txt")
	authored, err := harness.liveJSONAs(ctx, author, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", seeded.Key, first.Slug),
		map[string]any{
			"title":   "Written by somebody else",
			"fromRef": map[string]any{"id": "refs/heads/feature/theirs"},
			"toRef":   map[string]any{"id": "refs/heads/master"},
		})
	if err != nil {
		t.Fatalf("create the other author's pull request failed: %v", err)
	}
	theirs := fmt.Sprintf("%d", int64(authored["id"].(float64)))

	// A pull request the caller wrote in another project, so a project filter
	// has something of their own to leave out, and the dashboard without one
	// has somewhere else to reach.
	other, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed a second project failed: %v", err)
	}
	if err := harness.pushCommitOnBranch(other.Key, other.Repos[0].Slug, "feature/outside", "outside.txt"); err != nil {
		t.Fatalf("push feature/outside failed: %v", err)
	}
	outside, err := harness.createPullRequest(ctx, other.Key, other.Repos[0].Slug, "feature/outside", "master")
	if err != nil {
		t.Fatalf("create the pull request in the second project failed: %v", err)
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()

		// Ids are reported only for this test's project.
		//
		// A pull request id is unique within a repository and nowhere else, so
		// on the dashboard -- which spans the instance -- a bare "2" matches
		// whichever other repository happens to have a second pull request.
		// Against a Bitbucket the rest of the live suite is writing to in
		// parallel, that is most of them, and the assertion below read those as
		// this test's own.
		listed := func(t *testing.T, args map[string]any) []string {
			t.Helper()
			var payload struct {
				PullRequests []struct {
					ID         int64 `json:"id"`
					Repository *struct {
						ProjectKey string `json:"project_key"`
						Slug       string `json:"slug"`
					} `json:"repository"`
				} `json:"pull_requests"`
			}
			raw := callAndDecode(t, session, callCtx, "list_pull_requests", args, &payload)
			if len(payload.PullRequests) == 0 {
				t.Fatalf("list_pull_requests returned nothing for %#v: %s", args, raw)
			}
			ids := make([]string, 0, len(payload.PullRequests))
			for _, pullRequest := range payload.PullRequests {
				if pullRequest.Repository != nil && pullRequest.Repository.ProjectKey != seeded.Key {
					continue
				}
				ids = append(ids, fmt.Sprintf("%d", pullRequest.ID))
			}
			return ids
		}

		t.Run("repository mode answers with that repository only", func(t *testing.T) {
			ids := listed(t, map[string]any{"project": seeded.Key, "repo": second.Slug})
			if len(ids) != 1 || ids[0] != elsewhere {
				t.Errorf("the second repository reported %v, want just %s", ids, elsewhere)
			}
		})

		t.Run("dashboard mode reaches across repositories", func(t *testing.T) {
			// The dashboard is the caller's own pull requests wherever they
			// are, which is the property the repository endpoint cannot have:
			// two repositories, one answer. No role, because that is how an
			// agent asks for its own pull requests; the tool used to send
			// REVIEWER for it, and an author who reviews nothing got an empty
			// list (#576).
			// An explicit limit because the default is 25 rows of an
			// instance-wide listing, which the rest of the suite fills.
			ids := listed(t, map[string]any{"limit": dashboardPage})
			if !containsFold(ids, mine) || !containsFold(ids, elsewhere) {
				t.Errorf("the dashboard reported %v, want both %s and %s", ids, mine, elsewhere)
			}
		})

		t.Run("without a project the dashboard reaches other projects", func(t *testing.T) {
			var payload struct {
				PullRequests []struct {
					ID         int64 `json:"id"`
					Repository *struct {
						ProjectKey string `json:"project_key"`
					} `json:"repository"`
				} `json:"pull_requests"`
			}
			raw := callAndDecode(t, session, callCtx, "list_pull_requests", map[string]any{"limit": dashboardPage}, &payload)
			for _, pullRequest := range payload.PullRequests {
				if pullRequest.Repository != nil && strings.EqualFold(pullRequest.Repository.ProjectKey, other.Key) && fmt.Sprintf("%d", pullRequest.ID) == outside {
					return
				}
			}
			t.Errorf("the dashboard did not report %s in %s: %s", outside, other.Key, raw)
		})

		t.Run("a role narrows the dashboard", func(t *testing.T) {
			ids := listed(t, map[string]any{"role": "author", "limit": dashboardPage})
			if !containsFold(ids, mine) {
				t.Errorf("role=author reported %v, want %s", ids, mine)
			}
			// Somebody else's pull request is not the caller's, wherever it is.
			if containsFold(ids, theirs) {
				t.Errorf("the dashboard reported %s under role=author, which somebody else wrote: %v", theirs, ids)
			}
		})

		t.Run("a repository cannot be filtered by role", func(t *testing.T) {
			// Bitbucket declares no role parameter on the repository listing
			// and drops the one we used to send, so the answer was every open
			// pull request -- including the one the caller did not write --
			// while the tool's own description promised a filter. Refusing says
			// so; the previous behaviour did not.
			result, callErr := session.CallTool(callCtx, &mcp.CallToolParams{
				Name:      "list_pull_requests",
				Arguments: map[string]any{"project": seeded.Key, "repo": first.Slug, "role": "author"},
			})
			if callErr != nil {
				t.Fatalf("tools/call returned a protocol error: %v", callErr)
			}
			if !result.IsError {
				t.Fatalf("a repository filtered by role was accepted: %s", mcpResultText(result))
			}
			if text := mcpResultText(result); !strings.Contains(text, "dashboard") {
				t.Errorf("the refusal does not point at the dashboard: %s", text)
			}
		})

		t.Run("a project without a repository narrows the dashboard to it", func(t *testing.T) {
			listedInProject(t, session, map[string]any{"project": seeded.Key, "limit": dashboardPage}, seeded.Key, mine, elsewhere)
		})

		t.Run("a repository needs its project", func(t *testing.T) {
			result, callErr := session.CallTool(callCtx, &mcp.CallToolParams{
				Name:      "list_pull_requests",
				Arguments: map[string]any{"repo": first.Slug},
			})
			if callErr != nil {
				t.Fatalf("tools/call returned a protocol error: %v", callErr)
			}
			if !result.IsError || !strings.Contains(mcpResultText(result), "project") {
				t.Fatalf("a repository without its project was not refused for it: %s", mcpResultText(result))
			}
		})
	}, "ai", "mcp", "serve")

	// Scoped to the project, the call with no arguments is bound to it: the
	// project is filled in, and the dashboard is narrowed to it rather than the
	// call refused for the repository the scope does not name.
	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		t.Run("a project-scoped server narrows the dashboard", func(t *testing.T) {
			listedInProject(t, session, map[string]any{"limit": dashboardPage}, seeded.Key, mine, elsewhere)
		})
	}, "ai", "mcp", "serve", "--project", seeded.Key)
}

// listedInProject calls list_pull_requests, fails if anything it returns lies
// outside the project, and checks that the pull requests named are among it.
func listedInProject(t *testing.T, session *mcp.ClientSession, args map[string]any, projectKey string, want ...string) {
	t.Helper()

	var payload struct {
		PullRequests []struct {
			ID         int64 `json:"id"`
			Repository *struct {
				ProjectKey string `json:"project_key"`
			} `json:"repository"`
		} `json:"pull_requests"`
	}
	raw := callAndDecode(t, session, context.Background(), "list_pull_requests", args, &payload)

	ids := make([]string, 0, len(payload.PullRequests))
	for _, pullRequest := range payload.PullRequests {
		if pullRequest.Repository == nil || !strings.EqualFold(pullRequest.Repository.ProjectKey, projectKey) {
			t.Fatalf("list_pull_requests %v returned a pull request outside %s: %s", args, projectKey, raw)
		}
		ids = append(ids, fmt.Sprintf("%d", pullRequest.ID))
	}
	for _, id := range want {
		if !containsFold(ids, id) {
			t.Errorf("list_pull_requests %v reported %v, want %s among them", args, ids, id)
		}
	}
}

// TestLiveMCPGetFileContentReturnsTheFileAsText covers get_file_content against
// a file Bitbucket is actually storing.
//
// The endpoint is the raw one, which answers with the bytes rather than a JSON
// entity, and the tool has to hand them back as text. A unit test compared the
// path bb requested against the path it expected and read back the same string
// its own handler had written, so neither half of that was Bitbucket's.
func TestLiveMCPGetFileContentReturnsTheFileAsText(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A path with a directory in it, because the raw endpoint takes the path as
	// path segments rather than as a parameter.
	const path = "src/main.go"
	const content = "package main\n\nfunc main() {}\n"
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", path, content); err != nil {
		t.Fatalf("push the file failed: %v", err)
	}
	// The same path holding something else on a branch, so which of the two
	// comes back shows the ref arrived: without it the default branch answers.
	const branch = "feature/other-main"
	const branchContent = "package main\n\nfunc main() { println(\"branch\") }\n"
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, path, branchContent); err != nil {
		t.Fatalf("push the file on %s failed: %v", branch, err)
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		for _, read := range []struct{ at, want string }{
			{at: "refs/heads/master", want: content},
			{at: "refs/heads/" + branch, want: branchContent},
		} {
			var payload struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			callAndDecode(t, session, context.Background(), "get_file_content", map[string]any{
				"project": seeded.Key, "repo": repo.Slug, "path": path, "at": read.at,
			}, &payload)

			if payload.Content != read.want {
				t.Errorf("get_file_content at %s returned %q, want the file pushed there, %q", read.at, payload.Content, read.want)
			}
			if payload.Path != path {
				t.Errorf("get_file_content at %s reported path %q, want %q", read.at, payload.Path, path)
			}
		}
	}, "ai", "mcp", "serve")
}
