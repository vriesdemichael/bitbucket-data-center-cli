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

// MediaBytes is the most audio or video that is returned. Neither can be made
// smaller here, so a file over this is described instead. It is the same
// budget as an image's, for the same reason: base64 makes it 5,000,000
// characters, about the most a client takes in one piece of content.
const MediaBytes = ImageBytes

// jpegQuality is what an image re-encoded as JPEG is written at: the usual
// point past which a photograph's artefacts stop being visible and fine print
// in it stays sharp.
const jpegQuality = 85

// DocumentTextBytes is the most text extracted from a Word, PowerPoint or Excel
// file. The text is held whole to be cut into windows, so it needs a bound, and
// 8 MiB is some two million tokens: more than a model pages through, and room
// for any document written to be read rather than a data dump in a workbook.
const DocumentTextBytes = 8 << 20

// documentXMLBytes is the most XML read out of such a file, decompressed. Its
// parts are zipped, and a small zip can expand without end; this bounds the
// work of one read -- a few seconds of parsing -- well past what the text
// bound usually stops first.
const documentXMLBytes = 128 << 20

// ArchiveEntries is the most entries an archive's listing holds. A line each,
// that is a few megabytes of listing, and more than anyone reads a window at a
// time; the header says when there are more.
const ArchiveEntries = 100_000

// archiveExpandBytes is the most a gzip-compressed tar is expanded to list it.
// A tar has no index, so listing one means reading all of it, and a small
// compressed file can expand without end; this bounds the work of one read.
const archiveExpandBytes = 1 << 30
