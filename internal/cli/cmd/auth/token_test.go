package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
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
			Name:         &name,
			Slug:         &m.userSlug,
			DisplayName:  &name,
			Active:       &active,
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
			Name:         &name,
			Slug:         &m.userSlug,
			DisplayName:  &name,
			Active:       &active,
		},
	}, nil
}

func TestAuthTokenCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/users/alice":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-1","name":"UserToken"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-2","name":"ProjToken"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/repos/repo1":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-3","name":"RepoToken"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserToken"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/access-tokens/latest/users/alice":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserToken","token":"secret-token-123"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserTokenUpdated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var jsonFlag bool
	deps := Dependencies{
		JSONEnabled: func() bool { return jsonFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL: server.URL,
			}, nil
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
		NewUsersClient: func(cfg config.AppConfig) (usersClient, error) {
			return &mockUsersClient{userSlug: "alice"}, nil
		},
	}

	execute := func(args ...string) (string, error) {
		cmd := New(deps)
		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return buffer.String(), err
	}

	// 1. List user, project, repo
	out, err := execute("token", "list", "--user", "alice")
	if err != nil {
		t.Fatalf("list user failed: %v", err)
	}
	if !strings.Contains(out, "tok-1") || !strings.Contains(out, "UserToken") {
		t.Fatalf("unexpected list user output: %s", out)
	}

	out, err = execute("token", "list", "--project", "PRJ")
	if err != nil {
		t.Fatalf("list project failed: %v", err)
	}
	if !strings.Contains(out, "tok-2") || !strings.Contains(out, "ProjToken") {
		t.Fatalf("unexpected list project output: %s", out)
	}

	out, err = execute("token", "list", "--repo", "PRJ/repo1")
	if err != nil {
		t.Fatalf("list repo failed: %v", err)
	}
	if !strings.Contains(out, "tok-3") || !strings.Contains(out, "RepoToken") {
		t.Fatalf("unexpected list repo output: %s", out)
	}

	// 2. Resolve default user identity slug (no --user flag passed)
	out, err = execute("token", "list")
	if err != nil {
		t.Fatalf("list default user failed: %v", err)
	}
	if !strings.Contains(out, "tok-1") {
		t.Fatalf("expected resolved default user output, got: %s", out)
	}

	// 3. Get token
	out, err = execute("token", "get", "tok-1", "--user", "alice")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !strings.Contains(out, "tok-1") || !strings.Contains(out, "UserToken") {
		t.Fatalf("unexpected get output: %s", out)
	}

	// 4. Create token
	out, err = execute("token", "create", "UserToken", "--user", "alice", "--permission", "PROJECT_READ", "--expiry-days", "30")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !strings.Contains(out, "tok-1") || !strings.Contains(out, "secret-token-123") {
		t.Fatalf("unexpected create output: %s", out)
	}

	// 5. Update token
	out, err = execute("token", "update", "tok-1", "--user", "alice", "--name", "UserTokenUpdated")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(out, "updated successfully") {
		t.Fatalf("unexpected update output: %s", out)
	}

	// 6. Revoke token
	out, err = execute("token", "revoke", "tok-1", "--user", "alice")
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}
	if !strings.Contains(out, "revoked successfully") {
		t.Fatalf("unexpected revoke output: %s", out)
	}

	// 7. JSON output tests
	jsonFlag = true
	decodeJSONData := func(t *testing.T, rawJSON string, target any) {
		t.Helper()
		var env struct {
			Version string          `json:"version"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(rawJSON), &env); err != nil {
			t.Fatalf("failed to decode envelope: %v (raw: %s)", err, rawJSON)
		}
		if err := json.Unmarshal(env.Data, target); err != nil {
			t.Fatalf("failed to decode data: %v (data: %s)", err, string(env.Data))
		}
	}

	out, err = execute("token", "list", "--user", "alice")
	if err != nil {
		t.Fatalf("json list failed: %v", err)
	}
	var listParsed []map[string]any
	decodeJSONData(t, out, &listParsed)
	if len(listParsed) != 1 {
		t.Fatalf("invalid json list length, got %d", len(listParsed))
	}

	out, err = execute("token", "get", "tok-1", "--user", "alice")
	if err != nil {
		t.Fatalf("json get failed: %v", err)
	}
	var getParsed map[string]any
	decodeJSONData(t, out, &getParsed)
	if getParsed["id"] != "tok-1" {
		t.Fatalf("invalid json get output: %s", out)
	}

	out, err = execute("token", "create", "UserToken", "--user", "alice")
	if err != nil {
		t.Fatalf("json create failed: %v", err)
	}
	var createParsed map[string]any
	decodeJSONData(t, out, &createParsed)
	if createParsed["id"] != "tok-1" {
		t.Fatalf("invalid json create output: %s", out)
	}

	out, err = execute("token", "update", "tok-1", "--user", "alice", "--name", "UserTokenUpdated")
	if err != nil {
		t.Fatalf("json update failed: %v", err)
	}
	var updateParsed map[string]any
	decodeJSONData(t, out, &updateParsed)
	if updateParsed["name"] != "UserTokenUpdated" {
		t.Fatalf("invalid json update output: %s", out)
	}

	out, err = execute("token", "revoke", "tok-1", "--user", "alice")
	if err != nil {
		t.Fatalf("json revoke failed: %v", err)
	}
	var revokeParsed map[string]any
	decodeJSONData(t, out, &revokeParsed)
	if revokeParsed["status"] != "ok" {
		t.Fatalf("invalid json revoke output: %s", out)
	}

	// 8. Errors / validation checks
	jsonFlag = false
	// More than one scope specified
	_, err = execute("token", "list", "--user", "alice", "--project", "PRJ")
	if err == nil || !strings.Contains(err.Error(), "only one of --user, --project, or --repo") {
		t.Fatalf("expected multi-scope error, got: %v", err)
	}

	// Missing token name for create
	_, err = execute("token", "create")
	if err == nil || !strings.Contains(err.Error(), "token name is required") {
		t.Fatalf("expected token name validation error, got: %v", err)
	}
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
