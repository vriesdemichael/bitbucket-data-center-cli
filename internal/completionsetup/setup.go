// Package completionsetup sets bb's shell completion up where each shell loads
// it from, and takes it out again.
//
// What it writes is a loader rather than a script: a few lines that run `bb
// completion <shell>` when the shell reads them, so completion always matches
// the bb that is installed and an upgrade needs nothing done. A package manager
// ships the script itself to a system directory and replaces it on every
// upgrade; this is for what that leaves -- PowerShell, which has no such
// directory, and a bb that was downloaded rather than installed.
//
// A loader goes either into a file of bb's own, which the shell reads the first
// time it completes bb, or into a marked block in a file bb shares with its
// owner: a PowerShell profile, a .zshrc. Either way, doing it twice changes
// nothing, and removing it takes out exactly what was added.
//
// For every user of the machine there is a place for each shell on Linux and
// macOS, and only PowerShell's on Windows. Where the place depends on how the
// shell was built -- zsh's fpath, fish's configuration directory -- the shell
// is asked, as PowerShell is asked for its profile, and a shell that reads
// nothing bb can write to is told so rather than written to anyway.
package completionsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Shell is a shell bb can set completion up for.
type Shell string

const (
	Bash       Shell = "bash"
	Zsh        Shell = "zsh"
	Fish       Shell = "fish"
	PowerShell Shell = "powershell"
)

// Shells are the shells a setup can be written for, in the order bb documents
// them.
var Shells = []Shell{Bash, Zsh, Fish, PowerShell}

// ParseShell reads a shell's name as `bb completion` spells it.
func ParseShell(name string) (Shell, error) {
	for _, shell := range Shells {
		if strings.EqualFold(strings.TrimSpace(name), string(shell)) {
			return shell, nil
		}
	}

	return "", apperrors.New(apperrors.KindValidation,
		fmt.Sprintf("unknown shell %q: bb sets completion up for bash, zsh, fish and powershell", name), nil)
}

// DetectShell is the shell bb was started from, as far as can be told.
//
// SHELL names the login shell on Linux and macOS. On Windows it is set only by
// shells that came from there -- Git Bash, MSYS2 -- so a Windows machine
// without it is running PowerShell. PowerShell on Linux sets nothing of its
// own, which is why --shell exists.
func DetectShell(system System) (Shell, error) {
	value := strings.TrimSpace(system.Getenv("SHELL"))
	name := strings.TrimSuffix(path.Base(filepath.ToSlash(value)), ".exe")

	switch name {
	case "bash":
		return Bash, nil
	case "zsh":
		return Zsh, nil
	case "fish":
		return Fish, nil
	case "pwsh", "powershell":
		return PowerShell, nil
	}

	if system.GOOS == "windows" && value == "" {
		return PowerShell, nil
	}

	return "", apperrors.New(apperrors.KindValidation,
		"could not tell which shell this is; name it with --shell bash, zsh, fish or powershell", nil)
}

// Scope is whose shells a setup reaches.
type Scope string

const (
	// CurrentUser is the person running bb.
	CurrentUser Scope = "user"
	// AllUsers is everyone on the machine, which needs an administrator.
	AllUsers Scope = "all-users"
)

// Target is one place a setup goes.
type Target struct {
	Shell Shell
	Scope Scope
	// Edition tells PowerShell's two apart: Windows PowerShell 5.1 and
	// PowerShell 7 read different profiles. Empty for the other shells.
	Edition string
	Path    string
	// Shared is a block in a file somebody else owns -- a PowerShell profile,
	// a .zshrc -- rather than a file of bb's own.
	Shared bool
	// Blocked is why the shell would not run the setup, empty when it would.
	// Nothing is written to a blocked target; removing from one is fine.
	Blocked string
}

// Status is what happened to one target.
type Status string

const (
	Installed Status = "installed"
	Updated   Status = "updated"
	Unchanged Status = "unchanged"
	Removed   Status = "removed"
	NotFound  Status = "not_found"
	Blocked   Status = "blocked"
)

// Outcome is what a change did to one target.
type Outcome struct {
	Target Target
	Status Status
	// Note says something the status does not, such as a script written
	// there before that the loader replaced.
	Note string
}

// System is what a setup reads about the machine: the real one outside a test.
type System struct {
	GOOS     string
	Getenv   func(string) string
	HomeDir  func() (string, error)
	LookPath func(string) (string, error)
	// Run runs a shell to ask it something, and returns what it printed.
	Run func(ctx context.Context, executable string, args ...string) (string, error)
}

// Real is the machine bb is running on.
func Real() System {
	return System{
		GOOS:     runtime.GOOS,
		Getenv:   os.Getenv,
		HomeDir:  os.UserHomeDir,
		LookPath: exec.LookPath,
		Run:      run,
	}
}

func run(ctx context.Context, executable string, args ...string) (string, error) {
	// #nosec G204 -- executable is what LookPath found for one of the literal
	// names pwsh, powershell, zsh and fish, and the arguments are this
	// package's own questions.
	process := exec.CommandContext(ctx, executable, args...)

	var stderr bytes.Buffer
	process.Stderr = &stderr

	output, err := process.Output()
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return "", fmt.Errorf("%w: %s", err, message)
		}

		return "", err
	}

	return string(output), nil
}

// Targets are the places a setup of shell for scope goes.
func Targets(ctx context.Context, system System, shell Shell, scope Scope) ([]Target, error) {
	if shell == PowerShell {
		return powerShellTargets(ctx, system, scope)
	}

	if scope == AllUsers {
		return allUsersTargets(ctx, system, shell)
	}

	home, err := system.HomeDir()
	if err != nil {
		return nil, apperrors.New(apperrors.KindInternal, "failed to determine the home directory", err)
	}

	switch shell {
	case Bash:
		// bash-completion reads a file named after the command from here the
		// first time the command is completed.
		directory := absolute(system.Getenv("BASH_COMPLETION_USER_DIR"))
		if directory == "" {
			directory = filepath.Join(xdg(system, "XDG_DATA_HOME", home, ".local", "share"), "bash-completion")
		}

		return []Target{{Shell: Bash, Scope: CurrentUser, Path: filepath.Join(directory, "completions", "bb")}}, nil
	case Zsh:
		// zsh has no directory of its own for a user's completions, so the
		// loader goes into the file every interactive zsh reads.
		directory := absolute(system.Getenv("ZDOTDIR"))
		if directory == "" {
			directory = home
		}

		return []Target{{Shell: Zsh, Scope: CurrentUser, Path: filepath.Join(directory, ".zshrc"), Shared: true}}, nil
	case Fish:
		return []Target{{
			Shell: Fish,
			Scope: CurrentUser,
			Path:  filepath.Join(xdg(system, "XDG_CONFIG_HOME", home, ".config"), "fish", "completions", "bb.fish"),
		}}, nil
	}

	return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("unknown shell %q", shell), nil)
}

// systemBashCompletions is where bash-completion looks, the first time a
// command is completed, for a file named after it that an administrator put
// there: the /usr/local half of its default search, which a package leaves
// alone. Checked on Debian 12 and Fedora 41.
const systemBashCompletions = "/usr/local/share/bash-completion/completions"

// systemZshFunctions is the /usr/local directory on zsh's default fpath on
// Debian, Fedora and macOS. It is used only when the zsh here says so.
const systemZshFunctions = "/usr/local/share/zsh/site-functions"

// allUsersTargets are the places every user's bash, zsh or fish reads.
func allUsersTargets(ctx context.Context, system System, shell Shell) ([]Target, error) {
	if system.GOOS == "windows" {
		return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf(
			"%s on Windows has no place every user's shell reads completion from; run bb completion install --shell %s as each user instead",
			shell, shell), nil)
	}

	switch shell {
	case Bash:
		return []Target{{Shell: Bash, Scope: AllUsers, Path: path.Join(systemBashCompletions, "bb")}}, nil
	case Zsh:
		output, err := ask(ctx, system, "zsh", "-fc", "print -rl -- $fpath")
		if err != nil {
			return nil, err
		}
		for _, directory := range lines(output) {
			if directory == systemZshFunctions {
				return []Target{{Shell: Zsh, Scope: AllUsers, Path: path.Join(directory, "_bb")}}, nil
			}
		}

		return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf(
			"this zsh does not read %s, which is where bb puts completion for every user; "+
				"run bb completion install --shell zsh as each user instead", systemZshFunctions), nil)
	case Fish:
		output, err := ask(ctx, system, "fish", "--no-config", "-c", "echo $__fish_sysconf_dir")
		if err != nil {
			return nil, err
		}
		directory := strings.Join(lines(output), "")
		if !path.IsAbs(directory) {
			return nil, apperrors.New(apperrors.KindInternal, fmt.Sprintf("fish named %q as its configuration directory", directory), nil)
		}

		return []Target{{Shell: Fish, Scope: AllUsers, Path: path.Join(directory, "completions", "bb.fish")}}, nil
	}

	return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("unknown shell %q", shell), nil)
}

// ask runs the named shell with args and returns what it printed.
func ask(ctx context.Context, system System, name string, args ...string) (string, error) {
	executable, err := system.LookPath(name)
	if err != nil {
		return "", apperrors.New(apperrors.KindValidation, name+" is not installed here, so there is nowhere to set its completion up", err)
	}

	output, err := system.Run(ctx, executable, args...)
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "could not ask "+name+" where it reads completion from", err)
	}

	return output, nil
}

// byteOrderMark is what Windows PowerShell 5.1 can put in front of its output.
var byteOrderMark = string(rune(0xFEFF))

// lines are the non-empty lines of output, trimmed.
func lines(output string) []string {
	found := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if trimmed := strings.TrimSpace(strings.TrimPrefix(line, byteOrderMark)); trimmed != "" {
			found = append(found, trimmed)
		}
	}

	return found
}

// xdg is an XDG base directory: the variable when it holds an absolute path,
// which the specification requires, and the default under home otherwise.
func xdg(system System, variable, home string, fallback ...string) string {
	if directory := absolute(system.Getenv(variable)); directory != "" {
		return directory
	}

	return filepath.Join(append([]string{home}, fallback...)...)
}

func absolute(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return ""
	}

	return value
}

// edition is one PowerShell installed on the machine.
type edition struct {
	name       string
	executable string
}

// powerShellEditions are the PowerShells found here. Windows PowerShell 5.1
// comes with Windows and PowerShell 7 is installed beside it, and each reads a
// profile of its own, so a setup goes to every one that is there.
func powerShellEditions(system System) []edition {
	wanted := []edition{{name: "PowerShell 7", executable: "pwsh"}}
	if system.GOOS == "windows" {
		wanted = append(wanted, edition{name: "Windows PowerShell 5.1", executable: "powershell"})
	}

	found := make([]edition, 0, len(wanted))
	for _, candidate := range wanted {
		if path, err := system.LookPath(candidate.executable); err == nil {
			found = append(found, edition{name: candidate.name, executable: path})
		}
	}

	return found
}

// powerShellTargets asks each PowerShell where its profile is, rather than
// working it out: the profile lives under the Documents folder, which OneDrive
// and folder redirection move and Windows names in the user's language.
func powerShellTargets(ctx context.Context, system System, scope Scope) ([]Target, error) {
	editions := powerShellEditions(system)
	if len(editions) == 0 {
		return nil, apperrors.New(apperrors.KindValidation, "no PowerShell was found on PATH (looked for pwsh and, on Windows, powershell)", nil)
	}

	property := "CurrentUserAllHosts"
	if scope == AllUsers {
		property = "AllUsersAllHosts"
	}
	// All hosts rather than the console alone: the profile the terminal in an
	// editor reads is the all-hosts one, not the console's.
	query := "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $PROFILE." + property + "; Get-ExecutionPolicy"

	targets := make([]Target, len(editions))

	var wait sync.WaitGroup
	for index, found := range editions {
		wait.Add(1)

		go func() {
			defer wait.Done()

			target := Target{Shell: PowerShell, Scope: scope, Edition: found.name, Shared: true}

			output, err := system.Run(ctx, found.executable, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", query)
			if err != nil {
				target.Blocked = "could not ask it where its profile is: " + err.Error()
				targets[index] = target

				return
			}

			path, policy := profileAndPolicy(output)
			target.Path = path
			switch {
			case path == "":
				target.Blocked = "it did not say where its profile is"
			default:
				target.Blocked = blockedByPolicy(policy)
			}
			targets[index] = target
		}()
	}
	wait.Wait()

	return targets, nil
}

func profileAndPolicy(output string) (path, policy string) {
	printed := lines(output)
	if len(printed) < 2 {
		return "", ""
	}

	return printed[len(printed)-2], printed[len(printed)-1]
}

// blockedByPolicy is why an execution policy keeps a profile from running,
// empty when it does not. A fresh Windows installation sets Windows PowerShell
// 5.1 to Restricted, which runs no script at all, profiles included.
func blockedByPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "restricted":
		return "its execution policy is Restricted, so it runs no profile script"
	case "allsigned":
		return "its execution policy is AllSigned, so it runs only signed scripts, and an edited profile is no longer signed"
	default:
		return ""
	}
}

// Install writes the setup to target, or leaves it as it is when it is
// already there.
func Install(target Target) (Outcome, error) {
	if target.Blocked != "" {
		return Outcome{Target: target, Status: Blocked, Note: target.Blocked}, nil
	}

	if target.Shared {
		return installBlock(target)
	}

	return installFile(target)
}

// Remove takes the setup out of target, and reports not_found when it was not
// there.
func Remove(target Target) (Outcome, error) {
	if target.Path == "" {
		return Outcome{Target: target, Status: NotFound}, nil
	}

	if target.Shared {
		return removeBlock(target)
	}

	return removeFile(target)
}

// State is what a target holds, for bb doctor.
type State struct {
	// Present is whether bb's setup is there.
	Present bool
	// Current is whether it is exactly what Install would write now.
	Current bool
	// Script is a script written there by hand from `bb completion <shell>`,
	// which does not follow an upgrade the way the loader does.
	Script bool
}

// Inspect reads what target holds without changing it.
func Inspect(target Target) (State, error) {
	if target.Path == "" {
		return State{}, nil
	}

	if target.Shared {
		file, err := readText(target.Path, "\n")
		if err != nil || !file.exists {
			return State{}, err
		}

		start, end, found, err := findBlock(file.text)
		if err != nil || !found {
			return State{}, err
		}

		return State{Present: true, Current: file.text[start:end] == block(target.Shell, file.newline)}, nil
	}

	content, err := os.ReadFile(target.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}

	switch {
	case isOwnFile(content):
		return State{Present: true, Current: string(content) == ownFile(target.Shell)}, nil
	case isGeneratedScript(target.Shell, content):
		return State{Script: true}, nil
	default:
		return State{}, nil
	}
}

// Loader is the text a setup writes to target, for a caller that shows it.
func Loader(target Target) string {
	if target.Shared {
		return block(target.Shell, "\n")
	}

	return ownFile(target.Shell)
}
