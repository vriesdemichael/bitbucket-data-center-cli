//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
)

// executeLiveMCPServer runs `bb ai mcp serve` and drives a real MCP client
// against it over the command's own stdin and stdout.
//
// Every other live test is invoke-and-assert, which executeLiveCLI expresses.
// The MCP server is a long-running JSON-RPC conversation, so it needs a
// callback that talks to it while it runs. The command runs in-process for the
// same reason the rest of the suite does: a subprocess executes code no
// coverage profile can see, and this is the surface an agent actually calls.
//
// The command words follow the callback, so tools/command-reach counts this
// as an invocation of `ai mcp serve` exactly as it counts the other two helpers.
func executeLiveMCPServer(t *testing.T, drive func(*mcp.ClientSession), args ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Two pipes, crossed: the test writes requests into the command's stdin and
	// reads responses from its stdout.
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	var stderr strings.Builder
	command := cli.NewRootCommand()
	command.SetIn(stdinReader)
	command.SetOut(stdoutWriter)
	command.SetErr(&stderr)
	command.SetArgs(args)

	served := make(chan error, 1)
	go func() {
		err := command.ExecuteContext(ctx)
		// Closing the write half unblocks the client if the server exits
		// early, so a startup failure surfaces as a connect error rather than
		// as the test timing out.
		_ = stdoutWriter.Close()
		served <- err
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "bb-live-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.IOTransport{
		Reader: stdoutReader,
		Writer: stdinWriter,
	}, nil)
	if err != nil {
		t.Fatalf("connect to bb ai mcp serve: %v\nserver stderr: %s", err, stderr.String())
	}

	drive(session)

	_ = session.Close()
	_ = stdinWriter.Close()
	cancel()

	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Error("bb ai mcp serve did not exit after the client disconnected")
	}
}

// TestLiveMCPServerStartsAndListsTools covers the regression that is otherwise
// invisible: the server failing to start at all. Nothing in the unit suite
// drives a process through startup, so a broken serve command would be
// discovered by an agent at runtime.
func TestLiveMCPServerStartsAndListsTools(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		listed, listErr := session.ListTools(context.Background(), nil)
		if listErr != nil {
			t.Fatalf("tools/list failed: %v", listErr)
		}
		if len(listed.Tools) == 0 {
			t.Fatal("tools/list returned no tools")
		}
		for _, tool := range listed.Tools {
			if strings.TrimSpace(tool.Description) == "" {
				t.Errorf("tool %q has no description", tool.Name)
			}
			if tool.OutputSchema == nil {
				t.Errorf("tool %q declares no output schema", tool.Name)
			}
		}
	}, "ai", "mcp", "serve")
}

// TestLiveMCPServerExposesEverySpec pins the catalogue against what a client
// actually receives. A spec registered without a working handler, or a
// registration that panics, fails here rather than in an agent's session.
func TestLiveMCPServerExposesEverySpec(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	want := make([]string, 0, len(bbmcp.AllSpecs()))
	for _, spec := range bbmcp.AllSpecs() {
		want = append(want, spec.Tool.Name)
	}
	sort.Strings(want)

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		got := listedToolNames(t, session)
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("tools/list with --yolo = %v\nwant %v", got, want)
		}
	}, "ai", "mcp", "serve", "--yolo")
}

// TestLiveMCPSafetyGateWithholdsUnsafeTools proves the gate gates.
//
// This is the property a unit test with a stub cannot establish, and the one
// that matters most: --yolo and the Safe classification are what stand between
// a prompt-injected agent and an irreversible merge. A gate that does not gate
// is worse than no gate, because the threat model claims it holds.
func TestLiveMCPSafetyGateWithholdsUnsafeTools(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	withheld := make([]string, 0)
	for _, spec := range bbmcp.AllSpecs() {
		if !spec.Safe {
			withheld = append(withheld, spec.Tool.Name)
		}
	}
	if len(withheld) == 0 {
		t.Fatal("no tools are withheld by default; the safety gate has nothing to prove")
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		exposed := make(map[string]bool)
		for _, name := range listedToolNames(t, session) {
			exposed[name] = true
		}

		for _, name := range withheld {
			if exposed[name] {
				t.Errorf("tool %q is withheld without --yolo but appears in tools/list", name)
			}

			// Not listed is not the same as not callable. An agent that knows
			// the name can still ask for it, so the call itself must fail.
			result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      name,
				Arguments: map[string]any{"project": seeded.Key, "repo": repo.Slug, "pr_id": "1"},
			})
			if callErr == nil && (result == nil || !result.IsError) {
				t.Errorf("calling withheld tool %q succeeded; the safety gate does not gate", name)
			}
		}
	}, "ai", "mcp", "serve")
}

// TestLiveMCPToolFilteringAdmitsAndExcludes covers --tools and --exclude,
// including the precedence between them and the safety filter.
func TestLiveMCPToolFilteringAdmitsAndExcludes(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	t.Run("tools admits exactly the named set", func(t *testing.T) {
		executeLiveMCPServer(t, func(session *mcp.ClientSession) {
			got := listedToolNames(t, session)
			sort.Strings(got)
			if strings.Join(got, ",") != "list_branches,list_tags" {
				t.Errorf("tools/list = %v, want [list_branches list_tags]", got)
			}
		}, "ai", "mcp", "serve", "--tools", "list_branches,list_tags")
	})

	t.Run("tools overrides the safety filter", func(t *testing.T) {
		executeLiveMCPServer(t, func(session *mcp.ClientSession) {
			got := listedToolNames(t, session)
			if len(got) != 1 || got[0] != "merge_pull_request" {
				t.Errorf("tools/list = %v, want [merge_pull_request]", got)
			}
		}, "ai", "mcp", "serve", "--tools", "merge_pull_request")
	})

	t.Run("exclude suppresses a tool that is otherwise safe", func(t *testing.T) {
		executeLiveMCPServer(t, func(session *mcp.ClientSession) {
			for _, name := range listedToolNames(t, session) {
				if name == "list_branches" {
					t.Error("list_branches was excluded but appears in tools/list")
				}
			}
		}, "ai", "mcp", "serve", "--exclude", "list_branches")
	})

	t.Run("exclude wins over an explicit allowlist", func(t *testing.T) {
		executeLiveMCPServer(t, func(session *mcp.ClientSession) {
			if got := listedToolNames(t, session); len(got) != 0 {
				t.Errorf("tools/list = %v, want no tools", got)
			}
		}, "ai", "mcp", "serve", "--tools", "list_branches", "--exclude", "list_branches")
	})
}

// TestLiveMCPReadOnlyToolsAgreeWithCLI is the parity assertion the naming map
// does not make. internal/cli pins which CLI command each tool corresponds to;
// nothing until now asserted the two return the same facts about the same
// repository.
func TestLiveMCPReadOnlyToolsAgreeWithCLI(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The CLI side of the comparison, gathered before the server starts.
	branchOutput, err := executeLiveCLI(t, "--json", "branch", "list")
	if err != nil {
		t.Fatalf("branch list failed: %v\noutput: %s", err, branchOutput)
	}
	wantBranches := displayIDsFromCLI(t, branchOutput, "branches")

	commitOutput, err := executeLiveCLI(t, "--json", "commit", "list", "--limit", "10")
	if err != nil {
		t.Fatalf("commit list failed: %v\noutput: %s", err, commitOutput)
	}
	wantCommits := idsFromCLI(t, commitOutput, "commits")

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()

		t.Run("list_branches", func(t *testing.T) {
			var payload struct {
				Branches []struct {
					DisplayID string `json:"displayId"`
				} `json:"branches"`
			}
			callAndDecode(t, session, callCtx, "list_branches", map[string]any{
				"project": seeded.Key, "repo": repo.Slug,
			}, &payload)

			got := make([]string, 0, len(payload.Branches))
			for _, branch := range payload.Branches {
				got = append(got, branch.DisplayID)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(wantBranches, ",") {
				t.Errorf("list_branches returned %v, but bb branch list returned %v", got, wantBranches)
			}
		})

		t.Run("list_commits", func(t *testing.T) {
			var payload struct {
				Commits []struct {
					ID string `json:"id"`
				} `json:"commits"`
			}
			callAndDecode(t, session, callCtx, "list_commits", map[string]any{
				"project": seeded.Key, "repo": repo.Slug, "limit": 10,
			}, &payload)

			got := make([]string, 0, len(payload.Commits))
			for _, commit := range payload.Commits {
				got = append(got, commit.ID)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(wantCommits, ",") {
				t.Errorf("list_commits returned %v, but bb commit list returned %v", got, wantCommits)
			}
		})

		t.Run("get_repository_clone_info", func(t *testing.T) {
			var payload struct {
				Repository struct {
					Slug          string `json:"slug"`
					CloneURLHTTPS string `json:"clone_url_https"`
				} `json:"repository"`
			}
			callAndDecode(t, session, callCtx, "get_repository_clone_info", map[string]any{
				"project": seeded.Key, "repo": repo.Slug,
			}, &payload)

			if !strings.EqualFold(payload.Repository.Slug, repo.Slug) {
				t.Errorf("clone info slug = %q, want %q", payload.Repository.Slug, repo.Slug)
			}
			if !strings.HasSuffix(payload.Repository.CloneURLHTTPS, ".git") {
				t.Errorf("clone URL %q does not look like a git URL", payload.Repository.CloneURLHTTPS)
			}
		})

		t.Run("search_repositories", func(t *testing.T) {
			var payload struct {
				Repositories []struct {
					Slug string `json:"slug"`
				} `json:"repositories"`
			}
			callAndDecode(t, session, callCtx, "search_repositories", map[string]any{
				"project": seeded.Key,
			}, &payload)

			found := false
			for _, found_ := range payload.Repositories {
				if strings.EqualFold(found_.Slug, repo.Slug) {
					found = true
				}
			}
			if !found {
				t.Errorf("search_repositories did not return the seeded repository %q", repo.Slug)
			}
		})

		// skip_review_summary is the argument an agent reaches for when it
		// wants the pull request and not the reading of it. What it saves is
		// the activity walk, and what a caller can see of that is
		// counts_source: a summary that says "none" is one nothing was
		// counted for.
		//
		// A unit test counted requests to a handler it wrote. Counting is not
		// something a caller can do, and a server does not report it -- the
		// field is what the contract actually offers.
		t.Run("get_pull_request skip_review_summary", func(t *testing.T) {
			branch := fmt.Sprintf("lt-mcp-skip-%d", time.Now().UnixNano()%100000)
			if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "mcp-skip.txt"); err != nil {
				t.Fatalf("push commit on branch failed: %v", err)
			}
			pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
			if err != nil {
				t.Fatalf("create pull request failed: %v", err)
			}

			countsSource := func(t *testing.T, arguments map[string]any) string {
				t.Helper()

				var payload struct {
					ReviewSummary struct {
						CountsSource string `json:"counts_source"`
					} `json:"review_summary"`
				}
				callAndDecode(t, session, callCtx, "get_pull_request", arguments, &payload)

				return payload.ReviewSummary.CountsSource
			}

			base := map[string]any{"project": seeded.Key, "repo": repo.Slug, "id": pullRequestID}
			if got := countsSource(t, base); got == "none" || got == "" {
				t.Errorf("counts_source = %q without skip_review_summary, want the summary to have been read", got)
			}

			skipped := map[string]any{}
			for key, value := range base {
				skipped[key] = value
			}
			skipped["skip_review_summary"] = true
			if got := countsSource(t, skipped); got != "none" {
				t.Errorf("counts_source = %q with skip_review_summary, want %q", got, "none")
			}

			// list_pr_comments against a timeline Bitbucket built. A unit test
			// held these shapes against one written beside it -- the thread
			// count, the ordering, the browser link -- so the summary it
			// checked was a tally of a fixture.
			//
			// The nested-payload guard is the one that matters most here: the
			// activity timeline repeats the entire pull request inside every
			// entry, and forwarding that to an agent spends its context on the
			// same object over and over.
			const remark = "an ordinary remark from the mcp live suite"
			if output, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID, "--text", remark); err != nil {
				t.Fatalf("pr comment add failed: %v\noutput: %s", err, output)
			}

			var comments struct {
				Summary struct {
					Unresolved float64 `json:"unresolved"`
				} `json:"summary"`
				Threads []struct {
					ID       float64 `json:"id"`
					URL      string  `json:"url"`
					Resolved bool    `json:"resolved"`
				} `json:"threads"`
			}
			raw := callAndDecode(t, session, callCtx, "list_pr_comments", map[string]any{
				"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID,
			}, &comments)

			if len(comments.Threads) == 0 {
				t.Fatalf("list_pr_comments returned no threads for a pull request with a comment on it:\n%s", raw)
			}
			if comments.Summary.Unresolved < 1 {
				t.Errorf("an unresolved comment was not counted: %s", raw)
			}
			first := comments.Threads[0]
			if first.ID == 0 || first.URL == "" {
				t.Errorf("a thread came back without an id or a browser link: %s", raw)
			}
			if strings.Contains(raw, `"fromRef"`) || strings.Contains(raw, `"toRef"`) {
				t.Errorf("the pull request payload nested in every activity was forwarded to the caller: %s", raw)
			}

			// path scopes the listing to one file, and reaches a different
			// endpoint to do it: the comments resource rather than the
			// timeline. The anchored comment is posted directly because bb
			// cannot create one, the same way TestLivePullRequestApplySuggestion
			// arranges its subject.
			const anchoredText = "an inline remark from the mcp live suite"
			if _, err := harness.liveJSON(ctx, http.MethodPost,
				fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/comments",
					seeded.Key, repo.Slug, pullRequestID),
				map[string]any{
					"text": anchoredText,
					"anchor": map[string]any{
						"line": 1, "lineType": "ADDED", "fileType": "TO", "path": "mcp-skip.txt",
					},
				}); err != nil {
				t.Fatalf("post an inline comment failed: %v", err)
			}

			var scoped struct {
				Threads []struct {
					Text string `json:"text"`
				} `json:"threads"`
			}
			scopedRaw := callAndDecode(t, session, callCtx, "list_pr_comments", map[string]any{
				"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID, "path": "mcp-skip.txt",
			}, &scoped)

			var sawAnchored, sawPlain bool
			for _, thread := range scoped.Threads {
				switch thread.Text {
				case anchoredText:
					sawAnchored = true
				case remark:
					sawPlain = true
				}
			}
			if !sawAnchored {
				t.Errorf("scoping to a path lost the comment anchored to it: %s", scopedRaw)
			}
			if sawPlain {
				t.Errorf("scoping to a path returned a comment anchored to nothing: %s", scopedRaw)
			}

			// state, against a thread that is resolved because it was
			// resolved. The unit version filtered a fixture whose entries were
			// marked resolved by the same file that then required them back.
			if output, err := executeLiveCLI(t, "--json", "pr", "comment", "resolve", pullRequestID,
				fmt.Sprintf("%d", int(first.ID))); err != nil {
				t.Fatalf("pr comment resolve failed: %v\noutput: %s", err, output)
			}

			byState := func(t *testing.T, state string) []string {
				t.Helper()

				var payload struct {
					Threads []struct {
						Text     string `json:"text"`
						Resolved bool   `json:"resolved"`
					} `json:"threads"`
				}
				stateRaw := callAndDecode(t, session, callCtx, "list_pr_comments", map[string]any{
					"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID, "state": state,
				}, &payload)

				texts := make([]string, 0, len(payload.Threads))
				for _, thread := range payload.Threads {
					texts = append(texts, thread.Text)
					if state == "resolved" && !thread.Resolved {
						t.Errorf("state=resolved returned an unresolved thread: %s", stateRaw)
					}
					if state == "unresolved" && thread.Resolved {
						t.Errorf("state=unresolved returned a resolved thread: %s", stateRaw)
					}
				}

				return texts
			}

			if resolved := byState(t, "resolved"); !slices.Contains(resolved, remark) {
				t.Errorf("state=resolved did not return the thread that was just resolved: %v", resolved)
			}
			if unresolved := byState(t, "unresolved"); slices.Contains(unresolved, remark) {
				t.Errorf("state=unresolved returned the thread that was just resolved: %v", unresolved)
			}
		})
	}, "ai", "mcp", "serve")
}

// TestLiveMCPToolsCommandListsCatalogue covers `bb ai mcp tools`, the companion
// command an operator uses to build an allowlist.
func TestLiveMCPToolsCommandListsCatalogue(t *testing.T) {
	harness := newLiveHarness(t)
	configureLiveCLIEnv(t, harness, "", "")

	output, err := executeLiveCLI(t, "ai", "mcp", "tools")
	if err != nil {
		t.Fatalf("ai mcp tools failed: %v\noutput: %s", err, output)
	}
	for _, spec := range bbmcp.AllSpecs() {
		if !strings.Contains(output, spec.Tool.Name) {
			t.Errorf("ai mcp tools output omits %q", spec.Tool.Name)
		}
	}

	safeOutput, err := executeLiveCLI(t, "ai", "mcp", "tools", "--safe-only")
	if err != nil {
		t.Fatalf("ai mcp tools --safe-only failed: %v\noutput: %s", err, safeOutput)
	}
	if strings.Contains(safeOutput, "merge_pull_request") {
		t.Error("ai mcp tools --safe-only lists merge_pull_request, which is withheld by default")
	}
}

// listedToolNames returns the tool names a session's server advertises.
func listedToolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// callAndDecode calls a tool and unmarshals its structuredContent into target.
// callAndDecode calls a tool, decodes its structured content into target, and
// returns that content as JSON so a caller can also assert on what is not in
// it.
func callAndDecode(t *testing.T, session *mcp.ClientSession, ctx context.Context, name string, args map[string]any, target any) string {
	t.Helper()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: tools/call returned a protocol error: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("%s: tools/call returned an error result: %s", name, mcpResultText(result))
	}
	if result.StructuredContent == nil {
		t.Fatalf("%s: result carries no structuredContent", name)
	}

	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("%s: marshal structuredContent: %v", name, err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("%s: decode structuredContent %s: %v", name, encoded, err)
	}

	return string(encoded)
}

// mcpResultText flattens a tool result's text content for failure messages.
func mcpResultText(result *mcp.CallToolResult) string {
	var parts []string
	for _, item := range result.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// displayIDsFromCLI pulls the sorted displayId values out of a bb --json
// envelope whose data object holds the named collection.
func displayIDsFromCLI(t *testing.T, output, key string) []string {
	t.Helper()
	values := collectionFromCLI(t, output, key)
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if entry, ok := value.(map[string]any); ok {
			ids = append(ids, asString(entry["displayId"]))
		}
	}
	sort.Strings(ids)
	return ids
}

// idsFromCLI pulls the sorted id values out of a bb --json envelope.
func idsFromCLI(t *testing.T, output, key string) []string {
	t.Helper()
	values := collectionFromCLI(t, output, key)
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if entry, ok := value.(map[string]any); ok {
			ids = append(ids, asString(entry["id"]))
		}
	}
	sort.Strings(ids)
	return ids
}

// collectionFromCLI reads a named array out of a bb --json envelope, tolerating
// the envelope holding the array directly.
func collectionFromCLI(t *testing.T, output, key string) []any {
	t.Helper()
	payload := decodeJSONMap(t, output)
	if named, ok := payload[key].([]any); ok {
		return named
	}
	t.Fatalf("bb --json output has no %q collection: %s", key, output)
	return nil
}

// TestLiveMCPScopeBoundaryHolds proves the scope boundary against a real
// Bitbucket, with two projects that both genuinely exist.
//
// A unit test with a failing stub cannot establish this: every call errors
// there, so a boundary that refuses nothing looks identical to one that refuses
// correctly. Here the out-of-scope repository is one the token can really read,
// so a call that gets through returns data — and the test fails.
func TestLiveMCPScopeBoundaryHolds(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	inScope, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed in-scope project failed: %v", err)
	}
	outOfScope, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed out-of-scope project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, inScope.Key, inScope.Repos[0].Slug)

	// The out-of-scope repository is readable without the scope, which is what
	// makes the refusal below meaningful rather than incidental.
	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		var payload struct {
			Branches []struct {
				DisplayID string `json:"displayId"`
			} `json:"branches"`
		}
		callAndDecode(t, session, context.Background(), "list_branches", map[string]any{
			"project": outOfScope.Key, "repo": outOfScope.Repos[0].Slug,
		}, &payload)
		if len(payload.Branches) == 0 {
			t.Fatal("the out-of-scope repository has no branches, so refusing it later would prove nothing")
		}
	}, "ai", "mcp", "serve")

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()

		t.Run("in-scope calls succeed", func(t *testing.T) {
			var payload struct {
				Branches []struct {
					DisplayID string `json:"displayId"`
				} `json:"branches"`
			}
			callAndDecode(t, session, callCtx, "list_branches", map[string]any{
				"project": inScope.Key, "repo": inScope.Repos[0].Slug,
			}, &payload)
			if len(payload.Branches) == 0 {
				t.Error("in-scope list_branches returned nothing")
			}
		})

		t.Run("out-of-scope calls are refused", func(t *testing.T) {
			result, callErr := session.CallTool(callCtx, &mcp.CallToolParams{
				Name: "list_branches",
				Arguments: map[string]any{
					"project": outOfScope.Key, "repo": outOfScope.Repos[0].Slug,
				},
			})
			if callErr != nil {
				t.Fatalf("tools/call returned a protocol error: %v", callErr)
			}
			if !result.IsError {
				t.Fatalf("a call to %s/%s succeeded while scoped to %s; the boundary does not hold",
					outOfScope.Key, outOfScope.Repos[0].Slug, inScope.Key)
			}
			if text := mcpResultText(result); !strings.Contains(text, "outside the scope") {
				t.Errorf("refusal message = %q, want it to name the scope", text)
			}
		})

		t.Run("omitted arguments are bound to the scope", func(t *testing.T) {
			// Dashboard mode would otherwise reach every repository the token
			// can see, which is exactly what the scope exists to prevent.
			var payload struct {
				PullRequests []struct {
					ID int64 `json:"id"`
				} `json:"pull_requests"`
			}
			callAndDecode(t, session, callCtx, "list_pull_requests", map[string]any{}, &payload)
		})

		t.Run("unboundable tools are withheld", func(t *testing.T) {
			for _, name := range listedToolNames(t, session) {
				if name == "get_build_status" || name == "set_build_status" {
					t.Errorf("tool %q is listed under a scope it cannot honour", name)
				}
			}
		})
	}, "ai", "mcp", "serve", "--project", inScope.Key, "--repo", inScope.Repos[0].Slug)
}

// TestLiveMCPAuditTrailRecordsInvocations proves the audit file is written by a
// real server run, with the denial record that Bitbucket's own audit log cannot
// contain — a refused call never reaches it.
func TestLiveMCPAuditTrailRecordsInvocations(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	auditPath := filepath.Join(t.TempDir(), "mcp-audit.jsonl")

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()

		var payload struct {
			Tags []struct {
				DisplayID string `json:"displayId"`
			} `json:"tags"`
		}
		callAndDecode(t, session, callCtx, "list_tags", map[string]any{
			"project": seeded.Key, "repo": repo.Slug,
		}, &payload)

		// Refused by the scope, so it never reaches Bitbucket.
		_, _ = session.CallTool(callCtx, &mcp.CallToolParams{
			Name:      "list_tags",
			Arguments: map[string]any{"project": "NOSUCHPROJECT", "repo": repo.Slug},
		})
	}, "ai", "mcp", "serve", "--project", seeded.Key, "--audit-file", auditPath)

	contents, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	var statuses []string
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record struct {
			Event  string `json:"event"`
			Tool   string `json:"tool"`
			Status string `json:"status"`
			Host   string `json:"host"`
			Scope  string `json:"scope"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("audit line is not valid JSON: %v\nline: %s", err, line)
		}
		if record.Event != "mcp_tool_invocation" {
			t.Errorf("audit event = %q, want mcp_tool_invocation", record.Event)
		}
		if record.Tool != "list_tags" {
			t.Errorf("audit tool = %q, want list_tags", record.Tool)
		}
		if record.Scope != seeded.Key {
			t.Errorf("audit scope = %q, want %q", record.Scope, seeded.Key)
		}
		if strings.TrimSpace(record.Host) == "" {
			t.Error("audit record has no host")
		}
		statuses = append(statuses, record.Status)
	}

	if len(statuses) != 2 {
		t.Fatalf("expected 2 audit records, got %d: %s", len(statuses), contents)
	}
	if statuses[0] != "success" {
		t.Errorf("first audit status = %q, want success", statuses[0])
	}
	if statuses[1] != "denied" {
		t.Errorf("second audit status = %q, want denied; the denial is the event Bitbucket's own audit log cannot hold", statuses[1])
	}
}

// TestLiveMCPSubmitReviewMutatesForReal covers submit_pr_review end to end.
//
// This is the one review action with no other way in: there is no `bb pr review
// needs-work`, so NeedsWork is reachable through MCP and nowhere else. The
// command-reach report says every CLI command is asserted live, and this sits
// outside what that report can see -- its only coverage was a unit test against
// a participant payload the fixture wrote, which is a claim about how Bitbucket
// records a review status, made without asking Bitbucket.
//
// All three actions run against the same pull request, and each is read back
// through a second tool rather than trusted from the write's own answer.
func TestLiveMCPSubmitReviewMutatesForReal(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := fmt.Sprintf("lt-mcp-review-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "mcp-review.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	// A second account is not a convenience here. Bitbucket refuses the
	// author's own review outright -- "Authors may not update their status",
	// InvalidPullRequestRoleException, 400 -- so a test that reviews as the
	// author proves nothing about the three actions, and a mock would never
	// have said so.
	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant the reviewer write access failed: %v", err)
	}
	if _, err := harness.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/participants",
			seeded.Key, repo.Slug, pullRequestID),
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "role": "REVIEWER"}); err != nil {
		t.Fatalf("add the reviewer failed: %v", err)
	}

	// The server runs as the reviewer, because the review does.
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()

		// The two tools spell the same argument differently: submit_pr_review
		// takes pr_id, get_pull_request takes id.
		reviewArguments := map[string]any{"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID}
		readArguments := map[string]any{"project": seeded.Key, "repo": repo.Slug, "id": pullRequestID}

		submit := func(t *testing.T, action string) {
			t.Helper()

			args := map[string]any{}
			for key, value := range reviewArguments {
				args[key] = value
			}
			args["action"] = action

			var payload struct {
				PullRequest struct {
					Reviewers []struct {
						Name     string `json:"name"`
						Status   string `json:"status"`
						Approved bool   `json:"approved"`
					} `json:"reviewers"`
				} `json:"pull_request"`
			}
			callAndDecode(t, session, callCtx, "submit_pr_review", args, &payload)
		}

		statusOf := func(t *testing.T) (string, bool) {
			t.Helper()

			var payload struct {
				PullRequest struct {
					Reviewers []struct {
						Name     string `json:"name"`
						Status   string `json:"status"`
						Approved bool   `json:"approved"`
					} `json:"reviewers"`
				} `json:"pull_request"`
			}
			callAndDecode(t, session, callCtx, "get_pull_request", readArguments, &payload)

			for _, participant := range payload.PullRequest.Reviewers {
				if participant.Name == reviewer.Username {
					return participant.Status, participant.Approved
				}
			}

			return "", false
		}

		t.Run("approve", func(t *testing.T) {
			submit(t, "approve")

			status, approved := statusOf(t)
			if !approved || !strings.EqualFold(status, "APPROVED") {
				t.Fatalf("after approve: status=%q approved=%v", status, approved)
			}
		})

		t.Run("needs_work", func(t *testing.T) {
			// The action with no direct CLI equivalent, and the reason this
			// test exists.
			//
			// It runs after approve on purpose. A participant holds one status
			// -- UNAPPROVED, APPROVED or NEEDS_WORK -- and `approved` is
			// derived from it, so requesting changes does not sit alongside an
			// approval, it replaces it. Reading approved as false here is what
			// proves that rather than assuming it, and an agent that treats the
			// three as independent flags would be wrong about the one state
			// that blocks a merge.
			submit(t, "needs_work")

			status, approved := statusOf(t)
			if approved || !strings.EqualFold(status, "NEEDS_WORK") {
				t.Fatalf("after needs_work: status=%q approved=%v, want the approval replaced", status, approved)
			}
		})

		t.Run("unapprove", func(t *testing.T) {
			// Running after needs_work rather than after approve is the point:
			// despite the name, this clears whichever status is held. There is
			// no separate verb for withdrawing a request for changes, so a
			// reviewer stepping back from one reaches for the command named
			// after undoing an approval.
			submit(t, "unapprove")

			if status, approved := statusOf(t); approved || strings.EqualFold(status, "NEEDS_WORK") {
				t.Fatalf("after unapprove: status=%q approved=%v, want the request for changes cleared too",
					status, approved)
			}
		})
	}, "ai", "mcp", "serve", "--yolo")
}

// TestLiveMCPAddPRCommentRoutesInlineAndReply covers the two shapes
// add_pr_comment builds beyond a plain remark: an anchored comment and a reply.
//
// A unit test decoded the request body its own recording handler had just been
// handed, which says what bb sends and not whether Bitbucket keeps it. Here
// each comment is read back: the inline one has to come back anchored to the
// file and line it named, and the reply has to come back inside the thread it
// answered rather than as a comment of its own.
func TestLiveMCPAddPRCommentRoutesInlineAndReply(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const anchoredFile = "mcp-comment.txt"
	branch := fmt.Sprintf("lt-mcp-comment-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, anchoredFile); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		callCtx := context.Background()
		base := map[string]any{"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID}

		add := func(t *testing.T, extra map[string]any) float64 {
			t.Helper()

			args := map[string]any{}
			for key, value := range base {
				args[key] = value
			}
			for key, value := range extra {
				args[key] = value
			}

			var payload struct {
				Comment struct {
					ID float64 `json:"id"`
				} `json:"comment"`
			}
			raw := callAndDecode(t, session, callCtx, "add_pr_comment", args, &payload)
			if payload.Comment.ID == 0 {
				t.Fatalf("add_pr_comment returned no comment id:\n%s", raw)
			}

			return payload.Comment.ID
		}

		const plainText = "a plain remark from the mcp tool"
		plainID := add(t, map[string]any{"text": plainText})

		const inlineText = "this line needs a guard"
		inlineID := add(t, map[string]any{
			"text": inlineText, "path": anchoredFile, "line": 1, "line_type": "ADDED",
		})

		const replyText = "agreed, will fix"
		replyID := add(t, map[string]any{"text": replyText, "parent_id": plainID})

		// Read back through the same surface an agent would use.
		var listed struct {
			Threads []struct {
				ID     float64 `json:"id"`
				Text   string  `json:"text"`
				Anchor *struct {
					Path string  `json:"path"`
					Line float64 `json:"line"`
				} `json:"anchor"`
				Replies []struct {
					ID   float64 `json:"id"`
					Text string  `json:"text"`
				} `json:"replies"`
			} `json:"threads"`
		}
		raw := callAndDecode(t, session, callCtx, "list_pr_comments", map[string]any{
			"project": seeded.Key, "repo": repo.Slug, "pr_id": pullRequestID, "with_replies": true,
		}, &listed)

		var sawInline, sawReply bool
		for _, thread := range listed.Threads {
			if thread.ID == inlineID {
				if thread.Anchor == nil || thread.Anchor.Path != anchoredFile || int(thread.Anchor.Line) != 1 {
					t.Errorf("the inline comment did not come back anchored where it was put: %#v", thread.Anchor)
				}
				sawInline = true
			}
			if thread.ID == plainID {
				if thread.Anchor != nil {
					t.Errorf("a plain remark came back anchored: %#v", thread.Anchor)
				}
				for _, reply := range thread.Replies {
					if reply.ID == replyID {
						sawReply = true
					}
				}
			}
			if thread.ID == replyID {
				t.Errorf("the reply came back as a thread of its own rather than inside the one it answered:\n%s", raw)
			}
		}
		if !sawInline {
			t.Errorf("the inline comment is not in the listing:\n%s", raw)
		}
		if !sawReply {
			t.Errorf("the reply is not inside the thread it answered:\n%s", raw)
		}
	}, "ai", "mcp", "serve", "--yolo")
}
