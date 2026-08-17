//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveRepoPermissionShallowAliasesMatchDeepPaths pins the shallow spellings
// added for issue #338 to the canonical deep ones.
//
// The point of an alias is that it is not a second implementation. Byte
// equality against a real Bitbucket is what keeps that true: if the two ever
// stop being the same command, this fails rather than the two quietly drifting
// the way hand-maintained duplicates do.
func TestLiveRepoPermissionShallowAliasesMatchDeepPaths(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	deepUserList, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("deep users list failed: %v\noutput: %s", err, deepUserList)
	}
	shallowUserList, err := executeLiveCLI(t, "--json", "repo", "permissions", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("shallow permissions list failed: %v\noutput: %s", err, shallowUserList)
	}
	if deepUserList != shallowUserList {
		t.Fatalf("repo permissions list diverged from the deep path\ndeep:    %s\nshallow: %s", deepUserList, shallowUserList)
	}

	deepGroupList, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("deep groups list failed: %v\noutput: %s", err, deepGroupList)
	}
	shallowGroupList, err := executeLiveCLI(t, "--json", "repo", "permissions", "list", "--group", "--limit", "100")
	if err != nil {
		t.Fatalf("shallow permissions list --group failed: %v\noutput: %s", err, shallowGroupList)
	}
	if deepGroupList != shallowGroupList {
		t.Fatalf("repo permissions list --group diverged from the deep path\ndeep:    %s\nshallow: %s", deepGroupList, shallowGroupList)
	}

	// grant and revoke are compared under --dry-run: the predicted plan is the
	// whole of what distinguishes the two spellings, and comparing it does not
	// leave a mutation for the other half of the comparison to trip over.
	deepGrant, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "grant", "alias-parity-user", "REPO_WRITE")
	if err != nil {
		t.Fatalf("deep users grant dry-run failed: %v\noutput: %s", err, deepGrant)
	}
	shallowGrant, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "permissions", "grant", "alias-parity-user", "REPO_WRITE")
	if err != nil {
		t.Fatalf("shallow permissions grant dry-run failed: %v\noutput: %s", err, shallowGrant)
	}
	if deepGrant != shallowGrant {
		t.Fatalf("repo permissions grant diverged from the deep path\ndeep:    %s\nshallow: %s", deepGrant, shallowGrant)
	}

	deepRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "revoke", "alias-parity-group")
	if err != nil {
		t.Fatalf("deep groups revoke dry-run failed: %v\noutput: %s", err, deepRevoke)
	}
	shallowRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "permissions", "revoke", "--group", "alias-parity-group")
	if err != nil {
		t.Fatalf("shallow permissions revoke --group dry-run failed: %v\noutput: %s", err, shallowRevoke)
	}
	if deepRevoke != shallowRevoke {
		t.Fatalf("repo permissions revoke --group diverged from the deep path\ndeep:    %s\nshallow: %s", deepRevoke, shallowRevoke)
	}
}

// TestLivePullRequestStatus exercises bb pr status against a real Bitbucket.
//
// The two dashboard sections are cross-repository and always answerable. The
// current-branch section is not: the live CLI does not run inside a Bitbucket
// checkout, so it reports why rather than failing, and asserting that is
// asserting the degradation actually degrades.
func TestLivePullRequestStatus(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	output, err := executeLiveCLI(t, "--json", "pr", "status")
	if err != nil {
		t.Fatalf("pr status failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	for _, section := range []string{"current_branch", "created_by_you", "requesting_your_review"} {
		value, ok := payload[section].(map[string]any)
		if !ok {
			t.Fatalf("expected %q section in pr status output: %s", section, output)
		}
		if _, ok := value["pull_requests"]; !ok {
			t.Fatalf("expected pull_requests in %q section: %s", section, output)
		}
	}

	humanOutput, err := executeLiveCLI(t, "pr", "status")
	if err != nil {
		t.Fatalf("pr status (human) failed: %v\noutput: %s", err, humanOutput)
	}
	for _, heading := range []string{"Current branch", "Created by you", "Requesting a code review from you"} {
		if !strings.Contains(humanOutput, heading) {
			t.Fatalf("expected %q heading in pr status output, got: %s", heading, humanOutput)
		}
	}
}
