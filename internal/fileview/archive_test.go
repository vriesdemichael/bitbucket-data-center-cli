package fileview

import (
	"os"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport/filefixture"
)

// archiveEntries is the same small tree in every archive the tests build.
func archiveEntries() []filefixture.Entry {
	return []filefixture.Entry{
		{Name: "app/", Directory: true},
		{Name: "app/main.go", Body: []byte("package main\n")},
		{Name: "app/VERSION", Body: []byte("1")},
		{Name: "README.md", Body: []byte("# App\n\nRead me.\n")},
	}
}

func TestAZipArchiveIsListedEntryByEntry(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"dist/app.zip", "lib/app.jar"} {
		content := filefixture.Zip(archiveEntries()...)
		view, err := Read(t.Context(), Request{Path: path}, content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if view.Kind != KindArchive || view.MIMEType != "application/zip" || view.Window == nil {
			t.Fatalf("%s came back as %s %s: %q", path, view.Kind, view.MIMEType, view.Text)
		}

		want := "app/\tdirectory\napp/main.go\t13 bytes\napp/VERSION\t1 byte\nREADME.md\t16 bytes\n"
		if view.Window.Content != want {
			t.Errorf("%s listing:\n got %q\nwant %q", path, view.Window.Content, want)
		}
		header, _, _ := strings.Cut(view.Text, "\n")
		for _, fragment := range []string{
			path + ": a listing of a zip archive (" + formatSize(int64(len(content))) + "), 4 entries.",
			"Lines 1-4 follow: the whole listing.",
			"Each line is an entry: its path, a tab, and then its size or what it is.",
		} {
			if !strings.Contains(header, fragment) {
				t.Errorf("%s header does not say %q: %q", path, fragment, header)
			}
		}
	}
}

func TestATarArchiveIsListedCompressedOrNot(t *testing.T) {
	t.Parallel()

	entries := append(archiveEntries(), filefixture.Entry{Name: "latest", Link: "app/main.go"})
	tarball := filefixture.Tar(entries...)
	want := "app/\tdirectory\napp/main.go\t13 bytes\napp/VERSION\t1 byte\nREADME.md\t16 bytes\nlatest\tsymbolic link to app/main.go\n"

	cases := []struct {
		path, mimeType, name string
		content              []byte
	}{
		{path: "release.tar", mimeType: "application/x-tar", name: "a tar archive", content: tarball},
		{path: "release.tar.gz", mimeType: "application/x-gzip", name: "a gzip-compressed tar archive", content: filefixture.Gzip(tarball)},
		{path: "release.tgz", mimeType: "application/x-gzip", name: "a gzip-compressed tar archive", content: filefixture.Gzip(tarball)},
	}
	for _, testCase := range cases {
		view, err := Read(t.Context(), Request{Path: testCase.path}, testCase.content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if view.Kind != KindArchive || view.MIMEType != testCase.mimeType || view.Window == nil || view.Window.Content != want {
			t.Errorf("%s: %s %s, listing %q; want an archive %s listing %q", testCase.path, view.Kind, view.MIMEType, windowContent(view), testCase.mimeType, want)
		}
		if !strings.Contains(view.Text, "a listing of "+testCase.name) {
			t.Errorf("%s: header does not name %s: %q", testCase.path, testCase.name, view.Text)
		}
	}
}

// The bzip2 files are in testdata because nothing in Go writes bzip2. They
// were made with Python's tarfile and bz2 modules -- fixed times, owners and
// modes, compression level 9 -- from the tree archiveEntries builds, plus the
// link the test above adds.
func TestABzip2CompressedTarIsListed(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile("testdata/app.tar.bz2")
	if err != nil {
		t.Fatalf("read the archive: %v", err)
	}
	want := "app/\tdirectory\napp/main.go\t13 bytes\napp/VERSION\t1 byte\nREADME.md\t16 bytes\nlatest\tsymbolic link to app/main.go\n"

	for _, path := range []string{"release.tar.bz2", "release.tbz2", "release.tbz"} {
		view, err := Read(t.Context(), Request{Path: path}, content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if view.Kind != KindArchive || view.MIMEType != "application/x-bzip2" || windowContent(view) != want {
			t.Errorf("%s: %s %s, listing %q; want a bzip2 archive listing %q", path, view.Kind, view.MIMEType, windowContent(view), want)
		}
		if !strings.Contains(view.Text, path+": a listing of a bzip2-compressed tar archive (229 bytes), 5 entries.") {
			t.Errorf("%s: header does not name the archive: %q", path, view.Text)
		}
	}
}

// TestACompressedTarIsListedOnlyAsFarAsItsLimit: a tar has no index, so
// listing one expands all of it, and a small file can expand without end.
// Both archives hold a 2 MiB file of zeros and then a second entry, which a
// 1 MiB limit stops the listing short of.
func TestACompressedTarIsListedOnlyAsFarAsItsLimit(t *testing.T) {
	t.Parallel()

	archives := map[tarCompression][]byte{
		gzipped: filefixture.Gzip(filefixture.Tar(
			filefixture.Entry{Name: "zeros.bin", Body: make([]byte, 2<<20)},
			filefixture.Entry{Name: "after.txt", Body: []byte("after\n")},
		)),
		bzipped: readTestdata(t, "zeros.tar.bz2"),
	}

	for compression, content := range archives {
		view, ok, err := readTar(t.Context(), Request{Path: "zeros.tar"}, content, compression, 1<<20)
		if err != nil || !ok {
			t.Fatalf("compression %d: readTar = %v, %v", compression, ok, err)
		}
		if windowContent(view) != "zeros.bin\t2097152 bytes\n" ||
			!strings.Contains(view.Text, "The listing stops after 1 entry: the archive expands to more than the 1.0 MiB this tool reads of it.") {
			t.Errorf("compression %d: listing %q, text %q; want it stopped after the first entry", compression, windowContent(view), view.Text)
		}

		whole, err := Read(t.Context(), Request{Path: "zeros.tar"}, content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if windowContent(whole) != "zeros.bin\t2097152 bytes\nafter.txt\t6 bytes\n" || strings.Contains(whole.Text, "stops") {
			t.Errorf("compression %d, within the limit: listing %q, text %q", compression, windowContent(whole), whole.Text)
		}
	}
}

func TestBzip2IsRecognisedByItsStreamHeader(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"BZh91AY&SY and the block": true,
		"BZh1\x17rE8P\x90\x00\x00": true, // a stream with no blocks
		"BZh01AY&SY and the block": false,
		"BZh9 and then nothing":    false,
		"BZh9":                     false,
	}
	for content, want := range cases {
		if got := bzip2Stream([]byte(content)); got != want {
			t.Errorf("bzip2Stream(%q) = %v, want %v", content, got, want)
		}
	}
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()

	content, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	return content
}

func windowContent(view View) string {
	if view.Window == nil {
		return ""
	}

	return view.Window.Content
}

func TestAListingIsReadInWindowsLikeAnyText(t *testing.T) {
	t.Parallel()

	view, err := Read(t.Context(), Request{Path: "app.zip", StartLine: 2, LineCount: 2}, filefixture.Zip(archiveEntries()...))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	window := view.Window
	if window.StartLine != 2 || window.EndLine != 3 || window.TotalLines != 4 || window.NextStartLine != 4 ||
		window.Content != "app/main.go\t13 bytes\napp/VERSION\t1 byte\n" {
		t.Errorf("window = %+v", *window)
	}
	if !strings.Contains(view.Text, "Lines 2-3 follow; for the next, pass start_line=4.") {
		t.Errorf("header does not place the window: %q", view.Text)
	}
}

// TestAnEntryNameCannotStartALineOfItsOwn: a name is whatever the archive
// says, and a newline in one would read as another entry.
func TestAnEntryNameCannotStartALineOfItsOwn(t *testing.T) {
	t.Parallel()

	content := filefixture.Tar(filefixture.Entry{Name: "evil\nREADME.md\t99 bytes", Body: []byte("x")})
	view, err := Read(t.Context(), Request{Path: "odd.tar"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Window.TotalLines != 1 || view.Window.Content != "evil?README.md?99 bytes\t1 byte\n" {
		t.Errorf("listing = %q", view.Window.Content)
	}
}

func TestWhatLooksLikeAnArchiveButIsNotOneIsDescribed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path, says string
		content    []byte
		kind       Kind
	}{
		// A zip's local header, with the NULs its version and flags carry, and
		// then nothing a zip reader can use.
		{path: "broken.zip", content: []byte("PK\x03\x04\x14\x00\x00\x00 and then no central directory"), kind: KindBinary, says: "a zip archive (application/zip)"},
		{path: "notes.txt.gz", content: filefixture.Gzip([]byte(strings.Repeat("plain text, not a tar\n", 50))), kind: KindBinary, says: "a gzip-compressed file (application/x-gzip)"},
		{path: "empty.zip", content: filefixture.Zip(), kind: KindArchive, says: "empty.zip: a zip archive (22 bytes) with no entries."},
		{path: "notes.txt.bz2", content: readTestdata(t, "notes.txt.bz2"), kind: KindBinary, says: "a bzip2-compressed file (application/x-bzip2)"},
		// xz stays described: reading it would take a dependency.
		{path: "release.tar.xz", content: append([]byte("\xFD7zXZ\x00\x00\x04"), make([]byte, 60)...), kind: KindBinary, says: "an xz-compressed file (application/x-xz)"},
	}
	for _, testCase := range cases {
		view, err := Read(t.Context(), Request{Path: testCase.path}, testCase.content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if view.Kind != testCase.kind || !strings.Contains(view.Text, testCase.says) {
			t.Errorf("%s: %s %q, want %s saying %q", testCase.path, view.Kind, view.Text, testCase.kind, testCase.says)
		}
	}
}
