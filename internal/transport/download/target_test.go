package download

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// errorPrivilegeNotHeld is how Windows refuses a symbolic link to a process
// that runs neither as an administrator nor in developer mode.
const errorPrivilegeNotHeld = syscall.Errno(1314)

// symlink points link at target, and reports whether this machine let it. The
// one refusal accepted is Windows withholding the privilege, which is logged;
// any other failure fails the test, so nothing else can make a test that needs
// a link assert less than it says.
func symlink(t *testing.T, target, link string) bool {
	t.Helper()

	err := os.Symlink(target, link)
	if err == nil {
		return true
	}
	if !errors.Is(err, errorPrivilegeNotHeld) {
		t.Fatalf("link %s to %s: %v", link, target, err)
	}
	t.Logf("this machine does not let the test create a symbolic link, so only what needs none was asserted: %v", err)

	return false
}

// canonicalTempDir is a directory for one test with its own links resolved:
// /var is a link to /private/var on macOS, and a Windows temporary directory can
// be named in its short 8.3 form. A test comparing what a link points at needs
// the name the file system gives back.
func canonicalTempDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the test directory: %v", err)
	}

	return dir
}

// downloadInto writes body into target through a File and puts it in place, as
// bb repo archive does with what it downloads. adjust, when given, sees the
// File before anything is written.
func downloadInto(t *testing.T, target string, body string, adjust func(*File)) error {
	t.Helper()

	file, err := CreateFile(target)
	if err != nil {
		return err
	}
	defer file.Discard()

	if adjust != nil {
		adjust(file)
	}
	if _, err := file.Write([]byte(body)); err != nil {
		t.Fatalf("write into %s: %v", target, err)
	}

	return file.Commit()
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(content)
}

func permissions(t *testing.T, path string) fs.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	return info.Mode().Perm()
}

// TestADownloadIntoALinkReplacesTheFileItNames: os.Create wrote through a
// symbolic link, and so does a download. The file the link names is replaced,
// from a temporary file beside it, and the link stays a link to it.
func TestADownloadIntoALinkReplacesTheFileItNames(t *testing.T) {
	t.Parallel()

	links, files := canonicalTempDir(t), canonicalTempDir(t)
	named := filepath.Join(files, "snapshot.zip")
	if err := os.WriteFile(named, []byte("last week's archive"), 0o600); err != nil {
		t.Fatalf("write %s: %v", named, err)
	}

	// Without a link the file itself is replaced, which any machine can check.
	if err := downloadInto(t, named, "this week's archive", nil); err != nil || readFile(t, named) != "this week's archive" {
		t.Fatalf("a download into the file itself: %v", err)
	}

	link := filepath.Join(links, "archive.zip")
	if !symlink(t, named, link) {
		return
	}

	file, err := CreateFile(link)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer file.Discard()
	// Beside the file the link names, which is where the rename happens.
	if got := entries(t, links); len(got) != 1 {
		t.Fatalf("the link's directory holds %q while the download runs; the temporary file belongs beside the file the link names", got)
	}
	if got := entries(t, files); len(got) != 2 {
		t.Fatalf("the named file's directory holds %q while the download runs, want the file and a temporary file", got)
	}
	if _, err := file.Write([]byte("next week's archive")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := file.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("the link is no longer a link after the download (%v, %v)", info, err)
	}
	if pointsAt, err := os.Readlink(link); err != nil || pointsAt != named {
		t.Fatalf("the link points at %q (%v), want %q still", pointsAt, err, named)
	}
	if got := readFile(t, named); got != "next week's archive" {
		t.Fatalf("the file the link names holds %q, want the download", got)
	}
	if got := entries(t, files); len(got) != 1 {
		t.Fatalf("a temporary file was left beside the named file: %q", got)
	}
}

// TestADownloadIntoALinkToNothingCreatesTheFileItNames: a link to a file that
// does not exist yet is followed as os.Create follows it, relative or not, so
// the file is created where the link says and the link stays a link.
func TestADownloadIntoALinkToNothingCreatesTheFileItNames(t *testing.T) {
	t.Parallel()

	links, files := canonicalTempDir(t), canonicalTempDir(t)

	// Without a link, a new file at the path given.
	plain := filepath.Join(files, "plain.zip")
	if err := downloadInto(t, plain, "an archive", nil); err != nil || readFile(t, plain) != "an archive" {
		t.Fatalf("a download into a new file: %v", err)
	}

	named := filepath.Join(files, "first.zip")
	relative, err := filepath.Rel(links, named)
	if err != nil {
		t.Fatalf("relate %s to %s: %v", named, links, err)
	}
	link := filepath.Join(links, "archive.zip")
	if !symlink(t, relative, link) {
		return
	}

	if err := downloadInto(t, link, "the first archive", nil); err != nil {
		t.Fatalf("a download into a link to nothing: %v", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("the link was replaced (%v, %v)", info, err)
	}
	if got := readFile(t, named); got != "the first archive" {
		t.Fatalf("the file the link names holds %q, want the download", got)
	}
}

// TestADownloadKeepsThePermissionsOfTheFileItReplaces: the file put in place
// has the permissions of the one it replaces, as when os.Create wrote into it,
// and a file that was not there gets the default.
func TestADownloadKeepsThePermissionsOfTheFileItReplaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	reference := filepath.Join(dir, "reference.zip")
	created, err := os.Create(reference)
	if err != nil {
		t.Fatalf("create %s: %v", reference, err)
	}
	_ = created.Close()
	defaults := permissions(t, reference)

	existing := filepath.Join(dir, "kept.zip")
	if err := os.WriteFile(existing, []byte("last week's archive"), 0o600); err != nil {
		t.Fatalf("write %s: %v", existing, err)
	}
	if err := os.Chmod(existing, 0o640); err != nil {
		t.Fatalf("chmod %s: %v", existing, err)
	}
	want := permissions(t, existing)

	var asked []fs.FileMode
	err = downloadInto(t, existing, "this week's archive", func(file *File) {
		chmod := file.chmod
		file.chmod = func(name string, mode fs.FileMode) error {
			asked = append(asked, mode)

			return chmod(name, mode)
		}
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if got := permissions(t, existing); got != want {
		t.Fatalf("the replaced file's permissions are %v, want the %v it had", got, want)
	}
	// Asked for, on every system, whatever each keeps of it.
	if len(asked) != 1 || asked[0] != want {
		t.Fatalf("the download was given the permissions %v, want the replaced file's %v", asked, want)
	}

	fresh := filepath.Join(dir, "new.zip")
	if err := downloadInto(t, fresh, "an archive", nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if got := permissions(t, fresh); got != defaults {
		t.Fatalf("a new file has the permissions %v, want the default %v", got, defaults)
	}

	if want == defaults {
		t.Logf("this file system keeps no permission bits that tell %v from the default, so what was kept is only the same as what was asked for", want)
	}
}

// TestADownloadRefusesAFileItMayNotWrite: os.Create refused a read-only file,
// and a rename would replace one, so it is refused before anything is fetched.
// A process that may write it anyway, as root may, has it replaced, read-only
// as it was.
func TestADownloadRefusesAFileItMayNotWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "readonly.zip")
	if err := os.WriteFile(target, []byte("kept"), 0o600); err != nil {
		t.Fatalf("write %s: %v", target, err)
	}
	if err := os.Chmod(target, 0o444); err != nil {
		t.Fatalf("chmod %s: %v", target, err)
	}
	// Windows does not remove a read-only file, so the directory's cleanup
	// needs it writable again.
	t.Cleanup(func() { _ = os.Chmod(target, 0o600) })
	readOnly := permissions(t, target)

	writable := false
	if probe, err := os.OpenFile(target, os.O_WRONLY, 0); err == nil {
		_ = probe.Close()
		writable = true
	}

	err := downloadInto(t, target, "replaced", nil)
	if writable {
		t.Logf("this process may write a read-only file, so the download replaces it rather than being refused")
		if err != nil || readFile(t, target) != "replaced" || permissions(t, target) != readOnly {
			t.Fatalf("got %v, holding %q with %v; want it replaced and still %v", err, readFile(t, target), permissions(t, target), readOnly)
		}

		return
	}

	if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "cannot write "+target) {
		t.Fatalf("got %v, want the read-only file refused, named", err)
	}
	if got := readFile(t, target); got != "kept" {
		t.Fatalf("a refused download changed the file to %q", got)
	}
	if got := entries(t, dir); len(got) != 1 {
		t.Fatalf("a refused download left %q", got)
	}
}

// TestADownloadIntoADeviceWritesIntoIt: a device -- the null device on every
// system -- is written into, as os.Create wrote into it. Renamed over, it
// would be replaced by a regular file, which is what root would get.
func TestADownloadIntoADeviceWritesIntoIt(t *testing.T) {
	t.Parallel()

	file, err := CreateFile(os.DevNull)
	if err != nil {
		t.Fatalf("create %s: %v", os.DevNull, err)
	}
	defer file.Discard()

	if _, err := file.Write([]byte("discarded")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if file.Rewind() {
		t.Fatal("bytes written into a device were taken for bytes that can be taken back")
	}
	if err := file.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if info, err := os.Stat(os.DevNull); err != nil || info.Mode().IsRegular() {
		t.Fatalf("%s is no longer a device after the download (%v, %v)", os.DevNull, info, err)
	}
	directory, base := filepath.Split(os.DevNull)
	if directory == "" {
		directory = "."
	}
	for _, name := range entries(t, directory) {
		if strings.HasPrefix(name, "."+base+".") && strings.HasSuffix(name, ".part") {
			t.Fatalf("a temporary file %s was made to be renamed over %s", name, os.DevNull)
		}
	}
}
