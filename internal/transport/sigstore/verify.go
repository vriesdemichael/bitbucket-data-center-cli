package sigstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-openapi/runtime"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/rekor/pkg/generated/models"
	"github.com/sigstore/rekor/pkg/types"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	sigroot "github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

const (
	GitHubActionsIssuer = "https://token.actions.githubusercontent.com"
	ReleaseWorkflowPath = ".github/workflows/release.yml"
	MainBranchRef       = "refs/heads/main"
)

type TrustedMaterialProvider func(context.Context) (sigroot.TrustedMaterial, error)

type Options struct {
	ExpectedIssuer          string
	ExpectedSAN             string
	TrustedMaterialProvider TrustedMaterialProvider
	VerifierOptions         []sigverify.VerifierOption
}

type Verification struct {
	CertificateIdentity            string
	CertificateOIDCIssuer          string
	TransparencyLogEntriesVerified int
	VerifiedTimestampCount         int
}

type Verifier struct {
	expectedIssuer          string
	expectedSAN             string
	trustedMaterialProvider TrustedMaterialProvider
	verifierOptions         []sigverify.VerifierOption
}

type legacyBundlePayload struct {
	Base64Signature string             `json:"base64Signature"`
	Cert            string             `json:"cert"`
	RekorBundle     *legacyRekorBundle `json:"rekorBundle"`
	Bundle          *legacyRekorBundle `json:"bundle"`
}

type legacyRekorBundle struct {
	SignedEntryTimestamp string             `json:"SignedEntryTimestamp"`
	Payload              legacyRekorPayload `json:"Payload"`
}

type legacyRekorPayload struct {
	Body           any    `json:"body"`
	IntegratedTime int64  `json:"integratedTime"`
	LogIndex       int64  `json:"logIndex"`
	LogID          string `json:"logID"`
}

// ReleaseVerifierOptions describes the release signer bb will accept and where
// the trust material backing that decision comes from.
type ReleaseVerifierOptions struct {
	// Owner and Repo build the default signer identity: the release workflow of
	// the bb repository on GitHub Actions.
	Owner string
	Repo  string
	// TrustedRootPath and TUFRepositoryURL each replace the public-good TUF
	// fetch. At most one is expected; the file wins if both are given.
	TrustedRootPath  string
	TUFRepositoryURL string
	// ExpectedIdentity and ExpectedIssuer override the default signer, for
	// organisations that re-sign mirrored artifacts with their own Fulcio.
	ExpectedIdentity string
	ExpectedIssuer   string
	// HTTPClient is used for the TUF mirror fetch, so that the CA bundle and
	// client certificates resolved for this invocation apply to it.
	HTTPClient *http.Client
}

// NewReleaseVerifier builds the verifier for `bb update`.
//
// Defaults reproduce NewGitHubReleaseVerifier exactly: the bb release workflow
// on GitHub Actions, verified against trust material fetched from the
// public-good Sigstore TUF repository.
func NewReleaseVerifier(options ReleaseVerifierOptions) *Verifier {
	expectedIssuer := strings.TrimSpace(options.ExpectedIssuer)
	if expectedIssuer == "" {
		expectedIssuer = GitHubActionsIssuer
	}

	expectedSAN := strings.TrimSpace(options.ExpectedIdentity)
	if expectedSAN == "" {
		expectedSAN = DefaultReleaseIdentity(options.Owner, options.Repo)
	}

	var provider TrustedMaterialProvider
	switch {
	case strings.TrimSpace(options.TrustedRootPath) != "":
		provider = TrustedRootFromFile(options.TrustedRootPath)
	case strings.TrimSpace(options.TUFRepositoryURL) != "":
		provider = TrustedRootFromTUF(options.TUFRepositoryURL, options.HTTPClient)
	}

	return NewVerifier(Options{
		ExpectedIssuer:          expectedIssuer,
		ExpectedSAN:             expectedSAN,
		TrustedMaterialProvider: provider,
	})
}

// DefaultReleaseIdentity is the certificate SAN of the bb release workflow.
func DefaultReleaseIdentity(owner, repo string) string {
	return fmt.Sprintf("https://github.com/%s/%s/%s@%s", strings.TrimSpace(owner), strings.TrimSpace(repo), ReleaseWorkflowPath, MainBranchRef)
}

func NewGitHubReleaseVerifier(owner, repo string) *Verifier {
	return NewReleaseVerifier(ReleaseVerifierOptions{Owner: owner, Repo: repo})
}

func NewVerifier(options Options) *Verifier {
	provider := options.TrustedMaterialProvider
	if provider == nil {
		provider = FetchTrustedRoot
	}

	verifierOptions := options.VerifierOptions
	if len(verifierOptions) == 0 {
		verifierOptions = []sigverify.VerifierOption{
			sigverify.WithSignedCertificateTimestamps(1),
			sigverify.WithTransparencyLog(1),
			sigverify.WithObserverTimestamps(1),
		}
	}

	return &Verifier{
		expectedIssuer:          strings.TrimSpace(options.ExpectedIssuer),
		expectedSAN:             strings.TrimSpace(options.ExpectedSAN),
		trustedMaterialProvider: provider,
		verifierOptions:         verifierOptions,
	}
}

func (verifier *Verifier) VerifyBlob(ctx context.Context, artifact, bundleJSON []byte) (Verification, error) {
	if verifier == nil || verifier.trustedMaterialProvider == nil {
		return Verification{}, apperrors.New(apperrors.KindInternal, "sigstore verifier is not configured", nil)
	}
	if len(artifact) == 0 {
		return Verification{}, apperrors.New(apperrors.KindValidation, "artifact content is required for Sigstore verification", nil)
	}
	if len(bundleJSON) == 0 {
		return Verification{}, apperrors.New(apperrors.KindValidation, "Sigstore bundle content is required", nil)
	}
	if verifier.expectedIssuer == "" || verifier.expectedSAN == "" {
		return Verification{}, apperrors.New(apperrors.KindInternal, "sigstore verifier identity is not configured", nil)
	}

	entity, err := parseSignedEntity(bundleJSON, artifact)
	if err != nil {
		return Verification{}, apperrors.New(apperrors.KindPermanent, "release signature bundle is invalid", err)
	}

	return verifier.verifyEntity(ctx, entity, sigverify.WithArtifact(bytes.NewReader(artifact)))
}

func parseSignedEntity(bundleJSON, artifact []byte) (sigverify.SignedEntity, error) {
	var entity bundle.Bundle
	if err := entity.UnmarshalJSON(bundleJSON); err == nil {
		return &entity, nil
	} else {
		legacyEntity, legacyErr := parseLegacyBundle(bundleJSON, artifact)
		if legacyErr == nil {
			return legacyEntity, nil
		}
		return nil, fmt.Errorf("protobuf bundle parse failed: %w; legacy bundle parse failed: %w", err, legacyErr)
	}
}

func parseLegacyBundle(bundleJSON, artifact []byte) (*bundle.Bundle, error) {
	var legacy legacyBundlePayload
	if err := json.Unmarshal(bundleJSON, &legacy); err != nil {
		return nil, err
	}

	legacyRekor := legacy.RekorBundle
	if legacyRekor == nil {
		legacyRekor = legacy.Bundle
	}
	if strings.TrimSpace(legacy.Base64Signature) == "" {
		return nil, fmt.Errorf("missing base64Signature")
	}
	if legacyRekor == nil {
		return nil, fmt.Errorf("missing rekor bundle")
	}

	signatureBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(legacy.Base64Signature))
	if err != nil {
		return nil, fmt.Errorf("decoding signature: %w", err)
	}

	verificationMaterial, err := legacyVerificationMaterial(legacy.Cert)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := legacyBodyBytes(legacyRekor.Payload.Body)
	if err != nil {
		return nil, err
	}

	kindVersion, err := legacyKindVersion(bodyBytes)
	if err != nil {
		return nil, err
	}

	logID, err := hex.DecodeString(strings.TrimSpace(legacyRekor.Payload.LogID))
	if err != nil {
		return nil, fmt.Errorf("decoding rekor log id: %w", err)
	}

	signedEntryTimestamp, err := base64.StdEncoding.DecodeString(strings.TrimSpace(legacyRekor.SignedEntryTimestamp))
	if err != nil {
		return nil, fmt.Errorf("decoding signed entry timestamp: %w", err)
	}

	mediaType, err := bundle.MediaTypeString("0.1")
	if err != nil {
		return nil, fmt.Errorf("building legacy media type: %w", err)
	}

	digest := sha256.Sum256(artifact)
	entity, err := bundle.NewBundle(&protobundle.Bundle{
		MediaType: mediaType,
		Content: &protobundle.Bundle_MessageSignature{
			MessageSignature: &protocommon.MessageSignature{
				MessageDigest: &protocommon.HashOutput{
					Algorithm: protocommon.HashAlgorithm_SHA2_256,
					Digest:    digest[:],
				},
				Signature: signatureBytes,
			},
		},
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content: verificationMaterial.Content,
			TlogEntries: []*protorekor.TransparencyLogEntry{{
				LogIndex:       legacyRekor.Payload.LogIndex,
				LogId:          &protocommon.LogId{KeyId: logID},
				KindVersion:    kindVersion,
				IntegratedTime: legacyRekor.Payload.IntegratedTime,
				InclusionPromise: &protorekor.InclusionPromise{
					SignedEntryTimestamp: signedEntryTimestamp,
				},
				CanonicalizedBody: bodyBytes,
			}},
		},
	})
	if err != nil {
		return nil, err
	}

	return entity, nil
}

func legacyVerificationMaterial(certValue string) (*protobundle.VerificationMaterial, error) {
	certificates, err := legacyCertificates(certValue)
	if err != nil {
		return nil, err
	}
	if len(certificates) == 1 {
		return &protobundle.VerificationMaterial{
			Content: &protobundle.VerificationMaterial_Certificate{
				Certificate: &protocommon.X509Certificate{RawBytes: certificates[0].Raw},
			},
		}, nil
	}

	chain := make([]*protocommon.X509Certificate, 0, len(certificates))
	for _, certificate := range certificates {
		chain = append(chain, &protocommon.X509Certificate{RawBytes: certificate.Raw})
	}

	return &protobundle.VerificationMaterial{
		Content: &protobundle.VerificationMaterial_X509CertificateChain{
			X509CertificateChain: &protocommon.X509CertificateChain{Certificates: chain},
		},
	}, nil
}

func legacyCertificates(certValue string) ([]*x509.Certificate, error) {
	trimmed := strings.TrimSpace(certValue)
	if trimmed == "" {
		return nil, fmt.Errorf("missing certificate")
	}

	candidates := [][]byte{[]byte(trimmed)}
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		candidates = append([][]byte{decoded}, candidates...)
	}

	for _, candidate := range candidates {
		certificates, err := cryptoutils.UnmarshalCertificatesFromPEM(candidate)
		if err == nil && len(certificates) > 0 {
			return certificates, nil
		}
		if certificate, parseErr := x509.ParseCertificate(candidate); parseErr == nil {
			return []*x509.Certificate{certificate}, nil
		}
	}

	return nil, fmt.Errorf("legacy bundle certificate is not valid PEM or DER")
}

func legacyBodyBytes(body any) ([]byte, error) {
	switch value := body.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, fmt.Errorf("missing rekor body")
		}
		if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
			return decoded, nil
		}
		return []byte(trimmed), nil
	case []byte:
		if len(value) == 0 {
			return nil, fmt.Errorf("missing rekor body")
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unexpected rekor body type %T", body)
	}
}

func legacyKindVersion(body []byte) (*protorekor.KindVersion, error) {
	proposedEntry, err := models.UnmarshalProposedEntry(bytes.NewReader(body), runtime.JSONConsumer())
	if err != nil {
		return nil, fmt.Errorf("parsing rekor body: %w", err)
	}
	entry, err := types.UnmarshalEntry(proposedEntry)
	if err != nil {
		return nil, fmt.Errorf("decoding rekor entry: %w", err)
	}

	return &protorekor.KindVersion{
		Kind:    proposedEntry.Kind(),
		Version: entry.APIVersion(),
	}, nil
}

func (verifier *Verifier) verifyEntity(ctx context.Context, entity sigverify.SignedEntity, artifactPolicy sigverify.ArtifactPolicyOption) (Verification, error) {
	if verifier == nil || verifier.trustedMaterialProvider == nil {
		return Verification{}, apperrors.New(apperrors.KindInternal, "sigstore verifier is not configured", nil)
	}
	if verifier.expectedIssuer == "" || verifier.expectedSAN == "" {
		return Verification{}, apperrors.New(apperrors.KindInternal, "sigstore verifier identity is not configured", nil)
	}
	if entity == nil {
		return Verification{}, apperrors.New(apperrors.KindValidation, "Sigstore signed entity is required", nil)
	}

	trustedMaterial, err := verifier.trustedMaterialProvider(ctx)
	if err != nil {
		return Verification{}, err
	}

	entityVerifier, err := sigverify.NewVerifier(trustedMaterial, verifier.verifierOptions...)
	if err != nil {
		return Verification{}, apperrors.New(apperrors.KindInternal, "failed to initialize Sigstore verifier", err)
	}

	certificateIdentity, err := sigverify.NewShortCertificateIdentity(verifier.expectedIssuer, "", verifier.expectedSAN, "")
	if err != nil {
		return Verification{}, apperrors.New(apperrors.KindInternal, "failed to configure Sigstore certificate identity", err)
	}

	result, err := entityVerifier.Verify(entity, sigverify.NewPolicy(artifactPolicy, sigverify.WithCertificateIdentity(certificateIdentity)))
	if err != nil {
		return Verification{}, apperrors.New(apperrors.KindPermanent, "release signature verification failed", err)
	}
	if result == nil || result.Signature == nil || result.Signature.Certificate == nil {
		return Verification{}, apperrors.New(apperrors.KindPermanent, "release signature verification did not return a signing certificate", nil)
	}

	transparencyLogEntriesVerified := 0
	for _, timestamp := range result.VerifiedTimestamps {
		if strings.EqualFold(timestamp.Type, "tlog") {
			transparencyLogEntriesVerified++
		}
	}

	return Verification{
		CertificateIdentity:            strings.TrimSpace(result.Signature.Certificate.SubjectAlternativeName),
		CertificateOIDCIssuer:          strings.TrimSpace(result.Signature.Certificate.Extensions.Issuer),
		TransparencyLogEntriesVerified: transparencyLogEntriesVerified,
		VerifiedTimestampCount:         len(result.VerifiedTimestamps),
	}, nil
}
