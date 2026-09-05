package reposettings

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestUpdateRepositoryPullRequestRequiredApproversCount covers the fallback
// between two payload shapes, which needs two servers that disagree.
//
// A running Bitbucket 10.x takes the count as a plain number for every value
// including zero, and answers the object shape with 400 "The number of required
// approvals is invalid" — TestLiveGovernanceDryRunPredictionsReadRealState and
// TestLiveRepositorySettings drive that path. What no single instance can show
// is what happens on one that wants the other shape, which is the only reason
// the fallback exists.
//
// The order was the other way round until the probe above, on a comment
// asserting the object was what modern Bitbucket wanted. Every call therefore
// began with a request that could not succeed. This now pins the order: the
// number first, the object only after a validation error.
//
// mock-inventory: unreachable-state — an instance that wants the object shape, which is the older Bitbucket this fallback exists for and not the one we can run; the subject is the order of the attempts.
func TestUpdateRepositoryPullRequestRequiredApproversCount(t *testing.T) {
	t.Run("the number is enough, and is all that is sent", func(t *testing.T) {
		calls := 0
		service, close := settingsServiceOn(t, func(writer http.ResponseWriter, request *http.Request) {
			calls++
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), `"requiredApprovers":2`) {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte(`{"errors":[{"message":"expected the plain number first"}]}`))

				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"requiredApprovers":2}`))
		})
		defer close()

		settings, err := service.UpdateRepositoryPullRequestRequiredApproversCount(
			context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 2)
		if err != nil {
			t.Fatalf("expected the plain number to be accepted, got: %v", err)
		}
		if calls != 1 {
			t.Errorf("took %d requests to set a count the server accepts on the first", calls)
		}
		if value, ok := settings["requiredApprovers"].(float64); !ok || int(value) != 2 {
			t.Fatalf("expected required approvers count 2, got %#v", settings["requiredApprovers"])
		}
	})

	t.Run("a server that wants the object shape still gets it", func(t *testing.T) {
		numberCalls, objectCalls := 0, 0
		service, close := settingsServiceOn(t, func(writer http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			switch {
			case strings.Contains(string(body), `"requiredApprovers":2`):
				numberCalls++
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte(`{"errors":[{"message":"invalid payload"}]}`))
			case strings.Contains(string(body), `"requiredApprovers":{"count":2,"enabled":true}`):
				objectCalls++
				writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
				_, _ = writer.Write([]byte(`{"requiredApprovers":{"enabled":true,"count":2}}`))
			default:
				writer.WriteHeader(http.StatusInternalServerError)
			}
		})
		defer close()

		if _, err := service.UpdateRepositoryPullRequestRequiredApproversCount(
			context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 2); err != nil {
			t.Fatalf("expected the object fallback to be reached, got: %v", err)
		}
		if numberCalls != 1 || objectCalls != 1 {
			t.Errorf("expected one attempt of each shape, got number=%d object=%d", numberCalls, objectCalls)
		}
	})

	t.Run("a refusal that is not about the shape is not retried", func(t *testing.T) {
		calls := 0
		service, close := settingsServiceOn(t, func(writer http.ResponseWriter, _ *http.Request) {
			calls++
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"you may not change this"}]}`))
		})
		defer close()

		if _, err := service.UpdateRepositoryPullRequestRequiredApproversCount(
			context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 2); err == nil {
			t.Fatal("expected the refusal to be reported")
		}
		if calls != 1 {
			t.Errorf("a refusal about permission was retried as though it were about the payload: %d requests", calls)
		}
	})
}

// settingsServiceOn wires a service to a handler for the pull-request settings
// endpoint, and refuses anything else.
func settingsServiceOn(t *testing.T, handler http.HandlerFunc) (*Service, func()) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/latest/projects/PRJ/repos/demo/settings/pull-requests" {
			http.NotFound(writer, request)

			return
		}
		handler(writer, request)
	}))

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("create generated client: %v", err)
	}

	return NewService(client), server.Close
}

func TestRepositorySettingsHelperCoverage(t *testing.T) {
	permission, err := normalizeRepositoryPermission(" repo_read ")
	if err != nil || permission != "REPO_READ" {
		t.Fatalf("expected REPO_READ normalization, got permission=%q err=%v", permission, err)
	}

	_, err = normalizeRepositoryPermission("invalid")
	if err == nil {
		t.Fatal("expected validation error for invalid permission")
	}

}

// mock-inventory: unreachable-state — a bare array where an object belongs and non-JSON bodies on a 200, neither of which Bitbucket sends; the subject is what the service does with a reply it cannot read.
func TestRepositorySettingsJSONFallbackAndValidationBranches(t *testing.T) {
	service := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks":
			_, _ = writer.Write([]byte(`[1,2]`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks":
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte("created"))
		case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/pull-requests":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("updated"))
		default:
			http.NotFound(writer, request)
		}
	})

	repo := RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}

	webhooks, err := service.ListRepositoryWebhooks(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected no error listing array webhooks payload, got: %v", err)
	}
	if webhooks.Count != 2 {
		t.Fatalf("expected webhook count=2 from array payload, got: %d", webhooks.Count)
	}

	created, err := service.CreateRepositoryWebhook(context.Background(), repo, WebhookCreateInput{Name: "ci", URL: "http://example.local/hook"})
	if err != nil {
		t.Fatalf("expected no error creating webhook with non-json response, got: %v", err)
	}
	if created != nil {
		t.Fatalf("expected nil payload for non-json create response, got: %#v", created)
	}

	allTasksSettings, err := service.UpdateRepositoryPullRequestRequiredAllTasks(context.Background(), repo, true)
	if err != nil {
		t.Fatalf("expected no error updating all tasks with fallback response, got: %v", err)
	}
	if value, ok := allTasksSettings["requiredAllTasksComplete"].(bool); !ok || !value {
		t.Fatalf("expected fallback requiredAllTasksComplete=true, got: %#v", allTasksSettings)
	}

	// The plain number is what goes first and what this handler echoes, so it
	// is what comes back. It used to be the object, because the object was
	// tried first -- an order a running Bitbucket answers with 400.
	approverSettings, err := service.UpdateRepositoryPullRequestRequiredApproversCount(context.Background(), repo, 3)
	if err != nil {
		t.Fatalf("expected no error updating approvers with fallback response, got: %v", err)
	}
	// An int when the echo is the request map, a float64 when it came back
	// through JSON. This handler answers with neither, so the service echoes
	// what it sent.
	count := 0
	switch value := approverSettings["requiredApprovers"].(type) {
	case int:
		count = value
	case float64:
		count = int(value)
	default:
		t.Fatalf("expected requiredApprovers as a number, got %T: %#v", value, value)
	}
	if count != 3 {
		t.Fatalf("expected requiredApprovers 3, got: %v", count)
	}

	_, err = service.UpdateRepositoryPullRequestRequiredApproversCount(context.Background(), repo, -1)
	if err == nil {
		t.Fatal("expected validation error for negative approvers count")
	}

	if err := service.DeleteRepositoryWebhook(context.Background(), repo, " "); err == nil {
		t.Fatal("expected validation error for empty webhook id")
	}
}

func newServiceWithBaseURL(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	return NewService(client)
}

// mock-inventory: unreachable-state — "not-json" and a bare array where a settings object belongs, alongside a closed listener; the subject is that an unreadable reply is an error rather than an empty result.
func TestRepositorySettingsAdditionalBranches(t *testing.T) {
	// The pagination half of this used to live here: two hand-written pages and
	// an assertion that the second request carried the limit the first one had.
	// Both halves were the author's, so they agreed by construction, and the
	// only thing under test was the walk -- which is openapi.PageThrough's now,
	// and tested where it lives. TestLiveLimitActuallyCaps drives the listing
	// against a real Bitbucket.

	t.Run("webhooks invalid json and transport", func(t *testing.T) {
		invalidService := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte("not-json"))
		})
		if _, err := invalidService.ListRepositoryWebhooks(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}); err == nil {
			t.Fatal("expected invalid json payload error")
		}

		baseURL := testsupport.ClosedListenerURL(t)

		client, err := openapigenerated.NewClientWithResponses(baseURL)
		if err != nil {
			t.Fatalf("create generated client: %v", err)
		}
		transportService := NewService(client)
		if _, err := transportService.ListRepositoryWebhooks(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error, got: %v", err)
		}
	})

	t.Run("permission and webhook validations", func(t *testing.T) {
		// Refused before a request exists, so the listener fails the test if
		// one arrives.
		service := newServiceWithBaseURL(t, testsupport.UnreachedHandler(t))

		if err := service.GrantRepositoryUserPermission(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, " ", "REPO_READ"); err == nil {
			t.Fatal("expected username validation error")
		}
		if _, err := service.CreateRepositoryWebhook(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, WebhookCreateInput{Name: "", URL: "http://example.local"}); err == nil {
			t.Fatal("expected webhook name validation error")
		}
		if _, err := service.CreateRepositoryWebhook(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, WebhookCreateInput{Name: "ci", URL: ""}); err == nil {
			t.Fatal("expected webhook url validation error")
		}
	})

	t.Run("pull request settings decode and status branches", func(t *testing.T) {
		decodeService := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
			switch {
			case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/pull-requests":
				_, _ = writer.Write([]byte(`[]`))
			case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/pull-requests":
				_, _ = writer.Write([]byte(`[]`))
			default:
				http.NotFound(writer, request)
			}
		})

		if _, err := decodeService.GetRepositoryPullRequestSettings(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}); err == nil {
			t.Fatal("expected decode error for pull request settings map")
		}
		if _, err := decodeService.UpdateRepositoryPullRequestRequiredAllTasks(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, true); err == nil {
			t.Fatal("expected decode error for all tasks update map")
		}
		if _, err := decodeService.UpdateRepositoryPullRequestRequiredApproversCount(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 2); err == nil {
			t.Fatal("expected error for approvers update map")
		}

		// A 401 mapping to exit 3 stood here, from a handler answering 401 to
		// everything. Every call in this service goes through
		// openapi.MapStatusError, so TestMapStatusError asserts it once.
	})

	t.Run("validate repository ref branch", func(t *testing.T) {
		service := newServiceWithBaseURL(t, testsupport.UnreachedHandler(t))
		if _, err := service.ListRepositoryPermissionUsers(context.Background(), RepositoryRef{}, 10); err == nil {
			t.Fatal("expected repository validation error")
		}
	})
}

func TestRepositoryServicePermissionsValidationAdditional(t *testing.T) {
	service := NewService(nil)
	repo := RepositoryRef{ProjectKey: "P", Slug: "S"}
	if err := service.GrantRepositoryGroupPermission(context.Background(), RepositoryRef{}, "g", "p"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantRepositoryGroupPermission(context.Background(), repo, "", "p"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryGroupPermission(context.Background(), RepositoryRef{}, "g"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryGroupPermission(context.Background(), repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryUserPermission(context.Background(), RepositoryRef{}, "u"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryUserPermission(context.Background(), repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRequiredBuildsMergeChecks(context.Background(), RepositoryRef{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRepositoryServiceValidationErrors(t *testing.T) {
	service := NewService(nil)
	repo := RepositoryRef{ProjectKey: "P", Slug: "S"}
	emptyRepo := RepositoryRef{}
	ctx := context.Background()

	// RepositoryRef validation check on all new methods
	if _, err := service.GetRepositoryAutoMergeSettings(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryAutoMergeSettings(ctx, emptyRepo, true); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteRepositoryAutoMergeSettings(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetRepositoryAutoDeclineSettings(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryAutoDeclineSettings(ctx, emptyRepo, true, 4); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteRepositoryAutoDeclineSettings(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRepositoryLabels(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if err := service.AddRepositoryLabel(ctx, emptyRepo, "label"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RemoveRepositoryLabel(ctx, emptyRepo, "label"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.WatchRepository(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if err := service.UnwatchRepository(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListDefaultTasks(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.AddDefaultTask(ctx, emptyRepo, "desc", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateDefaultTask(ctx, emptyRepo, "123", "desc", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteDefaultTask(ctx, emptyRepo, "123"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhook(ctx, emptyRepo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateWebhook(ctx, emptyRepo, "1", "name", "url", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.TestWebhook(ctx, emptyRepo, "1", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatistics(ctx, emptyRepo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatisticsSummary(ctx, emptyRepo, "1"); err == nil {
		t.Fatal("expected error")
	}

	// Parameter validations
	if err := service.AddRepositoryLabel(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RemoveRepositoryLabel(ctx, repo, " "); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.AddDefaultTask(ctx, repo, "", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateDefaultTask(ctx, repo, "", "desc", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateDefaultTask(ctx, repo, "123", "", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteDefaultTask(ctx, repo, " "); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhook(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateWebhook(ctx, repo, "", "name", "url", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.TestWebhook(ctx, repo, "", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.TestWebhook(ctx, repo, "not-an-int", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatistics(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatisticsSummary(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
}

// TestNewRepositorySettingsMethods is gone rather than moved.
//
// It drove twenty-odd service methods -- auto-merge, auto-decline, labels,
// watch, default tasks, webhook lifecycle -- past a handwritten Bitbucket and
// asserted that each returned what the fixture beside it had written. Every
// one of them is a command, and command reach is 234/234, so each is asserted
// against a real instance with the state read back.
//
// Two of them were not commands. SearchWebhooks and
// GetWebhookLatestInvocation had no caller anywhere: not the CLI, not the MCP
// server. This test was the only thing keeping them compiled, and a mock is
// the only place they could ever have been called from. They are deleted.
