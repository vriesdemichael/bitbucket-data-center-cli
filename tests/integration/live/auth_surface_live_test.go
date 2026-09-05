//go:build live

package live_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveAuthIdentityAndTokenURL covers the two read-only auth commands that
// talk to the server.
//
// auth identity is the one that matters: bb auth status now depends on it to
// decide whether the configured credential still works, so a change that broke
// identity resolution would silently turn the status check into a check of
// nothing.
func TestLiveAuthIdentityAndTokenURL(t *testing.T) {
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
	tokenURLOutput, err := executeLiveCLI(t, "auth", "token-url", "--host", "http://localhost:7990")
	if err != nil {
		t.Fatalf("auth token-url failed: %v\noutput: %s", err, tokenURLOutput)
	}
	if !strings.Contains(tokenURLOutput, "localhost:7990") {
		t.Fatalf("expected the configured host in the token url, got: %s", tokenURLOutput)
	}
}

// TestLiveAuthAliasLifecycle covers auth alias add, list, remove and discover.
//
// Aliases live in the stored configuration, so this drives a temporary config
// file rather than the developer's own — the same isolation the stored-config
// flow test uses.
func TestLiveAuthAliasLifecycle(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb-config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	const host = "http://localhost:7990"
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

	if output, err := executeLiveCLI(t, "auth", "alias", "remove", alias, "--host", host); err != nil {
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
	_ = ctx

	configureLiveCLIEnv(t, harness, "", "")

	name := fmt.Sprintf("live-token-%d", time.Now().UnixNano()%100000)
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
		_, _ = executeLiveCLI(t, "auth", "token", "revoke", tokenID, "--user", "admin")
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
			t.Skipf("the create response carried no secret to authenticate with:\n%s", createOutput)
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
	})

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

	if _, err := executeLiveCLI(t, "--json", "auth", "token", "revoke", tokenID, "--user", "admin"); err != nil {
		t.Fatalf("auth token revoke failed: %v", err)
	}
}
