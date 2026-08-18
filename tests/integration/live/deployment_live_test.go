//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLiveDeploymentLifecycle covers bb deployment create, get and delete.
//
// None of the three had ever run against a real Bitbucket. They are the shape
// #378 turned out to be: a real endpoint with a real payload, exercised only by
// stubs that agreed with bb about what the API looks like.
func TestLiveDeploymentLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	deploymentKey := fmt.Sprintf("live-deploy-%d", time.Now().UnixNano()%100000)
	const envKey = "live-env"

	createOutput, err := executeLiveCLI(t, "--json", "deployment", "create", commitID,
		"--key", deploymentKey,
		"--env-key", envKey,
		"--env-name", "Live Environment",
		"--env-type", "STAGING",
		"--state", "SUCCESSFUL",
		"--display-name", "Live deployment",
		"--url", "http://localhost:65535/deployment",
		"--env-url", "http://localhost:65535/env",
		"--description", "created by the live suite",
		"--deployment-sequence-number", "1",
	)
	if err != nil {
		t.Fatalf("deployment create failed: %v\noutput: %s", err, createOutput)
	}

	// Read it back from the server rather than trusting the create response:
	// that is the step which would have caught a wrong endpoint or payload.
	getOutput, err := executeLiveCLI(t, "--json", "deployment", "get", commitID,
		"--key", deploymentKey,
		"--env-key", envKey,
		"--deployment-sequence-number", "1",
	)
	if err != nil {
		t.Fatalf("deployment get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, deploymentKey) {
		t.Fatalf("expected the created deployment to be readable, got: %s", getOutput)
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "deployment", "delete", commitID,
		"--key", deploymentKey,
		"--env-key", envKey,
		"--deployment-sequence-number", "1",
	)
	if err != nil {
		t.Fatalf("deployment delete failed: %v\noutput: %s", err, deleteOutput)
	}
}
