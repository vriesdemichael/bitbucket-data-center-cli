package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSafeTransport(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		blockEnv  string
		wantError bool
	}{
		{
			name:      "localhost allowed when blocked",
			url:       "http://localhost:8080",
			blockEnv:  "1",
			wantError: false,
		},
		{
			name:      "127.0.0.1 allowed when blocked",
			url:       "http://127.0.0.1:8080",
			blockEnv:  "1",
			wantError: false,
		},
		{
			name:      "::1 allowed when blocked",
			url:       "http://[::1]:8080",
			blockEnv:  "1",
			wantError: false,
		},
		{
			name:      "external blocked when enabled",
			url:       "http://example.com",
			blockEnv:  "1",
			wantError: true,
		},
		{
			name:      "external allowed when disabled",
			url:       "http://example.com",
			blockEnv:  "0",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("BB_BLOCK_EXTERNAL_NETWORK", tt.blockEnv)

			// Use a dummy transport for the success cases to avoid real network calls
			// if the URL is actually reachable.
			base := &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200}, nil
				},
			}

			transport := &SafeTransport{Base: base}
			req, _ := http.NewRequest(http.MethodGet, tt.url, nil)

			response, err := transport.RoundTrip(req)
			closeResponse(response)
			if (err != nil) != tt.wantError {
				t.Errorf("SafeTransport.RoundTrip() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

type mockTransport struct {
	roundTripFunc func(*http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestNewSafeTransport(t *testing.T) {
	t.Run("insecure mode enabled", func(t *testing.T) {
		roundTripper, err := NewSafeTransport(TLSOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		safe, ok := roundTripper.(*SafeTransport)
		if !ok {
			t.Fatalf("expected SafeTransport, got %T", roundTripper)
		}

		base, ok := safe.Base.(*http.Transport)
		if !ok {
			t.Fatalf("expected *http.Transport base, got %T", safe.Base)
		}

		if base.TLSClientConfig == nil || !base.TLSClientConfig.InsecureSkipVerify {
			t.Fatal("expected InsecureSkipVerify to be true")
		}
	})

	t.Run("missing ca file", func(t *testing.T) {
		_, err := NewSafeTransport(TLSOptions{CAFile: filepath.Join(t.TempDir(), "missing.pem")})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid ca file", func(t *testing.T) {
		caFile := filepath.Join(t.TempDir(), "bad.pem")
		if err := os.WriteFile(caFile, []byte("not-a-cert"), 0o600); err != nil {
			t.Fatalf("write ca file: %v", err)
		}

		_, err := NewSafeTransport(TLSOptions{CAFile: caFile})
		if err == nil {
			t.Fatal("expected parse error")
		}
	})

	t.Run("only client cert provided without key", func(t *testing.T) {
		certPEM, _ := generateCertificatePair(t)
		certFile := filepath.Join(t.TempDir(), "client.crt")
		if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
			t.Fatalf("write cert: %v", err)
		}

		_, err := NewSafeTransport(TLSOptions{ClientCertFile: certFile})
		if err == nil {
			t.Fatal("expected error when client key is missing")
		}
	})

	t.Run("only client key provided without cert", func(t *testing.T) {
		_, keyPEM := generateCertificatePair(t)
		keyFile := filepath.Join(t.TempDir(), "client.key")
		if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}

		_, err := NewSafeTransport(TLSOptions{ClientKeyFile: keyFile})
		if err == nil {
			t.Fatal("expected error when client cert is missing")
		}
	})

	t.Run("missing client cert file on disk", func(t *testing.T) {
		dir := t.TempDir()
		_, err := NewSafeTransport(TLSOptions{
			ClientCertFile: filepath.Join(dir, "missing.crt"),
			ClientKeyFile:  filepath.Join(dir, "missing.key"),
		})
		if err == nil {
			t.Fatal("expected error for missing client cert file")
		}
	})

	t.Run("invalid client cert PEM", func(t *testing.T) {
		dir := t.TempDir()
		certFile := filepath.Join(dir, "bad.crt")
		keyFile := filepath.Join(dir, "bad.key")
		if err := os.WriteFile(certFile, []byte("not-a-cert"), 0o600); err != nil {
			t.Fatalf("write bad cert: %v", err)
		}
		if err := os.WriteFile(keyFile, []byte("not-a-key"), 0o600); err != nil {
			t.Fatalf("write bad key: %v", err)
		}

		_, err := NewSafeTransport(TLSOptions{
			ClientCertFile: certFile,
			ClientKeyFile:  keyFile,
		})
		if err == nil {
			t.Fatal("expected error for invalid PEM")
		}
	})

	t.Run("valid client cert and key configure certificates", func(t *testing.T) {
		dir := t.TempDir()
		certPEM, keyPEM := generateCertificatePair(t)
		certFile := filepath.Join(dir, "client.crt")
		keyFile := filepath.Join(dir, "client.key")
		if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
			t.Fatalf("write cert: %v", err)
		}
		if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}

		roundTripper, err := NewSafeTransport(TLSOptions{
			ClientCertFile: certFile,
			ClientKeyFile:  keyFile,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		safe, ok := roundTripper.(*SafeTransport)
		if !ok {
			t.Fatalf("expected SafeTransport, got %T", roundTripper)
		}
		base, ok := safe.Base.(*http.Transport)
		if !ok {
			t.Fatalf("expected *http.Transport base, got %T", safe.Base)
		}
		if len(base.TLSClientConfig.Certificates) != 1 {
			t.Fatalf("expected 1 certificate loaded, got %d", len(base.TLSClientConfig.Certificates))
		}
	})

	t.Run("mutual TLS roundtrip against server requiring client cert", func(t *testing.T) {
		dir := t.TempDir()
		caCertPEM, caKeyPEM, caPool := generateTestCA(t)
		caFile := filepath.Join(dir, "ca.pem")
		if err := os.WriteFile(caFile, caCertPEM, 0o600); err != nil {
			t.Fatalf("write ca: %v", err)
		}

		serverCertPEM, serverKeyPEM := generateSignedCert(t, caCertPEM, caKeyPEM, "127.0.0.1", true)
		serverTLS, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
		if err != nil {
			t.Fatalf("load server key pair: %v", err)
		}

		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("mtls-ok"))
		}))
		server.TLS = &tls.Config{
			Certificates: []tls.Certificate{serverTLS},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    caPool,
			MinVersion:   tls.VersionTLS12,
		}
		server.StartTLS()
		defer server.Close()

		clientCertPEM, clientKeyPEM := generateSignedCert(t, caCertPEM, caKeyPEM, "client-identity", false)
		clientCertFile := filepath.Join(dir, "client.pem")
		clientKeyFile := filepath.Join(dir, "client.key")
		if err := os.WriteFile(clientCertFile, clientCertPEM, 0o600); err != nil {
			t.Fatalf("write client cert: %v", err)
		}
		if err := os.WriteFile(clientKeyFile, clientKeyPEM, 0o600); err != nil {
			t.Fatalf("write client key: %v", err)
		}

		// Request WITH client cert should succeed
		validTransport, err := NewSafeTransport(TLSOptions{
			CAFile:         caFile,
			ClientCertFile: clientCertFile,
			ClientKeyFile:  clientKeyFile,
		})
		if err != nil {
			t.Fatalf("create valid mTLS transport: %v", err)
		}
		client := &http.Client{Transport: validTransport}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("expected successful mTLS request, got err: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}

		// Request WITHOUT client cert should fail TLS handshake
		noCertTransport, err := NewSafeTransport(TLSOptions{
			CAFile: caFile,
		})
		if err != nil {
			t.Fatalf("create no-cert transport: %v", err)
		}
		noCertClient := &http.Client{Transport: noCertTransport}
		refused, err := noCertClient.Get(server.URL)
		closeResponse(refused)
		if err == nil {
			t.Fatal("expected request without client cert to fail mTLS handshake")
		}
	})
}

func generateCertificatePair(t *testing.T) ([]byte, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test-client",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certBuf := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	keyBuf := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certBuf, keyBuf
}

func generateTestCA(t *testing.T) ([]byte, []byte, *x509.CertPool) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject: pkix.Name{
			CommonName: "Test Root CA",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create ca cert: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal ca key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)

	return certPEM, keyPEM, pool
}

func generateSignedCert(t *testing.T, caCertPEM, caKeyPEM []byte, hostOrCN string, isServer bool) ([]byte, []byte) {
	t.Helper()
	caBlock, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("parse ca cert: %v", err)
	}

	keyBlock, _ := pem.Decode(caKeyPEM)
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("parse ca key: %v", err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: hostOrCN,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}

	if isServer {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		if ip := net.ParseIP(hostOrCN); ip != nil {
			template.IPAddresses = []net.IP{ip}
		} else {
			template.DNSNames = []string{hostOrCN}
		}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &priv.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create signed cert: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}

// closeResponse releases a response body when there is one.
//
// A request that fails returns a nil response, and one that succeeds holds a
// connection open until its body is closed. Tests that discard the response
// leak the second kind, which is what bodyclose reports and what this makes
// unnecessary to think about at each call site.
func closeResponse(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}
