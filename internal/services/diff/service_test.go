package diff

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestDiffRefsPatchWithPathRejected(t *testing.T) {
	service := NewService(nil)
	_, err := service.DiffRefs(context.Background(), DiffRefsInput{
		Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
		From:       "main",
		To:         "feature",
		Path:       "seed.txt",
		Output:     OutputKindPatch,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected exit code 2, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestDiffValidationBranches(t *testing.T) {
	service := NewService(nil)

	_, err := service.DiffRefs(context.Background(), DiffRefsInput{
		Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
		From:       "",
		To:         "feature",
		Output:     OutputKindRaw,
	})
	if err == nil {
		t.Fatal("expected validation error for missing from")
	}

	_, err = service.DiffPR(context.Background(), DiffPRInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}})
	if err == nil {
		t.Fatal("expected validation error for missing pull request id")
	}

	_, err = service.DiffCommit(context.Background(), DiffCommitInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}})
	if err == nil {
		t.Fatal("expected validation error for missing commit id")
	}

	_, err = service.DiffRefs(context.Background(), DiffRefsInput{
		Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
		From:       "main",
		To:         "feature",
		Output:     OutputKind("unknown"),
	})
	if err == nil {
		t.Fatal("expected validation error for unsupported output")
	}
}

func TestDiffHelpers(t *testing.T) {
	if pathOrDot("") != "." {
		t.Fatal("expected empty path to map to dot")
	}
	if pathOrDot(" seed.txt ") != "seed.txt" {
		t.Fatal("expected path trimming")
	}
	// Records in the shape git writes them, which is not the shape this test
	// used to use: it had "diff --git a/dev/null b/new.txt" for an add, and git
	// never does that -- the header names the same path on both sides and
	// /dev/null appears only on the --- and +++ lines. Reading the header was
	// what made a path containing a space come back truncated.
	//
	// The trailing tab on a +++ line is git's, not a typo: it separates the
	// optional timestamp field, and it is what a live diff carries.
	diffText := strings.Join([]string{
		"diff --git a/a.txt b/a.txt",
		"index 111..222 100644",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1 +1 @@",
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"diff --git a/new.txt b/new.txt",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/new.txt",
		"diff --git a/old.txt b/old.txt",
		"deleted file mode 100644",
		"--- a/old.txt",
		"+++ /dev/null",
		"diff --git a/was.txt b/now.txt",
		"similarity index 100%",
		"--- a/was.txt",
		"+++ b/now.txt",
		"diff --git a/docs site/guide.txt b/docs site/guide.txt",
		"--- /dev/null",
		"+++ b/docs site/guide.txt\t",
		"diff --git a/logo.png b/logo.png",
		"Binary files a/logo.png and b/logo.png differ",
	}, "\n")

	names := extractNamesFromUnifiedDiff(diffText)
	want := []string{"a.txt", "new.txt", "old.txt", "now.txt", "docs site/guide.txt", "logo.png"}
	if len(names) != len(want) {
		t.Fatalf("expected %d names, got %d: %#v", len(want), len(names), names)
	}
	for index, expected := range want {
		if names[index] != expected {
			t.Fatalf("name %d = %q, want %q (all: %#v)", index, names[index], expected, names)
		}
	}
}

// mock-inventory: transport-fault — the connection is broken on purpose; no live server refuses on request.
func TestDiffRefsTransportFailure(t *testing.T) {
	baseURL := testsupport.ClosedListenerURL(t)

	client, err := openapigenerated.NewClientWithResponses(baseURL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	_, err = service.DiffRefs(context.Background(), DiffRefsInput{
		Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
		From:       "main",
		To:         "feature",
		Output:     OutputKindRaw,
	})
	if err == nil {
		t.Fatal("expected transient transport error")
	}
	if apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestDiffValidationAndHelperEdgeBranches(t *testing.T) {
	service := NewService(nil)

	_, err := service.DiffRefs(context.Background(), DiffRefsInput{Repository: RepositoryRef{}, From: "main", To: "feature"})
	if err == nil {
		t.Fatal("expected repository validation error")
	}

	_, err = service.DiffPR(context.Background(), DiffPRInput{Repository: RepositoryRef{ProjectKey: "PRJ"}, PullRequestID: "1", Output: OutputKindRaw})
	if err == nil {
		t.Fatal("expected repository validation error for diff pr")
	}

	_, err = service.DiffCommit(context.Background(), DiffCommitInput{Repository: RepositoryRef{ProjectKey: "PRJ"}, CommitID: "abc"})
	if err == nil {
		t.Fatal("expected repository validation error for diff commit")
	}

	names := extractNamesFromUnifiedDiff(strings.Join([]string{
		"diff --git",
		"diff --git a",
		"diff --git a/ b/",
		"diff --git a//dev/null b//dev/null",
	}, "\n"))
	if len(names) != 0 {
		t.Fatalf("expected no extracted names from malformed lines, got: %#v", names)
	}

}

// mock-inventory: transport-fault — the failures are injected below the API; the subject is how each branch classifies them.
func TestDiffTransportFailureBranches(t *testing.T) {
	closedService := func(t *testing.T) *Service {
		t.Helper()
		baseURL := testsupport.ClosedListenerURL(t)

		client, err := openapigenerated.NewClientWithResponses(baseURL)
		if err != nil {
			t.Fatalf("create generated client: %v", err)
		}

		return NewService(client)
	}

	t.Run("diff refs patch transport", func(t *testing.T) {
		service := closedService(t)
		_, err := service.DiffRefs(context.Background(), DiffRefsInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, From: "main", To: "feature", Output: OutputKindPatch})
		if err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error, got: %v", err)
		}
	})

	t.Run("diff refs stat transport", func(t *testing.T) {
		service := closedService(t)
		_, err := service.DiffRefs(context.Background(), DiffRefsInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, From: "main", To: "feature", Output: OutputKindStat})
		if err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error, got: %v", err)
		}
	})

	t.Run("diff pr patch transport", func(t *testing.T) {
		service := closedService(t)
		_, err := service.DiffPR(context.Background(), DiffPRInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, PullRequestID: "1", Output: OutputKindPatch})
		if err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error, got: %v", err)
		}
	})

	t.Run("diff pr stat transport", func(t *testing.T) {
		service := closedService(t)
		_, err := service.DiffPR(context.Background(), DiffPRInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, PullRequestID: "1", Output: OutputKindStat})
		if err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error, got: %v", err)
		}
	})

	t.Run("diff pr raw transport", func(t *testing.T) {
		service := closedService(t)
		_, err := service.DiffPR(context.Background(), DiffPRInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, PullRequestID: "1", Output: OutputKindRaw})
		if err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error, got: %v", err)
		}
	})

	t.Run("diff commit path transport", func(t *testing.T) {
		service := closedService(t)
		_, err := service.DiffCommit(context.Background(), DiffCommitInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, CommitID: "abc", Path: "seed.txt"})
		if err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error, got: %v", err)
		}
	})

	t.Run("diff commit path status error", func(t *testing.T) {
		service := newDiffServiceWithHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte("missing"))
		})

		_, err := service.DiffCommit(context.Background(), DiffCommitInput{Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, CommitID: "abc", Path: "seed.txt"})
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found error, got: %v", err)
		}
	})
}

func newDiffServiceWithHandler(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	return NewService(client)
}

func TestFormatRestDiff(t *testing.T) {
	t.Run("nil diff", func(t *testing.T) {
		if FormatRestDiff(nil) != "" {
			t.Fatal("expected empty string for nil input")
		}
	})

	t.Run("binary diff", func(t *testing.T) {
		isBinary := true
		srcComp := []string{"old.bin"}
		dstComp := []string{"new.bin"}
		diff := &openapigenerated.RestDiff{
			Binary: &isBinary,
			Source: &struct {
				Components *[]string `json:"components,omitempty"`
				Extension  *string   `json:"extension,omitempty"`
				Name       *string   `json:"name,omitempty"`
				Parent     *string   `json:"parent,omitempty"`
			}{Components: &srcComp},
			Destination: &struct {
				Components *[]string `json:"components,omitempty"`
				Extension  *string   `json:"extension,omitempty"`
				Name       *string   `json:"name,omitempty"`
				Parent     *string   `json:"parent,omitempty"`
			}{Components: &dstComp},
		}

		formatted := FormatRestDiff(diff)
		if !strings.Contains(formatted, "--- a/old.bin") ||
			!strings.Contains(formatted, "+++ b/new.bin") ||
			!strings.Contains(formatted, "Binary files differ") {
			t.Fatalf("unexpected formatted binary diff:\n%s", formatted)
		}
	})

	t.Run("text diff with hunks", func(t *testing.T) {
		srcComp := []string{"old.txt"}
		dstComp := []string{"new.txt"}
		isBinary := false
		sourceLine := int32(10)
		sourceSpan := int32(2)
		destLine := int32(10)
		destSpan := int32(3)
		hunkCtx := "hunk context header"

		segTypeRemoved := openapigenerated.RestDiffSegmentTypeREMOVED
		segTypeAdded := openapigenerated.RestDiffSegmentTypeADDED
		segTypeContext := openapigenerated.RestDiffSegmentTypeCONTEXT

		removedLineVal := "removed line"
		addedLineVal := "added line"
		contextLineVal := "context line"

		diff := &openapigenerated.RestDiff{
			Binary: &isBinary,
			Source: &struct {
				Components *[]string `json:"components,omitempty"`
				Extension  *string   `json:"extension,omitempty"`
				Name       *string   `json:"name,omitempty"`
				Parent     *string   `json:"parent,omitempty"`
			}{Components: &srcComp},
			Destination: &struct {
				Components *[]string `json:"components,omitempty"`
				Extension  *string   `json:"extension,omitempty"`
				Name       *string   `json:"name,omitempty"`
				Parent     *string   `json:"parent,omitempty"`
			}{Components: &dstComp},
			Hunks: &[]openapigenerated.RestDiffHunk{
				{
					SourceLine:      &sourceLine,
					SourceSpan:      &sourceSpan,
					DestinationLine: &destLine,
					DestinationSpan: &destSpan,
					Context:         &hunkCtx,
					Segments: &[]openapigenerated.RestDiffSegment{
						{
							Type: &segTypeContext,
							Lines: &[]openapigenerated.RestDiffLine{
								{Line: &contextLineVal},
							},
						},
						{
							Type: &segTypeRemoved,
							Lines: &[]openapigenerated.RestDiffLine{
								{Line: &removedLineVal},
							},
						},
						{
							Type: &segTypeAdded,
							Lines: &[]openapigenerated.RestDiffLine{
								{Line: &addedLineVal},
							},
						},
					},
				},
			},
		}

		formatted := FormatRestDiff(diff)
		expected := strings.Join([]string{
			"--- a/old.txt",
			"+++ b/new.txt",
			"@@ -10,2 +10,3 @@ hunk context header",
			" context line",
			"-removed line",
			"+added line",
			"",
		}, "\n")
		if formatted != expected {
			t.Fatalf("formatted diff mismatch.\nExpected:\n%q\nGot:\n%q", expected, formatted)
		}
	})

	// What was here besides the formatter is elsewhere now.
	//
	// A cap of zero reaching the wire is ADR-074's clause, asserted for every
	// paged listing at once in internal/services/contract. A 404 mapping to
	// exit 4 is TestLiveErrorTaxonomyMissingResources, against a pull request
	// that really is not there. An empty comparison is a commit compared with
	// itself in TestLiveRepoContentCommands, which is an empty answer Bitbucket
	// gives rather than one written here.
	t.Run("format edge cases", func(t *testing.T) {

		// FormatRestDiff nil paths and structures
		diff := &openapigenerated.RestDiff{}
		formatted := FormatRestDiff(diff)
		if !strings.Contains(formatted, "--- /dev/null") || !strings.Contains(formatted, "+++ /dev/null") {
			t.Fatalf("expected /dev/null rendering, got:\n%s", formatted)
		}

		// FormatRestDiff hunks nil/empty
		isBinary := false
		diffWithNilHunks := &openapigenerated.RestDiff{
			Binary: &isBinary,
		}
		formatted = FormatRestDiff(diffWithNilHunks)
		if !strings.Contains(formatted, "--- /dev/null") {
			t.Fatalf("expected standard headers, got:\n%s", formatted)
		}

		// FormatRestDiff segment nil lines and unknown segment types
		srcComp := []string{"path.txt"}
		hunk := openapigenerated.RestDiffHunk{
			Segments: &[]openapigenerated.RestDiffSegment{
				{
					Type: nil, // defaults to " "
					Lines: &[]openapigenerated.RestDiffLine{
						{Line: nil},
					},
				},
			},
		}
		diffWithNilSegmentLines := &openapigenerated.RestDiff{
			Binary: &isBinary,
			Source: &struct {
				Components *[]string `json:"components,omitempty"`
				Extension  *string   `json:"extension,omitempty"`
				Name       *string   `json:"name,omitempty"`
				Parent     *string   `json:"parent,omitempty"`
			}{Components: &srcComp},
			Hunks: &[]openapigenerated.RestDiffHunk{hunk},
		}
		formatted = FormatRestDiff(diffWithNilSegmentLines)
		if !strings.Contains(formatted, " ") {
			t.Fatalf("expected context space rendering, got:\n%s", formatted)
		}
	})
}

// TestDecodeStatsSummaryRefusesToCallAnUnreadableBodyEmpty is #526.
//
// The spec types both diff-stats-summary endpoints as RestDiff, which shares no
// field with what Bitbucket sends. Unmarshal ignores unknown fields, so the
// generated wrapper produced an empty struct, the payload marshalled to {}, and
// omitempty removed the key: `bb diff --stat` reported output=stat with no
// stats and exit 0. A caller could not tell "nothing to summarise" from "the
// summary did not decode".
func TestDecodeStatsSummaryRefusesToCallAnUnreadableBodyEmpty(t *testing.T) {
	t.Parallel()

	t.Run("the body Bitbucket actually sends", func(t *testing.T) {
		t.Parallel()

		summary, err := decodeStatsSummary([]byte(`{"filesChanged":1,"totalDeletions":0,"totalInsertions":1}`))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if summary["filesChanged"] != float64(1) {
			t.Errorf("filesChanged = %v, want 1", summary["filesChanged"])
		}
		if summary["totalInsertions"] != float64(1) {
			t.Errorf("totalInsertions = %v, want 1", summary["totalInsertions"])
		}
	})

	t.Run("an honest zero summary is a fact, not an absence", func(t *testing.T) {
		t.Parallel()

		summary, err := decodeStatsSummary([]byte(`{"filesChanged":0,"totalDeletions":0,"totalInsertions":0}`))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if summary["filesChanged"] != float64(0) {
			t.Errorf("a diff with nothing in it must still publish its zeros: %v", summary)
		}
	})

	t.Run("a field the spec never named still reaches the caller", func(t *testing.T) {
		t.Parallel()

		// Two of the three operations were typed as a bare interface{}, so
		// they published whatever came back. Decoding into a fixed struct
		// would have fixed one endpoint by narrowing two others.
		summary, err := decodeStatsSummary([]byte(`{"filesChanged":1,"somethingNew":"kept"}`))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if summary["somethingNew"] != "kept" {
			t.Errorf("an unrecognised field was dropped: %v", summary)
		}
	})

	for _, testCase := range []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"whitespace only", "   \n"},
		{"malformed json", `{"filesChanged":`},
		{"a shape with none of the counts", `{"binary":false,"hunks":[]}`},
		{"json null", "null"},
		{"an empty object", "{}"},
	} {
		t.Run(testCase.name+" is an error", func(t *testing.T) {
			t.Parallel()

			if _, err := decodeStatsSummary([]byte(testCase.body)); err == nil {
				t.Errorf("%q decoded to a summary; an unreadable body must not be reported as an empty one (ADR-077)", testCase.body)
			}
		})
	}
}

// TestStatRunsSurfaceAnUndecodableSummary covers the error return on both stat
// paths: an unreadable body must not reach the caller as an empty summary
// (#526, ADR-077).
// mock-inventory: transport-fault — an undecodable summary is injected; the subject is that bb says so rather than reporting zero changes.
func TestStatRunsSurfaceAnUndecodableSummary(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		body string
	}{
		{"a body holding none of the counts", `{"binary":false,"hunks":[]}`},
		{"an empty body", ``},
		{"malformed json", `{"filesChanged":`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := newDiffServiceWithHandler(t, func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(testCase.body))
			})

			t.Run("refs", func(t *testing.T) {
				_, err := service.DiffRefs(context.Background(), DiffRefsInput{
					Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
					From:       "main",
					To:         "feat",
					Output:     OutputKindStat,
				})
				if err == nil {
					t.Fatal("an unreadable summary was reported as a successful stat run")
				}
			})

			t.Run("pull request", func(t *testing.T) {
				_, err := service.DiffPR(context.Background(), DiffPRInput{
					Repository:    RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
					PullRequestID: "1",
					Output:        OutputKindStat,
				})
				if err == nil {
					t.Fatal("an unreadable summary was reported as a successful stat run")
				}
			})
		})
	}
}

// CompareDiff answers a diff or it answers an error; a 200 carrying nothing is
// neither, and reporting it as an empty diff would say the two refs match.
//
// mock-inventory: unreachable-state — a 200 with no body at all, which this endpoint does not send; the subject is that an absent diff is refused rather than read as no differences.
func TestCompareDiffRefusesABodylessSuccess(t *testing.T) {
	service := newDiffServiceWithHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	_, err := service.CompareDiff(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "main", "feat")
	if err == nil || !strings.Contains(err.Error(), "empty body where the specification documents a payload") {
		t.Fatalf("a bodyless 200 was read as a diff, got %v", err)
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindPermanent {
		t.Errorf("kind = %v, want permanent", kind)
	}
}
