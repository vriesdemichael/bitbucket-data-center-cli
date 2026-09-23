package doctorcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/completionsetup"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

const (
	seven = "PowerShell 7"
	five  = "Windows PowerShell 5.1"
)

// TestCompletionSetUpAnywhereIsReportedAsFact covers every place completion
// can be set up that needs nothing fixed: bb's loader, current or an earlier
// bb's; a package's script; a saved script that is what this bb prints; and a
// startup file set up by hand. None of them fails the run.
func TestCompletionSetUpAnywhereIsReportedAsFact(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	machineWide := t.TempDir()
	packages := t.TempDir()

	sevenUser := filepath.Join(home, "PowerShell", "profile.ps1")
	sevenAll := filepath.Join(machineWide, "PowerShell", "7", "profile.ps1")
	fiveUser := filepath.Join(home, "WindowsPowerShell", "profile.ps1")
	fiveAll := filepath.Join(machineWide, "WindowsPowerShell", "v1.0", "profile.ps1")
	packaged := filepath.Join(packages, "bash-completion", "completions", "bb")

	fake := &fakeMachine{
		home: home,
		work: t.TempDir(),
		installed: map[string]string{
			"bash": "bash.exe", "pwsh": "pwsh.exe", "powershell": "powershell.exe",
		},
		profiles: map[string]profileAnswers{
			"pwsh.exe":       {user: sevenUser + "\nRemoteSigned\n", allUsers: sevenAll + "\nRemoteSigned\n"},
			"powershell.exe": {user: fiveUser + "\r\nRemoteSigned\r\n", allUsers: fiveAll + "\r\nRemoteSigned\r\n"},
		},
		packaged: map[completionsetup.Shell][]string{
			completionsetup.Bash: {packaged},
			completionsetup.Zsh:  {filepath.Join(packages, "zsh", "vendor-completions", "_bb")},
		},
	}

	// bash: bb's loader, a package's script, and a line in .bashrc. The line
	// commented out in .bash_profile runs nothing.
	bashUser := filepath.Join(home, ".local", "share", "bash-completion", "completions", "bb")
	install(t, completionsetup.Target{Shell: completionsetup.Bash, Scope: completionsetup.CurrentUser, Path: bashUser})
	script, _ := generatedScript(completionsetup.Bash, true)
	put(t, packaged, script)
	bashrc := filepath.Join(home, ".bashrc")
	put(t, bashrc, "export EDITOR=vim\nsource <(bb completion bash)\n")
	put(t, filepath.Join(home, ".bash_profile"), "# source <(bb completion bash)\n")

	// zsh is not installed, and a .zshrc brought from another machine sets it
	// up all the same.
	zshrc := filepath.Join(home, ".zshrc")
	put(t, zshrc, "autoload -Uz compinit && compinit\n")
	install(t, completionsetup.Target{Shell: completionsetup.Zsh, Scope: completionsetup.CurrentUser, Path: zshrc, Shared: true})

	// Nor is fish, whose script was saved from this bb without descriptions,
	// through PowerShell 7, which writes CRLF.
	fishUser := filepath.Join(home, ".config", "fish", "completions", "bb.fish")
	script, _ = generatedScript(completionsetup.Fish, false)
	put(t, fishUser, strings.ReplaceAll(script, "\n", "\r\n"))

	// PowerShell 7: the current loader for the user, an earlier bb's for every
	// user. Windows PowerShell 5.1: a line in the console's profile, which is
	// the one $PROFILE names.
	install(t, completionsetup.Target{Shell: completionsetup.PowerShell, Scope: completionsetup.CurrentUser, Edition: seven, Path: sevenUser, Shared: true})
	earlier := strings.Replace(completionsetup.Loader(completionsetup.Target{Shell: completionsetup.PowerShell, Shared: true}), "| Out-String ", "", 1)
	put(t, sevenAll, earlier)
	fiveConsole := filepath.Join(home, "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
	put(t, fiveConsole, "Import-Module posh-git\r\nbb completion powershell | Out-String | Invoke-Expression\r\n")

	output, _, err := runDoctorOn(t, fake.machine(), config.Diagnosis{}, false)
	if err != nil {
		t.Fatalf("facts failed the run: %v\n%s", err, output)
	}

	want := strings.Join([]string{
		"Shell completion",
		"  bash        set up in " + bashUser,
		"              installed by a package in " + packaged,
		"              set up by hand in " + bashrc,
		"  zsh         not installed",
		"              set up in " + zshrc,
		"  fish        not installed",
		"              a script saved from bb completion fish in " + fishUser,
		"  powershell  PowerShell 7: set up in " + sevenUser,
		"              PowerShell 7: set up in " + sevenAll + ", by an earlier bb",
		"              Windows PowerShell 5.1: set up by hand in " + fiveConsole,
		"",
	}, "\n")
	if !strings.Contains(output, want) {
		t.Errorf("the report lacks\n%s\nin\n%s", want, output)
	}
	if !strings.HasSuffix(output, "No issues found.\n") {
		t.Errorf("the report does not end saying there is nothing to fix:\n%s", output)
	}

	output, _, err = runDoctorOn(t, fake.machine(), config.Diagnosis{}, true)
	if err != nil {
		t.Fatalf("facts failed the --json run: %v", err)
	}
	report := decodeReport(t, output)

	user, allUsers := string(completionsetup.CurrentUser), string(completionsetup.AllUsers)
	wantCompletion := []Completion{
		{Shell: "bash", Installed: true, Places: []CompletionPlace{
			{Kind: placeSetup, Scope: user, Path: bashUser, Current: true},
			{Kind: placePackage, Scope: allUsers, Path: packaged},
			{Kind: placeStartup, Scope: user, Path: bashrc},
		}},
		{Shell: "zsh", Places: []CompletionPlace{{Kind: placeSetup, Scope: user, Path: zshrc, Current: true}}},
		{Shell: "fish", Places: []CompletionPlace{{Kind: placeScript, Scope: user, Path: fishUser, Current: true}}},
		{Shell: "powershell", Installed: true, Places: []CompletionPlace{
			{Kind: placeSetup, Scope: user, Edition: seven, Path: sevenUser, Current: true},
			{Kind: placeSetup, Scope: allUsers, Edition: seven, Path: sevenAll},
			{Kind: placeStartup, Scope: user, Edition: five, Path: fiveConsole},
		}},
	}
	if !reflect.DeepEqual(report.Completion, wantCompletion) {
		t.Errorf("completion:\n got %+v\nwant %+v", report.Completion, wantCompletion)
	}
}

// TestCompletionThatDoesNotWorkFailsTheRun covers what needs fixing: bb's
// setup in a profile its PowerShell will not run, a saved script this bb has
// moved on from, and a block somebody cut short, which bb completion install
// and remove refuse to touch.
func TestCompletionThatDoesNotWorkFailsTheRun(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	fiveUser := filepath.Join(home, "WindowsPowerShell", "profile.ps1")
	fake := &fakeMachine{
		home:      home,
		work:      t.TempDir(),
		installed: map[string]string{"zsh": "zsh.exe", "fish": "fish.exe", "powershell": "powershell.exe"},
		profiles: map[string]profileAnswers{
			"powershell.exe": {user: fiveUser + "\r\nRestricted\r\n"},
		},
	}

	// Set up while the execution policy allowed it; Restricted since.
	install(t, completionsetup.Target{Shell: completionsetup.PowerShell, Scope: completionsetup.CurrentUser, Edition: five, Path: fiveUser, Shared: true})

	fishUser := filepath.Join(home, ".config", "fish", "completions", "bb.fish")
	put(t, fishUser, "# fish completion for bb                   -*- shell-script -*-\n# the script an earlier bb printed\n")

	block := strings.Split(strings.TrimSuffix(completionsetup.Loader(completionsetup.Target{Shell: completionsetup.Zsh, Shared: true}), "\n"), "\n")
	zshrc := filepath.Join(home, ".zshrc")
	put(t, zshrc, strings.Join(block[:len(block)-1], "\n")+"\n")

	remedy := "bb completion install --shell fish replaces it with a loader that follows upgrades"
	unblock := "; to let it, run Set-ExecutionPolicy -Scope CurrentUser RemoteSigned in Windows PowerShell 5.1"

	output, _, err := runDoctorOn(t, fake.machine(), config.Diagnosis{}, false)
	if apperrors.KindOf(err) != apperrors.KindPermanent || apperrors.ExitCode(err) != 1 {
		t.Fatalf("issues must exit 1 as permanent, got %v\n%s", err, output)
	}
	if want := "3 issues to fix: zsh completion, fish completion, Windows PowerShell 5.1 completion"; apperrors.MessageOf(err) != want {
		t.Errorf("message = %q, want %q", apperrors.MessageOf(err), want)
	}

	details := apperrors.DetailsOf(err)
	if len(details) != 3 {
		t.Errorf("details = %#v, want three entries", details)
	}
	if got, want := details["completion/fish/user"], fishUser+": a script saved from bb completion fish, which is not what this bb prints, so it has fallen behind; "+remedy; got != want {
		t.Errorf("completion/fish/user:\n got %q\nwant %q", got, want)
	}
	powerShell := details["completion/powershell/"+five]
	if !strings.HasPrefix(powerShell, fiveUser+": bb completion is set up here, and Windows PowerShell 5.1 does not run it: ") ||
		!strings.Contains(powerShell, "Restricted") || !strings.HasSuffix(powerShell, unblock) {
		t.Errorf("completion/powershell/%s = %q", five, powerShell)
	}
	if zsh := details["completion/zsh/user"]; !strings.HasPrefix(zsh, zshrc+": ") || !strings.Contains(zsh, "end marker") {
		t.Errorf("completion/zsh/user = %q", zsh)
	}

	for _, line := range []string{
		"  zsh         set up by hand in " + zshrc + "\n              problem: " + details["completion/zsh/user"] + "\n",
		"  fish        a script saved from bb completion fish in " + fishUser + "\n" +
			"              problem: it is not what this bb prints, so it has fallen behind; " + remedy + "\n",
		"  powershell  Windows PowerShell 5.1: set up in " + fiveUser + "\n" +
			"              problem: Windows PowerShell 5.1 does not run it: ",
		unblock + "\n",
		"3 issues to fix.",
	} {
		if !strings.Contains(output, line) {
			t.Errorf("the report lacks %q:\n%s", line, output)
		}
	}

	output, _, err = runDoctorOn(t, fake.machine(), config.Diagnosis{}, true)
	if output != "" {
		t.Errorf("a run with issues wrote a report beside its failure:\n%s", output)
	}
	written := &bytes.Buffer{}
	if writeErr := jsonoutput.WriteError(written, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	var envelope jsonoutput.ErrorEnvelope
	if decodeErr := json.Unmarshal(written.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("not one JSON document: %v\n%s", decodeErr, written)
	}
	if !reflect.DeepEqual(envelope.Error.Details, details) {
		t.Errorf("the envelope's details:\n got %#v\nwant %#v", envelope.Error.Details, details)
	}
}

// TestAShellIsListedWhenInstalledOrSetUp: a shell that is not installed and
// has nothing of bb's is not mentioned, and a shell whose setup cannot be
// worked out -- no place for every user on Windows, a PowerShell that does
// not answer -- has nothing set up there rather than failing the run.
func TestAShellIsListedWhenInstalledOrSetUp(t *testing.T) {
	t.Parallel()

	output, _, err := runDoctor(t, config.Diagnosis{}, false)
	if err != nil || !strings.Contains(output, "Shell completion\n  none of bash, zsh, fish or PowerShell is installed\n") {
		t.Errorf("a machine with no shell: err=%v\n%s", err, output)
	}

	fake := &fakeMachine{
		home:      t.TempDir(),
		work:      t.TempDir(),
		installed: map[string]string{"bash": "bash.exe", "pwsh": "pwsh.exe"},
		failures:  map[string]error{"pwsh.exe": errors.New("exit status 1")},
	}

	output, _, err = runDoctorOn(t, fake.machine(), config.Diagnosis{}, false)
	if err != nil {
		t.Fatalf("nothing set up failed the run: %v\n%s", err, output)
	}
	if want := "Shell completion\n  bash        not set up\n  powershell  not set up\n\n"; !strings.Contains(output, want) {
		t.Errorf("the report lacks\n%s\nin\n%s", want, output)
	}

	output, _, err = runDoctorOn(t, fake.machine(), config.Diagnosis{}, true)
	if err != nil {
		t.Fatalf("nothing set up failed the --json run: %v", err)
	}
	want := []Completion{
		{Shell: "bash", Installed: true, Places: []CompletionPlace{}},
		{Shell: "powershell", Installed: true, Places: []CompletionPlace{}},
	}
	if report := decodeReport(t, output); !reflect.DeepEqual(report.Completion, want) {
		t.Errorf("completion:\n got %+v\nwant %+v", report.Completion, want)
	}
}

// TestAScriptAPackageInstalledIsThePackages covers a place that is both. On
// an Intel Mac, Homebrew puts its zsh script where bb completion install
// --all-users would. It is the package's to replace, so it is reported as the
// package's and is never out of date for bb doctor, whichever bb is running.
func TestAScriptAPackageInstalledIsThePackages(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	fishUser := filepath.Join(home, ".config", "fish", "completions", "bb.fish")
	put(t, fishUser, "# fish completion for bb                   -*- shell-script -*-\n# the script another bb printed\n")

	fake := &fakeMachine{
		home:     home,
		work:     t.TempDir(),
		packaged: map[completionsetup.Shell][]string{completionsetup.Fish: {fishUser}},
	}

	output, _, err := runDoctorOn(t, fake.machine(), config.Diagnosis{}, false)
	if err != nil {
		t.Fatalf("a package's script failed the run: %v\n%s", err, output)
	}
	if want := "  fish  not installed\n        installed by a package in " + fishUser + "\n"; !strings.Contains(output, want) {
		t.Errorf("the report lacks\n%s\nin\n%s", want, output)
	}
}

// TestASavedScriptThatCannotBeComparedFailsBbDoctorItself: not being able to
// generate this bb's script is a bug in bb, not a finding about the machine,
// so it keeps its own kind rather than becoming an issue to fix.
func TestASavedScriptThatCannotBeComparedFailsBbDoctorItself(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	put(t, filepath.Join(home, ".config", "fish", "completions", "bb.fish"), "# fish completion for bb                   -*- shell-script -*-\n")

	root := &cobra.Command{Use: "bb", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(New(Dependencies{
		Diagnose: func(config.DiagnoseInput) config.Diagnosis { return config.Diagnosis{} },
		CompletionScript: func(completionsetup.Shell, bool) (string, error) {
			return "", apperrors.New(apperrors.KindInternal, "the fish script Cobra generates has moved on", nil)
		},
		Machine: (&fakeMachine{home: home, work: t.TempDir()}).machine(),
	}))
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"doctor"})

	if err := root.Execute(); apperrors.KindOf(err) != apperrors.KindInternal || !strings.Contains(err.Error(), "has moved on") {
		t.Errorf("a script that cannot be generated gave %v, want the internal failure", err)
	}
}
