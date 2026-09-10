package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// malformed is YAML no parser will accept: a tab cannot start a token.
const malformed = "default_host: corp-a\nhosts:\n  corp-a: {url: https://a.example}\n  corp-b: {url: https://b.example}\n\tbad-indent: {url: https://oops}\n"

func writeMalformed(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(malformed), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	return path
}

// TestADamagedConfigIsReportedRatherThanTreatedAsEmpty is #567.
//
// Three loads discarded their error, so a file bb could not parse resolved as
// a file that was not there: the user was told they were not logged in, and
// the remedy that message prescribed rewrote the config and deleted every
// other host in it.
func TestADamagedConfigIsReportedRatherThanTreatedAsEmpty(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		variable string
		names    string
	}{
		{name: "stored", variable: "BB_CONFIG_PATH", names: "stored configuration"},
		{name: "system", variable: "BB_SYSTEM_CONFIG_PATH", names: "system configuration"},
		{name: "workspace", variable: "BB_WORKSPACE_CONFIG_PATH", names: "workspace configuration"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeMalformed(t, testCase.name+".yaml")
			t.Setenv(testCase.variable, path)
			t.Setenv("BB_DISABLE_STORED_CONFIG", "")
			t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
			t.Setenv("BITBUCKET_TOKEN", "t")

			_, err := LoadFromEnv()
			if err == nil {
				t.Fatal("a config that cannot be parsed was accepted")
			}

			message := err.Error()
			if !strings.Contains(message, testCase.names) {
				t.Errorf("the error does not say which file: %v", message)
			}
			if !strings.Contains(message, path) {
				t.Errorf("the error does not name the path %s: %v", path, message)
			}
			// The sentence that turned a damaged config into a destroyed one.
			if strings.Contains(message, "auth login") {
				t.Errorf("the error still prescribes the remedy that deletes the file: %v", message)
			}
		})
	}
}

// The system file carries the administrator's policy. Ignoring a broken one
// silently means the controls do not apply and nobody is told, so a policy
// could be switched off by corrupting the file.
func TestABrokenSystemPolicyFailsClosed(t *testing.T) {
	t.Setenv("BB_SYSTEM_CONFIG_PATH", writeMalformed(t, "system.yaml"))
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
	t.Setenv("BITBUCKET_TOKEN", "t")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("a damaged system policy was ignored, so the policy did not apply and nothing said so")
	}
}

// TestSaveLoginRefusesToRewriteAConfigItCouldNotRead is the data loss itself.
func TestSaveLoginRefusesToRewriteAConfigItCouldNotRead(t *testing.T) {
	path := writeMalformed(t, "stored.yaml")
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	if _, err := SaveLogin(LoginInput{Host: "https://bitbucket.example", Token: "t"}); err == nil {
		t.Fatal("login rewrote a config it could not read")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("the config was modified:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// The common case has to keep working: a config that is simply absent is not
// an error, and the "run auth login" advice is right there and only there.
func TestAnAbsentConfigIsNotAnError(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "not-created.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
	t.Setenv("BITBUCKET_TOKEN", "t")

	if _, err := LoadFromEnv(); err != nil {
		t.Fatalf("an absent config must resolve, got: %v", err)
	}
}
