package browsecmd

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func executeTestCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	root := &cobra.Command{Use: "bb"}
	jsonFlag := root.PersistentFlags().Bool("json", false, "")
	deps := Dependencies{
		JSONEnabled: func() bool { return *jsonFlag },
		URLOpener:   func(string) error { return nil },
	}
	root.AddCommand(New(deps))
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestBrowseCommand(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://bad-url")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	_, err := executeTestCLI(t, "browse", "--no-browser")
	if err == nil {
		t.Fatal("expected browse URL build validation error")
	}

	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	output, err := executeTestCLI(t, "browse")
	if err != nil {
		t.Fatalf("expected browse open success, got: %v", err)
	}
	if !strings.Contains(output, "https://bitbucket.example.com/projects/PRJ/repos/repo") {
		t.Fatalf("expected browse output URL, got: %s", output)
	}
}

func TestBuildBitbucketBrowseURL(t *testing.T) {
	t.Parallel()

	target := browseTarget{kind: browseTargetPath, path: "src/main.go", branch: "main"}
	browseURL, err := buildBitbucketBrowseURL("https://bitbucket.example.com/context", "PRJ", "repo", target)
	if err != nil {
		t.Fatalf("expected valid browse URL, got: %v", err)
	}
	if browseURL != "https://bitbucket.example.com/context/projects/PRJ/repos/repo/browse/src/main.go?at=main" {
		t.Fatalf("unexpected browse URL: %s", browseURL)
	}

	_, err = buildBitbucketBrowseURL("bad-url", "PRJ", "repo", target)
	if err == nil {
		t.Fatal("expected invalid base URL error")
	}
}

func TestResolveBrowseTargetValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		opts browseResolveOptions
	}{
		{name: "branch and commit conflict", opts: browseResolveOptions{branch: "main", commit: "abc"}},
		{name: "blame needs path", opts: browseResolveOptions{blame: true}},
		{name: "settings cannot use arg", args: []string{"217"}, opts: browseResolveOptions{settings: true}},
		{name: "settings cannot use blame", opts: browseResolveOptions{settings: true, blame: true}},
		{name: "settings cannot use branch", opts: browseResolveOptions{settings: true, branch: "main"}},
		{name: "settings cannot use commit", opts: browseResolveOptions{settings: true, commit: "abc"}},
		{name: "number cannot use branch", args: []string{"217"}, opts: browseResolveOptions{branch: "main"}},
		{name: "number cannot use commit", args: []string{"217"}, opts: browseResolveOptions{commit: "abc"}},
		{name: "commit cannot use blame", args: []string{"77507cd"}, opts: browseResolveOptions{blame: true}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := resolveBrowseTarget(testCase.args, testCase.opts)
			if err == nil {
				t.Fatalf("expected validation error for case %q", testCase.name)
			}
		})
	}
}

func TestBrowseCommandValidationBranches(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	_, err := executeTestCLI(t, "browse", "--settings", "--releases")
	if err == nil {
		t.Fatal("expected mutually exclusive settings/releases validation error")
	}

	t.Setenv("BB_REQUEST_TIMEOUT", "not-a-duration")
	_, err = executeTestCLI(t, "browse")
	if err == nil {
		t.Fatal("expected load config validation error")
	}
}

func TestResolveBrowseTargetModes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		args       []string
		opts       browseResolveOptions
		expectKind browseTargetKind
	}{
		{name: "home default", expectKind: browseTargetHome},
		{name: "commit flag", opts: browseResolveOptions{commit: "abc1234"}, expectKind: browseTargetCommit},
		{name: "path target", args: []string{"src/main.go"}, expectKind: browseTargetPath},
		{name: "commit sha arg", args: []string{"abcdef1"}, expectKind: browseTargetCommit},
		{name: "number target", args: []string{"123"}, expectKind: browseTargetPR},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			target, err := resolveBrowseTarget(testCase.args, testCase.opts)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if target.kind != testCase.expectKind {
				t.Fatalf("unexpected target kind: %q", target.kind)
			}
		})
	}
}

func TestSplitPathAndLineAndEncoding(t *testing.T) {
	t.Parallel()

	path, line := splitPathAndLine("")
	if path != "" || line != 0 {
		t.Fatalf("unexpected split result for empty input: %q %d", path, line)
	}

	path, line = splitPathAndLine("main.go:312")
	if path != "main.go" || line != 312 {
		t.Fatalf("unexpected split result: %q %d", path, line)
	}

	path, line = splitPathAndLine("dir:name/file.txt")
	if path != "dir:name/file.txt" || line != 0 {
		t.Fatalf("unexpected split result with non-line suffix: %q %d", path, line)
	}

	encoded := encodePathSegments("src/a b/main.go")
	if encoded != "src/a%20b/main.go" {
		t.Fatalf("unexpected encoded path: %q", encoded)
	}

	encoded = encodePathSegments("")
	if encoded != "" {
		t.Fatalf("expected empty encoded path, got: %q", encoded)
	}

	encoded = encodePathSegments("src//main.go")
	if encoded != "src/main.go" {
		t.Fatalf("unexpected collapsed encoded path: %q", encoded)
	}
}

func TestResolveBrowseRepositoryReference(t *testing.T) {
	cfg := config.AppConfig{
		BitbucketURL: "https://bitbucket.example.com",
		ProjectKey:   "PRJ",
		RepoSlug:     "repo",
	}

	repo, host, err := resolveBrowseRepositoryReference("bb.company.local/OPS/tooling", cfg)
	if err != nil {
		t.Fatalf("expected host-qualified repo selector to parse: %v", err)
	}
	if repo.ProjectKey != "OPS" || repo.Slug != "tooling" {
		t.Fatalf("unexpected repo selector: %+v", repo)
	}
	if host != "https://bb.company.local" {
		t.Fatalf("unexpected host: %s", host)
	}

	repo, host, err = resolveBrowseRepositoryReference("", cfg)
	if err != nil {
		t.Fatalf("expected fallback repo resolution: %v", err)
	}
	if repo.ProjectKey != "PRJ" || repo.Slug != "repo" {
		t.Fatalf("unexpected fallback repo: %+v", repo)
	}
	if host != "https://bitbucket.example.com" {
		t.Fatalf("unexpected fallback host: %s", host)
	}

	repo, host, err = resolveBrowseRepositoryReference("PRJ/other", cfg)
	if err != nil {
		t.Fatalf("expected explicit selector to parse: %v", err)
	}
	if repo.ProjectKey != "PRJ" || repo.Slug != "other" || host != "https://bitbucket.example.com" {
		t.Fatalf("unexpected explicit selector result: %+v host=%s", repo, host)
	}

	unslugged := cfg
	unslugged.RepoSlug = ""

	_, _, err = resolveBrowseRepositoryReference("", unslugged)
	if err == nil {
		t.Fatal("expected fallback resolve error when no repository is configured")
	}

	_, _, err = resolveBrowseRepositoryReference("bad", cfg)
	if err == nil {
		t.Fatal("expected parse error for invalid selector")
	}
}

func TestBuildBitbucketBrowseURLVariants(t *testing.T) {
	t.Parallel()

	base := "https://bitbucket.example.com"

	variants := []browseTarget{
		{kind: browseTargetHome},
		{kind: browseTargetHome, branch: "master"},
		{kind: "settings"},
		{kind: "releases"},
		{kind: browseTargetPR, line: 7},
		{kind: browseTargetCommit, commit: "abc1234"},
		{kind: browseTargetPath, path: "src/main.go", commit: "abc1234"},
	}

	for _, target := range variants {
		result, err := buildBitbucketBrowseURL(base, "PRJ", "repo", target)
		if err != nil {
			t.Fatalf("unexpected error building variant %+v: %v", target, err)
		}
		parsed, err := url.Parse(result)
		if err != nil {
			t.Fatalf("expected valid URL: %v", err)
		}
		if parsed.Scheme != "https" || parsed.Host != "bitbucket.example.com" {
			t.Fatalf("unexpected parsed URL: %s", result)
		}
	}

	_, err := buildBitbucketBrowseURL(base, "", "repo", browseTarget{kind: browseTargetHome})
	if err == nil {
		t.Fatal("expected empty project validation error")
	}
}

func TestParseHostQualifiedRepositorySelector(t *testing.T) {
	t.Parallel()

	host, repo, ok := parseHostQualifiedRepositorySelector("bb.company.local/OPS/tooling")
	if !ok {
		t.Fatal("expected host-qualified selector to parse")
	}
	if host != "bb.company.local" || repo.ProjectKey != "OPS" || repo.Slug != "tooling" {
		t.Fatalf("unexpected parse result host=%s repo=%+v", host, repo)
	}

	if _, _, ok := parseHostQualifiedRepositorySelector("OPS/tooling"); ok {
		t.Fatal("expected selector without host to fail host-qualified parse")
	}
	if _, _, ok := parseHostQualifiedRepositorySelector("bb//tooling"); ok {
		t.Fatal("expected malformed host-qualified selector to fail")
	}
}

func TestBrowserCommand(t *testing.T) {
	t.Parallel()

	cmd, args := browserCommand("darwin", "https://example.com")
	if cmd != "open" || len(args) != 1 || args[0] != "https://example.com" {
		t.Fatalf("unexpected darwin command: %s %v", cmd, args)
	}

	cmd, args = browserCommand("windows", "https://example.com")
	if cmd != "rundll32" || len(args) != 2 || args[1] != "https://example.com" {
		t.Fatalf("unexpected windows command: %s %v", cmd, args)
	}

	cmd, args = browserCommand("linux", "https://example.com")
	if cmd != "xdg-open" || len(args) != 1 || args[0] != "https://example.com" {
		t.Fatalf("unexpected linux command: %s %v", cmd, args)
	}
}

func TestBrowseDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.WriteJSON == nil || d.URLOpener == nil {
		t.Fatal("expected WriteJSON and URLOpener to default to non-nil")
	}
	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig: %v", err)
		}
	}
}
