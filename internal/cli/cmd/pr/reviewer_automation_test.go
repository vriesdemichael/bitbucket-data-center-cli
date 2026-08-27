package prcmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/execgit"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// executePrSplit runs the pr command tree with stdout and stderr captured
// separately, so a test can tell a warning apart from command output.
func executePrSplit(t *testing.T, serverURL string, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	var dryRunFlag bool
	filteredArgs := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--json":
			jsonFlag = true
		case "--dry-run":
			dryRunFlag = true
		default:
			filteredArgs = append(filteredArgs, a)
		}
	}

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonFlag },
		DryRunEnabled: func() bool { return dryRunFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, cfgErr := config.LoadFromEnv()
			if cfgErr != nil {
				return config.AppConfig{}, nil, cfgErr
			}
			client, clientErr := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, clientErr
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return testChecker{}
		},
	}

	root := New(deps)
	// The real root command silences these, and a test that asserts on stdout
	// has to see the same stdout production does.
	root.SilenceUsage = true
	root.SilenceErrors = true

	outBuffer := &bytes.Buffer{}
	errBuffer := &bytes.Buffer{}
	root.SetOut(outBuffer)
	root.SetErr(errBuffer)
	root.SetArgs(filteredArgs)

	err = root.Execute()
	return outBuffer.String(), errBuffer.String(), err
}

// Registering "--users" and "--reviewers" as extra flags bound to the same slice
// looked like an alias but was not: pflag tracks "has this flag been set" per
// flag, so the first use of each spelling replaced the slice and every earlier
// value was dropped without a word.
func TestReviewerFlagAliasesAccumulate(t *testing.T) {
	server := newMockPRServer(t)

	t.Run("reviewer add merges every user spelling", func(t *testing.T) {
		out, err := executePr(t, server.URL,
			"review", "reviewer", "add", "42",
			"--user", "bob",
			"--users", "charlie",
			"--reviewers", "dave",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, want := range []string{"bob", "charlie", "dave"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in output, got:\n%s", want, out)
			}
		}
	})

	t.Run("reviewer add merges every reviewer-group spelling", func(t *testing.T) {
		out, err := executePr(t, server.URL,
			"review", "reviewer", "add", "42",
			"--reviewer-group", "core-team",
			"--reviewer-groups", "go-team",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// core-team expands to bob and charlie, go-team to gopher.
		for _, want := range []string{"bob", "charlie", "gopher"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in output, got:\n%s", want, out)
			}
		}
	})

	t.Run("pr create merges every reviewer-group spelling", func(t *testing.T) {
		out, _, err := executePrSplit(t, server.URL, "--json", "create",
			"--from-ref", "feature/y",
			"--to-ref", "main",
			"--title", "Created PR",
			"--no-default-reviewers", "--no-codeowners",
			"--reviewer-group", "core-team",
			"--reviewer-groups", "go-team",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, want := range []string{"bob", "charlie", "gopher"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in created reviewers, got:\n%s", want, out)
			}
		}
	})

	t.Run("pr create still accepts its own --reviewers flag", func(t *testing.T) {
		out, _, err := executePrSplit(t, server.URL, "--json", "create",
			"--from-ref", "feature/y",
			"--to-ref", "main",
			"--title", "Created PR",
			"--no-default-reviewers", "--no-codeowners",
			"--reviewers", "alice,bob",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, want := range []string{"alice", "bob"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in created reviewers, got:\n%s", want, out)
			}
		}
	})
}

// newReviewerFailureServer serves the happy path but fails the endpoints named
// in failPaths with a 500, so a test can pick exactly which lookup breaks.
func newReviewerFailureServer(t *testing.T, failPaths map[string]bool) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if failPaths[path] {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors":[{"message":"upstream exploded"}]}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/users":
			w.Header().Set("X-AUSERNAME", "currentuser")
			_, _ = w.Write([]byte(`{"values":[]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo":
			_, _ = w.Write([]byte(`{"id":77,"slug":"demo"}`))

		case r.Method == http.MethodGet && path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/reviewers":
			_, _ = w.Write([]byte(`[{"reviewers":[{"name":"defaultuser"}]}]`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			var payload struct {
				Title     string `json:"title"`
				Reviewers []struct {
					User struct {
						Name string `json:"name"`
					} `json:"user"`
				} `json:"reviewers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			var reviewers []map[string]any
			for _, rev := range payload.Reviewers {
				reviewers = append(reviewers, map[string]any{
					"user": map[string]any{"name": rev.User.Name, "displayName": rev.User.Name},
					"role": "REVIEWER",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        43,
				"title":     payload.Title,
				"state":     "OPEN",
				"open":      true,
				"fromRef":   map[string]any{"displayId": "feature/y"},
				"toRef":     map[string]any{"displayId": "main"},
				"reviewers": reviewers,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// Default reviewers are commonly mandatory approvers. Swallowing a failed lookup
// produced a pull request that silently skipped them.
func TestPRCreateSurfacesDefaultReviewerFailure(t *testing.T) {
	failing := map[string]bool{
		"/rest/default-reviewers/latest/projects/PRJ/repos/demo/reviewers": true,
	}

	t.Run("warns and still creates when the feature is only on by default", func(t *testing.T) {
		server := newReviewerFailureServer(t, failing)

		stdout, stderr, err := executePrSplit(t, server.URL, "create",
			"--from-ref", "feature/y",
			"--to-ref", "main",
			"--title", "Created PR",
			"--no-codeowners",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, "Created pull request #43") {
			t.Fatalf("expected the pull request to still be created, got:\n%s", stdout)
		}
		if !strings.Contains(stderr, "could not resolve default reviewers") {
			t.Fatalf("expected a warning on stderr, got:\n%s", stderr)
		}
	})

	t.Run("fails when the user asked for default reviewers explicitly", func(t *testing.T) {
		server := newReviewerFailureServer(t, failing)

		_, _, err := executePrSplit(t, server.URL, "create",
			"--from-ref", "feature/y",
			"--to-ref", "main",
			"--title", "Created PR",
			"--default-reviewers",
			"--no-codeowners",
		)
		if err == nil {
			t.Fatal("expected an error when --default-reviewers was requested explicitly")
		}
	})

	t.Run("warnings never reach stdout in json mode", func(t *testing.T) {
		server := newReviewerFailureServer(t, failing)

		stdout, stderr, err := executePrSplit(t, server.URL, "--json", "create",
			"--from-ref", "feature/y",
			"--to-ref", "main",
			"--title", "Created PR",
			"--no-codeowners",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stderr, "could not resolve default reviewers") {
			t.Fatalf("expected the warning on stderr, got:\n%s", stderr)
		}
		var decoded map[string]any
		if jsonErr := json.Unmarshal([]byte(stdout), &decoded); jsonErr != nil {
			t.Fatalf("stdout was not valid JSON (%v):\n%s", jsonErr, stdout)
		}
	})
}

// A CODEOWNERS lookup that fails for a reason other than "no such file" must not
// be mistaken for a repository that simply does not use CODEOWNERS.
func TestPRCreateSurfacesCodeOwnersFailure(t *testing.T) {
	failing := map[string]bool{
		"/rest/api/latest/projects/PRJ/repos/demo/raw/.bitbucket/CODEOWNERS": true,
		"/rest/api/latest/projects/PRJ/repos/demo/raw/CODEOWNERS":            true,
	}

	t.Run("warns and still creates when the feature is only on by default", func(t *testing.T) {
		server := newReviewerFailureServer(t, failing)

		stdout, stderr, err := executePrSplit(t, server.URL, "create",
			"--from-ref", "feature/y",
			"--to-ref", "main",
			"--title", "Created PR",
			"--no-default-reviewers",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, "Created pull request #43") {
			t.Fatalf("expected the pull request to still be created, got:\n%s", stdout)
		}
		if !strings.Contains(stderr, "could not resolve code owners") {
			t.Fatalf("expected a warning on stderr, got:\n%s", stderr)
		}
	})

	t.Run("fails when the user asked for code owners explicitly", func(t *testing.T) {
		server := newReviewerFailureServer(t, failing)

		_, _, err := executePrSplit(t, server.URL, "create",
			"--from-ref", "feature/y",
			"--to-ref", "main",
			"--title", "Created PR",
			"--no-default-reviewers",
			"--codeowners",
		)
		if err == nil {
			t.Fatal("expected an error when --codeowners was requested explicitly")
		}
	})
}

// A repository without a CODEOWNERS file is not a failure; the pull request is
// created and nothing is said about it.
func TestPRCreateSkipsAbsentCodeOwnersQuietly(t *testing.T) {
	server := newReviewerFailureServer(t, nil)

	stdout, stderr, err := executePrSplit(t, server.URL, "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
		"--codeowners",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Created pull request #43") {
		t.Fatalf("expected the pull request to be created, got:\n%s", stdout)
	}
	if strings.Contains(stderr, "Warning:") {
		t.Fatalf("an absent CODEOWNERS file should not warn, got:\n%s", stderr)
	}
}

// Adding reviewers is one request per reviewer, so a failure partway through
// leaves earlier reviewers attached. Aborting on the first error reported total
// failure and hid what had already been applied.
func TestPRReviewerAddReportsPartialSuccess(t *testing.T) {
	var added []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/users":
			w.Header().Set("X-AUSERNAME", "currentuser")
			_, _ = w.Write([]byte(`{"values":[]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"version":1,"author":{"user":{"name":"authoruser"}},"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/participants":
			var payload struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload.User.Name == "charlie" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"message":"charlie is not a valid user"}]}`))
				return
			}
			added = append(added, payload.User.Name)
			_, _ = w.Write([]byte(`{"user":{"name":"` + payload.User.Name + `"},"role":"REVIEWER","status":"UNAPPROVED"}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, _, err := executePrSplit(t, server.URL,
		"review", "reviewer", "add", "42",
		"--user", "bob", "--user", "charlie", "--user", "dave",
	)

	if err == nil {
		t.Fatal("expected the failed reviewer to surface as an error")
	}
	if !strings.Contains(err.Error(), "charlie") {
		t.Fatalf("error should name the reviewer that failed, got: %v", err)
	}
	if !strings.Contains(stdout, "bob") || !strings.Contains(stdout, "dave") {
		t.Fatalf("expected the reviewers that succeeded to be reported, got:\n%s", stdout)
	}
	if len(added) != 2 || added[0] != "bob" || added[1] != "dave" {
		t.Fatalf("a failure must not stop the remaining reviewers, server saw %v", added)
	}
}

// Bitbucket answers the participants endpoint with a participant, not a pull
// request, so using that response as the pull request produced "#0".
func TestPRReviewerAddReportsTheRealPullRequestID(t *testing.T) {
	server := newMockPRServer(t)

	t.Run("text output", func(t *testing.T) {
		out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--user", "bob")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(out, "#0") {
			t.Fatalf("pull request ID was taken from the participant response, got:\n%s", out)
		}
		if !strings.Contains(out, "pull request #42") {
			t.Fatalf("expected pull request #42 in output, got:\n%s", out)
		}
	})

	t.Run("json output", func(t *testing.T) {
		out, err := executePr(t, server.URL, "--json", "review", "reviewer", "add", "42", "--user", "bob")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var decoded struct {
			Data struct {
				PullRequest struct {
					ID    int64  `json:"id"`
					Title string `json:"title"`
				} `json:"pull_request"`
				Added []string `json:"added"`
			} `json:"data"`
		}
		if jsonErr := json.Unmarshal([]byte(out), &decoded); jsonErr != nil {
			t.Fatalf("output was not valid JSON (%v):\n%s", jsonErr, out)
		}
		if decoded.Data.PullRequest.ID != 42 {
			t.Fatalf("pull_request.id = %d, want 42", decoded.Data.PullRequest.ID)
		}
		if decoded.Data.PullRequest.Title != "Test PR" {
			t.Fatalf("pull_request.title = %q, want %q", decoded.Data.PullRequest.Title, "Test PR")
		}
		if len(decoded.Data.Added) != 1 || decoded.Data.Added[0] != "bob" {
			t.Fatalf("added = %v, want [bob]", decoded.Data.Added)
		}
	})
}

// initGitRepoWithRemote creates a throwaway git repository whose origin points at
// the given Bitbucket project and slug.
func initGitRepoWithRemote(t *testing.T, dir, projectKey, slug string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	remote := "https://bitbucket.example.com/scm/" + projectKey + "/" + slug + ".git"
	commands := [][]string{
		{"init"},
		{"remote", "add", "origin", remote},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = execgit.ScopeFreeEnv()
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v failed: %v (%s)", args, err, output)
		}
	}
}

func writeLocalCodeOwners(t *testing.T, dir, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(dir, ".bitbucket"), 0o755); err != nil {
		t.Fatalf("failed to create .bitbucket: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".bitbucket", "CODEOWNERS"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write CODEOWNERS: %v", err)
	}
}

// The CODEOWNERS lookup reads the working directory before the server. That is
// only sound when the working directory is a checkout of the repository being
// targeted; otherwise `--repo OTHER/repo` picked up reviewers from whatever
// unrelated checkout the command happened to run in.
func TestCodeOwnersIgnoresUnrelatedLocalCheckout(t *testing.T) {
	server := newMockPRServer(t)

	dir := t.TempDir()
	initGitRepoWithRemote(t, dir, "OTHER", "unrelated")
	writeLocalCodeOwners(t, dir, "*.go @local-only-team\n")
	t.Chdir(dir)

	out, _, err := executePrSplit(t, server.URL, "--json", "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "local-only-team") {
		t.Fatalf("CODEOWNERS from an unrelated checkout must not be used, got:\n%s", out)
	}
	// The mock serves "*.go @go-team" for the target repository, which expands
	// to gopher; that is what should have been applied.
	if !strings.Contains(out, "gopher") {
		t.Fatalf("expected the target repository's CODEOWNERS to be used, got:\n%s", out)
	}
}

// When the working directory really is a checkout of the target repository the
// local file remains the fastest source of truth.
func TestCodeOwnersUsesMatchingLocalCheckout(t *testing.T) {
	server := newMockPRServer(t)

	dir := t.TempDir()
	initGitRepoWithRemote(t, dir, "PRJ", "demo")
	writeLocalCodeOwners(t, dir, "*.go alice\n")
	t.Chdir(dir)

	out, _, err := executePrSplit(t, server.URL, "--json", "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("expected the local CODEOWNERS of the target repository to be used, got:\n%s", out)
	}
}

// A directory that is not a git repository at all must not be consulted either.
func TestCodeOwnersIgnoresNonRepositoryWorkingDirectory(t *testing.T) {
	server := newMockPRServer(t)

	dir := t.TempDir()
	writeLocalCodeOwners(t, dir, "*.go @local-only-team\n")
	t.Chdir(dir)

	out, _, err := executePrSplit(t, server.URL, "--json", "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "local-only-team") {
		t.Fatalf("a CODEOWNERS file outside any repository must not be used, got:\n%s", out)
	}
}

// newCodeOwnersServer serves a repository whose CODEOWNERS content, reviewer
// groups and open pull requests are supplied by the caller.
func newCodeOwnersServer(t *testing.T, codeOwners string, groups string, groupUsers map[string]string, openPRs string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/users":
			w.Header().Set("X-AUSERNAME", "currentuser")
			_, _ = w.Write([]byte(`{"values":[]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo":
			_, _ = w.Write([]byte(`{"id":77,"slug":"demo"}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/raw/.bitbucket/CODEOWNERS"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(codeOwners))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
			_, _ = w.Write([]byte(groups))

		case r.Method == http.MethodGet && strings.Contains(path, "/settings/reviewer-groups/") && strings.HasSuffix(path, "/users"):
			segments := strings.Split(strings.Trim(path, "/"), "/")
			id := segments[len(segments)-2]
			body, ok := groupUsers[id]
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"message":"members unavailable"}]}`))
				return
			}
			_, _ = w.Write([]byte(body))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			// An empty openPRs body is the caller's way of asking for a failing
			// pull request listing.
			if openPRs == "" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"message":"pull request listing unavailable"}]}`))
				return
			}
			_, _ = w.Write([]byte(openPRs))

		// A ref-to-ref diff is streamed from the repository patch endpoint.
		case r.Method == http.MethodGet && (strings.HasSuffix(path, "/patch") || strings.HasSuffix(path, "/diff") || strings.Contains(path, "/diff/")):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n"))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			var payload struct {
				Reviewers []struct {
					User struct {
						Name string `json:"name"`
					} `json:"user"`
				} `json:"reviewers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			var reviewers []map[string]any
			for _, rev := range payload.Reviewers {
				reviewers = append(reviewers, map[string]any{
					"user": map[string]any{"name": rev.User.Name},
					"role": "REVIEWER",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        43,
				"title":     "Created PR",
				"state":     "OPEN",
				"open":      true,
				"reviewers": reviewers,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// least_busy(N) ranks candidates by how many unapproved reviews they already
// hold, so the reviewer with the lightest load has to win.
func TestCodeOwnersLeastBusySelection(t *testing.T) {
	// alice carries two unapproved reviews, bob one, carol none.
	openPRs := `{"isLastPage":true,"values":[
		{"id":1,"state":"OPEN","open":true,"participants":[
			{"role":"REVIEWER","approved":false,"user":{"name":"alice"}},
			{"role":"REVIEWER","approved":false,"user":{"name":"bob"}}]},
		{"id":2,"state":"OPEN","open":true,"participants":[
			{"role":"REVIEWER","approved":false,"user":{"name":"alice"}},
			{"role":"REVIEWER","approved":true,"user":{"name":"carol"}}]}
	]}`

	server := newCodeOwnersServer(t,
		"*.go @busy-team:least_busy(1)\n",
		`{"values":[{"id":10,"name":"busy-team"}]}`,
		map[string]string{"10": `[{"name":"alice"},{"name":"bob"},{"name":"carol"}]`},
		openPRs,
	)

	out, _, err := executePrSplit(t, server.URL, "--json", "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "carol") {
		t.Fatalf("expected the least busy member (carol) to be selected, got:\n%s", out)
	}
	if strings.Contains(out, "alice") {
		t.Fatalf("the busiest member must not be selected, got:\n%s", out)
	}
}

// least_busy still has to produce reviewers when the open pull requests cannot
// be read; it just falls back to group order and says so.
func TestCodeOwnersLeastBusyDegradesWithWarning(t *testing.T) {
	server := newCodeOwnersServer(t,
		"*.go @busy-team:least_busy(1)\n",
		`{"values":[{"id":10,"name":"busy-team"}]}`,
		map[string]string{"10": `[{"name":"alice"},{"name":"bob"}]`},
		"", // makes the pull request listing fail
	)

	out, stderr, err := executePrSplit(t, server.URL, "--json", "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("expected a fallback selection in group order, got:\n%s", out)
	}
	if !strings.Contains(stderr, "least_busy") {
		t.Fatalf("expected a warning explaining the degraded ranking, got:\n%s", stderr)
	}
}

// CODEOWNERS also permits "@name" to mean an individual user, so a name that is
// not a reviewer group is read as a username.
func TestCodeOwnersFallsBackToUsernameForUnknownGroup(t *testing.T) {
	server := newCodeOwnersServer(t,
		"*.go @solo-owner\n",
		`{"values":[]}`,
		map[string]string{},
		`{"isLastPage":true,"values":[]}`,
	)

	out, _, err := executePrSplit(t, server.URL, "--json", "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "solo-owner") {
		t.Fatalf("expected the unknown group to be treated as a username, got:\n%s", out)
	}
}

// A group that exists but whose membership cannot be read is a failure, not an
// invitation to invent a username out of the group name.
func TestCodeOwnersGroupMembershipFailureIsNotTreatedAsAUsername(t *testing.T) {
	server := newCodeOwnersServer(t,
		"*.go @core-team\n",
		`{"values":[{"id":10,"name":"core-team"}]}`,
		map[string]string{}, // no entry for id 10 -> the members endpoint 500s
		`{"isLastPage":true,"values":[]}`,
	)

	out, _, err := executePrSplit(t, server.URL, "--json", "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
		"--codeowners",
	)
	if err == nil {
		t.Fatalf("expected the membership failure to surface, got output:\n%s", out)
	}
	if strings.Contains(out, "core-team") {
		t.Fatalf("the group name must not be assigned as a username, got:\n%s", out)
	}
}

// The working copy lookup resolves candidates against the repository root, so it
// does not depend on which subdirectory bb was invoked from.
func TestCodeOwnersReadsLocalFileFromRepositoryRoot(t *testing.T) {
	server := newMockPRServer(t)

	dir := t.TempDir()
	initGitRepoWithRemote(t, dir, "PRJ", "demo")
	writeLocalCodeOwners(t, dir, "*.go alice\n")

	nested := filepath.Join(dir, "internal", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("failed to create nested directory: %v", err)
	}
	t.Chdir(nested)

	out, _, err := executePrSplit(t, server.URL, "--json", "create",
		"--from-ref", "feature/y",
		"--to-ref", "main",
		"--title", "Created PR",
		"--no-default-reviewers",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("expected the repository root CODEOWNERS to be found from a subdirectory, got:\n%s", out)
	}
}

// Under --json stdout is a machine contract carrying exactly one document. A
// partial failure must not emit a success envelope on top of the failure
// envelope the entry point writes.
func TestPRReviewerAddPartialFailureEmitsOneJSONDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/users":
			w.Header().Set("X-AUSERNAME", "currentuser")
			_, _ = w.Write([]byte(`{"values":[]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"version":1,"author":{"user":{"name":"authoruser"}}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/participants":
			var payload struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload.User.Name == "charlie" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"message":"charlie is not a valid user"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"user":{"name":"` + payload.User.Name + `"},"role":"REVIEWER"}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, _, err := executePrSplit(t, server.URL, "--json",
		"review", "reviewer", "add", "42",
		"--user", "bob", "--user", "charlie",
	)

	if err == nil {
		t.Fatal("expected an error for the reviewer that could not be added")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout must be left to the failure envelope, got:\n%s", stdout)
	}
	// The reviewers that were already applied are only recoverable from the
	// message, because the failure envelope carries no payload.
	if !strings.Contains(err.Error(), "bob") || !strings.Contains(err.Error(), "charlie") {
		t.Fatalf("error should name both the added and the failed reviewer, got: %v", err)
	}
}
