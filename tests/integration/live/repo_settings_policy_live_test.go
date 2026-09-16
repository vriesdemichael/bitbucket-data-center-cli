//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveRepoSettingsAutoDeclineLifecycle covers set, get and delete for the
// repository auto-decline policy, none of which had run against a real server.
//
// The read-back after each write is the point: a setting that reports success
// and does not persist is indistinguishable from one that works, and only the
// server can tell the two apart.
func TestLiveRepoSettingsAutoDeclineLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Eight weeks, not four. A repository without a policy of its own already
	// reads as enabled after four, the instance default, so a set that stored
	// nothing read back exactly what four asked for.
	setOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-decline", "set",
		"--enabled", "--inactivity-weeks", "8", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-decline set failed: %v\noutput: %s", err, setOutput)
	}

	getOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-decline", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-decline get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "true") {
		t.Fatalf("expected auto-decline to read back as enabled, got: %s", getOutput)
	}
	if enabled, weeks := repoAutoDeclineReadBack(t, getOutput); !enabled || weeks != 8 {
		t.Fatalf("auto-decline reads back enabled=%t after %v weeks, want enabled after 8: %s", enabled, weeks, getOutput)
	}

	// Enabled is the default, so only switching it off shows the flag is stored.
	// No weeks are named: Bitbucket refuses a write without them, and the eight
	// set above have to go with it and stay (OPENAPI-034).
	mustLiveCLI(t, "repo", "settings", "auto-decline", "set", "--enabled=false", "--repo", repoRef)

	disabled := mustLiveCLI(t, "repo", "settings", "auto-decline", "get", "--repo", repoRef)
	if enabled, weeks := repoAutoDeclineReadBack(t, disabled); enabled || weeks != 8 {
		t.Fatalf("auto-decline reads back enabled=%t after %v weeks, want disabled with the 8 weeks kept: %s", enabled, weeks, disabled)
	}

	// bb's get reports the policy in force and not whose it is, so only the
	// scope can say the delete removed the repository's own policy.
	autoDecline := "/rest/api/latest/projects/" + seeded.Key + "/repos/" + repo.Slug + "/settings/auto-decline"
	if scope := repoPolicyScope(t, autoDecline); scope != "REPOSITORY" {
		t.Fatalf("auto-decline scope before the delete = %s, want REPOSITORY", scope)
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-decline", "delete", "--repo", repoRef, "--yes")
	if err != nil {
		t.Fatalf("auto-decline delete failed: %v\noutput: %s", err, deleteOutput)
	}

	if scope := repoPolicyScope(t, autoDecline); scope == "REPOSITORY" {
		t.Fatalf("the repository still has an auto-decline policy of its own after the delete")
	}

	// Human output too: it is a separate rendering path, and the JSON passing
	// says nothing about it.
	humanOutput, err := executeLiveCLI(t, "repo", "settings", "auto-decline", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-decline get (human) failed: %v\noutput: %s", err, humanOutput)
	}
	if strings.TrimSpace(humanOutput) == "" {
		t.Fatalf("expected human auto-decline output, got nothing")
	}
}

// TestLiveRepoSettingsAutoMergeLifecycle covers the get and delete halves of
// the repository auto-merge setting. set is exercised by the auto-merge pull
// request test, which needs it enabled to arm anything.
func TestLiveRepoSettingsAutoMergeLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	if output, err := executeLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", repoRef); err != nil {
		t.Fatalf("auto-merge set failed: %v\noutput: %s", err, output)
	}

	getOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-merge", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-merge get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "true") {
		t.Fatalf("expected auto-merge to read back as enabled, got: %s", getOutput)
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-merge", "delete", "--repo", repoRef, "--yes")
	if err != nil {
		t.Fatalf("auto-merge delete failed: %v\noutput: %s", err, deleteOutput)
	}
}

// repoAutoDeclineReadBack reads whether auto-decline is on, and after how many
// weeks, from a get's JSON output.
func repoAutoDeclineReadBack(t *testing.T, output string) (bool, float64) {
	t.Helper()

	settings := decodeJSONMap(t, output)
	enabled, ok := settings["enabled"].(bool)
	if !ok {
		t.Fatalf("no enabled flag in the auto-decline settings: %s", output)
	}
	weeks, _ := settings["inactivityWeeks"].(float64)

	return enabled, weeks
}

// repoPolicyScope reads the level a repository's auto-merge or auto-decline
// policy comes from: REPOSITORY, PROJECT or GLOBAL.
func repoPolicyScope(t *testing.T, path string) string {
	t.Helper()

	output := mustLiveCLI(t, "api", path)
	scope, ok := decodeJSONMap(t, output)["scope"].(map[string]any)
	if !ok {
		t.Fatalf("no scope in %s: %s", path, output)
	}

	return asString(scope["type"])
}
