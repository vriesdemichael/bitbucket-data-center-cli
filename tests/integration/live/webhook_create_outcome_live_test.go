//go:build live

package live_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveWebhookCreateLooksBeforeReportingAnUnknownOutcome covers what a
// webhook create does when it cannot hear Bitbucket's answer: it looks for the
// webhook, reports it when it is there, and stays unknown_outcome when it is
// not, without sending the create again.
//
// Not parallel: bb reaches Bitbucket through a proxy named in BITBUCKET_URL.
func TestLiveWebhookCreateLooksBeforeReportingAnUnknownOutcome(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const receiver = "http://localhost:7990/status"
	for _, scope := range []struct {
		name     string
		listPath string
		create   func(name string) []string
	}{
		{
			name:     "repository",
			listPath: fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/webhooks", seeded.Key, repo.Slug),
			create:   func(name string) []string { return []string{"--json", "webhook", "create", name, receiver} },
		},
		{
			name:     "project",
			listPath: fmt.Sprintf("/rest/api/latest/projects/%s/webhooks", seeded.Key),
			create: func(name string) []string {
				return []string{"--json", "project", "webhook", "create", seeded.Key, name, receiver}
			},
		},
	} {
		t.Run(scope.name+" create whose answer was lost reports the webhook", func(t *testing.T) {
			t.Setenv("BITBUCKET_URL", loseWebhookCreateAnswer(t, harness.config.BitbucketURL, true))
			name := testsupport.UniqueName("lost-answer-")

			output, err := executeLiveCLI(t, scope.create(name)...)
			if err != nil {
				t.Fatalf("Bitbucket stored the webhook and bb reported a failure: %v\n%s", err, output)
			}
			if hook, _ := decodeJSONMap(t, output)["webhook"].(map[string]any); hook["name"] != name {
				t.Errorf("bb reported %v, want the webhook named %s", hook, name)
			}
			if count := liveWebhooksNamed(t, ctx, harness, scope.listPath, name); count != 1 {
				t.Fatalf("%d webhooks named %s, want the one create", count, name)
			}
		})

		t.Run(scope.name+" create that never arrived stays unknown", func(t *testing.T) {
			t.Setenv("BITBUCKET_URL", loseWebhookCreateAnswer(t, harness.config.BitbucketURL, false))
			name := testsupport.UniqueName("never-sent-")

			output, err := executeLiveCLI(t, scope.create(name)...)
			if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) {
				t.Fatalf("got %v, want unknown_outcome\n%s", err, output)
			}
			if count := liveWebhooksNamed(t, ctx, harness, scope.listPath, name); count != 0 {
				t.Fatalf("%d webhooks named %s for a create Bitbucket never received", count, name)
			}
		})
	}
}

// loseWebhookCreateAnswer puts a proxy between bb and Bitbucket that loses one
// thing: the answer to a webhook create. With forward, Bitbucket receives the
// create and applies it, and the connection closes before the answer reaches
// bb. Without, the create never reaches Bitbucket, and the connection closes
// the same way. Every other request passes through.
//
// It is the network failing, not a stand-in for Bitbucket: every answer bb
// reads is one the server sent (ADR-079).
func loseWebhookCreateAnswer(t *testing.T, bitbucketURL string, forward bool) string {
	t.Helper()

	target, err := url.Parse(bitbucketURL)
	if err != nil {
		t.Fatalf("parse %s: %v", bitbucketURL, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || path.Base(request.URL.Path) != "webhooks" {
			proxy.ServeHTTP(writer, request)
			return
		}

		if forward {
			proxy.ServeHTTP(httptest.NewRecorder(), request)
		} else {
			_, _ = io.Copy(io.Discard, request.Body)
		}

		connection, _, err := http.NewResponseController(writer).Hijack()
		if err != nil {
			t.Errorf("take over the create's connection: %v", err)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)

	return server.URL
}

// liveWebhooksNamed counts a scope's webhooks with a name, asking Bitbucket
// directly rather than through any proxy.
func liveWebhooksNamed(t *testing.T, ctx context.Context, harness *liveHarness, listPath, name string) int {
	t.Helper()

	listing, err := harness.liveJSON(ctx, http.MethodGet, listPath+"?limit=1000", nil)
	if err != nil {
		t.Fatalf("list webhooks at %s: %v", listPath, err)
	}

	count := 0
	values, _ := listing["values"].([]any)
	for _, value := range values {
		if hook, ok := value.(map[string]any); ok && hook["name"] == name {
			count++
		}
	}

	return count
}
