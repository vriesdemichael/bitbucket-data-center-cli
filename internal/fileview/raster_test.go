package fileview

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport/filefixture"
	"golang.org/x/image/tiff"
)

// solid is a picture of one colour, for a page that must not be the one
// returned.
func solid(width, height int, fill color.RGBA) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.SetRGBA(x, y, fill)
		}
	}

	return picture
}

// assertExactQuadrants checks a picture that went only through lossless
// formats: every corner exactly Quadrants' colour.
func assertExactQuadrants(t *testing.T, picture image.Image) {
	t.Helper()

	if err := filefixture.CheckQuadrants(picture, 0); err != nil {
		t.Error(err)
	}
}

// TestABMPComesBackAsAPNG: model APIs refuse a bitmap, so it is converted
// even when it is small, and the text says from what.
func TestABMPComesBackAsAPNG(t *testing.T) {
	t.Parallel()

	content := filefixture.BMP(filefixture.Quadrants(120, 80))
	view, err := Read(t.Context(), Request{Path: "diagram.bmp"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	assertExactQuadrants(t, returnedImage(t, view))
	if view.MIMEType != "image/bmp" || view.Image.MIMEType != "image/png" || view.Image.Scaled || view.Image.Turned ||
		view.Image.Width != 120 || view.Image.Height != 80 {
		t.Errorf("a BMP came back as %s, image %+v; want it converted to a 120x80 PNG", view.MIMEType, *view.Image)
	}
	want := "diagram.bmp: a BMP image, 120x80 pixels, " + formatSize(int64(len(content))) + ". It follows as a PNG of " +
		formatSize(int64(len(view.Image.Data))) + ", converted from BMP, which clients do not take."
	if view.Text != want {
		t.Errorf("text:\n got %q\nwant %q", view.Text, want)
	}
}

func TestALargeBMPIsConvertedAndScaled(t *testing.T) {
	t.Parallel()

	view, err := Read(t.Context(), Request{Path: "wide.bmp"}, filefixture.BMP(filefixture.Quadrants(3000, 1000)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	returnedImage(t, view)
	if !view.Image.Scaled || view.Image.ReturnedWidth != ImageEdge || view.Image.ReturnedHeight != 683 {
		t.Errorf("image = %+v, want it scaled to %dx683", *view.Image, ImageEdge)
	}
	for _, want := range []string{
		"It follows scaled down to 2048x683 pixels, as a PNG of ",
		", converted from BMP, which clients do not take.",
		"Small text in it may no longer be legible because of the scaling.",
	} {
		if !strings.Contains(view.Text, want) {
			t.Errorf("text does not say %q: %q", want, view.Text)
		}
	}
}

// TestATIFFComesBackAsItsFirstPage: a scan is often several pages, and the
// decoder reads the first. The text has to say the rest are there.
func TestATIFFComesBackAsItsFirstPage(t *testing.T) {
	t.Parallel()

	content := filefixture.TIFF(0, filefixture.Quadrants(90, 60),
		solid(90, 60, color.RGBA{R: 9, G: 9, B: 9, A: 255}), solid(40, 40, color.RGBA{R: 99, G: 9, B: 9, A: 255}))
	view, err := Read(t.Context(), Request{Path: "scan.tif"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	assertExactQuadrants(t, returnedImage(t, view))
	if view.MIMEType != "image/tiff" || view.Image.Pages != 3 || view.Image.MIMEType != "image/png" || view.Image.Turned {
		t.Errorf("a TIFF came back as %s, image %+v; want the first of 3 pages as a PNG", view.MIMEType, *view.Image)
	}
	for _, want := range []string{
		"scan.tif: a TIFF image of 3 pages, 90x60 pixels, ",
		" Its first page follows as a PNG of ",
		", converted from TIFF, which clients do not take.",
	} {
		if !strings.Contains(view.Text, want) {
			t.Errorf("text does not say %q: %q", want, view.Text)
		}
	}
}

// TestATIFFsOwnOrientationTurnsIt: the tag EXIF borrowed is TIFF's, and a
// scanner or a camera writing TIFF sets it in the file's first directory.
func TestATIFFsOwnOrientationTurnsIt(t *testing.T) {
	t.Parallel()

	for orientation := 1; orientation <= 8; orientation++ {
		content := filefixture.TIFF(orientation, filefixture.Oriented(filefixture.Quadrants(90, 60), orientation))
		if got := tiffOrientation(content); got != orientation {
			t.Errorf("a TIFF tagged %d reads as %d", orientation, got)
		}

		view, err := Read(t.Context(), Request{Path: "scan.tif"}, content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		assertExactQuadrants(t, returnedImage(t, view))
		if view.Image.Width != 90 || view.Image.Height != 60 || view.Image.Turned != (orientation > 1) {
			t.Errorf("orientation %d: image %+v, want 90x60, turned %v", orientation, *view.Image, orientation > 1)
		}
		if turned := strings.Contains(view.Text, " It follows turned upright from its TIFF orientation, as a PNG of "); turned != (orientation > 1) {
			t.Errorf("orientation %d: text %q", orientation, view.Text)
		}
	}
}

// TestATIFFFromTheStandardEncoderIsRead: what golang.org/x/image writes, with
// its compression, rather than the plainest TIFF the fixture writes.
func TestATIFFFromTheStandardEncoderIsRead(t *testing.T) {
	t.Parallel()

	var encoded bytes.Buffer
	if err := tiff.Encode(&encoded, filefixture.Quadrants(64, 48), &tiff.Options{Compression: tiff.Deflate, Predictor: true}); err != nil {
		t.Fatalf("encode TIFF: %v", err)
	}

	view, err := Read(t.Context(), Request{Path: "chart.tiff"}, encoded.Bytes())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	assertExactQuadrants(t, returnedImage(t, view))
	if view.Image.Pages != 0 || view.Image.Turned || !strings.HasPrefix(view.Text, "chart.tiff: a TIFF image, 64x48 pixels, ") {
		t.Errorf("a single-page TIFF: %+v %q", *view.Image, view.Text)
	}
}

func TestTheTIFFPageCountFollowsTheDirectoriesAndNoFurther(t *testing.T) {
	t.Parallel()

	page := filefixture.Quadrants(4, 4)
	if got := tiffPages(filefixture.TIFF(0, page, page, page)); got != 3 {
		t.Errorf("three pages count as %d", got)
	}
	if got := tiffPages([]byte("not a TIFF at all")); got != 0 {
		t.Errorf("a file that is not a TIFF has %d pages", got)
	}

	// The last four bytes of the fixture are the first directory's pointer to
	// the next, which is none.
	single := filefixture.TIFF(0, page)
	first := binary.LittleEndian.Uint32(single[4:8])
	for name, next := range map[string]uint32{
		"a directory that points at itself": first,
		"a next directory past the end":     uint32(len(single) + 100),
	} {
		content := append([]byte{}, single...)
		binary.LittleEndian.PutUint32(content[len(content)-4:], next)
		if got := tiffPages(content); got != 1 {
			t.Errorf("%s: %d pages, want 1", name, got)
		}
	}
	if got := tiffPages(single[:len(single)-2]); got != 1 {
		t.Errorf("a directory cut off before its pointer: %d pages, want 1", got)
	}
}

func TestABMPOrATIFFThatCannotBeDecodedIsDescribed(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		content []byte
		says    string
	}{
		"broken.bmp": {content: append([]byte("BM"), make([]byte, 60)...), says: "a BMP image (image/bmp), 62 bytes. It cannot be decoded, so it is not shown."},
		"broken.tif": {content: append([]byte("II*\x00\x08\x00\x00\x00"), make([]byte, 30)...), says: "a TIFF image (image/tiff), 38 bytes. It cannot be decoded, so it is not shown."},
	}
	for path, testCase := range cases {
		view, err := Read(t.Context(), Request{Path: path}, testCase.content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if view.Kind != KindBinary || !strings.Contains(view.Text, testCase.says) {
			t.Errorf("%s: %s %q, want it described as %q", path, view.Kind, view.Text, testCase.says)
		}
	}
}
