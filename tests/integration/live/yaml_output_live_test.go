//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestLiveYAMLIsTheJSONDocument reads the same Bitbucket state through --json
// and through --yaml and holds the two answers to being one document (ADR-095).
//
// The unit walk reaches what a command does with no server, which for almost
// every command is its failure envelope. This is the other half: real payloads,
// with the values Bitbucket fills in -- dates in milliseconds, versions, nested
// reviewers, a list cut at --limit -- where an encoder that quoted too little
// or kept too little would change what a YAML reader gets back.
func TestLiveYAMLIsTheJSONDocument(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/yaml-output"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "yaml.txt"); err != nil {
		t.Fatalf("push %s failed: %v", branch, err)
	}
	pullRequest, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create the pull request failed: %v", err)
	}

	for _, args := range [][]string{
		{"repo", "get"},
		{"branch", "list"},
		{"pr", "get", pullRequest},
		// One pull request at --limit 1 comes back at the limit, so meta
		// carries limitReached as well as bbVersion.
		{"pr", "list", "--limit", "1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			asJSON, stderr, err := executeLiveCLISplit(t, "", append([]string{"--json"}, args...)...)
			if err != nil {
				t.Fatalf("bb --json %s failed: %v\nstderr: %s", strings.Join(args, " "), err, stderr)
			}
			asYAML, stderr, err := executeLiveCLISplit(t, "", append([]string{"--yaml"}, args...)...)
			if err != nil {
				t.Fatalf("bb --yaml %s failed: %v\nstderr: %s", strings.Join(args, " "), err, stderr)
			}

			var fromJSON any
			if err := json.Unmarshal([]byte(asJSON), &fromJSON); err != nil {
				t.Fatalf("--json wrote no JSON document: %v\n%s", err, asJSON)
			}
			if json.Valid([]byte(asYAML)) {
				t.Fatalf("--yaml wrote JSON:\n%s", asYAML)
			}
			var fromYAML any
			if err := yaml.Unmarshal([]byte(asYAML), &fromYAML); err != nil {
				t.Fatalf("--yaml wrote no YAML document: %v\n%s", err, asYAML)
			}

			// Through encoding/json, so a YAML int and a JSON float64 holding
			// the same number compare equal.
			encoded, err := json.Marshal(fromYAML)
			if err != nil {
				t.Fatalf("the YAML document holds a value JSON cannot: %v", err)
			}
			var yamlAsJSON any
			if err := json.Unmarshal(encoded, &yamlAsJSON); err != nil {
				t.Fatalf("re-decode: %v", err)
			}

			if !reflect.DeepEqual(fromJSON, yamlAsJSON) {
				t.Fatalf("--yaml is not the --json document\n--json:\n%s\n--yaml:\n%s", asJSON, asYAML)
			}
		})
	}
}
