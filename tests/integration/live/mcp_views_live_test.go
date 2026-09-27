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
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// viewPayloadKey is where show puts what its view draws (ADR-101).
const viewPayloadKey = "io.github.vriesdemichael.bb/view"

// TestLiveMCPShowCarriesWhatItsViewDraws: in a client that renders views, show
// answers with what its view draws, read from Bitbucket through bb: the pull
// request and its reviewers' decisions, the builds on its source commit, the
// avatars as images, the diff, and the list with each pull request's build
// counts. A client that renders none is told nothing was shown.
func TestLiveMCPShowCarriesWhatItsViewDraws(t *testing.T) {
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

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create a reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant the reviewer read access failed: %v", err)
	}

	branch := testsupport.UniqueName("feature/shown-")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, "shown.txt", "shown in a view\n"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	title := testsupport.UniqueName("Shown in a view ")
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", title,
		"--reviewers", reviewer.Username, "--no-default-reviewers", "--no-codeowners")))
	id := fmt.Sprint(created["id"])

	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s", seeded.Key, repo.Slug, id)
	if _, err := harness.liveJSONAs(ctx, reviewer, http.MethodPut, path+"/participants/"+reviewer.Username,
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "status": "APPROVED"}); err != nil {
		t.Fatalf("the reviewer's approval failed: %v", err)
	}
	commit := asString(mcpLivePullRequest(t, repoRef, id)["sourceCommit"])
	buildKey := testsupport.UniqueName("shown-build-")
	if _, err := harness.liveJSON(ctx, http.MethodPost, "/rest/build-status/latest/commits/"+commit, map[string]any{
		"state": "FAILED", "key": buildKey, "name": "Shown build", "url": "https://ci.example.com/shown",
	}); err != nil {
		t.Fatalf("post a build status failed: %v", err)
	}

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		show := func(arguments map[string]any) map[string]any {
			t.Helper()
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show", Arguments: arguments})
			if err != nil || result.IsError {
				t.Fatalf("show %v: %v %s", arguments, err, mcpResultText(result))
			}
			payload, _ := result.Meta[viewPayloadKey].(map[string]any)
			if payload == nil {
				t.Fatalf("show %v carries no view payload: %v", arguments, result.Meta)
			}
			return payload
		}

		card := show(map[string]any{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": id})
		pr, _ := card["pull_request"].(map[string]any)
		if pr["title"] != title {
			t.Errorf("the card shows %q, want %q", pr["title"], title)
		}
		reviewers, _ := pr["reviewers"].([]any)
		if len(reviewers) != 1 || asString(reviewers[0].(map[string]any)["status"]) != "APPROVED" {
			t.Errorf("the card shows reviewers %v, want %s approved", reviewers, reviewer.Username)
		}
		checks, _ := pr["checks"].([]any)
		if len(checks) != 1 || asString(checks[0].(map[string]any)["key"]) != buildKey || asString(checks[0].(map[string]any)["state"]) != "FAILED" {
			t.Errorf("the card shows builds %v, want %s failed", checks, buildKey)
		}
		avatars, _ := card["avatars"].(map[string]any)
		for _, person := range []string{asString(pr["author_username"]), reviewer.Username} {
			if !strings.HasPrefix(asString(avatars[person]), "data:image/") {
				t.Errorf("the card has no avatar image for %s: %.40q", person, asString(avatars[person]))
			}
		}

		diff := show(map[string]any{"kind": "diff", "project": seeded.Key, "repo": repo.Slug, "id": id})
		patch := asString(diff["diff"].(map[string]any)["patch"])
		if !strings.Contains(patch, "shown.txt") || !strings.Contains(patch, "+shown in a view") {
			t.Errorf("the diff does not carry the change:\n%s", patch)
		}

		list := show(map[string]any{"kind": "pull_requests", "project": seeded.Key, "repo": repo.Slug})
		listed, _ := list["pull_requests"].([]any)
		var found map[string]any
		for _, item := range listed {
			if candidate, _ := item.(map[string]any); fmt.Sprint(candidate["id"]) == id {
				found = candidate
			}
		}
		if found == nil {
			t.Fatalf("the list does not show #%s: %v", id, listed)
		}
		if counts, _ := found["check_counts"].(map[string]any); counts["failed"] != float64(1) {
			t.Errorf("the list counts builds %v for #%s, want one failed", found["check_counts"], id)
		}
	}, "ai", "mcp", "serve")

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show",
			Arguments: map[string]any{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": id}})
		if err != nil || result.IsError {
			t.Fatalf("show in a client without views: %v %s", err, mcpResultText(result))
		}
		if !strings.Contains(mcpResultText(result), "displays no views") || result.Meta[viewPayloadKey] != nil {
			t.Errorf("a client without views got %q with payload %v", mcpResultText(result), result.Meta[viewPayloadKey] != nil)
		}
	}, "ai", "mcp", "serve")
}
