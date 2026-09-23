package update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// recording is the real file system with each call noted in calls, in the
// order an install makes it. A call that fail answers with an error is not
// made; the error is returned in its place. Its clock is a fake one: a pause is
// noted as a call too, and moves the clock on without waiting.
//
// A call names its files by role -- target, temp, aside -- rather than by path,
// because the names of the other two are random.
func recording(target string, fail func(call string) error) (fileSystem, *[]string) {
	calls := &[]string{}
	real := osFileSystem()
	clock := time.Date(2026, time.September, 23, 12, 0, 0, 0, time.UTC)

	name := func(path string) string {
		switch {
		case path == target:
			return "target"
		case strings.HasPrefix(filepath.Base(path), ".bb-update-"):
			return "temp"
		case isOldBinary(filepath.Base(target), filepath.Base(path)):
			return "aside"
		default:
			return filepath.Base(path)
		}
	}
	call := func(description string) error {
		*calls = append(*calls, description)
		if fail == nil {
			return nil
		}
		return fail(description)
	}

	return fileSystem{
		now: func() time.Time { return clock },
		sleep: func(pause time.Duration) {
			*calls = append(*calls, "sleep "+pause.String())
			clock = clock.Add(pause)
		},
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

// failing fails each call errs names, every time it is made, and lets the rest
// through.
func failing(errs map[string]error) func(string) error {
	return func(call string) error { return errs[call] }
}

// failingTimes fails call with err the first times it is made, and lets it
// through after.
func failingTimes(call string, times int, err error) func(string) error {
	return func(made string) error {
		if made != call || times == 0 {
			return nil
		}
		times--
		return err
	}
}

// renameRefused is a rename Windows refused with errno.
func renameRefused(errno syscall.Errno) error {
	return &os.LinkError{Op: "rename", Old: "old", New: "new", Err: errno}
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

	files, _ := recording(target, failing(map[string]error{"sync directory": errors.New("sync not supported")}))
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

			files, calls := recording(target, failing(testCase.fail))
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
	files, _ := recording(target, failing(map[string]error{"rename temp to target": renameFailed, "remove temp": removeFailed}))
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

// splitSetAside sorts what dir holds into the binaries an update set aside from
// executable, and everything else.
func splitSetAside(t *testing.T, dir, executable string) (setAside, others []string) {
	t.Helper()

	for _, name := range directoryEntries(t, dir) {
		if isOldBinary(executable, name) {
			setAside = append(setAside, name)
		} else {
			others = append(others, name)
		}
	}

	return setAside, others
}

func TestRenameAsideSetsTheOldBinaryAsideAndRenamesTheNewOneIn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb.exe")
	// Read-only, which Windows keeps too, as an attribute.
	installed := install(t, target, "old", 0o500)

	files, calls := recording(target, nil)
	if err := files.renameAside(target, []byte("new"), 0o755); err != nil {
		t.Fatalf("renameAside: %v", err)
	}

	assertHolds(t, target, "new")
	if got := permissionsOf(t, target); got != installed {
		t.Errorf("the new binary has permissions %v, want the %v of the one it replaced", got, installed)
	}
	want := []string{"create temp", "sync temp", "rename target to aside", "rename temp to target"}
	if !slices.Equal(*calls, want) {
		t.Errorf("calls = %q, want %q", *calls, want)
	}

	setAside, others := splitSetAside(t, dir, "bb.exe")
	if len(setAside) != 1 || !slices.Equal(others, []string{"bb.exe"}) {
		t.Fatalf("install directory holds %v and set aside %v, want bb.exe and the one binary it replaced", others, setAside)
	}
	assertHolds(t, filepath.Join(dir, setAside[0]), "old")
}

// The binary an earlier update set aside can still be running, so each update
// sets the one it replaces aside under a name of its own.
func TestRenameAsideNeverReusesAName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb.exe")
	install(t, target, "v1", 0o755)

	for _, version := range []string{"v2", "v3"} {
		if err := osFileSystem().renameAside(target, []byte(version), 0o755); err != nil {
			t.Fatalf("renameAside %s: %v", version, err)
		}
	}

	assertHolds(t, target, "v3")
	setAside, _ := splitSetAside(t, dir, "bb.exe")
	held := make([]string, 0, len(setAside))
	for _, name := range setAside {
		contents, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		held = append(held, string(contents))
	}
	slices.Sort(held)
	if !slices.Equal(held, []string{"v1", "v2"}) {
		t.Fatalf("the binaries set aside hold %q, want both replaced versions, v1 and v2", held)
	}
}

func TestRenameAsideLeavesTheTargetAsItWasWhenAStepFails(t *testing.T) {
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
			name:    "a running binary that cannot be set aside",
			fail:    map[string]error{"rename target to aside": broken},
			cause:   broken,
			kind:    apperrors.KindInternal,
			message: func(_, target string) string { return "failed to move " + target + " aside" },
			calls:   []string{"create temp", "sync temp", "rename target to aside", "remove temp"},
		},
		{
			// The old binary goes back where it was.
			name:    "a new binary that cannot be renamed into place",
			fail:    map[string]error{"rename temp to target": broken},
			cause:   broken,
			kind:    apperrors.KindInternal,
			message: func(_, target string) string { return "failed to move the new binary to " + target },
			calls:   []string{"create temp", "sync temp", "rename target to aside", "rename temp to target", "rename aside to target", "remove temp"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			target := filepath.Join(dir, "bb.exe")
			installed := install(t, target, "old", 0o700)

			files, calls := recording(target, failing(testCase.fail))
			err := files.renameAside(target, []byte("new"), 0o755)

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
			if entries := directoryEntries(t, dir); !slices.Equal(entries, []string{"bb.exe"}) {
				t.Errorf("install directory holds %v, want only bb.exe", entries)
			}
		})
	}
}

// When the old binary cannot be moved back either, bb is no longer at its path.
// Both failures are reported, and the message says how to put bb back.
func TestRenameAsideReportsBothFailuresWhenTheOldBinaryCannotBeMovedBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb.exe")
	install(t, target, "old", 0o755)

	installFailed := errors.New("the new binary was refused")
	restoreFailed := errors.New("the old binary was refused")
	files, calls := recording(target, failing(map[string]error{"rename temp to target": installFailed, "rename aside to target": restoreFailed}))
	err := files.renameAside(target, []byte("new"), 0o755)

	if !apperrors.IsKind(err, apperrors.KindInternal) || !errors.Is(err, installFailed) || !errors.Is(err, restoreFailed) {
		t.Fatalf("got %v, want an internal error reporting both renames", err)
	}
	want := []string{"create temp", "sync temp", "rename target to aside", "rename temp to target", "rename aside to target", "remove temp"}
	if !slices.Equal(*calls, want) {
		t.Errorf("calls = %q, want %q", *calls, want)
	}

	setAside, others := splitSetAside(t, dir, "bb.exe")
	if len(setAside) != 1 || len(others) != 0 {
		t.Fatalf("install directory holds %v and set aside %v, want only the old binary, set aside", others, setAside)
	}
	aside := filepath.Join(dir, setAside[0])
	assertHolds(t, aside, "old")
	if want := "rename " + aside + " to bb.exe to restore bb"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not say %q", err, want)
	}
}

func TestRenameAsideRequiresATarget(t *testing.T) {
	t.Parallel()

	if err := osFileSystem().renameAside("", []byte("new"), 0o755); !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("got %v, want a validation error", err)
	}
}

// A virus scanner holds a file it has just seen written or renamed for a moment,
// and Windows refuses a rename in that moment. The install tries again, with a
// pause that doubles each time, and goes on once the file is free.
func TestRenameAsideWaitsForAFileAnotherProcessHolds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		call  string
		errno syscall.Errno
		calls []string
	}{
		{
			name:  "the new binary, held by a scanner",
			call:  "rename temp to target",
			errno: errorSharingViolation,
			calls: []string{
				"create temp", "sync temp", "rename target to aside",
				"rename temp to target", "sleep 10ms", "rename temp to target", "sleep 20ms",
				"rename temp to target", "sleep 40ms", "rename temp to target",
			},
		},
		{
			name:  "the running binary",
			call:  "rename target to aside",
			errno: errorAccessDenied,
			calls: []string{
				"create temp", "sync temp",
				"rename target to aside", "sleep 10ms", "rename target to aside", "sleep 20ms",
				"rename target to aside", "sleep 40ms", "rename target to aside",
				"rename temp to target",
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			target := filepath.Join(dir, "bb.exe")
			install(t, target, "old", 0o755)

			files, calls := recording(target, failingTimes(testCase.call, 3, renameRefused(testCase.errno)))
			if err := files.renameAside(target, []byte("new"), 0o755); err != nil {
				t.Fatalf("renameAside: %v", err)
			}

			assertHolds(t, target, "new")
			if !slices.Equal(*calls, testCase.calls) {
				t.Errorf("calls = %q, want %q", *calls, testCase.calls)
			}
			if setAside, others := splitSetAside(t, dir, "bb.exe"); len(setAside) != 1 || !slices.Equal(others, []string{"bb.exe"}) {
				t.Errorf("install directory holds %v and set aside %v, want bb.exe and the one binary it replaced", others, setAside)
			}
		})
	}
}

// A file held for longer than two seconds is not waited for any longer: the
// refusal is reported, and the old binary goes back.
func TestRenameAsideGivesUpOnAFileHeldTooLong(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb.exe")
	installed := install(t, target, "old", 0o700)

	files, calls := recording(target, failing(map[string]error{"rename temp to target": renameRefused(errorSharingViolation)}))
	err := files.renameAside(target, []byte("new"), 0o755)

	if !apperrors.IsKind(err, apperrors.KindInternal) || !errors.Is(err, errorSharingViolation) {
		t.Fatalf("got %v, want the refusal reported", err)
	}
	if want := "failed to move the new binary to " + target; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not say %q", err, want)
	}

	// Pauses that double from 10ms, the last cut short, two seconds in all.
	want := []string{"create temp", "sync temp", "rename target to aside"}
	for _, pause := range []string{"10ms", "20ms", "40ms", "80ms", "160ms", "320ms", "640ms", "730ms"} {
		want = append(want, "rename temp to target", "sleep "+pause)
	}
	want = append(want, "rename temp to target", "rename aside to target", "remove temp")
	if !slices.Equal(*calls, want) {
		t.Errorf("calls = %q, want %q", *calls, want)
	}

	assertHolds(t, target, "old")
	if got := permissionsOf(t, target); got != installed {
		t.Errorf("the target's permissions became %v, want %v", got, installed)
	}
	if entries := directoryEntries(t, dir); !slices.Equal(entries, []string{"bb.exe"}) {
		t.Errorf("install directory holds %v, want only bb.exe", entries)
	}
}

// Putting the old binary back waits for a held file too.
func TestRenameAsideWaitsToPutTheOldBinaryBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb.exe")
	install(t, target, "old", 0o755)

	broken := errors.New("input/output error")
	restoreRefusals := 2
	files, calls := recording(target, func(call string) error {
		switch {
		case call == "rename temp to target":
			return broken
		case call == "rename aside to target" && restoreRefusals > 0:
			restoreRefusals--
			return renameRefused(errorSharingViolation)
		}
		return nil
	})
	if err := files.renameAside(target, []byte("new"), 0o755); !errors.Is(err, broken) {
		t.Fatalf("got %v, want the failed rename reported", err)
	}

	want := []string{
		"create temp", "sync temp", "rename target to aside", "rename temp to target",
		"rename aside to target", "sleep 10ms", "rename aside to target", "sleep 20ms", "rename aside to target",
		"remove temp",
	}
	if !slices.Equal(*calls, want) {
		t.Errorf("calls = %q, want %q", *calls, want)
	}
	assertHolds(t, target, "old")
}

// Only a held file is waited for. Anything else is reported at once, the file
// not being there included, which the go command does wait for.
func TestRenameAsideDoesNotWaitOutOtherFailures(t *testing.T) {
	t.Parallel()

	for name, failure := range map[string]error{
		"a broken disk":   errors.New("input/output error"),
		"a missing file":  renameRefused(syscall.Errno(2)),
		"no errno at all": &fs.PathError{Op: "rename", Path: "bb.exe", Err: fs.ErrPermission},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			target := filepath.Join(dir, "bb.exe")
			install(t, target, "old", 0o755)

			files, calls := recording(target, failing(map[string]error{"rename temp to target": failure}))
			if err := files.renameAside(target, []byte("new"), 0o755); !errors.Is(err, failure) {
				t.Fatalf("got %v, want %v reported", err, failure)
			}

			want := []string{"create temp", "sync temp", "rename target to aside", "rename temp to target", "rename aside to target", "remove temp"}
			if !slices.Equal(*calls, want) {
				t.Errorf("calls = %q, want %q", *calls, want)
			}
			assertHolds(t, target, "old")
		})
	}
}

// Linux and macOS have no need to wait: a rename there does not care who holds
// the file.
func TestRenameOverDoesNotWait(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "bb")
	install(t, target, "old", 0o755)

	refused := renameRefused(errorSharingViolation)
	files, calls := recording(target, failing(map[string]error{"rename temp to target": refused}))
	if err := files.renameOver(target, []byte("new"), 0o755); !errors.Is(err, refused) {
		t.Fatalf("got %v, want the refusal reported", err)
	}

	if want := []string{"create temp", "sync temp", "rename temp to target", "remove temp"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %q, want %q", *calls, want)
	}
	assertHolds(t, target, "old")
}

func TestRemoveUpdateLeftoversDeletesOnlyWhatUpdatesLeft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	left := []string{
		"bb.exe.old-0123456789abcdef",
		// Set aside by a bb started as BB.EXE; Windows names are not case sensitive.
		"BB.EXE.old-FEDCBA9876543210",
		// What the helper of earlier releases wrote: the binary it staged, and
		// the file it recorded its outcome in.
		"bb.exe.new",
		"BB.EXE.UPDATE-RESULT.JSON",
	}
	kept := []string{
		"bb.exe",
		"bb.exe.old",                     // a copy somebody kept by hand
		"bb.exe.old-v4.2.0",              // and another
		"bb.exe.old-0123456789abcde",     // one digit short
		"bb.exe.old-0123456789abcdeg",    // not hex
		"other.exe.old-0123456789abcdef", // another program's
		".bb-update-123",                 // what an update running beside this one is writing
		"bb.exe.newer",                   // names that only start like the helper's
		"bb.exe.new.txt",
		"bb.exe.update-result",
		"bb.exe.update-result.json.bak",
		"other.exe.new", // another program's
		"bb.new",
	}
	for _, name := range append(slices.Clone(left), kept...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	osFileSystem().removeLeftovers(filepath.Join(dir, "bb.exe"))

	want := slices.Clone(kept)
	slices.Sort(want)
	if got := directoryEntries(t, dir); !slices.Equal(got, want) {
		t.Fatalf("after the cleanup the directory holds %v, want %v", got, want)
	}
}

// A binary another bb is still running from cannot be deleted yet. The cleanup
// passes over it, says nothing, and deletes the rest.
func TestRemoveUpdateLeftoversCarriesOnPastOneItCannotDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const held, free = "bb.exe.old-0000000000000000", "bb.exe.old-ffffffffffffffff"
	for _, name := range []string{"bb.exe", held, free} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	files := osFileSystem()
	files.remove = func(path string) error {
		if filepath.Base(path) == held {
			return &fs.PathError{Op: "remove", Path: path, Err: fs.ErrPermission}
		}
		return os.Remove(path)
	}
	files.removeLeftovers(filepath.Join(dir, "bb.exe"))

	if got := directoryEntries(t, dir); !slices.Equal(got, []string{"bb.exe", held}) {
		t.Fatalf("after the cleanup the directory holds %v, want bb.exe and the binary it could not delete", got)
	}
}

// Only an update on Windows leaves anything beside bb, so only a run on Windows
// looks.
func TestRemoveUpdateLeftoversLooksOnlyOnWindows(t *testing.T) {
	t.Parallel()

	for goos, removed := range map[string]bool{"windows": true, "linux": false, "darwin": false} {
		t.Run(goos, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			executable := filepath.Join(dir, "bb.exe")
			left := []string{executable + ".old-0123456789abcdef", executable + ".new"}
			for _, path := range append([]string{executable}, left...) {
				if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
			}

			osFileSystem().removeLeftoversOn(goos, func() (string, error) { return executable, nil })

			for _, path := range left {
				if _, err := os.Stat(path); (err != nil) != removed {
					t.Fatalf("on %s %s was removed: %v, want %v", goos, filepath.Base(path), err != nil, removed)
				}
			}
		})
	}
}

// With no executable to look beside, or no directory to look in, the cleanup
// removes nothing and says nothing.
func TestRemoveUpdateLeftoversGivesUpQuietly(t *testing.T) {
	t.Parallel()

	files := osFileSystem()
	files.remove = func(path string) error {
		t.Errorf("removed %s", path)
		return nil
	}

	files.removeLeftoversOn("windows", func() (string, error) { return "", errors.New("no executable path") })
	files.removeLeftovers(filepath.Join(t.TempDir(), "gone", "bb.exe"))
}
