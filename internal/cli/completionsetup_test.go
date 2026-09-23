package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/completionsetup"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// runCompletionSetup runs `bb completion ...` with fish's configuration under
// a directory of the test's own, which is the one shell whose user setup is a
// file bb owns outright on every OS: nothing of the machine's is touched.
func runCompletionSetup(t *testing.T, configHome string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("XDG_CONFIG_HOME", configHome)

	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(append([]string{"completion"}, args...))

	err := command.Execute()

	return output.String(), err
}

func TestCompletionInstallSetsAShellUpAndRemoveTakesItOut(t *testing.T) {
	configHome := t.TempDir()
	loader := filepath.Join(configHome, "fish", "completions", "bb.fish")

	output, err := runCompletionSetup(t, configHome, "install", "--shell", "fish")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "fish: set up in "+loader) || !strings.Contains(output, "Open a new shell") {
		t.Errorf("install said:\n%s", output)
	}
	if content, err := os.ReadFile(loader); err != nil || !strings.Contains(string(content), "bb completion fish | source") {
		t.Fatalf("the loader was not written: %v\n%s", err, content)
	}

	output, err = runCompletionSetup(t, configHome, "install", "--shell", "fish")
	if err != nil || !strings.Contains(output, "already set up") {
		t.Errorf("a second install said %v:\n%s", err, output)
	}

	output, err = runCompletionSetup(t, configHome, "remove", "--shell", "fish", "--yes")
	if err != nil || !strings.Contains(output, "fish: removed from "+loader) {
		t.Errorf("remove said %v:\n%s", err, output)
	}
	if _, err := os.Stat(loader); !os.IsNotExist(err) {
		t.Errorf("the loader is still there: %v", err)
	}

	output, err = runCompletionSetup(t, configHome, "remove", "--shell", "fish", "--yes")
	if err != nil || !strings.Contains(output, "nothing to remove") {
		t.Errorf("a second remove said %v:\n%s", err, output)
	}
}

func TestCompletionInstallReportsEachTargetUnderJSON(t *testing.T) {
	configHome := t.TempDir()

	output, err := runCompletionSetup(t, configHome, "install", "--shell", "fish", "--json")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	var envelope struct {
		Data CompletionSetup `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("the output is not one JSON document: %v\n%s", err, output)
	}

	report := envelope.Data
	if report.Shell != "fish" || report.Scope != "user" || len(report.Targets) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if target := report.Targets[0]; target.Status != "installed" || target.Path != filepath.Join(configHome, "fish", "completions", "bb.fish") {
		t.Errorf("target = %+v", target)
	}
}

func TestCompletionInstallRefusesAShellItDoesNotKnow(t *testing.T) {
	if _, err := runCompletionSetup(t, t.TempDir(), "install", "--shell", "cmd"); err == nil {
		t.Error("an unknown shell was accepted")
	}
}

func TestAnInstallThatSetNothingUpFailsAndSaysWhy(t *testing.T) {
	t.Parallel()

	restricted := completionsetup.Outcome{
		Target: completionsetup.Target{Shell: completionsetup.PowerShell, Edition: "Windows PowerShell 5.1"},
		Status: completionsetup.Blocked,
		Note:   "its execution policy is Restricted, so it runs no profile script",
	}

	err := nothingSetUp([]completionsetup.Outcome{restricted})
	if apperrors.KindOf(err) != apperrors.KindPermanent {
		t.Fatalf("every target blocked gave %v", err)
	}
	for _, want := range []string{"Windows PowerShell 5.1", "Restricted", "Set-ExecutionPolicy -Scope CurrentUser RemoteSigned"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not say %q: %v", want, err)
		}
	}

	// One PowerShell set up and the other blocked is a success, which says
	// what was blocked and how to unblock it.
	installed := completionsetup.Outcome{
		Target: completionsetup.Target{Shell: completionsetup.PowerShell, Edition: "PowerShell 7", Path: "profile.ps1"},
		Status: completionsetup.Installed,
	}
	if err := nothingSetUp([]completionsetup.Outcome{installed, restricted}); err != nil {
		t.Errorf("a partial setup failed: %v", err)
	}

	output := &bytes.Buffer{}
	writeCompletionSetup(output, []completionsetup.Outcome{installed, restricted})
	for _, want := range []string{
		"PowerShell 7: set up in profile.ps1",
		"Windows PowerShell 5.1: not set up, because its execution policy is Restricted",
		"Set-ExecutionPolicy -Scope CurrentUser RemoteSigned",
		"Open a new shell",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("the report does not say %q:\n%s", want, output)
		}
	}
}

func TestEveryOutcomeIsReportedInWords(t *testing.T) {
	t.Parallel()

	fish := completionsetup.Target{Shell: completionsetup.Fish, Path: "bb.fish"}
	output := &bytes.Buffer{}
	writeCompletionSetup(output, []completionsetup.Outcome{
		{Target: fish, Status: completionsetup.Updated, Note: "replaced a script saved from an earlier bb"},
		{Target: fish, Status: completionsetup.Removed},
		{Target: fish, Status: completionsetup.NotFound, Note: "bb.fish is not bb's, so it was left alone"},
	})

	for _, want := range []string{
		"fish: updated in bb.fish",
		"  replaced a script saved from an earlier bb",
		"fish: removed from bb.fish",
		"fish: nothing to remove in bb.fish",
		"  bb.fish is not bb's, so it was left alone",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("the report does not say %q:\n%s", want, output)
		}
	}
}
