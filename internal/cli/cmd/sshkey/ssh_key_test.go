package sshkeycmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestSSHKeyCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/ssh/latest/keys":
			_, _ = w.Write([]byte(`{"values":[{"id":12,"label":"my-key","fingerprint":"SHA256:abc"}]}`))

		case r.Method == http.MethodPost && path == "/rest/ssh/latest/keys":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":13,"label":"new-key"}`))

		case r.Method == http.MethodDelete && path == "/rest/ssh/latest/keys/12":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL: server.URL,
	}

	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
	}

	// list
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(buf.String(), "my-key") {
		t.Fatalf("expected my-key in list output: %s", buf.String())
	}

	// add
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"add", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC dummy", "--label", "new-key"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on add: %v", err)
	}
	if !strings.Contains(buf.String(), "added successfully") {
		t.Fatalf("expected added successfully in add output: %s", buf.String())
	}

	// remove
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"remove", "12"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on remove: %v", err)
	}
	if !strings.Contains(buf.String(), "removed successfully") {
		t.Fatalf("expected removed successfully in remove output: %s", buf.String())
	}
}
