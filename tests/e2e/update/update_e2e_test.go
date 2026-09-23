//go:build e2e

// Package update_test is bb update end to end (#637). A bb built from this tree
// updates itself from a mirror that serves a published release's real files,
// verifies them against Sigstore, and replaces the binary it runs from.
//
// It does not run with the unit tests. It downloads the pinned release from
// GitHub, fetches Sigstore's trust root through TUF, and writes the system
// policy file bb reads an offline trust root from: /etc/bb/config.yaml, or
// %ProgramData%\bb\config.yaml on Windows. CI makes that directory writable on
// a runner it throws away afterwards. The test refuses to run where a policy
// file already exists, since that one belongs to somebody.
package update_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/tuf"
)

// pinnedRelease is a published release, signed by the release workflow, that the
// test installs over a bb built from this tree. Any release with a signed
// sha256sums.txt would do; pinning one keeps the test from depending on what was
// released last.
const pinnedRelease = "v4.1.0"

const (
	repository = "vriesdemichael/bitbucket-data-center-cli"

	// builtVersion is what the bb under test calls itself: older than
	// pinnedRelease, so bb update has something to install.
	builtVersion = "v0.0.1"

	// mirrorPrefix is where the mirror serves the release, under a path as an
	// Artifactory generic repository does.
	mirrorPrefix = "/artifactory/bb-releases"
)

func TestBBUpdatesItselfFromAMirrorServingAPublishedRelease(t *testing.T) {
	policy := systemPolicyPath()
	if _, err := os.Stat(policy); err == nil {
		t.Fatalf("%s exists. This test writes the system policy file and removes it afterwards, so it runs only where "+
			"it may own that file, as on a CI runner; it will not replace one that is there.", policy)
	}

	files := downloadRelease(t)
	mirror := serveMirror(t, files)
	built := buildBB(t)

	t.Run("public Sigstore trust root", func(t *testing.T) {
		updateAndCheck(t, built, mirror, "public Sigstore TUF repository", nil)
	})

	t.Run("trusted root file, with no network", func(t *testing.T) {
		rootFile := trustedRootFile(t)
		writeSystemPolicy(t, policy, fmt.Sprintf("update_trusted_root: '%s'\n", rootFile))

		// A proxy nothing listens on. Every request but the mirror's, which is on
		// loopback and never proxied, fails through it, so an update that
		// succeeds verified against the file alone.
		deadProxy := []string{"HTTPS_PROXY=http://127.0.0.1:9", "HTTP_PROXY=http://127.0.0.1:9"}
		updateAndCheck(t, built, mirror, "trusted root file "+rootFile, deadProxy)
	})

	for _, path := range mirror.requested() {
		if !strings.HasPrefix(path, mirrorPrefix+"/") {
			t.Errorf("bb update asked the mirror for %s, outside %s", path, mirrorPrefix)
		}
	}
}

// updateAndCheck installs a copy of built, previews an update from mirror with a
// dry run, updates, and checks the binary on disk is the release, verified
// against trustSource.
func updateAndCheck(t *testing.T, built string, mirror *releaseMirror, trustSource string, extraEnv []string) {
	t.Helper()

	installed := installCopy(t, built)
	env := isolatedEnvironment(t, mirror.caFile, extraEnv)

	dry := runUpdate(t, installed, env, "update", "--dry-run", "--base-url", mirror.baseURL)
	expectVerified(t, "the dry run", dry, trustSource)
	if !dry.DryRun || !dry.UpdateAvailable || dry.Applied {
		t.Fatalf("the dry run reported %+v, want an update available and nothing applied", dry)
	}
	if version := versionOf(t, installed, env); !strings.Contains(version, strings.TrimPrefix(builtVersion, "v")) {
		t.Fatalf("after the dry run the binary reports %q, want %s untouched", version, builtVersion)
	}

	updated := runUpdate(t, installed, env, "update", "--base-url", mirror.baseURL)
	expectVerified(t, "the update", updated, trustSource)
	if !updated.Applied || updated.LatestVersion != pinnedRelease {
		t.Fatalf("the update reported %+v, want %s applied", updated, pinnedRelease)
	}

	if version := versionOf(t, installed, env); !strings.Contains(version, strings.TrimPrefix(pinnedRelease, "v")) {
		t.Fatalf("after the update the binary at %s reports %q, want %s", installed, version, pinnedRelease)
	}

	expectBeside(t, installed, built)
}

// updateResult is the part of bb update's --json result this test reads.
type updateResult struct {
	DryRun          bool   `json:"dryRun"`
	Applied         bool   `json:"applied"`
	UpdateAvailable bool   `json:"updateAvailable"`
	LatestVersion   string `json:"latestVersion"`
	Trust           struct {
		Source            string `json:"source"`
		SignatureVerified bool   `json:"signatureVerified"`
		ChecksumVerified  bool   `json:"checksumVerified"`
	} `json:"trust"`
}

func expectVerified(t *testing.T, what string, result updateResult, trustSource string) {
	t.Helper()

	if !result.Trust.SignatureVerified || !result.Trust.ChecksumVerified {
		t.Fatalf("%s did not verify the release: %+v", what, result.Trust)
	}
	if result.Trust.Source != trustSource {
		t.Fatalf("%s verified against %q, want %q", what, result.Trust.Source, trustSource)
	}
}

// expectBeside checks what the update left in the directory it installed into.
// Linux and macOS replace the binary with a rename and leave nothing else.
// Windows will not delete a running binary, so the one the update replaced is
// set aside beside the new one; the next run of a bb that knows to deletes it,
// which the release installed here predates.
func expectBeside(t *testing.T, installed, built string) {
	t.Helper()

	entries, err := os.ReadDir(filepath.Dir(installed))
	if err != nil {
		t.Fatalf("read the install directory: %v", err)
	}

	name := filepath.Base(installed)
	var others []string
	for _, entry := range entries {
		if entry.Name() != name {
			others = append(others, entry.Name())
		}
	}

	if runtime.GOOS != "windows" {
		if len(others) != 0 {
			t.Fatalf("the update left %v beside %s", others, name)
		}
		return
	}

	if len(others) != 1 || !isAsideName(name, others[0]) {
		t.Fatalf("the update left %v beside %s, want the replaced binary set aside as %s.old-<16 hex digits>", others, name, name)
	}
	aside, err := os.ReadFile(filepath.Join(filepath.Dir(installed), others[0]))
	if err != nil {
		t.Fatalf("read %s: %v", others[0], err)
	}
	original, err := os.ReadFile(built)
	if err != nil {
		t.Fatalf("read the built binary: %v", err)
	}
	if !bytes.Equal(aside, original) {
		t.Fatalf("%s is not the binary the update replaced", others[0])
	}
}

func isAsideName(executable, name string) bool {
	suffix, found := strings.CutPrefix(name, executable+".old-")
	if !found || len(suffix) != 16 {
		return false
	}
	_, err := hex.DecodeString(suffix)

	return err == nil
}

// runUpdate runs installed with --json and args, and returns the result it
// reported. Any exit but 0 fails the test with what bb said.
func runUpdate(t *testing.T, installed string, env []string, args ...string) updateResult {
	t.Helper()

	stdout, stderr, err := run(installed, env, append([]string{"--json"}, args...)...)
	if err != nil {
		t.Fatalf("bb %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout, stderr)
	}

	var envelope struct {
		Data updateResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("bb %s printed %q, not a result document: %v", strings.Join(args, " "), stdout, err)
	}

	return envelope.Data
}

func versionOf(t *testing.T, installed string, env []string) string {
	t.Helper()

	stdout, stderr, err := run(installed, env, "--version")
	if err != nil {
		t.Fatalf("%s --version: %v\n%s", installed, err, stderr)
	}

	return strings.TrimSpace(stdout)
}

func run(binary string, env []string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = env
	command.Dir = filepath.Dir(binary)
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	return stdout.String(), stderr.String(), err
}

// isolatedEnvironment is the environment bb runs in: none of this machine's bb
// settings, a home directory of its own, and the mirror's CA trusted.
func isolatedEnvironment(t *testing.T, caFile string, extra []string) []string {
	t.Helper()

	home := t.TempDir()
	replaced := map[string]bool{
		"HOME": true, "USERPROFILE": true, "XDG_CONFIG_HOME": true, "APPDATA": true, "LOCALAPPDATA": true,
		"HTTPS_PROXY": true, "HTTP_PROXY": true, "NO_PROXY": true,
	}

	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if replaced[upper] || strings.HasPrefix(upper, "BB_") || strings.HasPrefix(upper, "BITBUCKET_") {
			continue
		}
		env = append(env, entry)
	}

	env = append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"APPDATA="+filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"),
		"BB_CONFIG_PATH="+filepath.Join(home, "bb.yaml"),
		"BB_CA_FILE="+caFile,
	)

	return append(env, extra...)
}

// buildBB builds bb from this tree, calling itself builtVersion.
func buildBB(t *testing.T) string {
	t.Helper()

	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("find the module: %v", err)
	}

	built := filepath.Join(t.TempDir(), "bb"+executableSuffix())
	command := exec.Command("go", "build", "-trimpath", "-ldflags", "-X main.Version="+builtVersion, "-o", built, "./cmd/bb")
	command.Dir = filepath.Dir(strings.TrimSpace(string(gomod)))
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build bb: %v\n%s", err, output)
	}

	return built
}

// installCopy copies built into a directory of its own, which is where bb update
// is going to replace it.
func installCopy(t *testing.T, built string) string {
	t.Helper()

	content, err := os.ReadFile(built)
	if err != nil {
		t.Fatalf("read the built binary: %v", err)
	}

	installed := filepath.Join(t.TempDir(), "bb"+executableSuffix())
	if err := os.WriteFile(installed, content, 0o755); err != nil {
		t.Fatalf("install a copy: %v", err)
	}

	return installed
}

// downloadRelease fetches the three files bb update reads from a mirror for this
// platform: the archive, sha256sums.txt, and its Sigstore bundle.
func downloadRelease(t *testing.T) map[string][]byte {
	t.Helper()

	extension := "tar.gz"
	if runtime.GOOS == "windows" {
		extension = "zip"
	}
	archive := fmt.Sprintf("bb_%s_%s_%s.%s", strings.TrimPrefix(pinnedRelease, "v"), runtime.GOOS, runtime.GOARCH, extension)

	client := &http.Client{Timeout: 5 * time.Minute}
	files := make(map[string][]byte, 3)
	for _, name := range []string{archive, "sha256sums.txt", "sha256sums.txt.sigstore.json"} {
		url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repository, pinnedRelease, name)

		var lastErr error
		for attempt := 0; attempt < 3 && files[name] == nil; attempt++ {
			files[name], lastErr = fetch(client, url)
		}
		if files[name] == nil {
			t.Fatalf("download %s: %v", url, lastErr)
		}
	}

	return files
}

func fetch(client *http.Client, url string) ([]byte, error) {
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", response.StatusCode)
	}

	return io.ReadAll(response.Body)
}

// releaseMirror serves a release the way a generic repository does: the files
// under mirrorPrefix, and a manifest naming them by relative URL.
type releaseMirror struct {
	baseURL string
	caFile  string

	mu    sync.Mutex
	paths []string
}

func (mirror *releaseMirror) record(path string) {
	mirror.mu.Lock()
	defer mirror.mu.Unlock()
	mirror.paths = append(mirror.paths, path)
}

func (mirror *releaseMirror) requested() []string {
	mirror.mu.Lock()
	defer mirror.mu.Unlock()

	return append([]string(nil), mirror.paths...)
}

// serveMirror stands in for an internal release mirror on loopback, over TLS
// with a certificate of its own that bb is told to trust.
func serveMirror(t *testing.T, files map[string][]byte) *releaseMirror {
	t.Helper()

	assets := make([]map[string]string, 0, len(files))
	for name := range files {
		assets = append(assets, map[string]string{"name": name, "browser_download_url": name})
	}
	manifest, err := json.Marshal(map[string]any{
		"tag_name": pinnedRelease,
		"html_url": fmt.Sprintf("https://github.com/%s/releases/tag/%s", repository, pinnedRelease),
		"assets":   assets,
	})
	if err != nil {
		t.Fatalf("encode the manifest: %v", err)
	}

	mirror := &releaseMirror{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirror.record(r.URL.Path)

		name, onMirror := strings.CutPrefix(r.URL.Path, mirrorPrefix+"/")
		switch {
		case !onMirror:
			http.NotFound(w, r)
		case name == "releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(manifest)
		case files[name] != nil:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(files[name])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	mirror.baseURL = server.URL + mirrorPrefix
	mirror.caFile = filepath.Join(t.TempDir(), "mirror-ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(mirror.caFile, certificate, 0o644); err != nil {
		t.Fatalf("write the mirror's CA: %v", err)
	}

	return mirror
}

// trustedRootFile is Sigstore's trust root, fetched through TUF and verified
// against the root sigstore-go embeds, in a file for update_trusted_root.
func trustedRootFile(t *testing.T) string {
	t.Helper()

	client, err := tuf.New(tuf.DefaultOptions().WithCachePath(t.TempDir()))
	if err != nil {
		t.Fatalf("start a TUF client: %v", err)
	}
	root, err := client.GetTarget("trusted_root.json")
	if err != nil {
		t.Fatalf("fetch trusted_root.json through TUF: %v", err)
	}

	path := filepath.Join(t.TempDir(), "trusted_root.json")
	if err := os.WriteFile(path, root, 0o644); err != nil {
		t.Fatalf("write the trusted root: %v", err)
	}

	return path
}

// writeSystemPolicy writes the system policy file for one subtest, and removes
// it when that subtest ends.
func writeSystemPolicy(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write the system policy %s: %v. CI makes its directory writable before the test runs.", path, err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Errorf("remove the system policy %s: %v", path, err)
		}
	})
}

// systemPolicyPath is where bb reads system policy from. bb asks Windows for the
// ProgramData folder; the variable names the same one on a runner.
func systemPolicyPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "bb", "config.yaml")
	}

	return "/etc/bb/config.yaml"
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}

	return ""
}
