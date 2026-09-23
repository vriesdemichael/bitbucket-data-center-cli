package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// fileSystem is each call installing a binary makes that can fail. A test
// replaces one of them to make that step fail and checks what the install left
// behind; the rest of it still runs against the real file system.
type fileSystem struct {
	createTemp func(dir, pattern string) (*os.File, error)
	syncFile   func(*os.File) error
	rename     func(oldPath, newPath string) error
	remove     func(path string) error
	syncDir    func(dir string) error
}

func osFileSystem() fileSystem {
	return fileSystem{
		createTemp: os.CreateTemp,
		syncFile:   (*os.File).Sync,
		rename:     os.Rename,
		remove:     os.Remove,
		syncDir:    syncDirectory,
	}
}

// replaceBinary installs binary over the bb at targetPath.
func replaceBinary(targetPath string, binary []byte, mode fs.FileMode) error {
	return osFileSystem().renameOver(targetPath, binary, mode)
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
