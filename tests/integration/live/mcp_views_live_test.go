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

// TestLiveMCPShowCountsEveryBuild: a pull request whose commit has more
// builds than a card lists. The card counts them all, as Bitbucket totals
// them, and the builds it lists keep the ones that failed, though they are
// the oldest, which Bitbucket's default newest-first order would leave out.
// The model is told the same counts.
func TestLiveMCPShowCountsEveryBuild(t *testing.T) {
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

	branch := testsupport.UniqueName("feature/many-builds-")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, "built.txt", "built many times\n"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", testsupport.UniqueName("Built many times "),
		"--no-default-reviewers", "--no-codeowners")))
	id := fmt.Sprint(created["id"])
	commit := asString(mcpLivePullRequest(t, repoRef, id)["sourceCommit"])

	// 120 builds, posted oldest first: four that failed, then one each that
	// runs, was canceled and has no known state, then the passes.
	states := []string{"FAILED", "FAILED", "FAILED", "FAILED", "INPROGRESS", "CANCELLED", "UNKNOWN"}
	for len(states) < 120 {
		states = append(states, "SUCCESSFUL")
	}
	failed := map[string]bool{}
	for i, state := range states {
		key := fmt.Sprintf("build-%03d", i)
		if state == "FAILED" {
			failed[key] = true
		}
		if _, err := harness.liveJSON(ctx, http.MethodPost, "/rest/build-status/latest/commits/"+commit, map[string]any{
			"state": state, "key": key, "name": "Build " + key, "url": "https://ci.example.com/" + key,
		}); err != nil {
			t.Fatalf("post build %s failed: %v", key, err)
		}
	}
	stats, err := harness.liveJSON(ctx, http.MethodGet, "/rest/build-status/latest/commits/stats/"+commit, nil)
	if err != nil {
		t.Fatalf("read the build totals back failed: %v", err)
	}
	if stats["failed"] != float64(4) || stats["successful"] != float64(113) || stats["unknown"] != float64(1) {
		t.Fatalf("Bitbucket holds %v for the commit, want the 120 builds posted", stats)
	}

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show",
			Arguments: map[string]any{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": id}})
		if err != nil || result.IsError {
			t.Fatalf("show: %v %s", err, mcpResultText(result))
		}
		payload, _ := result.Meta[viewPayloadKey].(map[string]any)
		pr, _ := payload["pull_request"].(map[string]any)

		counts, _ := pr["check_counts"].(map[string]any)
		want := map[string]float64{"failed": 4, "in_progress": 1, "cancelled": 1, "unknown": 1, "successful": 113}
		for field, count := range want {
			if counts[field] != count {
				t.Errorf("the card counts %v builds %s, want %v: %v", counts[field], field, count, counts)
			}
		}

		checks, _ := pr["checks"].([]any)
		listedFailed := 0
		for _, check := range checks {
			if failed[asString(check.(map[string]any)["key"])] {
				listedFailed++
			}
		}
		if len(checks) != 100 || listedFailed != 4 || pr["checks_limit_reached"] != true {
			t.Errorf("the card lists %d builds, %d of the 4 that failed, limit reached %v; want 100, all 4, and the limit said",
				len(checks), listedFailed, pr["checks_limit_reached"])
		}

		if text := mcpResultText(result); !strings.Contains(text, "Builds: 4 failed, 1 in progress, 1 canceled, 1 unknown, 113 passed.") {
			t.Errorf("the model is told %q, want every build counted", text)
		}
	}, "ai", "mcp", "serve")
}
