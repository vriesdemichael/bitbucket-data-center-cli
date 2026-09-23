package fileview

// The numbers that bound what one read of a file costs: memory here, and
// context in the model that reads the answer.

// MaxFileBytes is the most of a file that is read. The whole file is held in
// memory to be converted, so something has to bound it, and 64 MiB is what bb
// repo cat --json holds for the same reason. Past it a text file is more than
// anyone pages through a window at a time, and a picture or a document is
// larger than the few megabytes that come back of it anyway.
const MaxFileBytes = 64 << 20

// DefaultLineCount is how many lines a window holds when the caller does not
// say. Most source files fit in one, so they come back whole in one call.
const DefaultLineCount = 500

// MaxLineCount is the most lines a window holds. WindowBytes is what bounds a
// window's size in the context; this only matters for short lines, where it
// keeps a window of blank or one-word lines from becoming mostly line numbers.
const MaxLineCount = 2000

// WindowBytes is the most text one window holds, line numbers included: about
// eight thousand tokens. That leaves room for several windows in a context, and
// stays under the ten thousand tokens past which Claude Code warns about the
// size of a tool's answer. A line longer than this on its own is cut to fit, so
// one enormous line -- minified code, a data blob -- cannot flood the context.
const WindowBytes = 32 << 10

// ImageBytes is the most an image is returned in. A tool result carries it as
// base64, a third larger, so these 3,750,000 bytes are 5,000,000 characters:
// under the 5 MB Anthropic's API takes for one image, which is the tightest
// such limit among the clients that take images at all.
const ImageBytes = 3_750_000

// ImageEdge is the longest side, in pixels, an image is returned at. Vision
// models look at no more than this -- OpenAI's fit an image into 2048 by 2048,
// Anthropic's scale its long edge to 1568 -- so a larger one costs bytes and
// shows the model nothing more, and Anthropic's refuse one past 8000 outright.
const ImageEdge = 2048

// ImagePixels is the largest image decoded to be scaled: up to 200 MB at four
// bytes a pixel. It is more than a 45-megapixel camera produces; an image past
// it is described rather than decoded, so one call cannot take the memory of a
// server that other calls share.
const ImagePixels = 50_000_000

// jpegQuality is what an image re-encoded as JPEG is written at: the usual
// point past which a photograph's artefacts stop being visible and fine print
// in it stays sharp.
const jpegQuality = 85
