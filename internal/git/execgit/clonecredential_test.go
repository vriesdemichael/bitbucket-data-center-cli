package execgit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
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

		args, env := credentialConfig("https://bitbucket.example.com/scm/p/r.git", header, true)

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
		args, env := credentialConfig("https://bitbucket.example.com/scm/p/r.git", header, false)

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

		args, env := credentialConfig("https://bitbucket.example.com/scm/p/r.git", "", true)
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

// TestAFetchCredentialStaysOutOfTheCommandLine is #730: the fetch bb pr
// checkout runs put the credential on the command line after the clone had
// stopped doing so.
//
// It reads what git itself recorded rather than what bb meant to pass. Trace2
// logs every git process's argv, and the configuration parameter named below
// with the value git ended up holding, so the one run shows both that the
// credential stayed off the command line and that git still received it.
func TestAFetchCredentialStaysOutOfTheCommandLine(t *testing.T) {
	const secret = "S3cr3tF3tchT0k3nValue"
	const configParam = "http.https://bitbucket.example.com/.extraheader"

	trace := filepath.Join(t.TempDir(), "trace2.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	t.Setenv("GIT_TRACE2_CONFIG_PARAMS", configParam)

	backend := New()
	source := newCommittedRepository(t, backend)
	target := newCommittedRepository(t, backend)
	if err := backend.AddRemote(context.Background(), target, git.Remote{Name: "src", URL: source}); err != nil {
		t.Fatalf("add remote failed: %v", err)
	}

	// The remote is a directory, so nothing is sent anywhere; the credential
	// is scoped to the Bitbucket it belongs to, which is all git needs to
	// hold it.
	if err := backend.Fetch(context.Background(), target, git.FetchOptions{
		Remote:      "src",
		Credentials: &git.Credentials{URL: "https://bitbucket.example.com/scm/PRJ/repo.git", Token: secret},
	}); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	events, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("git wrote no trace: %v", err)
	}

	var onCommandLine, received bool
	for _, line := range strings.Split(string(events), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// value is a string on def_param and a list on other events.
		var event struct {
			Event string          `json:"event"`
			Argv  []string        `json:"argv"`
			Param string          `json:"param"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("unreadable trace event %q: %v", line, err)
		}
		switch event.Event {
		case "start":
			if strings.Contains(strings.Join(event.Argv, " "), secret) {
				onCommandLine = true
			}
		case "def_param":
			var value string
			if event.Param == configParam && json.Unmarshal(event.Value, &value) == nil && value == "Authorization: Bearer "+secret {
				received = true
			}
		}
	}

	if !received {
		t.Fatalf("git never held the credential, so the fetch would have gone out unauthenticated:\n%s", events)
	}

	// A git before 2.31 does not read configuration from the environment, and
	// the command line is the only way left to hand it the credential.
	if backend.gitReadsConfigFromEnvironment(context.Background()) {
		if onCommandLine {
			t.Errorf("the credential was on git's command line, where any local account can read it")
		}
	} else if !onCommandLine {
		t.Errorf("a git too old to read configuration from the environment was not given the credential on its command line")
	}
}
