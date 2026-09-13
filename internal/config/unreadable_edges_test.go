package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The edges of the #567 and #587 review fixes that need no server: a config
// location that cannot be named, a keyring key with nowhere to scope it to,
// and a write that cannot complete.

func TestAnUnreadableConfigWhoseLocationCannotBeNamedStillSaysWhatToDo(t *testing.T) {
	t.Parallel()

	unnamed := func() (string, error) { return "", errors.New("%AppData% is not defined") }
	err := unreadableConfig(unnamed, "stored configuration", errors.New("yaml: line 1"))

	message := err.Error()
	if !strings.Contains(message, "the stored configuration could not be read") ||
		!strings.Contains(message, "Fix or remove that file") {
		t.Fatalf("the message without a path does not say what failed and what to do: %v", err)
	}
}

// With no config location there is no file to scope the key to, and a
// credential is filed under the host alone, as bb always has.
func TestAKeyringKeyWithNoConfigLocationIsTheHost(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", "")
	t.Setenv("APPDATA", "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, err := ConfigPath(); err == nil {
		t.Skip("this platform still resolves a config directory with the variables unset")
	}

	if key := credentialKey("https://bitbucket.example"); key != hostKey("https://bitbucket.example") {
		t.Fatalf("credentialKey = %q, want the host key", key)
	}
}

func TestABlankLegacyKeyIsSkipped(t *testing.T) {
	store := withWorkingKeyring(t)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))

	// Hand-edited configs can file a profile under an empty key; that entry is
	// not a key to read a credential from.
	store[keyringServiceName+"/:token"] = "should-not-be-read"

	if token, password := storedSecrets("https://nothing-stored.example", "", "  "); token != "" || password != "" {
		t.Fatalf("a blank legacy key answered: token %q, password %q", token, password)
	}
}

func TestUpdateDoesNotRunPastADamagedStoredOrSystemConfig(t *testing.T) {
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
	t.Setenv("BB_UPDATE_BASE_URL", "")
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	t.Run("stored", func(t *testing.T) {
		path := writeMalformed(t, "stored.yaml")
		t.Setenv("BB_CONFIG_PATH", path)

		if _, err := ResolveUpdateBaseURL(""); err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("a damaged stored config was skipped: %v", err)
		}
	})

	t.Run("system", func(t *testing.T) {
		t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
		path := writeMalformed(t, "system.yaml")
		t.Setenv("BB_SYSTEM_CONFIG_PATH", path)

		if _, err := ResolveUpdateBaseURL(""); err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("a damaged system config was skipped: %v", err)
		}
	})
}

func TestAWriteThatCannotCompleteLeavesTheFileAsItWas(t *testing.T) {
	t.Parallel()

	t.Run("nowhere to write beside it", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "no-such-directory", "config.yaml")
		if err := writeFileAtomically(missing, []byte("hosts: {}\n")); err == nil {
			t.Fatal("a write into a directory that does not exist succeeded")
		}
	})

	t.Run("the target cannot be replaced", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		// A directory where the file should be: the rename over it fails.
		target := filepath.Join(directory, "config.yaml")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := writeFileAtomically(target, []byte("hosts: {}\n")); err == nil {
			t.Fatal("a rename over a directory succeeded")
		}

		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("read dir: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("the failed write left its temporary file behind: %d entries", len(entries))
		}
	})
}
