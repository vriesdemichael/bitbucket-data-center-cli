package fileview

import (
	"strings"
	"testing"
)

// opaqueBinary is bytes no converter reads: a NUL early, bytes that are not
// UTF-8, and no signature.
func opaqueBinary() []byte {
	content := []byte{'\n', 0x00, 0xFF, 0xFE, 0x01, 0x02}
	for index := range 5000 {
		content = append(content, byte(index*37+index/256))
	}

	return content
}

const fileURL = "https://bitbucket.example.com/projects/PROJ/repos/app/browse/assets/blob.bin?at=refs%2Fheads%2Fmain"

// TestABinaryFileIsDescribedNotShown is the reason the tool changed: the bytes
// of a binary file used to come back as corrupted text.
func TestABinaryFileIsDescribedNotShown(t *testing.T) {
	t.Parallel()

	content := opaqueBinary()
	view, err := Read(t.Context(), Request{Path: "assets/blob.bin", At: "refs/heads/main", WebURL: fileURL, StartLine: 3}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if view.Kind != KindBinary || view.MIMEType != "application/octet-stream" || view.Size != int64(len(content)) {
		t.Fatalf("view = %s %s %d bytes, want binary application/octet-stream %d bytes", view.Kind, view.MIMEType, view.Size, len(content))
	}
	if view.Window != nil {
		t.Errorf("a binary file came back with a window of lines: %+v", view.Window)
	}

	want := "assets/blob.bin at refs/heads/main: a binary file (application/octet-stream), 4.9 KiB. " +
		"Its bytes are not shown: they are not text, and not a kind of file this tool converts. " +
		"A person can open it in Bitbucket at " + fileURL
	if view.Text != want {
		t.Errorf("description:\n got %q\nwant %q", view.Text, want)
	}
}

func TestADescriptionNamesTheTypeTheBytesShow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		content  []byte
		mimeType string
		name     string
	}{
		{content: []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n\x00"), mimeType: "application/pdf", name: "a PDF document"},
		{content: []byte("wOFF\x00\x01\x00\x00"), mimeType: "font/woff", name: "a font"},
		{content: []byte("\x00asm\x01\x00\x00\x00"), mimeType: "application/wasm", name: "a WebAssembly module"},
		{content: []byte("Rar!\x1a\x07\x01\x00\x00"), mimeType: "application/x-rar-compressed", name: "a RAR archive"},
	}

	for _, testCase := range cases {
		view, err := Read(t.Context(), Request{Path: "file"}, testCase.content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if view.Kind != KindBinary || view.MIMEType != testCase.mimeType || !strings.Contains(view.Text, testCase.name+" ("+testCase.mimeType+")") {
			t.Errorf("%q: %s %s %q, want binary %s named %q", testCase.content, view.Kind, view.MIMEType, view.Text, testCase.mimeType, testCase.name)
		}
	}
}

func TestAFileTooLargeToReadIsDescribed(t *testing.T) {
	t.Parallel()

	declared := TooLarge(Request{Path: "big.log", WebURL: fileURL}, MaxFileBytes, 120<<20)
	if declared.Kind != KindTooLarge || declared.Size != 120<<20 || declared.Window != nil {
		t.Fatalf("view = %+v", declared)
	}
	if want := "big.log: 120.0 MiB, larger than the 64.0 MiB this tool reads, so it was not read. A person can open it in Bitbucket at " + fileURL; declared.Text != want {
		t.Errorf("description:\n got %q\nwant %q", declared.Text, want)
	}

	undeclared := TooLarge(Request{Path: "big.log"}, MaxFileBytes, -1)
	if undeclared.Size != -1 || undeclared.Text != "big.log: larger than the 64.0 MiB this tool reads, so it was not read." {
		t.Errorf("a file of unknown size: %d, %q", undeclared.Size, undeclared.Text)
	}
}

func TestFormatSize(t *testing.T) {
	t.Parallel()

	cases := map[int64]string{
		0:           "0 bytes",
		1:           "1 byte",
		1023:        "1023 bytes",
		1024:        "1.0 KiB",
		5000:        "4.9 KiB",
		64 << 20:    "64.0 MiB",
		3 << 30:     "3.0 GiB",
		5 << 40:     "5.0 TiB",
		3_750_000:   "3.6 MiB",
		120_000_000: "114.4 MiB",
	}
	for bytes, want := range cases {
		if got := formatSize(bytes); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", bytes, got, want)
		}
	}
}
