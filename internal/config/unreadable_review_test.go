package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The #567 fix covered login and the main load. A review found the paths it
// did not reach: logout still wrote a damaged file back as empty, the CI
// variable meant to ignore the file read it anyway, and several commands
// reported the damage without naming the file.

func TestLogoutRefusesToRewriteAConfigItCouldNotRead(t *testing.T) {
	path := writeMalformed(t, "stored.yaml")
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	err = Logout("https://a.example")
	if err == nil {
		t.Fatal("logout rewrote a config it could not read")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the refusal does not name the file: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("the config was modified:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// BB_DISABLE_STORED_CONFIG is documented as ignoring the file entirely. A
// damaged file left on a shared runner is the case it exists for.
func TestDisablingTheStoredConfigIgnoresADamagedOne(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", writeMalformed(t, "stored.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
	t.Setenv("BITBUCKET_TOKEN", "from-the-environment")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("a disabled stored config was read anyway: %v", err)
	}
	if cfg.BitbucketToken != "from-the-environment" {
		t.Fatalf("resolved token %q, want the environment's", cfg.BitbucketToken)
	}
}

// With no %AppData% or $HOME there is no stored file to have misread, so a
// run configured entirely from the environment has to keep working.
func TestAConfigLocationThatCannotBeWorkedOutIsNotAnError(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", "")
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("APPDATA", "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
	t.Setenv("BITBUCKET_TOKEN", "t")

	if _, err := ConfigPath(); err == nil {
		t.Skip("this platform still resolves a config directory with the variables unset")
	}

	if _, err := LoadFromEnv(); err != nil {
		t.Fatalf("an unresolvable config location failed an environment-only run: %v", err)
	}
}

// Permanent, exit 1: nothing in the invocation is wrong. The system file is not
// the reader's to remove, so it names who can repair it.
func TestADamagedConfigIsPermanentAndSaysWhoseItIsToFix(t *testing.T) {
	t.Run("stored", func(t *testing.T) {
		path := writeMalformed(t, "stored.yaml")
		t.Setenv("BB_CONFIG_PATH", path)
		t.Setenv("BB_DISABLE_STORED_CONFIG", "")
		t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
		t.Setenv("BITBUCKET_TOKEN", "t")

		_, err := LoadFromEnv()
		if apperrors.ExitCode(err) != 1 || !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("got %v (exit %d), want permanent and exit 1", err, apperrors.ExitCode(err))
		}
		// The kind is said once. The parse error used to bring its own
		// "validation:" into the middle of the sentence.
		if strings.Contains(apperrors.MessageOf(err), "validation:") {
			t.Errorf("the message repeats a kind: %s", apperrors.MessageOf(err))
		}
		if !strings.Contains(err.Error(), "Fix or remove that file") {
			t.Errorf("the message does not say what to do: %v", err)
		}
	})

	t.Run("system", func(t *testing.T) {
		path := writeMalformed(t, "system.yaml")
		t.Setenv("BB_SYSTEM_CONFIG_PATH", path)
		t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
		t.Setenv("BITBUCKET_TOKEN", "t")

		_, err := LoadFromEnv()
		if err == nil {
			t.Fatal("a damaged system policy was ignored")
		}
		message := err.Error()
		if !strings.Contains(message, path) || !strings.Contains(message, "administrator") {
			t.Errorf("the message does not name the file and who can repair it: %v", err)
		}
		if strings.Contains(message, "remove that file") {
			t.Errorf("an unprivileged user was told to remove the administrator's policy: %v", err)
		}
	})
}

// The alias and server commands returned the bare parse error: what was wrong,
// and not where.
func TestTheCommandsThatEditTheConfigNameTheDamagedFile(t *testing.T) {
	path := writeMalformed(t, "stored.yaml")
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	_, contextsErr := ListServerContexts()
	_, defaultErr := SetDefaultHost("https://a.example")
	_, _, aliasErr := AddHostAliases("https://a.example", []string{"git.a.example"})

	for name, err := range map[string]error{
		"auth server list": contextsErr,
		"auth server use":  defaultErr,
		"auth alias add":   aliasErr,
	} {
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%s does not name the damaged file: %v", name, err)
		}
	}
}

// bb update skipped a damaged file and took the next file's base URL.
func TestUpdateDoesNotRunPastADamagedConfig(t *testing.T) {
	path := writeMalformed(t, "workspace.yaml")
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", path)
	t.Setenv("BB_UPDATE_BASE_URL", "")

	if _, err := ResolveUpdateBaseURL(""); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("a damaged workspace config was skipped: %v", err)
	}
}

// Saving replaces the file whole. A truncate-then-write left an empty file for a
// moment -- and forever after a crash -- which read as a valid empty config.
func TestSavingReplacesTheConfigWholeAndLeavesNothingBehind(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	for _, host := range []string{"https://first.example", "https://second.example"} {
		stored := StoredConfig{DefaultHost: host, Hosts: map[string]StoredProfile{hostKey(host): {URL: host}}}
		if err := SaveStoredConfig(stored); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	loaded, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.DefaultHost != "https://second.example" {
		t.Fatalf("the second save did not replace the first: %+v", loaded)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("saving left files behind: %v", names)
	}
}
