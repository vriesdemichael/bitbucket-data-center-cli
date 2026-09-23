package completionsetup

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// ownMarker opens a file bb writes whole, and is how Remove knows the file is
// bb's to delete.
const ownMarker = "# Written by `bb completion install`; `bb completion remove` deletes this file."

// ownFile is the loader for a shell that reads a file named after the
// command: bash-completion, fish, and zsh from its fpath all look one up the
// first time bb is completed, so none of them pays anything when a shell
// starts.
func ownFile(shell Shell) string {
	const follows = "# It loads the script from bb itself, so it follows every upgrade.\n"

	switch shell {
	case Bash:
		return ownMarker + "\n" + follows +
			"if command -v bb >/dev/null 2>&1; then\n" +
			"    source <(bb completion bash)\n" +
			"fi\n"
	case Fish:
		return ownMarker + "\n" + follows +
			"if command -q bb\n" +
			"    bb completion fish | source\n" +
			"end\n"
	case Zsh:
		// An autoloaded function, which zsh finds by the #compdef on its first
		// line. The script it sources defines _bb over this one and, seeing it
		// is running as _bb, completes -- the same thing the script does when
		// it is the file.
		return "#compdef bb\n" + ownMarker + "\n" + follows +
			"(( $+commands[bb] )) || return 1\n" +
			"source <(bb completion zsh)\n"
	default:
		return ""
	}
}

// isOwnFile looks for the marker on one of the first lines: second, after
// zsh's #compdef, and first everywhere else.
func isOwnFile(content []byte) bool {
	for _, line := range firstLines(content, 3) {
		if line == ownMarker {
			return true
		}
	}

	return false
}

// isGeneratedScript reports a script `bb completion <shell>` printed and
// somebody saved, as the documentation once said to. It is bb's completion,
// so installing replaces it and removing deletes it; a file that is neither
// this nor bb's own loader belongs to somebody else and is left alone.
func isGeneratedScript(shell Shell, content []byte) bool {
	// "# bash completion V2 for bb", "# fish completion for bb", and for zsh
	// the same line a few below its #compdef.
	for _, line := range firstLines(content, 5) {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "#" || fields[1] != string(shell) || fields[2] != "completion" {
			continue
		}
		for index := 3; index+1 < len(fields); index++ {
			if fields[index] == "for" && fields[index+1] == "bb" {
				return true
			}
		}
	}

	return false
}

func firstLines(content []byte, count int) []string {
	found := make([]string, 0, count)
	for _, line := range strings.SplitN(string(content), "\n", count+1) {
		if len(found) == count {
			break
		}
		found = append(found, strings.TrimRight(line, "\r"))
	}

	return found
}

func installFile(target Target) (Outcome, error) {
	want := ownFile(target.Shell)

	existing, err := os.ReadFile(target.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := writeFile(target.Path, []byte(want), 0o644, target.Scope); err != nil {
			return Outcome{}, err
		}

		return Outcome{Target: target, Status: Installed}, nil
	case err != nil:
		return Outcome{}, apperrors.New(apperrors.KindInternal, "failed to read "+target.Path, err)
	}

	switch {
	case isOwnFile(existing) && string(existing) == want:
		return Outcome{Target: target, Status: Unchanged}, nil
	case isOwnFile(existing):
		if err := writeFile(target.Path, []byte(want), 0o644, target.Scope); err != nil {
			return Outcome{}, err
		}

		return Outcome{Target: target, Status: Updated}, nil
	case isGeneratedScript(target.Shell, existing):
		if err := writeFile(target.Path, []byte(want), 0o644, target.Scope); err != nil {
			return Outcome{}, err
		}

		return Outcome{Target: target, Status: Updated, Note: "replaced a script saved from an earlier bb, which does not follow an upgrade"}, nil
	default:
		return Outcome{}, apperrors.New(apperrors.KindConflict, fmt.Sprintf(
			"%s is there and bb did not write it; move it aside and run this again, or keep it and leave completion as it is",
			target.Path), nil)
	}
}

func removeFile(target Target) (Outcome, error) {
	existing, err := os.ReadFile(target.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return Outcome{Target: target, Status: NotFound}, nil
	case err != nil:
		return Outcome{}, apperrors.New(apperrors.KindInternal, "failed to read "+target.Path, err)
	}

	if !isOwnFile(existing) && !isGeneratedScript(target.Shell, existing) {
		return Outcome{Target: target, Status: NotFound, Note: target.Path + " is not bb's, so it was left alone"}, nil
	}

	if err := os.Remove(target.Path); err != nil {
		return Outcome{}, apperrors.New(apperrors.KindInternal, "failed to remove "+target.Path, err)
	}

	return Outcome{Target: target, Status: Removed}, nil
}

// beginMarker and endMarker bracket the block bb adds to a file it shares, so
// a second install replaces it and a remove finds exactly what to take out.
const (
	beginMarker = "# >>> bb completion >>>"
	endMarker   = "# <<< bb completion <<<"
)

// block is the loader for a shell that reads it from a file of its owner's,
// markers included, with the file's own line endings.
//
// Each checks that bb is still there before running it, so uninstalling bb
// without removing this leaves a shell that starts quietly rather than one
// that reports a missing command every time.
func block(shell Shell, newline string) string {
	var lines []string

	switch shell {
	case Zsh:
		lines = []string{
			beginMarker,
			"# Added by `bb completion install`; `bb completion remove` takes it out.",
			"# compdef is there once compinit has run, which is what a completion needs.",
			"if (( $+commands[bb] && $+functions[compdef] )); then",
			"    source <(bb completion zsh)",
			"fi",
			endMarker,
		}
	case PowerShell:
		lines = []string{
			beginMarker,
			"# Added by `bb completion install`; `bb completion remove` takes it out.",
			"if (Get-Command bb -CommandType Application -ErrorAction SilentlyContinue) {",
			"    bb completion powershell | Out-String | Invoke-Expression",
			"}",
			endMarker,
		}
	default:
		return ""
	}

	return strings.Join(lines, newline) + newline
}

func installBlock(target Target) (Outcome, error) {
	file, err := readText(target.Path, defaultNewline(target))
	if err != nil {
		return Outcome{}, err
	}

	want := block(target.Shell, file.newline)

	start, end, found, err := findBlock(file.text)
	if err != nil {
		return Outcome{}, apperrors.New(apperrors.KindConflict, fmt.Sprintf("%s: %v", target.Path, err), nil)
	}

	status := Installed
	switch {
	case found && file.text[start:end] == want:
		return Outcome{Target: target, Status: Unchanged}, nil
	case found:
		file.text = file.text[:start] + want + file.text[end:]
		status = Updated
	default:
		file.text = appendBlock(file.text, want, file.newline)
	}

	if err := file.write(target.Path, target.Scope); err != nil {
		return Outcome{}, err
	}

	return Outcome{Target: target, Status: status}, nil
}

func removeBlock(target Target) (Outcome, error) {
	file, err := readText(target.Path, defaultNewline(target))
	if err != nil {
		return Outcome{}, err
	}
	if !file.exists {
		return Outcome{Target: target, Status: NotFound}, nil
	}

	start, end, found, err := findBlock(file.text)
	if err != nil {
		return Outcome{}, apperrors.New(apperrors.KindConflict, fmt.Sprintf("%s: %v", target.Path, err), nil)
	}
	if !found {
		return Outcome{Target: target, Status: NotFound}, nil
	}

	before := file.text[:start]
	// The blank line appendBlock put in front of the block goes with it.
	if strings.HasSuffix(before, file.newline+file.newline) {
		before = strings.TrimSuffix(before, file.newline)
	}
	file.text = before + file.text[end:]

	if err := file.write(target.Path, target.Scope); err != nil {
		return Outcome{}, err
	}

	return Outcome{Target: target, Status: Removed}, nil
}

// appendBlock puts the block at the end, where it runs after whatever the file
// sets up first -- compinit, in a .zshrc -- and after a blank line, so it reads
// as the separate thing it is.
//
// Always exactly one blank line, even after a file that already ends in one:
// removeBlock takes one out with the block, and a file has to come back as it
// was.
func appendBlock(text, block, newline string) string {
	if text == "" {
		return block
	}
	if !strings.HasSuffix(text, "\n") {
		text += newline
	}

	return text + newline + block
}

// findBlock locates bb's block: from the start of the line with the begin
// marker to the end of the line with the end marker. A begin marker with no end
// marker after it is a block somebody has edited, which is not bb's to guess
// the extent of.
func findBlock(text string) (start, end int, found bool, err error) {
	start = markerLine(text, beginMarker, 0)
	if start < 0 {
		return 0, 0, false, nil
	}

	endLine := markerLine(text, endMarker, start)
	if endLine < 0 {
		return 0, 0, false, errors.New("the block bb added has lost its end marker (" + endMarker + "); remove it by hand")
	}

	end = endLine + len(endMarker)
	switch {
	case strings.HasPrefix(text[end:], "\r\n"):
		end += 2
	case strings.HasPrefix(text[end:], "\n"):
		end++
	}

	return start, end, true, nil
}

// markerLine is the offset of a line that is exactly marker, searching from
// offset, or -1.
func markerLine(text, marker string, offset int) int {
	for offset <= len(text) {
		index := strings.Index(text[offset:], marker)
		if index < 0 {
			return -1
		}

		at := offset + index
		after := at + len(marker)
		startsLine := at == 0 || text[at-1] == '\n'
		endsLine := after == len(text) || text[after] == '\n' || text[after] == '\r'
		if startsLine && endsLine {
			return at
		}

		offset = after
	}

	return -1
}

func defaultNewline(target Target) string {
	if target.Shell == PowerShell && filepath.Separator == '\\' {
		return "\r\n"
	}

	return "\n"
}

// textFile is a file bb edits a part of, with what it needs to write it back
// as it found it.
//
// Windows PowerShell 5.1 writes UTF-16 when a profile is created with `>` or
// Out-File, so a profile can be in either encoding, and appending UTF-8 to a
// UTF-16 file would corrupt the whole of it. The byte order mark, the
// encoding and the line endings all go back the way they came.
type textFile struct {
	path    string
	exists  bool
	bom     []byte
	order   binary.ByteOrder
	newline string
	mode    os.FileMode
	text    string
}

var (
	utf8BOM    = []byte{0xEF, 0xBB, 0xBF}
	utf16LEBOM = []byte{0xFF, 0xFE}
	utf16BEBOM = []byte{0xFE, 0xFF}
)

func readText(path, newline string) (textFile, error) {
	file := textFile{path: path, newline: newline, mode: 0o644}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return textFile{}, apperrors.New(apperrors.KindInternal, "failed to read "+path, err)
	}
	file.exists = true

	if info, err := os.Stat(path); err == nil {
		file.mode = info.Mode().Perm()
	}

	switch {
	case bytes.HasPrefix(raw, utf8BOM):
		file.bom = utf8BOM
		file.text = string(raw[len(utf8BOM):])
	case bytes.HasPrefix(raw, utf16LEBOM):
		file.bom, file.order = utf16LEBOM, binary.LittleEndian
		file.text, err = decodeUTF16(raw[len(utf16LEBOM):], binary.LittleEndian)
	case bytes.HasPrefix(raw, utf16BEBOM):
		file.bom, file.order = utf16BEBOM, binary.BigEndian
		file.text, err = decodeUTF16(raw[len(utf16BEBOM):], binary.BigEndian)
	default:
		file.text = string(raw)
	}
	if err != nil {
		return textFile{}, apperrors.New(apperrors.KindInternal, "failed to read "+path, err)
	}

	switch {
	case strings.Contains(file.text, "\r\n"):
		file.newline = "\r\n"
	case strings.Contains(file.text, "\n"):
		file.newline = "\n"
	}

	return file, nil
}

func decodeUTF16(raw []byte, order binary.ByteOrder) (string, error) {
	if len(raw)%2 != 0 {
		return "", errors.New("a UTF-16 file with an odd number of bytes")
	}

	units := make([]uint16, len(raw)/2)
	for index := range units {
		units[index] = order.Uint16(raw[2*index:])
	}

	return string(utf16.Decode(units)), nil
}

func (file textFile) write(path string, scope Scope) error {
	var encoded []byte
	if file.order != nil {
		units := utf16.Encode([]rune(file.text))
		encoded = make([]byte, 2*len(units))
		for index, unit := range units {
			file.order.PutUint16(encoded[2*index:], unit)
		}
	} else {
		encoded = []byte(file.text)
	}

	return writeFile(path, append(append([]byte{}, file.bom...), encoded...), file.mode, scope)
}

// writeFile replaces path through a temporary file beside it, so a failure
// part way through leaves the old file rather than half of a new one -- a
// shell profile cut short is a shell that fails to start cleanly.
func writeFile(path string, content []byte, mode os.FileMode, scope Scope) error {
	directory := filepath.Dir(path)
	if err := makeDirectory(directory, scope); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to create "+directory, err)
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".bb-*")
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to write "+path, err)
	}
	defer func() { _ = os.Remove(temporary.Name()) }()

	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()

		return apperrors.New(apperrors.KindInternal, "failed to write "+path, err)
	}
	if err := temporary.Close(); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to write "+path, err)
	}
	if err := os.Chmod(temporary.Name(), mode); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to write "+path, err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to write "+path, err)
	}

	return nil
}

// makeDirectory creates where a setup goes. A user's own directories are kept
// to the user; one for every user has to be readable by all of them, or the
// shells of everyone but the administrator who ran bb could not read it.
func makeDirectory(directory string, scope Scope) error {
	if scope == AllUsers {
		// #nosec G301 -- read by every user's shell, like the directories
		// around it under /usr/local/share and /etc.
		return os.MkdirAll(directory, 0o755)
	}

	return os.MkdirAll(directory, 0o750)
}
