package fileview

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"strings"

	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

// imageFormats are the formats returned as images, named for a sentence. Each
// is decoded here, to scale it when it has to be, or to convert it.
var imageFormats = map[string]string{
	"image/png":  "PNG",
	"image/jpeg": "JPEG",
	"image/gif":  "GIF",
	"image/webp": "WebP",
	"image/bmp":  "BMP",
	"image/tiff": "TIFF",
}

// clientFormats are the four every client taking images accepts, and can be
// returned as they are. The rest -- BMP, TIFF, which model APIs refuse -- are
// always converted, to a PNG or, for a photograph, a JPEG.
var clientFormats = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
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
// takes, and otherwise decoded, turned upright, scaled down and encoded again,
// with a sentence saying which -- after scaling, small text may no longer be
// legible. An animated image gives its first frame. One that cannot be decoded
// is described instead.
//
// The only error is ctx's: a picture of tens of megapixels takes a second or
// two to decode and scale, and a cancelled call stops at the decoder's next
// read, or between one step and the next.
func readImage(ctx context.Context, request Request, mimeType string, content []byte, limits imageLimits) (View, error) {
	name := imageFormats[mimeType]
	subject := subjectOf(request)
	size := int64(len(content))

	notShown := func(what, why string) (View, error) {
		return describeBinary(subject, request.WebURL, binaryType{mimeType: mimeType, name: what, reason: why}, size), nil
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

	frames, pages, orientation := 1, 1, 1
	lossy := mimeType == "image/jpeg"
	switch mimeType {
	case "image/jpeg":
		orientation = jpegOrientation(content)
	case "image/gif":
		frames = max(1, gifFrames(content))
	case "image/webp":
		features := webpFeatures(content)
		if features.animated {
			return notShown("an animated WebP image of "+stored, "Its frames cannot be decoded here, so it is not shown.")
		}
		lossy, orientation = features.lossy, exifOrientation(features.exif)
	case "image/tiff":
		// A TIFF carries the orientation tag EXIF borrowed, in its own first
		// directory, which is the first page the decoder reads.
		pages, orientation = max(1, tiffPages(content)), tiffOrientation(content)
	}

	// Decoded even when it is returned as it is: an image a client cannot
	// decode can fail the whole request it is sent in, not just this answer.
	decoded, err := decodeImage(ctx, mimeType, content, config)
	if cause := ctx.Err(); cause != nil {
		return View{}, cause
	}
	if err != nil {
		return notShown(fmt.Sprintf("a %s image of %s", name, stored), "It cannot be decoded, so it is not shown.")
	}

	// Turned before it is scaled, so the limits apply to the picture as it is
	// seen, and every size given is the upright picture's.
	upright := turn(decoded, orientation)
	width, height := upright.Bounds().Dx(), upright.Bounds().Dy()

	returned := &Image{Width: width, Height: height, ReturnedWidth: width, ReturnedHeight: height, Turned: orientation > 1}
	if frames > 1 {
		returned.Frames = frames
	}
	if pages > 1 {
		returned.Pages = pages
	}

	var text strings.Builder
	text.WriteString(subject + ": ")
	switch {
	case frames > 1:
		fmt.Fprintf(&text, "an animated %s image of %d frames, %dx%d pixels, %s.", name, frames, width, height, formatSize(size))
	case pages > 1:
		fmt.Fprintf(&text, "a %s image of %d pages, %dx%d pixels, %s.", name, pages, width, height, formatSize(size))
	default:
		fmt.Fprintf(&text, "a %s image, %dx%d pixels, %s.", name, width, height, formatSize(size))
	}

	// A turned picture is always encoded again, without the tag: returned as
	// it is, it would be upright only to a client that reads the tag. So is a
	// format clients do not take, whatever its size.
	if frames == 1 && !returned.Turned && clientFormats[mimeType] && len(content) <= limits.bytes && max(width, height) <= limits.edge {
		returned.Data, returned.MIMEType = content, mimeType
		text.WriteString(" It follows as an image.")

		return View{Kind: KindImage, MIMEType: mimeType, Size: size, Text: text.String(), Image: returned}, nil
	}

	encoded, asJPEG, scaledWidth, scaledHeight, ok := fitImage(ctx, upright, lossy, limits)
	if cause := ctx.Err(); cause != nil {
		return View{}, cause
	}
	if !ok {
		return notShown(fmt.Sprintf("a %s image of %s", name, stored), fmt.Sprintf(
			"It could not be made smaller than the %s an image is returned in, so it is not shown.", formatSize(int64(limits.bytes))))
	}

	returned.Data, returned.ReturnedWidth, returned.ReturnedHeight = encoded, scaledWidth, scaledHeight
	returned.MIMEType = "image/png"
	if asJPEG {
		returned.MIMEType = "image/jpeg"
	}
	returned.Scaled = scaledWidth != width || scaledHeight != height
	text.WriteString(returnedNote(returned, mimeType, limits))

	return View{Kind: KindImage, MIMEType: mimeType, Size: size, Text: text.String(), Image: returned}, nil
}

// returnedNote says how the image returned came from the one stored: what
// follows, turned or scaled, in what format, converted from what; and why,
// when it was scaled or merely encoded again.
func returnedNote(returned *Image, mimeType string, limits imageLimits) string {
	var note strings.Builder
	switch {
	case returned.Frames > 1:
		note.WriteString(" Its first frame follows")
	case returned.Pages > 1:
		note.WriteString(" Its first page follows")
	default:
		note.WriteString(" It follows")
	}

	var changes []string
	if returned.Turned {
		// A TIFF records the tag in its own right; everything else in Exif.
		source := "EXIF"
		if mimeType == "image/tiff" {
			source = "TIFF"
		}
		changes = append(changes, "turned upright from its "+source+" orientation")
	}
	if returned.Scaled {
		changes = append(changes, fmt.Sprintf("scaled down to %dx%d pixels", returned.ReturnedWidth, returned.ReturnedHeight))
	}
	if len(changes) > 0 {
		note.WriteString(" " + strings.Join(changes, " and ") + ",")
	}
	fmt.Fprintf(&note, " as a %s of %s", strings.ToUpper(strings.TrimPrefix(returned.MIMEType, "image/")), formatSize(int64(len(returned.Data))))
	converted := !clientFormats[mimeType]
	if converted {
		fmt.Fprintf(&note, ", converted from %s, which clients do not take", imageFormats[mimeType])
	}
	note.WriteString(".")

	switch {
	case returned.Scaled:
		fmt.Fprintf(&note, " It was scaled to fit the %d pixels and %s an image is returned in. "+
			"Small text in it may no longer be legible because of the scaling.", limits.edge, formatSize(int64(limits.bytes)))
	case !returned.Turned && !converted && returned.Frames == 0:
		fmt.Fprintf(&note, " It was encoded again to fit the %s an image is returned in.", formatSize(int64(limits.bytes)))
	}

	return note.String()
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
// would take, when an encoder fails, and before a round once ctx is done.
func fitImage(ctx context.Context, decoded image.Image, lossy bool, limits imageLimits) (encoded []byte, asJPEG bool, width, height int, ok bool) {
	bounds := decoded.Bounds()
	asJPEG = lossy && opaque(decoded)
	width, height = fit(bounds.Dx(), bounds.Dy(), limits.edge)

	for range 8 {
		if ctx.Err() != nil {
			return nil, false, 0, 0, false
		}

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

// decodeImageConfig reads an image's size from its header, before anything is
// decoded, so a picture too large to decode is found without decoding it.
func decodeImageConfig(mimeType string, content []byte) (image.Config, error) {
	reader := bytes.NewReader(content)
	switch mimeType {
	case "image/png":
		return png.DecodeConfig(reader)
	case "image/jpeg":
		return jpeg.DecodeConfig(reader)
	case "image/gif":
		return gif.DecodeConfig(reader)
	case "image/bmp":
		return bmp.DecodeConfig(reader)
	case "image/tiff":
		return tiff.DecodeConfig(reader)
	default:
		return webp.DecodeConfig(reader)
	}
}

// decodeImage decodes an image: a TIFF's first page, or an animated GIF's first
// frame. A frame may cover part of the canvas, so it is drawn onto one the
// size of the image, transparent where the frame does not reach. The decoder
// reads through ctx, and fails at its next read once ctx is done.
func decodeImage(ctx context.Context, mimeType string, content []byte, config image.Config) (image.Image, error) {
	reader := &cancellable{ctx: ctx, reader: bytes.NewReader(content)}
	switch mimeType {
	case "image/png":
		return png.Decode(reader)
	case "image/jpeg":
		return jpeg.Decode(reader)
	case "image/webp":
		return webp.Decode(reader)
	case "image/bmp":
		return bmp.Decode(reader)
	case "image/tiff":
		return tiff.Decode(reader)
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

// webpInfo is what a WebP's chunks say that the decoder does not.
type webpInfo struct {
	// lossy is set when the pixels were compressed lossily.
	lossy bool
	// animated is set for an animation, which the decoder cannot read at all.
	animated bool
	// exif is the EXIF chunk, which follows the pixels, or nil.
	exif []byte
}

// maxWebPChunks is the most chunks read: a still WebP has at most six.
const maxWebPChunks = 64

// webpFeatures reads a WebP's chunks.
func webpFeatures(content []byte) webpInfo {
	var info webpInfo

	const riffHeader = 12 // "RIFF", the length, "WEBP"
	offset := riffHeader
	for range maxWebPChunks {
		if offset+8 > len(content) {
			break
		}
		length := int64(binary.LittleEndian.Uint32(content[offset+4 : offset+8]))
		end := min(int64(len(content)), int64(offset)+8+length)
		body := content[offset+8 : int(end)]

		switch string(content[offset : offset+4]) {
		case "VP8X":
			const animationFlag = 1 << 1
			if len(body) > 0 && body[0]&animationFlag != 0 {
				info.animated = true
			}
		case "VP8 ":
			info.lossy = true
		case "ANIM", "ANMF":
			info.animated = true
		case "EXIF":
			info.exif = body
		}

		next := int64(offset) + 8 + length + length&1
		if next > int64(len(content)) {
			break
		}
		offset = int(next)
	}

	return info
}
