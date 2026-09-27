package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// writeRestrictionPolicy points bb at a system configuration holding body.
func writeRestrictionPolicy(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", path)

	return path
}

// The levers read from the system configuration as top-level keys and inside
// the policy block, and are off when nothing sets them.
func TestRestrictionsReadFromTheSystemConfiguration(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want Restrictions
	}{
		"nothing set":      {"default_host: https://bb.example.com\n", Restrictions{}},
		"top-level keys":   {"disable_bb: true\ndisable_mcp_server: true\nread_only: true\n", Restrictions{DisableBB: true, DisableMCPServer: true, ReadOnly: true}},
		"the policy block": {"policy:\n  disable_bb: true\n  disable_mcp_server: true\n  read_only: true\n", Restrictions{DisableBB: true, DisableMCPServer: true, ReadOnly: true}},
		"set false":        {"policy:\n  disable_bb: false\n", Restrictions{}},
	} {
		t.Run(name, func(t *testing.T) {
			writeRestrictionPolicy(t, tc.body)
			got, err := LoadRestrictions()
			if err != nil {
				t.Fatalf("LoadRestrictions: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A system configuration that cannot be read is an error, never levers that
// are all off.
func TestRestrictionsFromAnUnreadablePolicyAreAnError(t *testing.T) {
	path := writeRestrictionPolicy(t, "policy:\n  read_only: [\n")

	if _, err := LoadRestrictions(); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("got %v, want an error naming %s", err, path)
	}
}

// Each refusal is an authorization error that names its lever and the file it
// was set in, so the person refused knows whom to ask and what to ask for.
func TestRestrictionRefusalsNameTheirLever(t *testing.T) {
	path := writeRestrictionPolicy(t, "disable_bb: true\ndisable_mcp_server: true\nread_only: true\n")

	for lever, err := range map[string]error{
		"disable_bb":         DisabledError(),
		"disable_mcp_server": MCPServerDisabledError(),
		"read_only":          ReadOnlyError("bb pr merge"),
	} {
		if !apperrors.IsKind(err, apperrors.KindAuthorization) || !strings.Contains(err.Error(), lever+" in the system configuration file "+path) {
			t.Errorf("%s: got %v", lever, err)
		}
	}
}
