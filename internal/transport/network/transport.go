package network

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

// SafeTransport is a http.RoundTripper that can be configured to block
// outgoing requests to non-local addresses, typically used during tests.
type SafeTransport struct {
	Base http.RoundTripper
}

func (t *SafeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if os.Getenv("BB_BLOCK_EXTERNAL_NETWORK") == "1" {
		host := req.URL.Hostname()
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			return nil, fmt.Errorf("external network access is disabled during tests (attempted to reach %s)", host)
		}
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

type TLSOptions struct {
	CAFile             string
	InsecureSkipVerify bool
	ClientCertFile     string
	ClientKeyFile      string
}

func NewSafeTransport(options TLSOptions) (http.RoundTripper, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &SafeTransport{}, nil
	}

	transport := base.Clone()
	tlsConfig := transport.TLSClientConfig
	if tlsConfig != nil {
		tlsConfig = tlsConfig.Clone()
	} else {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	tlsConfig.InsecureSkipVerify = options.InsecureSkipVerify

	if options.CAFile != "" {
		pemData, err := os.ReadFile(options.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA bundle: %w", err)
		}

		rootCAs, err := x509.SystemCertPool()
		if err != nil || rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}

		if ok := rootCAs.AppendCertsFromPEM(pemData); !ok {
			return nil, fmt.Errorf("parse CA bundle: no certificates found")
		}

		tlsConfig.RootCAs = rootCAs
	}

	if options.ClientCertFile != "" || options.ClientKeyFile != "" {
		if options.ClientCertFile == "" || options.ClientKeyFile == "" {
			return nil, fmt.Errorf("both client certificate and private key must be provided")
		}
		cert, err := tls.LoadX509KeyPair(options.ClientCertFile, options.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	transport.TLSClientConfig = tlsConfig
	return &SafeTransport{Base: transport}, nil
}
