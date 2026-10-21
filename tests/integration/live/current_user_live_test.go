//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestLiveACallerWhoseNameIsNotTheirSlug covers #807: a user whose login name
// holds a + and an @, as names under LDAP and SSO often do. Bitbucket names the
// caller in X-AUSERNAME by that name, percent-encoded, and gives them a slug
// that differs from it. bb used the header as it came, as the name and as the
// slug, so every lookup it fed went wrong for such a user.
//
// Each subtest reads the result back through a request of its own: what bb
// says about the user against what Bitbucket stores, and a review status
// against the pull request.
func TestLiveACallerWhoseNameIsNotTheirSlug(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	name := "LT.Who+" + uniqueSuffix() + "@example.local"
	person := createLicensedUserNamed(ctx, t, harness, name)
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, name, "REPO_WRITE"); err != nil {
		t.Fatalf("grant %s write access failed: %v", name, err)
	}
	slug := userSlug(ctx, t, harness, name)
	if slug == name {
		t.Fatalf("Bitbucket gave %s the slug %s, the same as the name, so nothing here could tell them apart", name, slug)
	}
	// Bitbucket finds a user by any name, display name or email address that
	// contains the text, so a common name has namesakes, and they can fill the
	// first pages of the search ahead of the person.
	createNamesakes(ctx, t, harness, name, 100)

	// A pull request somebody else wrote, with the person reviewing it.
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/reviewed", "reviewed.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "feature/reviewed", "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	if _, err := harness.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/participants", seeded.Key, repo.Slug, pullRequestID),
		map[string]any{"user": map[string]any{"name": name}, "role": "REVIEWER"}); err != nil {
		t.Fatalf("add %s as a reviewer failed: %v", name, err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, person)

	t.Run("bb auth identity names the person and their slug", func(t *testing.T) {
		user := nestedJSONMap(t, mustLiveCLI(t, "auth", "identity"), "user")
		if user["name"] != name || user["slug"] != slug {
			t.Errorf("bb auth identity says name %v, slug %v; Bitbucket stores %s, %s", user["name"], user["slug"], name, slug)
		}
	})

	// A repository token acts as a user Bitbucket makes for it, which has a
	// name and no slug.
	t.Run("bb auth identity names a repository token's user", func(t *testing.T) {
		created, err := harness.liveJSON(ctx, http.MethodPut, "/rest/access-tokens/latest/projects/"+seeded.Key+"/repos/"+repo.Slug,
			map[string]any{"name": "lt-identity-" + uniqueSuffix(), "permissions": []string{"REPO_READ"}, "expiryDays": 1})
		if err != nil {
			t.Fatalf("create the repository token failed: %v", err)
		}
		actOnToken(t, stringField(created, "token"))

		user := nestedJSONMap(t, mustLiveCLI(t, "auth", "identity"), "user")
		tokenUser, _ := user["name"].(string)
		if !strings.HasPrefix(tokenUser, "access-token-user/") || user["slug"] != nil {
			t.Errorf("bb auth identity says name %v, slug %v; want the token's user, access-token-user/..., with no slug", user["name"], user["slug"])
		}
	})

	t.Run("bb pr review set NEEDS_WORK sets the person's own status", func(t *testing.T) {
		mustLiveCLI(t, "pr", "review", "set", pullRequestID, "NEEDS_WORK", "--repo", repoRef)
		if status := mcpLiveReviewer(t, mcpLivePullRequest(t, repoRef, pullRequestID), name)["status"]; status != "NEEDS_WORK" {
			t.Errorf("%s's status reads back as %v, want NEEDS_WORK", name, status)
		}

		// The dry run finds the person among the reviewers by name, so a status
		// they already hold is no change.
		output := mustLiveCLI(t, "--json", "--dry-run", "pr", "review", "set", pullRequestID, "NEEDS_WORK", "--repo", repoRef)
		assertLivePreviewOf(t, output, "pr review set", jsonoutput.OutcomeNoOp)
	})

	t.Run("a view knows the person among the reviewers", func(t *testing.T) {
		capabilities := &mcp.ClientCapabilities{}
		capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
		executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show", Arguments: map[string]any{
				"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": pullRequestID,
			}})
			if err != nil || result.IsError {
				t.Fatalf("show: %v %s", err, mcpResultText(result))
			}
			payload, _ := result.Meta[viewPayloadKey].(map[string]any)
			me, _ := payload["me"].(map[string]any)
			if me["username"] != name || me["status"] != "NEEDS_WORK" || me["author"] == true {
				t.Errorf("the view sees the person as %v, want %s, a reviewer who requested changes", me, name)
			}
		}, "ai", "mcp", "serve")
	})

	// On a token no username is configured, so only the name Bitbucket gives
	// the caller can tell bb who the author is. The dry run shows the
	// reviewers bb would send: Bitbucket 10.5 drops an author from them
	// itself, so a real create would not show what bb chose.
	t.Run("bb pr create leaves the author out of the reviewers", func(t *testing.T) {
		created, err := harness.liveJSONAs(ctx, person, http.MethodPut, "/rest/access-tokens/latest/users/"+url.PathEscape(slug),
			map[string]any{"name": "lt-author-" + uniqueSuffix(), "permissions": []string{"REPO_WRITE"}, "expiryDays": 1})
		if err != nil {
			t.Fatalf("create the person's token failed: %v", err)
		}
		actOnToken(t, stringField(created, "token"))

		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/authored", "authored.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}
		output := mustLiveCLI(t, "--json", "--dry-run", "pr", "create", "--repo", repoRef, "--from-ref", "feature/authored",
			"--to-ref", "master", "--title", "Authored by the person", "--reviewers", name+","+harness.username())
		document := assertLivePreview(t, output, jsonoutput.OutcomeWouldApply)
		if reviewers := fmt.Sprint(document.effect(t, output).Target["reviewers"]); reviewers != "["+harness.username()+"]" {
			t.Errorf("bb pr create would send the reviewers %s, want %s alone", reviewers, harness.username())
		}
	})
}

// actOnToken has bb act on token alone, with no username configured.
func actOnToken(t *testing.T, token string) {
	t.Helper()

	setLiveCredentials(t, restrictedUser{})
	t.Setenv("BITBUCKET_TOKEN", token)
	for _, variable := range []string{"BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD", "ADMIN_USER", "ADMIN_PASSWORD"} {
		t.Setenv(variable, "")
	}
}

// createNamesakes creates count users without a licence whose display name
// holds name and sorts ahead of it, so that a search for name lists them first.
func createNamesakes(ctx context.Context, t *testing.T, harness *liveHarness, name string, count int) {
	t.Helper()

	suffix := uniqueSuffix()
	failures := make(chan error, count)
	var created sync.WaitGroup
	// Bitbucket hashes each password, so a few at a time is quicker than one.
	slots := make(chan struct{}, 8)
	for index := range count {
		login := fmt.Sprintf("LT.Namesake%03d.%s", index, suffix)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = harness.deleteRestrictedUser(cleanupCtx, login)
		})
		created.Add(1)
		go func() {
			defer created.Done()
			slots <- struct{}{}
			defer func() { <-slots }()

			password := "Ltp@ss" + uniqueSuffix() + "!"
			addToDefaultGroup := false
			response, err := harness.client.CreateUser(ctx, &openapigenerated.CreateUserParams{
				Name:              login,
				Password:          &password,
				DisplayName:       "Aa " + name,
				EmailAddress:      strings.ToLower(login) + "@example.local",
				AddToDefaultGroup: &addToDefaultGroup,
			})
			if err != nil {
				failures <- fmt.Errorf("create user %s: %w", login, err)
				return
			}
			response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				failures <- fmt.Errorf("create user %s returned status %d", login, response.StatusCode)
			}
		}()
	}
	created.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}

	// The search must list them ahead of the person, or this covers nothing.
	first, err := harness.liveJSON(ctx, http.MethodGet, "/rest/api/latest/users?limit="+strconv.Itoa(count)+"&filter="+url.QueryEscape(name), nil)
	if err != nil {
		t.Fatalf("search for %s: %v", name, err)
	}
	values, _ := first["values"].([]any)
	for _, value := range values {
		if user, _ := value.(map[string]any); user["name"] == name {
			t.Fatalf("a search for %s lists the person among its first %d users, ahead of the namesakes", name, count)
		}
	}
}
