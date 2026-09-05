// Package contract holds the assertions every paged service keeps, asked once
// rather than once per service.
package contract_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	browseservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/browse"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	commitservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commit"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	gpgkeyservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/gpgkey"
	jiraservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/jira"
	projectservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/project"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	activityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
	reposettingsservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
	repositoryservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
	sshkeyservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/sshkey"
	tagservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/tag"
	tokenservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/token"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

const (
	contractProject = "PRJ"
	contractSlug    = "demo"
)

// clients builds both client shapes against one base URL, because the services
// are split between the generated client and the hand-rolled one.
type clients struct {
	generated *openapigenerated.ClientWithResponses
	http      *httpclient.Client
}

func clientsFor(t *testing.T, baseURL string) clients {
	t.Helper()

	generated, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	return clients{
		generated: generated,
		http: httpclient.NewFromConfig(config.AppConfig{
			BitbucketURL:   baseURL,
			RequestTimeout: 5 * time.Second,
			RetryCount:     0,
		}),
	}
}

// pagedListing is one call that walks pages, named for the package it belongs
// to.
type pagedListing struct {
	name string
	call func(ctx context.Context, c clients) error
}

// listings is one paged call per package that owns one.
//
// A package missing from this table has no proof that a listing which loses the
// connection reports it. Adding a paged service means adding a row.
func listings() []pagedListing {
	branchRepo := branchservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug}

	return []pagedListing{
		{"branch", func(ctx context.Context, c clients) error {
			_, err := branchservice.NewService(c.generated).List(ctx, branchRepo, branchservice.ListOptions{MaxResults: 25})

			return err
		}},
		{"browse", func(ctx context.Context, c clients) error {
			_, err := browseservice.NewService(c.generated, c.http).Tree(ctx,
				browseservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug},
				"docs", browseservice.TreeOptions{MaxResults: 25})

			return err
		}},
		{"comment", func(ctx context.Context, c clients) error {
			_, err := commentservice.NewService(c.generated).List(ctx, commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug},
				PullRequestID: "1",
			}, "file.txt", 25)

			return err
		}},
		{"commit", func(ctx context.Context, c clients) error {
			_, err := commitservice.NewService(c.generated).List(ctx,
				commitservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug},
				commitservice.ListOptions{MaxResults: 25})

			return err
		}},
		{"diff", func(ctx context.Context, c clients) error {
			_, err := diffservice.NewService(c.generated).CompareChanges(ctx,
				diffservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug}, "main", "feature", 25)

			return err
		}},
		{"gpgkey", func(ctx context.Context, c clients) error {
			_, err := gpgkeyservice.NewService(c.generated).ListGpgKeys(ctx, 25)

			return err
		}},
		{"jira", func(ctx context.Context, c clients) error {
			_, err := jiraservice.NewService(c.http).GetIssueCommits(ctx, "ISSUE-1", 25)

			return err
		}},
		{"project", func(ctx context.Context, c clients) error {
			_, err := projectservice.NewService(c.generated).List(ctx, projectservice.ListOptions{MaxResults: 25})

			return err
		}},
		{"project restrictions", func(ctx context.Context, c clients) error {
			_, err := projectservice.NewService(c.generated).ListRestrictions(ctx, contractProject,
				projectservice.RestrictionListOptions{MaxResults: 25})

			return err
		}},
		{"pullrequest", func(ctx context.Context, c clients) error {
			_, err := pullrequestservice.NewService(c.http).List(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug},
				pullrequestservice.ListOptions{MaxResults: 25})

			return err
		}},
		{"pullrequest commits", func(ctx context.Context, c clients) error {
			_, err := pullrequestservice.NewService(c.http).ListCommits(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug}, "1",
				pullrequestservice.PageOptions{MaxResults: 25})

			return err
		}},
		{"pullrequestactivity", func(ctx context.Context, c clients) error {
			_, err := activityservice.NewService(c.generated).List(ctx,
				activityservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug}, "1",
				activityservice.ListOptions{MaxResults: 25})

			return err
		}},
		{"quality", func(ctx context.Context, c clients) error {
			_, err := qualityservice.NewService(c.generated).GetBuildStatuses(ctx, "abc", 25, "")

			return err
		}},
		{"reposettings", func(ctx context.Context, c clients) error {
			_, err := reposettingsservice.NewService(c.generated).ListRepositoryPermissionUsers(ctx,
				reposettingsservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug}, 25)

			return err
		}},
		{"repository", func(ctx context.Context, c clients) error {
			_, err := repositoryservice.NewService(c.http).ListByProject(ctx, contractProject,
				repositoryservice.ListOptions{MaxResults: 25})

			return err
		}},
		{"sshkey", func(ctx context.Context, c clients) error {
			_, err := sshkeyservice.NewService(c.generated).ListUserKeys(ctx, 25, 0)

			return err
		}},
		{"tag", func(ctx context.Context, c clients) error {
			_, err := tagservice.NewService(c.generated).List(ctx,
				tagservice.RepositoryRef{ProjectKey: contractProject, Slug: contractSlug},
				tagservice.ListOptions{MaxResults: 25})

			return err
		}},
		{"token", func(ctx context.Context, c clients) error {
			_, err := tokenservice.NewService(c.generated).List(ctx, tokenservice.ScopeUser, "admin", 25)

			return err
		}},
	}
}

// TestEveryPagedListingReportsALostConnection is the one thing a live suite
// cannot ask of these services, and the one every one of them now shares.
//
// Eighteen packages were converted to openapi.PageThrough, which moved each
// one's failure handling into a closure the loop calls. A closure that swallows
// its error returns the pages it managed and no error at all, and a caller
// cannot tell a short listing from a complete one -- the same shape as every
// --limit that quietly returned everything, pointing the other way.
//
// A server that is not there is the cheapest total failure and the one a real
// Bitbucket cannot be asked to perform. Transient is the kind that matters:
// telling a caller their request was invalid, or the resource gone, when the
// truth is that the connection dropped, sends them to fix the wrong thing and
// stops an agent retrying something that would succeed.
//
// mock-inventory: transport-fault — the listener is closed before the call, which no live instance can be asked to do; the subject is that every paged service classifies a dead connection rather than returning a short answer.
func TestEveryPagedListingReportsALostConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	baseURL := server.URL
	server.Close()

	for _, listing := range listings() {
		t.Run(listing.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			err := listing.call(ctx, clientsFor(t, baseURL))
			if err == nil {
				t.Fatal("a listing against a closed server returned no error, so a caller cannot tell it from an empty repository")
			}
			if code := apperrors.ExitCode(err); code != 10 {
				t.Errorf("exit code = %d, want 10 (transient): %v", code, err)
			}
		})
	}
}
