// Command openapi-spec keeps the vendored Atlassian OpenAPI reference in step
// with the Bitbucket release the live harness runs.
//
// ADR-042 records the version under test in exactly one place, the base image
// tag in docker/harness/Dockerfile, so that nothing else can drift away from it.
// The vendored spec used to carry its own pinned version in the Taskfile and in
// its filename, which is a second copy of the same fact — and it drifted: the
// harness moved to 10.4.x while the spec stayed at 10.2.
//
// This tool derives the API version from that one tag instead. Refresh mode
// downloads the matching spec; verify mode fails when the vendored spec and the
// harness have parted ways.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const (
	// specURLTemplate is Atlassian's published OpenAPI artifact, addressed by
	// the major.minor of a Bitbucket Data Center release.
	specURLTemplate = "https://dac-static.atlassian.com/server/bitbucket/%s.swagger.v3.json"

	// expectedOpenAPIVersion is the OpenAPI dialect the sanitizer and the code
	// generator are written against. A change here is not a routine bump.
	expectedOpenAPIVersion = "3.0.1"
)

// harnessImageRE matches the base image tag ADR-042 designates as the single
// record of the version under test.
var harnessImageRE = regexp.MustCompile(`(?m)^FROM\s+atlassian/bitbucket:(\d+)\.(\d+)(?:\.\d+)*`)

func main() {
	dockerfile := flag.String("dockerfile", filepath.Join("docker", "harness", "Dockerfile"), "Harness Dockerfile that pins the Bitbucket release under test")
	specPath := flag.String("spec", filepath.Join("docs", "reference", "atlassian", "bitbucket-openapi.json"), "Vendored OpenAPI reference")
	refresh := flag.Bool("refresh", false, "Download the spec matching the harness release instead of verifying the vendored one")
	printVersion := flag.Bool("print-version", false, "Print the API version derived from the harness Dockerfile and exit")
	flag.Parse()

	apiVersion, err := harnessAPIVersion(*dockerfile)
	if err != nil {
		fail(err)
	}

	switch {
	case *printVersion:
		fmt.Println(apiVersion)
	case *refresh:
		if err := refreshSpec(apiVersion, *specPath); err != nil {
			fail(err)
		}
	default:
		if err := verifySpec(apiVersion, *specPath); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}

// harnessAPIVersion reads the major.minor Atlassian publishes a spec for from
// the harness base image tag. The image is pinned to a patch release (10.4.2)
// while specs are published per minor (10.4), so the patch is discarded.
func harnessAPIVersion(dockerfile string) (string, error) {
	content, err := os.ReadFile(dockerfile)
	if err != nil {
		return "", fmt.Errorf("read harness Dockerfile: %w", err)
	}

	match := harnessImageRE.FindSubmatch(content)
	if match == nil {
		return "", fmt.Errorf("no %q base image tag found in %s; ADR-042 requires the version under test to live there", "atlassian/bitbucket", dockerfile)
	}

	return fmt.Sprintf("%s.%s", match[1], match[2]), nil
}

func refreshSpec(apiVersion, specPath string) error {
	url := fmt.Sprintf(specURLTemplate, apiVersion)

	body, err := download(url)
	if err != nil {
		return err
	}

	if err := validateSpec(body, apiVersion); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}

	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		return fmt.Errorf("create spec directory: %w", err)
	}
	if err := os.WriteFile(specPath, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", specPath, err)
	}

	paths, _ := specPathCount(body)
	fmt.Printf("vendored Bitbucket %s OpenAPI spec from %s (paths=%d)\n", apiVersion, url, paths)
	return nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}

	response, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		// Atlassian publishes a spec per minor, and not every release has one
		// yet. Say so plainly rather than writing an error page to disk.
		return nil, fmt.Errorf("download %s: HTTP %d (Atlassian may not have published a spec for this release yet)", url, response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	return body, nil
}

func verifySpec(apiVersion, specPath string) error {
	body, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", specPath, err)
	}

	if err := validateSpec(body, apiVersion); err != nil {
		return fmt.Errorf("%s is out of step with the harness: %w\n\nRefresh it with: task openapi:refresh", specPath, err)
	}

	fmt.Printf("vendored OpenAPI spec matches the harness release (Bitbucket %s)\n", apiVersion)
	return nil
}

func validateSpec(body []byte, apiVersion string) error {
	var spec struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal(body, &spec); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}
	if spec.OpenAPI != expectedOpenAPIVersion {
		return fmt.Errorf("openapi dialect is %q, want %q", spec.OpenAPI, expectedOpenAPIVersion)
	}
	if spec.Info.Version != apiVersion {
		return fmt.Errorf("spec is for Bitbucket %q but the harness runs %q", spec.Info.Version, apiVersion)
	}
	if len(spec.Paths) == 0 {
		return fmt.Errorf("spec declares no paths")
	}

	return nil
}

func specPathCount(body []byte) (int, error) {
	var spec struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(body, &spec); err != nil {
		return 0, err
	}
	return len(spec.Paths), nil
}
