package httpclient

import (
	"context"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
)

func TestExternalNetworkBlocking(t *testing.T) {
	t.Setenv("BB_BLOCK_EXTERNAL_NETWORK", "1")
	client := NewFromConfig(config.AppConfig{BitbucketURL: "http://external.invalid"})

	err := client.GetJSON(context.Background(), "/any", nil, nil)
	if err == nil {
		t.Fatal("expected error for external network access, got nil")
	}
	if !strings.Contains(err.Error(), "external network access is disabled") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
