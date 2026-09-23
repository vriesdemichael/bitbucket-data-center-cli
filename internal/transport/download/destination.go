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

// Stream writes a body through to writer as it arrives -- standard output,
// typically. A download that breaks off can resume onto it, since appending
// continues the same stream, but cannot start again once a byte has gone out.
func Stream(writer io.Writer) Destination {
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

// File downloads into a temporary file beside its target and renames it into
// place once the download is complete. A download that fails leaves nothing at
// the target, and a file already there stays as it was: writing into the target
// itself left a truncated file after a failure, and destroyed the old one before
// the first byte arrived.
type File struct {
	target    string
	temporary *os.File
	finished  bool
}

// CreateFile prepares a download into target. The temporary file is created at
// once, so a target that cannot be written fails before anything is fetched.
func CreateFile(target string) (*File, error) {
	// Found now rather than when the finished download fails to rename over it.
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("cannot write %s: it is a directory", target), nil)
	}

	directory, base := filepath.Split(target)
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
			return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("cannot write %s", target), err)
		}

		return &File{target: target, temporary: temporary}, nil
	}

	return nil, apperrors.New(apperrors.KindInternal, fmt.Sprintf("could not find an unused temporary name beside %s", target), nil)
}

func (file *File) Write(payload []byte) (int, error) {
	return file.temporary.Write(payload)
}

// Rewind empties the temporary file.
func (file *File) Rewind() bool {
	if err := file.temporary.Truncate(0); err != nil {
		return false
	}
	_, err := file.temporary.Seek(0, io.SeekStart)

	return err == nil
}

// Commit puts the downloaded file in place at its target.
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

	name := file.temporary.Name()
	syncErr := file.temporary.Sync()
	closeErr := file.temporary.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		_ = os.Remove(name)

		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to finish writing %s", file.target), err)
	}

	if err := os.Rename(name, file.target); err != nil {
		_ = os.Remove(name)

		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to put the download in place at %s", file.target), err)
	}

	return nil
}

// Discard removes the temporary file, unless Commit has already put it in
// place. It is meant to be deferred.
func (file *File) Discard() {
	if file == nil || file.finished {
		return
	}
	file.finished = true

	_ = file.temporary.Close()
	_ = os.Remove(file.temporary.Name())
}
