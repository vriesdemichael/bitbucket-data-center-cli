package fileview

import (
	"fmt"
	"strings"
)

// binaryType is what a file whose bytes are not shown was found to be.
type binaryType struct {
	mimeType string
	// name is what it is called in a sentence: "a PDF document".
	name string
	// reason says why its bytes are not shown.
	reason string
}

// detectBinary names a file that is not text from the type its bytes were
// sniffed as.
func detectBinary(_ string, sniffed string) binaryType {
	mimeType := sniffed
	if strings.HasPrefix(mimeType, "text/") {
		// A signature did not match and the first 512 bytes looked like
		// text, but the file is not: a NUL or a byte that is not UTF-8
		// comes later.
		mimeType = "application/octet-stream"
	}

	return binaryType{mimeType: mimeType, name: nameOf(mimeType), reason: unreadable}
}

// unreadable is why the bytes of a file of no kind this package converts are
// not shown.
const unreadable = "Its bytes are not shown: they are not text, and not a kind of file this tool converts."

// nameOf is what a type is called in a sentence.
func nameOf(mimeType string) string {
	switch mimeType {
	case "application/pdf":
		return "a PDF document"
	case "application/postscript":
		return "a PostScript document"
	case "application/zip":
		return "a zip archive"
	case "application/x-gzip":
		return "a gzip-compressed file"
	case "application/x-rar-compressed":
		return "a RAR archive"
	case "application/wasm":
		return "a WebAssembly module"
	case "application/vnd.ms-fontobject":
		return "a font"
	}

	family, format, _ := strings.Cut(mimeType, "/")
	switch family {
	case "font":
		return "a font"
	case "image":
		return "an image (" + strings.TrimPrefix(format, "x-") + ")"
	case "audio":
		return "audio"
	case "video":
		return "a video"
	}

	return "a binary file"
}

// describeBinary views a file whose bytes are not shown: what it is, how
// large, and where a person can open it.
func describeBinary(subject, webURL string, found binaryType, size int64) View {
	text := fmt.Sprintf("%s: %s (%s), %s. %s%s", subject, found.name, found.mimeType, formatSize(size), found.reason, personCanOpen(webURL))

	return View{Kind: KindBinary, MIMEType: found.mimeType, Size: size, Text: text}
}

// personCanOpen points a person, not the model, at the file in Bitbucket. A
// model with nothing but this conversation cannot follow a link, and is never
// sent to one; the person it is talking to can.
func personCanOpen(webURL string) string {
	if webURL == "" {
		return ""
	}

	return " A person can open it in Bitbucket at " + webURL
}

// formatSize renders a byte count for a sentence, as the downloader's messages
// do: bytes below a KiB, one decimal above.
func formatSize(bytes int64) string {
	const unit = 1024
	switch {
	case bytes == 1:
		return "1 byte"
	case bytes < unit:
		return fmt.Sprintf("%d bytes", bytes)
	}

	value := float64(bytes) / unit
	for _, suffix := range []string{"KiB", "MiB", "GiB"} {
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
		value /= unit
	}

	return fmt.Sprintf("%.1f TiB", value)
}
