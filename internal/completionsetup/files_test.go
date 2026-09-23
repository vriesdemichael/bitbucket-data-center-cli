package completionsetup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestAStartupFileStaysTheFileItWas holds install and remove to changing a
// shared file where it is. A .zshrc linked from a dotfiles repository, or a
// profile with an owner and permissions of its own, is the same file
// afterwards, which a new file renamed over it would not be.
//
// A second hard link shows it on every file system: it sees each change only
// while the name bb writes to is still the same file. A symbolic link shows it
// where this machine can make one, which Windows does only in Developer Mode
// or for an administrator.
func TestAStartupFileStaysTheFileItWas(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	const original = "export EDITOR=vim\n"
	kept := filepath.Join(directory, "zshrc")
	write(t, kept, original)

	hardLink := filepath.Join(directory, "hard-link")
	if err := os.Link(kept, hardLink); err != nil {
		t.Fatalf("link the startup file: %v", err)
	}
	names := []string{hardLink}
	symbolicLink := filepath.Join(directory, ".zshrc")
	if os.Symlink(kept, symbolicLink) == nil {
		names = append(names, symbolicLink)
	}

	for _, name := range names {
		target := Target{Shell: Zsh, Scope: CurrentUser, Path: name, Shared: true}

		if outcome, err := Install(target); err != nil || outcome.Status != Installed {
			t.Fatalf("%s: install = %+v, %v", name, outcome, err)
		}
		if got := read(t, kept); !strings.HasPrefix(got, original) || !strings.Contains(got, beginMarker) {
			t.Errorf("%s: install did not change the file the name leads to:\n%s", name, got)
		}

		if outcome, err := Remove(target); err != nil || outcome.Status != Removed {
			t.Fatalf("%s: remove = %+v, %v", name, outcome, err)
		}
		if got := read(t, kept); got != original {
			t.Errorf("%s: remove did not restore the file the name leads to: %q", name, got)
		}
	}

	if len(names) > 1 {
		if info, err := os.Lstat(symbolicLink); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("the symbolic link was not kept: %v", err)
		}
	}
}

// TestAScriptAPackageLinkedIsLeftToThePackage covers Homebrew on an Intel Mac,
// which links bb's zsh script to where --all-users puts the loader. A link
// there is a package's: install leaves the script it leads to as it is, and
// remove leaves the link. A user's link is to a copy of the user's own, which
// install replaces through the link.
//
// Where this machine cannot make a link, a script there is one somebody saved,
// which install replaces.
func TestAScriptAPackageLinkedIsLeftToThePackage(t *testing.T) {
	t.Parallel()

	const script = "#compdef bb\ncompdef _bb bb\n\n# zsh completion for bb                                    -*- shell-script -*-\n"
	directory := t.TempDir()
	cellar := filepath.Join(directory, "Cellar", "bb", "_bb")
	write(t, cellar, script)

	target := Target{Shell: Zsh, Scope: AllUsers, Path: filepath.Join(directory, "site-functions", "_bb")}
	if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
		t.Fatal(err)
	}

	if os.Symlink(cellar, target.Path) != nil {
		write(t, target.Path, script)
		if outcome, err := Install(target); err != nil || outcome.Status != Updated {
			t.Errorf("install over a saved script = %+v, %v", outcome, err)
		}

		return
	}

	outcome, err := Install(target)
	if err != nil || outcome.Status != Unchanged || !strings.Contains(outcome.Note, "package") {
		t.Errorf("install over a package's link = %+v, %v", outcome, err)
	}
	outcome, err = Remove(target)
	if err != nil || outcome.Status != NotFound || !strings.Contains(outcome.Note, "package") {
		t.Errorf("remove of a package's link = %+v, %v", outcome, err)
	}
	if read(t, cellar) != script {
		t.Error("the package's script was changed")
	}
	if info, err := os.Lstat(target.Path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the package's link was not kept: %v", err)
	}

	saved := filepath.Join(directory, "dotfiles", "_bb")
	write(t, saved, script)
	user := Target{Shell: Zsh, Scope: CurrentUser, Path: filepath.Join(directory, "functions", "_bb")}
	if err := os.MkdirAll(filepath.Dir(user.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(saved, user.Path); err != nil {
		t.Fatalf("a second link could not be made: %v", err)
	}
	if outcome, err := Install(user); err != nil || outcome.Status != Updated {
		t.Errorf("install over a user's linked script = %+v, %v", outcome, err)
	}
	if read(t, saved) != ownFile(Zsh) {
		t.Error("the user's linked script was not replaced by the loader")
	}
}

// TestAnEmptiedProfileThatIsALinkIsKept: a profile that held only bb's block
// goes with it, unless it is a link to a file somebody keeps elsewhere, which
// would lose the link. That one is left, empty.
func TestAnEmptiedProfileThatIsALinkIsKept(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	kept := filepath.Join(directory, "dotfiles", "profile.ps1")
	write(t, kept, "")
	profile := Target{Shell: PowerShell, Scope: CurrentUser, Path: filepath.Join(directory, "profile.ps1"), Shared: true}

	linked := os.Symlink(kept, profile.Path) == nil
	if !linked {
		write(t, profile.Path, "")
	}

	if outcome, err := Install(profile); err != nil || outcome.Status != Installed {
		t.Fatalf("install = %+v, %v", outcome, err)
	}
	if outcome, err := Remove(profile); err != nil || outcome.Status != Removed {
		t.Fatalf("remove = %+v, %v", outcome, err)
	}

	_, err := os.Lstat(profile.Path)
	switch {
	case linked && err != nil:
		t.Errorf("a linked profile was removed: %v", err)
	case linked && strings.TrimSpace(read(t, kept)) != "":
		t.Errorf("the linked profile still holds something: %q", read(t, kept))
	case !linked && !errors.Is(err, os.ErrNotExist):
		t.Errorf("a profile that held only bb's block is still there: %v", err)
	}
}

// TestAFileBbMayNotChangeSaysWhoMay: a file the operating system will not let
// bb change is a permission the person running it lacks, not a failure of
// bb's -- for every user's setup, an administrator's.
func TestAFileBbMayNotChangeSaysWhoMay(t *testing.T) {
	t.Parallel()

	const original = "Write-Host hello\n"
	path := filepath.Join(t.TempDir(), "profile.ps1")
	write(t, path, original)
	// A mode on Linux and macOS, the read-only attribute on Windows, which
	// the temporary directory cannot be removed with.
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	// root writes whatever the mode says, so the file system is asked.
	probe, err := os.OpenFile(path, os.O_WRONLY, 0)
	writable := err == nil
	if writable {
		_ = probe.Close()
	}

	for _, scope := range []Scope{CurrentUser, AllUsers} {
		target := Target{Shell: PowerShell, Scope: scope, Path: path, Shared: true}

		_, err := Install(target)
		switch {
		case writable:
			if err != nil {
				t.Errorf("%s: install into a file this process may write = %v", scope, err)
			}
			if _, err := Remove(target); err != nil {
				t.Errorf("%s: remove = %v", scope, err)
			}
		case apperrors.KindOf(err) != apperrors.KindAuthorization || !strings.Contains(err.Error(), path):
			t.Errorf("%s: install into a file bb may not change gave %v", scope, err)
		case scope == AllUsers && !strings.Contains(err.Error(), "administrator"):
			t.Errorf("an all-users install does not say it needs an administrator: %v", err)
		}
	}

	if got := read(t, path); got != original {
		t.Errorf("the file was changed: %q", got)
	}
}
