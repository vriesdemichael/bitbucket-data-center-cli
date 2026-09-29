package repocmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// authorizationBeacon is a server that is not Bitbucket: it answers every
// request alike and records the Authorization it was sent, and the name on the
// client certificate it was shown.
type authorizationBeacon struct {
	*httptest.Server

	mu           sync.Mutex
	seen         []string
	certificates []string
}

// mock-inventory: routing-beacon — the reply is never read as Bitbucket's; the subject is which credential a request to this host carried.
func newAuthorizationBeacon(t *testing.T) *authorizationBeacon {
	t.Helper()

	beacon := &authorizationBeacon{}
	beacon.Server = httptest.NewServer(beacon.record())
	t.Cleanup(beacon.Close)

	return beacon
}

// mock-inventory: routing-beacon — the reply is never read as Bitbucket's; the subject is which client certificate a request to this host presented.
func newCertificateBeacon(t *testing.T) *authorizationBeacon {
	t.Helper()

	beacon := &authorizationBeacon{}
	beacon.Server = httptest.NewUnstartedServer(beacon.record())
	beacon.TLS = &tls.Config{ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS12}
	beacon.Config.ErrorLog = log.New(io.Discard, "", 0)
	beacon.StartTLS()
	t.Cleanup(beacon.Close)

	return beacon
}

func (beacon *authorizationBeacon) record() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		certificate := ""
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			certificate = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		beacon.mu.Lock()
		beacon.seen = append(beacon.seen, r.Header.Get("Authorization"))
		beacon.certificates = append(beacon.certificates, certificate)
		beacon.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
}

func (beacon *authorizationBeacon) authorizations() []string {
	beacon.mu.Lock()
	defer beacon.mu.Unlock()

	return append([]string(nil), beacon.seen...)
}

func (beacon *authorizationBeacon) presented() []string {
	beacon.mu.Lock()
	defer beacon.mu.Unlock()

	return append([]string(nil), beacon.certificates...)
}

// withNoOtherConfiguration keeps this machine's own configuration out of a
// load: no stored file, no policy or workspace file, and nothing in the
// environment that names a host or a credential.
func withNoOtherConfiguration(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"BITBUCKET_URL", "BITBUCKET_TOKEN", "BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD",
		"ADMIN_USER", "ADMIN_PASSWORD", "BB_REQUIRE_KEYRING", "BB_DISABLE_STORED_CONFIG",
		"BB_CA_FILE", "BB_CLIENT_CERT", "BB_CLIENT_KEY", "BB_INSECURE_SKIP_VERIFY",
	} {
		t.Setenv(key, "")
	}
	absent := t.TempDir()
	t.Setenv("BB_CONFIG_PATH", filepath.Join(absent, "bb", "config.yaml"))
	t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(absent, "policy.yaml"))
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(absent, "workspace.yaml"))
}

// After a clone, bb asks the clone host whether the repository is a fork. That
// request carries the credential git is given for the clone host, and no
// other: the configuration's own is the default host's, and a clone URL on
// another server used to take it there.
func TestCloneParentLookupCarriesOnlyTheCloneHostsCredential(t *testing.T) {
	withNoOtherConfiguration(t)

	defaultHost := newAuthorizationBeacon(t)
	unknown := newAuthorizationBeacon(t)
	stored := newAuthorizationBeacon(t)
	if _, err := config.SaveLogin(config.LoginInput{Host: stored.URL, Token: "stored-token", SetDefault: false}); err != nil {
		t.Fatalf("save login: %v", err)
	}

	deps := (&Dependencies{}).withDefaults()
	cfg := config.AppConfig{BitbucketURL: defaultHost.URL, BitbucketToken: "default-token", RequestTimeout: 5 * time.Second}
	repo := cloneRepoRef{ProjectKey: "PRJ", Slug: "demo"}

	for host, want := range map[*authorizationBeacon]string{
		unknown:     "",
		stored:      "Bearer stored-token",
		defaultHost: "Bearer default-token",
	} {
		if _, _, err := lookupParentCloneURL(context.Background(), deps, cfg, host.URL, repo); err != nil {
			t.Fatalf("parent lookup on %s: %v", host.URL, err)
		}
		if seen := host.authorizations(); len(seen) != 1 || seen[0] != want {
			t.Errorf("the parent lookup on %s carried %q, want one request with %q", host.URL, seen, want)
		}
	}
}

// The client certificate goes where the token does. The configuration's is the
// one stored for the default host, and the lookup on another clone host
// presented it there: the private key never leaves, but the certificate says
// who is asking. Another host is shown a certificate set for every host, and
// no other.
func TestCloneParentLookupPresentsOnlyTheCloneHostsCertificate(t *testing.T) {
	withNoOtherConfiguration(t)

	defaultHost := newCertificateBeacon(t)
	other := newCertificateBeacon(t)
	dir := t.TempDir()
	caFile := writePEM(t, dir, "ca.pem", "CERTIFICATE", defaultHost.Certificate().Raw)
	t.Setenv("BB_CA_FILE", caFile)
	defaultCert, defaultKey := writeClientCertificate(t, dir, "default-host-client")

	deps := (&Dependencies{}).withDefaults()
	cfg := config.AppConfig{
		BitbucketURL:   defaultHost.URL,
		CAFile:         caFile,
		ClientCertFile: defaultCert,
		ClientKeyFile:  defaultKey,
		RequestTimeout: 5 * time.Second,
	}
	repo := cloneRepoRef{ProjectKey: "PRJ", Slug: "demo"}
	lookup := func(host *authorizationBeacon) {
		t.Helper()
		if _, _, err := lookupParentCloneURL(context.Background(), deps, cfg, host.URL, repo); err != nil {
			t.Fatalf("parent lookup on %s: %v", host.URL, err)
		}
	}

	lookup(other)
	lookup(defaultHost)
	if got := other.presented(); len(got) != 1 || got[0] != "" {
		t.Errorf("another clone host was shown %q, want one request with no certificate", got)
	}
	if got := defaultHost.presented(); len(got) != 1 || got[0] != "default-host-client" {
		t.Errorf("the default host was shown %q, want its own certificate", got)
	}

	// A certificate set for every host is shown to every host.
	everyCert, everyKey := writeClientCertificate(t, dir, "every-host-client")
	t.Setenv("BB_CLIENT_CERT", everyCert)
	t.Setenv("BB_CLIENT_KEY", everyKey)
	lookup(other)
	if got := other.presented(); len(got) != 2 || got[1] != "every-host-client" {
		t.Errorf("a certificate set for every host was not shown to another clone host: %q", got)
	}
}

func writeClientCertificate(t *testing.T, dir, name string) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	return writePEM(t, dir, name+".pem", "CERTIFICATE", der), writePEM(t, dir, name+"-key.pem", "EC PRIVATE KEY", keyDER)
}

func writePEM(t *testing.T, dir, name, blockType string, der []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}
