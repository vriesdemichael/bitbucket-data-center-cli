//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpDisclosed is text as Bitbucket holds it once an agent wrote it through
// MCP for user, whose login name needs no quoting: with the line saying so
// (#797). Spelled out here rather than taken from bb, so a change to the line
// shows.
func mcpDisclosed(text, user string) string {
	return text + "\n\n_Written by an AI agent for @" + user + ", via bb._"
}

// TestLiveMCPWritesCarryTheAIDisclosure covers the line bb ends what an agent
// writes through MCP with (#797): a pull request's description when it is
// created and each time it is updated, and a comment, each read back through
// the CLI.
//
// The server runs as a user whose login name holds a + and an @. Bitbucket
// names the caller percent-encoded, and its Markdown links such a name only
// quoted: bare, an email-style name renders as a mail link.
func TestLiveMCPWritesCarryTheAIDisclosure(t *testing.T) {
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
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/disclosed", "disclosed.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	name := "LT.Agent+" + uniqueSuffix() + "@example.local"
	person := createLicensedUserNamed(ctx, t, harness, name)
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, person.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant %s write access failed: %v", name, err)
	}
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, person)
	line := `_Written by an AI agent for @"` + name + `", via bb._`

	description := func(t *testing.T, id string) (string, float64) {
		t.Helper()
		pr := mcpLivePullRequest(t, repoRef, id)
		version, _ := pr["version"].(float64)
		return asString(pr["description"]), version
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()
		base := map[string]any{"project": seeded.Key, "repo": repo.Slug}
		with := func(extra map[string]any) map[string]any {
			args := map[string]any{}
			for key, value := range base {
				args[key] = value
			}
			for key, value := range extra {
				args[key] = value
			}
			return args
		}

		var created struct {
			PullRequest struct {
				ID float64 `json:"id"`
			} `json:"pull_request"`
		}
		callAndDecode(t, session, callCtx, "create_pull_request",
			with(map[string]any{"from_ref": "feature/disclosed", "to_ref": "master", "title": "Retry the refund", "description": "Adds retries."}), &created)
		id := fmt.Sprintf("%d", int64(created.PullRequest.ID))

		held, version := description(t, id)
		if held != "Adds retries.\n\n"+line {
			t.Fatalf("the created description reads %q, want the text and then %q", held, line)
		}

		// Read back and sent again with an edit, as an agent does: the line it
		// carries is kept, not added twice.
		edited := strings.Replace(held, "Adds retries.", "Adds retries, and logs each one.", 1)
		callAndDecode(t, session, callCtx, "update_pull_request",
			with(map[string]any{"pr_id": id, "version": version, "description": edited}), new(map[string]any))
		held, version = description(t, id)
		if held != "Adds retries, and logs each one.\n\n"+line {
			t.Errorf("a description sent back with its line reads %q, want the line once", held)
		}

		// Rewritten without it, it comes back.
		callAndDecode(t, session, callCtx, "update_pull_request",
			with(map[string]any{"pr_id": id, "version": version, "description": "Retries refunds."}), new(map[string]any))
		if held, _ = description(t, id); held != "Retries refunds.\n\n"+line {
			t.Errorf("a description rewritten without its line reads %q, want the line added", held)
		}

		var commented struct {
			Comment struct {
				ID float64 `json:"id"`
			} `json:"comment"`
		}
		callAndDecode(t, session, callCtx, "add_pr_comment", with(map[string]any{"pr_id": id, "text": "Looks fine."}), &commented)
		commentID := fmt.Sprintf("%d", int64(commented.Comment.ID))
		if stored := repoCLIPRComment(t, id, commentID); stored["text"] != "Looks fine.\n\n"+line {
			t.Errorf("the comment reads %q, want the text and then %q", stored["text"], line)
		}
	}, "ai", "mcp", "serve", "--allow-writes")
}

// TestLiveMCPWithoutTheLineNothingIsPublishedUnderThePersonsName covers
// --ai-disclosure=false, for an organisation that cannot allow the line: the
// tools that would publish a pull request's title and description at once are
// not offered, and a comment arrives as written, waiting in the person's
// review for them to publish, which no one else sees until they do (ADR-104).
func TestLiveMCPWithoutTheLineNothingIsPublishedUnderThePersonsName(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/unmarked", "unmarked.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "feature/unmarked", "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		listed := map[string]bool{}
		for _, name := range listedToolNames(t, session) {
			listed[name] = true
		}
		for _, withheld := range []string{"create_pull_request", "update_pull_request"} {
			if listed[withheld] {
				t.Errorf("%s is offered by a server started with --ai-disclosure=false", withheld)
			}
		}
		if !listed["add_pr_comment"] {
			t.Fatal("add_pr_comment is not offered, though a comment waits for the person to publish it")
		}

		var commented struct {
			Comment struct {
				ID float64 `json:"id"`
			} `json:"comment"`
		}
		callAndDecode(t, session, context.Background(), "add_pr_comment",
			map[string]any{"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID, "text": "Looks fine."}, &commented)
		stored := repoCLIPRComment(t, pullRequestID, fmt.Sprintf("%d", int64(commented.Comment.ID)))
		if stored["text"] != "Looks fine." || stored["pending"] != true {
			t.Errorf("the comment reads %q, pending %v; want it as written, waiting in the review", stored["text"], stored["pending"])
		}
	}, "ai", "mcp", "serve", "--allow-writes", "--ai-disclosure=false")
}
