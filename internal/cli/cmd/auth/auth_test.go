package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type fakeUsersClient struct {
	response     *openapigenerated.GetUsers2Response
	userResponse *openapigenerated.GetUserResponse
	err          error
}

type fakeReposClient struct {
	recent *openapigenerated.GetRepositoriesRecentlyAccessedResponse
	all    *openapigenerated.GetRepositories1Response
	err    error
}

func (client *fakeUsersClient) GetUsers2WithResponse(ctx context.Context, params *openapigenerated.GetUsers2Params, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetUsers2Response, error) {
	if client.err != nil {
		return nil, client.err
	}
	return client.response, nil
}

func (client *fakeUsersClient) GetUserWithResponse(ctx context.Context, userSlug string, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetUserResponse, error) {
	if client.err != nil {
		return nil, client.err
	}
	return client.userResponse, nil
}

func (client *fakeReposClient) GetRepositoriesRecentlyAccessedWithResponse(ctx context.Context, params *openapigenerated.GetRepositoriesRecentlyAccessedParams, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetRepositoriesRecentlyAccessedResponse, error) {
	if client.err != nil {
		return nil, client.err
	}
	return client.recent, nil
}

func (client *fakeReposClient) GetRepositories1WithResponse(ctx context.Context, params *openapigenerated.GetRepositories1Params, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetRepositories1Response, error) {
	if client.err != nil {
		return nil, client.err
	}
	return client.all, nil
}

func TestStatusJSONUsesHostOverride(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://initial.example")

	var seenHost string
	cmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			seenHost = os.Getenv("BITBUCKET_URL")
			return config.AppConfig{
				BitbucketURL:           seenHost,
				BitbucketVersionTarget: "9.4.16",
				AuthSource:             "env/default",
			}, nil
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	buffer := &bytes.Buffer{}
	cmd.SetOut(buffer)
	cmd.SetErr(buffer)
	cmd.SetArgs([]string{"status", "--host", "http://override.example"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if seenHost != "http://override.example" {
		t.Fatalf("expected host override to be applied, got %q", seenHost)
	}

	var parsed map[string]string
	if err := decodeJSONEnvelopeData(buffer.Bytes(), &parsed); err != nil {
		t.Fatalf("expected json output, got %q (%v)", buffer.String(), err)
	}

	if parsed["bitbucket_url"] != "http://override.example" {
		t.Fatalf("expected overridden bitbucket_url, got %q", parsed["bitbucket_url"])
	}
	if parsed["auth_mode"] != "none" {
		t.Fatalf("expected auth_mode none, got %q", parsed["auth_mode"])
	}
}

func decodeJSONEnvelopeData(raw []byte, target any) error {
	var envelope struct {
		Version string `json:"version"`
		Data    any    `json:"data"`
	}

	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}

	if strings.TrimSpace(envelope.Version) == "" {
		return os.ErrInvalid
	}

	if envelope.Data == nil {
		return os.ErrInvalid
	}

	encodedData, err := json.Marshal(envelope.Data)
	if err != nil {
		return err
	}

	return json.Unmarshal(encodedData, target)
}

func TestStatusMissingDependenciesReturnError(t *testing.T) {
	cmd := New(Dependencies{})
	buffer := &bytes.Buffer{}
	cmd.SetOut(buffer)
	cmd.SetErr(buffer)
	cmd.SetArgs([]string{"status"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when dependencies are missing")
	}
}

func TestAuthCommandAdditionalBranches(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	t.Run("login with positional host arg stores credentials", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://resolved.local:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
		})

		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"login", "http://resolved.local:7990", "--token", "abc", "--set-default=true"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("login failed: %v", err)
		}
		if !strings.Contains(out.String(), "resolved.local") {
			t.Fatalf("expected resolved host in output, got: %s", out.String())
		}
	})

	t.Run("logout json path", func(t *testing.T) {
		if _, err := config.SaveLogin(config.LoginInput{Host: "http://logout.local:7990", Token: "tok", SetDefault: true}); err != nil {
			t.Fatalf("save login for logout: %v", err)
		}

		cmd := New(Dependencies{
			JSONEnabled: func() bool { return true },
			LoadConfig: func() (config.AppConfig, error) {
				return config.LoadFromEnv()
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
		})

		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"logout", "--host", "http://logout.local:7990"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("logout json failed: %v", err)
		}
		if !strings.Contains(out.String(), "\"status\": \"ok\"") {
			t.Fatalf("expected json ok status, got: %s", out.String())
		}
	})

	t.Run("default dependency handlers return configured errors", func(t *testing.T) {
		cmd := New(Dependencies{JSONEnabled: func() bool { return true }})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected dependency error when LoadConfig default is used")
		}

		cmd = New(Dependencies{JSONEnabled: func() bool { return true }, LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: "http://x.local:7990"}, nil
		}})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected dependency error when WriteJSON default is used")
		}
	})
}

func TestServerListAndUseCommands(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := config.SaveLogin(config.LoginInput{Host: "http://one.local:7990", Token: "token-1", SetDefault: true}); err != nil {
		t.Fatalf("save login one: %v", err)
	}
	if _, err := config.SaveLogin(config.LoginInput{Host: "http://two.local:7990", Token: "token-2", SetDefault: false}); err != nil {
		t.Fatalf("save login two: %v", err)
	}

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	listOutput := &bytes.Buffer{}
	cmd.SetOut(listOutput)
	cmd.SetErr(listOutput)
	cmd.SetArgs([]string{"server", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("server list failed: %v", err)
	}

	listText := listOutput.String()
	if !strings.Contains(listText, "* http://one.local:7990") {
		t.Fatalf("expected default host marker in list output, got: %s", listText)
	}
	if !strings.Contains(listText, "http://two.local:7990") {
		t.Fatalf("expected second host in list output, got: %s", listText)
	}

	useOutput := &bytes.Buffer{}
	cmd = New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})
	cmd.SetOut(useOutput)
	cmd.SetErr(useOutput)
	cmd.SetArgs([]string{"server", "use", "--host", "http://two.local:7990"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("server use failed: %v", err)
	}

	if !strings.Contains(useOutput.String(), "Active server set to http://two.local:7990") {
		t.Fatalf("unexpected server use output: %s", useOutput.String())
	}

	stored, err := config.LoadStoredConfig()
	if err != nil {
		t.Fatalf("load stored config: %v", err)
	}
	if stored.DefaultHost != "http://two.local:7990" {
		t.Fatalf("expected default host to switch, got: %q", stored.DefaultHost)
	}
}

func TestServerCommandsJSONAndEmptyStates(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	emptyBuffer := &bytes.Buffer{}
	cmd.SetOut(emptyBuffer)
	cmd.SetErr(emptyBuffer)
	cmd.SetArgs([]string{"server", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("server list empty failed: %v", err)
	}
	if !strings.Contains(emptyBuffer.String(), "No stored server contexts") {
		t.Fatalf("expected empty contexts message, got: %s", emptyBuffer.String())
	}

	if _, err := config.SaveLogin(config.LoginInput{Host: "http://json.local:7990", Token: "token", SetDefault: true}); err != nil {
		t.Fatalf("save login json host: %v", err)
	}

	jsonCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	jsonBuffer := &bytes.Buffer{}
	jsonCmd.SetOut(jsonBuffer)
	jsonCmd.SetErr(jsonBuffer)
	jsonCmd.SetArgs([]string{"server", "list"})
	if err := jsonCmd.Execute(); err != nil {
		t.Fatalf("server list json failed: %v", err)
	}
	if !strings.Contains(jsonBuffer.String(), "servers") {
		t.Fatalf("expected servers in json payload, got: %s", jsonBuffer.String())
	}

	useCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	useBuffer := &bytes.Buffer{}
	useCmd.SetOut(useBuffer)
	useCmd.SetErr(useBuffer)
	useCmd.SetArgs([]string{"server", "use", "http://json.local:7990"})
	if err := useCmd.Execute(); err != nil {
		t.Fatalf("server use positional failed: %v", err)
	}
	if !strings.Contains(useBuffer.String(), "default_host") {
		t.Fatalf("expected default_host in json output, got: %s", useBuffer.String())
	}
}

func TestServerCommandsErrorBranches(t *testing.T) {
	configPath := t.TempDir()
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"server", "list"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when config path points to directory")
	}

	configPath = filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	if _, err := config.SaveLogin(config.LoginInput{Host: "http://example.local:7990", Token: "t", SetDefault: true}); err != nil {
		t.Fatalf("save login: %v", err)
	}

	cmd = New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"server", "use"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected validation error when host is missing")
	}

	cmd = New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"server", "use", "--host", "http://unknown.local:7990"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected not-found error when selecting unknown host")
	}
}

func TestServerListIncludesUsernameForBasicAuth(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := config.SaveLogin(config.LoginInput{Host: "http://basic.local:7990", Username: "alice", Password: "secret", SetDefault: true}); err != nil {
		t.Fatalf("save basic login: %v", err)
	}

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	})

	buffer := &bytes.Buffer{}
	cmd.SetOut(buffer)
	cmd.SetErr(buffer)
	cmd.SetArgs([]string{"server", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("server list failed: %v", err)
	}

	if !strings.Contains(buffer.String(), "user=alice") {
		t.Fatalf("expected username in server list output, got: %s", buffer.String())
	}
}

func TestTokenURLCommand(t *testing.T) {
	t.Run("human output with explicit host", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
		})

		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs([]string{"token-url", "--host", "https://bitbucket.acme.corp"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("token-url command failed: %v", err)
		}
		if !strings.Contains(buffer.String(), "https://bitbucket.acme.corp/plugins/servlet/access-tokens/manage") {
			t.Fatalf("expected PAT URL in output, got: %s", buffer.String())
		}
	})

	t.Run("json output with config host fallback", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return true },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://localhost:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
		})

		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs([]string{"token-url"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("token-url json command failed: %v", err)
		}
		if !strings.Contains(buffer.String(), `"token_url": "http://localhost:7990/plugins/servlet/access-tokens/manage"`) {
			t.Fatalf("expected token_url in json output, got: %s", buffer.String())
		}
	})

	t.Run("invalid host returns validation error", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
		})

		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"token-url", "--host", "://bad"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected validation error for invalid host")
		}
	})
}

func TestIdentityCommand(t *testing.T) {
	t.Run("human output", func(t *testing.T) {
		displayName := "Automation User"
		email := "automation@example.local"
		name := "svc-automation"
		slug := "svc-automation"
		id := int32(42)
		userType := openapigenerated.RestApplicationUserTypeNORMAL
		active := true

		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://example.local:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
			NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
				return &fakeUsersClient{response: &openapigenerated.GetUsers2Response{
					HTTPResponse: &http.Response{StatusCode: 200},
					ApplicationjsonCharsetUTF8200: &openapigenerated.RestApplicationUser{
						DisplayName:  &displayName,
						EmailAddress: &email,
						Name:         &name,
						Slug:         &slug,
						Id:           &id,
						Type:         &userType,
						Active:       &active,
					},
				}}, nil
			},
		})

		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs([]string{"identity"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("identity command failed: %v", err)
		}
		if !strings.Contains(buffer.String(), "Automation User") || !strings.Contains(buffer.String(), "name=svc-automation") {
			t.Fatalf("expected identity summary output, got: %s", buffer.String())
		}
	})

	t.Run("json output", func(t *testing.T) {
		name := "svc-bot"
		slug := "svc-bot"
		active := true

		cmd := New(Dependencies{
			JSONEnabled: func() bool { return true },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://example.local:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
			NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
				return &fakeUsersClient{response: &openapigenerated.GetUsers2Response{
					HTTPResponse:                  &http.Response{StatusCode: 200},
					ApplicationjsonCharsetUTF8200: &openapigenerated.RestApplicationUser{Name: &name, Slug: &slug, Active: &active},
				}}, nil
			},
		})

		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs([]string{"identity"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("identity json command failed: %v", err)
		}
		if !strings.Contains(buffer.String(), `"slug": "svc-bot"`) {
			t.Fatalf("expected identity slug in json output, got: %s", buffer.String())
		}
	})

	t.Run("api failure surfaces error", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://example.local:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
			NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
				return &fakeUsersClient{err: errors.New("boom")}, nil
			},
		})

		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"whoami"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected identity lookup error")
		}
	})

	t.Run("host override setenv failure returns error", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://example.local:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
			NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
				return &fakeUsersClient{}, nil
			},
		})

		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"identity", "--host", string([]byte{'a', 0, 'b'})})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected host override validation error")
		}
	})

	t.Run("identity status error is mapped", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://example.local:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
			NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
				return &fakeUsersClient{response: &openapigenerated.GetUsers2Response{HTTPResponse: &http.Response{StatusCode: 401}, Body: []byte("unauthorized")}}, nil
			},
		})

		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"identity"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected mapped auth error")
		}
		if apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected auth error exit code 3, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("human summary fallback unknown", func(t *testing.T) {
		if got := identityHumanSummary(authIdentity{}); !strings.Contains(got, "unknown") {
			t.Fatalf("expected unknown fallback, got %q", got)
		}
	})

	t.Run("resolves identity via X-AUSERNAME header", func(t *testing.T) {
		displayName := "Real Authenticated User"
		email := "realuser@example.local"
		name := "real-user"
		slug := "real-user"
		id := int32(100)
		userType := openapigenerated.RestApplicationUserTypeNORMAL
		active := true

		header := http.Header{}
		header.Set("X-AUSERNAME", "real-user")

		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://example.local:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
			NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
				return &fakeUsersClient{
					response: &openapigenerated.GetUsers2Response{
						HTTPResponse: &http.Response{
							StatusCode: 200,
							Header:     header,
						},
						ApplicationjsonCharsetUTF8200: &openapigenerated.RestApplicationUser{
							Active: nil,
						},
					},
					userResponse: &openapigenerated.GetUserResponse{
						HTTPResponse: &http.Response{StatusCode: 200},
						ApplicationjsonCharsetUTF8200: &openapigenerated.RestApplicationUser{
							DisplayName:  &displayName,
							EmailAddress: &email,
							Name:         &name,
							Slug:         &slug,
							Id:           &id,
							Type:         &userType,
							Active:       &active,
						},
					},
				}, nil
			},
		})

		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs([]string{"identity"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("identity command failed: %v", err)
		}
		if !strings.Contains(buffer.String(), "Real Authenticated User") || !strings.Contains(buffer.String(), "name=real-user") {
			t.Fatalf("expected real user identity summary output, got: %s", buffer.String())
		}
	})
}
func TestPersonalAccessTokenURL(t *testing.T) {
	t.Run("generic URL without user slug", func(t *testing.T) {
		got, err := personalAccessTokenURL("https://bitbucket.corp", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://bitbucket.corp/plugins/servlet/access-tokens/manage" {
			t.Fatalf("unexpected URL: %q", got)
		}
	})

	t.Run("per-user URL with slug", func(t *testing.T) {
		got, err := personalAccessTokenURL("https://bitbucket.corp", "alice")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://bitbucket.corp/plugins/servlet/access-tokens/users/alice/manage" {
			t.Fatalf("unexpected URL: %q", got)
		}
	})

	t.Run("empty host returns validation error", func(t *testing.T) {
		_, err := personalAccessTokenURL("", "alice")
		if err == nil {
			t.Fatal("expected validation error for empty host")
		}
	})
}

func TestTokenURLCommandWithUserSlug(t *testing.T) {
	slug := "alice"
	respondWith := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"alice","slug":%q,"displayName":"Alice","id":1,"type":"NORMAL","active":true}`, slug)
	}
	server := httptest.NewServer(http.HandlerFunc(respondWith))
	defer server.Close()

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: server.URL, BitbucketToken: "tok"}, nil
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
		NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
			return openapi.NewClientWithResponsesFromConfig(cfg)
		},
	})

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"token-url"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("token-url with slug failed: %v", err)
	}

	got := buf.String()
	expected := fmt.Sprintf("/plugins/servlet/access-tokens/users/%s/manage", slug)
	if !strings.Contains(got, expected) {
		t.Fatalf("expected per-user PAT URL in output, got: %q", got)
	}
}

// TestAuthNonJSONHumanOutputPaths covers the human-readable output branches that are skipped
// when --json / JSONEnabled is true.
func TestAuthNonJSONHumanOutputPaths(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	t.Run("auth status non-JSON human output", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{
					BitbucketURL:           "http://status.local:7990",
					BitbucketVersionTarget: "9.4",
					BitbucketToken:         "tok",
					AuthSource:             "env/default",
				}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
		})
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("auth status (non-JSON) failed: %v", err)
		}
		if !strings.Contains(out.String(), "status.local") {
			t.Fatalf("expected host in output, got: %s", out.String())
		}
	})

	t.Run("auth login JSON output", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return true },
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{BitbucketURL: "http://login-json.local:7990"}, nil
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
		})
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"login", "http://login-json.local:7990", "--token", "my-token"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("auth login (JSON) failed: %v", err)
		}
		if !strings.Contains(out.String(), "auth_mode") {
			t.Fatalf("expected auth_mode in JSON output, got: %s", out.String())
		}
	})

	t.Run("auth logout non-JSON human output", func(t *testing.T) {
		if _, err := config.SaveLogin(config.LoginInput{Host: "http://logout-human.local:7990", Token: "tok", SetDefault: false}); err != nil {
			t.Fatalf("save login for logout: %v", err)
		}

		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig: func() (config.AppConfig, error) {
				return config.LoadFromEnv()
			},
			WriteJSON: func(writer io.Writer, payload any) error {
				return jsonoutput.Write(writer, payload)
			},
		})
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"logout", "--host", "http://logout-human.local:7990"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("auth logout (non-JSON) failed: %v", err)
		}
		if !strings.Contains(out.String(), "Stored credentials removed") {
			t.Fatalf("expected removal message, got: %s", out.String())
		}
	})
}

// TestIdentityHumanSummaryNameAndSlugBranches covers the name-only and slug-only branches
// of identityHumanSummary (both skipped when DisplayName is set).
func TestIdentityHumanSummaryNameAndSlugBranches(t *testing.T) {
	t.Run("name branch when display name empty", func(t *testing.T) {
		got := identityHumanSummary(authIdentity{Name: "alice"})
		if !strings.Contains(got, "alice") {
			t.Fatalf("expected 'alice' in summary, got %q", got)
		}
	})

	t.Run("slug branch when name and display name empty", func(t *testing.T) {
		got := identityHumanSummary(authIdentity{Slug: "alice-slug"})
		if !strings.Contains(got, "alice-slug") {
			t.Fatalf("expected slug in summary, got %q", got)
		}
	})
}

func TestAuthAliasCommandsAndDiscovery(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	cloneLinks := map[string]interface{}{
		"clone": []any{
			map[string]any{"name": "ssh", "href": "ssh://git@git.company.org:7999/scm/PRJ/repo.git"},
			map[string]any{"name": "http", "href": "https://bitbucket.company.org/scm/PRJ/repo.git"},
		},
	}
	recentResponse := &openapigenerated.GetRepositoriesRecentlyAccessedResponse{
		HTTPResponse: &http.Response{StatusCode: 200},
		ApplicationjsonCharsetUTF8200: &struct {
			IsLastPage    *bool                              `json:"isLastPage,omitempty"`
			Limit         *float32                           `json:"limit,omitempty"`
			NextPageStart *int32                             `json:"nextPageStart,omitempty"`
			Size          *float32                           `json:"size,omitempty"`
			Start         *int32                             `json:"start,omitempty"`
			Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
		}{Values: &[]openapigenerated.RestRepository{{Links: &cloneLinks}}},
	}

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
		NewReposClient: func(cfg config.AppConfig) (repositoriesClient, error) {
			return &fakeReposClient{recent: recentResponse, all: recentResponseToAll(recentResponse)}, nil
		},
	})

	loginOut := &bytes.Buffer{}
	cmd.SetOut(loginOut)
	cmd.SetErr(loginOut)
	cmd.SetArgs([]string{"login", "https://bitbucket.company.org", "--token", "tok"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("login with discovery failed: %v", err)
	}
	if !strings.Contains(loginOut.String(), "aliases") {
		t.Fatalf("expected aliases in login json output, got: %s", loginOut.String())
	}
	var loginPayload struct {
		Host    string   `json:"host"`
		Aliases []string `json:"aliases"`
	}
	if err := decodeJSONEnvelopeData(loginOut.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode login json: %v", err)
	}
	if loginPayload.Aliases == nil {
		t.Fatal("expected login aliases json field to be a non-nil array")
	}

	listCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})
	listOut := &bytes.Buffer{}
	listCmd.SetOut(listOut)
	listCmd.SetErr(listOut)
	listCmd.SetArgs([]string{"alias", "list", "--host", "https://bitbucket.company.org"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("alias list failed: %v", err)
	}
	if !strings.Contains(listOut.String(), "git.company.org:7999") {
		t.Fatalf("expected discovered alias in list output, got: %s", listOut.String())
	}
	var listPayload struct {
		Host    string   `json:"host"`
		Aliases []string `json:"aliases"`
	}
	if err := decodeJSONEnvelopeData(listOut.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode alias list json: %v", err)
	}
	if listPayload.Aliases == nil {
		t.Fatal("expected alias list json aliases to be a non-nil array")
	}

	addCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})
	addOut := &bytes.Buffer{}
	addCmd.SetOut(addOut)
	addCmd.SetErr(addOut)
	addCmd.SetArgs([]string{"alias", "add", "--host", "https://bitbucket.company.org", "git.company.org:22"})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("alias add failed: %v", err)
	}
	if !strings.Contains(addOut.String(), "git.company.org:22") {
		t.Fatalf("expected added alias in output, got: %s", addOut.String())
	}

	removeCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})
	removeOut := &bytes.Buffer{}
	removeCmd.SetOut(removeOut)
	removeCmd.SetErr(removeOut)
	removeCmd.SetArgs([]string{"alias", "remove", "--host", "https://bitbucket.company.org", "git.company.org:22"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("alias remove failed: %v", err)
	}
	if strings.Contains(removeOut.String(), "git.company.org:22") && !strings.Contains(removeOut.String(), "status") {
		t.Fatalf("unexpected alias remove output: %s", removeOut.String())
	}

	discoverCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		NewReposClient: func(cfg config.AppConfig) (repositoriesClient, error) {
			return &fakeReposClient{recent: recentResponse, all: recentResponseToAll(recentResponse)}, nil
		},
	})
	discoverOut := &bytes.Buffer{}
	discoverCmd.SetOut(discoverOut)
	discoverCmd.SetErr(discoverOut)
	discoverCmd.SetArgs([]string{"alias", "discover", "--host", "https://bitbucket.company.org"})
	if err := discoverCmd.Execute(); err != nil {
		t.Fatalf("alias discover failed: %v", err)
	}
	if !strings.Contains(discoverOut.String(), "git.company.org:7999") {
		t.Fatalf("expected discovered alias output, got: %s", discoverOut.String())
	}
}

func TestAuthJSONOutputsUseEmptyAliasArrays(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})

	loginOut := &bytes.Buffer{}
	cmd.SetOut(loginOut)
	cmd.SetErr(loginOut)
	cmd.SetArgs([]string{"login", "https://empty-array.company.org", "--token", "tok", "--discover-aliases=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	var loginPayload struct {
		Aliases []string `json:"aliases"`
	}
	if err := decodeJSONEnvelopeData(loginOut.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode login payload: %v", err)
	}
	if loginPayload.Aliases == nil || len(loginPayload.Aliases) != 0 {
		t.Fatalf("expected empty login aliases array, got %+v", loginPayload.Aliases)
	}

	aliasListCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})
	aliasListOut := &bytes.Buffer{}
	aliasListCmd.SetOut(aliasListOut)
	aliasListCmd.SetErr(aliasListOut)
	aliasListCmd.SetArgs([]string{"alias", "list", "--host", "https://empty-array.company.org"})
	if err := aliasListCmd.Execute(); err != nil {
		t.Fatalf("alias list failed: %v", err)
	}
	var aliasListPayload struct {
		Aliases []string `json:"aliases"`
	}
	if err := decodeJSONEnvelopeData(aliasListOut.Bytes(), &aliasListPayload); err != nil {
		t.Fatalf("decode alias list payload: %v", err)
	}
	if aliasListPayload.Aliases == nil || len(aliasListPayload.Aliases) != 0 {
		t.Fatalf("expected empty alias list array, got %+v", aliasListPayload.Aliases)
	}

	serverListCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})
	serverListOut := &bytes.Buffer{}
	serverListCmd.SetOut(serverListOut)
	serverListCmd.SetErr(serverListOut)
	serverListCmd.SetArgs([]string{"server", "list"})
	if err := serverListCmd.Execute(); err != nil {
		t.Fatalf("server list failed: %v", err)
	}
	var serverListPayload struct {
		Servers []struct {
			Aliases []string `json:"aliases"`
		} `json:"servers"`
	}
	if err := decodeJSONEnvelopeData(serverListOut.Bytes(), &serverListPayload); err != nil {
		t.Fatalf("decode server list payload: %v", err)
	}
	if len(serverListPayload.Servers) != 1 {
		t.Fatalf("expected one server, got %d", len(serverListPayload.Servers))
	}
	if serverListPayload.Servers[0].Aliases == nil || len(serverListPayload.Servers[0].Aliases) != 0 {
		t.Fatalf("expected empty server aliases array, got %+v", serverListPayload.Servers[0].Aliases)
	}

	removeCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})
	removeOut := &bytes.Buffer{}
	removeCmd.SetOut(removeOut)
	removeCmd.SetErr(removeOut)
	removeCmd.SetArgs([]string{"alias", "add", "--host", "https://empty-array.company.org", "git.company.org:22"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("alias add failed: %v", err)
	}

	removeCmd = New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})
	removeOut = &bytes.Buffer{}
	removeCmd.SetOut(removeOut)
	removeCmd.SetErr(removeOut)
	removeCmd.SetArgs([]string{"alias", "remove", "--host", "https://empty-array.company.org", "git.company.org:22"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("alias remove failed: %v", err)
	}
	var removePayload struct {
		Aliases []string `json:"aliases"`
	}
	if err := decodeJSONEnvelopeData(removeOut.Bytes(), &removePayload); err != nil {
		t.Fatalf("decode alias remove payload: %v", err)
	}
	if removePayload.Aliases == nil || len(removePayload.Aliases) != 0 {
		t.Fatalf("expected empty alias remove array, got %+v", removePayload.Aliases)
	}

	discoverCmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		NewReposClient: func(cfg config.AppConfig) (repositoriesClient, error) {
			recent := &openapigenerated.GetRepositoriesRecentlyAccessedResponse{
				HTTPResponse: &http.Response{StatusCode: 200},
				ApplicationjsonCharsetUTF8200: &struct {
					IsLastPage    *bool                              `json:"isLastPage,omitempty"`
					Limit         *float32                           `json:"limit,omitempty"`
					NextPageStart *int32                             `json:"nextPageStart,omitempty"`
					Size          *float32                           `json:"size,omitempty"`
					Start         *int32                             `json:"start,omitempty"`
					Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
				}{Values: &[]openapigenerated.RestRepository{}},
			}
			return &fakeReposClient{recent: recent, all: recentResponseToAll(recent)}, nil
		},
	})
	discoverOut := &bytes.Buffer{}
	discoverCmd.SetOut(discoverOut)
	discoverCmd.SetErr(discoverOut)
	discoverCmd.SetArgs([]string{"alias", "discover", "--host", "https://empty-array.company.org"})
	if err := discoverCmd.Execute(); err != nil {
		t.Fatalf("alias discover failed: %v", err)
	}
	var discoverPayload struct {
		Aliases []string `json:"aliases"`
	}
	if err := decodeJSONEnvelopeData(discoverOut.Bytes(), &discoverPayload); err != nil {
		t.Fatalf("decode alias discover payload: %v", err)
	}
	if discoverPayload.Aliases == nil || len(discoverPayload.Aliases) != 0 {
		t.Fatalf("expected empty alias discover array, got %+v", discoverPayload.Aliases)
	}
}

func TestDiscoverAliasesEdgeCases(t *testing.T) {
	t.Run("deduplicates duplicate clone aliases", func(t *testing.T) {
		cloneLinks := map[string]interface{}{
			"clone": []any{
				map[string]any{"name": "ssh", "href": "ssh://git@git.company.org:7999/scm/PRJ/repo.git"},
				map[string]any{"name": "ssh", "href": "git@git.company.org:scm/PRJ/repo.git"},
				map[string]any{"name": "ssh", "href": "ssh://git@git.company.org:7999/scm/PRJ/repo.git"},
			},
		}
		aliases := extractRepositoryCloneAliases(openapigenerated.RestRepository{Links: &cloneLinks})
		if len(aliases) != 2 {
			t.Fatalf("expected two normalized aliases, got %+v", aliases)
		}
	})

	t.Run("returns no aliases when clone links missing", func(t *testing.T) {
		aliases := extractRepositoryCloneAliases(openapigenerated.RestRepository{})
		if len(aliases) != 0 {
			t.Fatalf("expected no aliases, got %+v", aliases)
		}
	})

	t.Run("discover handles empty repository pages", func(t *testing.T) {
		recent := &openapigenerated.GetRepositoriesRecentlyAccessedResponse{
			HTTPResponse: &http.Response{StatusCode: 200},
			ApplicationjsonCharsetUTF8200: &struct {
				IsLastPage    *bool                              `json:"isLastPage,omitempty"`
				Limit         *float32                           `json:"limit,omitempty"`
				NextPageStart *int32                             `json:"nextPageStart,omitempty"`
				Size          *float32                           `json:"size,omitempty"`
				Start         *int32                             `json:"start,omitempty"`
				Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
			}{Values: &[]openapigenerated.RestRepository{}},
		}
		aliases, err := discoverAliases(context.Background(), config.AppConfig{BitbucketURL: "https://bitbucket.company.org", BitbucketToken: "tok"}, func(cfg config.AppConfig) (repositoriesClient, error) {
			return &fakeReposClient{recent: recent, all: recentResponseToAll(recent)}, nil
		})
		if err != nil {
			t.Fatalf("discover aliases failed: %v", err)
		}
		if len(aliases) != 0 {
			t.Fatalf("expected no aliases, got %+v", aliases)
		}
	})

	t.Run("falls back from recent to all repositories", func(t *testing.T) {
		recent := &openapigenerated.GetRepositoriesRecentlyAccessedResponse{
			HTTPResponse: &http.Response{StatusCode: 200},
			ApplicationjsonCharsetUTF8200: &struct {
				IsLastPage    *bool                              `json:"isLastPage,omitempty"`
				Limit         *float32                           `json:"limit,omitempty"`
				NextPageStart *int32                             `json:"nextPageStart,omitempty"`
				Size          *float32                           `json:"size,omitempty"`
				Start         *int32                             `json:"start,omitempty"`
				Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
			}{Values: &[]openapigenerated.RestRepository{}},
		}
		cloneLinks := map[string]interface{}{
			"clone": []any{map[string]any{"name": "ssh", "href": "ssh://git@git.company.org:7999/scm/PRJ/repo.git"}},
		}
		all := &openapigenerated.GetRepositories1Response{
			HTTPResponse: &http.Response{StatusCode: 200},
			ApplicationjsonCharsetUTF8200: &struct {
				IsLastPage    *bool                              `json:"isLastPage,omitempty"`
				Limit         *float32                           `json:"limit,omitempty"`
				NextPageStart *int32                             `json:"nextPageStart,omitempty"`
				Size          *float32                           `json:"size,omitempty"`
				Start         *int32                             `json:"start,omitempty"`
				Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
			}{Values: &[]openapigenerated.RestRepository{{Links: &cloneLinks}}},
		}
		aliases, err := discoverAliases(context.Background(), config.AppConfig{BitbucketURL: "https://bitbucket.company.org", BitbucketToken: "tok"}, func(cfg config.AppConfig) (repositoriesClient, error) {
			return &fakeReposClient{recent: recent, all: all}, nil
		})
		if err != nil {
			t.Fatalf("discover aliases fallback failed: %v", err)
		}
		if len(aliases) != 1 || aliases[0] != "git.company.org:7999" {
			t.Fatalf("unexpected aliases from fallback: %+v", aliases)
		}
	})

	t.Run("surfaces repository page status errors", func(t *testing.T) {
		recent := &openapigenerated.GetRepositoriesRecentlyAccessedResponse{HTTPResponse: &http.Response{StatusCode: 403}, Body: []byte("forbidden")}
		if _, err := discoverAliases(context.Background(), config.AppConfig{BitbucketURL: "https://bitbucket.company.org", BitbucketToken: "tok"}, func(cfg config.AppConfig) (repositoriesClient, error) {
			return &fakeReposClient{recent: recent}, nil
		}); err == nil {
			t.Fatal("expected discovery status error")
		}
	})

	t.Run("surfaces client initialization errors", func(t *testing.T) {
		if _, err := discoverAliases(context.Background(), config.AppConfig{BitbucketURL: "https://bitbucket.company.org", BitbucketToken: "tok"}, func(cfg config.AppConfig) (repositoriesClient, error) {
			return nil, errors.New("boom")
		}); err == nil {
			t.Fatal("expected client initialization error")
		}
	})

	t.Run("normalize clone endpoint rejects invalid values", func(t *testing.T) {
		if _, err := normalizeCloneEndpoint("://bad"); err == nil {
			t.Fatal("expected invalid clone endpoint error")
		}
	})

	t.Run("repository page status helper rejects non success and nil payload", func(t *testing.T) {
		if _, _, err := discoverAliasesFromRepositoryPage(403, []byte("forbidden"), nil); err == nil {
			t.Fatal("expected page status error")
		}
		empty := &struct {
			IsLastPage    *bool                              `json:"isLastPage,omitempty"`
			Limit         *float32                           `json:"limit,omitempty"`
			NextPageStart *int32                             `json:"nextPageStart,omitempty"`
			Size          *float32                           `json:"size,omitempty"`
			Start         *int32                             `json:"start,omitempty"`
			Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
		}{}
		aliases, found, err := discoverAliasesFromRepositoryPage(200, nil, empty)
		if err != nil || found || len(aliases) != 0 {
			t.Fatalf("expected empty page result, got aliases=%+v found=%v err=%v", aliases, found, err)
		}
	})
}

func TestAuthAliasHelperBranches(t *testing.T) {
	t.Run("extract repository clone aliases skips invalid entries", func(t *testing.T) {
		cloneLinks := map[string]interface{}{
			"clone": []any{
				"not-a-map",
				map[string]any{"name": "http", "href": "https://bitbucket.company.org/scm/PRJ/repo.git"},
				map[string]any{"name": "ssh", "href": ""},
				map[string]any{"name": "ssh", "href": "://bad"},
			},
		}
		aliases := extractRepositoryCloneAliases(openapigenerated.RestRepository{Links: &cloneLinks})
		if len(aliases) != 0 {
			t.Fatalf("expected invalid clone entries to be skipped, got %+v", aliases)
		}
	})

	t.Run("normalize clone endpoint covers common schemes", func(t *testing.T) {
		cases := map[string]string{
			"git@git.company.org:scm/PRJ/repo.git":            "git.company.org:22",
			"ssh://git@git.company.org/scm/PRJ/repo.git":      "git.company.org:22",
			"ssh://git@git.company.org:7999/scm/PRJ/repo.git": "git.company.org:7999",
			"https://bitbucket.company.org/scm/PRJ/repo.git":  "bitbucket.company.org:443",
			"http://bitbucket.company.org/scm/PRJ/repo.git":   "bitbucket.company.org:80",
		}
		for input, want := range cases {
			got, err := normalizeCloneEndpoint(input)
			if err != nil {
				t.Fatalf("normalizeCloneEndpoint(%q) returned error: %v", input, err)
			}
			if got != want {
				t.Fatalf("normalizeCloneEndpoint(%q) = %q, want %q", input, got, want)
			}
		}
		if _, err := normalizeCloneEndpoint(""); err == nil {
			t.Fatal("expected blank clone endpoint error")
		}
	})

	t.Run("safe bool handles nil and true", func(t *testing.T) {
		if safeBool(nil) {
			t.Fatal("expected nil bool pointer to be false")
		}
		value := true
		if !safeBool(&value) {
			t.Fatal("expected true bool pointer to be true")
		}
	})
}

func TestAuthAliasHumanAndErrorBranches(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := config.SaveLogin(config.LoginInput{Host: "https://bitbucket.company.org", Token: "tok", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}

	t.Run("login without discovery has no alias line", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		})
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"login", "https://nodiscover.company.org", "--token", "tok", "--discover-aliases=false"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("login without discovery failed: %v", err)
		}
		if strings.Contains(out.String(), "Discovered aliases:") {
			t.Fatalf("did not expect discovered aliases line, got: %s", out.String())
		}
	})

	t.Run("alias list human empty message", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		})
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"alias", "list", "--host", "https://bitbucket.company.org"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("alias list failed: %v", err)
		}
		if !strings.Contains(out.String(), "No aliases configured") {
			t.Fatalf("expected empty alias message, got: %s", out.String())
		}
	})

	t.Run("alias discover human empty message", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
			NewReposClient: func(cfg config.AppConfig) (repositoriesClient, error) {
				recent := &openapigenerated.GetRepositoriesRecentlyAccessedResponse{
					HTTPResponse: &http.Response{StatusCode: 200},
					ApplicationjsonCharsetUTF8200: &struct {
						IsLastPage    *bool                              `json:"isLastPage,omitempty"`
						Limit         *float32                           `json:"limit,omitempty"`
						NextPageStart *int32                             `json:"nextPageStart,omitempty"`
						Size          *float32                           `json:"size,omitempty"`
						Start         *int32                             `json:"start,omitempty"`
						Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
					}{Values: &[]openapigenerated.RestRepository{}},
				}
				return &fakeReposClient{recent: recent, all: recentResponseToAll(recent)}, nil
			},
		})
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"alias", "discover", "--host", "https://bitbucket.company.org"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("alias discover failed: %v", err)
		}
		if !strings.Contains(out.String(), "No aliases discovered") {
			t.Fatalf("expected no aliases discovered message, got: %s", out.String())
		}
	})

	t.Run("alias remove human no aliases remain", func(t *testing.T) {
		if _, err := config.SetHostAliases("https://bitbucket.company.org", []string{"git.company.org:22"}); err != nil {
			t.Fatalf("set aliases failed: %v", err)
		}
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		})
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"alias", "remove", "--host", "https://bitbucket.company.org", "git.company.org:22"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("alias remove failed: %v", err)
		}
		if !strings.Contains(out.String(), "no aliases remain") {
			t.Fatalf("expected no aliases remain message, got: %s", out.String())
		}
	})

	t.Run("alias command errors for unknown host", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"alias", "list", "--host", "https://missing.company.org"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected alias list error for unknown host")
		}
	})

	t.Run("human command branches with discovered aliases", func(t *testing.T) {
		cloneLinks := map[string]interface{}{
			"clone": []any{map[string]any{"name": "ssh", "href": "ssh://git@git.company.org:7999/scm/PRJ/repo.git"}},
		}
		recent := &openapigenerated.GetRepositoriesRecentlyAccessedResponse{
			HTTPResponse: &http.Response{StatusCode: 200},
			ApplicationjsonCharsetUTF8200: &struct {
				IsLastPage    *bool                              `json:"isLastPage,omitempty"`
				Limit         *float32                           `json:"limit,omitempty"`
				NextPageStart *int32                             `json:"nextPageStart,omitempty"`
				Size          *float32                           `json:"size,omitempty"`
				Start         *int32                             `json:"start,omitempty"`
				Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
			}{Values: &[]openapigenerated.RestRepository{{Links: &cloneLinks}}},
		}

		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
			NewReposClient: func(cfg config.AppConfig) (repositoriesClient, error) {
				return &fakeReposClient{recent: recent, all: recentResponseToAll(recent)}, nil
			},
		})

		loginOut := &bytes.Buffer{}
		cmd.SetOut(loginOut)
		cmd.SetErr(loginOut)
		cmd.SetArgs([]string{"login", "https://human.company.org", "--token", "tok"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("human login failed: %v", err)
		}
		if !strings.Contains(loginOut.String(), "Discovered aliases: git.company.org:7999") {
			t.Fatalf("expected discovered aliases in human login output, got: %s", loginOut.String())
		}

		listCmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		})
		listOut := &bytes.Buffer{}
		listCmd.SetOut(listOut)
		listCmd.SetErr(listOut)
		listCmd.SetArgs([]string{"alias", "list", "--host", "https://human.company.org"})
		if err := listCmd.Execute(); err != nil {
			t.Fatalf("human alias list failed: %v", err)
		}
		if !strings.Contains(listOut.String(), "Aliases for https://human.company.org") || !strings.Contains(listOut.String(), "git.company.org:7999") {
			t.Fatalf("expected populated alias list output, got: %s", listOut.String())
		}

		addCmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		})
		addOut := &bytes.Buffer{}
		addCmd.SetOut(addOut)
		addCmd.SetErr(addOut)
		addCmd.SetArgs([]string{"alias", "add", "--host", "https://human.company.org", "git.company.org:22"})
		if err := addCmd.Execute(); err != nil {
			t.Fatalf("human alias add failed: %v", err)
		}
		if !strings.Contains(addOut.String(), "Aliases updated:") {
			t.Fatalf("expected alias add human output, got: %s", addOut.String())
		}

		removeCmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
		})
		removeOut := &bytes.Buffer{}
		removeCmd.SetOut(removeOut)
		removeCmd.SetErr(removeOut)
		removeCmd.SetArgs([]string{"alias", "remove", "--host", "https://human.company.org", "git.company.org:22"})
		if err := removeCmd.Execute(); err != nil {
			t.Fatalf("human alias remove failed: %v", err)
		}
		if !strings.Contains(removeOut.String(), "Remaining aliases:") {
			t.Fatalf("expected alias remove remaining output, got: %s", removeOut.String())
		}

		discoverCmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
			NewReposClient: func(cfg config.AppConfig) (repositoriesClient, error) {
				return &fakeReposClient{recent: recent, all: recentResponseToAll(recent)}, nil
			},
		})
		discoverOut := &bytes.Buffer{}
		discoverCmd.SetOut(discoverOut)
		discoverCmd.SetErr(discoverOut)
		discoverCmd.SetArgs([]string{"alias", "discover", "--host", "https://human.company.org"})
		if err := discoverCmd.Execute(); err != nil {
			t.Fatalf("human alias discover failed: %v", err)
		}
		if !strings.Contains(discoverOut.String(), "Discovered aliases for https://human.company.org") {
			t.Fatalf("expected human alias discover output, got: %s", discoverOut.String())
		}
	})

	t.Run("login ignores discovery failure and still stores credentials", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
			LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
			WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
			NewReposClient: func(cfg config.AppConfig) (repositoriesClient, error) {
				return nil, errors.New("boom")
			},
		})
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs([]string{"login", "https://ignore-discovery.company.org", "--token", "tok"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("login should succeed when discovery fails: %v", err)
		}
		if strings.Contains(out.String(), "Discovered aliases:") {
			t.Fatalf("did not expect alias output when discovery fails, got: %s", out.String())
		}
	})
}

func recentResponseToAll(response *openapigenerated.GetRepositoriesRecentlyAccessedResponse) *openapigenerated.GetRepositories1Response {
	if response == nil {
		return nil
	}
	return &openapigenerated.GetRepositories1Response{
		HTTPResponse:                  response.HTTPResponse,
		ApplicationjsonCharsetUTF8200: response.ApplicationjsonCharsetUTF8200,
	}
}
