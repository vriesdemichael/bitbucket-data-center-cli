package repocmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestReadPublicKeyAndScope(t *testing.T) {
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

// TestFinishArchiveFile covers the close that `bb repo archive` reports on.
//
// The failure branch is the point: io.Copy succeeding does not mean the bytes
// reached the disk, and this command used to print success over a truncated
// archive. Closing an already-closed file is the portable way to make Close
// fail.
func TestFinishArchiveFile(t *testing.T) {
	t.Run("nil file is not an error", func(t *testing.T) {
		if err := finishArchiveFile(nil, "unused"); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("closes an open file", func(t *testing.T) {
		file, err := os.Create(filepath.Join(t.TempDir(), "archive.zip"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := finishArchiveFile(file, "archive.zip"); err != nil {
			t.Fatalf("expected a clean close, got %v", err)
		}
	})

	t.Run("reports a failed close as an error naming the target", func(t *testing.T) {
		file, err := os.Create(filepath.Join(t.TempDir(), "archive.zip"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("first close: %v", err)
		}

		err = finishArchiveFile(file, "/tmp/archive.zip")
		if err == nil {
			t.Fatal("expected a failed close to be reported, got nil")
		}
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected KindInternal, got %v", err)
		}
		if !strings.Contains(err.Error(), "/tmp/archive.zip") {
			t.Fatalf("message does not name the target file: %v", err)
		}
	})
}

// The repo misc, ssh key and label/watch/task suites are live now.
//
// Each asserted the line a command printed against a payload written in
// this file, so what passed was that the formatter agreed with the fixture.
// Every command in them is asserted against a real Bitbucket by the live
// suite, and command-reach fails if any of them loses that.
