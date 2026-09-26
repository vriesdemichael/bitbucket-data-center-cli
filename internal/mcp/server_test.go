package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// testClients builds Clients against a listener that has been closed, so every
// request fails at the transport.
//
// The subject is our handlers: each one has a real, non-nil client and has to
// return an error rather than panic. A server answering 500 to everything did
// the same job and made a claim besides -- that Bitbucket answers 500 for each
// of these routes -- which it does not, and which no assertion here needs.
func testClients(t *testing.T) Clients {
	t.Helper()

	return clientsForURL(t, testsupport.RefusedURL)
}

// clientsForURL builds Clients against an arbitrary base URL.
func clientsForURL(t *testing.T, baseURL string) Clients {
	t.Helper()
	clients, err := ClientsFromConfig(config.AppConfig{
		BitbucketURL:   baseURL,
		RequestTimeout: 5 * time.Second,
		RetryCount:     0,
		RetryBackoff:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ClientsFromConfig: %v", err)
	}
	return clients
}

// connect starts an MCP server over an in-memory transport and returns a
// connected client session. This is a real client-to-server round trip through
// the SDK's own encoding, validation and dispatch — the only way to observe
// what a tool actually puts on the wire.
func connect(t *testing.T, clients Clients, allow, exclude []string) *mcp.ClientSession {
	t.Helper()
	return connectWith(t, ServerOptions{
		Name:    "bb",
		Version: "test",
		Clients: clients,
		Allow:   allow,
		Exclude: exclude,
	})
}

// connectWith is connect with the full options struct, for the governance tests.
func connectWith(t *testing.T, opts ServerOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := NewServer(opts)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	client := mcp.NewClient(&mcp.Implementation{Name: "bb-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// listToolNames returns the tool names the server advertises over tools/list.
func listToolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestAllSpecsReturnsExpectedCount ensures the catalog has exactly the expected number of tools.
func TestAllSpecsReturnsExpectedCount(t *testing.T) {
	t.Parallel()

	const wantCount = 24
	specs := AllSpecs()
	if len(specs) != wantCount {
		t.Errorf("AllSpecs: got %d tools, want %d", len(specs), wantCount)
	}
}

// TestAllSpecsHaveNonEmptyNames ensures every spec has a non-empty tool name.
func TestAllSpecsHaveNonEmptyNames(t *testing.T) {
	t.Parallel()

	for i, spec := range AllSpecs() {
		if strings.TrimSpace(spec.Tool.Name) == "" {
			t.Errorf("spec[%d] has empty tool name", i)
		}
	}
}

// TestAllSpecsHaveNonEmptyDescriptions ensures every spec has a non-empty description.
func TestAllSpecsHaveNonEmptyDescriptions(t *testing.T) {
	t.Parallel()

	for _, spec := range AllSpecs() {
		if strings.TrimSpace(spec.Tool.Description) == "" {
			t.Errorf("tool %q has empty description", spec.Tool.Name)
		}
	}
}

// TestAllSpecsHaveRegistrars ensures every spec can register itself.
func TestAllSpecsHaveRegistrars(t *testing.T) {
	t.Parallel()

	for _, spec := range AllSpecs() {
		if spec.Register == nil {
			t.Errorf("tool %q has nil Register", spec.Tool.Name)
		}
	}
}

// TestEveryToolDeclaresItsHintsAndTitle holds every tool to saying all four
// hints and a title outright.
//
// Clients act on the hints: some skip their own prompt for a read-only tool,
// and some prompt unless a write says it is neither destructive nor open-world.
// A hint left out falls back to its pessimistic default, which says something
// about the tool that nobody decided. readOnly and writes declare every hint;
// this catches a tool whose annotations are built by hand. The SDK writes
// readOnlyHint and idempotentHint whatever they hold, so only the two pointers
// can go missing.
func TestEveryToolDeclaresItsHintsAndTitle(t *testing.T) {
	t.Parallel()

	for _, spec := range AllSpecs() {
		annotations := spec.Tool.Annotations
		switch {
		case annotations == nil:
			t.Errorf("tool %q has no annotations", spec.Tool.Name)
		case annotations.DestructiveHint == nil:
			t.Errorf("tool %q leaves destructiveHint to its default", spec.Tool.Name)
		case annotations.OpenWorldHint == nil:
			t.Errorf("tool %q leaves openWorldHint to its default", spec.Tool.Name)
		case strings.TrimSpace(annotations.Title) == "" || spec.Tool.Title != annotations.Title:
			t.Errorf("tool %q: title %q and annotation title %q must both be set, and agree",
				spec.Tool.Name, spec.Tool.Title, annotations.Title)
		}
	}
}

// TestNoToolIsOpenWorld: bb talks to one Bitbucket Data Center instance, which
// is a closed domain. readOnly says when that would change.
func TestNoToolIsOpenWorld(t *testing.T) {
	t.Parallel()

	for _, spec := range AllSpecs() {
		if hint := spec.Tool.Annotations.OpenWorldHint; hint != nil && *hint {
			t.Errorf("tool %q is annotated open-world", spec.Tool.Name)
		}
	}
}

// TestReadOnlyToolsDoNotAsk: a tool that changes nothing has nothing for the
// person to confirm. The two facts are declared apart, the hint on the tool
// and the asking on its spec, which is what makes comparing them worth doing.
func TestReadOnlyToolsDoNotAsk(t *testing.T) {
	t.Parallel()

	for _, spec := range AllSpecs() {
		if spec.ReadOnly() && spec.Asks != AsksNever {
			t.Errorf("tool %q is annotated read-only but asks (%s): either it writes after all, or there is nothing to confirm",
				spec.Tool.Name, spec.Asks)
		}
	}
}

// TestAllSpecsHaveUniqueNames ensures no two tools share the same name.
func TestAllSpecsHaveUniqueNames(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	for i, spec := range AllSpecs() {
		if prev, ok := seen[spec.Tool.Name]; ok {
			t.Errorf("duplicate tool name %q at index %d (first seen at %d)", spec.Tool.Name, i, prev)
		} else {
			seen[spec.Tool.Name] = i
		}
	}
}

// TestNewServerExposesEveryToolByDefault checks through a real tools/list that
// nothing is withheld unless a filter says so. The tools that ask are listed to
// every client, whether it can show a confirmation or not: the list must not
// vary per connection.
func TestNewServerExposesEveryToolByDefault(t *testing.T) {
	t.Parallel()

	session := connect(t, Clients{}, nil, nil)
	if got, want := len(listToolNames(t, session)), len(AllSpecs()); got != want {
		t.Errorf("tools/list returned %d tools, want all %d", got, want)
	}
}

// TestNewServerAllowListKeepsOnlyTheNamedTools verifies --tools narrows the
// listing to what it names.
func TestNewServerAllowListKeepsOnlyTheNamedTools(t *testing.T) {
	t.Parallel()

	session := connect(t, Clients{}, []string{"merge_pull_request"}, nil)
	got := listToolNames(t, session)
	if len(got) != 1 || got[0] != "merge_pull_request" {
		t.Errorf("tools/list = %v, want exactly [merge_pull_request]", got)
	}
}

// TestNewServerExcludeAppliesAfterAllowList verifies exclude wins in every mode.
func TestNewServerExcludeAppliesAfterAllowList(t *testing.T) {
	t.Parallel()

	session := connect(t, Clients{}, []string{"merge_pull_request"}, []string{"merge_pull_request"})
	if got := listToolNames(t, session); len(got) != 0 {
		t.Errorf("tools/list = %v, want no tools", got)
	}
}

// TestReadOnlyServerExposesOnlyTheReadOnlyTools verifies --read-only through a
// real tools/list.
func TestReadOnlyServerExposesOnlyTheReadOnlyTools(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{Name: "bb", Version: "test", ReadOnly: true})
	got := listToolNames(t, session)

	want := map[string]bool{}
	for _, spec := range AllSpecs() {
		if spec.ReadOnly() {
			want[spec.Tool.Name] = true
		}
	}
	if len(got) != len(want) {
		t.Errorf("a read-only server listed %d tools, want the %d read-only ones: %v", len(got), len(want), got)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("a read-only server listed %q, which writes", name)
		}
	}
}

// TestReadOnlyIsNotOverriddenByTheAllowList: naming a writing tool in --tools
// does not bring it back to a read-only server, as naming one cannot bring
// back a tool a scope withholds.
func TestReadOnlyIsNotOverriddenByTheAllowList(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", ReadOnly: true,
		Allow: []string{"merge_pull_request", "get_pull_request"},
	})
	got := listToolNames(t, session)
	if len(got) != 1 || got[0] != "get_pull_request" {
		t.Errorf("tools/list = %v, want exactly [get_pull_request]", got)
	}
}

// TestServerAdvertisesToolsAndNothingElse: left to itself the SDK advertises
// logging, which the 2026-07-28 revision deprecates and this server never
// sends, and a tool list that changes, which this one never does.
func TestServerAdvertisesToolsAndNothingElse(t *testing.T) {
	t.Parallel()

	result := connect(t, Clients{}, nil, nil).InitializeResult()
	if result == nil || result.Capabilities == nil {
		t.Fatal("the server answered without capabilities")
	}

	// As the client receives them, which is also the only place logging can be
	// asked about without the deprecated field.
	encoded, err := json.Marshal(result.Capabilities)
	if err != nil {
		t.Fatalf("encode capabilities: %v", err)
	}
	var advertised map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &advertised); err != nil {
		t.Fatalf("decode capabilities %s: %v", encoded, err)
	}
	if len(advertised) != 1 || string(advertised["tools"]) != "{}" {
		t.Errorf("capabilities = %s, want tools and nothing else, with no listChanged", encoded)
	}
	if strings.TrimSpace(result.Instructions) == "" {
		t.Error("the server sends no instructions")
	}
}

// TestInstructionsFollowTheConfiguration: a read-only server does not tell the
// model about confirmations it has no tools to ask for, and a confined one says
// where it is confined to.
func TestInstructionsFollowTheConfiguration(t *testing.T) {
	t.Parallel()

	if text := instructions(ServerOptions{}); !strings.Contains(text, "confirm") || strings.Contains(text, "read-only") {
		t.Errorf("default instructions: %q", text)
	}
	if text := instructions(ServerOptions{ReadOnly: true}); !strings.Contains(text, "read-only") || strings.Contains(text, "confirm") {
		t.Errorf("read-only instructions: %q", text)
	}
	if text := instructions(ServerOptions{Scope: Scope{ProjectKey: "PROJ", RepoSlug: "payments"}}); !strings.Contains(text, "PROJ/payments") {
		t.Errorf("scoped instructions do not name the scope: %q", text)
	}
}

// TestToSet covers empty input, normal input, and whitespace trimming.
func TestToSet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input []string
		check string
		want  bool
	}{
		{nil, "anything", false},
		{[]string{}, "anything", false},
		{[]string{"a", "b"}, "a", true},
		{[]string{"a", "b"}, "c", false},
		{[]string{" a "}, "a", true}, // trimmed
	}
	for _, tc := range cases {
		m := toSet(tc.input)
		got := m[tc.check]
		if got != tc.want {
			t.Errorf("toSet(%v)[%q]: got %v, want %v", tc.input, tc.check, got, tc.want)
		}
	}
}

// TestLimitOrDefault pins the substitution an omitted limit relies on.
func TestLimitOrDefault(t *testing.T) {
	t.Parallel()

	cases := []struct{ in, want int }{
		{0, defaultLimit},
		{-1, defaultLimit},
		{1, 1},
		{100, 100},
	}
	for _, tc := range cases {
		if got := limitOrDefault(tc.in); got != tc.want {
			t.Errorf("limitOrDefault(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestToolNamesMatchExpected verifies the catalog contains exactly the documented tool set.
func TestToolNamesMatchExpected(t *testing.T) {
	t.Parallel()

	want := []string{
		"get_pull_request",
		"list_pull_requests",
		"create_pull_request",
		"update_pull_request",
		"list_pr_comments",
		"get_pr_diff",
		"get_file_content",
		"add_pr_comment",
		"submit_pr_review",
		"merge_pull_request",
		"enable_auto_merge",
		"disable_auto_merge",
		"search_repositories",
		"get_repository_clone_info",
		"list_branches",
		"resolve_ref",
		"list_tags",
		"create_tag",
		"get_build_status",
		"set_build_status",
		"list_required_builds",
		"list_commits",
		"get_commit",
		"compare_refs",
	}
	specs := AllSpecs()
	if len(specs) != len(want) {
		t.Fatalf("AllSpecs: got %d, want %d", len(specs), len(want))
	}
	for i, w := range want {
		if specs[i].Tool.Name != w {
			t.Errorf("AllSpecs[%d]: got %q, want %q", i, specs[i].Tool.Name, w)
		}
	}
}

// TestMissingRequiredArgumentIsRejected verifies the SDK validates arguments
// against the declared input schema before the handler runs. Previously each
// handler discarded the error from RequireString and carried on with an empty
// string, so a missing project reached the API as a request for project "".
func TestMissingRequiredArgumentIsRejected(t *testing.T) {
	t.Parallel()

	session := connect(t, testClients(t), nil, nil)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_pull_request",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("tools/call returned a protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected an error result for missing required arguments, got: %+v", result)
	}
}

// TestClientFromConfig verifies that ClientsFromConfig populates all three Clients fields.
func TestClientFromConfig(t *testing.T) {
	t.Parallel()

	clients := testClients(t)
	if clients.HTTP == nil {
		t.Error("HTTP client is nil")
	}
	if clients.OpenAPI == nil {
		t.Error("OpenAPI client is nil")
	}
	if clients.BaseURL == "" {
		t.Error("BaseURL is empty")
	}
	if strings.HasSuffix(clients.BaseURL, "/") {
		t.Errorf("BaseURL should not have trailing slash, got %q", clients.BaseURL)
	}
}

// TestBuildCloneURLs exercises the URL construction helper.
func TestBuildCloneURLs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		baseURL   string
		project   string
		repo      string
		wantHTTPS string
		wantSSH   string
		wantErr   bool
	}{
		{
			name:      "standard http",
			baseURL:   "http://bitbucket.example.com",
			project:   "PROJ",
			repo:      "my-repo",
			wantHTTPS: "http://bitbucket.example.com/scm/proj/my-repo.git",
			wantSSH:   "git@bitbucket.example.com:scm/proj/my-repo.git",
		},
		{
			name:      "https with context path",
			baseURL:   "https://bb.example.com/bitbucket",
			project:   "TEAM",
			repo:      "service",
			wantHTTPS: "https://bb.example.com/bitbucket/scm/team/service.git",
			wantSSH:   "git@bb.example.com:scm/team/service.git",
		},
		{
			name:      "trailing slash stripped",
			baseURL:   "https://bb.example.com/",
			project:   "P",
			repo:      "r",
			wantHTTPS: "https://bb.example.com/scm/p/r.git",
			wantSSH:   "git@bb.example.com:scm/p/r.git",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			httpsURL, sshURL, err := buildCloneURLs(tc.baseURL, tc.project, tc.repo)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if httpsURL != tc.wantHTTPS {
				t.Errorf("HTTPS URL: got %q, want %q", httpsURL, tc.wantHTTPS)
			}
			if sshURL != tc.wantSSH {
				t.Errorf("SSH URL: got %q, want %q", sshURL, tc.wantSSH)
			}
		})
	}
}

// TestTitledCopiesTheAnnotationTitle: clients on 2025-06-18 and later read a
// tool's own title and 2025-03-26 clients only the annotation's, so toolSpec
// gives the tool the title its annotations carry. A tool with no annotations
// is left alone rather than panicking while the package initialises;
// TestEveryToolDeclaresItsHintsAndTitle is what refuses one.
func TestTitledCopiesTheAnnotationTitle(t *testing.T) {
	t.Parallel()

	type in struct{}
	type out struct {
		Value string `json:"value"`
	}
	handler := func(Clients) mcp.ToolHandlerFor[in, out] {
		return func(context.Context, *mcp.CallToolRequest, in) (*mcp.CallToolResult, out, error) {
			return nil, out{}, nil
		}
	}

	if spec := toolSpec(&mcp.Tool{Name: "titled", Annotations: readOnly("Titled tool")}, handler); spec.Tool.Title != "Titled tool" {
		t.Errorf("title = %q, want the annotation's", spec.Tool.Title)
	}
	if spec := toolSpec(&mcp.Tool{Name: "own_title", Title: "Own", Annotations: readOnly("Annotation")}, handler); spec.Tool.Title != "Own" {
		t.Errorf("title = %q, want the tool's own", spec.Tool.Title)
	}
	if spec := toolSpec(&mcp.Tool{Name: "bare"}, handler); spec.Tool.Title != "" || spec.Asks != AsksNever {
		t.Errorf("a tool with no annotations came back as %+v", spec)
	}
}
