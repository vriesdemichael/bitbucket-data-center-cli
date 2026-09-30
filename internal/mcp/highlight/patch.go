package highlight

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Patch returns the spans of each code line of each file of a unified diff,
// keyed by the file's index among the patch's files, in patch order. A file's
// slice has one entry per code line, in the order the patch lists them. A
// file has no key when no lexer matches its name, it has no hunks, its part
// of the patch is larger than MaxBytes, or the deadline passes before it is
// done; files after the deadline are not started.
//
// Each hunk is tokenized as its two sides, the old (context and removed
// lines) and the new (context and added lines), so that a comment or string
// spanning lines colours every line of it. A removed line takes its spans
// from the old side, an added or context line from the new.
func Patch(patch string, options Options) map[int][]string {
	highlighted := map[int][]string{}
	for index, file := range parsePatch(patch) {
		if time.Now().After(options.Deadline) {
			break
		}
		if spans, ok := file.spans(options); ok {
			highlighted[index] = spans
		}
	}
	return highlighted
}

// patchFile is one file of a patch, as far as highlighting needs it.
type patchFile struct {
	oldPath, newPath string
	deleted          bool
	binary           bool
	size             int // bytes of the patch from the file's header to the next
	hunks            [][]codeLine
}

// codeLine is a line of a hunk: its mark (+, - or a space) and its text,
// without the \r of a \r\n it ended in.
type codeLine struct {
	mark byte
	text string
}

// The patterns are the view's own, from parseDiff in views/diff.js, so that
// the two read the same files, hunks and code lines out of a patch. A
// JavaScript . matches no \r, U+2028 or U+2029, where a Go . matches all
// three, so the patterns spell out what the view's . matches.
var (
	patchPaths = regexp.MustCompile(`^diff --git (?:a/|src://)([^\r\x{2028}\x{2029}]*) (?:b/|dst://)([^\r\x{2028}\x{2029}]*)$`)
	hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@[^\r\x{2028}\x{2029}]*$`)
)

// parsePatch reads a unified diff, as git and Bitbucket write one, the way
// parseDiff in views/diff.js does: a file starts at each diff --git line, a
// hunk at each @@ header, and nothing before a file's first hunk is code.
// Inside the hunks, a line starting with +, - or a space is code; one
// starting with \, such as "\ No newline at end of file", is not, and
// neither is any other.
func parsePatch(patch string) []*patchFile {
	var (
		files     []*patchFile
		file      *patchFile
		hunk      *[]codeLine
		fileStart int
		offset    int
	)
	for line := range strings.SplitSeq(patch, "\n") {
		lineStart := offset
		offset += len(line) + 1
		if strings.HasPrefix(line, "diff --git ") {
			if file != nil {
				file.size = lineStart - fileStart
			}
			file, hunk, fileStart = &patchFile{}, nil, lineStart
			if paths := patchPaths.FindStringSubmatch(line); paths != nil {
				file.oldPath, file.newPath = paths[1], paths[2]
			}
			files = append(files, file)
			continue
		}
		if file == nil {
			continue
		}
		if hunkHeader.MatchString(line) {
			file.hunks = append(file.hunks, nil)
			hunk = &file.hunks[len(file.hunks)-1]
			continue
		}
		if hunk == nil {
			file.readHeader(line)
			continue
		}
		if line == "" {
			continue
		}
		switch mark := line[0]; mark {
		case '+', '-', ' ':
			*hunk = append(*hunk, codeLine{mark: mark, text: strings.TrimSuffix(line[1:], "\r")})
		}
	}
	if file != nil {
		file.size = len(patch) - fileStart
	}
	return files
}

// readHeader reads a line between a file's diff --git line and its first
// hunk, as parseDiff does: what it says of the file's paths, whether the file
// was deleted, and whether it is binary.
func (f *patchFile) readHeader(line string) {
	switch {
	case strings.HasPrefix(line, "new file mode"):
		f.deleted = false
	case strings.HasPrefix(line, "deleted file mode"):
		f.deleted = true
	case strings.HasPrefix(line, "rename from "):
		f.oldPath, f.deleted = strings.TrimPrefix(line, "rename from "), false
	case strings.HasPrefix(line, "rename to "):
		f.newPath, f.deleted = strings.TrimPrefix(line, "rename to "), false
	case strings.HasPrefix(line, "--- "):
		if name := strings.TrimPrefix(line, "--- "); name == "/dev/null" {
			f.deleted = false
		} else {
			f.oldPath = stripDiffPrefix(name)
		}
	case strings.HasPrefix(line, "+++ "):
		if name := strings.TrimPrefix(line, "+++ "); name == "/dev/null" {
			f.deleted = true
		} else {
			f.newPath = stripDiffPrefix(name)
		}
	case strings.HasPrefix(line, "Binary files "), strings.HasPrefix(line, "GIT binary patch"):
		f.binary = true
	}
}

// stripDiffPrefix removes the a/ or b/ git puts before a path, or the src://
// or dst:// Bitbucket does.
func stripDiffPrefix(name string) string {
	for _, prefix := range []string{"a/", "b/", "src://", "dst://"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			return rest
		}
	}
	return name
}

// languageName is the file name the file's language is read from: the new
// path's, or the old one's for a deleted file, as the view names the file.
// git quotes a path with unusual characters in it, and ends a --- or +++ path
// that has a space in it with a tab; the view shows those as they are, and
// the language is read from the name inside.
func (f *patchFile) languageName() string {
	newPath, oldPath := f.newPath, f.oldPath
	if newPath == "" {
		newPath = oldPath
	}
	if oldPath == "" {
		oldPath = newPath
	}
	name := newPath
	if f.deleted {
		name = oldPath
	}
	name = strings.TrimRight(name, "\t\r")
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		if unquoted, err := strconv.Unquote(name); err == nil {
			name = unquoted
		}
	}
	return path.Base(name)
}

// spans tokenizes the file's hunks and returns the spans of its code lines.
func (f *patchFile) spans(options Options) ([]string, bool) {
	deadline := options.Deadline
	if f.binary || len(f.hunks) == 0 || f.size > MaxBytes {
		return nil, false
	}
	lexer := lexerFor(f.languageName(), options.Templates)
	if lexer == nil {
		return nil, false
	}
	var spans []string
	for _, hunk := range f.hunks {
		var oldSide, newSide strings.Builder
		removes := false
		for _, line := range hunk {
			if line.mark != '+' {
				oldSide.WriteString(line.text)
				oldSide.WriteByte('\n')
			}
			if line.mark != '-' {
				newSide.WriteString(line.text)
				newSide.WriteByte('\n')
			}
			removes = removes || line.mark == '-'
		}
		// The old side of a hunk that removes nothing is context alone, whose
		// spans come from the new side.
		var oldSpans, newSpans []string
		var ok bool
		if removes {
			if oldSpans, ok = tokenize(lexer, oldSide.String(), deadline); !ok {
				return nil, false
			}
		}
		if newSide.Len() > 0 {
			if newSpans, ok = tokenize(lexer, newSide.String(), deadline); !ok {
				return nil, false
			}
		}
		oldLine, newLine := 0, 0
		for _, line := range hunk {
			switch line.mark {
			case '-':
				spans = append(spans, oldSpans[oldLine])
				oldLine++
			case '+':
				spans = append(spans, newSpans[newLine])
				newLine++
			default:
				spans = append(spans, newSpans[newLine])
				oldLine++
				newLine++
			}
		}
	}
	return spans, true
}
