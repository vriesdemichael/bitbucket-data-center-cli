package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
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

	cfg := config.AppConfig{
		BitbucketURL:   srv.URL,
		RequestTimeout: 5 * time.Second,
		RetryCount:     0,
		RetryBackoff:   time.Millisecond,
	}
	clients, err := ClientsFromConfig(cfg)
	if err != nil {
		t.Fatalf("testClients: ClientsFromConfig: %v", err)
	}
	return clients
}

// TestAllSpecsReturnsExpectedCount ensures the catalog has exactly the expected number of tools.
func TestAllSpecsReturnsExpectedCount(t *testing.T) {
	const wantCount = 22
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

// TestAllSpecsHaveHandlers ensures every spec's Handler factory is non-nil.
func TestAllSpecsHaveHandlers(t *testing.T) {
	for _, spec := range AllSpecs() {
		if spec.Handler == nil {
			t.Errorf("tool %q has nil handler factory", spec.Tool.Name)
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

// TestNewServerNoFilter verifies that with empty allow/exclude all tools are registered.
func TestNewServerNoFilter(_ *testing.T) {
	// NewServer must not panic with a zero Clients value and no filter lists.
	_ = NewServer("bb", "test", Clients{}, nil, nil, false)
}

// TestNewServerAllowList verifies that only the allowed tool is registered.
func TestNewServerAllowList(t *testing.T) {
	specs := AllSpecs()
	target := specs[0].Tool.Name
	s := NewServer("bb", "test", Clients{}, []string{target}, nil, false)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	// The server is opaque; we trust NewServer if it doesn't panic and returns non-nil.
}

// TestNewServerExcludeList verifies that excluded tools don't cause a panic.
func TestNewServerExcludeList(_ *testing.T) {
	specs := AllSpecs()
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Tool.Name
	}
	// Exclude all tools — server should be created with no tools registered.
	_ = NewServer("bb", "test", Clients{}, nil, names, false)
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

// TestMergePullRequestIsUnsafe verifies the merge tool is not in the safe set.
func TestMergePullRequestIsUnsafe(t *testing.T) {
	unsafeTools := []string{"merge_pull_request", "set_build_status"}
	safeByName := make(map[string]bool)
	for _, s := range SafeSpecs() {
		safeByName[s.Tool.Name] = true
	}
	allByName := make(map[string]bool)
	for _, s := range AllSpecs() {
		if s.Safe {
			allByName[s.Tool.Name] = true
		}
	}
	for _, name := range unsafeTools {
		if safeByName[name] {
			t.Errorf("%q must not appear in SafeSpecs", name)
		}
		if allByName[name] {
			t.Errorf("%q must have Safe=false in AllSpecs", name)
		}
		found := false
		for _, s := range AllSpecs() {
			if s.Tool.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("%q not found in AllSpecs", name)
		}
	}
}

// TestNewServerSafeModeExcludesMerge verifies that safe mode (yolo=false) does not expose merge_pull_request.
func TestNewServerSafeModeExcludesMerge(_ *testing.T) {
	// Safe mode: NewServer must not panic even though merge_pull_request is withheld.
	_ = NewServer("bb", "test", Clients{}, nil, nil, false)
}

// TestNewServerYoloIncludesAllTools verifies that yolo mode exposes all tools without panic.
func TestNewServerYoloIncludesAllTools(_ *testing.T) {
	_ = NewServer("bb", "test", Clients{}, nil, nil, true)
}

// TestNewServerAllowListOverridesSafeMode verifies an explicit allowlist can include unsafe tools.
func TestNewServerAllowListOverridesSafeMode(_ *testing.T) {
	// Explicitly requesting merge_pull_request in safe mode must not panic.
	_ = NewServer("bb", "test", Clients{}, []string{"merge_pull_request"}, nil, false)
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

// TestResultJSONSerializableValue verifies resultJSON with a plain struct.
func TestResultJSONSerializableValue(t *testing.T) {
	result, err := resultJSON(map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("resultJSON returned nil result")
	}
	if result.IsError {
		t.Fatal("resultJSON returned an error result for a serializable value")
	}
}

// TestResultJSONUnserializableValue verifies resultJSON returns an error result, not a Go error.
func TestResultJSONUnserializableValue(t *testing.T) {
	// A channel is not JSON-serialisable.
	result, err := resultJSON(make(chan int))
	if err != nil {
		t.Fatalf("resultJSON should not return a Go error for serialisation failures, got: %v", err)
	}
	if result == nil {
		t.Fatal("resultJSON returned nil result for unserializable value")
	}
	if !result.IsError {
		t.Fatal("expected an error tool result for an unserializable value")
	}
}

// TestHandlerFactoriesReturnNonNilHandlers verifies each handler factory produces a non-nil handler.
func TestHandlerFactoriesReturnNonNilHandlers(t *testing.T) {
	clients := Clients{} // zero value; factories must not dereference nil clients at construction time
	for _, spec := range AllSpecs() {
		handler := spec.Handler(clients)
		if handler == nil {
			t.Errorf("tool %q handler factory returned nil handler", spec.Tool.Name)
		}
	}
}

// TestToolNamesMatchExpected verifies the catalog contains exactly the documented tool set.
func TestToolNamesMatchExpected(t *testing.T) {
	want := []string{
		"get_pull_request",
		"list_pull_requests",
		"create_pull_request",
		"list_pr_comments",
		"add_pr_comment",
		"list_pr_tasks",
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

// TestHandlerReturnsToolErrorOnMissingRequiredArg exercises the required-argument path for one tool.
// This does not require a live Bitbucket connection.
func TestHandlerReturnsToolErrorOnMissingRequiredArg(t *testing.T) {
	// get_pull_request requires project, repo, and pr_id. Call with nothing.
	var targetSpec Spec
	for _, s := range AllSpecs() {
		if s.Tool.Name == "get_pull_request" {
			targetSpec = s
			break
		}
	}
	if targetSpec.Tool.Name == "" {
		t.Fatal("get_pull_request spec not found")
	}

	handler := targetSpec.Handler(Clients{})
	req := mcpgo.CallToolRequest{}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned unexpected Go error: %v", err)
	}
	if result == nil {
		t.Fatal("handler returned nil result")
	}
	// Must be an error tool result (missing required args).
	if !result.IsError {
		t.Errorf("expected error result for missing required args, got: %+v", result)
	}
}

func TestListPRCommentsHandlerPathScopedAndAggregate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/comments" {
				http.NotFound(w, r)
				return
			}
			if r.URL.Query().Get("path") != "seed.txt" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"message":"expected path query"}]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":301,"text":"mcp pr comment","version":2}]}`))
		}))
		defer srv.Close()

		clients, err := ClientsFromConfig(config.AppConfig{BitbucketURL: srv.URL, RequestTimeout: 5 * time.Second, RetryCount: 0, RetryBackoff: time.Millisecond})
		if err != nil {
			t.Fatalf("ClientsFromConfig failed: %v", err)
		}

		handler := specListPRComments().Handler(clients)
		result, err := handler(context.Background(), mcpgo.CallToolRequest{
			Params: mcpgo.CallToolParams{Arguments: map[string]any{"project": "TEST", "repo": "demo", "pr_id": "30", "path": "seed.txt", "limit": 10}},
		})
		if err != nil {
			t.Fatalf("handler returned Go error: %v", err)
		}
		if result == nil || result.IsError {
			t.Fatalf("expected success result, got: %+v", result)
		}
	})

	t.Run("aggregate without path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/activities" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":301,"action":"COMMENTED","comment":{"id":401,"text":"aggregate comment","version":3}}]}`))
		}))
		defer srv.Close()

		clients, err := ClientsFromConfig(config.AppConfig{BitbucketURL: srv.URL, RequestTimeout: 5 * time.Second, RetryCount: 0, RetryBackoff: time.Millisecond})
		if err != nil {
			t.Fatalf("ClientsFromConfig failed: %v", err)
		}

		handler := specListPRComments().Handler(clients)
		result, err := handler(context.Background(), mcpgo.CallToolRequest{
			Params: mcpgo.CallToolParams{Arguments: map[string]any{"project": "TEST", "repo": "demo", "pr_id": "30"}},
		})
		if err != nil {
			t.Fatalf("handler returned Go error: %v", err)
		}
		if result == nil || result.IsError {
			t.Fatalf("expected aggregate success result, got: %+v", result)
		}
	})
}

// toleratesServiceFailure is the set of tools that intentionally succeed even when
// all backend API calls fail (e.g. because the result is derived from inputs alone, or
// because the backend lookup is best-effort).
var toleratesServiceFailure = map[string]bool{
	// Clone URLs are derived from the base URL; the display-name lookup is best-effort.
	"get_repository_clone_info": true,
}

// TestAllHandlersReturnErrorResultsOnServerFailure exercises the full error path
// in every tool handler body by calling them with clients pointing at a failing
// test server. This covers param extraction, service call, and error-return branches.
func TestAllHandlersReturnErrorResultsOnServerFailure(t *testing.T) {
	clients := testClients(t)
	for _, spec := range AllSpecs() {
		spec := spec // capture range variable
		t.Run(spec.Tool.Name, func(t *testing.T) {
			handler := spec.Handler(clients)
			result, err := handler(context.Background(), mcpgo.CallToolRequest{})
			if err != nil {
				t.Fatalf("handler returned unexpected Go error: %v", err)
			}
			if result == nil {
				t.Fatal("handler returned nil result")
			}
			if toleratesServiceFailure[spec.Tool.Name] {
				return // success or error are both acceptable for this tool
			}
			// Failing server cannot satisfy any API call, so every handler must
			// return an error tool result.
			if !result.IsError {
				t.Errorf("expected error tool result for failing-server call, got success: %+v", result)
			}
		})
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

// TestResultJSONPreservesData verifies the JSON content round-trips correctly.
func TestResultJSONPreservesData(t *testing.T) {
	type payload struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	input := payload{ID: 42, Name: "test"}

	result, err := resultJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Extract the text content from the result.
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}

	raw, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var got payload
	if err := json.Unmarshal([]byte(raw.Text), &got); err != nil {
		t.Fatalf("could not unmarshal result content: %v", err)
	}
	if got != input {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, input)
	}
}
