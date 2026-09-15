package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func httpOptIn(value bool) *bool { return &value }

func expectUpdateURLError(t *testing.T, err error, kind apperrors.Kind, want string) {
	t.Helper()

	if kind == "" {
		if err != nil {
			t.Fatalf("expected the URL to be accepted, got %v", err)
		}
		return
	}
	if !apperrors.IsKind(err, kind) {
		t.Fatalf("expected %s, got %v", kind, err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected the error to contain %q, got %v", want, err)
	}
}

func TestUpdateURLsAreHTTPSOnlyUntilSomeoneAsks(t *testing.T) {
	t.Setenv(settingAllowHTTPUpdate.environment, "")

	permission, err := resolveUpdateHTTPPermission(PolicyConfig{}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if permission.Allowed || permission.ForbiddenByPolicy {
		t.Fatalf("default permission = %+v, want https only with no policy", permission)
	}

	cases := map[string]struct {
		url  string
		kind apperrors.Kind
		want string
	}{
		"https":                 {"https://mirror.corp.internal/bb", "", ""},
		"https on another port": {"https://mirror.corp.internal:8443/bb", "", ""},
		"plain http":            {"http://mirror.corp.internal/bb", apperrors.KindValidation, "pass --allow-http or set BB_ALLOW_HTTP_UPDATE=1"},
		"an upper-case scheme":  {"HTTP://mirror.corp.internal/bb", apperrors.KindValidation, "uses plain HTTP"},
		"another scheme":        {"ftp://mirror.corp.internal/bb", apperrors.KindValidation, "must be an absolute https URL"},
		"a file URL":            {"file:///srv/bb", apperrors.KindValidation, "must be an absolute https URL"},
		"no host":               {"mirror.corp.internal/bb", apperrors.KindValidation, "must be an absolute https URL"},
		"not a URL":             {":\x7f", apperrors.KindValidation, "must be an absolute https URL"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			expectUpdateURLError(t, permission.CheckURL(testCase.url), testCase.kind, testCase.want)
		})
	}
}

func TestPlainHTTPNeedsAnExplicitOptIn(t *testing.T) {
	t.Run("the flag permits it", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "")

		permission, err := resolveUpdateHTTPPermission(PolicyConfig{}, httpOptIn(true))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if !permission.Allowed || permission.Source != "--allow-http" {
			t.Fatalf("permission = %+v, want allowed by --allow-http", permission)
		}
		expectUpdateURLError(t, permission.CheckURL("http://mirror.corp.internal/bb"), "", "")
	})

	t.Run("the environment variable permits it", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "1")

		permission, err := resolveUpdateHTTPPermission(PolicyConfig{}, nil)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if !permission.Allowed || permission.Source != "BB_ALLOW_HTTP_UPDATE" {
			t.Fatalf("permission = %+v, want allowed by BB_ALLOW_HTTP_UPDATE", permission)
		}
	})

	t.Run("the flag outranks the environment variable", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "true")

		permission, err := resolveUpdateHTTPPermission(PolicyConfig{}, httpOptIn(false))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if permission.Allowed {
			t.Fatalf("permission = %+v, want --allow-http=false to win", permission)
		}
	})

	t.Run("a value that is not a boolean is refused", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "sometimes")

		_, err := resolveUpdateHTTPPermission(PolicyConfig{}, nil)
		expectUpdateURLError(t, err, apperrors.KindValidation, "BB_ALLOW_HTTP_UPDATE must be a boolean")
	})
}

func TestAllowHTTPUpdatePolicyOverridesTheUser(t *testing.T) {
	forbid := PolicyConfig{AllowHTTPUpdate: httpOptIn(false)}
	permit := PolicyConfig{AllowHTTPUpdate: httpOptIn(true)}

	t.Run("false refuses the flag", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "")

		_, err := resolveUpdateHTTPPermission(forbid, httpOptIn(true))
		expectUpdateURLError(t, err, apperrors.KindAuthorization, "--allow-http is refused: plain-HTTP update URLs are disabled by administrative policy")
	})

	t.Run("false refuses the environment variable", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "1")

		_, err := resolveUpdateHTTPPermission(forbid, nil)
		expectUpdateURLError(t, err, apperrors.KindAuthorization, "BB_ALLOW_HTTP_UPDATE is refused")
	})

	t.Run("false refuses a plain-HTTP URL nobody opted into", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "")

		permission, err := resolveUpdateHTTPPermission(forbid, nil)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if permission.Allowed || !permission.ForbiddenByPolicy {
			t.Fatalf("permission = %+v, want forbidden by policy", permission)
		}
		expectUpdateURLError(t, permission.CheckURL("http://mirror.corp.internal/bb"), apperrors.KindAuthorization, "which administrative policy forbids (allow_http_update)")
		expectUpdateURLError(t, permission.CheckURL("https://mirror.corp.internal/bb"), "", "")
	})

	t.Run("false accepts an opt-in set to false", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "")

		if _, err := resolveUpdateHTTPPermission(forbid, httpOptIn(false)); err != nil {
			t.Fatalf("--allow-http=false asks for nothing policy forbids, got %v", err)
		}
	})

	t.Run("true permits plain HTTP without an opt-in", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "")

		permission, err := resolveUpdateHTTPPermission(permit, nil)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if !permission.Allowed || permission.Source != "the allow_http_update policy" {
			t.Fatalf("permission = %+v, want allowed by policy", permission)
		}
	})

	t.Run("true is not undone by the user", func(t *testing.T) {
		t.Setenv(settingAllowHTTPUpdate.environment, "false")

		permission, err := resolveUpdateHTTPPermission(permit, httpOptIn(false))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if !permission.Allowed {
			t.Fatalf("permission = %+v, want policy to decide", permission)
		}
	})
}

func TestAllowHTTPUpdateIsReadFromTheSystemConfiguration(t *testing.T) {
	files := map[string]string{
		"at the top level":      "allow_http_update: false\n",
		"in a policies mapping": "policies:\n  allow_http_update: false\n",
		"in a policy mapping":   "policy:\n  allow_http_update: false\n",
	}
	for name, content := range files {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			systemPath := filepath.Join(directory, "system-config.yaml")
			if err := os.WriteFile(systemPath, []byte(content), 0o600); err != nil {
				t.Fatalf("write system config: %v", err)
			}
			t.Setenv("BB_SYSTEM_CONFIG_PATH", systemPath)
			t.Setenv("BB_CONFIG_PATH", filepath.Join(directory, "user.yaml"))
			t.Setenv(settingAllowHTTPUpdate.environment, "")

			_, err := ResolveUpdateHTTPPermission(httpOptIn(true))
			expectUpdateURLError(t, err, apperrors.KindAuthorization, "disabled by administrative policy")
		})
	}
}

func TestAnUpdateURLErrorDoesNotPrintAPassword(t *testing.T) {
	t.Parallel()

	err := UpdateHTTPPermission{}.CheckURL("http://deploy:hunter2@mirror.corp.internal/bb")
	if err == nil {
		t.Fatal("expected plain HTTP to be refused")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("the error prints the URL's password: %v", err)
	}
	if !strings.Contains(err.Error(), "mirror.corp.internal") {
		t.Fatalf("the error does not name the host: %v", err)
	}
}
