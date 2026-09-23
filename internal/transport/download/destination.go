package download

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Destination receives a body as it arrives.
type Destination interface {
	io.Writer
	// Rewind discards everything written so far, so the body can be written
	// again from its first byte after a failure the server cannot resume from.
	// It reports false when that is impossible: bytes already on standard
	// output cannot be taken back.
	Rewind() bool
}

// Opener is a Destination that sees a response's header before the first byte
// of its body is written, and may refuse it. It is told again when the body
// starts over.
type Opener interface {
	Open(header http.Header) error
}

// Memory holds a body in memory. A request writing into one needs a Limit.
type Memory struct {
	buffer bytes.Buffer
}

func (memory *Memory) Write(payload []byte) (int, error) {
	return memory.buffer.Write(payload)
}

// Rewind empties the buffer.
func (memory *Memory) Rewind() bool {
	memory.buffer.Reset()

	return true
}

// Bytes returns the body.
func (memory *Memory) Bytes() []byte {
	return memory.buffer.Bytes()
}

// To writes a body through to writer as it arrives -- standard output,
// typically. A download that breaks off can resume onto it, since appending
// continues the same stream, but cannot start again once a byte has gone out.
//
// Not called Stream: tools/quality-report counts a call by its name wherever
// the generated client is imported, and Stream is one of its operations.
func To(writer io.Writer) Destination {
	return &stream{writer: writer}
}

type stream struct {
	writer  io.Writer
	written int64
}

func (stream *stream) Write(payload []byte) (int, error) {
	written, err := stream.writer.Write(payload)
	stream.written += int64(written)

	return written, err
}

// Rewind succeeds only while nothing has been written.
func (stream *stream) Rewind() bool {
	return stream.written == 0
}

// File downloads into a temporary file beside the file it replaces and renames
// it over that file once the download is complete. A download that fails
// leaves nothing behind, and a file already there stays as it was: writing into
// the target itself left a truncated file after a failure, and destroyed the
// old one before the first byte arrived.
//
// Nothing about what it writes to changes but its content, as when os.Create
// wrote into it. A symbolic link is followed, and the file it names replaced,
// so the link stays a link; a file that is replaced keeps its permissions; one
// this process may not write is refused, not replaced; and a device or a named
// pipe is written into, since renaming a file over it would put a regular file
// where the device was.
type File struct {
	// target is the path the caller named, and destination the file a
	// download into it replaces: target, or the file a link there names, which
	// linked says it is.
	target      string
	destination string
	linked      bool
	// temporary takes the download; for a device, it is the device itself.
	temporary *os.File
	direct    bool
	written   int64
	// mode is the permissions of the file replaced, which the download takes
	// when keepMode is set. chmod applies them.
	mode     fs.FileMode
	keepMode bool
	chmod    func(string, fs.FileMode) error
	finished bool
}

// CreateFile prepares a download into target. The temporary file is created at
// once, so a target that cannot be written fails before anything is fetched.
func CreateFile(target string) (*File, error) {
	destination, err := destinationOf(target)
	if err != nil {
		return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("cannot write %s", target), err)
	}

	file := &File{target: target, destination: destination, chmod: os.Chmod}
	if info, err := os.Lstat(target); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		file.linked = true
	}

	info, err := os.Stat(destination)
	switch {
	case err == nil && info.IsDir():
		// Found now rather than when the finished download fails to rename
		// over it.
		return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("cannot write %s: it is a directory", file.named()), nil)
	case err == nil && !info.Mode().IsRegular():
		opened, openErr := os.OpenFile(destination, os.O_WRONLY, 0)
		if openErr != nil {
			return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("cannot write %s", file.named()), openErr)
		}
		file.temporary, file.direct = opened, true

		return file, nil
	case err == nil:
		// Asked here, without truncating anything, because a rename would
		// replace a read-only file that os.Create refused to open.
		probe, openErr := os.OpenFile(destination, os.O_WRONLY, 0)
		if openErr != nil {
			return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("cannot write %s", file.named()), openErr)
		}
		_ = probe.Close()
		file.mode, file.keepMode = info.Mode().Perm(), true
	case !errors.Is(err, fs.ErrNotExist):
		return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("cannot write %s", file.named()), err)
	}

	directory, base := filepath.Split(destination)
	if directory == "" {
		directory = "."
	}

	for range 8 {
		name := filepath.Join(directory, "."+base+"."+rand.Text()[:10]+".part")

		// 0666 before the umask, as os.Create has it: this is the file the
		// user asked for, not a secret. os.CreateTemp would make it 0600, and
		// a download other users could read before would stop being one.
		// O_EXCL, because the name is new: it will not truncate, or follow a
		// link to, something that is already there.
		temporary, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666) // #nosec G302 -- the permissions os.Create uses; the umask applies
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("cannot write %s", file.named()), err)
		}
		file.temporary = temporary

		return file, nil
	}

	return nil, apperrors.New(apperrors.KindInternal, fmt.Sprintf("could not find an unused temporary name beside %s", destination), nil)
}

// destinationOf is the file a download into target replaces: target itself,
// or, when it is a symbolic link, the file the link names, followed to the end
// as os.Create follows it -- to a file that does not exist yet, too.
func destinationOf(target string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		return resolved, nil
	}

	// Nothing there, or a link to a file that is not there yet. The links are
	// followed by hand to the name the new file will have; with none, that is
	// target.
	current := target
	for range maxLinks {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			return current, nil
		}

		next, err := os.Readlink(current)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(next) {
			next = filepath.Join(filepath.Dir(current), next)
		}
		current = next
	}

	return "", fmt.Errorf("more than %d symbolic links lead from it", maxLinks)
}

// maxLinks is how many links destinationOf follows before it gives up, as
// Linux does.
const maxLinks = 40

// named is how a message names the file: as the caller did, and, when that is
// a link, as the file the link names.
func (file *File) named() string {
	if !file.linked {
		return file.target
	}

	return fmt.Sprintf("%s, the file %s links to,", file.destination, file.target)
}

func (file *File) Write(payload []byte) (int, error) {
	written, err := file.temporary.Write(payload)
	file.written += int64(written)

	return written, err
}

// Rewind empties the temporary file. What went into a device stays there, so
// one can start again only while nothing has been written.
func (file *File) Rewind() bool {
	if file.direct {
		return file.written == 0
	}
	if err := file.temporary.Truncate(0); err != nil {
		return false
	}
	_, err := file.temporary.Seek(0, io.SeekStart)
	if err == nil {
		file.written = 0
	}

	return err == nil
}

// Commit puts the downloaded file in place, over the file it replaces.
//
// The sync and the close are the last places a write that did not land can
// show itself -- a full disk, a network filesystem -- so a failure there is
// reported rather than renamed into place. Whatever happens, no temporary file
// is left behind.
func (file *File) Commit() error {
	if file.finished {
		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("the download to %s was already finished", file.target), nil)
	}
	file.finished = true

	if file.direct {
		if err := file.temporary.Close(); err != nil {
			return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to finish writing %s", file.target), err)
		}

		return nil
	}

	name := file.temporary.Name()
	syncErr := file.temporary.Sync()
	closeErr := file.temporary.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		_ = os.Remove(name)

		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to finish writing %s", file.target), err)
	}

	if file.keepMode {
		if err := file.chmod(name, file.mode); err != nil {
			_ = os.Remove(name)

			return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to give the download the permissions of %s", file.named()), err)
		}
	}

	if err := os.Rename(name, file.destination); err != nil {
		_ = os.Remove(name)

		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to put the download in place at %s", file.named()), err)
	}

	return nil
}

// Discard removes the temporary file, unless Commit has already put it in
// place. It is meant to be deferred. What went into a device stays there.
func (file *File) Discard() {
	if file == nil || file.finished {
		return
	}
	file.finished = true

	_ = file.temporary.Close()
	if !file.direct {
		_ = os.Remove(file.temporary.Name())
	}
}
