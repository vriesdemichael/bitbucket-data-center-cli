package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
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

// newRecordingClients builds Clients against a server that records the last
// request it saw and replies with a canned JSON body.
func newRecordingClients(t *testing.T, response string, record func(r *http.Request, body []byte)) Clients {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if record != nil {
			record(r, body)
		}
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = w.Write([]byte(response))
	}))
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
	clients := newRecordingClients(t, `{"id":1}`, func(r *http.Request, body []byte) {
		t.Errorf("no request should be made for an invalid anchor, got %s %s", r.Method, r.URL.Path)
	})

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

func TestAddPRCommentRoutesInlineAndPlain(t *testing.T) {
	t.Run("inline anchors the comment", func(t *testing.T) {
		var payload map[string]any
		clients := newRecordingClients(t, `{"id":1}`, func(r *http.Request, body []byte) {
			payload = map[string]any{}
			_ = json.Unmarshal(body, &payload)
		})

		result := callTool(t, specAddPRComment(), clients, map[string]any{
			"project": "TEST", "repo": "demo", "pr_id": "30", "text": "nit",
			"path": "src/main.go", "line": 12, "line_type": "CONTEXT",
		})
		if result.IsError {
			t.Fatalf("expected success, got: %s", resultText(result))
		}

		anchor, ok := payload["anchor"].(map[string]any)
		if !ok {
			t.Fatalf("expected an anchor in the payload, got %#v", payload)
		}
		if anchor["lineType"] != "CONTEXT" {
			t.Fatalf("expected the requested lineType to be forwarded, got %#v", anchor["lineType"])
		}
	})

	t.Run("reply carries the parent", func(t *testing.T) {
		var payload map[string]any
		clients := newRecordingClients(t, `{"id":1}`, func(r *http.Request, body []byte) {
			payload = map[string]any{}
			_ = json.Unmarshal(body, &payload)
		})

		result := callTool(t, specAddPRComment(), clients, map[string]any{
			"project": "TEST", "repo": "demo", "pr_id": "30", "text": "agreed",
			"parent_id": 900,
		})
		if result.IsError {
			t.Fatalf("expected success, got: %s", resultText(result))
		}

		parent, ok := payload["parent"].(map[string]any)
		if !ok {
			t.Fatalf("expected a parent in the payload, got %#v", payload)
		}
		if id, _ := parent["id"].(float64); int(id) != 900 {
			t.Fatalf("expected parent id 900, got %#v", parent["id"])
		}
		if _, hasAnchor := payload["anchor"]; hasAnchor {
			t.Fatalf("a reply must not be anchored, got %#v", payload)
		}
	})

	t.Run("invalid line_type is rejected", func(t *testing.T) {
		clients := newRecordingClients(t, `{"id":1}`, nil)

		result := callTool(t, specAddPRComment(), clients, map[string]any{
			"project": "TEST", "repo": "demo", "pr_id": "30", "text": "nit",
			"path": "src/main.go", "line": 12, "line_type": "SIDEWAYS",
		})
		if !result.IsError {
			t.Fatalf("expected an error result, got: %+v", result)
		}
	})
}

func TestListPullRequestsModeSelection(t *testing.T) {
	t.Run("repo mode queries the repository endpoint", func(t *testing.T) {
		var gotPath string
		var gotQuery url.Values
		clients := newRecordingClients(t, `{"values":[],"isLastPage":true}`, func(r *http.Request, body []byte) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
		})

		result := callTool(t, specListPullRequests(), clients, map[string]any{
			"project": "TEST", "repo": "demo", "role": "author",
		})
		if result.IsError {
			t.Fatalf("expected success, got: %s", resultText(result))
		}
		if gotPath != "/rest/api/latest/projects/TEST/repos/demo/pull-requests" {
			t.Fatalf("unexpected path %q", gotPath)
		}
		if gotQuery.Get("role") != "AUTHOR" {
			t.Fatalf("expected role AUTHOR, got %q", gotQuery.Get("role"))
		}
	})

	t.Run("dashboard mode queries the dashboard endpoint", func(t *testing.T) {
		var gotPath string
		clients := newRecordingClients(t, `{"values":[],"isLastPage":true}`, func(r *http.Request, body []byte) {
			gotPath = r.URL.Path
		})

		result := callTool(t, specListPullRequests(), clients, map[string]any{})
		if result.IsError {
			t.Fatalf("expected success, got: %s", resultText(result))
		}
		if !strings.Contains(gotPath, "dashboard") {
			t.Fatalf("expected the dashboard endpoint, got %q", gotPath)
		}
	})

	t.Run("project without repo is rejected", func(t *testing.T) {
		clients := newRecordingClients(t, `{}`, func(r *http.Request, body []byte) {
			t.Errorf("no request should be made, got %s", r.URL.Path)
		})

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
	})
}

func TestSubmitPRReviewNeedsWorkAction(t *testing.T) {
	var gotMethod string
	var gotPath string
	var payload map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")

		// Every authenticated response carries the current user, which is how
		// needs_work resolves the participant to update.
		w.Header().Set("X-AUSERNAME", "alice")
		if r.URL.Path == "/rest/api/latest/users" {
			_, _ = w.Write([]byte(`{"values":[]}`))
			return
		}

		body, _ := io.ReadAll(r.Body)
		gotMethod = r.Method
		gotPath = r.URL.Path
		payload = map[string]any{}
		_ = json.Unmarshal(body, &payload)

		_, _ = w.Write([]byte(`{"id":30,"state":"OPEN","open":true,"participants":[{"role":"REVIEWER","status":"NEEDS_WORK","user":{"name":"alice"}}]}`))
	}))
	defer srv.Close()

	clients, err := ClientsFromConfig(config.AppConfig{
		BitbucketURL:   srv.URL,
		RequestTimeout: 5 * time.Second,
		RetryCount:     0,
		RetryBackoff:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ClientsFromConfig failed: %v", err)
	}

	result := callTool(t, specSubmitPRReview(), clients, map[string]any{
		"project": "TEST", "repo": "demo", "pr_id": "30", "action": "needs_work",
	})
	if result.IsError {
		t.Fatalf("expected success, got: %s", resultText(result))
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("expected PUT for needs_work, got %s", gotMethod)
	}
	if gotPath != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/participants/alice" {
		t.Fatalf("unexpected participant path %q", gotPath)
	}
	if payload["status"] != "NEEDS_WORK" {
		t.Fatalf("expected a NEEDS_WORK payload, got %#v", payload)
	}
}

func TestSubmitPRReviewRejectsUnknownAction(t *testing.T) {
	clients := newRecordingClients(t, `{}`, nil)

	result := callTool(t, specSubmitPRReview(), clients, map[string]any{
		"project": "TEST", "repo": "demo", "pr_id": "30", "action": "merge",
	})
	if !result.IsError {
		t.Fatalf("expected an error result, got: %+v", result)
	}
}

func TestGetFileContentReturnsRawText(t *testing.T) {
	var gotPath string
	clients := newRecordingClients(t, "package main\n", func(r *http.Request, body []byte) {
		gotPath = r.URL.Path
	})

	result := callTool(t, specGetFileContent(), clients, map[string]any{
		"project": "TEST", "repo": "demo", "path": "src/main.go", "at": "refs/heads/main",
	})
	if result.IsError {
		t.Fatalf("expected success, got: %s", resultText(result))
	}
	if gotPath != "/rest/api/latest/projects/TEST/repos/demo/raw/src/main.go" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if text := resultText(result); !strings.Contains(text, "package main") {
		t.Fatalf("expected the file body, got %q", text)
	}
}

func TestGetFileContentRejectsTraversal(t *testing.T) {
	clients := newRecordingClients(t, `{}`, func(r *http.Request, body []byte) {
		t.Errorf("no request should be made for a traversal path, got %s", r.URL.Path)
	})

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
	clients := newRecordingClients(t, `{"id":1}`, func(r *http.Request, body []byte) {
		t.Errorf("no request should be made, got %s %s", r.Method, r.URL.Path)
	})

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
