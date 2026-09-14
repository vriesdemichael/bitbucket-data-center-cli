package gateparity

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	releaseArtifactsWorkflowPath = ".github/workflows/release-artifacts.yml"
	releaseWorkflowPath          = ".github/workflows/release.yml"
)

// TestEveryReleasedPlatformHasItsSBOMsAttested keeps the platforms release.yml
// attests equal to the platforms release-artifacts.yml builds.
//
// The build uploads each platform's archives with their SBOMs, and the
// attestation job downloads them one platform at a time. A platform added to
// the build and not to the attestation would be published with SBOMs nothing
// attested, and no step would fail for it.
func TestEveryReleasedPlatformHasItsSBOMsAttested(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	built := matrixPlatforms(t, filepath.Join(root, releaseArtifactsWorkflowPath), "build")
	attested := matrixPlatforms(t, filepath.Join(root, releaseWorkflowPath), "attest-sboms")

	// A parser that stopped matching would find two empty matrices equal.
	if len(built) < 6 {
		t.Fatalf("expected %s to build at least six platforms, found %v; the matrix parser has probably stopped matching", releaseArtifactsWorkflowPath, built)
	}
	if !slices.Equal(built, attested) {
		t.Errorf("%s builds %v, but %s attests %v", releaseArtifactsWorkflowPath, built, releaseWorkflowPath, attested)
	}
}

// matrixPlatforms returns the goos/goarch and archive format of each entry in
// a job's matrix, sorted.
func matrixPlatforms(t *testing.T, path, job string) []string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []map[string]string `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	definition, ok := workflow.Jobs[job]
	if !ok {
		t.Fatalf("%s has no %s job", path, job)
	}

	platforms := []string{}
	for _, entry := range definition.Strategy.Matrix.Include {
		platforms = append(platforms, entry["goos"]+"/"+entry["goarch"]+" "+entry["archive"])
	}
	slices.Sort(platforms)

	return platforms
}
