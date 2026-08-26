package prcmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func TestReviewerFlagAliasNormalization(t *testing.T) {
	t.Run("reviewer add", func(t *testing.T) {
		tests := map[string]string{
			"user":              "user",
			"users":             "user",
			"reviewers":         "user",
			"reviewer-group":    "reviewer-group",
			"reviewer-groups":   "reviewer-group",
			"codeowners":        "codeowners",
			"default-reviewers": "default-reviewers",
		}
		for in, want := range tests {
			if got := string(reviewerAddFlagAliases(nil, in)); got != want {
				t.Fatalf("reviewerAddFlagAliases(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("pr create keeps --reviewers as its own flag", func(t *testing.T) {
		tests := map[string]string{
			"reviewers":       "reviewers",
			"reviewer-group":  "reviewer-group",
			"reviewer-groups": "reviewer-group",
			"title":           "title",
		}
		for in, want := range tests {
			if got := string(createReviewerFlagAliases(nil, in)); got != want {
				t.Fatalf("createReviewerFlagAliases(%q) = %q, want %q", in, got, want)
			}
		}
	})

	// The normalization has to survive being attached to a real flag set.
	t.Run("aliases append rather than replace", func(t *testing.T) {
		var users []string
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		flags.StringSliceVar(&users, "user", nil, "")
		flags.SetNormalizeFunc(reviewerAddFlagAliases)

		if err := flags.Parse([]string{"--user", "alice", "--users", "bob", "--reviewers", "carol"}); err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		want := []string{"alice", "bob", "carol"}
		if len(users) != len(want) {
			t.Fatalf("users = %v, want %v", users, want)
		}
		for i := range want {
			if users[i] != want[i] {
				t.Fatalf("users = %v, want %v", users, want)
			}
		}
	})
}

func TestIsAuthor(t *testing.T) {
	tests := []struct {
		name           string
		author         string
		authorUsername string
		candidate      string
		want           bool
	}{
		{name: "matches the username", authorUsername: "alice", candidate: "alice", want: true},
		{name: "matches the username case-insensitively", authorUsername: "alice", candidate: "ALICE", want: true},
		{name: "matches the display name", author: "Alice Smith", candidate: "Alice Smith", want: true},
		{name: "ignores surrounding space", authorUsername: "alice", candidate: "  alice  ", want: true},
		{name: "different user", authorUsername: "alice", candidate: "bob", want: false},
		{name: "empty candidate is never the author", authorUsername: "alice", candidate: "", want: false},
		{name: "no author known", candidate: "alice", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isAuthor(testCase.author, testCase.authorUsername, testCase.candidate); got != testCase.want {
				t.Fatalf("isAuthor(%q, %q, %q) = %v, want %v", testCase.author, testCase.authorUsername, testCase.candidate, got, testCase.want)
			}
		})
	}
}

func TestFormatReviewerList(t *testing.T) {
	if got := formatReviewerList([]string{"alice"}); got != "reviewer alice" {
		t.Fatalf("got %q, want %q", got, "reviewer alice")
	}
	if got := formatReviewerList([]string{"alice", "bob"}); got != "reviewers alice, bob" {
		t.Fatalf("got %q, want %q", got, "reviewers alice, bob")
	}
}

func TestWriteWarning(t *testing.T) {
	buffer := &bytes.Buffer{}
	writeWarning(buffer, "something degraded")
	if got := buffer.String(); got != "warning: something degraded\n" {
		t.Fatalf("got %q", got)
	}

	// A nil writer must be a no-op rather than a panic.
	writeWarning(nil, "ignored")
}

func TestIsMissingResource(t *testing.T) {
	if isMissingResource(nil) {
		t.Fatal("a nil error is not a missing resource")
	}
}

// The server's own username wins over whatever was configured, because the
// configured value may be an email address or differ in case and a mismatch
// leaves the author in the reviewer list.
func TestResolveAuthorUsername(t *testing.T) {
	newConfig := func(serverURL, username string) config.AppConfig {
		return config.AppConfig{
			BitbucketURL:      serverURL,
			BitbucketToken:    "test-token",
			BitbucketUsername: username,
			RequestTimeout:    5 * time.Second,
		}
	}

	t.Run("prefers the authenticated username", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-AUSERNAME", "server-slug")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"values":[]}`))
		}))
		defer server.Close()

		cfg := newConfig(server.URL, "configured.name@example.com")
		got := resolveAuthorUsername(t.Context(), httpclient.NewFromConfig(cfg), cfg)
		if got != "server-slug" {
			t.Fatalf("author = %q, want %q", got, "server-slug")
		}
	})

	t.Run("falls back to the configured username", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cfg := newConfig(server.URL, "configured-user")
		got := resolveAuthorUsername(t.Context(), httpclient.NewFromConfig(cfg), cfg)
		if got != "configured-user" {
			t.Fatalf("author = %q, want %q", got, "configured-user")
		}
	})
}

// A group name that cannot be read because the server failed is not a licence to
// treat the name as a username; only a genuine "no such group" is.
func TestAtGroupShorthandDoesNotMaskServerFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == "/rest/api/latest/users":
			w.Header().Set("X-AUSERNAME", "currentuser")
			_, _ = w.Write([]byte(`{"values":[]}`))
		case path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"version":1,"author":{"user":{"name":"authoruser"}}}`))
		case strings.Contains(path, "reviewer-groups"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors":[{"message":"reviewer groups unavailable"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, _, err := executePrSplit(t, server.URL, "review", "reviewer", "add", "42", "--user", "@some-team")
	if err == nil {
		t.Fatalf("expected the reviewer group failure to surface, got output:\n%s", out)
	}
	if strings.Contains(out, "Added reviewer some-team") {
		t.Fatalf("the group name must not be assigned as a username, got:\n%s", out)
	}
}
