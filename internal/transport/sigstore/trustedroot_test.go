package sigstore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

const minimalTrustedRoot = `{"mediaType":"application/vnd.dev.sigstore.trustedroot+json;version=0.1"}`

func writeTrustedRoot(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trusted_root.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write trusted root: %v", err)
	}
	return path
}

func TestTrustedRootFromFile(t *testing.T) {
	t.Run("loads trust material without touching the network", func(t *testing.T) {
		provider := TrustedRootFromFile(writeTrustedRoot(t, minimalTrustedRoot))
		material, err := provider(context.Background())
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if material == nil {
			t.Fatal("expected trust material")
		}
	})

	t.Run("reports a missing file as trust material unavailable", func(t *testing.T) {
		provider := TrustedRootFromFile(filepath.Join(t.TempDir(), "absent.json"))
		_, err := provider(context.Background())
		if err == nil {
			t.Fatal("expected an error for a missing trusted root")
		}
		if !errors.Is(err, ErrTrustedRootUnavailable) {
			t.Fatalf("expected ErrTrustedRootUnavailable, got: %v", err)
		}
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected KindTransient, got: %v", err)
		}
	})
}

func TestTrustedRootFromTUFUsesSuppliedClient(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	provider := TrustedRootFromTUF(server.URL, server.Client())
	_, err := provider(context.Background())
	if err == nil {
		t.Fatal("expected an error from a mirror that serves no TUF metadata")
	}
	if !errors.Is(err, ErrTrustedRootUnavailable) {
		t.Fatalf("expected ErrTrustedRootUnavailable, got: %v", err)
	}
	if len(requested) == 0 {
		t.Fatal("expected the configured mirror to be contacted through the supplied client")
	}
}

func TestHTTPFetcherDownloadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("payload"))
		case "/large":
			_, _ = w.Write([]byte(strings.Repeat("x", 64)))
		default:
			http.Error(w, "nope", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	fetcher := &httpFetcher{client: server.Client()}

	body, err := fetcher.DownloadFile(server.URL+"/ok", 1024, time.Second)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("unexpected body: %s", body)
	}

	if _, err := fetcher.DownloadFile(server.URL+"/large", 8, time.Second); err == nil {
		t.Fatal("expected an oversized response to be rejected")
	}

	if _, err := fetcher.DownloadFile(server.URL+"/missing", 1024, time.Second); err == nil {
		t.Fatal("expected a non-200 response to be rejected")
	}
}

func TestNewReleaseVerifierIdentityResolution(t *testing.T) {
	t.Run("defaults to the bb release workflow", func(t *testing.T) {
		verifier := NewReleaseVerifier(ReleaseVerifierOptions{Owner: "vriesdemichael", Repo: "bitbucket-data-center-cli"})
		if verifier.expectedIssuer != GitHubActionsIssuer {
			t.Fatalf("unexpected issuer: %s", verifier.expectedIssuer)
		}
		expected := "https://github.com/vriesdemichael/bitbucket-data-center-cli/" + ReleaseWorkflowPath + "@" + MainBranchRef
		if verifier.expectedSAN != expected {
			t.Fatalf("expected %s, got %s", expected, verifier.expectedSAN)
		}
	})

	t.Run("policy overrides replace the pinned signer", func(t *testing.T) {
		verifier := NewReleaseVerifier(ReleaseVerifierOptions{
			Owner:            "vriesdemichael",
			Repo:             "bitbucket-data-center-cli",
			ExpectedIdentity: " https://gitlab.corp.internal/ops/bb//.gitlab-ci.yml@refs/heads/main ",
			ExpectedIssuer:   " https://fulcio.corp.internal ",
		})
		if verifier.expectedIssuer != "https://fulcio.corp.internal" {
			t.Fatalf("unexpected issuer: %s", verifier.expectedIssuer)
		}
		if !strings.HasPrefix(verifier.expectedSAN, "https://gitlab.corp.internal/") {
			t.Fatalf("unexpected identity: %s", verifier.expectedSAN)
		}
	})

	t.Run("a configured trusted root replaces the public TUF fetch", func(t *testing.T) {
		verifier := NewReleaseVerifier(ReleaseVerifierOptions{
			Owner:           "vriesdemichael",
			Repo:            "bitbucket-data-center-cli",
			TrustedRootPath: writeTrustedRoot(t, minimalTrustedRoot),
		})

		material, err := verifier.trustedMaterialProvider(context.Background())
		if err != nil {
			t.Fatalf("expected offline trust material, got: %v", err)
		}
		if material == nil {
			t.Fatal("expected trust material")
		}
	})

	t.Run("a trusted root file wins over a TUF mirror", func(t *testing.T) {
		verifier := NewReleaseVerifier(ReleaseVerifierOptions{
			Owner:            "vriesdemichael",
			Repo:             "bitbucket-data-center-cli",
			TrustedRootPath:  writeTrustedRoot(t, minimalTrustedRoot),
			TUFRepositoryURL: "https://artifactory.corp.internal/tuf",
		})

		if _, err := verifier.trustedMaterialProvider(context.Background()); err != nil {
			t.Fatalf("expected the file to be used, got: %v", err)
		}
	})
}
