package fileview

import (
	"bytes"
	"encoding/binary"
	"image"
)

// A camera stores a photograph the way its sensor was held and records, in an
// Orientation tag, how to turn it upright: 1 is upright already, 2 to 8 are the
// seven turns and mirrorings of a rectangle. A viewer that reads the tag shows
// the picture upright. Encoding the picture again drops the tag, so a picture
// is turned here, before it is scaled or encoded, and comes back upright
// whatever a client makes of its metadata.
//
// Metadata is the file's own say about itself, so anything that cannot be read
// in it -- a short block, a wrong offset, a value outside 1 to 8 -- is taken as
// upright, never as a reason to fail.

const (
	// orientationTag is the TIFF tag EXIF borrows for the orientation.
	orientationTag = 0x0112
	// maxDirectoryEntries is the most entries of a TIFF directory read looking
	// for it. The format allows 65535; a camera writes a few dozen.
	maxDirectoryEntries = 1024
	// maxJPEGSegments is the most segments read looking for the Exif block,
	// which a camera writes first, straight after the start of the image.
	maxJPEGSegments = 64
)

// tiffHeader reads the header of TIFF-structured data -- a TIFF file, or the
// block EXIF stores in one -- for its byte order and where its first directory
// is.
func tiffHeader(data []byte) (binary.ByteOrder, int64, bool) {
	if len(data) < 8 {
		return nil, 0, false
	}

	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, 0, false
	}
	if order.Uint16(data[2:4]) != 42 {
		return nil, 0, false
	}

	return order, int64(order.Uint32(data[4:8])), true
}

// tiffOrientation reads the Orientation tag from the first directory of
// TIFF-structured data, or 1 when it is not there or cannot be read.
func tiffOrientation(data []byte) int {
	order, directory, ok := tiffHeader(data)
	if !ok || directory+2 > int64(len(data)) {
		return 1
	}

	entries := int64(order.Uint16(data[directory:]))
	for index := range min(entries, maxDirectoryEntries) {
		entry := directory + 2 + 12*index
		if entry+12 > int64(len(data)) {
			return 1
		}
		if order.Uint16(data[entry:]) != orientationTag {
			continue
		}

		// One SHORT, as the tag is defined, held in the first two bytes of
		// the entry's value.
		const short = 3
		if order.Uint16(data[entry+2:]) != short || order.Uint32(data[entry+4:]) != 1 {
			return 1
		}
		if value := int(order.Uint16(data[entry+8:])); value >= 1 && value <= 8 {
			return value
		}

		return 1
	}

	return 1
}

// exifOrientation reads the orientation from an Exif block: TIFF-structured
// data, which a JPEG's APP1 segment -- and some WebP writers' EXIF chunk --
// puts after an "Exif\0\0" marker.
func exifOrientation(block []byte) int {
	return tiffOrientation(bytes.TrimPrefix(block, []byte("Exif\x00\x00")))
}

// jpegOrientation finds a JPEG's Exif block among the segments before its
// image data, and reads the orientation from it.
func jpegOrientation(content []byte) int {
	if !bytes.HasPrefix(content, []byte{0xFF, 0xD8}) {
		return 1
	}

	offset := 2
	for range maxJPEGSegments {
		if offset+4 > len(content) || content[offset] != 0xFF {
			return 1
		}

		marker := content[offset+1]
		switch {
		case marker == 0xFF:
			// Fill before a marker.
			offset++

			continue
		case marker == 0xDA, marker == 0xD9:
			// The image data starts, or the image ends: the block, if any,
			// was before.
			return 1
		}

		length := int(binary.BigEndian.Uint16(content[offset+2:]))
		if length < 2 || offset+2+length > len(content) {
			return 1
		}
		if segment := content[offset+4 : offset+2+length]; marker == 0xE1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return exifOrientation(segment)
		}
		offset += 2 + length
	}

	return 1
}

// turn returns a picture turned upright as orientation says. A YCbCr picture
// -- a JPEG's, a lossy WebP's -- stays YCbCr, at the memory it already takes;
// anything else becomes RGBA.
func turn(picture image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return picture
	}

	bounds := picture.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	upright := image.Rect(0, 0, width, height)
	if orientation >= 5 {
		upright = image.Rect(0, 0, height, width)
	}

	// stored is where the upright picture's pixel at x, y is stored. The
	// cases follow the tag's definition, which says what the stored rows and
	// columns are: for 6, the first row is the right-hand side and the first
	// column the top, so the picture is stored a quarter turn anticlockwise
	// and shown a quarter turn clockwise. Each comment says how the picture
	// is stored.
	stored := func(x, y int) (int, int) {
		switch orientation {
		case 2: // mirrored left to right
			x = width - 1 - x
		case 3: // half a turn round
			x, y = width-1-x, height-1-y
		case 4: // mirrored top to bottom
			y = height - 1 - y
		case 5: // mirrored across the diagonal from the top left
			x, y = y, x
		case 6: // a quarter turn anticlockwise
			x, y = y, height-1-x
		case 7: // mirrored across the diagonal from the top right
			x, y = width-1-y, height-1-x
		case 8: // a quarter turn clockwise
			x, y = width-1-y, x
		}

		return bounds.Min.X + x, bounds.Min.Y + y
	}

	if source, ok := picture.(*image.YCbCr); ok {
		if ratio, ok := turnedRatio(source.SubsampleRatio, orientation); ok {
			return turnYCbCr(source, upright, ratio, stored)
		}
	}

	target := image.NewRGBA(upright)
	for y := range upright.Dy() {
		for x := range upright.Dx() {
			target.Set(x, y, picture.At(stored(x, y)))
		}
	}

	return target
}

// turnedRatio is the chroma subsampling a turned YCbCr picture keeps: the same,
// or across and down swapped when the turn swaps them. The two 4:1 ratios have
// no swapped counterpart, and turn as RGBA instead.
func turnedRatio(ratio image.YCbCrSubsampleRatio, orientation int) (image.YCbCrSubsampleRatio, bool) {
	swapped := orientation >= 5
	switch ratio {
	case image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio420:
		return ratio, true
	case image.YCbCrSubsampleRatio422:
		if swapped {
			return image.YCbCrSubsampleRatio440, true
		}

		return ratio, true
	case image.YCbCrSubsampleRatio440:
		if swapped {
			return image.YCbCrSubsampleRatio422, true
		}

		return ratio, true
	}

	return ratio, false
}

// turnYCbCr turns a YCbCr picture plane by plane. A chroma sample covers a
// block of pixels, and is read from where the first of them is stored.
func turnYCbCr(source *image.YCbCr, upright image.Rectangle, ratio image.YCbCrSubsampleRatio, stored func(x, y int) (int, int)) *image.YCbCr {
	target := image.NewYCbCr(upright, ratio)
	for y := range upright.Dy() {
		for x := range upright.Dx() {
			target.Y[target.YOffset(x, y)] = source.Y[source.YOffset(stored(x, y))]
		}
	}

	across, down := 1, 1
	switch ratio {
	case image.YCbCrSubsampleRatio422:
		across = 2
	case image.YCbCrSubsampleRatio420:
		across, down = 2, 2
	case image.YCbCrSubsampleRatio440:
		down = 2
	}
	for y := 0; y < upright.Dy(); y += down {
		for x := 0; x < upright.Dx(); x += across {
			from, to := source.COffset(stored(x, y)), target.COffset(x, y)
			target.Cb[to], target.Cr[to] = source.Cb[from], source.Cr[from]
		}
	}

	return target
}
