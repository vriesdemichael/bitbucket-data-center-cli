package fileview

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport/filefixture"
)

// distinct is a picture no two pixels of which are alike, so a pixel in the
// wrong place shows.
func distinct(width, height int) *image.NRGBA {
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 40), G: uint8(y * 70), B: uint8(17 + x + 9*y), A: 255})
		}
	}

	return picture
}

// TestEveryOrientationIsTurnedUpright stores a picture the way each
// orientation says, from the specification's wording, and turns it back.
func TestEveryOrientationIsTurnedUpright(t *testing.T) {
	t.Parallel()

	upright := distinct(5, 3)
	for orientation := 1; orientation <= 8; orientation++ {
		t.Run(fmt.Sprintf("orientation %d", orientation), func(t *testing.T) {
			t.Parallel()

			turned := turn(filefixture.Oriented(upright, orientation), orientation)
			if bounds := turned.Bounds(); bounds.Dx() != 5 || bounds.Dy() != 3 {
				t.Fatalf("turned upright, the picture is %dx%d, want 5x3", bounds.Dx(), bounds.Dy())
			}
			for y := range 3 {
				for x := range 5 {
					got, want := color.RGBAModel.Convert(turned.At(x, y)), color.RGBAModel.Convert(upright.At(x, y))
					if got != want {
						t.Fatalf("pixel %d,%d is %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

// storedYCbCr is a YCbCr picture whose every luma sample differs, and whose
// every chroma sample does too.
func storedYCbCr(width, height int, ratio image.YCbCrSubsampleRatio) *image.YCbCr {
	picture := image.NewYCbCr(image.Rect(0, 0, width, height), ratio)
	for index := range picture.Y {
		picture.Y[index] = uint8(index * 5)
	}
	for index := range picture.Cb {
		picture.Cb[index], picture.Cr[index] = uint8(index*11), uint8(255-index*13)
	}

	return picture
}

// TestAYCbCrPictureIsTurnedInItsOwnPlanes: a JPEG decodes to YCbCr, and
// turning it into RGBA would take four bytes a pixel where it takes one and a
// half. Each sample has to land where the specification puts its pixel, and
// a turn across the diagonal swaps how the chroma is subsampled.
func TestAYCbCrPictureIsTurnedInItsOwnPlanes(t *testing.T) {
	t.Parallel()

	ratios := map[image.YCbCrSubsampleRatio]image.YCbCrSubsampleRatio{
		image.YCbCrSubsampleRatio444: image.YCbCrSubsampleRatio444,
		image.YCbCrSubsampleRatio422: image.YCbCrSubsampleRatio440,
		image.YCbCrSubsampleRatio420: image.YCbCrSubsampleRatio420,
		image.YCbCrSubsampleRatio440: image.YCbCrSubsampleRatio422,
	}

	for ratio, swapped := range ratios {
		for orientation := 2; orientation <= 8; orientation++ {
			t.Run(fmt.Sprintf("%v orientation %d", ratio, orientation), func(t *testing.T) {
				t.Parallel()

				stored := storedYCbCr(8, 6, ratio)
				uprightWidth, uprightHeight, wantRatio := 8, 6, ratio
				if orientation >= 5 {
					uprightWidth, uprightHeight, wantRatio = 6, 8, swapped
				}

				turned, ok := turn(stored, orientation).(*image.YCbCr)
				if !ok {
					t.Fatalf("a YCbCr picture was turned into a %T", turn(stored, orientation))
				}
				if turned.SubsampleRatio != wantRatio || turned.Rect.Dx() != uprightWidth || turned.Rect.Dy() != uprightHeight {
					t.Fatalf("turned into a %dx%d %v, want %dx%d %v", turned.Rect.Dx(), turned.Rect.Dy(), turned.SubsampleRatio,
						uprightWidth, uprightHeight, wantRatio)
				}
				for row := range 6 {
					for column := range 8 {
						x, y := filefixture.VisualPosition(uprightWidth, uprightHeight, column, row, orientation)
						if got, want := turned.YCbCrAt(x, y), stored.YCbCrAt(column, row); got != want {
							t.Fatalf("stored %d,%d shows at %d,%d as %v, want %v", column, row, x, y, got, want)
						}
					}
				}
			})
		}
	}

	// A 4:1 ratio has no counterpart turned across the diagonal, so it
	// becomes RGBA, and every pixel still lands in its place.
	stored := storedYCbCr(8, 6, image.YCbCrSubsampleRatio411)
	turned, ok := turn(stored, 6).(*image.RGBA)
	if !ok {
		t.Fatalf("a 4:1:1 picture turned across the diagonal is a %T, want RGBA", turn(stored, 6))
	}
	for row := range 6 {
		for column := range 8 {
			x, y := filefixture.VisualPosition(6, 8, column, row, 6)
			if got, want := turned.RGBAAt(x, y), color.RGBAModel.Convert(stored.At(column, row)); got != want {
				t.Fatalf("stored %d,%d shows at %d,%d as %v, want %v", column, row, x, y, got, want)
			}
		}
	}
}

func TestTheOrientationIsReadFromAJPEGsExifBlock(t *testing.T) {
	t.Parallel()

	plain := filefixture.JPEG(distinct(8, 8), 90)
	if got := jpegOrientation(plain); got != 1 {
		t.Errorf("a JPEG without an Exif block reads as %d, want 1", got)
	}

	for _, bigEndian := range []bool{false, true} {
		for orientation := 1; orientation <= 8; orientation++ {
			if got := jpegOrientation(filefixture.WithSegment(plain, filefixture.ExifBlock(orientation, bigEndian))); got != orientation {
				t.Errorf("orientation %d, big-endian %v, reads as %d", orientation, bigEndian, got)
			}
		}
	}

	// XMP shares the APP1 marker. A reader that stopped at the first APP1
	// would miss the Exif block after it.
	xmp := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), bytes.Repeat([]byte{'x'}, 40)...)
	both := filefixture.WithSegment(filefixture.WithSegment(plain, filefixture.ExifBlock(6, false)), xmp)
	if got := jpegOrientation(both); got != 6 {
		t.Errorf("an Exif block after an XMP one reads as %d, want 6", got)
	}
}

// TestAnOrientationThatCannotBeReadIsUpright: the tag is the file's say about
// itself, and a block that cannot be read says nothing -- never a failure.
func TestAnOrientationThatCannotBeReadIsUpright(t *testing.T) {
	t.Parallel()

	good := filefixture.ExifBlock(6, false)
	corrupt := func(change func(block []byte) []byte) []byte {
		return change(append([]byte{}, good...))
	}
	blocks := map[string][]byte{
		"cut after the byte order": good[:8],
		"no byte order":            corrupt(func(block []byte) []byte { copy(block[6:], "XX"); return block }),
		"not TIFF's 42":            corrupt(func(block []byte) []byte { block[8] = 43; return block }),
		"a directory past the end": corrupt(func(block []byte) []byte { block[10] = 200; return block }),
		"an entry cut short":       good[:24],
		"a second entry that is not there": corrupt(func(block []byte) []byte {
			// Two entries claimed; the first is not the orientation, and
			// the second is past the end.
			block[14], block[16] = 2, 0x00
			return block[:28]
		}),
		"a LONG, not a SHORT": corrupt(func(block []byte) []byte { block[18] = 4; return block }),
		"two values":          corrupt(func(block []byte) []byte { block[20] = 2; return block }),
		"orientation 0":       corrupt(func(block []byte) []byte { block[24] = 0; return block }),
		"orientation 9":       corrupt(func(block []byte) []byte { block[24] = 9; return block }),
	}

	plain := filefixture.JPEG(distinct(8, 8), 90)
	for name, block := range blocks {
		if got := jpegOrientation(filefixture.WithSegment(plain, block)); got != 1 {
			t.Errorf("%s: reads as %d, want 1", name, got)
		}
	}

	segments := map[string][]byte{
		"not a JPEG":              []byte("\x89PNG\r\n\x1a\n"),
		"a segment past the end":  append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xF0}, good...),
		"a length shorter than 2": []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x01, 0x00, 0x00},
		"no marker":               []byte{0xFF, 0xD8, 0x00, 0xE1, 0x00, 0x10},
		"image data first":        append([]byte{0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x04, 0x00, 0x00}, good...),
		"fill, then nothing":      []byte{0xFF, 0xD8, 0xFF, 0xFF, 0xFF, 0xFF},
	}
	for name, content := range segments {
		if got := jpegOrientation(content); got != 1 {
			t.Errorf("%s: reads as %d, want 1", name, got)
		}
	}

	// And read end to end: a JPEG whose block cannot be read is returned
	// as it is, not turned and not refused.
	content := filefixture.WithSegment(plain, blocks["orientation 9"])
	view, err := Read(t.Context(), Request{Path: "odd.jpg"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Kind != KindImage || view.Image.Turned || !bytes.Equal(view.Image.Data, content) || strings.Contains(view.Text, "turned") {
		t.Errorf("a JPEG with an unreadable orientation: %s, turned %v, %q", view.Kind, view.Image != nil && view.Image.Turned, view.Text)
	}
}

// assertQuadrants checks that a picture has filefixture.Quadrants' colours in
// its corners, within what compressing it as a JPEG changes.
func assertQuadrants(t *testing.T, picture image.Image) {
	t.Helper()

	if err := filefixture.CheckQuadrants(picture, 40); err != nil {
		t.Error(err)
	}
}

// TestASidewaysPhotographComesBackUpright is the case this exists for: a
// phone's photograph, stored sideways with its orientation in its Exif block.
// Encoded again, the tag is gone, so the pixels have to be upright.
func TestASidewaysPhotographComesBackUpright(t *testing.T) {
	t.Parallel()

	upright := filefixture.Quadrants(60, 40)
	for orientation := 1; orientation <= 8; orientation++ {
		t.Run(fmt.Sprintf("orientation %d", orientation), func(t *testing.T) {
			t.Parallel()

			content := filefixture.WithSegment(filefixture.JPEG(filefixture.Oriented(upright, orientation), 95), filefixture.ExifBlock(orientation, orientation%2 == 0))
			view, err := Read(t.Context(), Request{Path: "photo.jpg"}, content)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}

			picture := returnedImage(t, view)
			assertQuadrants(t, picture)
			if view.Image.Width != 60 || view.Image.Height != 40 || view.Image.ReturnedWidth != 60 || view.Image.ReturnedHeight != 40 {
				t.Errorf("image = %+v, want 60x40 upright and returned", *view.Image)
			}

			if orientation == 1 {
				if view.Image.Turned || !bytes.Equal(view.Image.Data, content) {
					t.Error("an upright photograph was not returned as it is")
				}

				return
			}
			if !view.Image.Turned || view.Image.Scaled || view.Image.MIMEType != "image/jpeg" || bytes.Equal(view.Image.Data, content) {
				t.Errorf("image = turned %v, scaled %v, %s; want turned, not scaled, and encoded again as a JPEG",
					view.Image.Turned, view.Image.Scaled, view.Image.MIMEType)
			}
			if !strings.Contains(view.Text, "photo.jpg: a JPEG image, 60x40 pixels, ") ||
				!strings.Contains(view.Text, " It follows turned upright from its EXIF orientation, as a JPEG of ") {
				t.Errorf("text does not say the photograph was turned upright: %q", view.Text)
			}
		})
	}
}

func TestALargeSidewaysPhotographIsTurnedThenScaled(t *testing.T) {
	t.Parallel()

	content := filefixture.WithSegment(filefixture.JPEG(filefixture.Oriented(filefixture.Quadrants(1200, 3000), 6), 90), filefixture.ExifBlock(6, true))
	view, err := Read(t.Context(), Request{Path: "tall.jpg"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	assertQuadrants(t, returnedImage(t, view))
	image := view.Image
	if !image.Turned || !image.Scaled || image.Width != 1200 || image.Height != 3000 || image.ReturnedWidth != 819 || image.ReturnedHeight != 2048 {
		t.Fatalf("image = %+v, want 1200x3000 turned upright and scaled to 819x2048", *image)
	}
	for _, want := range []string{
		"tall.jpg: a JPEG image, 1200x3000 pixels, ",
		"It follows turned upright from its EXIF orientation and scaled down to 819x2048 pixels, as a JPEG of ",
		"Small text in it may no longer be legible because of the scaling.",
	} {
		if !strings.Contains(view.Text, want) {
			t.Errorf("text does not say %q: %q", want, view.Text)
		}
	}
}

// withExifChunk makes a simple WebP an extended one with an EXIF chunk: the
// VP8X header naming the chunk, the image, and the chunk, which comes last.
func withExifChunk(t *testing.T, simple, exif []byte) []byte {
	t.Helper()

	config, err := decodeImageConfig("image/webp", simple)
	if err != nil {
		t.Fatalf("decode the WebP's size: %v", err)
	}

	const exifFlag = 1 << 3
	header := []byte{exifFlag, 0, 0, 0}
	header = append(header, byte(config.Width-1), byte((config.Width-1)>>8), byte((config.Width-1)>>16))
	header = append(header, byte(config.Height-1), byte((config.Height-1)>>8), byte((config.Height-1)>>16))

	chunk := func(name string, body []byte) []byte {
		out := binary.LittleEndian.AppendUint32([]byte(name), uint32(len(body)))
		out = append(out, body...)
		if len(body)%2 == 1 {
			out = append(out, 0)
		}

		return out
	}

	body := append([]byte("WEBP"), chunk("VP8X", header)...)
	body = append(body, simple[12:]...) // the simple file's image chunk
	body = append(body, chunk("EXIF", exif)...)

	return append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)
}

func TestAWebPsExifChunkTurnsItUpright(t *testing.T) {
	t.Parallel()

	simple, err := os.ReadFile("testdata/lossy.webp")
	if err != nil {
		t.Fatalf("read the WebP: %v", err)
	}

	// Some writers put the chunk's TIFF data straight in, and some keep the
	// "Exif\0\0" a JPEG's segment starts with.
	for name, exif := range map[string][]byte{
		"with the Exif marker":    filefixture.ExifBlock(6, false),
		"without the Exif marker": filefixture.ExifBlock(6, true)[6:],
	} {
		view, err := Read(t.Context(), Request{Path: "photo.webp"}, withExifChunk(t, simple, exif))
		if err != nil {
			t.Fatalf("%s: Read: %v", name, err)
		}
		returnedImage(t, view)
		if !view.Image.Turned || view.Image.Width != 100 || view.Image.Height != 150 || view.Image.MIMEType != "image/jpeg" ||
			!strings.Contains(view.Text, "turned upright from its EXIF orientation") {
			t.Errorf("%s: image = %+v, text %q; want the 150x100 WebP turned to 100x150 as a JPEG", name, *view.Image, view.Text)
		}
	}
}

func TestTheJPEGOfATurnedPictureCarriesNoTagToTurnItAgain(t *testing.T) {
	t.Parallel()

	content := filefixture.WithSegment(filefixture.JPEG(filefixture.Oriented(filefixture.Quadrants(60, 40), 8), 95), filefixture.ExifBlock(8, false))
	view, err := Read(t.Context(), Request{Path: "photo.jpg"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := jpegOrientation(view.Image.Data); got != 1 {
		t.Errorf("the returned JPEG says to turn it %d, want no tag", got)
	}
	if _, err := jpeg.DecodeConfig(bytes.NewReader(view.Image.Data)); err != nil {
		t.Errorf("the returned JPEG does not decode: %v", err)
	}
}
