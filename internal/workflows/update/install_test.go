package update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// recording is the real file system with each call noted in calls, in the
// order an install makes it. A call that fail answers with an error is not
// made; the error is returned in its place.
//
// A call names its files by role -- target, temp -- rather than by path,
// because a temporary file's name is random.
func recording(target string, fail map[string]error) (fileSystem, *[]string) {
	calls := &[]string{}
	real := osFileSystem()

	name := func(path string) string {
		switch {
		case path == target:
			return "target"
		case strings.HasPrefix(filepath.Base(path), ".bb-update-"):
			return "temp"
		default:
			return filepath.Base(path)
		}
	}
	call := func(description string) error {
		*calls = append(*calls, description)
		return fail[description]
	}

	return fileSystem{
		createTemp: func(dir, pattern string) (*os.File, error) {
			if err := call("create temp"); err != nil {
				return nil, err
			}
			return real.createTemp(dir, pattern)
		},
		syncFile: func(file *os.File) error {
			if err := call("sync " + name(file.Name())); err != nil {
				return err
			}
			return real.syncFile(file)
		},
		rename: func(oldPath, newPath string) error {
			if err := call("rename " + name(oldPath) + " to " + name(newPath)); err != nil {
				return err
			}
			return real.rename(oldPath, newPath)
		},
		remove: func(path string) error {
			if err := call("remove " + name(path)); err != nil {
				return err
			}
			return real.remove(path)
		},
		syncDir: func(dir string) error {
			if err := call("sync directory"); err != nil {
				return err
			}
			return real.syncDir(dir)
		},
	}, calls
}

// install writes an installed bb at target and returns its permissions as the
// file system stores them.
func install(t *testing.T, target, contents string, mode fs.FileMode) fs.FileMode {
	t.Helper()

	if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
		t.Fatalf("install %s: %v", target, err)
	}
	if err := os.Chmod(target, mode); err != nil {
		t.Fatalf("chmod %s: %v", target, err)
	}

	return permissionsOf(t, target)
}

func permissionsOf(t *testing.T, path string) fs.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	return info.Mode().Perm()
}

// storedPermissions is what this file system reports for a file given mode:
// all of it where the file system keeps Unix permissions, and only whether the
// file is read-only where it does not, as on Windows.
func storedPermissions(t *testing.T, mode fs.FileMode) fs.FileMode {
	t.Helper()

	return install(t, filepath.Join(t.TempDir(), "probe"), "", mode)
}

func assertHolds(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s holds %q, want %q", path, got, want)
	}
}

// directoryEntries is every name in dir, sorted.
func directoryEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

func TestRenameOverReplacesTheTargetInOneRename(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb")
	installed := install(t, target, "old", 0o700)

	files, calls := recording(target, nil)
	if err := files.renameOver(target, []byte("new"), 0o755); err != nil {
		t.Fatalf("renameOver: %v", err)
	}

	assertHolds(t, target, "new")
	if got := permissionsOf(t, target); got != installed {
		t.Errorf("the new binary has permissions %v, want the %v of the one it replaced", got, installed)
	}
	// Synced before the rename that makes it the target, and the directory
	// after. The target is never renamed away, so there is no moment without
	// one, and nothing is kept as a backup.
	want := []string{"create temp", "sync temp", "rename temp to target", "sync directory"}
	if !slices.Equal(*calls, want) {
		t.Errorf("calls = %q, want %q", *calls, want)
	}
	if entries := directoryEntries(t, dir); !slices.Equal(entries, []string{"bb"}) {
		t.Errorf("install directory holds %v, want only bb", entries)
	}
}

func TestRenameOverGivesANewTargetTheArchivesMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb")

	files, calls := recording(target, nil)
	if err := files.renameOver(target, []byte("new"), 0o750); err != nil {
		t.Fatalf("renameOver: %v", err)
	}

	assertHolds(t, target, "new")
	if got, want := permissionsOf(t, target), storedPermissions(t, 0o750); got != want {
		t.Errorf("the new binary has permissions %v, want the archive's, stored as %v", got, want)
	}
	if want := []string{"create temp", "sync temp", "rename temp to target", "sync directory"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %q, want %q", *calls, want)
	}
}

// Syncing the directory is the one step allowed to fail: the new binary is in
// place by then.
func TestRenameOverSucceedsWhenTheDirectoryCannotBeSynced(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb")
	install(t, target, "old", 0o755)

	files, _ := recording(target, map[string]error{"sync directory": errors.New("sync not supported")})
	if err := files.renameOver(target, []byte("new"), 0o755); err != nil {
		t.Fatalf("renameOver: %v", err)
	}

	assertHolds(t, target, "new")
}

// An archive entry can carry no permissions at all. The binary is still one its
// owner can run.
func TestPermissionsForANewTargetFromAnArchiveThatHasNone(t *testing.T) {
	t.Parallel()

	got, err := permissionsFor(filepath.Join(t.TempDir(), "bb"), 0)
	if err != nil || got != 0o755 {
		t.Fatalf("permissionsFor = %v, %v; want 0755", got, err)
	}
}

// The install ignores a directory it cannot sync, but syncDirectory itself says
// so.
func TestSyncDirectoryReportsADirectoryItCannotOpen(t *testing.T) {
	t.Parallel()

	if err := syncDirectory(filepath.Join(t.TempDir(), "gone")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want the missing directory reported", err)
	}
}

func TestRenameOverLeavesTheTargetAsItWasWhenAStepFails(t *testing.T) {
	t.Parallel()

	refused := &fs.PathError{Op: "open", Path: ".bb-update-1", Err: fs.ErrPermission}
	broken := errors.New("input/output error")

	cases := []struct {
		name    string
		fail    map[string]error
		cause   error
		kind    apperrors.Kind
		message func(dir, target string) string
		calls   []string
	}{
		{
			name:  "a directory bb may not write to",
			fail:  map[string]error{"create temp": refused},
			cause: fs.ErrPermission,
			kind:  apperrors.KindAuthorization,
			message: func(dir, _ string) string {
				return "you may not write to " + dir + ", where bb is installed; run bb update again as a user who may"
			},
			calls: []string{"create temp"},
		},
		{
			name:    "a new binary that cannot be synced",
			fail:    map[string]error{"sync temp": broken},
			cause:   broken,
			kind:    apperrors.KindInternal,
			message: func(dir, _ string) string { return "failed to write to " + dir + ", where bb is installed" },
			calls:   []string{"create temp", "sync temp", "remove temp"},
		},
		{
			name:    "a rename that fails",
			fail:    map[string]error{"rename temp to target": broken},
			cause:   broken,
			kind:    apperrors.KindInternal,
			message: func(_, target string) string { return "failed to replace " + target },
			calls:   []string{"create temp", "sync temp", "rename temp to target", "remove temp"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			target := filepath.Join(dir, "bb")
			installed := install(t, target, "old", 0o700)

			files, calls := recording(target, testCase.fail)
			err := files.renameOver(target, []byte("new"), 0o755)

			if !apperrors.IsKind(err, testCase.kind) || !errors.Is(err, testCase.cause) {
				t.Fatalf("got %v, want a %s error caused by %v", err, testCase.kind, testCase.cause)
			}
			if want := testCase.message(dir, target); !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not say %q", err, want)
			}
			assertHolds(t, target, "old")
			if got := permissionsOf(t, target); got != installed {
				t.Errorf("the target's permissions became %v, want %v", got, installed)
			}
			if !slices.Equal(*calls, testCase.calls) {
				t.Errorf("calls = %q, want %q", *calls, testCase.calls)
			}
			if entries := directoryEntries(t, dir); !slices.Equal(entries, []string{"bb"}) {
				t.Errorf("install directory holds %v, want only bb", entries)
			}
		})
	}
}

// A partial download that cannot be removed is reported with the failure that
// left it, because the person reading the error is the one who has to remove it.
func TestRenameOverReportsAPartialDownloadItCannotRemove(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb")
	install(t, target, "old", 0o755)

	renameFailed := errors.New("rename refused")
	removeFailed := errors.New("remove refused")
	files, _ := recording(target, map[string]error{"rename temp to target": renameFailed, "remove temp": removeFailed})
	err := files.renameOver(target, []byte("new"), 0o755)

	if !errors.Is(err, renameFailed) || !errors.Is(err, removeFailed) {
		t.Fatalf("got %v, want both the rename and the removal reported", err)
	}
	assertHolds(t, target, "old")

	// Sorted, so the partial download's leading dot puts it first.
	entries := directoryEntries(t, dir)
	if len(entries) != 2 || !strings.HasPrefix(entries[0], ".bb-update-") || entries[1] != "bb" {
		t.Fatalf("install directory holds %v, want the partial download and bb", entries)
	}
	if want := "could not remove " + filepath.Join(dir, entries[0]); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not say %q", err, want)
	}
}

func TestRenameOverRequiresATarget(t *testing.T) {
	t.Parallel()

	if err := osFileSystem().renameOver(" ", []byte("new"), 0o755); !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("got %v, want a validation error", err)
	}
}
