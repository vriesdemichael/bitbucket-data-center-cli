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

// The non-strict resolver falls back to the default host on purpose: it answers
// "which server should bb talk to", so `bb pr list` works without --host.
func TestResolveStoredCredentialsFallsBackToDefaultHost(t *testing.T) {
	t.Parallel()

	_, ok := resolveStoredCredentials(storedFixture(), "https://unconfigured.example.org")
	if !ok {
		t.Fatal("expected the default-host fallback to resolve for bb's own commands")
	}
}

// The strict resolver must not. This is the regression guard for a real defect:
// the git credential helper originally used the non-strict lookup, so when git
// asked it for github.com credentials it returned the Bitbucket token — handing
// the credential to an unrelated host, which is the exact failure the helper
// exists to prevent.
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
