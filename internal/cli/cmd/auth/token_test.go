package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type mockUsersClient struct {
	userSlug string
	err      error
	status   int
}

func (m *mockUsersClient) GetUsers2WithResponse(ctx context.Context, params *openapigenerated.GetUsers2Params, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetUsers2Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.status != 0 && (m.status < 200 || m.status >= 300) {
		return &openapigenerated.GetUsers2Response{
			HTTPResponse: &http.Response{StatusCode: m.status},
		}, nil
	}
	name := "Alice"
	active := true
	resp := &openapigenerated.GetUsers2Response{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		ApplicationjsonCharsetUTF8200: &openapigenerated.RestApplicationUser{
			Name:        &name,
			Slug:        &m.userSlug,
			DisplayName: &name,
			Active:      &active,
		},
	}
	return resp, nil
}

func (m *mockUsersClient) GetUserWithResponse(ctx context.Context, userSlug string, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetUserResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.status != 0 && (m.status < 200 || m.status >= 300) {
		return &openapigenerated.GetUserResponse{
			HTTPResponse: &http.Response{StatusCode: m.status},
		}, nil
	}
	name := "Alice"
	active := true
	return &openapigenerated.GetUserResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		ApplicationjsonCharsetUTF8200: &openapigenerated.RestApplicationUser{
			Name:        &name,
			Slug:        &m.userSlug,
			DisplayName: &name,
			Active:      &active,
		},
	}, nil
}

func TestAuthTokenCommandsErrors(t *testing.T) {
	// 1. LoadConfig error
	depsErrConfig := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{}, fmt.Errorf("simulated config error")
		},
	}
	cmd1 := New(depsErrConfig)
	cmd1.SetArgs([]string{"token", "list"})
	if err := cmd1.Execute(); err == nil || !strings.Contains(err.Error(), "simulated config error") {
		t.Fatalf("expected config error, got: %v", err)
	}

	// 2. Client init error (CAFile doesn't exist)
	depsErrClient := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL: "http://localhost",
				CAFile:       "/nonexistent/ca/path/for/test",
			}, nil
		},
	}
	cmd2 := New(depsErrClient)
	cmd2.SetArgs([]string{"token", "list"})
	if err := cmd2.Execute(); err == nil || (!strings.Contains(err.Error(), "read CA bundle") && !strings.Contains(err.Error(), "cannot find")) {
		t.Fatalf("expected client init error, got: %v", err)
	}

	// 3. NewUsersClient error
	depsErrUsers := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL: "http://localhost",
			}, nil
		},
		NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
			return nil, fmt.Errorf("simulated users client error")
		},
	}
	cmd3 := New(depsErrUsers)
	cmd3.SetArgs([]string{"token", "list"})
	if err := cmd3.Execute(); err == nil || !strings.Contains(err.Error(), "simulated users client error") {
		t.Fatalf("expected users client error, got: %v", err)
	}

	// 4. API identity lookup error (GetUsers2WithResponse fails)
	depsErrAPI := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL: "http://localhost",
			}, nil
		},
		NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
			return &mockUsersClient{err: fmt.Errorf("api failure")}, nil
		},
	}
	cmd4 := New(depsErrAPI)
	cmd4.SetArgs([]string{"token", "list"})
	if err := cmd4.Execute(); err == nil || !strings.Contains(err.Error(), "api failure") {
		t.Fatalf("expected api lookup failure, got: %v", err)
	}

	// 5. API identity lookup non-200 status code
	depsErrStatus := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL: "http://localhost",
			}, nil
		},
		NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
			return &mockUsersClient{status: 500}, nil
		},
	}
	cmd5 := New(depsErrStatus)
	cmd5.SetArgs([]string{"token", "list"})
	if err := cmd5.Execute(); err == nil || !strings.Contains(err.Error(), "failed to resolve current user slug") {
		t.Fatalf("expected status error to fail user slug resolution, got: %v", err)
	}
}
