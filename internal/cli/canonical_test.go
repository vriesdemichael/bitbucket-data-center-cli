package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestEveryShallowAliasNamesACommandThatExists keeps shallowAliasCanonical true
// to the tree in both directions: every command whose help calls it an alias is
// in it, and everything it names is a runnable command. An alias missing from
// it would report its own path in meta.command, and its document would no
// longer be its canonical command's (ADR-050).
func TestEveryShallowAliasNamesACommandThatExists(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	runnable := map[string]*cobra.Command{}
	aliases := map[string]bool{}
	saysAlias := regexp.MustCompile(`(?i)\balias for bb\b|\bshallow alias\b`)

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			walk(child)
		}
		if !cmd.Runnable() {
			return
		}
		path := commandPathWithoutRoot(cmd)
		runnable[path] = cmd
		if saysAlias.MatchString(cmd.Short + " " + cmd.Long) {
			aliases[path] = true
		}
	}
	walk(root)

	for path := range aliases {
		if _, ok := shallowAliasCanonical[path]; !ok {
			t.Errorf("%q says it is an alias but names no canonical command in shallowAliasCanonical", path)
		}
	}

	for path, canonical := range shallowAliasCanonical {
		alias, ok := runnable[path]
		if !ok {
			t.Errorf("shallowAliasCanonical lists %q, which is not a runnable command", path)
			continue
		}
		if !aliases[path] {
			t.Errorf("%q is listed as an alias but its help does not say so", path)
		}

		targets := []string{canonical(alias)}
		if alias.Flags().Lookup("group") != nil {
			if err := alias.Flags().Set("group", "true"); err != nil {
				t.Fatalf("%s: set --group: %v", path, err)
			}
			targets = append(targets, canonical(alias))
		}
		for _, target := range targets {
			if _, ok := runnable[target]; !ok {
				t.Errorf("%q stands for %q, which is not a runnable command", path, target)
			}
		}
	}
}

// TestCloneIsAnAliasOfRepoClone: bb clone and bb repo clone are one
// constructor registered twice. bb repo clone names its subject, so it is the
// canonical path (ADR-050): both spellings write one document, meta.command
// naming bb repo clone, and each help names the other.
func TestCloneIsAnAliasOfRepoClone(t *testing.T) {
	root := NewRootCommand()
	alias, _, err := root.Find([]string{"clone"})
	if err != nil {
		t.Fatalf("find clone: %v", err)
	}
	canonical, _, err := root.Find([]string{"repo", "clone"})
	if err != nil {
		t.Fatalf("find repo clone: %v", err)
	}

	for _, cmd := range []*cobra.Command{alias, canonical} {
		if got := CanonicalPath(cmd); got != "repo clone" {
			t.Errorf("bb %s reports meta.command %q, want %q", commandPathWithoutRoot(cmd), got, "repo clone")
		}
	}
	if !strings.Contains(alias.Long, "Alias for bb repo clone") {
		t.Errorf("bb clone's help does not name bb repo clone:\n%s", alias.Long)
	}
	if !strings.Contains(canonical.Long, "Also available as bb clone") {
		t.Errorf("bb repo clone's help does not name bb clone:\n%s", canonical.Long)
	}

	// The preview nothing is sent for, so the whole document can be compared.
	short := runIsolated(t, "--json", "--dry-run", "clone", "PRJ/demo")
	long := runIsolated(t, "--json", "--dry-run", "repo", "clone", "PRJ/demo")
	if short != long {
		t.Errorf("bb clone wrote a different document from bb repo clone\nrepo clone: %s\nclone:      %s", long, short)
	}
}

// TestAShallowAliasReportsItsCanonicalCommand: the permission aliases choose
// users or groups by --group, and meta.command follows the flag.
func TestAShallowAliasReportsItsCanonicalCommand(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	grant, _, err := root.Find(strings.Fields("repo permissions grant"))
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	if got := CanonicalPath(grant); got != "repo settings security permissions users grant" {
		t.Errorf("without --group: %q", got)
	}
	if err := grant.Flags().Set("group", "true"); err != nil {
		t.Fatalf("set --group: %v", err)
	}
	if got := CanonicalPath(grant); got != "repo settings security permissions groups grant" {
		t.Errorf("with --group: %q", got)
	}

	checks, _, _ := root.Find(strings.Fields("pr checks"))
	if got := CanonicalPath(checks); got != "pr build status" {
		t.Errorf("pr checks: %q", got)
	}

	merge, _, _ := root.Find(strings.Fields("pr merge"))
	if got := CanonicalPath(merge); got != "pr merge" {
		t.Errorf("a command that is no alias: %q", got)
	}
}
