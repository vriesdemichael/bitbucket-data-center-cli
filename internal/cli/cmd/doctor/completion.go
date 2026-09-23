package doctorcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/completionsetup"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The kinds of place shell completion is set up in.
const (
	// placeSetup is what bb completion install writes: a loader that runs bb
	// completion <shell>, so it follows every upgrade.
	placeSetup = "setup"
	// placeScript is a script somebody saved from bb completion <shell>. It
	// stays as it was when an upgrade changes what bb prints.
	placeScript = "script"
	// placePackage is the script a package manager installed with bb, and
	// replaces on every upgrade.
	placePackage = "package"
	// placeStartup is a startup file somebody added bb completion to by hand.
	placeStartup = "startup"
)

// shellCompletion is where completion is set up for one shell.
type shellCompletion struct {
	shell     completionsetup.Shell
	installed bool
	places    []completionPlace
	// unchecked are places bb's own setup could be that could not be read: a
	// file that will not open, or a block somebody cut short. bb completion
	// install and remove refuse them as well.
	unchecked []issue
}

// completionPlace is one place completion is set up.
type completionPlace struct {
	kind    string
	scope   completionsetup.Scope
	edition string
	path    string
	// current is whether it is exactly what this bb writes: bb completion
	// install's loader as it is now, or the script bb completion <shell>
	// prints.
	current bool
	// problem says, under the place, why it needs fixing; issue says the same
	// for error.details. Both are empty for a fact.
	problem string
	issue   *issue
}

// inspectCompletion finds where completion is set up for every shell that is
// installed, and for any other shell something of bb's is found for.
//
// A failure to generate this bb's script, which a saved one is compared with,
// is a failure of bb doctor itself and ends the run.
func inspectCompletion(
	ctx context.Context,
	machine Machine,
	generate func(completionsetup.Shell, bool) (string, error),
) ([]shellCompletion, error) {
	targets := targetsByShell(ctx, machine.System)

	found := []shellCompletion{}
	for _, shell := range completionsetup.Shells {
		completion := shellCompletion{shell: shell, installed: completionsetup.OnPath(machine.System, shell)}
		generated := &generatedScripts{shell: shell, generate: generate}

		packaged := machine.PackagedScripts(shell)
		fromPackage := map[string]bool{}
		for _, script := range packaged {
			fromPackage[filepath.Clean(script)] = true
		}

		for _, target := range targets[shell] {
			if err := completion.inspect(target, fromPackage, generated); err != nil {
				return nil, err
			}
		}

		for _, script := range packaged {
			state, err := completionsetup.Inspect(completionsetup.Target{Shell: shell, Scope: completionsetup.AllUsers, Path: script})
			if err == nil && state.Script {
				completion.places = append(completion.places, completionPlace{kind: placePackage, scope: completionsetup.AllUsers, path: script})
			}
		}

		// A startup file that cannot be read runs nothing either.
		for _, file := range completionsetup.StartupFiles(machine.System, shell, targets[shell]) {
			if byHand, err := completionsetup.SetUpByHand(file.Path); err == nil && byHand {
				completion.places = append(completion.places, completionPlace{
					kind: placeStartup, scope: file.Scope, edition: file.Edition, path: file.Path,
				})
			}
		}

		// Each PowerShell together, in the order Targets found them.
		editions := completionsetup.Editions()
		sort.SliceStable(completion.places, func(i, j int) bool {
			return slices.Index(editions, completion.places[i].edition) < slices.Index(editions, completion.places[j].edition)
		})

		if completion.installed || len(completion.places) > 0 || len(completion.unchecked) > 0 {
			found = append(found, completion)
		}
	}

	return found, nil
}

// targetsByShell asks where every shell's setup goes, for both scopes, at
// once: each PowerShell is a process started and asked for each scope, and
// asked one after another the waits would add up.
//
// A shell that is not installed, or that has no place for every user here, has
// nothing set up there, so an error is nothing found rather than a failure.
func targetsByShell(ctx context.Context, system completionsetup.System) map[completionsetup.Shell][]completionsetup.Target {
	scopes := []completionsetup.Scope{completionsetup.CurrentUser, completionsetup.AllUsers}
	answers := make([][]completionsetup.Target, len(completionsetup.Shells)*len(scopes))

	var wait sync.WaitGroup
	for shellIndex, shell := range completionsetup.Shells {
		for scopeIndex, scope := range scopes {
			wait.Add(1)

			go func() {
				defer wait.Done()

				if targets, err := completionsetup.Targets(ctx, system, shell, scope); err == nil {
					answers[shellIndex*len(scopes)+scopeIndex] = targets
				}
			}()
		}
	}
	wait.Wait()

	byShell := map[completionsetup.Shell][]completionsetup.Target{}
	for shellIndex, shell := range completionsetup.Shells {
		for scopeIndex := range scopes {
			byShell[shell] = append(byShell[shell], answers[shellIndex*len(scopes)+scopeIndex]...)
		}
	}

	return byShell
}

// inspect adds what target holds of bb's. A script a package installed is left
// to the packages, which report it as theirs: Homebrew on an Intel Mac puts
// its zsh script where bb completion install --all-users would.
func (completion *shellCompletion) inspect(target completionsetup.Target, fromPackage map[string]bool, generated *generatedScripts) error {
	if target.Path == "" {
		return nil
	}

	state, err := completionsetup.Inspect(target)
	switch {
	case err != nil:
		completion.unchecked = append(completion.unchecked, issue{
			key:     completionKey(target),
			message: target.Path + ": " + readFailure(err),
			summary: labelOf(target) + " completion",
		})
	case state.Present:
		place := completionPlace{kind: placeSetup, scope: target.Scope, edition: target.Edition, path: target.Path, current: state.Current}
		if target.Blocked != "" {
			place.problem, place.issue = notRun(target)
		}
		completion.places = append(completion.places, place)
	case state.Script && !fromPackage[filepath.Clean(target.Path)]:
		content, readErr := os.ReadFile(target.Path)
		if readErr != nil {
			completion.unchecked = append(completion.unchecked, issue{
				key:     completionKey(target),
				message: target.Path + ": " + readFailure(readErr),
				summary: labelOf(target) + " completion",
			})
			return nil
		}

		current, err := generated.include(content)
		if err != nil {
			return err
		}

		place := completionPlace{kind: placeScript, scope: target.Scope, edition: target.Edition, path: target.Path, current: current}
		if !current {
			place.problem, place.issue = fallenBehind(target)
		}
		completion.places = append(completion.places, place)
	}

	return nil
}

// generatedScripts are the scripts this bb prints for one shell, with and
// without descriptions, generated the first time a saved script needs them.
type generatedScripts struct {
	shell    completionsetup.Shell
	generate func(completionsetup.Shell, bool) (string, error)
	scripts  []string
}

// include reports whether a saved script is one this bb prints. Line endings
// aside: a script saved through PowerShell 7 has CRLF, and is no less current
// for it.
func (generated *generatedScripts) include(saved []byte) (bool, error) {
	if generated.scripts == nil {
		for _, withDescriptions := range []bool{true, false} {
			script, err := generated.generate(generated.shell, withDescriptions)
			if err != nil {
				return false, err
			}
			generated.scripts = append(generated.scripts, withLF(script))
		}
	}

	return slices.Contains(generated.scripts, withLF(string(saved))), nil
}

// notRun is the issue with bb's setup in a file its shell will not run: a
// PowerShell profile its execution policy keeps from running. bb does not
// change the policy itself: it is a security setting, and not bb's to relax.
// It says how to, when a policy is the reason.
func notRun(target completionsetup.Target) (string, *issue) {
	label := labelOf(target)

	reason := fmt.Sprintf("%s does not run it: %s", label, target.Blocked)
	if strings.Contains(target.Blocked, "execution policy") {
		reason += fmt.Sprintf("; to let it, run Set-ExecutionPolicy -Scope CurrentUser RemoteSigned in %s", label)
	}

	return reason, &issue{
		key:     completionKey(target),
		message: target.Path + ": bb completion is set up here, and " + reason,
		summary: label + " completion",
	}
}

// fallenBehind is the issue with a saved script that is not what this bb
// prints. install replaces it with the loader, which is what stops it falling
// behind again.
func fallenBehind(target completionsetup.Target) (string, *issue) {
	command := "bb completion install --shell " + string(target.Shell)
	if target.Scope == completionsetup.AllUsers {
		command += " --all-users"
	}
	remedy := command + " replaces it with a loader that follows upgrades"

	return "it is not what this bb prints, so it has fallen behind; " + remedy, &issue{
		key: completionKey(target),
		message: fmt.Sprintf("%s: a script saved from bb completion %s, which is not what this bb prints, so it has fallen behind; %s",
			target.Path, target.Shell, remedy),
		summary: labelOf(target) + " completion",
	}
}

// completionKey is where error.details names an issue with completion: by
// shell and scope, and for PowerShell by edition, whose execution policy
// decides whether any of its profiles runs.
func completionKey(target completionsetup.Target) string {
	if target.Edition != "" {
		return "completion/" + string(target.Shell) + "/" + target.Edition
	}

	return "completion/" + string(target.Shell) + "/" + string(target.Scope)
}

func labelOf(target completionsetup.Target) string {
	if target.Edition != "" {
		return target.Edition
	}

	return string(target.Shell)
}

// readFailure says why a file could not be checked, without the path a
// PathError would repeat.
func readFailure(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return "could not be read: " + pathErr.Err.Error()
	}

	return apperrors.MessageOf(err)
}

func withLF(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func completionIssues(shells []shellCompletion) []issue {
	found := []issue{}
	for _, completion := range shells {
		for _, place := range completion.places {
			if place.issue != nil {
				found = append(found, *place.issue)
			}
		}
		found = append(found, completion.unchecked...)
	}

	return found
}

func writeCompletion(w io.Writer, shells []shellCompletion) {
	fmt.Fprintln(w, "Shell completion")
	if len(shells) == 0 {
		fmt.Fprintln(w, "  none of bash, zsh, fish or PowerShell is installed")
		return
	}

	width := 0
	for _, completion := range shells {
		width = max(width, len(completion.shell))
	}
	indent := strings.Repeat(" ", 2+width+2)

	for _, completion := range shells {
		lines := []string{}
		if !completion.installed {
			lines = append(lines, "not installed")
		}
		for _, place := range completion.places {
			lines = append(lines, describePlace(completion.shell, place))
			if place.problem != "" {
				lines = append(lines, "problem: "+place.problem)
			}
		}
		for _, unchecked := range completion.unchecked {
			lines = append(lines, "problem: "+unchecked.message)
		}
		if len(lines) == 0 {
			lines = append(lines, "not set up")
		}

		fmt.Fprintf(w, "  %-*s  %s\n", width, completion.shell, lines[0])
		for _, line := range lines[1:] {
			fmt.Fprintf(w, "%s%s\n", indent, line)
		}
	}
}

func describePlace(shell completionsetup.Shell, place completionPlace) string {
	var text string
	switch place.kind {
	case placeSetup:
		text = "set up in " + place.path
		if !place.current {
			text += ", by an earlier bb"
		}
	case placeScript:
		text = "a script saved from bb completion " + string(shell) + " in " + place.path
	case placePackage:
		text = "installed by a package in " + place.path
	case placeStartup:
		text = "set up by hand in " + place.path
	}

	if place.edition != "" {
		return place.edition + ": " + text
	}

	return text
}
