package fileview

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// wav is a WAV file of one second of silence at 8 kHz, as the format lays it
// out: a RIFF header, the format chunk, and the samples.
func wav() []byte {
	samples := make([]byte, 8000)
	file := []byte("RIFF")
	file = binary.LittleEndian.AppendUint32(file, uint32(36+len(samples)))
	file = append(file, "WAVEfmt "...)
	file = binary.LittleEndian.AppendUint32(file, 16)   // the format chunk's length
	file = binary.LittleEndian.AppendUint16(file, 1)    // PCM
	file = binary.LittleEndian.AppendUint16(file, 1)    // one channel
	file = binary.LittleEndian.AppendUint32(file, 8000) // samples a second
	file = binary.LittleEndian.AppendUint32(file, 8000) // bytes a second
	file = binary.LittleEndian.AppendUint16(file, 1)    // bytes a sample
	file = binary.LittleEndian.AppendUint16(file, 8)    // bits a sample
	file = append(file, "data"...)
	file = binary.LittleEndian.AppendUint32(file, uint32(len(samples)))

	return append(file, samples...)
}

// isoMedia is the start of an MP4-family file of the given brand: an ftyp
// box, then enough of a movie to be binary.
func isoMedia(brand string) []byte {
	return append([]byte("\x00\x00\x00\x18ftyp"+brand+"\x00\x00\x02\x00isommp41"), make([]byte, 64)...)
}

// ogg is the first page of an Ogg stream, with the codec's identification
// header where a reader looks for it, at byte 28.
func ogg(codec string) []byte {
	page := append([]byte("OggS\x00\x02"), make([]byte, 22)...)

	return append(append(page, codec...), make([]byte, 32)...)
}

func TestMediaIsRecognisedByItsBytes(t *testing.T) {
	t.Parallel()

	mpegFrame := append([]byte{0xFF, 0xFB, 0x90, 0x44}, make([]byte, 400)...)
	cases := []struct {
		path, mimeType string
		content        []byte
		kind           Kind
	}{
		{path: "beep.wav", content: wav(), mimeType: "audio/wav", kind: KindAudio},
		{path: "song.mp3", content: append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), mpegFrame...), mimeType: "audio/mpeg", kind: KindAudio},
		{path: "untagged.mp3", content: mpegFrame, mimeType: "audio/mpeg", kind: KindAudio},
		{path: "voice.m4a", content: isoMedia("M4A "), mimeType: "audio/mp4", kind: KindAudio},
		{path: "demo.mp4", content: isoMedia("isom"), mimeType: "video/mp4", kind: KindVideo},
		{path: "clip.mov", content: isoMedia("qt  "), mimeType: "video/quicktime", kind: KindVideo},
		{path: "call.3gp", content: isoMedia("3gp5"), mimeType: "video/3gpp", kind: KindVideo},
		{path: "theme.ogg", content: ogg("\x01vorbis"), mimeType: "audio/ogg", kind: KindAudio},
		{path: "intro.ogv", content: ogg("\x80theora"), mimeType: "video/ogg", kind: KindVideo},
		{path: "sample.flac", content: append([]byte("fLaC\x00\x00\x00\x22"), make([]byte, 40)...), mimeType: "audio/flac", kind: KindAudio},
		{path: "talk.webm", content: append([]byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\x82\x84webm"), make([]byte, 40)...), mimeType: "video/webm", kind: KindVideo},
		{path: "film.mkv", content: append([]byte("\x1a\x45\xdf\xa3\xa3\x42\x86\x81\x01\x42\x82\x88matroska"), make([]byte, 40)...), mimeType: "video/x-matroska", kind: KindVideo},
	}

	for _, testCase := range cases {
		view, err := Read(Request{Path: testCase.path}, testCase.content)
		if err != nil {
			t.Fatalf("Read(%s): %v", testCase.path, err)
		}
		if view.Kind != testCase.kind || view.MIMEType != testCase.mimeType {
			t.Errorf("%s came back as %s %s, want %s %s", testCase.path, view.Kind, view.MIMEType, testCase.kind, testCase.mimeType)

			continue
		}
		if view.Media == nil || !bytes.Equal(view.Media.Data, testCase.content) || view.Media.MIMEType != testCase.mimeType {
			t.Errorf("%s: the file itself did not come back beside its description", testCase.path)
		}
	}
}

// TestFramesWithoutATagNeedTheName: an MPEG frame's sync bits start too much
// else to be trusted alone, so without an .mp3 name they are not audio.
func TestFramesWithoutATagNeedTheName(t *testing.T) {
	t.Parallel()

	view, err := Read(Request{Path: "blob.bin"}, append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 400)...))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Kind != KindBinary || view.Media != nil {
		t.Errorf("sync bits without the name came back as %s", view.Kind)
	}
}

func TestShortMediaFollowsItsDescription(t *testing.T) {
	t.Parallel()

	audio, err := Read(Request{Path: "beep.wav", WebURL: fileURL}, wav())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := "beep.wav: audio (audio/wav), 7.9 KiB. It follows as audio, for a client that can play it; to one that cannot, this description is all there is."; audio.Text != want {
		t.Errorf("audio:\n got %q\nwant %q", audio.Text, want)
	}

	video, err := Read(Request{Path: "demo.mp4"}, isoMedia("isom"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(video.Text, "demo.mp4: a video (video/mp4), 88 bytes. MCP has no content for video, so it follows as an embedded resource") {
		t.Errorf("video: %q", video.Text)
	}
}

// TestMediaOverTheCapIsDescribedAlone: audio or a video cannot be made smaller
// here, so past MediaBytes the description is all that comes back, with the
// page a person can play it on.
func TestMediaOverTheCapIsDescribedAlone(t *testing.T) {
	t.Parallel()

	long := append(wav(), make([]byte, MediaBytes)...)
	audio, err := Read(Request{Path: "talk.wav", WebURL: fileURL}, long)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if audio.Kind != KindAudio || audio.Media != nil || audio.Size != int64(len(long)) {
		t.Fatalf("audio over the cap: %s, media %v, %d bytes", audio.Kind, audio.Media != nil, audio.Size)
	}
	want := "talk.wav: audio (audio/wav), 3.6 MiB. That is more than the 3.6 MiB this tool returns of audio, so only this description is returned. " +
		"A person can listen to it in Bitbucket at " + fileURL
	if audio.Text != want {
		t.Errorf("audio over the cap:\n got %q\nwant %q", audio.Text, want)
	}

	video, err := Read(Request{Path: "film.mp4", WebURL: fileURL}, append(isoMedia("isom"), make([]byte, MediaBytes)...))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if video.Kind != KindVideo || video.Media != nil || !strings.Contains(video.Text, "this tool returns of video") ||
		!strings.HasSuffix(video.Text, "A person can watch it in Bitbucket at "+fileURL) {
		t.Errorf("video over the cap: %s %q", video.Kind, video.Text)
	}
}

func TestAPictureInTheMP4ContainerIsDescribed(t *testing.T) {
	t.Parallel()

	view, err := Read(Request{Path: "photo.heic"}, isoMedia("heic"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Kind != KindBinary || view.MIMEType != "image/heic" ||
		!strings.Contains(view.Text, "photo.heic: an image (image/heic), 88 bytes. Its format is not one this tool decodes, so it is not shown.") {
		t.Errorf("a HEIF photograph: %s %s %q", view.Kind, view.MIMEType, view.Text)
	}
}
