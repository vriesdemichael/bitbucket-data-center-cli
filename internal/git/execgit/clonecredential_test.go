package execgit

import (
	"slices"
	"strings"
	"testing"
)

// TestACloneCredentialStaysOutOfTheCommandLine is the disclosure a command line
// is: /proc/<pid>/cmdline is world-readable on Linux and `ps` prints it, so a
// token passed with -c was visible to every local account for the length of the
// clone. /proc/<pid>/environ is readable only by the process owner.
func TestACloneCredentialStaysOutOfTheCommandLine(t *testing.T) {
	t.Parallel()

	const header = "Authorization: Bearer S3cr3tT0k3nValue"

	t.Run("a git that reads its configuration from the environment", func(t *testing.T) {
		t.Parallel()

		args, env := cloneCredentialConfig("https://bitbucket.example.com/scm/p/r.git", header, true)

		if len(args) != 0 {
			t.Fatalf("the credential reached the command line: %v", args)
		}
		if !slices.Contains(env, "GIT_CONFIG_VALUE_0="+header) {
			t.Fatalf("the credential is not in the environment either: %v", env)
		}
		if !slices.Contains(env, "GIT_CONFIG_KEY_0=http.https://bitbucket.example.com/.extraHeader") {
			t.Errorf("the header is not scoped to the host being cloned from: %v", env)
		}
		if !slices.Contains(env, "GIT_CONFIG_COUNT=1") {
			t.Errorf("git is not told how many pairs to read: %v", env)
		}
	})

	t.Run("a git too old for that", func(t *testing.T) {
		t.Parallel()

		// Before 2.31 the variables are ignored without a word, and a clone
		// that needs the credential would fail as an authentication error.
		args, env := cloneCredentialConfig("https://bitbucket.example.com/scm/p/r.git", header, false)

		if len(env) != 0 {
			t.Fatalf("an old git was given configuration it cannot read: %v", env)
		}
		if len(args) != 2 || args[0] != "-c" || !strings.HasSuffix(args[1], header) {
			t.Fatalf("the fallback does not pass the header: %v", args)
		}
		if !strings.HasPrefix(args[1], "http.https://bitbucket.example.com/.extraHeader=") {
			t.Errorf("the fallback header is not scoped to the host: %v", args)
		}
	})

	t.Run("no credential, no configuration", func(t *testing.T) {
		t.Parallel()

		args, env := cloneCredentialConfig("https://bitbucket.example.com/scm/p/r.git", "", true)
		if len(args) != 0 || len(env) != 0 {
			t.Fatalf("a clone with no credential carries configuration: %v %v", args, env)
		}
	})
}

// TestTheInheritedConfigurationPairsAreReplaced is the environment being a list
// rather than a map: two definitions of one variable are not merged, and on
// Linux the first is the one getenv answers with.
func TestTheInheritedConfigurationPairsAreReplaced(t *testing.T) {
	t.Parallel()

	inherited := []string{
		"PATH=/usr/bin",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Bearer inherited",
		"HOME=/home/someone",
	}

	kept := withoutGitConfigPairs(inherited)

	for _, entry := range kept {
		if strings.HasPrefix(entry, "GIT_CONFIG_") {
			t.Errorf("an inherited configuration pair survived: %s", entry)
		}
	}
	for _, wanted := range []string{"PATH=/usr/bin", "HOME=/home/someone"} {
		if !slices.Contains(kept, wanted) {
			t.Errorf("%s was dropped with them", wanted)
		}
	}
}

func TestGitVersionAtLeast(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]bool{
		"git version 2.31.0":           true,
		"git version 2.43.0.windows.1": true,
		"git version 3.0.1":            true,
		"git version 2.30.2":           false,
		"git version 1.9.5":            false,
		"":                             false,
		"git version unknown":          false,
	} {
		if got := gitVersionAtLeast(version, 2, 31); got != want {
			t.Errorf("%q: got %v, want %v", version, got, want)
		}
	}
}
