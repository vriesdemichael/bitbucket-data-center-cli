package pullrequestactivity

import (
	"context"
	"encoding/json"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestListAndExtractComments(t *testing.T) {
	service := newActivityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/activities" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":1001,"action":"COMMENTED","createdDate":123,"comment":{"id":41,"text":"general comment","version":2}},{"id":1002,"action":"COMMENTED","createdDate":124,"comment":{"id":42,"text":"anchored comment","version":1,"anchor":{"path":{"parent":"src","name":"main.go"}}}},{"id":1003,"action":"APPROVED","createdDate":125}]}`))
	})

	activities, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12", ListOptions{MaxResults: 10})
	if err != nil {
		t.Fatalf("expected list to succeed, got: %v", err)
	}
	if len(activities) != 3 {
		t.Fatalf("expected 3 activities, got %d", len(activities))
	}
	if activities[0].Comment == nil || activities[0].Comment.Text == nil || *activities[0].Comment.Text != "general comment" {
		t.Fatalf("expected first activity comment to be decoded, got: %#v", activities[0])
	}
	if activities[2].Comment != nil {
		t.Fatalf("expected non-comment activity to have nil comment, got: %#v", activities[2])
	}

	comments := ExtractComments(activities)
	if len(comments) != 2 {
		t.Fatalf("expected 2 extracted comments, got %d", len(comments))
	}
	if comments[1].Anchor == nil || comments[1].Anchor.Path == nil || comments[1].Anchor.Path.Name == nil || *comments[1].Anchor.Path.Name != "main.go" {
		t.Fatalf("expected anchored comment path to be preserved, got: %#v", comments[1])
	}
	if activities[0].Raw["action"] != "COMMENTED" {
		t.Fatalf("expected raw activity payload to be preserved, got: %#v", activities[0].Raw)
	}
}

func TestListValidation(t *testing.T) {
	service := newActivityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

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

func TestListPaginationAndStatusBranches(t *testing.T) {
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

	t.Run("status mapping and decode failures", func(t *testing.T) {
		testCases := []struct {
			name         string
			status       int
			body         string
			expectedKind apperrors.Kind
		}{
			{name: "bad request", status: http.StatusBadRequest, body: `{"errors":[{"message":"bad request"}]}`, expectedKind: apperrors.KindValidation},
			{name: "not found", status: http.StatusNotFound, body: `{"errors":[{"message":"missing"}]}`, expectedKind: apperrors.KindNotFound},
			{name: "server error", status: http.StatusInternalServerError, body: `{"errors":[{"message":"boom"}]}`, expectedKind: apperrors.KindInternal},
			{name: "invalid json", status: http.StatusOK, body: `{`, expectedKind: apperrors.KindTransient},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				service := newActivityTestService(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(testCase.status)
					_, _ = w.Write([]byte(testCase.body))
				})

				_, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12", ListOptions{})
				if err == nil {
					t.Fatal("expected error")
				}
				if apperrors.KindOf(err) != testCase.expectedKind {
					t.Fatalf("expected kind %s, got %s (err=%v)", testCase.expectedKind, apperrors.KindOf(err), err)
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
