//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveRepoGet covers `bb repo get` on a repository without a README and
// then on the same repository once it has one.
//
// The first half is the case a mock would have got wrong by construction:
// Bitbucket answers a repository with no README with a 404, and the command has
// to leave the section out rather than fail. Only a real repository without a
// README says what that 404 looks like.
func TestLiveRepoGet(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The seeded repository carries seed.txt and nothing else.
	bare, err := executeLiveCLI(t, "--json", "repo", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo get on a repository without a README failed: %v\noutput: %s", err, bare)
	}
	payload := decodeJSONMap(t, bare)
	detail, _ := payload["repository"].(map[string]any)
	if detail["projectKey"] != seeded.Key || detail["slug"] != repo.Slug {
		t.Errorf("repo get described %v/%v, want %s", detail["projectKey"], detail["slug"], repoRef)
	}
	if detail["state"] != "AVAILABLE" {
		t.Errorf("state = %v, want AVAILABLE", detail["state"])
	}
	if _, present := payload["readme"]; present {
		t.Errorf("a repository without a README reported one:\n%s", bare)
	}
	clones, _ := payload["cloneUrls"].([]any)
	if len(clones) == 0 {
		t.Errorf("no clone URLs were reported:\n%s", bare)
	}
	for _, clone := range clones {
		entry, _ := clone.(map[string]any)
		if url, _ := entry["url"].(string); !strings.Contains(strings.ToLower(url), strings.ToLower(repo.Slug)) {
			t.Errorf("clone URL %q does not name the repository", url)
		}
	}

	bareHuman, err := executeLiveCLI(t, "repo", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo get on a repository without a README failed: %v\noutput: %s", err, bareHuman)
	}
	if !strings.Contains(bareHuman, "No README") {
		t.Errorf("expected the human output to say there is no README, got:\n%s", bareHuman)
	}

	const readme = "# Live README\n\nWritten by the bb repo get live test.\n\n- one\n- two\n"
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", "README.md", readme); err != nil {
		t.Fatalf("pushing a README failed: %v", err)
	}

	withReadme, err := executeLiveCLI(t, "--json", "repo", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo get failed: %v\noutput: %s", err, withReadme)
	}
	got, _ := decodeJSONMap(t, withReadme)["readme"].(map[string]any)
	if got["content"] != readme || got["encoding"] != "utf-8" {
		t.Errorf("readme = %v, want the pushed README as utf-8:\n%s", got, withReadme)
	}

	human, err := executeLiveCLI(t, "repo", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo get failed: %v\noutput: %s", err, human)
	}
	if !strings.Contains(human, repoRef) || !strings.Contains(human, readme) {
		t.Errorf("expected the details and the whole README, unrendered, got:\n%s", human)
	}

	details, err := executeLiveCLI(t, "--json", "repo", "get", "--repo", repoRef, "--readme=false")
	if err != nil {
		t.Fatalf("repo get --readme=false failed: %v\noutput: %s", err, details)
	}
	if _, present := decodeJSONMap(t, details)["readme"]; present {
		t.Errorf("--readme=false still reported the README:\n%s", details)
	}

	detailsHuman, err := executeLiveCLI(t, "repo", "get", "--repo", repoRef, "--readme=false")
	if err != nil {
		t.Fatalf("repo get --readme=false failed: %v\noutput: %s", err, detailsHuman)
	}
	if strings.Contains(detailsHuman, "Live README") || strings.Contains(detailsHuman, "No README") {
		t.Errorf("--readme=false still printed a README section:\n%s", detailsHuman)
	}

	// The gh spelling is the same command, so it prints the same bytes.
	viewed, err := executeLiveCLI(t, "repo", "view", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo view failed: %v\noutput: %s", err, viewed)
	}
	if viewed != human {
		t.Errorf("bb repo view printed something other than bb repo get:\nview:\n%s\nget:\n%s", viewed, human)
	}
}
