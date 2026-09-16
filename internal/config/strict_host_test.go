package config

import "testing"

// storedFixture builds a config with one configured Bitbucket host that is also
// the default, which is the ordinary single-server setup.
func storedFixture() StoredConfig {
	return StoredConfig{
		DefaultHost: hostKey("https://bitbucket.example.com"),
		Hosts: map[string]StoredProfile{
			hostKey("https://bitbucket.example.com"): {
				URL:      "https://bitbucket.example.com",
				AuthMode: "token",
				Aliases:  []string{"git.example.com"},
			},
		},
	}
}

// There is one resolver, and it does not fall back to the default host.
//
// A lookup that did existed for bb's own commands, and the host it answered for
// is chosen by whatever pointed bb at a server: a .env in any parent directory,
// a cloned repository's .bb/config.yaml, a URL given to `bb api`, or --host on
// an MCP server. Each of those took the token stored for the user's own
// Bitbucket and offered it elsewhere.
//
// This is also the regression guard for the defect that came first: the git
// credential helper originally used the non-strict lookup, so when git asked it
// for github.com credentials it returned the Bitbucket token.
func TestResolveStoredCredentialsStrictDoesNotFallBackToDefaultHost(t *testing.T) {
	t.Parallel()

	unrelated := []string{
		"https://github.com",
		"https://evil.example.org",
		"https://bitbucket.example.com.attacker.test",
	}

	for _, host := range unrelated {
		if _, ok := resolveStoredCredentialsStrict(storedFixture(), host); ok {
			t.Fatalf("strict lookup returned credentials for unrelated host %q", host)
		}
	}
}

func TestResolveStoredCredentialsStrictMatchesConfiguredHost(t *testing.T) {
	t.Parallel()

	if _, ok := resolveStoredCredentialsStrict(storedFixture(), "https://bitbucket.example.com"); !ok {
		t.Fatal("expected the configured host to resolve")
	}
}

// Aliases exist because many deployments serve the API and git traffic from
// different hostnames, so an alias is a genuine match rather than a fallback.
func TestResolveStoredCredentialsStrictMatchesConfiguredAlias(t *testing.T) {
	t.Parallel()

	if _, ok := resolveStoredCredentialsStrict(storedFixture(), "https://git.example.com"); !ok {
		t.Fatal("expected a configured alias to resolve")
	}
}

// Bitbucket is frequently reached over http in local and internal deployments
// while the token was stored against https, so the same host under the other
// scheme is also a genuine match.
func TestResolveStoredCredentialsStrictMatchesAlternateScheme(t *testing.T) {
	t.Parallel()

	if _, ok := resolveStoredCredentialsStrict(storedFixture(), "http://bitbucket.example.com"); !ok {
		t.Fatal("expected the same host under the alternate scheme to resolve")
	}
}

func TestResolveStoredCredentialsStrictWithNoHostsConfigured(t *testing.T) {
	t.Parallel()

	if _, ok := resolveStoredCredentialsStrict(StoredConfig{}, "https://bitbucket.example.com"); ok {
		t.Fatal("expected no credentials when nothing is configured")
	}
}
