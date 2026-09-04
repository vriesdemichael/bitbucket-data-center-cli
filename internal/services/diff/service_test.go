package diff

import (
	"context"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestDiffRefsPatchAndStatErrorBranches(t *testing.T) {
	t.Run("patch success", func(t *testing.T) {
		service := newDiffServiceWithHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte("diff --git a/x.txt b/x.txt\n"))
		})

		result, err := service.DiffRefs(context.Background(), DiffRefsInput{
			Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			From:       "main",
			To:         "feature",
			Output:     OutputKindPatch,
		})
		if err != nil {
			t.Fatalf("expected patch success, got: %v", err)
		}
		if !strings.Contains(result.Patch, "diff --git") {
			t.Fatalf("expected patch payload, got: %q", result.Patch)
		}
	})

	t.Run("patch status error", func(t *testing.T) {
		service := newDiffServiceWithHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte("missing"))
		})

		_, err := service.DiffRefs(context.Background(), DiffRefsInput{
			Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			From:       "main",
			To:         "feature",
			Output:     OutputKindPatch,
		})
		if err == nil {
			t.Fatal("expected not found error")
		}
		if apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found exit code 4, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("stat status error", func(t *testing.T) {
		service := newDiffServiceWithHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusConflict)
			_, _ = writer.Write([]byte("conflict"))
		})

		_, err := service.DiffRefs(context.Background(), DiffRefsInput{
			Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			From:       "main",
			To:         "feature",
			Output:     OutputKindStat,
		})
		if err == nil {
			t.Fatal("expected conflict error")
		}
		if apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict exit code 5, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})
}

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

	diffText := strings.Join([]string{
		"diff --git a/a.txt b/a.txt",
		"diff --git a/a.txt b/a.txt",
		"diff --git a/dev/null b/new.txt",
		"diff --git a/old.txt b/dev/null",
	}, "\n")

	names := extractNamesFromUnifiedDiff(diffText)
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d: %#v", len(names), names)
	}
	if names[0] != "a.txt" || names[1] != "new.txt" || names[2] != "old.txt" {
		t.Fatalf("unexpected names extraction: %#v", names)
	}
}

func TestMapStatusErrorCoverage(t *testing.T) {
	if err := openapi.MapStatusError(http.StatusOK, nil); err != nil {
		t.Fatalf("expected nil on 2xx, got: %v", err)
	}

	tests := []struct {
		status   int
		exitCode int
	}{
		{status: http.StatusBadRequest, exitCode: 2},
		{status: http.StatusUnauthorized, exitCode: 3},
		{status: http.StatusForbidden, exitCode: 3},
		{status: http.StatusNotFound, exitCode: 4},
		{status: http.StatusConflict, exitCode: 5},
		{status: http.StatusTooManyRequests, exitCode: 10},
		{status: http.StatusInternalServerError, exitCode: 10},
		{status: http.StatusNotAcceptable, exitCode: 1},
	}

	for _, testCase := range tests {
		err := openapi.MapStatusError(testCase.status, []byte("boom"))
		if err == nil {
			t.Fatalf("expected error for status %d", testCase.status)
		}
		if apperrors.ExitCode(err) != testCase.exitCode {
			t.Fatalf("expected exit code %d for status %d, got %d", testCase.exitCode, testCase.status, apperrors.ExitCode(err))
		}
	}
}

func TestDiffPRPatchAndStatModes(t *testing.T) {
	t.Run("patch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte("diff --git a/p.txt b/p.txt\n"))
		}))
		defer server.Close()

		client, err := openapigenerated.NewClientWithResponses(server.URL)
		if err != nil {
			t.Fatalf("create generated client: %v", err)
		}

		service := NewService(client)
		result, err := service.DiffPR(context.Background(), DiffPRInput{
			Repository:    RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			PullRequestID: "12",
			Output:        OutputKindPatch,
		})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !strings.Contains(result.Patch, "diff --git") {
			t.Fatalf("expected patch output, got: %q", result.Patch)
		}
	})

	t.Run("stat", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			// The three counts a diff-stats-summary endpoint returns; this used to
			// serve a paged-list shape, which decoded to nothing (#526).
			_, _ = writer.Write([]byte(`{"filesChanged":1,"totalInsertions":3,"totalDeletions":2}`))
		}))
		defer server.Close()

		client, err := openapigenerated.NewClientWithResponses(server.URL)
		if err != nil {
			t.Fatalf("create generated client: %v", err)
		}

		service := NewService(client)
		result, err := service.DiffPR(context.Background(), DiffPRInput{
			Repository:    RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			PullRequestID: "12",
			Output:        OutputKindStat,
		})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if result.Stats == nil {
			t.Fatal("expected stats payload")
		}
	})
}

func TestDiffPRErrorBranches(t *testing.T) {
	t.Run("patch status error", func(t *testing.T) {
		service := newDiffServiceWithHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte("unauthorized"))
		})

		_, err := service.DiffPR(context.Background(), DiffPRInput{
			Repository:    RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			PullRequestID: "7",
			Output:        OutputKindPatch,
		})
		if err == nil {
			t.Fatal("expected authentication error")
		}
		if apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected auth exit code 3, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("stat status error", func(t *testing.T) {
		service := newDiffServiceWithHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusNotAcceptable)
			_, _ = writer.Write([]byte("not acceptable"))
		})

		_, err := service.DiffPR(context.Background(), DiffPRInput{
			Repository:    RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			PullRequestID: "7",
			Output:        OutputKindStat,
		})
		if err == nil {
			t.Fatal("expected permanent error")
		}
		if apperrors.ExitCode(err) != 1 {
			t.Fatalf("expected permanent exit code 1, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("raw status error", func(t *testing.T) {
		service := newDiffServiceWithHandler(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte("rate limited"))
		})

		_, err := service.DiffPR(context.Background(), DiffPRInput{
			Repository:    RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			PullRequestID: "7",
			Output:        OutputKindRaw,
		})
		if err == nil {
			t.Fatal("expected transient error")
		}
		if apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})
}

func TestDiffRefsStatAndCommitWithPath(t *testing.T) {
	t.Run("refs stat", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			// The three counts a diff-stats-summary endpoint returns; this used to
			// serve a paged-list shape, which decoded to nothing (#526).
			_, _ = writer.Write([]byte(`{"filesChanged":1,"totalInsertions":3,"totalDeletions":2}`))
		}))
		defer server.Close()

		client, err := openapigenerated.NewClientWithResponses(server.URL)
		if err != nil {
			t.Fatalf("create generated client: %v", err)
		}

		service := NewService(client)
		result, err := service.DiffRefs(context.Background(), DiffRefsInput{
			Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			From:       "main",
			To:         "feature",
			Path:       "seed.txt",
			Output:     OutputKindStat,
		})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if result.Stats == nil {
			t.Fatal("expected stats payload")
		}
	})

	t.Run("commit path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte("diff --git a/seed.txt b/seed.txt\n"))
		}))
		defer server.Close()

		client, err := openapigenerated.NewClientWithResponses(server.URL)
		if err != nil {
			t.Fatalf("create generated client: %v", err)
		}

		service := NewService(client)
		result, err := service.DiffCommit(context.Background(), DiffCommitInput{
			Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
			CommitID:   "abc123",
			Path:       "seed.txt",
		})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !strings.Contains(result.Patch, "diff --git") {
			t.Fatalf("expected patch output, got: %q", result.Patch)
		}
	})
}

func TestDiffCommitStatusErrorMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte("missing commit"))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	_, err = service.DiffCommit(context.Background(), DiffCommitInput{
		Repository: RepositoryRef{ProjectKey: "PRJ", Slug: "demo"},
		CommitID:   "abc123",
	})
	if err == nil {
		t.Fatal("expected not found error")
	}
	if apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not found exit code 4, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestDiffRefsTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("ok"))
	}))
	baseURL := server.URL
	server.Close()

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

	err = openapi.MapStatusError(http.StatusBadRequest, []byte("   "))
	if err == nil {
		t.Fatal("expected validation error for bad request")
	}
	if !strings.Contains(err.Error(), "Bad Request") {
		t.Fatalf("expected status text fallback in error message, got: %v", err)
	}
}

func TestDiffTransportFailureBranches(t *testing.T) {
	closedService := func(t *testing.T) *Service {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}))
		baseURL := server.URL
		server.Close()

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

	t.Run("compare and format edge cases", func(t *testing.T) {
		repo := RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}
		service := newDiffServiceWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
		})

		// CompareChanges limit <= 0
		_, err := service.CompareChanges(context.Background(), repo, "main", "feat", 0)
		if err != nil {
			t.Fatalf("expected compare success with limit <= 0, got %v", err)
		}

		// CompareChanges status error
		errService := newDiffServiceWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		_, err = errService.CompareChanges(context.Background(), repo, "main", "feat", 10)
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found error, got %v", err)
		}

		// CompareChanges empty body
		emptyBodyService := newDiffServiceWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		})
		res, err := emptyBodyService.CompareChanges(context.Background(), repo, "main", "feat", 10)
		if err != nil || len(res) != 0 {
			t.Fatalf("expected empty response, got %v, err %v", res, err)
		}

		// CompareDiff status error
		_, err = errService.CompareDiff(context.Background(), repo, "main", "feat")
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found error, got %v", err)
		}

		// CompareDiff empty body
		nilBodyDiffService := newDiffServiceWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		_, err = nilBodyDiffService.CompareDiff(context.Background(), repo, "main", "feat")
		if err == nil || !strings.Contains(err.Error(), "empty diff response") {
			t.Fatalf("expected empty response error, got %v", err)
		}

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
