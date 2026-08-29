//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli"
	bbmcp "github.com/vriesdemichael/bitbucket-server-cli/internal/mcp"
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
// The command words follow the callback, so tools/cli-live-coverage counts this
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
func callAndDecode(t *testing.T, session *mcp.ClientSession, ctx context.Context, name string, args map[string]any, target any) {
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
