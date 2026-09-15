package updatecmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/githubrelease"
	updateworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/update"
)

func fetchThrough(t *testing.T, transport http.RoundTripper, target string) (string, error) {
	t.Helper()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return string(body), nil
}

func plainHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("served over plain HTTP"))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestTheUpdateTransportRefusesPlainHTTPUnlessPermitted(t *testing.T) {
	t.Parallel()

	plain := plainHTTPServer(t)

	_, err := fetchThrough(t, requireScheme(http.DefaultTransport, config.UpdateHTTPPermission{}), plain.URL+"/sha256sums.txt")
	if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "uses plain HTTP") {
		t.Fatalf("expected the plain-HTTP request to be refused as validation, got %v", err)
	}

	body, err := fetchThrough(t, requireScheme(http.DefaultTransport, config.UpdateHTTPPermission{Allowed: true}), plain.URL+"/sha256sums.txt")
	if err != nil || body != "served over plain HTTP" {
		t.Fatalf("expected a permitted plain-HTTP request to go through, got %q, %v", body, err)
	}
}

// TestAnHTTPSMirrorCannotRedirectToPlainHTTP is why the check sits on the
// transport: the base URL is https, and only the redirect is not.
func TestAnHTTPSMirrorCannotRedirectToPlainHTTP(t *testing.T) {
	t.Parallel()

	plain := plainHTTPServer(t)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, plain.URL+request.URL.Path, http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	trustsTheTestCertificate := secure.Client().Transport

	_, err := fetchThrough(t, requireScheme(trustsTheTestCertificate, config.UpdateHTTPPermission{}), secure.URL+"/sha256sums.txt")
	if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), strings.TrimPrefix(plain.URL, "http://")) {
		t.Fatalf("expected the redirect to plain HTTP to be refused, naming its target, got %v", err)
	}

	body, err := fetchThrough(t, requireScheme(trustsTheTestCertificate, config.UpdateHTTPPermission{Allowed: true}), secure.URL+"/sha256sums.txt")
	if err != nil || body != "served over plain HTTP" {
		t.Fatalf("expected a permitted redirect to go through, got %q, %v", body, err)
	}
}

// TestTheReleaseClientKeepsTheRefusalsKind: the release client wraps transport
// errors as transient, and a refusal is not something to retry.
func TestTheReleaseClientKeepsTheRefusalsKind(t *testing.T) {
	t.Parallel()

	plain := plainHTTPServer(t)
	client := githubrelease.NewClient(plain.URL, &http.Client{Transport: requireScheme(http.DefaultTransport, config.UpdateHTTPPermission{})}, "bb/test")

	_, err := client.Download(context.Background(), "sha256sums.txt")
	if kind := apperrors.KindOf(err); kind != apperrors.KindValidation {
		t.Fatalf("expected a validation error, got kind %q: %v", kind, err)
	}
}

func TestUpdateRefusesAPlainHTTPMirrorBeforeFetchingAnything(t *testing.T) {
	if BuildDisablesSelfUpdate {
		t.Skip("skipping in no_self_update build")
	}

	directory := t.TempDir()
	systemPath := filepath.Join(directory, "system-config.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", systemPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(directory, "user.yaml"))
	t.Setenv("BB_DISABLE_UPDATE", "")
	t.Setenv("BB_UPDATE_BASE_URL", "")
	t.Setenv("BB_ALLOW_HTTP_UPDATE", "")

	originalFactory := UpdateRunnerFactory
	t.Cleanup(func() { UpdateRunnerFactory = originalFactory })

	var built *UpdateCommandHTTPConfig
	UpdateRunnerFactory = func(version string, httpConfig UpdateCommandHTTPConfig) *updateworkflow.Runner {
		built = &httpConfig
		return updateworkflow.NewRunner(updateworkflow.Dependencies{
			Releases: updateCommandReleaseClient{
				release: githubrelease.Release{TagName: "v1.1.0", HTMLURL: "https://example.test/release"},
			},
			RepositoryOwner: "vriesdemichael",
			RepositoryName:  "bitbucket-data-center-cli",
			CurrentVersion:  func() string { return "v1.1.0" },
			ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
			Platform:        func() (string, string) { return "linux", "amd64" },
		})
	}

	run := func(args ...string) (string, error) {
		built = nil
		root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
		root.AddCommand(New(Dependencies{}))
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		root.SetOut(stdout)
		root.SetErr(stderr)
		root.SetArgs(append([]string{"update"}, args...))
		err := root.Execute()
		return stderr.String(), err
	}

	const mirror = "http://mirror.corp.internal/bb"

	_, err := run("--base-url", mirror)
	if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "--allow-http") {
		t.Fatalf("expected a plain-HTTP mirror to be refused with the way to permit it, got %v", err)
	}
	if built != nil {
		t.Fatal("the runner was built for a mirror the command refused")
	}

	stderr, err := run("--base-url", mirror, "--allow-http")
	if err != nil {
		t.Fatalf("expected --allow-http to permit the mirror, got %v", err)
	}
	if built == nil || built.UpdateBaseURL != mirror || !built.HTTPPermission.Allowed {
		t.Fatalf("expected the runner to be built for %s with plain HTTP permitted, got %+v", mirror, built)
	}
	if !strings.Contains(stderr, "plain-HTTP release mirror") || !strings.Contains(stderr, "permitted by --allow-http") {
		t.Fatalf("expected a warning naming the mirror and the opt-in, got %q", stderr)
	}

	if err := os.WriteFile(systemPath, []byte("allow_http_update: false\n"), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	_, err = run("--base-url", mirror, "--allow-http")
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected policy to refuse --allow-http, got %v", err)
	}
	if built != nil {
		t.Fatal("the runner was built although policy refused plain HTTP")
	}
}
