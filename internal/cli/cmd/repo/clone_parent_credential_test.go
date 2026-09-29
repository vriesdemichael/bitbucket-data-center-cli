package repocmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// authorizationBeacon is a server that is not Bitbucket: it answers every
// request alike and records the Authorization it was sent.
type authorizationBeacon struct {
	*httptest.Server

	mu   sync.Mutex
	seen []string
}

// mock-inventory: routing-beacon — the reply is never read as Bitbucket's; the subject is which credential a request to this host carried.
func newAuthorizationBeacon(t *testing.T) *authorizationBeacon {
	t.Helper()

	beacon := &authorizationBeacon{}
	beacon.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beacon.mu.Lock()
		beacon.seen = append(beacon.seen, r.Header.Get("Authorization"))
		beacon.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(beacon.Close)

	return beacon
}

func (beacon *authorizationBeacon) authorizations() []string {
	beacon.mu.Lock()
	defer beacon.mu.Unlock()

	return append([]string(nil), beacon.seen...)
}

// After a clone, bb asks the clone host whether the repository is a fork. That
// request carries the credential git is given for the clone host, and no
// other: the configuration's own is the default host's, and a clone URL on
// another server used to take it there.
func TestCloneParentLookupCarriesOnlyTheCloneHostsCredential(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "bb", "config.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	defaultHost := newAuthorizationBeacon(t)
	unknown := newAuthorizationBeacon(t)
	stored := newAuthorizationBeacon(t)
	if _, err := config.SaveLogin(config.LoginInput{Host: stored.URL, Token: "stored-token", SetDefault: false}); err != nil {
		t.Fatalf("save login: %v", err)
	}

	cfg := config.AppConfig{BitbucketURL: defaultHost.URL, BitbucketToken: "default-token", RequestTimeout: 5 * time.Second}
	repo := cloneRepoRef{ProjectKey: "PRJ", Slug: "demo"}

	for host, want := range map[*authorizationBeacon]string{
		unknown:     "",
		stored:      "Bearer stored-token",
		defaultHost: "Bearer default-token",
	} {
		if _, _, err := lookupParentCloneURL(context.Background(), cfg, host.URL, repo); err != nil {
			t.Fatalf("parent lookup on %s: %v", host.URL, err)
		}
		if seen := host.authorizations(); len(seen) != 1 || seen[0] != want {
			t.Errorf("the parent lookup on %s carried %q, want one request with %q", host.URL, seen, want)
		}
	}
}
