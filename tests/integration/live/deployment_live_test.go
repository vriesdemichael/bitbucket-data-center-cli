//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveDeploymentLifecycle covers bb deployment create, get and delete.
//
// None of the three had ever run against a real Bitbucket. They are the shape
// #378 turned out to be: a real endpoint with a real payload, exercised only by
// stubs that agreed with bb about what the API looks like.
func TestLiveDeploymentLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	deploymentKey := testsupport.UniqueName("live-deploy-")
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

	// Three neighbours, each sharing two of the three values that address a
	// deployment and differing in the third. get and delete need all three, and
	// Bitbucket answers a delete that matches nothing with 204, so the get
	// finding this deployment rather than a neighbour, and the neighbours
	// outliving the delete, is what shows each value reached it.
	neighbours := []struct{ name, key, envKey, sequence string }{
		{"another key", testsupport.UniqueName("live-deploy-"), envKey, "1"},
		{"another environment", deploymentKey, "live-env-neighbour", "1"},
		{"another sequence number", deploymentKey, envKey, "2"},
	}
	for _, neighbour := range neighbours {
		mustLiveCLI(t, "deployment", "create", commitID,
			"--key", neighbour.key,
			"--env-key", neighbour.envKey,
			"--env-name", "Neighbour Environment",
			"--state", "FAILED",
			"--display-name", neighbour.name,
			"--url", "http://localhost:65535/neighbour",
			"--description", "a neighbour with "+neighbour.name,
			"--deployment-sequence-number", neighbour.sequence,
		)
	}
	assertNeighbours := func(when string) {
		t.Helper()

		for _, neighbour := range neighbours {
			output, err := executeLiveCLI(t, "--json", "deployment", "get", commitID,
				"--key", neighbour.key,
				"--env-key", neighbour.envKey,
				"--deployment-sequence-number", neighbour.sequence,
			)
			if err != nil {
				t.Errorf("%s, the neighbour with %s could not be read: %v\n%s", when, neighbour.name, err, output)
				continue
			}
			assertLiveDeploymentFields(t, "the neighbour with "+neighbour.name, decodeJSONMap(t, output), map[string]any{
				"key":                      neighbour.key,
				"environment.key":          neighbour.envKey,
				"environment.displayName":  "Neighbour Environment",
				"deploymentSequenceNumber": neighbour.sequence,
				"state":                    "FAILED",
				"displayName":              neighbour.name,
				"url":                      "http://localhost:65535/neighbour",
				"description":              "a neighbour with " + neighbour.name,
				"toCommit":                 commitID,
			})
		}
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

	// Every value the create sent. The environment's type and URL are optional
	// and absent unless sent. The commit is toCommit: fromCommit is where the
	// previous deployment of the key left the environment, and there is none.
	assertLiveDeploymentFields(t, "the deployment", decodeJSONMap(t, getOutput), map[string]any{
		"key":                      deploymentKey,
		"environment.key":          envKey,
		"environment.displayName":  "Live Environment",
		"environment.type":         "STAGING",
		"environment.url":          "http://localhost:65535/env",
		"deploymentSequenceNumber": "1",
		"state":                    "SUCCESSFUL",
		"displayName":              "Live deployment",
		"url":                      "http://localhost:65535/deployment",
		"description":              "created by the live suite",
		"toCommit":                 commitID,
		"repository.projectKey":    seeded.Key,
		"repository.slug":          repo.Slug,
	})
	assertNeighbours("before the delete")

	deleteOutput, err := executeLiveCLI(t, "--json", "deployment", "delete", commitID,
		"--key", deploymentKey,
		"--env-key", envKey,
		"--deployment-sequence-number", "1",
		"--yes",
	)
	if err != nil {
		t.Fatalf("deployment delete failed: %v\noutput: %s", err, deleteOutput)
	}

	afterDelete, err := executeLiveCLI(t, "--json", "deployment", "get", commitID,
		"--key", deploymentKey,
		"--env-key", envKey,
		"--deployment-sequence-number", "1",
	)
	if err == nil || apperrors.ExitCode(err) != 4 {
		t.Errorf("reading the deleted deployment did not answer not found (exit 4): %v\n%s", err, afterDelete)
	}
	assertNeighbours("after the delete")
}

// assertLiveDeploymentFields compares a deployment as `bb deployment get`
// reports it with the values it was created with. A dotted name reaches into a
// nested object, and a number is compared in its decimal form, the way it was
// given on the command line.
func assertLiveDeploymentFields(t *testing.T, subject string, deployment map[string]any, want map[string]any) {
	t.Helper()

	for path, value := range want {
		var got any = deployment
		for _, step := range strings.Split(path, ".") {
			object, _ := got.(map[string]any)
			got = object[step]
		}
		if number, ok := got.(float64); ok {
			got, _ = numericOrStringID(number)
		}
		if got != value {
			t.Errorf("%s: %s came back as %v, want %v", subject, path, got, value)
		}
	}
}
