package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// writeLeverPolicy points bb at a system configuration holding the policy, and
// at a refused port, so a command the policy lets through fails there rather
// than at a real server.
func writeLeverPolicy(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", testsupport.RefusedURL)
	t.Setenv("BITBUCKET_TOKEN", "token")

	return path
}

// runBB runs bb with the arguments and returns what it printed and its error.
// It stamps a version, as main does, so --version exists.
func runBB(args ...string) (string, error) {
	command := NewRootCommand()
	command.Version = "0.0.0-test"
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)
	err := command.Execute()

	return output.String(), err
}

// refusedBy asserts a run was refused by the named lever: an authorization
// error that names the lever and the file it was set in.
func refusedBy(t *testing.T, args []string, err error, lever, file string) {
	t.Helper()

	if !apperrors.IsKind(err, apperrors.KindAuthorization) || !strings.Contains(err.Error(), lever+" in ") || !strings.Contains(err.Error(), file) {
		t.Errorf("bb %s: got %v, want an authorization error naming %s in %s", strings.Join(args, " "), err, lever, file)
	}
}

// notRefused asserts a run got past the levers. It may still fail, at the
// refused port or for want of a repository, but not because of policy.
func notRefused(t *testing.T, args []string, err error) {
	t.Helper()

	if err != nil && strings.Contains(err.Error(), "by administrative policy") {
		t.Errorf("bb %s was refused by policy: %v", strings.Join(args, " "), err)
	}
}

// Under disable_bb a copy of bb nobody may use refuses every command, and
// leaves what contacts nothing and changes nothing, which is how a person finds
// out why and how a package manager installs bb: help, --describe, --version,
// doctor and a completion script. A completion answers nothing, with the
// reason as Active Help.
func TestDisableBBRefusesEverythingButWhatSaysWhy(t *testing.T) {
	file := writeLeverPolicy(t, "policy:\n  disable_bb: true\n")

	for _, args := range [][]string{
		{"pr", "list", "--repo", "PROJ/app"},
		{"ai", "mcp", "tools"},
		{"auth", "status"},
		{"auth", "server", "list"},
	} {
		_, err := runBB(args...)
		refusedBy(t, args, err, "disable_bb", file)
	}

	for _, args := range [][]string{
		{"--version"},
		{"help"},
		{"pr", "--help"},
		{"pr", "list", "--describe"},
		{"pr", "--describe"},
		{"completion", "bash"},
	} {
		if _, err := runBB(args...); err != nil {
			t.Errorf("bb %s under disable_bb: %v", strings.Join(args, " "), err)
		}
	}
	report, err := runBB("doctor")
	notRefused(t, []string{"doctor"}, err)
	if !strings.Contains(report, "disable_bb") || !strings.Contains(report, file) {
		t.Errorf("bb doctor does not report disable_bb and the file that sets it:\n%s", report)
	}

	output, err := runBB(cobra.ShellCompRequestCmd, "pr", "merge", "")
	if err != nil {
		t.Fatalf("a completion under disable_bb failed: %v", err)
	}
	if !strings.Contains(output, "disable_bb") {
		t.Errorf("the completion does not say why it answers nothing:\n%s", output)
	}
	for _, line := range strings.Split(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "_activeHelp_") && !strings.HasPrefix(trimmed, ":") && !strings.HasPrefix(trimmed, "Completion ended") {
			t.Errorf("a completion under disable_bb offered %q", trimmed)
		}
	}
}

// Under disable_mcp_server the server is refused, and the rest of bb, the
// listing of the server's tools included, keeps working.
func TestDisableMCPServerRefusesOnlyTheServer(t *testing.T) {
	file := writeLeverPolicy(t, "disable_mcp_server: true\n")

	args := []string{"ai", "mcp", "serve"}
	_, err := runBB(args...)
	refusedBy(t, args, err, "disable_mcp_server", file)

	for _, args := range [][]string{{"ai", "mcp", "tools"}, {"pr", "list", "--repo", "PROJ/app"}} {
		_, err := runBB(args...)
		notRefused(t, args, err)
	}
}

// Under read_only a change to Bitbucket is refused before any request, and a
// read, a preview and bb api's reads still go.
func TestReadOnlyRefusesWhatChangesBitbucket(t *testing.T) {
	file := writeLeverPolicy(t, "policy:\n  read_only: true\n")

	for _, args := range [][]string{
		{"pr", "merge", "7", "--repo", "PROJ/app"},
		{"repo", "delete", "PROJ/app", "--yes"},
		{"api", "--method", "POST", "/rest/api/latest/projects"},
		{"api", "/rest/api/latest/projects", "--field", "key=X"},
	} {
		_, err := runBB(args...)
		refusedBy(t, args, err, "read_only", file)
	}

	for _, args := range [][]string{
		{"pr", "merge", "7", "--repo", "PROJ/app", "--dry-run"},
		{"pr", "list", "--repo", "PROJ/app"},
		{"api", "/rest/api/latest/projects"},
		{"api", "--method", "POST", "/rest/api/latest/projects", "--dry-run"},
	} {
		_, err := runBB(args...)
		notRefused(t, args, err)
	}
}

// Every command classified as changing Bitbucket is refused under read_only,
// and none that only reads or works on this machine is: the classification is
// what the lever rests on, and a command added later is covered by it.
func TestReadOnlyCoversEveryCommandThatChangesBitbucket(t *testing.T) {
	writeLeverPolicy(t, "read_only: true\n")

	refused, allowed := 0, 0
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		for _, child := range command.Commands() {
			visit(child)
		}
		if !command.Runnable() || command.Hidden {
			return
		}

		path := dryRunCommandPath(command)
		err := refuseByPolicy(command, false)
		switch {
		case path == "api":
			// Decided by its method, which only the command resolves.
			if err != nil {
				t.Errorf("bb api was refused before its method was known: %v", err)
			}
		case classifyCommand(path) == classificationMutating:
			refused++
			if !apperrors.IsKind(err, apperrors.KindAuthorization) || !strings.Contains(err.Error(), "read_only") {
				t.Errorf("bb %s changes Bitbucket and was not refused under read_only: %v", path, err)
			}
		default:
			allowed++
			if err != nil {
				t.Errorf("bb %s does not change Bitbucket and was refused under read_only: %v", path, err)
			}
		}
	}
	visit(NewRootCommand())

	if refused == 0 || allowed == 0 {
		t.Fatalf("walked %d refused and %d allowed commands; the walk found nothing to hold", refused, allowed)
	}
}

// A system configuration bb cannot read refuses a command, as the lever it may
// carry would, and doctor still runs to say which file is broken.
func TestAnUnreadablePolicyRefusesAllButDoctor(t *testing.T) {
	file := writeLeverPolicy(t, "policy: [\n")

	_, err := runBB("pr", "list", "--repo", "PROJ/app")
	if err == nil || !strings.Contains(err.Error(), file) {
		t.Errorf("a command under an unreadable policy got %v, want an error naming %s", err, file)
	}

	report, _ := runBB("doctor")
	if !strings.Contains(report, "Configuration files") || !strings.Contains(report, file) {
		t.Errorf("bb doctor did not run to report the broken file:\n%s", report)
	}
	for _, args := range [][]string{{"--version"}, {"completion", "bash"}} {
		if _, err := runBB(args...); err != nil {
			t.Errorf("bb %s needed the policy: %v", strings.Join(args, " "), err)
		}
	}
}
