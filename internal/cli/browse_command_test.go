package cli

import (
	"errors"
	"net/url"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
)

func TestBrowseCommandHomeAndNoBrowser(t *testing.T) {
	originalOpener := browseURLOpener
	browseURLOpener = func(string) error { return errors.New("should not open") }
	t.Cleanup(func() { browseURLOpener = originalOpener })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	output, err := executeTestCLI(t, "browse", "--no-browser")
	if err != nil {
		t.Fatalf("browse failed: %v", err)
	}

	if !strings.Contains(output, "https://bitbucket.example.com/projects/PRJ/repos/repo") {
		t.Fatalf("unexpected browse output: %s", output)
	}
}

func TestBrowserCommand(t *testing.T) {
	cases := []struct {
		goos     string
		wantName string
		wantArgs []string
	}{
		{goos: "windows", wantName: "rundll32", wantArgs: []string{"url.dll,FileProtocolHandler", "https://example.com"}},
		{goos: "darwin", wantName: "open", wantArgs: []string{"https://example.com"}},
		{goos: "linux", wantName: "xdg-open", wantArgs: []string{"https://example.com"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.goos, func(t *testing.T) {
			name, args := browserCommand(testCase.goos, "https://example.com")
			if name != testCase.wantName {
				t.Fatalf("command name: got %q, want %q", name, testCase.wantName)
			}
			if !reflect.DeepEqual(args, testCase.wantArgs) {
				t.Fatalf("command args: got %#v, want %#v", args, testCase.wantArgs)
			}
		})
	}
}

func TestOpenInBrowser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stubs the launcher with sh, which is unavailable on Windows; TestBrowserCommand covers the Windows command mapping")
	}

	originalExec := browseExecCommand
	t.Cleanup(func() { browseExecCommand = originalExec })

	browseExecCommand = func(name string, args ...string) *exec.Cmd {
		a := append([]string{"-c", "exit 0"}, args...)
		_ = name
		return exec.Command("sh", a...)
	}

	if err := openInBrowser("https://example.com"); err != nil {
		t.Fatalf("expected openInBrowser success, got: %v", err)
	}

	browseExecCommand = func(name string, args ...string) *exec.Cmd {
		a := append([]string{"-c", "exit 1"}, args...)
		_ = name
		return exec.Command("sh", a...)
	}

	err := openInBrowser("https://example.com")
	if err == nil {
		t.Fatal("expected openInBrowser failure")
	}
}

func TestBrowseCommandTargets(t *testing.T) {
	originalOpener := browseURLOpener
	browseURLOpener = func(string) error { return nil }
	t.Cleanup(func() { browseURLOpener = originalOpener })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com/ctx")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	tests := []struct {
		name    string
		args    []string
		expects string
	}{
		{name: "settings", args: []string{"browse", "--no-browser", "--settings"}, expects: "/projects/PRJ/repos/repo/settings"},
		{name: "releases", args: []string{"browse", "--no-browser", "--releases"}, expects: "/projects/PRJ/repos/repo/tags"},
		{name: "number", args: []string{"browse", "--no-browser", "217"}, expects: "/projects/PRJ/repos/repo/pull-requests/217"},
		{name: "commit arg", args: []string{"browse", "--no-browser", "77507cd"}, expects: "/projects/PRJ/repos/repo/commits/77507cd"},
		{name: "path line blame", args: []string{"browse", "--no-browser", "main.go:312", "--blame"}, expects: "/projects/PRJ/repos/repo/browse/main.go?blame=true&line=312"},
		{name: "path branch", args: []string{"browse", "--no-browser", "src/main.go", "--branch", "bug-fix"}, expects: "/projects/PRJ/repos/repo/browse/src/main.go?at=bug-fix"},
		{name: "path commit", args: []string{"browse", "--no-browser", "src/main.go", "--commit", "77507cd"}, expects: "/projects/PRJ/repos/repo/browse/src/main.go?at=77507cd"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := executeTestCLI(t, testCase.args...)
			if err != nil {
				t.Fatalf("browse failed: %v", err)
			}
			if !strings.Contains(output, testCase.expects) {
				t.Fatalf("expected output containing %q, got: %s", testCase.expects, output)
			}
		})
	}
}

func TestBrowseCommandHostQualifiedRepoAndJSON(t *testing.T) {
	originalOpener := browseURLOpener
	browseURLOpener = func(string) error { return nil }
	t.Cleanup(func() { browseURLOpener = originalOpener })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")

	output, err := executeTestCLI(t, "--json", "browse", "--repo", "bb.company.local/OPS/tooling", "src/main.go", "--branch", "main")
	if err != nil {
		t.Fatalf("browse json failed: %v", err)
	}

	if !strings.Contains(output, `"url": "https://bb.company.local/projects/OPS/repos/tooling/browse/src/main.go?at=main"`) {
		t.Fatalf("unexpected json output: %s", output)
	}
}

func TestBrowseCommandValidationAndOpenerFailure(t *testing.T) {
	originalOpener := browseURLOpener
	browseURLOpener = func(string) error { return errors.New("cannot open") }
	t.Cleanup(func() { browseURLOpener = originalOpener })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	_, err := executeTestCLI(t, "browse", "--settings", "217")
	if err == nil {
		t.Fatal("expected incompatible arg+flag validation error")
	}

	_, err = executeTestCLI(t, "browse", "--repo", "bad")
	if err == nil {
		t.Fatal("expected invalid repository selector error")
	}

	_, err = executeTestCLI(t, "browse", "--wiki")
	if err == nil {
		t.Fatal("expected unknown flag error for --wiki")
	}

	_, err = executeTestCLI(t, "browse", "--branch", "main", "--commit", "abc")
	if err == nil {
		t.Fatal("expected branch+commit validation error")
	}

	_, err = executeTestCLI(t, "browse")
	if err == nil {
		t.Fatal("expected opener failure")
	}
	if !strings.Contains(err.Error(), "cannot open") {
		t.Fatalf("expected opener error, got: %v", err)
	}
}

func TestBrowseCommandBuildURLAndOpenSuccessPaths(t *testing.T) {
	originalOpener := browseURLOpener
	browseURLOpener = func(string) error { return nil }
	t.Cleanup(func() { browseURLOpener = originalOpener })

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
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

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

	t.Setenv("BITBUCKET_REPO_SLUG", "")
	_, _, err = resolveBrowseRepositoryReference("", cfg)
	if err == nil {
		t.Fatal("expected fallback resolve error when repo env is absent")
	}

	_, _, err = resolveBrowseRepositoryReference("bad", cfg)
	if err == nil {
		t.Fatal("expected parse error for invalid selector")
	}
}

func TestBuildBitbucketBrowseURLVariants(t *testing.T) {
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
