//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestLiveCodeOwnersEndpointContract records what Bitbucket's code-owners
// endpoint is, because nothing else does.
//
// ADR-080 decided bb should ask the server who the code owners are rather than
// parse CODEOWNERS itself, and that decision stands: our parser and Bitbucket
// disagreed on six points of the same file, and an endpoint that answers
// correctly beats a documented parser that answers wrongly. But the endpoint it
// asks is not a public API and cannot be vendored (OPENAPI-030):
//
//   - The vendored specification describes 26 namespaces and `ui` is not one of
//     them. No path in it mentions code-owners.
//   - The instance serves no specification of its own. /rest/openapi/latest,
//     /rest/api/latest/swagger.json, /rest/swagger.json, /rest/api/openapi.json
//     and /rest/ui/latest/openapi.json all answer 404, and no openapi or swagger
//     document exists anywhere on the container filesystem.
//   - The resource is
//     com.atlassian.bitbucket.internal.rest.codeowners.CodeOwnersResource, and
//     the evaluation behind it is com.atlassian.bitbucket.internal.codeowners.*.
//     "internal" is in both package paths. That is the stability statement.
//
// So the contract is recorded here instead, and the live suite is what checks
// it. This is deliberately about the *shape* of the exchange -- the path, the
// parameters, the envelope, the refusals -- and not about which owners come
// back for which rule, which is TestLiveCodeOwnersPatternSyntax and
// TestLiveCodeOwnersOwnerSyntax. Those would still pass if Atlassian renamed a
// field or moved the route, because bb would simply report no owners.
func TestLiveCodeOwnersEndpointContract(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	owner, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the code owner failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, owner.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant the code owner read access failed: %v", err)
	}

	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master",
		".bitbucket/CODEOWNERS", "*.md @"+owner.Username+"\n"); err != nil {
		t.Fatalf("push CODEOWNERS failed: %v", err)
	}
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "feature/contract", "notes.md", "owned\n"); err != nil {
		t.Fatalf("push the change failed: %v", err)
	}

	path := fmt.Sprintf("/rest/ui/latest/projects/%s/repos/%s/code-owners", seeded.Key, repo.Slug)

	t.Run("the route is there and answers the documented envelope", func(t *testing.T) {
		// The three parameters the annotations name: sourceRefId, targetRefId
		// and the optional sourceRepo.
		payload, err := harness.liveJSON(ctx, http.MethodGet,
			path+"?sourceRefId=refs/heads/feature/contract&targetRefId=refs/heads/master", nil)
		if err != nil {
			t.Fatalf("the code-owners route did not answer: %v", err)
		}

		// {"codeOwners":[<user>]} on 10.4.2. The service reads the name, falling
		// back to the slug, so both are worth pinning.
		owners, ok := payload["codeOwners"].([]any)
		if !ok {
			encoded, _ := json.Marshal(payload)
			t.Fatalf("the response carries no codeOwners array, so the envelope changed: %s", encoded)
		}
		if len(owners) != 1 {
			t.Fatalf("expected one code owner for a *.md change, got %d: %v", len(owners), owners)
		}

		entry, ok := owners[0].(map[string]any)
		if !ok {
			t.Fatalf("a code owner is not an object: %#v", owners[0])
		}
		if _, hasName := entry["name"]; !hasName {
			t.Errorf("a code owner carries no name field, which is what bb reads: %#v", entry)
		}
		if _, hasSlug := entry["slug"]; !hasSlug {
			t.Errorf("a code owner carries no slug field, which is bb's fallback: %#v", entry)
		}
		if name, _ := entry["name"].(string); !strings.EqualFold(name, owner.Username) {
			t.Errorf("code owner = %q, want %s", name, owner.Username)
		}
	})

	t.Run("a ref that is not there is refused, not answered empty", func(t *testing.T) {
		// An empty answer would be indistinguishable from a change nobody owns,
		// which is the shape of mistake OPENAPI-029 records elsewhere.
		if _, err := harness.liveJSON(ctx, http.MethodGet,
			path+"?sourceRefId=refs/heads/no-such-branch&targetRefId=refs/heads/master", nil); err == nil {
			t.Error("a source ref that does not exist was answered rather than refused")
		}
	})

	t.Run("identical refs are refused", func(t *testing.T) {
		// There is no diff to attribute, so there is no answer to give.
		if _, err := harness.liveJSON(ctx, http.MethodGet,
			path+"?sourceRefId=refs/heads/master&targetRefId=refs/heads/master", nil); err == nil {
			t.Error("identical source and target refs were answered rather than refused")
		}
	})

	t.Run("a repository that is not there is refused", func(t *testing.T) {
		missing := fmt.Sprintf("/rest/ui/latest/projects/%s/repos/no-such-repository/code-owners"+
			"?sourceRefId=refs/heads/feature/contract&targetRefId=refs/heads/master", seeded.Key)
		if _, err := harness.liveJSON(ctx, http.MethodGet, missing, nil); err == nil {
			t.Error("a repository that does not exist was answered rather than refused")
		}
	})

	t.Run("bb survives the route disappearing", func(t *testing.T) {
		// The reason an internal endpoint is tolerable at all. If Atlassian
		// removes it, `pr create` skips code owners rather than failing, because
		// the call site quiets only openapi.IsRouteMissing. Asking a namespace
		// that does not exist is the closest a live instance gets to that.
		if _, err := harness.liveJSON(ctx, http.MethodGet,
			fmt.Sprintf("/rest/ui/latest/projects/%s/repos/%s/code-owners-gone", seeded.Key, repo.Slug), nil); err == nil {
			t.Error("a route that does not exist answered successfully")
		}

		// And the command itself still opens a pull request.
		if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "feature/still-works", "plain.txt", "x\n"); err != nil {
			t.Fatalf("push failed: %v", err)
		}
		output := mustLiveCLI(t, "pr", "create",
			"--from-ref", "feature/still-works", "--to-ref", "refs/heads/master",
			"--title", "Opens with code owners on", "--no-default-reviewers")
		if id, _ := extractPRData(decodeJSONMap(t, output))["id"].(float64); id == 0 {
			t.Errorf("pr create with --codeowners did not report an id:\n%s", output)
		}
	})
}
