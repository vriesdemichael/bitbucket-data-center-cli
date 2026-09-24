package fileview

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"
)

// stripes is an image with edges in it, which compresses well as a PNG, like
// the screenshots and diagrams most PNGs are.
func stripes(width, height int) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.SetRGBA(x, y, color.RGBA{R: uint8(x / 8 % 2 * 200), G: uint8(y / 8 % 2 * 200), B: 80, A: 255})
		}
	}

	return picture
}

// noise is an image no encoder compresses, for what happens over the byte
// budget. alpha below 255 makes every pixel partly transparent.
func noise(width, height int, alpha uint8) *image.NRGBA {
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	state := uint32(2463534242)
	for index := range picture.Pix {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		picture.Pix[index] = byte(state)
		if index%4 == 3 {
			picture.Pix[index] = alpha
		}
	}

	return picture
}

func encodePNG(t *testing.T, picture image.Image) []byte {
	t.Helper()

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}

	return encoded.Bytes()
}

func encodeJPEG(t *testing.T, picture image.Image) []byte {
	t.Helper()

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}

	return encoded.Bytes()
}

// returnedImage decodes what a view returned, and fails when it does not
// decode as the type it claims to be.
func returnedImage(t *testing.T, view View) image.Image {
	t.Helper()

	if view.Kind != KindImage || view.Image == nil {
		t.Fatalf("view is %s with image %v, want an image: %q", view.Kind, view.Image, view.Text)
	}

	picture, format, err := image.Decode(bytes.NewReader(view.Image.Data))
	if err != nil {
		t.Fatalf("the image returned does not decode: %v", err)
	}
	if "image/"+format != view.Image.MIMEType {
		t.Fatalf("the image returned is a %s labelled %s", format, view.Image.MIMEType)
	}
	if bounds := picture.Bounds(); bounds.Dx() != view.Image.ReturnedWidth || bounds.Dy() != view.Image.ReturnedHeight {
		t.Fatalf("the image returned is %dx%d, labelled %dx%d", bounds.Dx(), bounds.Dy(), view.Image.ReturnedWidth, view.Image.ReturnedHeight)
	}

	return picture
}

func TestAnImageThatFitsIsReturnedAsItIs(t *testing.T) {
	t.Parallel()

	content := encodePNG(t, stripes(64, 48))
	view, err := Read(Request{Path: "docs/diagram.png"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	returnedImage(t, view)
	if !bytes.Equal(view.Image.Data, content) || view.Image.Scaled || view.MIMEType != "image/png" {
		t.Errorf("an image that fits was not returned byte for byte: scaled %v, %s", view.Image.Scaled, view.MIMEType)
	}
	if want := "docs/diagram.png: a PNG image, 64x48 pixels, " + formatSize(int64(len(content))) + ". It follows as an image."; view.Text != want {
		t.Errorf("text:\n got %q\nwant %q", view.Text, want)
	}
}

// TestAnImageLongerThanTheEdgeIsScaledDownWithAWarning is the case the owner
// asked for by name: the model is told the picture was scaled, from what to
// what, and that small text may not have survived it.
func TestAnImageLongerThanTheEdgeIsScaledDownWithAWarning(t *testing.T) {
	t.Parallel()

	content := encodePNG(t, stripes(3000, 1000))
	view, err := Read(Request{Path: "docs/wide.png"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	returnedImage(t, view)
	image := view.Image
	if !image.Scaled || image.Width != 3000 || image.Height != 1000 || image.ReturnedWidth != ImageEdge || image.ReturnedHeight != 683 {
		t.Fatalf("image = %+v, want 3000x1000 scaled to %dx683", *image, ImageEdge)
	}
	if image.MIMEType != "image/png" || len(image.Data) > ImageBytes {
		t.Errorf("a PNG came back as a %s of %d bytes, want a PNG within %d", image.MIMEType, len(image.Data), ImageBytes)
	}
	for _, want := range []string{
		"a PNG image, 3000x1000 pixels",
		"It follows scaled down to 2048x683 pixels, as a PNG of",
		"Small text in it may no longer be legible because of the scaling.",
	} {
		if !strings.Contains(view.Text, want) {
			t.Errorf("text does not say %q: %q", want, view.Text)
		}
	}
}

func TestAPhotographScaledDownStaysAJPEG(t *testing.T) {
	t.Parallel()

	content := encodeJPEG(t, stripes(2500, 1000))
	view, err := Read(Request{Path: "photo.jpg"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	returnedImage(t, view)
	if view.Image.MIMEType != "image/jpeg" || view.Image.ReturnedWidth != ImageEdge || view.Image.ReturnedHeight != 819 {
		t.Errorf("a JPEG came back as a %dx%d %s, want a %dx819 JPEG", view.Image.ReturnedWidth, view.Image.ReturnedHeight, view.Image.MIMEType, ImageEdge)
	}
}

// TestAnImageOverTheByteBudgetShrinksUntilItFits runs with small limits, so a
// picture the test can build is over them.
func TestAnImageOverTheByteBudgetShrinksUntilItFits(t *testing.T) {
	t.Parallel()

	limits := imageLimits{bytes: 20_000, edge: ImageEdge, pixels: ImagePixels}

	t.Run("an opaque PNG becomes a JPEG before it shrinks", func(t *testing.T) {
		t.Parallel()

		content := encodePNG(t, noise(200, 200, 255))
		view := readImage(Request{Path: "noise.png"}, "image/png", content, limits)

		returnedImage(t, view)
		if view.Image.MIMEType != "image/jpeg" || len(view.Image.Data) > limits.bytes {
			t.Errorf("came back as a %s of %d bytes, want a JPEG within %d", view.Image.MIMEType, len(view.Image.Data), limits.bytes)
		}
		if !view.Image.Scaled || view.Image.ReturnedWidth >= 200 || !strings.Contains(view.Text, "may no longer be legible") {
			t.Errorf("an image over the byte budget was not scaled down with a warning: %+v %q", *view.Image, view.Text)
		}
	})

	t.Run("a transparent PNG stays a PNG and shrinks", func(t *testing.T) {
		t.Parallel()

		content := encodePNG(t, noise(200, 200, 128))
		view := readImage(Request{Path: "noise.png"}, "image/png", content, limits)

		returnedImage(t, view)
		if view.Image.MIMEType != "image/png" || len(view.Image.Data) > limits.bytes || !view.Image.Scaled {
			t.Errorf("came back as a %s of %d bytes, scaled %v; want a scaled PNG within %d",
				view.Image.MIMEType, len(view.Image.Data), view.Image.Scaled, limits.bytes)
		}
	})

	t.Run("a JPEG encoded again can fit without shrinking", func(t *testing.T) {
		t.Parallel()

		content := encodeJPEG(t, stripes(300, 300))
		view := readImage(Request{Path: "flat.jpg"}, "image/jpeg", content, imageLimits{bytes: len(content) - 1, edge: ImageEdge, pixels: ImagePixels})

		returnedImage(t, view)
		if view.Image.Scaled || view.Image.ReturnedWidth != 300 || len(view.Image.Data) >= len(content) {
			t.Errorf("image = %dx%d scaled %v, %d bytes", view.Image.ReturnedWidth, view.Image.ReturnedHeight, view.Image.Scaled, len(view.Image.Data))
		}
		if strings.Contains(view.Text, "legible") || !strings.Contains(view.Text, "encoded again to fit") {
			t.Errorf("text %q, want it to say the image was encoded again and nothing about legibility", view.Text)
		}
	})
}

func TestAnAnimatedGIFGivesItsFirstFrameAndSaysSo(t *testing.T) {
	t.Parallel()

	animation := &gif.GIF{}
	for _, fill := range []uint8{2, 3, 4} {
		frame := image.NewPaletted(image.Rect(0, 0, 40, 30), palette.Plan9)
		for index := range frame.Pix {
			frame.Pix[index] = fill
		}
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, 10)
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatalf("encode GIF: %v", err)
	}

	view, err := Read(Request{Path: "spinner.gif"}, encoded.Bytes())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	picture := returnedImage(t, view)
	if view.Image.Frames != 3 || view.Image.MIMEType != "image/png" || view.Image.Scaled {
		t.Errorf("image = %+v, want the first of 3 frames as an unscaled PNG", *view.Image)
	}
	if got, want := color.NRGBAModel.Convert(picture.At(20, 15)), color.NRGBAModel.Convert(palette.Plan9[2]); got != want {
		t.Errorf("the frame returned is %v at its centre, want the first frame's %v", got, want)
	}
	for _, want := range []string{"an animated GIF image of 3 frames, 40x30 pixels", "Its first frame follows, as a PNG of"} {
		if !strings.Contains(view.Text, want) {
			t.Errorf("text does not say %q: %q", want, view.Text)
		}
	}

	still := &gif.GIF{Image: animation.Image[:1], Delay: animation.Delay[:1]}
	encoded.Reset()
	if err := gif.EncodeAll(&encoded, still); err != nil {
		t.Fatalf("encode GIF: %v", err)
	}
	view, err = Read(Request{Path: "still.gif"}, encoded.Bytes())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	returnedImage(t, view)
	if view.Image.Frames != 0 || view.Image.MIMEType != "image/gif" || !bytes.Equal(view.Image.Data, encoded.Bytes()) {
		t.Errorf("a still GIF was not returned as it is: %+v", *view.Image)
	}
}

// The two WebP files are from golang.org/x/image's own test data: nothing in
// Go encodes a WebP, so a test cannot build one.
func TestAWebPIsReturnedAsItIsOrDecodedToScaleIt(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		file, scaledType string
	}{
		{file: "lossless.webp", scaledType: "image/png"},
		{file: "lossy.webp", scaledType: "image/jpeg"},
	} {
		content, err := os.ReadFile("testdata/" + testCase.file)
		if err != nil {
			t.Fatalf("read %s: %v", testCase.file, err)
		}

		view, err := Read(Request{Path: testCase.file}, content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		returnedImage(t, view)
		if view.Image.MIMEType != "image/webp" || !bytes.Equal(view.Image.Data, content) {
			t.Errorf("%s was not returned as it is: %s", testCase.file, view.Image.MIMEType)
		}

		scaled := readImage(Request{Path: testCase.file}, "image/webp", content, imageLimits{bytes: ImageBytes, edge: 20, pixels: ImagePixels})
		returnedImage(t, scaled)
		if !scaled.Image.Scaled || max(scaled.Image.ReturnedWidth, scaled.Image.ReturnedHeight) != 20 || scaled.Image.MIMEType != testCase.scaledType {
			t.Errorf("%s scaled to an edge of 20 came back as a %dx%d %s, want a %s",
				testCase.file, scaled.Image.ReturnedWidth, scaled.Image.ReturnedHeight, scaled.Image.MIMEType, testCase.scaledType)
		}
	}
}

// pngHeader is a PNG that ends after its header: it says how large it is and
// has no pixels.
func pngHeader(width, height uint32) []byte {
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[0:], width)
	binary.BigEndian.PutUint32(header[4:], height)
	header[8], header[9] = 8, 6 // eight bits a channel, RGBA

	chunk := append([]byte("IHDR"), header...)
	content := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0d")
	content = append(content, chunk...)

	return binary.BigEndian.AppendUint32(content, crc32.ChecksumIEEE(chunk))
}

func TestAnImageThatCannotBeReturnedIsDescribed(t *testing.T) {
	t.Parallel()

	// A VP8X header with the animation flag set, for a 64x64 canvas, and the
	// ANIM chunk an animation has next. 44 bytes: the RIFF length is 36.
	animatedWebP := []byte("RIFF\x24\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00\x3f\x00\x00\x3f\x00\x00ANIM\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00")

	cases := []struct {
		name, mimeType, says string
		content              []byte
	}{
		{name: "not decodable", mimeType: "image/png", content: []byte("\x89PNG\r\n\x1a\nnot really a PNG at all"), says: "It cannot be decoded"},
		{name: "a header and no pixels", mimeType: "image/png", content: pngHeader(3000, 1000), says: "a PNG image of 3000x1000 pixels (image/png)"},
		{name: "too many pixels to decode", mimeType: "image/png", content: pngHeader(10000, 10000), says: "more than the 50 megapixels this tool decodes"},
		{name: "an animated WebP", mimeType: "image/webp", content: animatedWebP, says: "an animated WebP image of 64x64 pixels"},
	}

	for _, testCase := range cases {
		view, err := Read(Request{Path: "broken", WebURL: fileURL}, testCase.content)
		if err != nil {
			t.Fatalf("%s: Read: %v", testCase.name, err)
		}
		if view.Kind != KindBinary || view.Image != nil || view.MIMEType != testCase.mimeType {
			t.Errorf("%s: came back as %s %s with image %v, want it described as binary %s", testCase.name, view.Kind, view.MIMEType, view.Image, testCase.mimeType)
		}
		if !strings.Contains(view.Text, testCase.says) || !strings.HasSuffix(view.Text, fileURL) {
			t.Errorf("%s: description does not say %q and end with the page: %q", testCase.name, testCase.says, view.Text)
		}
	}
}

// TestTheFrameCountSurvivesAGIFThatIsCutShort: it walks bytes a server sent,
// and a GIF that stops mid-block must count what it has, not panic.
func TestTheFrameCountSurvivesAGIFThatIsCutShort(t *testing.T) {
	t.Parallel()

	animation := &gif.GIF{}
	for range 2 {
		animation.Image = append(animation.Image, image.NewPaletted(image.Rect(0, 0, 8, 8), palette.Plan9))
		animation.Delay = append(animation.Delay, 10)
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatalf("encode GIF: %v", err)
	}

	whole := encoded.Bytes()
	if frames := gifFrames(whole); frames != 2 {
		t.Fatalf("gifFrames = %d, want 2", frames)
	}
	for length := range len(whole) {
		if frames := gifFrames(whole[:length]); frames < 0 || frames > 2 {
			t.Fatalf("gifFrames of the first %d bytes = %d", length, frames)
		}
		_ = webpFeatures(whole[:length])
	}
}
