package sigstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	sigroot "github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func TestVerifierVerifyEntity(t *testing.T) {
	virtualSigstore, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("NewVirtualSigstore returned error: %v", err)
	}
	artifact := []byte("signed checksum manifest")
	entity, err := virtualSigstore.Sign("foo!oidc.local", "http://oidc.local:8080", artifact)
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}

	verifier := NewVerifier(Options{
		ExpectedIssuer: "http://oidc.local:8080",
		ExpectedSAN:    "foo!oidc.local",
		TrustedMaterialProvider: func(context.Context) (sigroot.TrustedMaterial, error) {
			return virtualSigstore, nil
		},
		VerifierOptions: []sigverify.VerifierOption{
			sigverify.WithTransparencyLog(1),
			sigverify.WithObserverTimestamps(1),
		},
	})

	verification, err := verifier.verifyEntity(context.Background(), entity, sigverify.WithArtifact(bytes.NewReader(artifact)))
	if err != nil {
		t.Fatalf("verifyEntity returned error: %v", err)
	}
	if verification.CertificateIdentity != "foo!oidc.local" {
		t.Fatalf("unexpected certificate identity: %+v", verification)
	}
	if verification.CertificateOIDCIssuer != "http://oidc.local:8080" {
		t.Fatalf("unexpected certificate issuer: %+v", verification)
	}
	if verification.TransparencyLogEntriesVerified == 0 {
		t.Fatalf("expected Rekor verification evidence, got %+v", verification)
	}
	if verification.VerifiedTimestampCount == 0 {
		t.Fatalf("expected verified timestamps, got %+v", verification)
	}
}

func TestVerifierVerifyBlobLegacyBundle(t *testing.T) {
	artifact, virtualSigstore, _, _, _, legacyJSON := mustLegacyFixture(t)
	verifier := NewVerifier(Options{
		ExpectedIssuer: "http://oidc.local:8080",
		ExpectedSAN:    "foo!oidc.local",
		TrustedMaterialProvider: func(context.Context) (sigroot.TrustedMaterial, error) {
			return virtualSigstore, nil
		},
		VerifierOptions: []sigverify.VerifierOption{
			sigverify.WithTransparencyLog(1),
			sigverify.WithObserverTimestamps(1),
		},
	})

	verification, err := verifier.VerifyBlob(context.Background(), artifact, legacyJSON)
	if err != nil {
		t.Fatalf("VerifyBlob returned error: %v", err)
	}
	if verification.CertificateIdentity != "foo!oidc.local" {
		t.Fatalf("unexpected certificate identity: %+v", verification)
	}
	if verification.CertificateOIDCIssuer != "http://oidc.local:8080" {
		t.Fatalf("unexpected certificate issuer: %+v", verification)
	}
	if verification.TransparencyLogEntriesVerified == 0 {
		t.Fatalf("expected Rekor verification evidence, got %+v", verification)
	}
	if verification.VerifiedTimestampCount == 0 {
		t.Fatalf("expected verified timestamps, got %+v", verification)
	}
}

func TestLegacyVerificationMaterial(t *testing.T) {
	_, _, _, certificate, _, _ := mustLegacyFixture(t)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})

	t.Run("single certificate", func(t *testing.T) {
		material, err := legacyVerificationMaterial(base64.StdEncoding.EncodeToString(pemBytes))
		if err != nil {
			t.Fatalf("legacyVerificationMaterial returned error: %v", err)
		}
		if material.GetCertificate() == nil {
			t.Fatal("expected certificate verification material")
		}
	})

	t.Run("certificate chain", func(t *testing.T) {
		material, err := legacyVerificationMaterial(string(append(append([]byte{}, pemBytes...), pemBytes...)))
		if err != nil {
			t.Fatalf("legacyVerificationMaterial returned error: %v", err)
		}
		chain := material.GetX509CertificateChain()
		if chain == nil || len(chain.GetCertificates()) != 2 {
			t.Fatalf("expected two-certificate chain, got %+v", chain)
		}
	})

	t.Run("missing certificate", func(t *testing.T) {
		if _, err := legacyVerificationMaterial(" "); err == nil {
			t.Fatal("expected error for missing certificate")
		}
	})

	t.Run("invalid certificate", func(t *testing.T) {
		if _, err := legacyVerificationMaterial("not-a-cert"); err == nil {
			t.Fatal("expected error for invalid certificate")
		}
	})
}

func TestLegacyBodyBytes(t *testing.T) {
	t.Run("base64 string", func(t *testing.T) {
		body, err := legacyBodyBytes(base64.StdEncoding.EncodeToString([]byte("hello")))
		if err != nil {
			t.Fatalf("legacyBodyBytes returned error: %v", err)
		}
		if string(body) != "hello" {
			t.Fatalf("expected decoded body, got %q", string(body))
		}
	})

	t.Run("plain string", func(t *testing.T) {
		body, err := legacyBodyBytes("{\"kind\":\"hashedrekord\"}")
		if err != nil {
			t.Fatalf("legacyBodyBytes returned error: %v", err)
		}
		if string(body) != "{\"kind\":\"hashedrekord\"}" {
			t.Fatalf("unexpected plain string body %q", string(body))
		}
	})

	t.Run("bytes", func(t *testing.T) {
		body, err := legacyBodyBytes([]byte("raw"))
		if err != nil {
			t.Fatalf("legacyBodyBytes returned error: %v", err)
		}
		if string(body) != "raw" {
			t.Fatalf("unexpected bytes body %q", string(body))
		}
	})

	t.Run("empty string", func(t *testing.T) {
		if _, err := legacyBodyBytes("   "); err == nil {
			t.Fatal("expected error for empty string body")
		}
	})

	t.Run("empty bytes", func(t *testing.T) {
		if _, err := legacyBodyBytes([]byte{}); err == nil {
			t.Fatal("expected error for empty byte body")
		}
	})

	t.Run("invalid type", func(t *testing.T) {
		if _, err := legacyBodyBytes(123); err == nil {
			t.Fatal("expected error for invalid body type")
		}
	})
}

func TestLegacyKindVersion(t *testing.T) {
	_, _, _, _, body, _ := mustLegacyFixture(t)

	kindVersion, err := legacyKindVersion(body)
	if err != nil {
		t.Fatalf("legacyKindVersion returned error: %v", err)
	}
	if kindVersion.Kind == "" || kindVersion.Version == "" {
		t.Fatalf("expected populated kind/version, got %+v", kindVersion)
	}

	if _, err := legacyKindVersion([]byte("not-json")); err == nil {
		t.Fatal("expected error for invalid Rekor body")
	}
}

func TestParseLegacyBundleErrors(t *testing.T) {
	artifact, _, _, _, _, legacyJSON := mustLegacyFixture(t)
	var legacy map[string]any
	if err := json.Unmarshal(legacyJSON, &legacy); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "missing signature",
			mutate: func(payload map[string]any) {
				delete(payload, "base64Signature")
			},
			want: "missing base64Signature",
		},
		{
			name: "missing rekor bundle",
			mutate: func(payload map[string]any) {
				delete(payload, "rekorBundle")
			},
			want: "missing rekor bundle",
		},
		{
			name: "invalid signature encoding",
			mutate: func(payload map[string]any) {
				payload["base64Signature"] = "%%%"
			},
			want: "decoding signature",
		},
		{
			name: "invalid log id",
			mutate: func(payload map[string]any) {
				rekorBundle := payload["rekorBundle"].(map[string]any)
				rekorPayload := rekorBundle["Payload"].(map[string]any)
				rekorPayload["logID"] = "zz"
			},
			want: "decoding rekor log id",
		},
		{
			name: "invalid signed entry timestamp",
			mutate: func(payload map[string]any) {
				rekorBundle := payload["rekorBundle"].(map[string]any)
				rekorBundle["SignedEntryTimestamp"] = "%%%"
			},
			want: "decoding signed entry timestamp",
		},
		{
			name: "invalid certificate",
			mutate: func(payload map[string]any) {
				payload["cert"] = "%%%"
			},
			want: "certificate",
		},
		{
			name: "invalid body type",
			mutate: func(payload map[string]any) {
				rekorBundle := payload["rekorBundle"].(map[string]any)
				rekorPayload := rekorBundle["Payload"].(map[string]any)
				rekorPayload["body"] = true
			},
			want: "unexpected rekor body type",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			payload := cloneLegacyMap(t, legacy)
			testCase.mutate(payload)
			mutatedJSON, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("json.Marshal returned error: %v", err)
			}
			_, err = parseLegacyBundle(mutatedJSON, artifact)
			if err == nil {
				t.Fatal("expected parseLegacyBundle error")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected error containing %q, got %v", testCase.want, err)
			}
		})
	}
}

func TestNewVerifierDefaults(t *testing.T) {
	verifier := NewVerifier(Options{ExpectedIssuer: " https://issuer.example ", ExpectedSAN: " https://san.example "})
	if verifier == nil {
		t.Fatal("expected verifier")
	}
	if verifier.expectedIssuer != "https://issuer.example" {
		t.Fatalf("expected trimmed issuer, got %q", verifier.expectedIssuer)
	}
	if verifier.expectedSAN != "https://san.example" {
		t.Fatalf("expected trimmed san, got %q", verifier.expectedSAN)
	}
	if verifier.trustedMaterialProvider == nil {
		t.Fatal("expected trusted material provider")
	}
	if len(verifier.verifierOptions) != 3 {
		t.Fatalf("expected default verifier options, got %d", len(verifier.verifierOptions))
	}
}

func marshalLegacyBundle(entity sigverify.SignedEntity) ([]byte, error) {
	verificationContent, err := entity.VerificationContent()
	if err != nil {
		return nil, err
	}
	certificate := verificationContentCertificate(verificationContent)
	if certificate == nil {
		return nil, errMissingLegacyFixtureComponent("certificate")
	}

	signatureContent, err := entity.SignatureContent()
	if err != nil {
		return nil, err
	}
	messageSignature := signatureContent.MessageSignatureContent()
	if messageSignature == nil {
		return nil, errMissingLegacyFixtureComponent("message signature")
	}

	tlogEntries, err := entity.TlogEntries()
	if err != nil {
		return nil, err
	}
	if len(tlogEntries) == 0 {
		return nil, errMissingLegacyFixtureComponent("transparency log entry")
	}
	signedEntryTimestamp, err := signedEntryTimestampForTest(tlogEntries[0])
	if err != nil {
		return nil, err
	}

	legacy := map[string]any{
		"base64Signature": base64.StdEncoding.EncodeToString(messageSignature.Signature()),
		"cert":            base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})),
		"rekorBundle": map[string]any{
			"SignedEntryTimestamp": base64.StdEncoding.EncodeToString(signedEntryTimestamp),
			"Payload": map[string]any{
				"body":           tlogEntries[0].Body(),
				"integratedTime": tlogEntries[0].IntegratedTime().Unix(),
				"logIndex":       tlogEntries[0].LogIndex(),
				"logID":          hex.EncodeToString([]byte(tlogEntries[0].LogKeyID())),
			},
		},
	}

	return json.Marshal(legacy)
}

func artifactDigest(artifact []byte) []byte {
	digest := sha256.Sum256(artifact)
	return digest[:]
}

func errMissingLegacyFixtureComponent(name string) error {
	return fmt.Errorf("missing legacy fixture component: %s", name)
}

func verificationContentCertificate(content sigverify.VerificationContent) *x509.Certificate {
	type certificateProvider interface {
		Certificate() *x509.Certificate
	}
	type legacyCertificateProvider interface {
		GetCertificate() *x509.Certificate
	}

	if provider, ok := any(content).(certificateProvider); ok {
		return provider.Certificate()
	}
	if provider, ok := any(content).(legacyCertificateProvider); ok {
		return provider.GetCertificate()
	}
	return nil
}

func signedEntryTimestampForTest(entry any) ([]byte, error) {
	value := reflect.ValueOf(entry)
	if !value.IsValid() || value.IsNil() {
		return nil, errMissingLegacyFixtureComponent("signed entry timestamp")
	}
	entryValue := value.Elem()
	field := entryValue.FieldByName("signedEntryTimestamp")
	if !field.IsValid() {
		return nil, errMissingLegacyFixtureComponent("signed entry timestamp")
	}
	return *(*[]byte)(unsafe.Pointer(field.UnsafeAddr())), nil
}

func mustLegacyFixture(t *testing.T) ([]byte, sigroot.TrustedMaterial, sigverify.SignedEntity, *x509.Certificate, []byte, []byte) {
	t.Helper()
	virtualSigstore, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("NewVirtualSigstore returned error: %v", err)
	}
	artifact := []byte("signed checksum manifest")
	entity, err := virtualSigstore.Sign("foo!oidc.local", "http://oidc.local:8080", artifact)
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}
	legacyJSON, err := marshalLegacyBundle(entity)
	if err != nil {
		t.Fatalf("marshalLegacyBundle returned error: %v", err)
	}

	verificationContent, err := entity.VerificationContent()
	if err != nil {
		t.Fatalf("VerificationContent returned error: %v", err)
	}
	certificate := verificationContentCertificate(verificationContent)
	if certificate == nil {
		t.Fatal("expected certificate")
	}

	tlogEntries, err := entity.TlogEntries()
	if err != nil {
		t.Fatalf("TlogEntries returned error: %v", err)
	}
	if len(tlogEntries) == 0 {
		t.Fatal("expected Rekor entry")
	}
	body, err := legacyBodyBytes(tlogEntries[0].Body())
	if err != nil {
		t.Fatalf("legacyBodyBytes returned error: %v", err)
	}

	return artifact, virtualSigstore, entity, certificate, body, legacyJSON
}

func cloneLegacyMap(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	return clone
}

func TestNewVerifierUsesExplicitProviderAndOptions(t *testing.T) {
	providerCalls := 0
	provider := func(context.Context) (sigroot.TrustedMaterial, error) {
		providerCalls++
		return nil, nil
	}
	options := []sigverify.VerifierOption{sigverify.WithTransparencyLog(1)}

	verifier := NewVerifier(Options{
		ExpectedIssuer:          GitHubActionsIssuer,
		ExpectedSAN:             "https://github.com/example/repo/.github/workflows/release.yml@refs/heads/main",
		TrustedMaterialProvider: provider,
		VerifierOptions:         options,
	})

	if verifier.trustedMaterialProvider == nil {
		t.Fatal("expected trusted material provider")
	}
	if got := len(verifier.verifierOptions); got != len(options) {
		t.Fatalf("expected %d verifier options, got %d", len(options), got)
	}
	if verifier.expectedIssuer != GitHubActionsIssuer {
		t.Fatalf("unexpected issuer: %q", verifier.expectedIssuer)
	}
	if _, err := verifier.trustedMaterialProvider(context.Background()); err != nil {
		t.Fatalf("trustedMaterialProvider returned error: %v", err)
	}
	if providerCalls != 1 {
		t.Fatalf("expected provider to be invoked once, got %d", providerCalls)
	}
}

func TestNewGitHubReleaseVerifier(t *testing.T) {
	verifier := NewGitHubReleaseVerifier(" vriesdemichael ", " bitbucket-data-center-cli ")
	if verifier == nil {
		t.Fatal("expected verifier")
	}
	if verifier.expectedIssuer != GitHubActionsIssuer {
		t.Fatalf("expected GitHub issuer, got %q", verifier.expectedIssuer)
	}
	expectedSAN := "https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main"
	if verifier.expectedSAN != expectedSAN {
		t.Fatalf("expected SAN %q, got %q", expectedSAN, verifier.expectedSAN)
	}
}

func TestVerifierVerifyBlobErrors(t *testing.T) {
	t.Run("nil verifier", func(t *testing.T) {
		var verifier *Verifier
		_, err := verifier.VerifyBlob(context.Background(), []byte("artifact"), []byte("bundle"))
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("invalid bundle", func(t *testing.T) {
		verifier := NewVerifier(Options{ExpectedIssuer: GitHubActionsIssuer, ExpectedSAN: "san"})
		_, err := verifier.VerifyBlob(context.Background(), []byte("artifact"), []byte("not-json"))
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("missing artifact content", func(t *testing.T) {
		verifier := NewVerifier(Options{ExpectedIssuer: GitHubActionsIssuer, ExpectedSAN: "san"})
		_, err := verifier.VerifyBlob(context.Background(), nil, []byte("bundle"))
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("missing bundle content", func(t *testing.T) {
		verifier := NewVerifier(Options{ExpectedIssuer: GitHubActionsIssuer, ExpectedSAN: "san"})
		_, err := verifier.VerifyBlob(context.Background(), []byte("artifact"), nil)
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("missing expected identity configuration", func(t *testing.T) {
		verifier := NewVerifier(Options{})
		_, err := verifier.VerifyBlob(context.Background(), []byte("artifact"), []byte("bundle"))
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("trusted root failure bubbles up", func(t *testing.T) {
		virtualSigstore, err := ca.NewVirtualSigstore()
		if err != nil {
			t.Fatalf("NewVirtualSigstore returned error: %v", err)
		}
		artifact := []byte("artifact")
		entity, err := virtualSigstore.Sign("foo!oidc.local", GitHubActionsIssuer, artifact)
		if err != nil {
			t.Fatalf("Sign returned error: %v", err)
		}
		verifier := NewVerifier(Options{
			ExpectedIssuer: GitHubActionsIssuer,
			ExpectedSAN:    "foo!oidc.local",
			TrustedMaterialProvider: func(context.Context) (sigroot.TrustedMaterial, error) {
				return nil, apperrors.New(apperrors.KindTransient, "boom", nil)
			},
		})
		_, err = verifier.verifyEntity(context.Background(), entity, sigverify.WithArtifact(bytes.NewReader(artifact)))
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("identity mismatch", func(t *testing.T) {
		virtualSigstore, err := ca.NewVirtualSigstore()
		if err != nil {
			t.Fatalf("NewVirtualSigstore returned error: %v", err)
		}
		artifact := []byte("artifact")
		entity, err := virtualSigstore.Sign("foo!oidc.local", "http://oidc.local:8080", artifact)
		if err != nil {
			t.Fatalf("Sign returned error: %v", err)
		}

		verifier := NewVerifier(Options{
			ExpectedIssuer: "http://oidc.local:8080",
			ExpectedSAN:    "other@example.com",
			TrustedMaterialProvider: func(context.Context) (sigroot.TrustedMaterial, error) {
				return virtualSigstore, nil
			},
			VerifierOptions: []sigverify.VerifierOption{
				sigverify.WithTransparencyLog(1),
				sigverify.WithObserverTimestamps(1),
			},
		})

		_, err = verifier.verifyEntity(context.Background(), entity, sigverify.WithArtifact(bytes.NewReader(artifact)))
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("nil signed entity", func(t *testing.T) {
		verifier := NewVerifier(Options{ExpectedIssuer: GitHubActionsIssuer, ExpectedSAN: "san", TrustedMaterialProvider: func(context.Context) (sigroot.TrustedMaterial, error) {
			virtualSigstore, err := ca.NewVirtualSigstore()
			if err != nil {
				t.Fatalf("NewVirtualSigstore returned error: %v", err)
			}
			return virtualSigstore, nil
		}})
		_, err := verifier.verifyEntity(context.Background(), nil, sigverify.WithArtifact(bytes.NewReader([]byte("artifact"))))
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("missing trusted material provider", func(t *testing.T) {
		verifier := &Verifier{expectedIssuer: GitHubActionsIssuer, expectedSAN: "san"}
		_, err := verifier.verifyEntity(context.Background(), nil, sigverify.WithArtifact(bytes.NewReader([]byte("artifact"))))
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("missing expected identity configuration in verifyEntity", func(t *testing.T) {
		verifier := &Verifier{
			trustedMaterialProvider: func(context.Context) (sigroot.TrustedMaterial, error) { return nil, nil },
		}
		_, err := verifier.verifyEntity(context.Background(), nil, sigverify.WithArtifact(bytes.NewReader([]byte("artifact"))))
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

}