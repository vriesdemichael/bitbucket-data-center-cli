package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAWorkspaceProfileReleasesNoStoredCredential is a cloned repository's
// .bb/config.yaml choosing which of the user's credentials bb presents, and to
// whom.
//
// The user logged in to their own Bitbucket, so the keyring holds a token for
// it. A workspace file arrives with a clone. It does not choose the host
// (#770), but an agent the clone's content steers can, with --host, and a
// command then talks to that host with no stored credential, which
// TestACredentialDoesNotFollowAHostTheRepositoryChose holds. The file may also
// declare host profiles, and a profile decided which keyring entry was read:
// one keyed to the author's host with its url pointing at the user's
// Bitbucket, or one for the user's Bitbucket claiming the author's host as an
// alias, was handed the token stored for the user's Bitbucket, and bb sent it
// to the author's host. A profile could likewise name the user's client
// certificate for the author's host.
func TestAWorkspaceProfileReleasesNoStoredCredential(t *testing.T) {
	const (
		trusted     = "https://trusted.example.com"
		attacker    = "https://attacker.example.net"
		storedToken = "token-for-the-users-own-bitbucket"
	)

	certificateDirectory := t.TempDir()
	clientCert := filepath.Join(certificateDirectory, "client.pem")
	clientKey := filepath.Join(certificateDirectory, "client-key.pem")
	for _, path := range []string{clientCert, clientKey} {
		if err := os.WriteFile(path, []byte("not read before the request\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	for name, testCase := range map[string]struct {
		// host is the author's host, given with --host by an agent the clone's
		// content steers.
		host      string
		workspace []string
	}{
		"a profile keyed to another host whose url is the user's": {
			host: attacker,
			workspace: []string{
				"hosts:",
				"  " + attacker + ":",
				"    url: " + trusted,
			},
		},
		"a profile for the user's host claiming another host as an alias": {
			host: attacker,
			workspace: []string{
				"hosts:",
				"  " + trusted + ":",
				"    url: " + trusted,
				"    aliases:",
				"      - attacker.example.net:443",
			},
		},
		"a profile naming the user's client certificate for another host": {
			host: attacker,
			workspace: []string{
				"hosts:",
				"  " + attacker + ":",
				"    url: " + attacker,
				"    client_cert: " + filepath.ToSlash(clientCert),
				"    client_key: " + filepath.ToSlash(clientKey),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
			t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
			if _, err := SaveLogin(LoginInput{Host: trusted, Token: storedToken, SetDefault: true}); err != nil {
				t.Fatalf("log in to the user's own Bitbucket: %v", err)
			}

			workspacePath := filepath.Join(t.TempDir(), "workspace.yaml")
			if err := os.WriteFile(workspacePath, []byte(strings.Join(testCase.workspace, "\n")+"\n"), 0o600); err != nil {
				t.Fatalf("write workspace config: %v", err)
			}
			t.Setenv("BB_WORKSPACE_CONFIG_PATH", workspacePath)
			// Away from any .env above the package: the loader reads the nearest.
			t.Chdir(t.TempDir())

			passed := Overrides{Host: testCase.host}
			cfg, err := LoadWithOverrides(passed)
			if err != nil {
				t.Fatalf("LoadWithOverrides: %v", err)
			}

			// Without this the assertions below pass for a run that never
			// talked to the author's host at all.
			if cfg.BitbucketURL != attacker {
				t.Fatalf("the author's host is not the host bb talks to: %s", cfg.BitbucketURL)
			}
			if cfg.BitbucketToken != "" {
				t.Errorf("a workspace profile released a stored token to %s: %q", cfg.BitbucketURL, cfg.BitbucketToken)
			}
			if cfg.ClientCertFile != "" || cfg.ClientKeyFile != "" {
				t.Errorf("a workspace profile chose the client certificate bb presents to %s: %q, %q",
					cfg.BitbucketURL, cfg.ClientCertFile, cfg.ClientKeyFile)
			}

			// bb doctor reports the credential a command would send; it must not
			// report one the loader no longer releases (ADR-086).
			assertDiagnosisAgreesWithTheLoader(t, passed)
		})
	}
}

// TestAWorkspaceProfileStillNamesItsUsername keeps what a workspace host profile
// is for: describing a host the repository works with. A username is not a
// credential; bb sends it only beside a password the caller supplied.
func TestAWorkspaceProfileStillNamesItsUsername(t *testing.T) {
	const host = "https://team.example.com"

	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")

	workspacePath := filepath.Join(t.TempDir(), "workspace.yaml")
	workspace := strings.Join([]string{
		"hosts:",
		"  " + host + ":",
		"    url: " + host,
		"    username: alice",
		"",
	}, "\n")
	if err := os.WriteFile(workspacePath, []byte(workspace), 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", workspacePath)
	t.Chdir(t.TempDir())

	// The host comes from the command line: a workspace file does not choose
	// it (#770), and its profile still describes it.
	cfg, err := LoadWithOverrides(Overrides{Host: host, Password: "supplied-by-the-caller"})
	if err != nil {
		t.Fatalf("LoadWithOverrides: %v", err)
	}

	if cfg.BitbucketURL != host || cfg.BitbucketUsername != "alice" {
		t.Errorf("the workspace profile no longer names its host's username: %s as %q", cfg.BitbucketURL, cfg.BitbucketUsername)
	}
}
