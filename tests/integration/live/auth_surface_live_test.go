//go:build live

package live_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveAuthIdentityAndTokenURL covers the two read-only auth commands that
// talk to the server.
//
// auth identity is the one that matters: bb auth status now depends on it to
// decide whether the configured credential still works, so a change that broke
// identity resolution would silently turn the status check into a check of
// nothing.
func TestLiveAuthIdentityAndTokenURL(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	_ = harness

	identityOutput, err := executeLiveCLI(t, "--json", "auth", "identity")
	if err != nil {
		t.Fatalf("auth identity failed: %v\noutput: %s", err, identityOutput)
	}
	if !strings.Contains(identityOutput, "admin") {
		t.Fatalf("expected the authenticated user in the identity payload, got: %s", identityOutput)
	}

	// token-url is computed from the host rather than fetched, so the guarantee
	// is that it points at the right place for the configured server.
	configuredHost := strings.TrimRight(harness.config.BitbucketURL, "/")
	tokenURLOutput, err := executeLiveCLI(t, "auth", "token-url", "--host", configuredHost)
	if err != nil {
		t.Fatalf("auth token-url failed: %v\noutput: %s", err, tokenURLOutput)
	}
	if !strings.Contains(tokenURLOutput, strings.TrimPrefix(strings.TrimPrefix(configuredHost, "https://"), "http://")) {
		t.Fatalf("expected the configured host in the token url, got: %s", tokenURLOutput)
	}

	// Without --host the command has to find out who is asking, and the slug it
	// puts in the path is the server's, not the name in our config. A unit test
	// asserted this against a users endpoint it wrote itself, which agreed with
	// the CLI by construction about both the field and the path.
	ownTokenURL, err := executeLiveCLI(t, "auth", "token-url")
	if err != nil {
		t.Fatalf("auth token-url without a host failed: %v\noutput: %s", err, ownTokenURL)
	}
	if !strings.Contains(ownTokenURL, "/plugins/servlet/access-tokens/users/admin/manage") {
		t.Fatalf("expected the per-user manage path for the authenticated user, got: %s", ownTokenURL)
	}
}

// TestLiveAuthAliasLifecycle covers auth alias add, list, remove and discover.
//
// Aliases live in the stored configuration, so this drives a temporary config
// file rather than the developer's own — the same isolation the stored-config
// flow test uses.
func TestLiveAuthAliasLifecycle(t *testing.T) {
	// Read before the environment is cleared below.
	host := liveInstanceURL()

	configPath := filepath.Join(t.TempDir(), "bb-config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	if output, err := executeLiveCLIWithStdin(t, "admin", "auth", "login", host, "--username", "admin", "--password-stdin", "--set-default"); err != nil {
		t.Fatalf("auth login failed: %v\noutput: %s", err, output)
	}

	const alias = "git.live-suite.example:7999"
	if output, err := executeLiveCLI(t, "auth", "alias", "add", alias, "--host", host); err != nil {
		t.Fatalf("auth alias add failed: %v\noutput: %s", err, output)
	}

	listOutput, err := executeLiveCLI(t, "--json", "auth", "alias", "list", "--host", host)
	if err != nil {
		t.Fatalf("auth alias list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, "live-suite.example") {
		t.Fatalf("expected the added alias to be listed, got: %s", listOutput)
	}

	// discover runs between add and remove, which is the natural order and was
	// not possible before: it replaced the stored list, so it deleted the alias
	// added a moment earlier and the test had to run discover last to avoid it.
	if output, err := executeLiveCLI(t, "auth", "alias", "discover", "--host", host); err != nil {
		t.Fatalf("auth alias discover failed: %v\noutput: %s", err, output)
	}

	afterDiscover, err := executeLiveCLI(t, "--json", "auth", "alias", "list", "--host", host)
	if err != nil {
		t.Fatalf("auth alias list after discover failed: %v\noutput: %s", err, afterDiscover)
	}
	if !strings.Contains(afterDiscover, "live-suite.example") {
		t.Fatalf("expected discovery to keep the manually added alias, got: %s", afterDiscover)
	}

	// --replace is the explicit way to drop what discovery did not find, and it
	// names what it took away.
	replaceOutput, err := executeLiveCLI(t, "auth", "alias", "discover", "--host", host, "--replace")
	if err != nil {
		t.Fatalf("auth alias discover --replace failed: %v\noutput: %s", err, replaceOutput)
	}
	if !strings.Contains(replaceOutput, "live-suite.example") {
		t.Fatalf("expected --replace to report the alias it removed, got: %s", replaceOutput)
	}

	afterReplace, err := executeLiveCLI(t, "--json", "auth", "alias", "list", "--host", host)
	if err != nil {
		t.Fatalf("auth alias list after replace failed: %v\noutput: %s", err, afterReplace)
	}
	if strings.Contains(afterReplace, "live-suite.example") {
		t.Fatalf("expected --replace to drop the manual alias, got: %s", afterReplace)
	}

	// Put it back so remove has something of its own to take away.
	if output, err := executeLiveCLI(t, "auth", "alias", "add", alias, "--host", host); err != nil {
		t.Fatalf("auth alias add before remove failed: %v\noutput: %s", err, output)
	}

	if output, err := executeLiveCLI(t, "auth", "alias", "remove", alias, "--host", host, "--yes"); err != nil {
		t.Fatalf("auth alias remove failed: %v\noutput: %s", err, output)
	}

	afterRemove, err := executeLiveCLI(t, "--json", "auth", "alias", "list", "--host", host)
	if err != nil {
		t.Fatalf("auth alias list after remove failed: %v\noutput: %s", err, afterRemove)
	}
	if strings.Contains(afterRemove, "live-suite.example") {
		t.Fatalf("expected the alias to be gone, got: %s", afterRemove)
	}

	if output, err := executeLiveCLI(t, "auth", "logout", "--host", host); err != nil {
		t.Fatalf("auth logout failed: %v\noutput: %s", err, output)
	}
}

// TestLiveAuthTokenLifecycle covers auth token create, list, get, update and
// revoke — the personal access token surface.
//
// Every one of these mutates real credentials on the server, which is exactly
// the category where a misunderstood endpoint is worth catching before a user
// finds it.
func TestLiveAuthTokenLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	configureLiveCLIEnv(t, harness, "", "")

	// Two projects of the test's own, so the listing the token is tried on below
	// holds more than the one it is capped at.
	for range 2 {
		if _, err := harness.seedRepo(ctx, repoSeed{}); err != nil {
			t.Fatalf("seed a project failed: %v", err)
		}
	}

	name := testsupport.UniqueName("live-token-")
	createOutput, err := executeLiveCLI(t, "--json", "auth", "token", "create", name,
		"--user", "admin", "--permission", "REPO_READ", "--expiry-days", "1")
	if err != nil {
		t.Fatalf("auth token create failed: %v\noutput: %s", err, createOutput)
	}

	created := decodeJSONMap(t, createOutput)
	tokenID, ok := numericOrStringID(created["id"])
	if !ok {
		t.Fatalf("expected a token id in the create output: %s", createOutput)
	}
	defer func() {
		_, _ = executeLiveCLI(t, "auth", "token", "revoke", tokenID, "--user", "admin", "--yes")
	}()

	// The token is worth having only if Bitbucket accepts it the way bb sends
	// it, which is Authorization: Bearer.
	//
	// A unit test asserted that header against a mock that recorded it, which
	// says the client wrote the string it was told to write. Whether Bitbucket
	// honours that form -- rather than requiring the token as basic-auth
	// password, which some Atlassian products do -- is the server's answer, and
	// it is the difference between a CLI that works with a token and one that
	// only ever worked because the suite used a password.
	t.Run("bitbucket accepts the token as a bearer credential", func(t *testing.T) {
		secret, _ := created["token"].(string)
		if secret == "" {
			// A token nobody can read is not a token. The secret is returned
			// once, at creation, so a create that omits it has failed at the
			// one thing it exists to do -- and skipping here meant the bearer
			// assertion below never ran and nothing said so.
			t.Fatalf("auth token create returned no secret, so the token it made is unusable:\n%s", createOutput)
		}

		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", harness.config.BitbucketURL)
		t.Setenv("BITBUCKET_TOKEN", secret)
		t.Setenv("BITBUCKET_USERNAME", "")
		t.Setenv("BITBUCKET_PASSWORD", "")
		t.Setenv("ADMIN_USER", "")
		t.Setenv("ADMIN_PASSWORD", "")

		output, err := executeLiveCLI(t, "--json", "project", "list", "--limit", "1")
		if err != nil {
			t.Fatalf("a bearer token bb just created was refused: %v\noutput: %s", err, output)
		}
		var listed struct {
			Projects []map[string]any `json:"projects"`
		}
		decodeJSONData(t, output, &listed)
		if len(listed.Projects) != 1 {
			t.Errorf("project list --limit 1 answered %d projects: %s", len(listed.Projects), output)
		}
	})

	// --limit 50 only bounds the lookup: a cap of 50 shows only past 50 tokens,
	// which this test does not make.
	listOutput, err := executeLiveCLI(t, "--json", "auth", "token", "list", "--user", "admin", "--limit", "50")
	if err != nil {
		t.Fatalf("auth token list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, name) {
		t.Fatalf("expected the created token in the listing, got: %s", listOutput)
	}

	getOutput, err := executeLiveCLI(t, "--json", "auth", "token", "get", tokenID, "--user", "admin")
	if err != nil {
		t.Fatalf("auth token get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, name) {
		t.Fatalf("expected the token name in get output, got: %s", getOutput)
	}
	if got := decodeJSONMap(t, getOutput)["name"]; got != name {
		t.Errorf("name = %v, want %s", got, name)
	}

	// bb auth token get publishes neither the permissions nor the expiry, so
	// those are read from Bitbucket through bb api.
	stored := decodeJSONMap(t, mustLiveCLI(t, "api", "/rest/access-tokens/latest/users/admin/"+tokenID))
	if permissions, _ := stored["permissions"].([]any); len(permissions) != 1 || permissions[0] != "REPO_READ" {
		t.Errorf("permissions = %v, want [REPO_READ]", stored["permissions"])
	}
	// A token created without an expiry has no expiryDate at all, so one a day
	// after its creation is --expiry-days 1 arriving.
	createdDate, _ := stored["createdDate"].(float64)
	expiryDate, expires := stored["expiryDate"].(float64)
	if !expires || expiryDate-createdDate != float64((24*time.Hour).Milliseconds()) {
		t.Errorf("expiryDate = %v for createdDate %v, want one day later", stored["expiryDate"], stored["createdDate"])
	}

	renamed := name + "-renamed"
	if _, err := executeLiveCLI(t, "--json", "auth", "token", "update", tokenID, "--name", renamed, "--user", "admin"); err != nil {
		t.Fatalf("auth token update failed: %v", err)
	}

	afterUpdate, err := executeLiveCLI(t, "--json", "auth", "token", "get", tokenID, "--user", "admin")
	if err != nil {
		t.Fatalf("auth token get after update failed: %v\noutput: %s", err, afterUpdate)
	}
	if !strings.Contains(afterUpdate, renamed) {
		t.Fatalf("expected the rename to persist, got: %s", afterUpdate)
	}
	if got := decodeJSONMap(t, afterUpdate)["name"]; got != renamed {
		t.Errorf("name after update = %v, want %s", got, renamed)
	}

	if _, err := executeLiveCLI(t, "--json", "auth", "token", "revoke", tokenID, "--user", "admin", "--yes"); err != nil {
		t.Fatalf("auth token revoke failed: %v", err)
	}
	if output, err := executeLiveCLI(t, "--json", "auth", "token", "get", tokenID, "--user", "admin"); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("a revoked token is still found: %v\noutput: %s", err, output)
	}
}
