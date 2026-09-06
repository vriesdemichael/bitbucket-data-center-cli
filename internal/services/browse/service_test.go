package browse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newBrowseTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	// The raw and browse endpoints go through httpclient rather than the
	// generated client; httptest needs no real credentials.
	httpClient := httpclient.NewFromConfig(config.AppConfig{
		BitbucketURL:   server.URL,
		RequestTimeout: 10 * time.Second,
		RetryCount:     0,
	})

	return NewService(client, httpClient)
}

func TestBrowseServiceValidation(t *testing.T) {
	t.Parallel()

	service := newBrowseTestService(t, func(w http.ResponseWriter, r *http.Request) {
		// Every case here is refused before a request is built, so the handler
		// is an assertion rather than a stand-in: reaching it means a guard
		// let something through (ADR-079).
		t.Errorf("validation let a request through: %s %s", r.Method, r.URL.Path)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.Raw(context.Background(), repo, "", ""); err == nil {
		t.Fatal("expected raw path validation error")
	}

	if _, err := service.File(context.Background(), repo, "", FileOptions{}); err == nil {
		t.Fatal("expected file path validation error")
	}

}

func TestBrowseServiceTransientAndMapping(t *testing.T) {
	t.Parallel()

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	transientService := newBrowseTestService(t, func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		connection, _, hijackErr := hijacker.Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	})

	if _, err := transientService.Tree(context.Background(), repo, "", TreeOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected tree transient error, got %v", err)
	}
	if _, err := transientService.Tree(context.Background(), repo, "dir", TreeOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected tree1 transient error, got %v", err)
	}
	if _, err := transientService.Raw(context.Background(), repo, "file.txt", "abc"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected raw transient error, got %v", err)
	}
	if _, err := transientService.File(context.Background(), repo, "file.txt", FileOptions{At: "abc"}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected file transient error, got %v", err)
	}

	service := newBrowseTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/files":
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		case strings.Contains(r.URL.Path, "raw"):
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		default:
			http.NotFound(w, r)
		}
	})

	files, err := service.Tree(context.Background(), repo, "", TreeOptions{})
	if err != nil || len(files) != 0 {
		t.Fatalf("expected empty tree success, got %v", err)
	}

	if _, err := service.Raw(context.Background(), repo, "file.txt", ""); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not found get error, got %v", err)
	}

	if err := validateRepositoryRef(RepositoryRef{}); err == nil {
		t.Fatalf("expected validate error")
	}

}

func TestBrowseServiceEdit(t *testing.T) {
	t.Parallel()

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	t.Run("success", func(t *testing.T) {
		service := newBrowseTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/browse/path/to/file.txt" {
				http.NotFound(w, r)
				return
			}

			err := r.ParseMultipartForm(10 * 1024)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("bad multipart form"))
				return
			}

			if r.FormValue("branch") != "main" ||
				r.FormValue("message") != "my commit message" ||
				r.FormValue("content") != "new content" ||
				r.FormValue("sourceBranch") != "main" ||
				r.FormValue("sourceCommitId") != "abc1234" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("incorrect multipart fields"))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"def5678","displayId":"def5678"}`))
		})

		commit, err := service.Edit(context.Background(), repo, "path/to/file.txt", EditInput{
			Branch:         "main",
			Message:        "my commit message",
			Content:        "new content",
			SourceBranch:   "main",
			SourceCommitId: "abc1234",
		})
		if err != nil {
			t.Fatalf("expected edit success, got %v", err)
		}
		if commit == nil || *commit.Id != "def5678" {
			t.Fatalf("expected returned commit to have Id def5678, got %+v", commit)
		}
	})

	t.Run("empty path validation", func(t *testing.T) {
		service := newBrowseTestService(t, testsupport.UnreachedHandler(t))
		_, err := service.Edit(context.Background(), repo, "", EditInput{})
		if err == nil || !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("expected path validation error, got %v", err)
		}
	})

	t.Run("empty repo validation", func(t *testing.T) {
		service := newBrowseTestService(t, testsupport.UnreachedHandler(t))
		_, err := service.Edit(context.Background(), RepositoryRef{}, "file.txt", EditInput{})
		if err == nil || !strings.Contains(err.Error(), "project/repo") {
			t.Fatalf("expected repo validation error, got %v", err)
		}
	})

	t.Run("transient network failure", func(t *testing.T) {
		service := newBrowseTestService(t, func(w http.ResponseWriter, r *http.Request) {
			hijacker, ok := w.(http.Hijacker)
			if ok {
				conn, _, hijackErr := hijacker.Hijack()
				if hijackErr == nil {
					_ = conn.Close()
				}
			}
		})
		_, err := service.Edit(context.Background(), repo, "file.txt", EditInput{Branch: "main"})
		if err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient network error, got %v", err)
		}
	})

	t.Run("status error", func(t *testing.T) {
		service := newBrowseTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":[{"message":"bad input"}]}`))
		})
		_, err := service.Edit(context.Background(), repo, "file.txt", EditInput{Branch: "main"})
		if err == nil || apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected bad request exit code 2, got %v", err)
		}
	})

	t.Run("empty response payload", func(t *testing.T) {
		service := newBrowseTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		_, err := service.Edit(context.Background(), repo, "file.txt", EditInput{Branch: "main"})
		if err == nil || !strings.Contains(err.Error(), "empty body where the specification documents a payload") {
			t.Fatalf("expected the missing-payload error, got %v", err)
		}
		// Permanent, not internal: the server and the spec disagree, which is
		// not bb malfunctioning and not something a retry changes.
		if kind := apperrors.KindOf(err); kind != apperrors.KindPermanent {
			t.Errorf("kind = %v, want permanent", kind)
		}
	})
}

func TestBrowseServiceRejectsTraversal(t *testing.T) {
	t.Parallel()

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	service := newBrowseTestService(t, testsupport.UnreachedHandler(t))

	for _, path := range []string{"../../../etc/passwd", "docs/../../secret", ".."} {
		if _, err := service.Raw(context.Background(), repo, path, ""); err == nil {
			t.Fatalf("expected traversal rejection for Raw(%q)", path)
		} else if apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected validation exit code 2 for %q, got %d", path, apperrors.ExitCode(err))
		}

		if _, err := service.File(context.Background(), repo, path, FileOptions{}); err == nil {
			t.Fatalf("expected traversal rejection for File(%q)", path)
		}
	}
}

func TestEncodeFilePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "simple", input: "file.txt", want: "file.txt"},
		{name: "nested", input: "a/b/c.txt", want: "a/b/c.txt"},
		{name: "leading slash dropped", input: "/a/b.txt", want: "a/b.txt"},
		{name: "redundant slashes collapsed", input: "a//b.txt", want: "a/b.txt"},
		{name: "current dir segments dropped", input: "./a/./b.txt", want: "a/b.txt"},
		{name: "spaces escaped", input: "my dir/my file.txt", want: "my%20dir/my%20file.txt"},
		{name: "question mark escaped", input: "a?b.txt", want: "a%3Fb.txt"},
		{name: "hash escaped", input: "a#b.txt", want: "a%23b.txt"},
		{name: "percent escaped", input: "a%2Fb.txt", want: "a%252Fb.txt"},
		{name: "empty", input: "", wantErr: true},
		{name: "only separators", input: "///", wantErr: true},
		{name: "traversal", input: "a/../b", wantErr: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := encodeFilePath(testCase.input)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", testCase.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", testCase.input, err)
			}
			if got != testCase.want {
				t.Fatalf("encodeFilePath(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestRepositoryAPIPathEscapesRepositoryRef(t *testing.T) {
	t.Parallel()

	got := repositoryAPIPath(RepositoryRef{ProjectKey: "a b", Slug: "c/d"}, "raw", "file.txt")
	want := "/rest/api/latest/projects/a%20b/repos/c%2Fd/raw/file.txt"
	if got != want {
		t.Fatalf("repositoryAPIPath = %q, want %q", got, want)
	}
}

func TestBrowseServiceTreeRejectsTraversal(t *testing.T) {
	t.Parallel()

	service := newBrowseTestService(t, testsupport.UnreachedHandler(t))

	_, err := service.Tree(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "../secrets", TreeOptions{})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected a validation error, got %v", err)
	}
}
