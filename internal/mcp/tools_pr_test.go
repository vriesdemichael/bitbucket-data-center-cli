package mcp

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestParseCommaList(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty", input: "", want: nil},
		{name: "blank only", input: "   ", want: nil},
		{name: "commas only", input: " , , ", want: nil},
		{name: "single", input: "alice", want: []string{"alice"}},
		{name: "multiple", input: "alice,bob", want: []string{"alice", "bob"}},
		{name: "trims and skips blanks", input: " alice , , bob ,", want: []string{"alice", "bob"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCommaList(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseCommaList(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

// newUnreachedClients builds Clients against a server that fails the test if a
// request arrives.
//
// Every tool test left in this file is about a refusal that has to happen
// before anything is asked, so the server is the assertion rather than the
// scenery. It used to record and reply, and the tests that read what it replied
// are live now.
func newUnreachedClients(t *testing.T) Clients {
	t.Helper()

	srv := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(srv.Close)

	clients, err := ClientsFromConfig(config.AppConfig{
		BitbucketURL:   srv.URL,
		RequestTimeout: 5 * time.Second,
		RetryCount:     0,
		RetryBackoff:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ClientsFromConfig failed: %v", err)
	}
	return clients
}

// callTool invokes one tool through a real client-to-server round trip.
//
// Going through a session rather than calling the handler directly means these
// tests also cover argument validation and result encoding, which is where the
// wire-shape bug in issue #416 lived. The tool is allowlisted so the safety
// filter does not decide whether the test can reach it.
func callTool(t *testing.T, spec Spec, clients Clients, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	session := connect(t, clients, []string{spec.Tool.Name}, nil, true)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      spec.Tool.Name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("tools/call returned a protocol error: %v", err)
	}
	if result == nil {
		t.Fatal("tools/call returned a nil result")
	}
	return result
}

func TestAddPRCommentRejectsPartialAnchors(t *testing.T) {
	clients := newUnreachedClients(t)

	base := map[string]any{"project": "TEST", "repo": "demo", "pr_id": "30", "text": "hi"}

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "line without path",
			args: map[string]any{"line": 12},
			want: "line requires path",
		},
		{
			name: "path without line",
			args: map[string]any{"path": "src/main.go"},
			want: "requires a positive line",
		},
		{
			name: "path with zero line",
			args: map[string]any{"path": "src/main.go", "line": 0},
			want: "requires a positive line",
		},
		{
			name: "inline combined with parent_id",
			args: map[string]any{"path": "src/main.go", "line": 12, "parent_id": 900},
			want: "parent_id cannot be combined",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := map[string]any{}
			for key, value := range base {
				args[key] = value
			}
			for key, value := range testCase.args {
				args[key] = value
			}

			result := callTool(t, specAddPRComment(), clients, args)
			if !result.IsError {
				t.Fatalf("expected an error result, got: %+v", result)
			}
			if text := resultText(result); !strings.Contains(text, testCase.want) {
				t.Fatalf("expected the error to mention %q, got %q", testCase.want, text)
			}
		})
	}
}

// The two routing cases are live now, in
// TestLiveMCPAddPRCommentRoutesInlineAndReply: the inline comment has to come
// back anchored to the file and line it named, and the reply has to come back
// inside the thread it answered. This decoded the request body its own
// recording handler had just been handed, which says what bb sends and not
// whether Bitbucket keeps it.
//
// What is left is the refusal, which never reaches a request.
func TestAddPRCommentRejectsAnUnknownLineType(t *testing.T) {
	clients := newUnreachedClients(t)

	result := callTool(t, specAddPRComment(), clients, map[string]any{
		"project": "TEST", "repo": "demo", "pr_id": "30", "text": "nit",
		"path": "src/main.go", "line": 12, "line_type": "SIDEWAYS",
	})
	if !result.IsError {
		t.Fatalf("expected an error result, got: %+v", result)
	}
}

// TestListPullRequestsRejectsHalfARepository covers the one thing the mode
// selection decides locally.
//
// Which endpoint each mode reaches, and what each answers, is Bitbucket's --
// TestLiveMCPListPullRequestsModeSelection holds that, including the refusal
// to filter a repository by a role the repository endpoint does not have.
// Naming one half of a repository reaches no endpoint at all, so the recorder
// here is a guard: a request would mean the check did not run.
func TestListPullRequestsRejectsHalfARepository(t *testing.T) {
	clients := newUnreachedClients(t)

	for _, args := range []map[string]any{
		{"project": "TEST"},
		{"repo": "demo"},
	} {
		result := callTool(t, specListPullRequests(), clients, args)
		if !result.IsError {
			t.Fatalf("expected an error result for %#v, got: %+v", args, result)
		}
		if text := resultText(result); !strings.Contains(text, "both project and repo") {
			t.Fatalf("expected a both-or-neither message, got %q", text)
		}
	}
}

func TestSubmitPRReviewRejectsUnknownAction(t *testing.T) {
	clients := newUnreachedClients(t)

	result := callTool(t, specSubmitPRReview(), clients, map[string]any{
		"project": "TEST", "repo": "demo", "pr_id": "30", "action": "merge",
	})
	if !result.IsError {
		t.Fatalf("expected an error result, got: %+v", result)
	}
}

func TestGetFileContentRejectsTraversal(t *testing.T) {
	clients := newUnreachedClients(t)

	result := callTool(t, specGetFileContent(), clients, map[string]any{
		"project": "TEST", "repo": "demo", "path": "../../../etc/passwd",
	})
	if !result.IsError {
		t.Fatalf("expected an error result, got: %+v", result)
	}
}

// resultText flattens a tool result's content into a single string.
func resultText(result *mcp.CallToolResult) string {
	var builder strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func TestAddPRCommentRejectsLineTypeWithoutAnchor(t *testing.T) {
	clients := newUnreachedClients(t)

	result := callTool(t, specAddPRComment(), clients, map[string]any{
		"project": "TEST", "repo": "demo", "pr_id": "30", "text": "hi",
		"line_type": "ADDED",
	})
	if !result.IsError {
		t.Fatalf("expected an error result, got: %+v", result)
	}
	if text := resultText(result); !strings.Contains(text, "line_type only applies") {
		t.Fatalf("expected a line_type message, got %q", text)
	}
}

// TestSubmitPRReviewNeedsWorkAction is live now, in
// TestLiveMCPSubmitReviewMutatesForReal, which submits all three actions as a
// second account -- Bitbucket refuses the author's own review outright -- and
// reads the participant status back after each. The unit version asserted the
// PUT reached a participant path this file had written the reply for, which
// says nothing about whether Bitbucket keeps a NEEDS_WORK sent that way.
