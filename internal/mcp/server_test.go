package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
)

// testClients builds Clients pointing at an httptest server that returns HTTP 500
// for every request. This gives every handler a real (non-nil) client so no
// nil pointer dereferences occur, while ensuring every API call returns an error
// so we can exercise the error-return branch without a live Bitbucket instance.
func testClients(t *testing.T) Clients {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"test server error"}]}`))
	}))
	t.Cleanup(srv.Close)
	return clientsForURL(t, srv.URL)
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
func connect(t *testing.T, clients Clients, allow, exclude []string, yolo bool) *mcp.ClientSession {
	t.Helper()
	return connectWith(t, ServerOptions{
		Name:    "bb",
		Version: "test",
		Clients: clients,
		Allow:   allow,
		Exclude: exclude,
		Yolo:    yolo,
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
	const wantCount = 24
	specs := AllSpecs()
	if len(specs) != wantCount {
		t.Errorf("AllSpecs: got %d tools, want %d", len(specs), wantCount)
	}
}

// TestAllSpecsHaveNonEmptyNames ensures every spec has a non-empty tool name.
func TestAllSpecsHaveNonEmptyNames(t *testing.T) {
	for i, spec := range AllSpecs() {
		if strings.TrimSpace(spec.Tool.Name) == "" {
			t.Errorf("spec[%d] has empty tool name", i)
		}
	}
}

// TestAllSpecsHaveNonEmptyDescriptions ensures every spec has a non-empty description.
func TestAllSpecsHaveNonEmptyDescriptions(t *testing.T) {
	for _, spec := range AllSpecs() {
		if strings.TrimSpace(spec.Tool.Description) == "" {
			t.Errorf("tool %q has empty description", spec.Tool.Name)
		}
	}
}

// TestAllSpecsHaveRegistrars ensures every spec can register itself.
func TestAllSpecsHaveRegistrars(t *testing.T) {
	for _, spec := range AllSpecs() {
		if spec.Register == nil {
			t.Errorf("tool %q has nil Register", spec.Tool.Name)
		}
	}
}

// TestAllSpecsHaveAnnotations ensures every tool tells a client whether calling
// it is safe to do without asking. An MCP client uses these hints to decide how
// much ceremony a call deserves, so a missing annotation degrades to the
// pessimistic default and makes read-only tools look dangerous.
func TestAllSpecsHaveAnnotations(t *testing.T) {
	for _, spec := range AllSpecs() {
		if spec.Tool.Annotations == nil {
			t.Errorf("tool %q has no annotations", spec.Tool.Name)
		}
	}
}

// TestUnsafeToolsAreAnnotatedDestructive pins the two classifications together.
// Safe is what the server filters on; DestructiveHint is what a client reads.
// They answer the same question, so a tool withheld without --yolo that tells
// clients it is non-destructive is a contradiction that would mislead an agent.
func TestUnsafeToolsAreAnnotatedDestructive(t *testing.T) {
	for _, spec := range AllSpecs() {
		if spec.Safe {
			continue
		}
		annotations := spec.Tool.Annotations
		if annotations == nil {
			continue // reported by TestAllSpecsHaveAnnotations
		}
		if annotations.ReadOnlyHint {
			t.Errorf("tool %q is withheld without --yolo but annotated read-only", spec.Tool.Name)
		}
		if annotations.DestructiveHint == nil || !*annotations.DestructiveHint {
			t.Errorf("tool %q is withheld without --yolo but not annotated destructive", spec.Tool.Name)
		}
	}
}

// TestSafeToolsAreNotAnnotatedDestructive is the other half of the pairing
// above, and it points the more dangerous way.
//
// TestUnsafeToolsAreAnnotatedDestructive catches a gated tool that tells clients
// it is harmless. This catches the reverse: a tool the server hands out without
// --yolo while its own annotation says it is destructive. That contradiction
// matters more, because the annotation is advice a client may ignore and the
// Safe flag is what the server actually enforces -- so the permissive side wins
// and an agent gets a destructive tool it was never gated on.
//
// A tool that writes without being destructive is normal and stays allowed:
// create_pull_request changes nothing that existed and blocks nothing, which is
// why mutating() takes the flag rather than assuming it.
func TestSafeToolsAreNotAnnotatedDestructive(t *testing.T) {
	for _, spec := range AllSpecs() {
		if !spec.Safe {
			continue
		}
		annotations := spec.Tool.Annotations
		if annotations == nil {
			continue // reported by TestAllSpecsHaveAnnotations
		}
		if annotations.DestructiveHint != nil && *annotations.DestructiveHint {
			t.Errorf(
				"tool %q is exposed without --yolo but annotates itself destructive; "+
					"either it needs gating (Safe: false, listed in TestGatedToolsAreTheOnesThatMergeOrGate) "+
					"or the annotation overstates it",
				spec.Tool.Name,
			)
		}
	}
}

// TestAllSpecsHaveUniqueNames ensures no two tools share the same name.
func TestAllSpecsHaveUniqueNames(t *testing.T) {
	seen := map[string]int{}
	for i, spec := range AllSpecs() {
		if prev, ok := seen[spec.Tool.Name]; ok {
			t.Errorf("duplicate tool name %q at index %d (first seen at %d)", spec.Tool.Name, i, prev)
		} else {
			seen[spec.Tool.Name] = i
		}
	}
}

// TestNewServerExposesSafeToolsByDefault verifies the default filter through a
// real tools/list rather than by trusting NewServer not to panic.
func TestNewServerExposesSafeToolsByDefault(t *testing.T) {
	session := connect(t, Clients{}, nil, nil, false)
	got := listToolNames(t, session)

	want := make(map[string]bool)
	for _, spec := range SafeSpecs() {
		want[spec.Tool.Name] = true
	}
	if len(got) != len(want) {
		t.Errorf("tools/list returned %d tools, want %d safe tools", len(got), len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("tools/list exposed %q, which is not safe by default", name)
		}
	}
}

// TestNewServerYoloExposesEveryTool verifies --yolo lifts the safety filter.
func TestNewServerYoloExposesEveryTool(t *testing.T) {
	session := connect(t, Clients{}, nil, nil, true)
	if got, want := len(listToolNames(t, session)), len(AllSpecs()); got != want {
		t.Errorf("tools/list with yolo returned %d tools, want %d", got, want)
	}
}

// TestNewServerAllowListOverridesSafetyFilter verifies an explicit allowlist can
// name an unsafe tool without --yolo, and suppresses everything else.
func TestNewServerAllowListOverridesSafetyFilter(t *testing.T) {
	session := connect(t, Clients{}, []string{"merge_pull_request"}, nil, false)
	got := listToolNames(t, session)
	if len(got) != 1 || got[0] != "merge_pull_request" {
		t.Errorf("tools/list = %v, want exactly [merge_pull_request]", got)
	}
}

// TestNewServerExcludeAppliesAfterAllowList verifies exclude wins in every mode.
func TestNewServerExcludeAppliesAfterAllowList(t *testing.T) {
	session := connect(t, Clients{}, []string{"merge_pull_request"}, []string{"merge_pull_request"}, false)
	if got := listToolNames(t, session); len(got) != 0 {
		t.Errorf("tools/list = %v, want no tools", got)
	}
}

// TestSafeSpecsSubsetOfAllSpecs verifies SafeSpecs is a strict subset of AllSpecs.
func TestSafeSpecsSubsetOfAllSpecs(t *testing.T) {
	all := AllSpecs()
	safe := SafeSpecs()
	if len(safe) >= len(all) {
		t.Fatalf("expected SafeSpecs (%d) to be a strict subset of AllSpecs (%d)", len(safe), len(all))
	}
	allByName := make(map[string]bool, len(all))
	for _, s := range all {
		allByName[s.Tool.Name] = true
	}
	for _, s := range safe {
		if !s.Safe {
			t.Errorf("SafeSpecs contains tool %q with Safe=false", s.Tool.Name)
		}
		if !allByName[s.Tool.Name] {
			t.Errorf("SafeSpecs contains tool %q not present in AllSpecs", s.Tool.Name)
		}
	}
}

// TestGatedToolsAreWithheld verifies the tools that influence merge gating are
// not exposed without --yolo.
func TestGatedToolsAreWithheld(t *testing.T) {
	gated := []string{"merge_pull_request", "set_build_status", "submit_pr_review", "enable_auto_merge"}
	safeByName := make(map[string]bool)
	for _, s := range SafeSpecs() {
		safeByName[s.Tool.Name] = true
	}
	allByName := make(map[string]bool)
	for _, s := range AllSpecs() {
		allByName[s.Tool.Name] = true
	}
	for _, name := range gated {
		if !allByName[name] {
			t.Errorf("%q not found in AllSpecs", name)
			continue
		}
		if safeByName[name] {
			t.Errorf("%q must not appear in SafeSpecs", name)
		}
	}
}

// TestToSet covers empty input, normal input, and whitespace trimming.
func TestToSet(t *testing.T) {
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
	session := connect(t, testClients(t), nil, nil, false)

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
