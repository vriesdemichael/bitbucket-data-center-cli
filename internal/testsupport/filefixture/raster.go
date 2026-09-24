package filefixture

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
)

// maxRasterSide bounds what the raster writers here are asked for: a test
// picture, far inside what their size fields hold.
const maxRasterSide = 1 << 15

// rgbOf is a pixel's colour as eight bits a channel.
func rgbOf(picture image.Image, x, y int) color.RGBA {
	converted, ok := color.RGBAModel.Convert(picture.At(x, y)).(color.RGBA)
	if !ok {
		panic(fmt.Sprintf("the colour at %d,%d did not convert to RGBA", x, y))
	}

	return converted
}

// sides is a picture's width and height, checked to be one the writers make.
func sides(picture image.Image) (int, int) {
	bounds := picture.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 || width > maxRasterSide || height > maxRasterSide {
		panic(fmt.Sprintf("a %dx%d picture is not one the raster writers make", width, height))
	}

	return width, height
}

// field32 and field16 put a size or an offset in a 32-bit or 16-bit field,
// which a test file never outgrows.
func field32(value int) uint32 {
	if value < 0 || value > math.MaxUint32 {
		panic(fmt.Sprintf("%d does not fit a 32-bit field", value))
	}

	return uint32(value)
}

func field16(value int) uint16 {
	if value < 0 || value > math.MaxUint16 {
		panic(fmt.Sprintf("%d does not fit a 16-bit field", value))
	}

	return uint16(value)
}

// BMP writes a picture as a Windows bitmap, the plainest kind: 24 bits a pixel,
// uncompressed, rows from the bottom up, each padded to four bytes.
func BMP(picture image.Image) []byte {
	width, height := sides(picture)
	bounds := picture.Bounds()
	row := (width*3 + 3) &^ 3
	pixels := row * height

	le := binary.LittleEndian
	file := []byte("BM")
	file = le.AppendUint32(file, field32(14+40+pixels)) // the file's size
	file = le.AppendUint32(file, 0)                     // reserved
	file = le.AppendUint32(file, 14+40)                 // where the pixels start
	file = le.AppendUint32(file, 40)                    // BITMAPINFOHEADER's size
	file = le.AppendUint32(file, field32(width))
	file = le.AppendUint32(file, field32(height)) // positive: bottom up
	file = le.AppendUint16(file, 1)               // one plane
	file = le.AppendUint16(file, 24)              // bits a pixel
	file = le.AppendUint32(file, 0)               // uncompressed
	file = le.AppendUint32(file, field32(pixels))
	file = le.AppendUint32(file, 2835) // 72 dots an inch, across
	file = le.AppendUint32(file, 2835) // and down
	file = le.AppendUint32(file, 0)    // no palette
	file = le.AppendUint32(file, 0)    // every colour important

	for y := height - 1; y >= 0; y-- {
		line := make([]byte, row)
		for x := range width {
			pixel := rgbOf(picture, bounds.Min.X+x, bounds.Min.Y+y)
			line[3*x], line[3*x+1], line[3*x+2] = pixel.B, pixel.G, pixel.R
		}
		file = append(file, line...)
	}

	return file
}

// TIFF writes pictures as the pages of a TIFF, the plainest kind a reader
// takes: little-endian, 8-bit RGB, uncompressed, one strip a page. An
// orientation from 1 to 8 goes in the first page's directory as its
// Orientation tag; 0 leaves the tag out.
func TIFF(orientation int, pages ...image.Image) []byte {
	if orientation < 0 || orientation > 8 {
		panic(fmt.Sprintf("orientation %d is not one of 0 to 8", orientation))
	}

	le := binary.LittleEndian
	file := []byte("II")
	file = le.AppendUint16(file, 42)
	next := len(file) // where the offset of the next directory goes
	file = le.AppendUint32(file, 0)

	type entry struct {
		tag, kind, count, value int
	}
	const short, long = 3, 4

	for index, page := range pages {
		width, height := sides(page)
		bounds := page.Bounds()

		strip := len(file)
		for y := range height {
			for x := range width {
				pixel := rgbOf(page, bounds.Min.X+x, bounds.Min.Y+y)
				file = append(file, pixel.R, pixel.G, pixel.B)
			}
		}
		bits := len(file)
		file = le.AppendUint16(le.AppendUint16(le.AppendUint16(file, 8), 8), 8)

		entries := []entry{
			{tag: 256, kind: long, count: 1, value: width},
			{tag: 257, kind: long, count: 1, value: height},
			{tag: 258, kind: short, count: 3, value: bits}, // three values: an offset to them
			{tag: 259, kind: short, count: 1, value: 1},    // uncompressed
			{tag: 262, kind: short, count: 1, value: 2},    // RGB
			{tag: 273, kind: long, count: 1, value: strip},
		}
		if index == 0 && orientation > 0 {
			entries = append(entries, entry{tag: 274, kind: short, count: 1, value: orientation})
		}
		entries = append(entries,
			entry{tag: 277, kind: short, count: 1, value: 3},
			entry{tag: 278, kind: long, count: 1, value: height},
			entry{tag: 279, kind: long, count: 1, value: width * height * 3},
			entry{tag: 284, kind: short, count: 1, value: 1}, // chunky
		)

		// A directory starts on a word boundary.
		if len(file)%2 == 1 {
			file = append(file, 0)
		}
		le.PutUint32(file[next:], field32(len(file)))

		file = le.AppendUint16(file, field16(len(entries)))
		for _, field := range entries {
			file = le.AppendUint16(file, field16(field.tag))
			file = le.AppendUint16(file, field16(field.kind))
			file = le.AppendUint32(file, field32(field.count))
			if field.kind == short && field.count == 1 {
				// A single SHORT sits in the first half of the value.
				file = le.AppendUint16(le.AppendUint16(file, field16(field.value)), 0)
			} else {
				file = le.AppendUint32(file, field32(field.value))
			}
		}
		next = len(file)
		file = le.AppendUint32(file, 0)
	}

	return file
}
