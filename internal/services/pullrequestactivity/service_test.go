package pullrequestactivity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func newActivityTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestListValidation(t *testing.T) {
	service := newActivityTestService(t, testsupport.UnreachedHandler(t))

	if _, err := service.List(context.Background(), RepositoryRef{}, "12", ListOptions{}); err == nil {
		t.Fatal("expected repository validation error")
	}
	if _, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, " ", ListOptions{}); err == nil {
		t.Fatal("expected empty pull request id validation error")
	}
	if _, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "bad", ListOptions{}); err == nil {
		t.Fatal("expected pull request id validation error")
	}
	if _, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12", ListOptions{Start: -1}); err == nil {
		t.Fatal("expected start validation error")
	}
}

func TestActivityExtractionAndStatusMapping(t *testing.T) {
	// One comment can appear in the timeline more than once -- edited, replied
	// to, resolved -- so extracting comments has to collapse them.
	//
	// This used to be asserted through a two-page mock, which put the paging
	// convention in front of a function that never sees it: ExtractComments
	// takes the activities the walk produced, and whether they arrived in one
	// page or four is openapi.PageThrough's business and is tested there. The
	// duplicate is what this is about, so the duplicate is all that is here.
	t.Run("deduplicates extracted comments", func(t *testing.T) {
		comment := func(id int64, text string) *openapigenerated.RestComment {
			return &openapigenerated.RestComment{Id: &id, Text: &text}
		}

		comments := ExtractComments([]Activity{
			{ID: 2001, Action: "COMMENTED", Comment: comment(51, "duplicate")},
			{ID: 2002, Action: "COMMENTED", Comment: comment(51, "duplicate")},
			{ID: 2003, Action: "COMMENTED", Comment: comment(52, "next page")},
		})

		if len(comments) != 2 {
			t.Fatalf("expected duplicate comments to be collapsed, got %d", len(comments))
		}
	})

	// The status mapping, asked of the mapper rather than of a server.
	//
	// This used to run each case through a handler that answered the status
	// under test, which put a claim in the way of the assertion: that
	// Bitbucket answers 400 here, and 500 there. It does not need to. The
	// mapping is a pure function of the status, so the status can just be
	// passed to it -- and then the ones a stub never sent, 401 and 403, are
	// reachable too.
	t.Run("status mapping", func(t *testing.T) {
		for _, testCase := range []struct {
			name   string
			status int
			want   apperrors.Kind
		}{
			{name: "bad request", status: http.StatusBadRequest, want: apperrors.KindValidation},
			{name: "unauthenticated", status: http.StatusUnauthorized, want: apperrors.KindAuthentication},
			{name: "forbidden", status: http.StatusForbidden, want: apperrors.KindAuthorization},
			{name: "not found", status: http.StatusNotFound, want: apperrors.KindNotFound},
			{name: "server error", status: http.StatusInternalServerError, want: apperrors.KindInternal},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				err := mapActivityStatusError(testCase.status, []byte(`{"errors":[{"message":"whatever"}]}`))
				if apperrors.KindOf(err) != testCase.want {
					t.Fatalf("status %d mapped to %s, want %s", testCase.status, apperrors.KindOf(err), testCase.want)
				}
			})
		}
	})
}

func TestRawActivityHelpers(t *testing.T) {
	comments := ExtractComments([]Activity{{Comment: &openapigenerated.RestComment{Text: stringPtr("no id")}}, {Comment: &openapigenerated.RestComment{Text: stringPtr("no id")}}})
	if len(comments) != 2 {
		t.Fatalf("expected comments without ids to be preserved, got %d", len(comments))
	}

	activity := rawActivity{}
	if err := activity.UnmarshalJSON([]byte(`{"id":1,"action":"COMMENTED","comment":{"id":10},"extra":{"x":1}}`)); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if activity.Raw == nil || activity.Raw["extra"] == nil {
		t.Fatalf("expected raw payload to be preserved, got %#v", activity.Raw)
	}
	if err := activity.UnmarshalJSON([]byte(`{`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}

	if _, err := decodeActivityPage([]byte(`{`)); err == nil {
		t.Fatal("expected decodeActivityPage to fail for invalid JSON")
	}
	if _, err := mapActivity(rawActivity{Raw: map[string]json.RawMessage{"bad": []byte(`{`)}}); err == nil {
		t.Fatal("expected mapActivity to fail for invalid raw payload")
	}
	if _, err := mapActivity(rawActivity{Comment: rawMessagePtr(`{`), Raw: map[string]json.RawMessage{"comment": []byte(`{`)}}); err == nil {
		t.Fatal("expected mapActivity to fail for invalid comment payload")
	}

	if safederef.String(nil) != "" || safederef.String(stringPtr("ok")) != "ok" {
		t.Fatal("unexpected safeString behavior")
	}
	if safederef.Int64(nil) != 0 {
		t.Fatal("expected zero safeInt64")
	}

	if err := mapActivityStatusError(http.StatusTeapot, []byte("body")); err == nil || !strings.Contains(err.Error(), "418") {
		t.Fatalf("expected internal teapot error, got: %v", err)
	}
	if err := mapActivityStatusError(http.StatusCreated, nil); err == nil || !strings.Contains(err.Error(), "201") {
		t.Fatalf("expected internal default error, got: %v", err)
	}
}

func stringPtr(value string) *string {
	return &value
}

func rawMessagePtr(value string) *json.RawMessage {
	raw := json.RawMessage(value)
	return &raw
}

// A body that stops halfway is transient, not internal.
//
// The distinction is what an agent does next: a truncated response is worth
// retrying and a malformed one is not. This case used to sit in the status
// table as "invalid json", where it was the odd one out -- the others go
// through our mapper and this one never reaches it, because the generated
// client fails to parse before we see a page at all.
//
// mock-inventory: transport-fault — a body cut short mid-object, which is what a connection dropped mid-response looks like and not something a server can be asked for.
func TestListReportsATruncatedBodyAsTransient(t *testing.T) {
	service := newActivityTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"values":[{"id":1`))
	})

	_, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12", ListOptions{})
	if apperrors.KindOf(err) != apperrors.KindTransient {
		t.Fatalf("a truncated body was reported as %s, want transient (err=%v)", apperrors.KindOf(err), err)
	}
}

// TestListAndExtractComments is live now, in
// TestLivePullRequestReviewVisibility, against a timeline Bitbucket built: a
// comment, an inline comment that keeps its anchor, a task, and the
// non-comment activities that come with opening a pull request. Three things
// it checked are checked there -- a comment activity carries its comment, one
// that is not carries none, and the raw payload survives the mapping.
//
// The version here wrote three activities, including an APPROVED one with no
// comment field, so the nil it then required was the nil it had left out.
