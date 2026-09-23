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
