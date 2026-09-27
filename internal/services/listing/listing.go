// Package listing lists what a typed prefix could become, for the two places
// bb completes a value: the shell (internal/cli/completion) and an MCP client
// filling in a resource template or a prompt argument (internal/mcp).
//
// It lives apart from both because the knowledge is shared and easy to lose:
// Bitbucket filters projects and repositories by their display names, while
// the value being completed is a key or a slug.
package listing

import (
	"context"
	"strings"
	"sync"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	projectservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/project"
)

// Projects asks for the projects a typed prefix could become, up to maxResults
// from each listing.
//
// Two listings rather than one, because Bitbucket's filter and the value being
// completed are not the same thing: `name` matches the project's display name,
// while the candidate is its key. Filtering is what reaches past the first page
// on an instance with more projects than one -- PLATFORM against "Platform
// Services" -- and it matches nothing at all where the two do not resemble each
// other, so the unfiltered page is asked for as well.
func Projects(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	prefix string,
	maxResults int,
) ([]openapigenerated.RestProject, error) {
	service := projectservice.NewService(client)

	listings := []func(context.Context) ([]openapigenerated.RestProject, error){
		func(ctx context.Context) ([]openapigenerated.RestProject, error) {
			return service.List(ctx, projectservice.ListOptions{MaxResults: maxResults})
		},
	}

	if strings.TrimSpace(prefix) != "" {
		listings = append(listings, func(ctx context.Context) ([]openapigenerated.RestProject, error) {
			return service.List(ctx, projectservice.ListOptions{Name: prefix, MaxResults: maxResults})
		})
	}

	return Together(ctx, listings)
}

// Repositories asks for the repositories of one project, narrowed by what has
// been typed of the slug, up to maxResults from each listing.
//
// The filter is the server's. `projectkey` is what scopes it -- the
// project-scoped listing at /projects/{key}/repos accepts a `name` parameter
// and ignores it, which returns the whole project and looks exactly like a
// filter that matched everything.
//
// `name` matches the repository's name, while the value being completed is its
// slug, and the two part company as soon as a name has a space in it: "Data
// Center CLI" is data-center-cli, and the slug's prefix matches neither. So the
// project's own page is asked for beside the filtered one, which is the answer
// for every project small enough to fit in it.
func Repositories(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	projectKey string,
	prefix string,
	maxResults int,
) ([]openapigenerated.RestRepository, error) {
	listings := []func(context.Context) ([]openapigenerated.RestRepository, error){
		func(ctx context.Context) ([]openapigenerated.RestRepository, error) {
			return repositoryPage(ctx, client, projectKey, prefix, maxResults)
		},
	}

	if strings.TrimSpace(prefix) != "" {
		listings = append(listings, func(ctx context.Context) ([]openapigenerated.RestRepository, error) {
			return repositoryPage(ctx, client, projectKey, "", maxResults)
		})
	}

	return Together(ctx, listings)
}

// repositoryPage is one page of the instance-wide repository listing.
//
// The generated client rather than internal/services/repository, whose
// ListOptions carries `name` and `projectname` but not `projectkey`: filtering
// by the project's display name would be filtering by something the caller did
// not type.
func repositoryPage(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	projectKey string,
	prefix string,
	maxResults int,
) ([]openapigenerated.RestRepository, error) {
	limit := float32(maxResults)
	params := &openapigenerated.GetRepositories1Params{Limit: &limit}

	if key := strings.TrimSpace(projectKey); key != "" {
		params.Projectkey = &key
	}
	if name := strings.TrimSpace(prefix); name != "" {
		params.Name = &name
	}

	response, err := client.GetRepositories1WithResponse(ctx, params)
	if err != nil {
		return nil, apperrors.Transport("failed to list repositories", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	page := response.ApplicationjsonCharsetUTF8200
	if page == nil || page.Values == nil {
		return nil, nil
	}

	return *page.Values, nil
}

// Together runs the listings of one stage at the same time.
//
// A stage asks for two pages -- one narrowed by what was typed, one not -- and
// a completion has a single deadline covering both. Run in turn they would
// spend it twice over on an instance that is merely far away. The results are
// concatenated in the order asked for, the narrowed one first, and the caller
// drops the duplicates.
//
// An error is returned only when every listing failed: one page is a complete
// answer, and half an answer beats none.
func Together[T any](ctx context.Context, listings []func(context.Context) ([]T, error)) ([]T, error) {
	results := make([][]T, len(listings))
	failures := make([]error, len(listings))

	var waiting sync.WaitGroup
	for index, listing := range listings {
		waiting.Add(1)

		go func() {
			defer waiting.Done()
			defer func() {
				// A caller recovers its own goroutine, not the ones started
				// here, so a panic would end the process: in a shell with a
				// stack trace it has nowhere to put, in an MCP server with
				// the conversation.
				if recovered := recover(); recovered != nil {
					failures[index] = apperrors.New(apperrors.KindInternal, "listing failed", nil)
				}
			}()

			results[index], failures[index] = listing(ctx)
		}()
	}
	waiting.Wait()

	gathered := make([]T, 0)
	answered := false

	for index := range listings {
		if failures[index] != nil {
			continue
		}

		answered = true
		gathered = append(gathered, results[index]...)
	}

	if !answered {
		if len(failures) == 0 {
			return nil, nil
		}

		return nil, failures[0]
	}

	return gathered, nil
}
