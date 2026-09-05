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

// The two reviewer-group flag suites are live now, in
// TestLiveReviewerGroupFlagsExpandAndAccumulate.
//
// One asserted that --reviewer-group and --reviewer-groups accumulate rather
// than one discarding the other, which is a pflag hazard and bb's own; the
// other that "@reviewer-group/<name>" reaches a group through either flag,
// which is #503. Both ran against groups this file invented, so the members
// they then required back were the members it had written.
//
// The live version makes two real groups with one real member each, and a
// pull request that names both flags has to arrive with both people on it.
