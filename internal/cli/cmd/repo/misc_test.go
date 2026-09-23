package repocmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestReadPublicKeyAndScope(t *testing.T) {
	t.Parallel()

	// Direct text
	textKey := "ssh-rsa AAAA..."
	readKey, err := readPublicKey(textKey)
	if err != nil || readKey != textKey {
		t.Fatalf("unexpected readPublicKey text: %v", err)
	}

	// File text
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "id_rsa.pub")
	if err := os.WriteFile(keyFile, []byte("ssh-ed25519 BBBB...\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readKey, err = readPublicKey(keyFile)
	if err != nil || readKey != "ssh-ed25519 BBBB..." {
		t.Fatalf("unexpected readPublicKey file: %s, %v", readKey, err)
	}

	// Scope resolution
	proj, repo, isProj, err := resolveRepoSshKeyScope("PRJ", "")
	if err != nil || proj != "PRJ" || repo != "" || !isProj {
		t.Fatalf("unexpected resolveRepoSshKeyScope project: %s, %s, %v, %v", proj, repo, isProj, err)
	}

	proj, repo, isProj, err = resolveRepoSshKeyScope("", "PRJ/repo1")
	if err != nil || proj != "PRJ" || repo != "repo1" || isProj {
		t.Fatalf("unexpected resolveRepoSshKeyScope repo: %s, %s, %v, %v", proj, repo, isProj, err)
	}

	_, _, _, err = resolveRepoSshKeyScope("PRJ", "PRJ/repo1")
	if err == nil {
		t.Fatal("expected error when both project and repo are specified")
	}

	_, _, _, err = resolveRepoSshKeyScope("", "")
	if err == nil {
		t.Fatal("expected error when neither project nor repo is specified")
	}

	_, _, _, err = resolveRepoSshKeyScope("", "invalid-format")
	if err == nil {
		t.Fatal("expected error for invalid repo format")
	}
}

// TestAnArchiveFailureSaysWhatFailed: Bitbucket's own refusal goes back as
// every command reports one, and a failure in transit is wrapped with the
// command's context, keeping the kind the transport gave it (#478).
func TestAnArchiveFailureSaysWhatFailed(t *testing.T) {
	t.Parallel()

	if err := archiveFailure(nil); err != nil {
		t.Fatalf("no failure became %v", err)
	}

	// Shaped as the status mapping shapes one: the status it answered with, as
	// a detail beside the message.
	refused := apperrors.WithDetail(apperrors.New(apperrors.KindNotFound, "bitbucket API returned 404: Repository PRJ/gone does not exist.", nil), "upstreamStatus", "404")
	if got := archiveFailure(refused); !errors.Is(got, refused) || apperrors.MessageOf(got) != apperrors.MessageOf(refused) {
		t.Fatalf("Bitbucket's refusal came back as %v", got)
	}

	certificate := apperrors.New(apperrors.KindPermanent, "the server's TLS certificate was rejected", nil)
	got := archiveFailure(certificate)
	if !apperrors.IsKind(got, apperrors.KindPermanent) || !strings.Contains(got.Error(), "failed to stream the repository archive") {
		t.Fatalf("a failure in transit came back as %v", got)
	}
}

// TestAStreamWhoseReaderLeftIsNotAFailure: `bb repo archive -o - | head`
// closes the pipe once it has what it wants, and the command ends as it ends
// any other output. Any other failure to write is reported.
func TestAStreamWhoseReaderLeftIsNotAFailure(t *testing.T) {
	t.Parallel()

	if err := streamed(fmt.Errorf("write: %w", io.ErrClosedPipe)); err != nil {
		t.Fatalf("a reader that left was reported: %v", err)
	}

	full := apperrors.New(apperrors.KindInternal, "failed to write the download", errors.New("no space left on device"))
	if err := streamed(full); !errors.Is(err, full) {
		t.Fatalf("a failed write came back as %v", err)
	}
}

// The repo misc, ssh key and label/watch/task suites are live now.
//
// Each asserted the line a command printed against a payload written in
// this file, so what passed was that the formatter agreed with the fixture.
// Every command in them is asserted against a real Bitbucket by the live
// suite, and command-reach fails if any of them loses that.
