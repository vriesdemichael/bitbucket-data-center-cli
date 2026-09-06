//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// deleteProjectAndContents removes a seeded project and everything in it.
//
// Bitbucket refuses to delete a project that still holds repositories -- 409,
// "cannot be deleted because it has repositories" -- so the repositories go
// first. The harness used to issue the project delete alone and discard the
// response, which is why every test since had been leaving its fixtures on the
// instance.
//
// Repository deletion is asynchronous: the call is accepted and the repository
// disappears some moments later, so the project delete is retried until it
// takes rather than attempted once and hoped for.
func (h *liveHarness) deleteProjectAndContents(ctx context.Context, projectKey string) {
	for _, slug := range h.repositorySlugsIn(ctx, projectKey) {
		_, _ = h.liveJSON(ctx, http.MethodDelete,
			fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s", projectKey, slug), nil)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		_, err := h.liveJSON(ctx, http.MethodDelete, "/rest/api/latest/projects/"+projectKey, nil)
		if err == nil {
			return
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			// Reported, not failed: a test that passed did its job, and a
			// cleanup that could not finish is the harness's problem to see in
			// the log rather than a red test.
			h.t.Logf("leaving project %s behind: %v", projectKey, err)

			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// repositorySlugsIn lists what a project holds, one page deep.
//
// A page of 100 is more repositories than any test seeds; a project holding
// more than that is not one of ours.
func (h *liveHarness) repositorySlugsIn(ctx context.Context, projectKey string) []string {
	payload, err := h.liveJSON(ctx, http.MethodGet,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos?limit=100", projectKey), nil)
	if err != nil {
		return nil
	}

	values, _ := payload["values"].([]any)
	slugs := make([]string, 0, len(values))
	for _, value := range values {
		repository, _ := value.(map[string]any)
		if repository == nil {
			continue
		}
		if slug, _ := repository["slug"].(string); slug != "" {
			slugs = append(slugs, slug)
		}
	}

	return slugs
}

// projectRepositoryListing is the project's own repositories, as text a test
// can compare against itself.
//
// The dry-run tests used to compare `bb repo list` unscoped, before and
// after, byte for byte. Instance-wide, that is a claim about every repository
// on the server: with the suite running in parallel, another test creating one
// between the two calls made the listing differ for a reason that had nothing
// to do with the dry run. Scoped to the project the test seeded, only this test
// writes to it.
func projectRepositoryListing(t *testing.T, projectKey string) string {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "repo", "list", "--project", projectKey, "--limit", "200")
	if err != nil {
		t.Fatalf("repo list for %s failed: %v\noutput: %s", projectKey, err, output)
	}

	return output
}
