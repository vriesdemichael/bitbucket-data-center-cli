package viewhost

import (
	"encoding/json"
	"strings"
	"testing"
)

// The page carries the view page, the frames' results and the heading inside
// one script element, so nothing they hold may close it.
func TestPageKeepsWhatItCarriesInsideItsScript(t *testing.T) {
	t.Parallel()

	closing := "</script><script>window.escaped = true</script>"
	result, err := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": closing}}})
	if err != nil {
		t.Fatalf("marshal the result: %v", err)
	}
	page, err := Page("<html><body>"+closing+"</body></html>", []Frame{{Title: closing, Result: result}}, Options{Heading: closing})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if got, want := strings.Count(page, "</script>"), strings.Count(hostTemplate, "</script>"); got != want {
		t.Errorf("the page closes a script %d times, want only the template's own %d", got, want)
	}
	if strings.Contains(page, "/*BB_HOST_") {
		t.Error("the page still holds a placeholder")
	}
}

func TestPageOpensAFrameInlineAndLightUnlessTold(t *testing.T) {
	t.Parallel()

	page, err := Page("<p>view</p>", []Frame{
		{Title: "untold"},
		{Title: "told", Theme: "dark", Mode: "fullscreen"},
	}, Options{})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	frames := framesOf(t, page)
	if len(frames) != 2 {
		t.Fatalf("the page carries %d frames, want 2", len(frames))
	}
	if frames[0]["theme"] != "light" || frames[0]["mode"] != "inline" {
		t.Errorf("a frame that names neither opens %v and %v, want light and inline", frames[0]["theme"], frames[0]["mode"])
	}
	if frames[1]["theme"] != "dark" || frames[1]["mode"] != "fullscreen" {
		t.Errorf("a frame that names both opens %v and %v, want dark and fullscreen", frames[1]["theme"], frames[1]["mode"])
	}
}

// A real host keeps a view on an origin of its own; only a test that reads
// what a view drew reaches into its frame.
func TestPageKeepsAViewOnAnOriginOfItsOwnUnlessATestAsks(t *testing.T) {
	t.Parallel()

	for _, sameOrigin := range []bool{false, true} {
		page, err := Page("<p>view</p>", nil, Options{SameOrigin: sameOrigin})
		if err != nil {
			t.Fatalf("Page: %v", err)
		}
		want := `const SANDBOX = "allow-scripts";`
		if sameOrigin {
			want = `const SANDBOX = "allow-scripts allow-same-origin";`
		}
		if !strings.Contains(page, want) {
			t.Errorf("with SameOrigin %v the page lacks %s", sameOrigin, want)
		}
	}
}

func TestPageReportsAResultThatIsNotJSON(t *testing.T) {
	t.Parallel()

	_, err := Page("<p>view</p>", []Frame{{Title: "broken", Result: json.RawMessage("{not json")}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "encode the frames") {
		t.Errorf("Page with a result that is not JSON returned %v, want an error encoding the frames", err)
	}
}

// Not parallel: it swaps the template the other tests read.
func TestPageNoticesATemplateThatLostAPlaceholder(t *testing.T) {
	original := hostTemplate
	t.Cleanup(func() { hostTemplate = original })
	hostTemplate = strings.Replace(original, "/*BB_HOST_SANDBOX*/", `"allow-scripts"`, 1)

	_, err := Page("<p>view</p>", nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "/*BB_HOST_SANDBOX*/") {
		t.Errorf("Page from a template without its sandbox placeholder returned %v, want an error naming it", err)
	}
}

// framesOf reads back the frames the page carries.
func framesOf(t *testing.T, page string) []map[string]any {
	t.Helper()

	const marker = "const FRAMES = "
	start := strings.Index(page, marker)
	if start < 0 {
		t.Fatal("the page declares no frames")
	}
	encoded := page[start+len(marker):]
	encoded = encoded[:strings.Index(encoded, ";\n")]
	var frames []map[string]any
	if err := json.Unmarshal([]byte(encoded), &frames); err != nil {
		t.Fatalf("the page's frames are not JSON: %v", err)
	}
	return frames
}
