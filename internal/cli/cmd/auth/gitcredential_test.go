package auth

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func TestParseCredentialRequest(t *testing.T) {
	input := "protocol=https\nhost=bitbucket.example.com\npath=scm/PROJ/repo.git\nusername=alice\n\n"

	request, err := parseCredentialRequest(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if request.Protocol != "https" || request.Host != "bitbucket.example.com" {
		t.Fatalf("unexpected request: %+v", request)
	}
	if request.Path != "scm/PROJ/repo.git" || request.Username != "alice" {
		t.Fatalf("unexpected request: %+v", request)
	}
}

// git adds new keys over time (wwwauth[], capability[]). A helper that rejects
// what it does not recognise breaks on the next git release.
func TestParseCredentialRequestIgnoresUnknownKeys(t *testing.T) {
	input := "protocol=https\nhost=bitbucket.example.com\nwwwauth[]=Basic realm=\"x\"\ncapability[]=authtype\n\n"

	request, err := parseCredentialRequest(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if request.Host != "bitbucket.example.com" {
		t.Fatalf("expected host to survive unknown keys, got %+v", request)
	}
}

// A blank line terminates the request. Anything after it belongs to the next
// exchange and must not leak into this one.
func TestParseCredentialRequestStopsAtBlankLine(t *testing.T) {
	input := "protocol=https\nhost=bitbucket.example.com\n\nhost=evil.example.org\n"

	request, err := parseCredentialRequest(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if request.Host != "bitbucket.example.com" {
		t.Fatalf("request read past the terminator: %+v", request)
	}
}

func TestCredentialRequestURL(t *testing.T) {
	cases := map[credentialRequest]string{
		{Protocol: "https", Host: "bitbucket.example.com"}:      "https://bitbucket.example.com",
		{Protocol: "http", Host: "localhost:7990"}:              "http://localhost:7990",
		{Host: "bitbucket.example.com"}:                         "https://bitbucket.example.com",
		{Protocol: "https", Host: "bitbucket.example.com:8443"}: "https://bitbucket.example.com:8443",
	}

	for request, expected := range cases {
		if actual := request.URL(); actual != expected {
			t.Fatalf("URL() for %+v = %q, want %q", request, actual, expected)
		}
	}
}

// bb owns credential storage through `bb auth login`. Letting git write back
// would create a second writer that silently diverges from the keyring, so both
// verbs are accepted and ignored rather than rejected — rejecting them would
// make git treat the exchange as failed.
func TestGitCredentialStoreAndEraseAreAcceptedAndIgnored(t *testing.T) {
	for _, operation := range []string{"store", "erase"} {
		cmd := newGitCredentialCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader("protocol=https\nhost=bitbucket.example.com\n\n"))
		cmd.SetArgs([]string{operation})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("%s should succeed silently, got %v", operation, err)
		}
		if out.Len() != 0 {
			t.Fatalf("%s wrote to stdout: %q", operation, out.String())
		}
	}
}

func TestGitCredentialRejectsUnknownOperation(t *testing.T) {
	cmd := newGitCredentialCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"exfiltrate"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an unknown credential operation to be rejected")
	}
}

// A request with no host is not answerable. Silence lets git move on; an error
// would abort the whole credential lookup.
func TestGitCredentialWithoutHostStaysSilent(t *testing.T) {
	cmd := newGitCredentialCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("protocol=https\n\n"))
	cmd.SetArgs([]string{"get"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected silence rather than an error, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output, got %q", out.String())
	}
}

// Regression guard for the defect this command was written to avoid. An early
// version resolved credentials with the non-strict lookup, which falls back to
// the configured default host, so asking for github.com returned the Bitbucket
// token — handing the credential to an unrelated host.
func TestGitCredentialDoesNotAnswerForUnconfiguredHosts(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", t.TempDir()+"/config.yaml")

	for _, host := range []string{"github.com", "evil.example.org", "gitlab.com"} {
		cmd := newGitCredentialCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader("protocol=https\nhost=" + host + "\n\n"))
		cmd.SetArgs([]string{"get"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("expected silence for %s, got %v", host, err)
		}
		if out.Len() != 0 {
			t.Fatalf("leaked credentials for unconfigured host %s: %q", host, out.String())
		}
	}
}

func TestSetupGitRequiresAHost(t *testing.T) {
	cmd := newSetupGitCommand(Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return config.AppConfig{}, nil },
		ConfigureGitCredentialHelper: func(context.Context, string, string, bool, bool) error {
			t.Fatal("should not configure git without a host")
			return nil
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error when no host is configured")
	}
}

func TestSetupGitRejectsAMalformedHost(t *testing.T) {
	cmd := newSetupGitCommand(Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return config.AppConfig{}, nil },
		ConfigureGitCredentialHelper: func(context.Context, string, string, bool, bool) error {
			t.Fatal("should not configure git for a malformed host")
			return nil
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--host", "not-a-url"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected a malformed host to be rejected")
	}
}

// The configuration key must be scoped to the host. A bare credential.helper is
// consulted for every remote git talks to, which would offer bb — and therefore
// Bitbucket credentials — to unrelated hosts.
func TestSetupGitScopesTheHelperToTheHost(t *testing.T) {
	var capturedKey, capturedValue string

	cmd := newSetupGitCommand(Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return config.AppConfig{}, nil },
		ConfigureGitCredentialHelper: func(_ context.Context, key, value string, _, _ bool) error {
			capturedKey, capturedValue = key, value
			return nil
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--host", "https://bitbucket.example.com/"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedKey != "credential.https://bitbucket.example.com.helper" {
		t.Fatalf("helper is not scoped to the host: %q", capturedKey)
	}
	if !strings.Contains(capturedValue, "auth git-credential") {
		t.Fatalf("unexpected helper command: %q", capturedValue)
	}
	// Git resolves the helper through a shell whose PATH may differ from the
	// user's, and a helper that fails to launch is indistinguishable from
	// missing credentials, so the executable must be addressed absolutely.
	if !strings.HasPrefix(capturedValue, "!") {
		t.Fatalf("helper must be a shell command starting with !: %q", capturedValue)
	}
}

func TestSetupGitPassesScopeAndForceThrough(t *testing.T) {
	var gotGlobal, gotForce bool

	cmd := newSetupGitCommand(Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return config.AppConfig{}, nil },
		ConfigureGitCredentialHelper: func(_ context.Context, _, _ string, global, force bool) error {
			gotGlobal, gotForce = global, force
			return nil
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--host", "https://bitbucket.example.com", "--global=false", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotGlobal {
		t.Fatal("expected --global=false to be passed through")
	}
	if !gotForce {
		t.Fatal("expected --force to be passed through")
	}
}

// Matching `gh auth setup-git`, the default writes to the global config so a
// single setup covers every clone of that host.
func TestSetupGitDefaultsToGlobalScope(t *testing.T) {
	var gotGlobal bool

	cmd := newSetupGitCommand(Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return config.AppConfig{}, nil },
		ConfigureGitCredentialHelper: func(_ context.Context, _, _ string, global, _ bool) error {
			gotGlobal = global
			return nil
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--host", "https://bitbucket.example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotGlobal {
		t.Fatal("expected setup-git to default to global scope")
	}
}
