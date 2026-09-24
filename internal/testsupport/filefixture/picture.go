package filefixture

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
)

// edges says, for each orientation a picture's tag can record, which side of
// the upright picture its stored rows run from and which side its stored
// columns run from -- word for word as the TIFF specification defines the tag.
// A test builds a stored picture from this, rather than from the arithmetic
// the code under test turns it back with, so that a wrong turn in the code
// cannot be matched by the same wrong turn in the fixture.
var edges = [9]struct{ rows, columns string }{
	1: {rows: "top", columns: "left"},
	2: {rows: "top", columns: "right"},
	3: {rows: "bottom", columns: "right"},
	4: {rows: "bottom", columns: "left"},
	5: {rows: "left", columns: "top"},
	6: {rows: "right", columns: "top"},
	7: {rows: "right", columns: "bottom"},
	8: {rows: "left", columns: "bottom"},
}

// VisualPosition is where the stored pixel at column, row of a picture with
// the given orientation appears once the picture is upright, width by height.
func VisualPosition(width, height, column, row, orientation int) (x, y int) {
	edge := edges[orientation]

	switch edge.rows {
	case "top":
		y = row
	case "bottom":
		y = height - 1 - row
	case "left":
		x = row
	case "right":
		x = width - 1 - row
	}

	switch edge.columns {
	case "left":
		x = column
	case "right":
		x = width - 1 - column
	case "top":
		y = column
	case "bottom":
		y = height - 1 - column
	}

	return x, y
}

// Oriented is upright as a camera would store it under the given orientation:
// turned or mirrored so that a viewer that applies the tag shows upright.
func Oriented(upright image.Image, orientation int) *image.RGBA {
	if orientation < 1 || orientation > 8 {
		panic(fmt.Sprintf("orientation %d is not one of 1 to 8", orientation))
	}

	bounds := upright.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	stored := image.Rect(0, 0, width, height)
	if orientation >= 5 {
		stored = image.Rect(0, 0, height, width)
	}

	picture := image.NewRGBA(stored)
	for row := range stored.Dy() {
		for column := range stored.Dx() {
			x, y := VisualPosition(width, height, column, row, orientation)
			picture.Set(column, row, upright.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}

	return picture
}

// quadrantColors are the colours of Quadrants' corners: top left, top right,
// bottom left, bottom right.
var quadrantColors = [2][2]color.RGBA{
	{{R: 220, G: 30, B: 30, A: 255}, {R: 30, G: 200, B: 30, A: 255}},
	{{R: 30, G: 30, B: 220, A: 255}, {R: 230, G: 220, B: 30, A: 255}},
}

// Quadrants is an upright picture of four colours, one to each corner, so a
// turn or a mirroring left in it moves a colour to another corner.
func Quadrants(width, height int) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.SetRGBA(x, y, quadrantColors[y*2/height][x*2/width])
		}
	}

	return picture
}

// CheckQuadrants reports whether a picture, whatever its size, has
// Quadrants' colours in Quadrants' corners, each channel within tolerance
// -- what compressing it as a JPEG changes.
func CheckQuadrants(picture image.Image, tolerance int) error {
	bounds := picture.Bounds()
	for row := range 2 {
		for column := range 2 {
			x := bounds.Min.X + bounds.Dx()/4 + column*bounds.Dx()/2
			y := bounds.Min.Y + bounds.Dy()/4 + row*bounds.Dy()/2
			got, ok := color.RGBAModel.Convert(picture.At(x, y)).(color.RGBA)
			if !ok {
				return fmt.Errorf("the colour at %d,%d did not convert to RGBA", x, y)
			}
			want := quadrantColors[row][column]
			if apart(got.R, want.R) > tolerance || apart(got.G, want.G) > tolerance || apart(got.B, want.B) > tolerance {
				return fmt.Errorf("the corner at column %d, row %d of a %dx%d picture is %v, want about %v",
					column, row, bounds.Dx(), bounds.Dy(), got, want)
			}
		}
	}

	return nil
}

func apart(left, right uint8) int {
	if left > right {
		return int(left - right)
	}

	return int(right - left)
}

// JPEG encodes a picture as the standard library does.
func JPEG(picture image.Image, quality int) []byte {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: quality}); err != nil {
		panic(fmt.Sprintf("encode JPEG: %v", err))
	}

	return encoded.Bytes()
}

// ExifBlock is an Exif block holding one tag, the orientation, as a camera
// writes it into a JPEG's APP1 segment: "Exif\0\0", then TIFF-structured data
// in either byte order.
func ExifBlock(orientation int, bigEndian bool) []byte {
	if orientation < 0 || orientation > 0xFFFF {
		panic(fmt.Sprintf("orientation %d does not fit the tag's SHORT", orientation))
	}

	var order binary.AppendByteOrder = binary.LittleEndian
	block := []byte("Exif\x00\x00II")
	if bigEndian {
		order = binary.BigEndian
		block = []byte("Exif\x00\x00MM")
	}

	block = order.AppendUint16(block, 42)
	block = order.AppendUint32(block, 8)      // the first directory, after the header
	block = order.AppendUint16(block, 1)      // one entry
	block = order.AppendUint16(block, 0x0112) // Orientation
	block = order.AppendUint16(block, 3)      // a SHORT
	block = order.AppendUint32(block, 1)      // one of them
	block = order.AppendUint16(block, uint16(orientation))
	block = order.AppendUint16(block, 0) // the rest of the value field

	return order.AppendUint32(block, 0) // no next directory
}

// WithSegment splices an APP1 segment holding block into a JPEG, straight
// after its start-of-image marker, where a camera puts its Exif block.
func WithSegment(encoded, block []byte) []byte {
	length := len(block) + 2
	if length > 0xFFFF {
		panic(fmt.Sprintf("a segment holds at most 65533 bytes, not %d", len(block)))
	}

	segment := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(length))
	segment = append(segment, block...)

	spliced := append([]byte{}, encoded[:2]...)
	spliced = append(spliced, segment...)

	return append(spliced, encoded[2:]...)
}
