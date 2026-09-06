//go:build live

package live_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestLiveMCPServeIsNotScopedByTheDirectoryItStartsIn is the behavioural half
// of #538.
//
// The unit test that came with the fix pins the annotation: `ai mcp serve` sets
// it as a string literal because its package cannot import the one that reads
// it, and the test keeps the two from drifting. That is the right guard for
// that risk, and it cannot show the thing that actually went wrong.
//
// What went wrong is a server confined to a repository nobody named. Ambient
// inference fills --repo from the git remote of the working directory, serve
// reads --repo as its confinement scope (ADR-062), and the two together mean a
// server started inside a clone silently refuses every call naming a sibling.
// IDE-launched servers are the common case: an editor spawns them with the
// working directory set to the workspace root, which is usually a clone.
//
// Proving that needs a real server, a real clone and a second repository the
// call can name -- an annotation is only a promise that this holds.
func TestLiveMCPServeIsNotScopedByTheDirectoryItStartsIn(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	// Two repositories: the one the server starts inside, and the sibling that
	// a wrongly scoped server would refuse.
	seeded, err := harness.seedRepo(ctx, repoSeed{Repos: 2})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	inside, sibling := seeded.Repos[0], seeded.Repos[1]

	// No --repo in the environment either: the question is what the working
	// directory does on its own.
	configureLiveCLIEnv(t, harness, seeded.Key, inside.Slug)
	t.Setenv("BITBUCKET_REPO_SLUG", "")

	originURL, err := repositoryPushURL(harness.config, seeded.Key, inside.Slug)
	if err != nil {
		t.Fatalf("build push url: %v", err)
	}

	workingDirectory := t.TempDir()
	if err := runGit(workingDirectory, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := runGit(workingDirectory, "remote", "add", "origin", originURL); err != nil {
		t.Fatalf("git remote add failed: %v", err)
	}

	originalDirectory, wdErr := os.Getwd()
	if wdErr != nil {
		t.Fatalf("getwd failed: %v", wdErr)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()

		// The sibling is the assertion. A server scoped to the clone answers
		// this with "outside the scope" and never reaches Bitbucket.
		var payload struct {
			Branches []struct {
				DisplayID string `json:"displayId"`
			} `json:"branches"`
		}
		callAndDecode(t, session, callCtx, "list_branches", map[string]any{
			"project": seeded.Key, "repo": sibling.Slug,
		}, &payload)

		if len(payload.Branches) == 0 {
			t.Errorf("the sibling repository %s answered with no branches", sibling.Slug)
		}

		// And the tools a scoped server cannot bind stay in the catalogue.
		// Their absence was the other half of the report: scoping withholds
		// what it cannot confine, so the build-status tools disappeared.
		listed := listedToolNames(t, session)
		for _, name := range []string{"get_build_status", "search_repositories"} {
			if !containsFold(listed, name) {
				t.Errorf("%q is missing from the catalogue, so the server is scoped: %v", name, listed)
			}
		}
	}, "ai", "mcp", "serve")
}

// TestLiveMCPServeStillHonoursAnExplicitScope is the other side of the same
// change, and the reason it is not simply a removal.
//
// Confinement has to keep working when an operator asks for it -- the fix
// narrows where the scope may come from, not whether it exists. Without this,
// the annotation could be read as "serve ignores --repo".
func TestLiveMCPServeStillHonoursAnExplicitScope(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Repos: 2})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	confined, sibling := seeded.Repos[0], seeded.Repos[1]
	configureLiveCLIEnv(t, harness, seeded.Key, confined.Slug)

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()

		result, callErr := session.CallTool(callCtx, &mcp.CallToolParams{
			Name:      "list_branches",
			Arguments: map[string]any{"project": seeded.Key, "repo": sibling.Slug},
		})
		if callErr != nil {
			t.Fatalf("tools/call returned a protocol error: %v", callErr)
		}
		if !result.IsError {
			t.Fatalf("an explicitly scoped server answered for %s, which is outside its scope", sibling.Slug)
		}
		if text := mcpResultText(result); !strings.Contains(text, "outside the scope") {
			t.Errorf("the refusal does not say the call is out of scope: %s", text)
		}
	}, "ai", "mcp", "serve", "--repo", seeded.Key+"/"+confined.Slug)
}
