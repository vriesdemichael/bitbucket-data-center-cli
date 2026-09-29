package network_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/network"
)

// Both clients bb talks to Bitbucket through -- the hand-written one and the
// one generated from the OpenAPI description -- send bb's User-Agent, because
// both are built on the shared transport. They used to send Go's default.
//
// mock-inventory: routing-beacon — the reply is never read as Bitbucket's; the subject is the User-Agent each request arrived with.
func TestBothBitbucketClientsNameBB(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	beacon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("User-Agent"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer beacon.Close()

	cfg := config.AppConfig{BitbucketURL: beacon.URL, RequestTimeout: 5 * time.Second}

	if _, err := httpclient.NewFromConfig(cfg).DoRequest(context.Background(), httpclient.RequestOptions{Path: "/rest/api/1.0/projects"}); err != nil {
		t.Fatalf("hand-written client: %v", err)
	}
	generated, err := openapi.NewClientWithResponsesFromConfig(cfg)
	if err != nil {
		t.Fatalf("generated client: %v", err)
	}
	if _, err := generated.GetProjectWithResponse(context.Background(), "PROJ"); err != nil {
		t.Fatalf("generated client request: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("expected one request from each client, got %d", len(seen))
	}
	for _, userAgent := range seen {
		if userAgent != network.UserAgent() || !strings.HasPrefix(userAgent, "bb/") {
			t.Errorf("a request arrived with User-Agent %q, want %q", userAgent, network.UserAgent())
		}
	}
}
