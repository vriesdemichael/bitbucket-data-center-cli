package prcmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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
//
// mock-inventory: unreachable-state — one endpoint failing while the rest of the instance answers. A missing plugin gives 404, which is the "not found" case these tests exist to tell apart, and a closed listener would fail every lookup so the error would arrive for the wrong reason.
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
	// The endpoint Bitbucket answers code owners from, made to fail. bb no
	// longer reads the file, so there is one lookup to break rather than two.
	failing := map[string]bool{
		"/rest/ui/latest/projects/PRJ/repos/demo/code-owners": true,
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
// mock-inventory: transport-fault — some reviewers are made to fail and others not, which no live server does on request; the subject is that bb reports both halves instead of one.
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
				} `json:"pullRequest"`
				Added []string `json:"added"`
			} `json:"data"`
		}
		if jsonErr := json.Unmarshal([]byte(out), &decoded); jsonErr != nil {
			t.Fatalf("output was not valid JSON (%v):\n%s", jsonErr, out)
		}
		if decoded.Data.PullRequest.ID != 42 {
			t.Fatalf("pullRequest.id = %d, want 42", decoded.Data.PullRequest.ID)
		}
		if decoded.Data.PullRequest.Title != "Test PR" {
			t.Fatalf("pullRequest.title = %q, want %q", decoded.Data.PullRequest.Title, "Test PR")
		}
		if len(decoded.Data.Added) != 1 || decoded.Data.Added[0] != "bob" {
			t.Fatalf("added = %v, want [bob]", decoded.Data.Added)
		}
	})
}

// Under --json stdout is a machine contract carrying exactly one document. A
// partial failure must not emit a success envelope on top of the failure
// envelope the entry point writes.
// mock-inventory: transport-fault — same injected partial failure; the subject is that the envelope stays a single document.
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

// The same #503 prefix reaches the resolver from two flags as well as from
// CODEOWNERS, and carried the same defect. Fixing it at the lookup covers all
// three; this holds the two flag paths to it.
func TestReviewerFlagsAcceptTheReviewerGroupPrefix(t *testing.T) {
	for _, flags := range [][]string{
		{"--reviewers", "@reviewer-group/cog_product"},
		{"--reviewer-group", "reviewer-group/cog_product"},
		{"--reviewer-group", "@reviewer-group/cog_product"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			server := newReviewerGroupServer(t,
				`{"values":[{"id":10,"name":"cog_product"}]}`,
				map[string]string{"10": `[{"name":"alice"}]`},
			)

			args := append([]string{"--json", "create",
				"--from-ref", "feature/y",
				"--to-ref", "main",
				"--title", "Created PR",
				"--no-default-reviewers",
			}, flags...)

			out, _, err := executePrSplit(t, server.URL, args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, "alice") {
				t.Errorf("expected the group to expand to alice, got:\n%s", out)
			}
			if strings.Contains(out, "reviewer-group/cog_product") {
				t.Errorf("the group token must never be sent as a username, got:\n%s", out)
			}
		})
	}
}

// newReviewerGroupServer answers the lookups `--reviewers @<group>` and
// `--reviewer-group` make, and accepts the create that follows.
//
// It replaces a larger helper that also served .bitbucket/CODEOWNERS, a diff
// and an open-pull-request listing. Those were for bb's own CODEOWNERS
// evaluation, which Bitbucket does now (ADR-080); what is left is the flag
// path, where bb still resolves a reviewer group itself.
func newReviewerGroupServer(t *testing.T, groups string, groupUsers map[string]string) *httptest.Server {
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
			for _, reviewer := range payload.Reviewers {
				reviewers = append(reviewers, map[string]any{
					"user": map[string]any{"name": reviewer.User.Name},
					"role": "REVIEWER",
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 43, "title": "Created PR", "state": "OPEN", "open": true,
				"reviewers": reviewers,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// Ten suites went with bb's CODEOWNERS evaluation.
//
// They pinned a local-checkout read, a least_busy(N) ranking, a fallback from
// an unknown group to a username, and the reviewer-group prefix -- all of it
// bb's own reading of a file whose meaning is Bitbucket's. Bitbucket resolves
// CODEOWNERS itself and bb asks it now (ADR-080), so what those tests asserted
// is either the server's to decide or gone: least_busy is not a strategy
// Bitbucket implements, and a bare name is not an owner to it.
//
// What replaces them is TestLiveCodeOwners* in the live suite, which pins what
// the server answers rather than what we would have.
