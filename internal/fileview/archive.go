package fileview

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// readArchive views a zip or tar file, when content is one: a Word, PowerPoint
// or Excel file as its text, and any other archive as a listing of its entries.
// It reports false for a file that is neither, for the caller to go on.
func readArchive(request Request, sniffed string, content []byte) (View, bool, error) {
	switch {
	case bytes.HasPrefix(content, []byte("PK\x03\x04")), bytes.HasPrefix(content, []byte("PK\x05\x06")):
		view, err := readZip(request, content)

		return view, true, err
	case sniffed == "application/x-gzip":
		return readTar(request, content, gzipped, archiveExpandBytes)
	case sniffed == "application/x-bzip2":
		return readTar(request, content, bzipped, archiveExpandBytes)
	case isTar(request.Path, content):
		return readTar(request, content, uncompressed, archiveExpandBytes)
	}

	return View{}, false, nil
}

// tarCompression is what a tar is compressed with, if anything. xz is not
// among them: the standard library has no reader for it, and it is described.
type tarCompression int

const (
	uncompressed tarCompression = iota
	gzipped
	bzipped
)

// isTar reports a tar archive: by the magic a POSIX or GNU tar header carries,
// or, for the older kind that has none, by its name.
func isTar(path string, content []byte) bool {
	const magic = 257
	if len(content) >= magic+5 && string(content[magic:magic+5]) == "ustar" {
		return true
	}

	return strings.HasSuffix(strings.ToLower(path), ".tar")
}

// readZip views a zip archive: the text of an Office document, which is a zip
// of XML, or a listing of anything else -- a jar, a war, a plain zip.
func readZip(request Request, content []byte) (View, error) {
	size := int64(len(content))

	archive, err := zip.NewReader(bytes.NewReader(content), size)
	if err != nil {
		return describeBinary(subjectOf(request), request.WebURL, binaryType{
			mimeType: "application/zip",
			name:     "a zip archive",
			reason:   "It cannot be read as one, so it is not shown.",
		}, size), nil
	}

	if document, family, mainPart, ok := openOffice(archive); ok {
		return readOffice(request, size, document, family, mainPart)
	}

	var listing strings.Builder
	for index, file := range archive.File {
		if index == ArchiveEntries {
			break
		}
		listing.WriteString(zipEntry(file) + "\n")
	}

	more := ""
	if len(archive.File) > ArchiveEntries {
		more = fmt.Sprintf("The listing stops at %d entries, of the %d in the archive.", ArchiveEntries, len(archive.File))
	}

	return listArchive(request, "application/zip", "a zip archive", size, listing.String(), more)
}

func zipEntry(file *zip.File) string {
	name := entryName(file.Name)
	mode := file.Mode()

	switch {
	case mode.IsDir():
		return name + "\tdirectory"
	case mode&fs.ModeSymlink != 0:
		return name + "\tsymbolic link"
	}

	line := name + "\t" + byteCount(int64(min(file.UncompressedSize64, 1<<62)))
	const encrypted = 0x1
	if file.Flags&encrypted != 0 {
		line += ", encrypted"
	}

	return line
}

// errExpanded stops a read that has expanded past its limit.
var errExpanded = errors.New("the archive expands to more than is read")

// expansion reads a decompressed stream until it has given its limit.
type expansion struct {
	reader io.Reader
	left   int64
}

func (stream *expansion) Read(buffer []byte) (int, error) {
	if stream.left <= 0 {
		return 0, errExpanded
	}
	if int64(len(buffer)) > stream.left {
		buffer = buffer[:stream.left]
	}
	count, err := stream.reader.Read(buffer)
	stream.left -= int64(count)

	return count, err
}

// readTar views a tar archive, compressed or not, as a listing of its entries.
// A tar has no index, so this reads all of it, and a compressed one only as
// far as expanding it to limit bytes -- archiveExpandBytes, which a test
// makes smaller. It reports false when the content turns out not to be a tar
// at all, as a compressed file of another kind is not.
func readTar(request Request, content []byte, compression tarCompression, limit int64) (View, bool, error) {
	var source io.Reader = bytes.NewReader(content)
	mimeType, name := "application/x-tar", "a tar archive"
	switch compression {
	case gzipped:
		decompressed, err := gzip.NewReader(source)
		if err != nil {
			return View{}, false, nil
		}
		source = &expansion{reader: decompressed, left: limit}
		mimeType, name = "application/x-gzip", "a gzip-compressed tar archive"
	case bzipped:
		// A bzip2 stream is checked as it is read, so one that is not
		// valid fails at the first entry, as a gzip one fails here.
		source = &expansion{reader: bzip2.NewReader(source), left: limit}
		mimeType, name = "application/x-bzip2", "a bzip2-compressed tar archive"
	}

	reader := tar.NewReader(source)
	var listing strings.Builder
	count := 0
	more := ""
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			switch {
			case count == 0:
				return View{}, false, nil
			case errors.Is(err, errExpanded):
				more = fmt.Sprintf("The listing stops after %s: the archive expands to more than the %s this tool reads of it.",
					plural(count, "entry"), formatSize(limit))
			default:
				more = fmt.Sprintf("The listing stops after %s, where the archive cannot be read any further.", plural(count, "entry"))
			}

			break
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if count == ArchiveEntries {
			more = fmt.Sprintf("The listing stops at %d entries; the archive has more.", ArchiveEntries)

			break
		}
		listing.WriteString(tarEntry(header) + "\n")
		count++
	}

	view, err := listArchive(request, mimeType, name, int64(len(content)), listing.String(), more)

	return view, true, err
}

func tarEntry(header *tar.Header) string {
	name := entryName(header.Name)
	mode := header.FileInfo().Mode()

	switch {
	case mode.IsDir():
		return name + "\tdirectory"
	case header.Typeflag == tar.TypeSymlink:
		return name + "\tsymbolic link to " + entryName(header.Linkname)
	case header.Typeflag == tar.TypeLink:
		return name + "\thard link to " + entryName(header.Linkname)
	case mode.IsRegular():
		return name + "\t" + byteCount(header.Size)
	}

	return name + "\tspecial file"
}

// entryName keeps a name an archive gives on its own line: a control character
// in it -- a newline, a tab -- would start a line or a column that is not there.
func entryName(name string) string {
	return strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7F {
			return '?'
		}

		return character
	}, name)
}

// byteCount is an entry's exact size.
func byteCount(bytes int64) string {
	if bytes == 1 {
		return "1 byte"
	}

	return fmt.Sprintf("%d bytes", bytes)
}

// listArchive views an archive's listing as a window of lines.
func listArchive(request Request, mimeType, name string, size int64, listing, more string) (View, error) {
	return lines(request, KindArchive, mimeType, size, linedText{
		what:   "a listing of " + name + " (" + formatSize(size) + ")",
		unit:   "entry",
		layout: "Each line is an entry: its path, a tab, and then its size or what it is.",
		noun:   "listing",
		empty:  name + " (" + formatSize(size) + ") with no entries.",
		note:   more,
	}, listing)
}
