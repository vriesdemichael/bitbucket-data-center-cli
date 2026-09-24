package fileview

import (
	"bytes"
	"fmt"
	pathpkg "path"
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
// sniffed as, and from its name where the bytes say too little.
func detectBinary(path, sniffed string, content []byte) binaryType {
	switch {
	case bytes.HasPrefix(content, oleSignature):
		return legacyOffice(path)
	case sniffed == "application/pdf":
		return binaryType{mimeType: sniffed, name: "a PDF document", reason: "This tool does not extract the text of a PDF, so it is not shown."}
	case len(content) >= 12 && string(content[4:8]) == "ftyp" && isoImageBrands[string(content[8:12])] != "":
		// A picture in the MP4 family's container, which http does not
		// sniff: a phone's HEIF photograph, an AVIF.
		mimeType := isoImageBrands[string(content[8:12])]

		return binaryType{mimeType: mimeType, name: nameOf(mimeType), reason: "Its format is not one this tool decodes, so it is not shown."}
	}

	mimeType := sniffed
	if strings.HasPrefix(mimeType, "text/") {
		// A signature did not match and the first 512 bytes looked like
		// text, but the file is not: a NUL or a byte that is not UTF-8
		// comes later.
		mimeType = "application/octet-stream"
	}

	return binaryType{mimeType: mimeType, name: nameOf(mimeType), reason: unreadable}
}

// oleSignature starts a compound file: the container Office wrote its
// documents in before 2007, and Outlook still writes messages in.
var oleSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// legacyOffice names a compound file. What it holds is not in its first bytes,
// so the extension says which application wrote it.
func legacyOffice(path string) binaryType {
	const reason = "It is in the binary format Office used before 2007, whose text this tool does not extract, so it is not shown."

	switch strings.ToLower(pathpkg.Ext(path)) {
	case ".doc", ".dot":
		return binaryType{mimeType: "application/msword", name: "a Word 97-2003 document", reason: reason}
	case ".xls", ".xlt":
		return binaryType{mimeType: "application/vnd.ms-excel", name: "an Excel 97-2003 workbook", reason: reason}
	case ".ppt", ".pot", ".pps":
		return binaryType{mimeType: "application/vnd.ms-powerpoint", name: "a PowerPoint 97-2003 presentation", reason: reason}
	case ".msg":
		return binaryType{mimeType: "application/vnd.ms-outlook", name: "an Outlook message", reason: unreadable}
	}

	return binaryType{mimeType: "application/x-ole-storage", name: "a Microsoft compound file", reason: unreadable}
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
	case "application/x-bzip2":
		return "a bzip2-compressed file"
	case "application/x-xz":
		return "an xz-compressed file"
	case "application/x-rar-compressed":
		return "a RAR archive"
	case "application/wasm":
		return "a WebAssembly module"
	case "application/vnd.ms-fontobject":
		return "a font"
	}

	// The type in brackets after the name says which format.
	family, _, _ := strings.Cut(mimeType, "/")
	switch family {
	case "font":
		return "a font"
	case "image":
		return "an image"
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
