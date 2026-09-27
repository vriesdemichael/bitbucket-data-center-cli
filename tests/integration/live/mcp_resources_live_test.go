//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestLiveMCPResourcesReadWhatTheToolsRead holds the resources, the resource
// list, the completions and the prompts to a running Data Center.
//
// One pull request is the caller's own, with an open comment thread; another
// was opened by somebody else with the caller as its reviewer. The scoped
// server is confined to their repository, so the list is exactly those two,
// whatever else the instance holds: a third, the caller's own in a sibling
// repository of the same project, stays out of it and cannot be read.
func TestLiveMCPResourcesReadWhatTheToolsRead(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 2, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	key, slug, sibling := seeded.Key, seeded.Repos[0].Slug, seeded.Repos[1].Slug
	repoRef := key + "/" + slug
	configureLiveCLIEnv(t, harness, key, slug)

	const (
		branch     = "feature/mcp-resources"
		fileName   = "notes/resources.md"
		fileLine   = "The resource reads this line back."
		threadText = "Rename this before it merges."
	)
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("encode the picture: %v", err)
	}
	if err := harness.pushFilesOnBranch(key, slug, branch, map[string][]byte{
		fileName:        []byte(fileLine + "\n"),
		"notes/dot.png": picture.Bytes(),
	}); err != nil {
		t.Fatalf("push the files failed: %v", err)
	}
	mine, err := harness.createPullRequest(ctx, key, slug, branch, "master")
	if err != nil {
		t.Fatalf("create the pull request failed: %v", err)
	}
	commit := asString(mcpLivePullRequest(t, repoRef, mine)["sourceCommit"])
	if _, err := harness.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/comments", key, slug, mine),
		map[string]any{"text": threadText}); err != nil {
		t.Fatalf("comment on the pull request failed: %v", err)
	}

	// Somebody else's pull request, waiting on the caller's review.
	author, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the pull request author failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, key, slug, author.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant the author write access failed: %v", err)
	}
	if err := harness.pushCommitOnBranch(key, slug, "feature/mcp-review-me", "review-me.txt"); err != nil {
		t.Fatalf("push the branch to review failed: %v", err)
	}
	authored, err := harness.liveJSONAs(ctx, author, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", key, slug),
		map[string]any{
			"title":     "Waiting on the caller",
			"fromRef":   map[string]any{"id": "refs/heads/feature/mcp-review-me"},
			"toRef":     map[string]any{"id": "refs/heads/master"},
			"reviewers": []map[string]any{{"user": map[string]any{"name": harness.username()}}},
		})
	if err != nil {
		t.Fatalf("create the pull request to review failed: %v", err)
	}
	waiting := fmt.Sprintf("%d", int64(authored["id"].(float64)))

	// The caller's own pull request in the sibling repository.
	if err := harness.pushCommitOnBranch(key, sibling, "feature/mcp-elsewhere", "elsewhere.txt"); err != nil {
		t.Fatalf("push the sibling branch failed: %v", err)
	}
	elsewhere, err := harness.createPullRequest(ctx, key, sibling, "feature/mcp-elsewhere", "master")
	if err != nil {
		t.Fatalf("create the sibling pull request failed: %v", err)
	}

	base := fmt.Sprintf("bitbucket://projects/%s/repos/%s", key, slug)
	pullRequest := base + "/pull-requests/" + mine
	fileURI := base + "/files/" + fileName + "?at=" + url.QueryEscape(branch)
	auditPath := filepath.Join(t.TempDir(), "mcp-audit.jsonl")

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		read := func(t *testing.T, uri string) *mcp.ReadResourceResult {
			t.Helper()
			result, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
			if err != nil {
				t.Fatalf("read %s: %v", uri, err)
			}
			if len(result.Contents) == 0 {
				t.Fatalf("read %s: no contents", uri)
			}
			if result.CacheScope != "private" || result.TTLMs != 0 {
				t.Errorf("read %s: cacheScope %q ttlMs %d, want private and 0", uri, result.CacheScope, result.TTLMs)
			}
			return result
		}
		refused := func(t *testing.T, uri, why string) {
			t.Helper()
			_, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
			var wire *jsonrpc.Error
			if !errors.As(err, &wire) || wire.Code != jsonrpc.CodeInvalidParams || !strings.Contains(wire.Message, why) {
				t.Errorf("read %s: got %v, want -32602 saying %q", uri, err, why)
			}
		}

		t.Run("every template is listed", func(t *testing.T) {
			templates, err := session.ListResourceTemplates(context.Background(), nil)
			if err != nil {
				t.Fatalf("resources/templates/list: %v", err)
			}
			if len(templates.ResourceTemplates) != 5 {
				t.Errorf("listed %d templates, want 5: %+v", len(templates.ResourceTemplates), templates.ResourceTemplates)
			}
		})

		t.Run("a pull request reads as get_pull_request answers", func(t *testing.T) {
			content := read(t, pullRequest).Contents[0]
			var body struct {
				PullRequest struct {
					ID    json.Number `json:"id"`
					Title string      `json:"title"`
				} `json:"pull_request"`
				ReviewSummary map[string]any `json:"review_summary"`
			}
			if err := json.Unmarshal([]byte(content.Text), &body); err != nil {
				t.Fatalf("decode %s: %v", content.Text, err)
			}
			if content.MIMEType != "application/json" || body.PullRequest.ID.String() != mine || body.ReviewSummary == nil {
				t.Errorf("the pull request reads back as %s %s", content.MIMEType, content.Text)
			}
		})

		t.Run("its diff is the unified diff", func(t *testing.T) {
			content := read(t, pullRequest+"/diff").Contents[0]
			if content.MIMEType != "text/x-diff" || !strings.Contains(content.Text, fileName) || !strings.Contains(content.Text, "+"+fileLine) {
				t.Errorf("the diff reads back as %s %q", content.MIMEType, content.Text)
			}
		})

		t.Run("its open threads carry the comment", func(t *testing.T) {
			content := read(t, pullRequest+"/threads").Contents[0]
			if !strings.Contains(content.Text, threadText) {
				t.Errorf("the open threads do not carry %q: %s", threadText, content.Text)
			}
		})

		t.Run("a file reads back at its branch", func(t *testing.T) {
			content := read(t, fileURI).Contents[0]
			if !strings.Contains(content.Text, fileLine) || content.URI != fileURI {
				t.Errorf("the file reads back at %s as %q", content.URI, content.Text)
			}
		})

		t.Run("an image comes back as itself beside its description", func(t *testing.T) {
			contents := read(t, base+"/files/notes/dot.png?at="+url.QueryEscape(branch)).Contents
			if len(contents) != 2 || contents[1].MIMEType != "image/png" || len(contents[1].Blob) == 0 {
				t.Errorf("the image reads back as %d contents: %+v", len(contents), contents)
			}
		})

		t.Run("a commit reads as get_commit answers", func(t *testing.T) {
			content := read(t, base+"/commits/"+commit).Contents[0]
			if !strings.Contains(content.Text, `"id":"`+commit+`"`) {
				t.Errorf("the commit reads back as %s", content.Text)
			}
		})

		t.Run("what does not exist, or lies outside the scope, is refused", func(t *testing.T) {
			refused(t, base+"/pull-requests/999999", "not found")
			refused(t, fmt.Sprintf("bitbucket://projects/%s/repos/%s/pull-requests/%s", key, sibling, elsewhere), "outside the scope")
		})

		t.Run("the list holds my pull request and the one waiting on my review", func(t *testing.T) {
			list, err := session.ListResources(context.Background(), nil)
			if err != nil {
				t.Fatalf("resources/list: %v", err)
			}
			var uris []string
			for _, resource := range list.Resources {
				uris = append(uris, resource.URI)
			}
			if !slices.Contains(uris, pullRequest) || !slices.Contains(uris, base+"/pull-requests/"+waiting) || len(uris) != 2 {
				t.Errorf("listed %v, want exactly pull requests %s and %s of %s, and not %s of %s", uris, mine, waiting, slug, elsewhere, sibling)
			}
			if list.CacheScope != "private" || list.TTLMs != 60_000 {
				t.Errorf("the list is cacheScope %q ttlMs %d, want private and 60000", list.CacheScope, list.TTLMs)
			}
		})

		t.Run("completions come from the scoped repository", func(t *testing.T) {
			complete := func(template, argument, typed string, given map[string]string) []string {
				result, err := session.Complete(context.Background(), &mcp.CompleteParams{
					Ref:      &mcp.CompleteReference{Type: "ref/resource", URI: template},
					Argument: mcp.CompleteParamsArgument{Name: argument, Value: typed},
					Context:  &mcp.CompleteContext{Arguments: given},
				})
				if err != nil {
					t.Fatalf("complete %s: %v", argument, err)
				}
				return result.Completion.Values
			}
			if ids := complete("bitbucket://projects/{project}/repos/{repo}/pull-requests/{id}", "id", "", nil); !slices.Contains(ids, mine) || !slices.Contains(ids, waiting) {
				t.Errorf("pull requests completed as %v, want %s and %s", ids, mine, waiting)
			}
			files := "bitbucket://projects/{project}/repos/{repo}/files/{+path}{?at}"
			if paths := complete(files, "path", "notes/", map[string]string{"at": branch}); !slices.Contains(paths, fileName) || !slices.Contains(paths, "notes/dot.png") {
				t.Errorf("paths completed as %v", paths)
			}
			if refs := complete(files, "at", "feature/mcp-res", nil); !slices.Contains(refs, branch) {
				t.Errorf("refs completed as %v, want %s", refs, branch)
			}
		})

		t.Run("a prompt embeds the pull request, its diff and its threads", func(t *testing.T) {
			prompt, err := session.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "review_pull_request", Arguments: map[string]string{"id": mine}})
			if err != nil {
				t.Fatalf("prompts/get: %v", err)
			}
			var embedded []string
			for _, message := range prompt.Messages {
				if resource, ok := message.Content.(*mcp.EmbeddedResource); ok {
					embedded = append(embedded, resource.Resource.URI)
				}
			}
			if _, ok := prompt.Messages[0].Content.(*mcp.TextContent); !ok ||
				!slices.Equal(embedded, []string{pullRequest, pullRequest + "/diff", pullRequest + "/threads"}) {
				t.Errorf("the prompt embeds %v", embedded)
			}
		})
	}, "ai", "mcp", "serve", "--project", key, "--repo", slug, "--audit-file", auditPath)

	contents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read the audit log: %v", err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		var record struct {
			Event  string `json:"event"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode audit line %q: %v", line, err)
		}
		counts[record.Event+" "+record.Status]++
	}
	for want, atLeast := range map[string]int{
		"mcp_resource_read success": 6,
		"mcp_resource_read denied":  1,
		"mcp_resource_read error":   1,
		"mcp_resource_list success": 1,
		"mcp_prompt_get success":    1,
	} {
		if counts[want] < atLeast {
			t.Errorf("the audit log has %d %q records, want at least %d: %v", counts[want], want, atLeast, counts)
		}
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		t.Run("project and repository complete from Bitbucket", func(t *testing.T) {
			// By the project's name, which Bitbucket filters on and which is
			// unique, so the project is found however many the instance
			// holds; the answer is its key.
			prompt := &mcp.CompleteReference{Type: "ref/prompt", Name: "explain_pull_request"}
			projects, err := session.Complete(context.Background(), &mcp.CompleteParams{Ref: prompt, Argument: mcp.CompleteParamsArgument{Name: "project", Value: seeded.Name}})
			if err != nil || !slices.Contains(projects.Completion.Values, key) {
				t.Errorf("projects completed as %v, %v; want %s", projects, err, key)
			}
			repositories, err := session.Complete(context.Background(), &mcp.CompleteParams{
				Ref: prompt, Argument: mcp.CompleteParamsArgument{Name: "repo", Value: slug[:3]},
				Context: &mcp.CompleteContext{Arguments: map[string]string{"project": key}},
			})
			if err != nil || !slices.Contains(repositories.Completion.Values, slug) {
				t.Errorf("repositories completed as %v, %v; want %s", repositories, err, slug)
			}
		})

		t.Run("a tool result links the resource it came from", func(t *testing.T) {
			for tool, tc := range map[string]struct {
				arguments map[string]any
				link      string
			}{
				"get_pull_request": {map[string]any{"project": key, "repo": slug, "id": mine}, pullRequest},
				"get_file_content": {map[string]any{"project": key, "repo": slug, "path": fileName, "at": branch}, fileURI},
			} {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: tc.arguments})
				if err != nil || result.IsError {
					t.Fatalf("%s: %v %s", tool, err, mcpResultText(result))
				}
				last := result.Content[len(result.Content)-1]
				if link, ok := last.(*mcp.ResourceLink); !ok || link.URI != tc.link {
					t.Errorf("%s ends with %+v, want a link to %s", tool, last, tc.link)
				}
				if _, ok := result.Content[0].(*mcp.TextContent); !ok {
					t.Errorf("%s does not lead with its text: %+v", tool, result.Content[0])
				}
			}
		})
	}, "ai", "mcp", "serve")
}
