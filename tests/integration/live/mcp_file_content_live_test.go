//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// liveFileAnswer is get_file_content's structured answer.
type liveFileAnswer struct {
	Path          string  `json:"path"`
	At            string  `json:"at"`
	Kind          string  `json:"kind"`
	MIMEType      string  `json:"mime_type"`
	Size          *int64  `json:"size"`
	WebURL        string  `json:"web_url"`
	Content       *string `json:"content"`
	StartLine     *int    `json:"start_line"`
	EndLine       *int    `json:"end_line"`
	TotalLines    *int    `json:"total_lines"`
	NextStartLine *int    `json:"next_start_line"`
}

// liveNumberedLines is count lines, each naming its own number, so a window
// that holds the wrong lines cannot hold the right text.
func liveNumberedLines(count int) []byte {
	var text bytes.Buffer
	for number := 1; number <= count; number++ {
		fmt.Fprintf(&text, "line %04d: pushed to Bitbucket and read back through the MCP server\n", number)
	}

	return text.Bytes()
}

// TestLiveMCPGetFileContentReadsEachKindOfFile covers get_file_content against
// files Bitbucket is storing, one of each kind the tool converts.
//
// What comes back depends on the bytes the raw endpoint answers with, so no
// unit test can hold it: the converters are tested on content built in their
// tests, and this is where the bytes are Bitbucket's. Every file is pushed in
// one commit and read at that ref, and each answer is checked value by value.
func TestLiveMCPGetFileContentReadsEachKindOfFile(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const at = "refs/heads/master"
	long := liveNumberedLines(3000)
	binary := liveBinaryFile()

	if err := harness.pushFilesOnBranch(seeded.Key, repo.Slug, "master", map[string][]byte{
		"docs/long.txt":   long,
		"assets/blob.bin": binary,
	}); err != nil {
		t.Fatalf("push the files: %v", err)
	}

	webURL := func(path string) string {
		return fmt.Sprintf("%s/projects/%s/repos/%s/browse/%s?at=%s",
			strings.TrimRight(harness.config.BitbucketURL, "/"), seeded.Key, repo.Slug, path, url.QueryEscape(at))
	}

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		read := func(t *testing.T, path string, window map[string]any) (*mcp.CallToolResult, liveFileAnswer) {
			t.Helper()

			args := map[string]any{"project": seeded.Key, "repo": repo.Slug, "path": path, "at": at}
			for key, value := range window {
				args[key] = value
			}

			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_file_content", Arguments: args})
			if err != nil {
				t.Fatalf("get_file_content %s: protocol error: %v", path, err)
			}
			if result.IsError {
				t.Fatalf("get_file_content %s: error result: %s", path, mcpResultText(result))
			}

			encoded, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatalf("get_file_content %s: marshal structuredContent: %v", path, err)
			}
			var answer liveFileAnswer
			if err := json.Unmarshal(encoded, &answer); err != nil {
				t.Fatalf("get_file_content %s: decode structuredContent %s: %v", path, encoded, err)
			}
			if answer.Path != path || answer.At != at || answer.WebURL != webURL(path) {
				t.Errorf("get_file_content %s answered for %q at %q, page %q; want %q", path, answer.Path, answer.At, answer.WebURL, webURL(path))
			}

			return result, answer
		}

		t.Run("a window from the middle of a long text file", func(t *testing.T) {
			result, answer := read(t, "docs/long.txt", map[string]any{"start_line": 1200, "line_count": 100})

			if answer.Kind != "text" || answer.Size == nil || *answer.Size != int64(len(long)) {
				t.Fatalf("docs/long.txt came back as %q of %v bytes, want text of %d", answer.Kind, answer.Size, len(long))
			}
			if answer.StartLine == nil || *answer.StartLine != 1200 || answer.EndLine == nil || *answer.EndLine != 1299 ||
				answer.TotalLines == nil || *answer.TotalLines != 3000 || answer.NextStartLine == nil || *answer.NextStartLine != 1300 {
				t.Fatalf("window = %v-%v of %v, next %v; want 1200-1299 of 3000, next 1300",
					deref(answer.StartLine), deref(answer.EndLine), deref(answer.TotalLines), deref(answer.NextStartLine))
			}

			lines := strings.SplitAfter(string(long), "\n")
			if want := strings.Join(lines[1199:1299], ""); answer.Content == nil || *answer.Content != want {
				t.Errorf("content is not lines 1200-1299 of the file as pushed")
			}

			var numbered strings.Builder
			for index := 1199; index < 1299; index++ {
				fmt.Fprintf(&numbered, "%6d\t%s", index+1, lines[index])
			}
			want := "docs/long.txt at " + at + ": text, 3000 lines. Lines 1200-1299 follow; for the next, pass start_line=1300.\n" + numbered.String()
			if got := mcpResultText(result); got != want {
				t.Errorf("the text a model reads is not the header and lines 1200-1299 numbered:\n%.400s", got)
			}
		})

		t.Run("an opaque binary is described by its type and size", func(t *testing.T) {
			result, answer := read(t, "assets/blob.bin", nil)

			if answer.Kind != "binary" || answer.MIMEType != "application/octet-stream" || answer.Size == nil || *answer.Size != int64(len(binary)) {
				t.Fatalf("assets/blob.bin came back as %q %q of %v bytes, want binary application/octet-stream of %d",
					answer.Kind, answer.MIMEType, deref64(answer.Size), len(binary))
			}
			if answer.Content != nil || answer.StartLine != nil {
				t.Errorf("a binary file came back with lines")
			}

			if len(result.Content) != 1 {
				t.Fatalf("a binary file came back as %d content blocks, want its description alone", len(result.Content))
			}
			text := mcpResultText(result)
			want := "assets/blob.bin at " + at + ": a binary file (application/octet-stream), 97.7 KiB. " +
				"Its bytes are not shown: they are not text, and not a kind of file this tool converts. " +
				"A person can open it in Bitbucket at " + webURL("assets/blob.bin")
			if text != want {
				t.Errorf("description:\n got %q\nwant %q", text, want)
			}
		})
	}, "ai", "mcp", "serve")
}

func deref(value *int) any {
	if value == nil {
		return nil
	}

	return *value
}

func deref64(value *int64) any {
	if value == nil {
		return nil
	}

	return *value
}
