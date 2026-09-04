package repository

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func TestListRepositoriesAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"Authentication required"}]}`)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	client := httpclient.NewFromConfig(cfg)
	service := NewService(client)

	_, err = service.List(context.Background(), ListOptions{MaxResults: 10})
	if err == nil {
		t.Fatal("expected auth error")
	}

	if apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected auth exit code 3, got %d (%v)", apperrors.ExitCode(err), err)
	}
}
