package update

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// fileSystem is each call installing a binary makes that can fail, and the
// clock it waits on. A test replaces one of them to make that step fail, or
// time pass, and checks what the install left behind; the rest of it still
// runs against the real file system.
type fileSystem struct {
	createTemp func(dir, pattern string) (*os.File, error)
	syncFile   func(*os.File) error
	rename     func(oldPath, newPath string) error
	remove     func(path string) error
	syncDir    func(dir string) error
	now        func() time.Time
	sleep      func(time.Duration)
}

func osFileSystem() fileSystem {
	return fileSystem{
		createTemp: os.CreateTemp,
		syncFile:   (*os.File).Sync,
		rename:     os.Rename,
		remove:     os.Remove,
		syncDir:    syncDirectory,
		now:        time.Now,
		sleep:      time.Sleep,
	}
}

// installBinary puts binary in place of the bb at targetPath, in the way goos
// lets a running binary be replaced.
func installBinary(goos, targetPath string, binary []byte, mode fs.FileMode) error {
	files := osFileSystem()
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		return files.renameAside(targetPath, binary, mode)
	}

	return files.renameOver(targetPath, binary, mode)
}

// renameOver replaces the file at targetPath with binary in one rename.
//
// The rename replaces the name atomically: whoever opens targetPath finds the
// old binary or the new one and never neither, and a bb that is running keeps
// the old one, because it holds the file rather than the name. No backup is
// kept. A failure before the rename leaves the target as it was, and the rename
// cannot leave it half replaced.
//
// The new file is written beside the target and synced before the rename: a
// rename moves a file within one file system only, and one that reaches the
// disk before the data it names can leave an empty binary after a crash.
func (files fileSystem) renameOver(targetPath string, binary []byte, mode fs.FileMode) error {
	target, err := installTarget(targetPath)
	if err != nil {
		return err
	}

	temp, err := files.writeBeside(target, binary, mode)
	if err != nil {
		return err
	}

	if err := files.rename(temp, target); err != nil {
		return files.discard(temp, changeFailure("replace "+target, err))
	}

	// The new binary is in place by now, so a directory that cannot be synced
	// does not fail the update. Where the file system can sync one, this is
	// what makes the rename survive a crash; some cannot.
	_ = files.syncDir(filepath.Dir(target))

	return nil
}

// renameAside replaces the binary at targetPath with binary on Windows, which
// renames a running executable but will not overwrite or delete one. The
// running binary is renamed aside and the new one renamed into its place.
//
// The name it is set aside under is one no other file has, because an earlier
// one can still be held by a bb running from it; the next run deletes it
// (RemoveUpdateLeftovers). Each rename waits out another process holding the
// file for a moment (renameWhenFree). When the new binary cannot be renamed
// into place, the old one is renamed back, and when that fails too, the error
// says where bb is.
func (files fileSystem) renameAside(targetPath string, binary []byte, mode fs.FileMode) error {
	target, err := installTarget(targetPath)
	if err != nil {
		return err
	}

	temp, err := files.writeBeside(target, binary, mode)
	if err != nil {
		return err
	}

	aside := oldBinaryPath(target)
	if err := files.renameWhenFree(target, aside); err != nil {
		return files.discard(temp, changeFailure("move "+target+" aside", err))
	}

	if err := files.renameWhenFree(temp, target); err != nil {
		if restoreErr := files.renameWhenFree(aside, target); restoreErr != nil {
			return files.discard(temp, apperrors.New(
				apperrors.KindInternal,
				fmt.Sprintf("failed to move the new binary to %s, and to move the old one back; rename %s to %s to restore bb", target, aside, filepath.Base(target)),
				errors.Join(err, restoreErr),
			))
		}

		return files.discard(temp, changeFailure("move the new binary to "+target, err))
	}

	return nil
}

// oldBinaryMarker and the random hex digits after it are what an update adds to
// the name of the binary it sets aside.
const (
	oldBinaryMarker = ".old-"
	oldBinaryDigits = 16
)

// oldBinaryPath is a name beside target that no other file has, for the binary
// an update sets aside.
func oldBinaryPath(target string) string {
	suffix := make([]byte, oldBinaryDigits/2)
	// crypto/rand.Read never fails; it fills the buffer or ends the process.
	_, _ = rand.Read(suffix)

	return target + oldBinaryMarker + hex.EncodeToString(suffix)
}

// A virus scanner or a search indexer opens a file it has just seen written or
// renamed, and holds it for a moment without sharing it. A rename in that
// moment fails with one of these, and succeeds a moment later. They are
// Windows error numbers: renameAside is the Windows install.
const (
	errorAccessDenied     = syscall.Errno(5)
	errorSharingViolation = syscall.Errno(32)
)

// renameRetryBudget is how long a rename waits for another process to let go
// of a file, as long as the go command waits for its own renames.
const renameRetryBudget = 2 * time.Second

// renameWhenFree renames oldPath to newPath, and tries again while the rename
// fails because another process holds the file: after 10ms, then after a pause
// twice as long each time, until renameRetryBudget has passed. Any other
// failure, and one that outlasts the budget, is returned as it is.
func (files fileSystem) renameWhenFree(oldPath, newPath string) error {
	start := files.now()
	pause := 10 * time.Millisecond

	for {
		err := files.rename(oldPath, newPath)
		if err == nil || !heldByAnotherProcess(err) {
			return err
		}

		remaining := renameRetryBudget - files.now().Sub(start)
		if remaining <= 0 {
			return err
		}
		files.sleep(min(pause, remaining))
		pause *= 2
	}
}

// heldByAnotherProcess reports whether err is how Windows refuses a rename while
// another process holds the file.
func heldByAnotherProcess(err error) bool {
	var errno syscall.Errno

	return errors.As(err, &errno) && (errno == errorAccessDenied || errno == errorSharingViolation)
}

// oldHelperFiles are what the swap helper of bb releases before this install
// wrote beside bb.exe: the binary it staged, and the file it recorded its
// outcome in. Neither is read or written any more.
var oldHelperFiles = []string{".new", ".update-result.json"}

// RemoveUpdateLeftovers deletes what earlier updates left next to the running
// bb, which only happens on Windows: the binaries an update set aside, and the
// files the helper of earlier releases wrote. It runs at the start of every bb
// run and reports nothing; a binary that another bb is still running from
// cannot be deleted yet, and waits for a later run.
func RemoveUpdateLeftovers() {
	osFileSystem().removeLeftoversOn(runtime.GOOS, os.Executable)
}

// removeLeftoversOn is RemoveUpdateLeftovers on goos, for the binary executable
// locates.
func (files fileSystem) removeLeftoversOn(goos string, executable func() (string, error)) {
	if !strings.EqualFold(strings.TrimSpace(goos), "windows") {
		return
	}

	path, err := executable()
	if err != nil {
		return
	}

	files.removeLeftovers(path)
}

// removeLeftovers deletes what it can of the files updates left next to
// executable, and ignores the rest.
func (files fileSystem) removeLeftovers(executable string) {
	directory := filepath.Dir(executable)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if isLeftover(filepath.Base(executable), entry.Name()) {
			_ = files.remove(filepath.Join(directory, entry.Name()))
		}
	}
}

// isLeftover reports whether name is a file an update left next to executable:
// a binary it set aside, or exactly one of the helper's files.
func isLeftover(executable, name string) bool {
	if isOldBinary(executable, name) {
		return true
	}

	for _, suffix := range oldHelperFiles {
		if strings.EqualFold(name, executable+suffix) {
			return true
		}
	}

	return false
}

// isOldBinary reports whether name is one oldBinaryPath gives a binary set aside
// from executable. Nothing else matches: not a copy somebody kept as bb.exe.old,
// and not another program's file in the same directory.
func isOldBinary(executable, name string) bool {
	prefix := executable + oldBinaryMarker
	if len(name) != len(prefix)+oldBinaryDigits || !strings.EqualFold(name[:len(prefix)], prefix) {
		return false
	}

	_, err := hex.DecodeString(name[len(prefix):])

	return err == nil
}

func installTarget(targetPath string) (string, error) {
	target := strings.TrimSpace(targetPath)
	if target == "" {
		return "", apperrors.New(apperrors.KindValidation, "target executable path is required", nil)
	}

	return target, nil
}

// writeBeside writes binary to a new file in target's directory, with target's
// permissions, syncs it to disk, and returns its path. When it fails it leaves
// nothing behind, or its error names what it left.
func (files fileSystem) writeBeside(target string, binary []byte, mode fs.FileMode) (string, error) {
	permissions, err := permissionsFor(target, mode)
	if err != nil {
		return "", changeFailure("read the permissions of "+target, err)
	}

	directory := filepath.Dir(target)
	writeTo := "write to " + directory + ", where bb is installed"

	file, err := files.createTemp(directory, ".bb-update-*")
	if err != nil {
		return "", changeFailure(writeTo, err)
	}

	_, writeErr := file.Write(binary)
	if err := errors.Join(writeErr, file.Chmod(permissions), files.syncFile(file), file.Close()); err != nil {
		return "", files.discard(file.Name(), changeFailure(writeTo, err))
	}

	return file.Name(), nil
}

// discard removes a file the install wrote and will not use, and returns
// failure. When the file cannot be removed, failure says so: it is left in the
// install directory, and whoever reads the error is the one to remove it.
func (files fileSystem) discard(path string, failure *apperrors.AppError) error {
	err := files.remove(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return failure
	}

	return apperrors.New(
		failure.Kind,
		fmt.Sprintf("%s, and could not remove %s", failure.Message, path),
		errors.Join(failure.Cause, err),
	)
}

// changeFailure is why a step of an install failed. A step the operating system
// refused is not a failure of bb's but a permission the person running it
// lacks, and the message says who has it.
func changeFailure(action string, err error) *apperrors.AppError {
	if errors.Is(err, fs.ErrPermission) {
		return apperrors.New(
			apperrors.KindAuthorization,
			fmt.Sprintf("you may not %s; run bb update again as a user who may, with sudo or from a PowerShell run as administrator", action),
			err,
		)
	}

	return apperrors.New(apperrors.KindInternal, "failed to "+action, err)
}

// permissionBits are the parts of a file mode a new binary takes from the one
// it replaces.
const permissionBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// permissionsFor is the mode a new binary at target gets: the target's own, so
// an update changes neither who may run bb nor who may replace it, or the
// archive's when there is no target to take it from.
func permissionsFor(target string, archived fs.FileMode) (fs.FileMode, error) {
	info, err := os.Stat(target)
	if err == nil {
		return info.Mode() & permissionBits, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}

	if archived&fs.ModePerm == 0 {
		return 0o755, nil
	}

	return archived & permissionBits, nil
}

// syncDirectory writes dir's entries to disk, which is what makes a rename in
// it survive a crash.
func syncDirectory(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}

	return errors.Join(directory.Sync(), directory.Close())
}
