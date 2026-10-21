//go:build live

package live_test

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveGitCredentialHelperSendsWhatBitbucketTakes: the git credential
// helper answers with an access token in a form Bitbucket accepts, for a
// personal token and for a repository token, whether or not git can take an
// authtype (#801).
//
// Over Basic auth Bitbucket takes a personal token only beside its owner's
// login name -- not x-token-auth, and not the slug, which differs from the name
// here -- and a repository token only beside x-token-auth. Bearer takes either.
// Each answer is sent to Bitbucket's git endpoint the way git sends it, and a
// real git, which takes an authtype from 2.46, lists the repository through the
// helper.
//
// Not parallel: the login and the helper read the configuration file
// BB_CONFIG_PATH names, which is one per process.
func TestLiveGitCredentialHelperSendsWhatBitbucketTakes(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	host, err := url.Parse(harness.config.BitbucketURL)
	if err != nil {
		t.Fatalf("parse bitbucket url: %v", err)
	}
	repoURL := strings.TrimRight(harness.config.BitbucketURL, "/") + "/scm/" + seeded.Key + "/" + repo.Slug + ".git"

	// A person whose login name has a + and an @ in it, so the slug differs
	// from it and Bitbucket reports it percent-encoded.
	owner := createLicensedUserNamed(ctx, t, harness, "LT.Git+"+uniqueSuffix()+"@example.local")
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, owner.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant the token owner read access failed: %v", err)
	}
	ownerSlug := userSlug(ctx, t, harness, owner.Username)
	if ownerSlug == owner.Username {
		t.Fatalf("the slug %q is the login name, so sending the slug would pass for the name", ownerSlug)
	}
	personal, err := harness.liveJSONAs(ctx, owner, http.MethodPut, "/rest/access-tokens/latest/users/"+url.PathEscape(ownerSlug),
		map[string]any{"name": testsupport.UniqueName("lt-git-personal-"), "permissions": []string{"REPO_READ"}, "expiryDays": 1})
	if err != nil {
		t.Fatalf("create the personal token failed: %v", err)
	}

	repository, err := harness.liveJSON(ctx, http.MethodPut, "/rest/access-tokens/latest/projects/"+seeded.Key+"/repos/"+repo.Slug,
		map[string]any{"name": testsupport.UniqueName("lt-git-repository-"), "permissions": []string{"REPO_READ"}, "expiryDays": 1})
	if err != nil {
		t.Fatalf("create the repository token failed: %v", err)
	}

	helperBinary, err := liveCLIBinaryPath(t)
	if err != nil {
		t.Fatalf("build bb: %v", err)
	}

	for _, testCase := range []struct {
		name string
		// token is the secret Bitbucket answered the create with.
		token string
		// basicUsername is the name the token goes beside over Basic auth.
		basicUsername string
		// storedOwner is the name the login stores, if any.
		storedOwner string
	}{
		{"personal token", stringField(personal, "token"), owner.Username, owner.Username},
		{"repository token", stringField(repository, "token"), "x-token-auth", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.token == "" {
				t.Fatal("Bitbucket answered the token create with no token")
			}

			configPath := filepath.Join(t.TempDir(), "bb-config.yaml")
			t.Setenv("BB_CONFIG_PATH", configPath)
			t.Setenv("BB_DISABLE_STORED_CONFIG", "")

			if output, err := executeLiveCLIWithStdin(t, testCase.token,
				"auth", "login", harness.config.BitbucketURL, "--token-stdin", "--discover-aliases=false", "--allow-insecure-storage"); err != nil {
				t.Fatalf("auth login failed: %v\noutput: %s", err, output)
			}

			// What the login stored, read back from the file.
			stored, err := config.LoadStoredConfig()
			if err != nil {
				t.Fatalf("read the stored configuration: %v", err)
			}
			profile := stored.Hosts[stored.DefaultHost]
			if profile.AuthMode != "token" || profile.Username != testCase.storedOwner {
				t.Fatalf("the login stored auth mode %q and username %q, want token and %q", profile.AuthMode, profile.Username, testCase.storedOwner)
			}

			// Older git: a username and password, which Bitbucket must take.
			answer := askGitCredential(t, "protocol="+host.Scheme+"\nhost="+host.Host+"\n\n")
			if answer["username"] != testCase.basicUsername || answer["password"] != testCase.token {
				t.Fatalf("without authtype the helper answered username %q, want %q beside the token", answer["username"], testCase.basicUsername)
			}
			requireGitEndpointTakes(ctx, t, repoURL, func(request *http.Request) {
				request.SetBasicAuth(answer["username"], answer["password"])
			})

			// git 2.46 and later: the token as Bearer.
			answer = askGitCredential(t, "capability[]=authtype\ncapability[]=state\nprotocol="+host.Scheme+"\nhost="+host.Host+"\n\n")
			if answer["capability[]"] != "authtype" || answer["authtype"] != "Bearer" || answer["credential"] != testCase.token {
				t.Fatalf("with authtype the helper answered %v, want the token as Bearer", redactedAnswer(answer, testCase.token))
			}
			requireGitEndpointTakes(ctx, t, repoURL, func(request *http.Request) {
				request.Header.Set("Authorization", answer["authtype"]+" "+answer["credential"])
			})

			// A real git, through the helper line bb auth setup-git writes.
			helperKey := "credential." + host.Scheme + "://" + host.Host + ".helper"
			helperValue := "!\"" + helperBinary + "\" auth git-credential --config \"" + configPath + "\""
			output, err := runGitCapture(t.TempDir(), "-c", helperKey+"=", "-c", helperKey+"="+helperValue, "ls-remote", repoURL, "refs/heads/master")
			if err != nil {
				t.Fatalf("git ls-remote through the helper failed: %v", err)
			}
			if !strings.HasSuffix(strings.TrimSpace(output), "\trefs/heads/master") {
				t.Fatalf("git ls-remote through the helper listed no master branch:\n%s", output)
			}
		})
	}
}

// createLicensedUserNamed is createLicensedUser with a login name of the
// test's choosing.
func createLicensedUserNamed(ctx context.Context, t *testing.T, harness *liveHarness, name string) restrictedUser {
	t.Helper()

	if err := harness.takeLicenceSeat(ctx); err != nil {
		t.Fatalf("take a licence seat: %v", err)
	}

	password := "Ltp@ss" + uniqueSuffix() + "!"
	addToDefaultGroup := false
	response, err := harness.client.CreateUser(ctx, &openapigenerated.CreateUserParams{
		Name:              name,
		Password:          &password,
		DisplayName:       "Live Test " + name,
		EmailAddress:      "lt" + uniqueSuffix() + "@example.local",
		AddToDefaultGroup: &addToDefaultGroup,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("create user %s returned status %d", name, response.StatusCode)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = harness.deleteRestrictedUser(cleanupCtx, name)
	})

	if err := harness.licenseUser(ctx, name); err != nil {
		t.Fatalf("license user %s: %v", name, err)
	}

	return restrictedUser{Username: name, Password: password}
}

// userSlug is the slug Bitbucket gave the user with this login name.
func userSlug(ctx context.Context, t *testing.T, harness *liveHarness, name string) string {
	t.Helper()

	found, err := harness.liveJSON(ctx, http.MethodGet, "/rest/api/latest/users?filter="+url.QueryEscape(name), nil)
	if err != nil {
		t.Fatalf("look up user %s: %v", name, err)
	}
	values, _ := found["values"].([]any)
	for _, value := range values {
		if user, _ := value.(map[string]any); user["name"] == name {
			if slug, _ := user["slug"].(string); slug != "" {
				return slug
			}
		}
	}
	t.Fatalf("no user named %s in %v", name, found)

	return ""
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

// askGitCredential runs `bb auth git-credential get` on a request as git
// writes it, and reads the answer as git does.
func askGitCredential(t *testing.T, request string) map[string]string {
	t.Helper()

	output, err := executeLiveCLIWithStdin(t, request, "auth", "git-credential", "get")
	if err != nil {
		t.Fatalf("git-credential get failed: %v\noutput: %s", err, output)
	}

	answer := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		if key, value, found := strings.Cut(scanner.Text(), "="); found {
			answer[key] = value
		}
	}

	return answer
}

// redactedAnswer is an answer fit for a failure message.
func redactedAnswer(answer map[string]string, token string) map[string]string {
	redacted := map[string]string{}
	for key, value := range answer {
		redacted[key] = strings.ReplaceAll(value, token, "<token>")
	}

	return redacted
}

// requireGitEndpointTakes sends the first request of a clone or fetch,
// authorized as given, and fails unless Bitbucket answers it.
func requireGitEndpointTakes(ctx context.Context, t *testing.T, repoURL string, authorize func(*http.Request)) {
	t.Helper()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, repoURL+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	authorize(request)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("ask Bitbucket's git endpoint: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Bitbucket's git endpoint answered %d to the helper's credential", response.StatusCode)
	}
}
