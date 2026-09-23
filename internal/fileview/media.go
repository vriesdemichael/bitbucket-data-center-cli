package fileview

import (
	"bytes"
	"fmt"
	pathpkg "path"
	"strings"
)

// Media is audio or video returned as it is.
type Media struct {
	Data     []byte
	MIMEType string
}

// readMedia views audio or a video when content is one: the file itself when
// it is within MediaBytes, beside a description, and the description alone
// when it is not. It reports false for anything else.
//
// MCP has audio content and no video content, so a video goes back as an
// embedded resource carrying its bytes and type -- which the caller builds,
// since it knows the file's page, the resource's address.
func readMedia(request Request, sniffed string, content []byte) (View, bool) {
	mimeType, video, ok := mediaType(request.Path, sniffed, content)
	if !ok {
		return View{}, false
	}

	kind, name, verb := KindAudio, "audio", "listen to"
	if video {
		kind, name, verb = KindVideo, "a video", "watch"
	}
	size := int64(len(content))

	var text strings.Builder
	fmt.Fprintf(&text, "%s: %s (%s), %s.", subjectOf(request), name, mimeType, formatSize(size))

	if len(content) > MediaBytes {
		fmt.Fprintf(&text, " That is more than the %s this tool returns of %s, so only this description is returned.",
			formatSize(MediaBytes), strings.TrimPrefix(name, "a "))
		if request.WebURL != "" {
			fmt.Fprintf(&text, " A person can %s it in Bitbucket at %s", verb, request.WebURL)
		}

		return View{Kind: kind, MIMEType: mimeType, Size: size, Text: text.String()}, true
	}

	if video {
		text.WriteString(" MCP has no content for video, so it follows as an embedded resource with the video's type, " +
			"for a client that can play it; to one that cannot, this description is all there is.")
	} else {
		text.WriteString(" It follows as audio, for a client that can play it; to one that cannot, this description is all there is.")
	}

	return View{Kind: kind, MIMEType: mimeType, Size: size, Text: text.String(), Media: &Media{Data: content, MIMEType: mimeType}}, true
}

// mediaType reads what audio or video content is. http.DetectContentType knows
// the common containers by their first bytes; what it does not -- which brand
// of MP4, which codec in an Ogg, an MP3 without a tag -- is read here, with the
// extension deciding only where the bytes cannot.
func mediaType(path, sniffed string, content []byte) (mimeType string, video, ok bool) {
	extension := strings.ToLower(pathpkg.Ext(path))

	if len(content) >= 12 && string(content[4:8]) == "ftyp" {
		return isoMediaType(string(content[8:12]), extension)
	}

	switch {
	case sniffed == "audio/wave":
		return "audio/wav", false, true
	case strings.HasPrefix(sniffed, "audio/"):
		return sniffed, false, true
	case sniffed == "application/ogg":
		return oggType(content, extension)
	case sniffed == "video/webm":
		if bytes.Contains(content[:min(len(content), 64)], []byte("matroska")) {
			return "video/x-matroska", true, true
		}
		if extension == ".weba" {
			return "audio/webm", false, true
		}

		return "video/webm", true, true
	case strings.HasPrefix(sniffed, "video/"):
		return sniffed, true, true
	case bytes.HasPrefix(content, []byte("fLaC")):
		return "audio/flac", false, true
	case bytes.HasPrefix(content, []byte("FLV\x01")):
		return "video/x-flv", true, true
	case len(content) >= 2 && content[0] == 0xFF && content[1]&0xE0 == 0xE0:
		// An MPEG audio frame's sync bits, which an MP3 without a tag
		// starts with -- and which too much else can start with to trust
		// without the name.
		switch {
		case extension == ".mp3":
			return "audio/mpeg", false, true
		case extension == ".aac" && content[1]&0xF6 == 0xF0:
			return "audio/aac", false, true
		}
	}

	return "", false, false
}

// isoMediaType reads an MP4-family file's major brand: the same container
// holds audio, video and pictures.
func isoMediaType(brand, extension string) (string, bool, bool) {
	switch {
	case brand == "M4A " || brand == "M4B " || brand == "M4P " || brand == "F4A " || brand == "F4B ":
		return "audio/mp4", false, true
	case brand == "qt  ":
		return "video/quicktime", true, true
	case strings.HasPrefix(brand, "3g2"):
		return "video/3gpp2", true, true
	case strings.HasPrefix(brand, "3gp"):
		return "video/3gpp", true, true
	case isoImageBrands[brand] != "":
		return "", false, false
	case extension == ".m4a" || extension == ".m4b" || extension == ".aac":
		return "audio/mp4", false, true
	}

	return "video/mp4", true, true
}

// isoImageBrands are the brands of the MP4 family that hold pictures: HEIF,
// which phones take photographs in, and AVIF. Neither is decoded here.
var isoImageBrands = map[string]string{
	"heic": "image/heic", "heix": "image/heic", "hevc": "image/heic-sequence", "hevx": "image/heic-sequence",
	"mif1": "image/heif", "msf1": "image/heif-sequence", "avif": "image/avif", "avis": "image/avif",
}

// oggType reads which codec an Ogg file holds from its first page, where the
// codec's identification header starts at byte 28.
func oggType(content []byte, extension string) (string, bool, bool) {
	const header = 28
	codec := content[min(len(content), header):]

	switch {
	case bytes.HasPrefix(codec, []byte("\x80theora")):
		return "video/ogg", true, true
	case bytes.HasPrefix(codec, []byte("OpusHead")), bytes.HasPrefix(codec, []byte("\x01vorbis")),
		bytes.HasPrefix(codec, []byte("\x7fFLAC")), bytes.HasPrefix(codec, []byte("Speex")):
		return "audio/ogg", false, true
	case extension == ".ogv":
		return "video/ogg", true, true
	}

	return "audio/ogg", false, true
}
