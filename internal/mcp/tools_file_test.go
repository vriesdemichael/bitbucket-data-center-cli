package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/fileview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport/filefixture"
)

func TestFileWebURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, base, project, repo, path, at, want string
	}{
		{
			name: "a ref names the revision", base: "https://bb.example.com", project: "PROJ", repo: "app", path: "src/main.go", at: "refs/heads/main",
			want: "https://bb.example.com/projects/PROJ/repos/app/browse/src/main.go?at=refs%2Fheads%2Fmain",
		},
		{
			name: "no ref is the default branch", base: "https://bb.example.com/", project: "PROJ", repo: "app", path: "README.md",
			want: "https://bb.example.com/projects/PROJ/repos/app/browse/README.md",
		},
		{
			name: "a context path is kept", base: "https://example.com/bitbucket", project: "~ALICE", repo: "notes", path: "a b/c#d.txt", at: "feature/x y",
			want: "https://example.com/bitbucket/projects/~ALICE/repos/notes/browse/a%20b/c%23d.txt?at=feature%2Fx+y",
		},
		{
			name: "the path is trimmed as the fetch trims it", base: "https://bb.example.com", project: "PROJ", repo: "app", path: "/./docs//guide.md",
			want: "https://bb.example.com/projects/PROJ/repos/app/browse/docs/guide.md",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := fileWebURL(testCase.base, testCase.project, testCase.repo, testCase.path, testCase.at); got != testCase.want {
				t.Errorf("fileWebURL = %q\n          want %q", got, testCase.want)
			}
		})
	}
}

// TestGetFileContentRefusesANegativeWindowBeforeFetching: no file makes a
// negative line number mean anything, so the refusal comes before a request.
// The server fails the test if one arrives.
func TestGetFileContentRefusesANegativeWindowBeforeFetching(t *testing.T) {
	t.Parallel()

	clients := newUnreachedClients(t)

	for _, window := range []map[string]any{{"start_line": -1}, {"line_count": -3}} {
		args := map[string]any{"project": "TEST", "repo": "demo", "path": "src/main.go"}
		for key, value := range window {
			args[key] = value
		}

		result := callTool(t, specGetFileContent(), clients, args)
		if !result.IsError {
			t.Fatalf("%v was accepted: %+v", window, result)
		}
	}
}

// TestGetFileContentAdvertisesItsWindowLimits holds the input schema to the
// limits fileview enforces, which is why the descriptions are built from them.
func TestGetFileContentAdvertisesItsWindowLimits(t *testing.T) {
	t.Parallel()

	descriptions := schemaDescriptions(specGetFileContent().Tool.InputSchema)
	lineCount := descriptions["line_count"]
	for _, want := range []string{
		fmt.Sprintf("default %d", fileview.DefaultLineCount),
		fmt.Sprintf("at most %d", fileview.MaxLineCount),
		fmt.Sprintf("%d KiB", fileview.WindowBytes>>10),
	} {
		if !strings.Contains(lineCount, want) {
			t.Errorf("line_count's description does not say %q: %q", want, lineCount)
		}
	}
	if !strings.Contains(descriptions["start_line"], "next_start_line") {
		t.Errorf("start_line's description does not say where its value comes from: %q", descriptions["start_line"])
	}
	if description := specGetFileContent().Tool.Description; !strings.Contains(description, fmt.Sprintf("%d MiB", fileview.MaxFileBytes>>20)) {
		t.Errorf("the tool's description does not give the most it reads: %q", description)
	}
}

// fileViews are one view of every kind the tool answers with, built from real
// content by the converters that produce them.
func fileViews(t *testing.T) map[string]fileview.View {
	t.Helper()

	read := func(request fileview.Request, content []byte) fileview.View {
		t.Helper()

		view, err := fileview.Read(t.Context(), request, content)
		if err != nil {
			t.Fatalf("fileview.Read(%s): %v", request.Path, err)
		}

		return view
	}

	return map[string]fileview.View{
		"text window":     read(fileview.Request{Path: "a.txt", LineCount: 1}, []byte("one\ntwo\n")),
		"empty text":      read(fileview.Request{Path: "empty.txt"}, nil),
		"image":           read(fileview.Request{Path: "small.png"}, pngOf(t, 40, 30)),
		"scaled image":    read(fileview.Request{Path: "wide.png"}, pngOf(t, 3000, 1000)),
		"turned image":    read(fileview.Request{Path: "photo.jpg"}, sidewaysJPEG(t)),
		"converted image": read(fileview.Request{Path: "diagram.bmp"}, filefixture.BMP(filefixture.Quadrants(40, 30))),
		"paged image": read(fileview.Request{Path: "scan.tif"},
			filefixture.TIFF(6, filefixture.Oriented(filefixture.Quadrants(40, 30), 6), filefixture.Quadrants(8, 8))),
		"document":                 read(fileview.Request{Path: "plan.docx"}, filefixture.Word(filefixture.WordParagraph("A plan."))),
		"empty document":           read(fileview.Request{Path: "blank.docx"}, filefixture.Word("")),
		"archive":                  read(fileview.Request{Path: "app.zip"}, filefixture.Zip(filefixture.Entry{Name: "a.txt", Body: []byte("a")})),
		"audio":                    read(fileview.Request{Path: "beep.wav"}, shortAudio),
		"audio over the cap":       read(fileview.Request{Path: "talk.wav"}, append(shortAudio, make([]byte, fileview.MediaBytes)...)),
		"video":                    read(fileview.Request{Path: "demo.mp4"}, shortVideo),
		"binary":                   read(fileview.Request{Path: "blob.bin"}, []byte{0, 1, 2, 0xFF, 0xFE}),
		"too large, size declared": fileview.TooLarge(fileview.Request{Path: "big.log"}, fileview.MaxFileBytes, 100<<20),
		"too large, size unknown":  fileview.TooLarge(fileview.Request{Path: "big.log"}, fileview.MaxFileBytes, -1),
	}
}

// shortAudio is the start of a WAV file, which is all a type is read from, and
// shortVideo the start of an MP4.
var (
	shortAudio = append([]byte("RIFF\x24\x10\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00"), make([]byte, 64)...)
	shortVideo = append([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp41"), make([]byte, 64)...)
)

// TestShortMediaComesBackAsItselfBesideItsDescription reads the media content
// as a client receives it: audio as audio content, and a video -- which MCP
// has no content for -- as an embedded resource addressed by the file's page.
func TestShortMediaComesBackAsItselfBesideItsDescription(t *testing.T) {
	t.Parallel()

	const page = "https://bb.example.com/projects/PROJ/repos/app/browse/demo.mp4?at=main"
	views := fileViews(t)

	decode := func(t *testing.T, content mcp.Content, target any) {
		t.Helper()

		wire, err := json.Marshal(content)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := json.Unmarshal(wire, target); err != nil {
			t.Fatalf("decode %s: %v", wire, err)
		}
	}

	result, structured := fileContentResult(GetFileContentInput{Path: "beep.wav"}, page, views["audio"])
	if len(result.Content) != 2 || structured.Kind != "audio" || structured.MediaReturned == nil || !*structured.MediaReturned {
		t.Fatalf("audio came back as %d blocks, kind %s, returned %v", len(result.Content), structured.Kind, structured.MediaReturned)
	}
	var audio struct {
		Type     string `json:"type"`
		MIMEType string `json:"mimeType"`
		Data     []byte `json:"data"`
	}
	decode(t, result.Content[1], &audio)
	if audio.Type != "audio" || audio.MIMEType != "audio/wav" || !bytes.Equal(audio.Data, shortAudio) {
		t.Errorf("the audio block is %q %q of %d bytes, want the file as audio/wav", audio.Type, audio.MIMEType, len(audio.Data))
	}

	result, structured = fileContentResult(GetFileContentInput{Path: "demo.mp4"}, page, views["video"])
	if len(result.Content) != 2 || structured.Kind != "video" || structured.MediaReturned == nil || !*structured.MediaReturned {
		t.Fatalf("video came back as %d blocks, kind %s, returned %v", len(result.Content), structured.Kind, structured.MediaReturned)
	}
	var video struct {
		Type     string `json:"type"`
		Resource struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Blob     []byte `json:"blob"`
		} `json:"resource"`
	}
	decode(t, result.Content[1], &video)
	if video.Type != "resource" || video.Resource.URI != page || video.Resource.MIMEType != "video/mp4" || !bytes.Equal(video.Resource.Blob, shortVideo) {
		t.Errorf("the video block is %q at %q, %q of %d bytes; want the file as a video/mp4 resource at its page",
			video.Type, video.Resource.URI, video.Resource.MIMEType, len(video.Resource.Blob))
	}

	result, structured = fileContentResult(GetFileContentInput{Path: "talk.wav"}, page, views["audio over the cap"])
	if len(result.Content) != 1 || structured.MediaReturned == nil || *structured.MediaReturned {
		t.Errorf("audio over the cap came back as %d blocks, returned %v; want the description alone", len(result.Content), structured.MediaReturned)
	}
}

// pngOf is a PNG of width by height pixels, encoded by the standard library.
func pngOf(t *testing.T, width, height int) []byte {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for index := range picture.Pix {
		picture.Pix[index] = byte(index / 4 % 7 * 30)
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}

	return encoded.Bytes()
}

// sidewaysJPEG is a 40 by 30 photograph stored a quarter turn round, with the
// orientation that says to turn it back, as a phone stores one.
func sidewaysJPEG(t *testing.T) []byte {
	t.Helper()

	upright := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for index := range upright.Pix {
		upright.Pix[index] = byte(index / 4 % 11 * 20)
	}

	return filefixture.WithSegment(filefixture.JPEG(filefixture.Oriented(upright, 6), 90), filefixture.ExifBlock(6, false))
}

// TestATurnedImageSaysSoInItsStructuredAnswer: the sizes a client reads are
// the upright picture's, as the image it receives is.
func TestATurnedImageSaysSoInItsStructuredAnswer(t *testing.T) {
	t.Parallel()

	result, structured := fileContentResult(GetFileContentInput{Path: "photo.jpg"}, "", fileViews(t)["turned image"])

	facts := structured.Image
	if facts == nil || !facts.Turned || facts.Scaled || facts.Width != 40 || facts.Height != 30 ||
		facts.ReturnedWidth != 40 || facts.ReturnedHeight != 30 || facts.ReturnedMIMEType != "image/jpeg" {
		encoded, _ := json.Marshal(structured)
		t.Fatalf("a photograph stored sideways: %s", encoded)
	}
	image, ok := result.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("the second block is %T, want the image", result.Content[1])
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(image.Data))
	if err != nil || config.Width != 40 || config.Height != 30 {
		t.Errorf("the image returned is %dx%d (%v), want it upright at 40x30", config.Width, config.Height, err)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "turned upright from its EXIF orientation") {
		t.Errorf("the text does not say the image was turned: %q", text)
	}
}

// TestAConvertedImageSaysWhatItWas: a BMP or a TIFF comes back as a PNG, and
// a TIFF's first page comes back with how many there are.
func TestAConvertedImageSaysWhatItWas(t *testing.T) {
	t.Parallel()

	views := fileViews(t)

	_, bitmap := fileContentResult(GetFileContentInput{Path: "diagram.bmp"}, "", views["converted image"])
	if bitmap.MIMEType != "image/bmp" || bitmap.Image == nil || bitmap.Image.ReturnedMIMEType != "image/png" || bitmap.Image.Pages != 0 {
		encoded, _ := json.Marshal(bitmap)
		t.Errorf("a BMP: %s", encoded)
	}

	_, scan := fileContentResult(GetFileContentInput{Path: "scan.tif"}, "", views["paged image"])
	if scan.MIMEType != "image/tiff" || scan.Image == nil || scan.Image.Pages != 2 || !scan.Image.Turned ||
		scan.Image.Width != 40 || scan.Image.Height != 30 || scan.Image.ReturnedMIMEType != "image/png" {
		encoded, _ := json.Marshal(scan)
		t.Errorf("a two-page TIFF stored sideways: %s", encoded)
	}
}

// TestAnImageComesBackAsAnImageBesideItsDescription reads the image content
// as a client receives it: base64 in JSON, which has to decode to the image.
func TestAnImageComesBackAsAnImageBesideItsDescription(t *testing.T) {
	t.Parallel()

	view := fileViews(t)["scaled image"]
	result, structured := fileContentResult(GetFileContentInput{Path: "wide.png"}, "", view)

	if len(result.Content) != 2 {
		t.Fatalf("an image came back as %d content blocks, want its description and the image", len(result.Content))
	}
	if text, ok := result.Content[0].(*mcp.TextContent); !ok || !strings.Contains(text.Text, "may no longer be legible") {
		t.Errorf("the first block is not the description with its scaling warning: %#v", result.Content[0])
	}

	wire, err := json.Marshal(result.Content[1])
	if err != nil {
		t.Fatalf("marshal the image content: %v", err)
	}
	var block struct {
		Type     string `json:"type"`
		MIMEType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	if err := json.Unmarshal(wire, &block); err != nil {
		t.Fatalf("decode the image content %s: %v", wire, err)
	}
	data, err := base64.StdEncoding.DecodeString(block.Data)
	if err != nil {
		t.Fatalf("the image's data is not base64: %v", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || block.Type != "image" || block.MIMEType != "image/png" {
		t.Fatalf("the image content is %q %q and decodes to %v, want an image/png", block.Type, block.MIMEType, err)
	}

	facts := structured.Image
	if facts == nil || structured.Kind != "image" || !facts.Scaled || facts.Width != 3000 || facts.Height != 1000 ||
		facts.ReturnedWidth != config.Width || facts.ReturnedHeight != config.Height || facts.ReturnedSize != len(data) ||
		facts.ReturnedMIMEType != "image/png" || config.Width != fileview.ImageEdge {
		encoded, _ := json.Marshal(structured)
		t.Errorf("the structured answer does not describe the %dx%d image returned: %s", config.Width, config.Height, encoded)
	}
	if structured.Content != nil || structured.StartLine != nil {
		t.Errorf("an image came back with lines")
	}
}

// TestEveryFileViewFitsTheOutputSchema: the SDK validates a result against the
// published output schema before it leaves the server and fails the call when
// it does not fit. Every kind is checked here against the schema a client
// receives, so a field shape that only one kind produces cannot turn a read
// into an error.
func TestEveryFileViewFitsTheOutputSchema(t *testing.T) {
	t.Parallel()

	session := connect(t, Clients{}, []string{"get_file_content"}, nil, true)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 1 {
		t.Fatalf("tools/list: %v (%d tools)", err, len(listed.Tools))
	}
	published, err := json.Marshal(listed.Tools[0].OutputSchema)
	if err != nil {
		t.Fatalf("marshal the output schema: %v", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(published, &schema); err != nil {
		t.Fatalf("decode the output schema: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve the output schema: %v", err)
	}

	in := GetFileContentInput{Project: "PROJ", Repo: "app", Path: "a.txt", At: "main"}
	for name, view := range fileViews(t) {
		result, structured := fileContentResult(in, "https://bb.example.com/projects/PROJ/repos/app/browse/a.txt?at=main", view)

		encoded, err := json.Marshal(structured)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var decoded any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if err := resolved.Validate(decoded); err != nil {
			t.Errorf("%s: %s does not fit the output schema: %v", name, encoded, err)
		}

		if len(result.Content) == 0 {
			t.Errorf("%s: no content for the model to read", name)
		}
		if text, ok := result.Content[0].(*mcp.TextContent); !ok || text.Text != view.Text {
			t.Errorf("%s: the first content is not the view's text: %#v", name, result.Content[0])
		}
	}
}

// TestATextViewCarriesItsWindowAndABinaryOneDoesNot pins which fields each kind
// fills: a model reads their presence as meaning.
func TestATextViewCarriesItsWindowAndABinaryOneDoesNot(t *testing.T) {
	t.Parallel()

	views := fileViews(t)
	in := GetFileContentInput{Path: "a.txt"}

	_, text := fileContentResult(in, "", views["text window"])
	if text.Kind != "text" || text.Content == nil || *text.Content != "one\n" ||
		text.StartLine == nil || *text.StartLine != 1 || text.EndLine == nil || *text.EndLine != 1 ||
		text.TotalLines == nil || *text.TotalLines != 2 || text.NextStartLine == nil || *text.NextStartLine != 2 ||
		text.Size == nil || *text.Size != 8 || !strings.HasPrefix(text.MIMEType, "text/plain") {
		encoded, _ := json.Marshal(text)
		t.Errorf("text window: %s", encoded)
	}

	_, empty := fileContentResult(in, "", views["empty text"])
	if empty.Content == nil || *empty.Content != "" || empty.TotalLines == nil || *empty.TotalLines != 0 || empty.NextStartLine != nil {
		encoded, _ := json.Marshal(empty)
		t.Errorf("an empty file has to say it has no lines, not leave the fields out: %s", encoded)
	}

	_, binary := fileContentResult(in, "", views["binary"])
	if binary.Kind != "binary" || binary.Content != nil || binary.StartLine != nil || binary.TotalLines != nil ||
		binary.Size == nil || *binary.Size != 5 || binary.MIMEType != "application/octet-stream" {
		encoded, _ := json.Marshal(binary)
		t.Errorf("binary: %s", encoded)
	}

	_, unsized := fileContentResult(in, "", views["too large, size unknown"])
	if unsized.Kind != "too_large" || unsized.Size != nil || unsized.MIMEType != "" {
		encoded, _ := json.Marshal(unsized)
		t.Errorf("too large, size unknown: %s", encoded)
	}
}
