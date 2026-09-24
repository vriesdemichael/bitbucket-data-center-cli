//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // the scaled image may come back as a JPEG
	"image/png"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport/filefixture"
)

// liveFileAnswer is get_file_content's structured answer.
type liveFileAnswer struct {
	Path          string  `json:"path"`
	At            string  `json:"at"`
	Kind          string  `json:"kind"`
	MIMEType      string  `json:"mime_type"`
	Size          *int64  `json:"size"`
	WebURL        string  `json:"web_url"`
	Content       *string `json:"content"`
	StartLine     *int    `json:"start_line"`
	EndLine       *int    `json:"end_line"`
	TotalLines    *int    `json:"total_lines"`
	NextStartLine *int    `json:"next_start_line"`
	Image         *struct {
		Width            int    `json:"width"`
		Height           int    `json:"height"`
		Turned           bool   `json:"turned"`
		Scaled           bool   `json:"scaled"`
		ReturnedWidth    int    `json:"returned_width"`
		ReturnedHeight   int    `json:"returned_height"`
		ReturnedMIMEType string `json:"returned_mime_type"`
		ReturnedSize     int    `json:"returned_size"`
		Pages            int    `json:"pages"`
	} `json:"image"`
	MediaReturned *bool `json:"media_returned"`
}

// liveLargePNG is a 3000 by 2000 PNG over the 3,750,000 bytes an image is
// returned in: stripes, and a block of noise no encoder compresses, so it is
// over both limits at once.
func liveLargePNG(t *testing.T) []byte {
	t.Helper()

	picture := image.NewNRGBA(image.Rect(0, 0, 3000, 2000))
	state := uint32(2463534242)
	for y := range 2000 {
		for x := range 3000 {
			offset := picture.PixOffset(x, y)
			if x < 1400 && y < 1000 {
				for channel := range 3 {
					state ^= state << 13
					state ^= state >> 17
					state ^= state << 5
					picture.Pix[offset+channel] = byte(state)
				}
			} else {
				picture.Pix[offset], picture.Pix[offset+1], picture.Pix[offset+2] = byte(x/16%2*220), byte(y/16%2*220), 90
			}
			picture.Pix[offset+3] = 255
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatalf("encode the large PNG: %v", err)
	}
	if encoded.Len() <= 3_750_000 {
		t.Fatalf("the large PNG is %d bytes, which does not test the byte budget", encoded.Len())
	}

	return encoded.Bytes()
}

// liveNumberedLines is count lines, each naming its own number, so a window
// that holds the wrong lines cannot hold the right text.
func liveNumberedLines(count int) []byte {
	var text bytes.Buffer
	for number := 1; number <= count; number++ {
		fmt.Fprintf(&text, "line %04d: pushed to Bitbucket and read back through the MCP server\n", number)
	}

	return text.Bytes()
}

// TestLiveMCPGetFileContentReadsEachKindOfFile covers get_file_content against
// files Bitbucket is storing, one of each kind the tool converts.
//
// What comes back depends on the bytes the raw endpoint answers with, so no
// unit test can hold it: the converters are tested on content built in their
// tests, and this is where the bytes are Bitbucket's. Every file is pushed in
// one commit and read at that ref, and each answer is checked value by value.
func TestLiveMCPGetFileContentReadsEachKindOfFile(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const at = "refs/heads/master"
	long := liveNumberedLines(3000)
	binary := liveBinaryFile()
	largePNG := liveLargePNG(t)

	// Each document's order is not its parts' names: the slide and the sheet
	// that come first are in slide2.xml and sheet2.xml.
	word := filefixture.Word(filefixture.WordParagraph("Release plan") + filefixture.WordParagraph("Ship on Friday.") +
		filefixture.WordTable([]string{"Owner", "Task"}, []string{"Ada", "Tag the release"}))
	slides := filefixture.PowerPoint(
		filefixture.Slide{Part: "slide2.xml", Texts: []string{"Welcome"}},
		filefixture.Slide{Part: "slide1.xml", Texts: []string{"Numbers"}, Notes: []string{"Pause here."}},
	)
	workbook := filefixture.Excel(
		filefixture.Workbook{SharedStrings: []string{filefixture.SharedString("Item"), filefixture.SharedString("Cost")}, Styles: []int{0, 14}},
		filefixture.Sheet{Name: "Summary", Part: "sheet2.xml", Rows: `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>` +
			`<row r="2"><c r="A2" t="inlineStr"><is><t>Rent</t></is></c><c r="B2"><f>1000+200</f><v>1200</v></c></row>`},
		filefixture.Sheet{Name: "Dates", Part: "sheet1.xml", Rows: `<row r="1"><c r="A1" s="1"><v>45292</v></c></row>`},
	)
	archive := filefixture.Zip(
		filefixture.Entry{Name: "app/", Directory: true},
		filefixture.Entry{Name: "app/main.go", Body: []byte("package main\n")},
		filefixture.Entry{Name: "README.md", Body: []byte("# App\n")},
	)
	// A photograph as a phone stores one held upright: a quarter turn round,
	// with orientation 6 in its Exif block to turn it back. Upright it is
	// 2000 by 3000, over the edge an image is returned at, so it is scaled
	// too, after it is turned.
	sideways := filefixture.WithSegment(filefixture.JPEG(filefixture.Oriented(filefixture.Quadrants(2000, 3000), 6), 90),
		filefixture.ExifBlock(6, false))
	// Formats model APIs refuse: a bitmap, and a two-page TIFF stored a
	// quarter turn round, with its own orientation tag to turn it back.
	bitmap := filefixture.BMP(filefixture.Quadrants(120, 80))
	scan := filefixture.TIFF(8, filefixture.Oriented(filefixture.Quadrants(300, 200), 8), filefixture.Quadrants(50, 50))
	// A WAV header and a second of 8 kHz silence: small enough to come back
	// as audio.
	audio := append([]byte("RIFF\x64\x1f\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x40\x1f\x00\x00\x40\x1f\x00\x00\x01\x00\x08\x00data\x40\x1f\x00\x00"),
		bytes.Repeat([]byte{0x80}, 8000)...)

	if err := harness.pushFilesOnBranch(seeded.Key, repo.Slug, "master", map[string][]byte{
		"docs/long.txt":       long,
		"assets/blob.bin":     binary,
		"images/large.png":    largePNG,
		"docs/plan.docx":      word,
		"docs/talk.pptx":      slides,
		"docs/budget.xlsx":    workbook,
		"dist/app.zip":        archive,
		"sounds/beep.wav":     audio,
		"images/sideways.jpg": sideways,
		"images/diagram.bmp":  bitmap,
		"images/scan.tif":     scan,
	}); err != nil {
		t.Fatalf("push the files: %v", err)
	}

	webURL := func(path string) string {
		return fmt.Sprintf("%s/projects/%s/repos/%s/browse/%s?at=%s",
			strings.TrimRight(harness.config.BitbucketURL, "/"), seeded.Key, repo.Slug, path, url.QueryEscape(at))
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		read := func(t *testing.T, path string, window map[string]any) (*mcp.CallToolResult, liveFileAnswer) {
			t.Helper()

			args := map[string]any{"project": seeded.Key, "repo": repo.Slug, "path": path, "at": at}
			for key, value := range window {
				args[key] = value
			}

			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_file_content", Arguments: args})
			if err != nil {
				t.Fatalf("get_file_content %s: protocol error: %v", path, err)
			}
			if result.IsError {
				t.Fatalf("get_file_content %s: error result: %s", path, mcpResultText(result))
			}

			encoded, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatalf("get_file_content %s: marshal structuredContent: %v", path, err)
			}
			var answer liveFileAnswer
			if err := json.Unmarshal(encoded, &answer); err != nil {
				t.Fatalf("get_file_content %s: decode structuredContent %s: %v", path, encoded, err)
			}
			if answer.Path != path || answer.At != at || answer.WebURL != webURL(path) {
				t.Errorf("get_file_content %s answered for %q at %q, page %q; want %q", path, answer.Path, answer.At, answer.WebURL, webURL(path))
			}

			return result, answer
		}

		t.Run("a window from the middle of a long text file", func(t *testing.T) {
			result, answer := read(t, "docs/long.txt", map[string]any{"start_line": 1200, "line_count": 100})

			if answer.Kind != "text" || answer.Size == nil || *answer.Size != int64(len(long)) {
				t.Fatalf("docs/long.txt came back as %q of %v bytes, want text of %d", answer.Kind, answer.Size, len(long))
			}
			if answer.StartLine == nil || *answer.StartLine != 1200 || answer.EndLine == nil || *answer.EndLine != 1299 ||
				answer.TotalLines == nil || *answer.TotalLines != 3000 || answer.NextStartLine == nil || *answer.NextStartLine != 1300 {
				t.Fatalf("window = %v-%v of %v, next %v; want 1200-1299 of 3000, next 1300",
					deref(answer.StartLine), deref(answer.EndLine), deref(answer.TotalLines), deref(answer.NextStartLine))
			}

			lines := strings.SplitAfter(string(long), "\n")
			if want := strings.Join(lines[1199:1299], ""); answer.Content == nil || *answer.Content != want {
				t.Errorf("content is not lines 1200-1299 of the file as pushed")
			}

			var numbered strings.Builder
			for index := 1199; index < 1299; index++ {
				fmt.Fprintf(&numbered, "%6d\t%s", index+1, lines[index])
			}
			want := "docs/long.txt at " + at + ": text, 3000 lines. Lines 1200-1299 follow; for the next, pass start_line=1300.\n" + numbered.String()
			if got := mcpResultText(result); got != want {
				t.Errorf("the text a model reads is not the header and lines 1200-1299 numbered:\n%.400s", got)
			}
		})

		t.Run("a large PNG comes back scaled within the budget, with a warning", func(t *testing.T) {
			result, answer := read(t, "images/large.png", nil)

			if answer.Kind != "image" || answer.MIMEType != "image/png" || answer.Size == nil || *answer.Size != int64(len(largePNG)) {
				t.Fatalf("images/large.png came back as %q %q of %v bytes, want an image/png of %d",
					answer.Kind, answer.MIMEType, deref64(answer.Size), len(largePNG))
			}
			if len(result.Content) != 2 {
				t.Fatalf("an image came back as %d content blocks, want its description and the image", len(result.Content))
			}

			returned, ok := result.Content[1].(*mcp.ImageContent)
			if !ok {
				t.Fatalf("the second block is %T, want the image", result.Content[1])
			}
			decoded, format, err := image.Decode(bytes.NewReader(returned.Data))
			if err != nil {
				t.Fatalf("the image returned does not decode: %v", err)
			}
			width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
			if len(returned.Data) > 3_750_000 || max(width, height) > 2048 || returned.MIMEType != "image/"+format {
				t.Errorf("the image returned is a %dx%d %s of %d bytes labelled %s, want one within 2048 pixels and 3,750,000 bytes",
					width, height, format, len(returned.Data), returned.MIMEType)
			}

			facts := answer.Image
			if facts == nil || !facts.Scaled || facts.Width != 3000 || facts.Height != 2000 || facts.ReturnedWidth != width ||
				facts.ReturnedHeight != height || facts.ReturnedSize != len(returned.Data) || facts.ReturnedMIMEType != returned.MIMEType {
				t.Errorf("the structured answer does not describe the %dx%d image returned: %+v", width, height, facts)
			}

			text := mcpResultText(result)
			for _, want := range []string{
				"images/large.png at " + at + ": a PNG image, 3000x2000 pixels",
				fmt.Sprintf("It follows scaled down to %dx%d pixels", width, height),
				"Small text in it may no longer be legible because of the scaling.",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("the description does not say %q: %q", want, text)
				}
			}
		})

		// The text each document's content holds, extracted in order; the
		// header says what it came from and what it leaves out.
		for _, document := range []struct {
			path, mimeType, from, text string
			size                       int
		}{
			{
				path: "docs/plan.docx", mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
				from: "a Word document", size: len(word),
				text: "Release plan\nShip on Friday.\nOwner\tTask\nAda\tTag the release\n",
			},
			{
				path: "docs/talk.pptx", mimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
				from: "a PowerPoint presentation", size: len(slides),
				text: "Slide 1\nWelcome\n\nSlide 2\nNumbers\nSpeaker notes:\nPause here.\n",
			},
			{
				path: "docs/budget.xlsx", mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
				from: "an Excel workbook", size: len(workbook),
				text: "Sheet 1: Summary\nItem\tCost\nRent\t1200\n\nSheet 2: Dates\n2024-01-01\n",
			},
		} {
			t.Run("the text of "+document.path+" in order", func(t *testing.T) {
				result, answer := read(t, document.path, nil)

				if answer.Kind != "document" || answer.MIMEType != document.mimeType || answer.Size == nil || *answer.Size != int64(document.size) {
					t.Fatalf("%s came back as %q %q of %v bytes, want a document %s of %d",
						document.path, answer.Kind, answer.MIMEType, deref64(answer.Size), document.mimeType, document.size)
				}
				if got := derefText(answer.Content); got != document.text {
					t.Errorf("the text extracted from %s:\n got %q\nwant %q", document.path, got, document.text)
				}

				header, _, _ := strings.Cut(mcpResultText(result), "\n")
				for _, want := range []string{
					document.path + " at " + at + ": text extracted from " + document.from,
					"Formatting, pictures and embedded objects are not included.",
				} {
					if !strings.Contains(header, want) {
						t.Errorf("the header does not say %q: %q", want, header)
					}
				}
			})
		}

		t.Run("a zip archive lists its entries", func(t *testing.T) {
			result, answer := read(t, "dist/app.zip", nil)

			if answer.Kind != "archive" || answer.MIMEType != "application/zip" || answer.TotalLines == nil || *answer.TotalLines != 3 {
				t.Fatalf("dist/app.zip came back as %q %q with %v lines, want an archive of 3 entries", answer.Kind, answer.MIMEType, deref(answer.TotalLines))
			}
			if got, want := derefText(answer.Content), "app/\tdirectory\napp/main.go\t13 bytes\nREADME.md\t6 bytes\n"; got != want {
				t.Errorf("the listing is not the entries pushed:\n got %q\nwant %q", got, want)
			}
			if header, _, _ := strings.Cut(mcpResultText(result), "\n"); !strings.Contains(header, "a listing of a zip archive") {
				t.Errorf("the header does not say it is a listing: %q", header)
			}
		})

		t.Run("a sideways photograph comes back upright and scaled, and says so", func(t *testing.T) {
			result, answer := read(t, "images/sideways.jpg", nil)

			if answer.Kind != "image" || answer.MIMEType != "image/jpeg" || len(result.Content) != 2 {
				t.Fatalf("images/sideways.jpg came back as %q %q in %d blocks, want a JPEG image beside its description",
					answer.Kind, answer.MIMEType, len(result.Content))
			}
			returned, ok := result.Content[1].(*mcp.ImageContent)
			if !ok {
				t.Fatalf("the second block is %T, want the image", result.Content[1])
			}
			decoded, _, err := image.Decode(bytes.NewReader(returned.Data))
			if err != nil {
				t.Fatalf("the image returned does not decode: %v", err)
			}

			// Upright is portrait, and the quadrants are in their corners:
			// left sideways, the picture would be landscape, and turned the
			// wrong way, its colours would be in the wrong corners.
			if bounds := decoded.Bounds(); bounds.Dx() != 1365 || bounds.Dy() != 2048 {
				t.Errorf("the image returned is %dx%d, want it upright and scaled to 1365x2048", bounds.Dx(), bounds.Dy())
			}
			if err := filefixture.CheckQuadrants(decoded, 40); err != nil {
				t.Errorf("the image returned is not upright: %v", err)
			}

			facts := answer.Image
			if facts == nil || !facts.Turned || !facts.Scaled || facts.Width != 2000 || facts.Height != 3000 ||
				facts.ReturnedWidth != 1365 || facts.ReturnedHeight != 2048 {
				t.Errorf("the structured answer does not describe the upright image: %+v", facts)
			}

			text := mcpResultText(result)
			for _, want := range []string{
				"images/sideways.jpg at " + at + ": a JPEG image, 2000x3000 pixels, ",
				"It follows turned upright from its EXIF orientation and scaled down to 1365x2048 pixels, as a JPEG of ",
				"Small text in it may no longer be legible because of the scaling.",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("the description does not say %q: %q", want, text)
				}
			}
		})

		// A BMP and a TIFF come back as PNGs, since clients do not take
		// either, and the PNG is lossless: every corner is exactly its colour.
		for _, converted := range []struct {
			path, from, says string
			pages            int
		}{
			{path: "images/diagram.bmp", from: "BMP", says: "images/diagram.bmp at " + at + ": a BMP image, 120x80 pixels, "},
			{path: "images/scan.tif", from: "TIFF", pages: 2, says: "images/scan.tif at " + at + ": a TIFF image of 2 pages, 300x200 pixels, "},
		} {
			t.Run(converted.path+" comes back as a PNG", func(t *testing.T) {
				result, answer := read(t, converted.path, nil)

				if answer.Kind != "image" || len(result.Content) != 2 {
					t.Fatalf("%s came back as %q in %d blocks, want an image beside its description", converted.path, answer.Kind, len(result.Content))
				}
				returned, ok := result.Content[1].(*mcp.ImageContent)
				if !ok || returned.MIMEType != "image/png" {
					t.Fatalf("the second block is %T, want a PNG image", result.Content[1])
				}
				decoded, err := png.Decode(bytes.NewReader(returned.Data))
				if err != nil {
					t.Fatalf("the PNG returned does not decode: %v", err)
				}
				if err := filefixture.CheckQuadrants(decoded, 0); err != nil {
					t.Errorf("the image returned is not the picture, upright: %v", err)
				}

				facts := answer.Image
				if facts == nil || facts.ReturnedMIMEType != "image/png" || facts.Pages != converted.pages || facts.Turned != (converted.pages > 0) {
					t.Errorf("the structured answer does not describe the converted image: %+v", facts)
				}

				text := mcpResultText(result)
				wants := []string{converted.says, ", converted from " + converted.from + ", which clients do not take."}
				if converted.pages > 0 {
					wants = append(wants, " Its first page follows turned upright from its TIFF orientation, as a PNG of ")
				}
				for _, want := range wants {
					if !strings.Contains(text, want) {
						t.Errorf("the description does not say %q: %q", want, text)
					}
				}
			})
		}

		t.Run("short audio comes back as audio beside its description", func(t *testing.T) {
			result, answer := read(t, "sounds/beep.wav", nil)

			if answer.Kind != "audio" || answer.MIMEType != "audio/wav" || answer.MediaReturned == nil || !*answer.MediaReturned {
				t.Fatalf("sounds/beep.wav came back as %q %q, returned %v; want audio/wav returned", answer.Kind, answer.MIMEType, answer.MediaReturned)
			}
			if len(result.Content) != 2 {
				t.Fatalf("audio came back as %d content blocks, want its description and the audio", len(result.Content))
			}
			returned, ok := result.Content[1].(*mcp.AudioContent)
			if !ok || returned.MIMEType != "audio/wav" || !bytes.Equal(returned.Data, audio) {
				t.Errorf("the second block is %T, want the %d bytes pushed as audio/wav", result.Content[1], len(audio))
			}
		})

		t.Run("an opaque binary is described by its type and size", func(t *testing.T) {
			result, answer := read(t, "assets/blob.bin", nil)

			if answer.Kind != "binary" || answer.MIMEType != "application/octet-stream" || answer.Size == nil || *answer.Size != int64(len(binary)) {
				t.Fatalf("assets/blob.bin came back as %q %q of %v bytes, want binary application/octet-stream of %d",
					answer.Kind, answer.MIMEType, deref64(answer.Size), len(binary))
			}
			if answer.Content != nil || answer.StartLine != nil {
				t.Errorf("a binary file came back with lines")
			}

			if len(result.Content) != 1 {
				t.Fatalf("a binary file came back as %d content blocks, want its description alone", len(result.Content))
			}
			text := mcpResultText(result)
			want := "assets/blob.bin at " + at + ": a binary file (application/octet-stream), 97.7 KiB. " +
				"Its bytes are not shown: they are not text, and not a kind of file this tool converts. " +
				"A person can open it in Bitbucket at " + webURL("assets/blob.bin")
			if text != want {
				t.Errorf("description:\n got %q\nwant %q", text, want)
			}
		})
	}, "ai", "mcp", "serve")
}

func deref(value *int) any {
	if value == nil {
		return nil
	}

	return *value
}

// derefText is a text field that may be absent, as an empty string when it is.
func derefText(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func deref64(value *int64) any {
	if value == nil {
		return nil
	}

	return *value
}
