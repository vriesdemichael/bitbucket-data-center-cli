package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestASealedTestCannotReachTheDevelopersConfiguration holds the seal to what
// it is for: a unit test that writes a login, as bb auth login and the token a
// clone asks for do, writes it somewhere of the test process's own.
//
// An empty BB_CONFIG_PATH is bb's default path, the developer's own file, and
// a test that reached config.SaveLogin through the seal wrote a host into it.
func TestASealedTestCannotReachTheDevelopersConfiguration(t *testing.T) {
	t.Parallel()

	sealed, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}

	userConfigDirectory, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("the operating system names no user configuration directory: %v", err)
	}
	developers := filepath.Join(userConfigDirectory, "bb", "config.yaml")

	if strings.EqualFold(filepath.Clean(sealed), filepath.Clean(developers)) {
		t.Fatalf("a sealed test resolves the developer's own configuration file: %s", sealed)
	}
	temporary, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		temporary = os.TempDir()
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(sealed))
	if err != nil {
		t.Fatalf("the sealed configuration directory does not exist: %v", err)
	}
	if !strings.HasPrefix(strings.ToLower(resolved), strings.ToLower(temporary)) {
		t.Errorf("the sealed configuration %s is not in the temporary directory %s", sealed, temporary)
	}
}
