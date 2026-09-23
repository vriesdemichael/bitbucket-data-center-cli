package fileview

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// imageFormats are the formats returned as images, named for a sentence: the
// four that every client taking images accepts, each of which is decoded here
// when it has to be scaled.
var imageFormats = map[string]string{
	"image/png":  "PNG",
	"image/jpeg": "JPEG",
	"image/gif":  "GIF",
	"image/webp": "WebP",
}

// imageLimits bound what an image is returned as. Reading uses the package's;
// a test passes smaller ones, to scale an image small enough to build.
type imageLimits struct {
	bytes  int
	edge   int
	pixels int64
}

var defaultImageLimits = imageLimits{bytes: ImageBytes, edge: ImageEdge, pixels: ImagePixels}

// readImage views an image as an image: as it is when it fits what a client
// takes, and otherwise decoded, scaled down and encoded again, with a sentence
// saying so -- small text may no longer be legible. An animated image gives
// its first frame. One that cannot be decoded is described instead.
func readImage(request Request, mimeType string, content []byte, limits imageLimits) View {
	name := imageFormats[mimeType]
	subject := subjectOf(request)
	size := int64(len(content))

	notShown := func(what, why string) View {
		return describeBinary(subject, request.WebURL, binaryType{mimeType: mimeType, name: what, reason: why}, size)
	}

	config, err := decodeImageConfig(mimeType, content)
	if err != nil {
		return notShown("a "+name+" image", "It cannot be decoded, so it is not shown.")
	}
	stored := fmt.Sprintf("%dx%d pixels", config.Width, config.Height)
	if int64(config.Width)*int64(config.Height) > limits.pixels {
		return notShown(fmt.Sprintf("a %s image of %s", name, stored), fmt.Sprintf(
			"That is more than the %d megapixels this tool decodes to scale an image, so it is not shown.", limits.pixels/1_000_000))
	}

	frames := 1
	lossy := mimeType == "image/jpeg"
	switch mimeType {
	case "image/gif":
		frames = max(1, gifFrames(content))
	case "image/webp":
		var animated bool
		if lossy, animated = webpFeatures(content); animated {
			return notShown("an animated WebP image of "+stored, "Its frames cannot be decoded here, so it is not shown.")
		}
	}

	returned := &Image{Width: config.Width, Height: config.Height, ReturnedWidth: config.Width, ReturnedHeight: config.Height}
	if frames > 1 {
		returned.Frames = frames
	}

	// Decoded even when it is returned as it is: an image a client cannot
	// decode can fail the whole request it is sent in, not just this answer.
	decoded, err := decodeImage(mimeType, content, config)
	if err != nil {
		return notShown(fmt.Sprintf("a %s image of %s", name, stored), "It cannot be decoded, so it is not shown.")
	}

	var text strings.Builder
	text.WriteString(subject + ": ")
	if frames > 1 {
		fmt.Fprintf(&text, "an animated %s image of %d frames, %s, %s.", name, frames, stored, formatSize(size))
	} else {
		fmt.Fprintf(&text, "a %s image, %s, %s.", name, stored, formatSize(size))
	}

	if frames == 1 && len(content) <= limits.bytes && max(config.Width, config.Height) <= limits.edge {
		returned.Data, returned.MIMEType = content, mimeType
		text.WriteString(" It follows as an image.")

		return View{Kind: KindImage, MIMEType: mimeType, Size: size, Text: text.String(), Image: returned}
	}

	encoded, asJPEG, width, height, ok := fitImage(decoded, lossy, limits)
	if !ok {
		return notShown(fmt.Sprintf("a %s image of %s", name, stored), fmt.Sprintf(
			"It could not be made smaller than the %s an image is returned in, so it is not shown.", formatSize(int64(limits.bytes))))
	}

	returned.Data, returned.ReturnedWidth, returned.ReturnedHeight = encoded, width, height
	returned.MIMEType = "image/png"
	returnedName := "PNG"
	if asJPEG {
		returned.MIMEType, returnedName = "image/jpeg", "JPEG"
	}
	returned.Scaled = width != config.Width || height != config.Height

	follows := " It follows"
	if frames > 1 {
		follows = " Its first frame follows"
	}
	switch {
	case returned.Scaled:
		fmt.Fprintf(&text, "%s scaled down to %dx%d pixels, as a %s of %s, to fit the %d pixels and %s an image is returned in. "+
			"Small text in it may no longer be legible because of the scaling.",
			follows, width, height, returnedName, formatSize(int64(len(encoded))), limits.edge, formatSize(int64(limits.bytes)))
	case frames > 1:
		fmt.Fprintf(&text, "%s, as a %s image of %s.", follows, returnedName, formatSize(int64(len(encoded))))
	default:
		fmt.Fprintf(&text, "%s as a %s of %s, encoded again to fit the %s an image is returned in.",
			follows, returnedName, formatSize(int64(len(encoded))), formatSize(int64(limits.bytes)))
	}

	return View{Kind: KindImage, MIMEType: mimeType, Size: size, Text: text.String(), Image: returned}
}

// fitImage encodes an image small enough to return: no longer than the edge
// limit on its long side, and no larger than the byte limit.
//
// A lossy source -- a JPEG, a lossy WebP -- is a photograph, and goes back as
// a JPEG; anything else goes back as a PNG, which keeps the sharp edges of a
// screenshot or a diagram sharp. A PNG that is still too large, with nothing
// transparent in it, is a photograph after all and becomes a JPEG: a larger
// picture keeps more of it legible than a smaller lossless one. Only after
// that does the picture shrink, by the square root of how far over it is, and
// a little more, since an encoded size is not quite proportional to area.
//
// It gives up after a few rounds, which only an image no encoder can shrink
// would take, and when an encoder fails.
func fitImage(decoded image.Image, lossy bool, limits imageLimits) (encoded []byte, asJPEG bool, width, height int, ok bool) {
	bounds := decoded.Bounds()
	asJPEG = lossy && opaque(decoded)
	width, height = fit(bounds.Dx(), bounds.Dy(), limits.edge)

	for range 8 {
		scaled := decoded
		if width != bounds.Dx() || height != bounds.Dy() {
			scaled = scale(decoded, width, height)
		}

		encoded, err := encodeImage(scaled, asJPEG)
		if err != nil {
			return nil, false, 0, 0, false
		}
		if len(encoded) <= limits.bytes {
			return encoded, asJPEG, width, height, true
		}

		if !asJPEG && opaque(scaled) {
			asJPEG = true

			continue
		}

		ratio := math.Sqrt(float64(limits.bytes)/float64(len(encoded))) * 0.9
		width, height = max(1, int(float64(width)*ratio)), max(1, int(float64(height)*ratio))
	}

	return nil, false, 0, 0, false
}

// fit scales width and height down, keeping their proportion, until the
// longer is no more than edge.
func fit(width, height, edge int) (int, int) {
	longest := max(width, height)
	if longest <= edge {
		return width, height
	}

	ratio := float64(edge) / float64(longest)

	return max(1, int(math.Round(float64(width)*ratio))), max(1, int(math.Round(float64(height)*ratio)))
}

// scale resamples an image to width by height with Catmull-Rom, which keeps
// edges and text crisper than bilinear when shrinking.
func scale(source image.Image, width, height int) image.Image {
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(target, target.Bounds(), source, source.Bounds(), xdraw.Src, nil)

	return target
}

func encodeImage(picture image.Image, asJPEG bool) ([]byte, error) {
	var encoded bytes.Buffer
	var err error
	if asJPEG {
		err = jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: jpegQuality})
	} else {
		err = png.Encode(&encoded, picture)
	}

	return encoded.Bytes(), err
}

// opaque reports an image with no transparency in it, which a JPEG could not
// keep. One that cannot say is taken to have some.
func opaque(picture image.Image) bool {
	if solid, ok := picture.(interface{ Opaque() bool }); ok {
		return solid.Opaque()
	}

	return false
}

func decodeImageConfig(mimeType string, content []byte) (image.Config, error) {
	reader := bytes.NewReader(content)
	switch mimeType {
	case "image/png":
		return png.DecodeConfig(reader)
	case "image/jpeg":
		return jpeg.DecodeConfig(reader)
	case "image/gif":
		return gif.DecodeConfig(reader)
	default:
		return webp.DecodeConfig(reader)
	}
}

// decodeImage decodes an image, or an animated GIF's first frame. A frame may
// cover part of the canvas, so it is drawn onto one the size of the image,
// transparent where the frame does not reach.
func decodeImage(mimeType string, content []byte, config image.Config) (image.Image, error) {
	reader := bytes.NewReader(content)
	switch mimeType {
	case "image/png":
		return png.Decode(reader)
	case "image/jpeg":
		return jpeg.Decode(reader)
	case "image/webp":
		return webp.Decode(reader)
	}

	first, err := gif.Decode(reader)
	if err != nil {
		return nil, err
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(canvas, first.Bounds(), first, first.Bounds().Min, draw.Over)

	return canvas, nil
}

// gifFrames counts a GIF's frames by walking its blocks, without decoding a
// single one: decoding them all to count them would hold every frame of an
// animation in memory at once.
func gifFrames(content []byte) int {
	const header = 13 // signature, version and logical screen descriptor
	if len(content) < header {
		return 0
	}

	offset := header + colorTableSize(content[10])
	frames := 0
	for offset < len(content) {
		switch content[offset] {
		case 0x21: // an extension: its label, then data sub-blocks
			offset = skipSubBlocks(content, offset+2)
		case 0x2C: // an image descriptor: a frame
			frames++
			if offset+10 > len(content) {
				return frames
			}
			offset += 10 + colorTableSize(content[offset+9])
			// The LZW minimum code size, then the image data's sub-blocks.
			offset = skipSubBlocks(content, offset+1)
		default: // the trailer, or something that is not a block
			return frames
		}
	}

	return frames
}

// colorTableSize is the size of the color table a GIF's packed flags declare.
func colorTableSize(flags byte) int {
	if flags&0x80 == 0 {
		return 0
	}

	return 3 << ((flags & 0x07) + 1)
}

// skipSubBlocks steps over a run of GIF data sub-blocks, each a length byte
// and that many bytes, ended by a zero length.
func skipSubBlocks(content []byte, offset int) int {
	for offset < len(content) {
		length := int(content[offset])
		offset++
		if length == 0 {
			return offset
		}
		offset += length
	}

	return offset
}

// webpFeatures reads a WebP's chunks for what the decoder does not say: whether
// its pixels were compressed lossily, and whether it is an animation, which
// the decoder cannot read at all.
func webpFeatures(content []byte) (lossy, animated bool) {
	const riffHeader = 12 // "RIFF", the length, "WEBP"
	for offset := riffHeader; offset+8 <= len(content); {
		length := int64(binary.LittleEndian.Uint32(content[offset+4 : offset+8]))

		switch string(content[offset : offset+4]) {
		case "VP8X":
			const animationFlag = 1 << 1
			if offset+8 < len(content) && content[offset+8]&animationFlag != 0 {
				return false, true
			}
		case "VP8 ":
			return true, false
		case "VP8L":
			return false, false
		case "ANIM", "ANMF":
			return false, true
		}

		next := int64(offset) + 8 + length + length&1
		if next > int64(len(content)) {
			break
		}
		offset = int(next)
	}

	return false, false
}
