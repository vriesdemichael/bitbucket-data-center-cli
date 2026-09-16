//go:build live

package live_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
	"golang.org/x/crypto/ssh"
)

// generateSSHPublicKey returns a fresh authorized_keys line.
//
// A fixed key would be simpler but wrong: Bitbucket stores a public key once per
// user and rejects the duplicate, so a literal key makes the second run of the
// suite fail on a leftover from the first.
func generateSSHPublicKey(t *testing.T, comment string) string {
	t.Helper()

	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key failed: %v", err)
	}
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatalf("convert to ssh public key failed: %v", err)
	}

	return fmt.Sprintf("%s %s %s",
		sshPublicKey.Type(),
		base64.StdEncoding.EncodeToString(sshPublicKey.Marshal()),
		comment,
	)
}

// TestLivePersonalSSHKeyLifecycle covers bb ssh-key add/list/remove.
//
// These write to the authenticated user's account rather than to a seeded
// project, so the test removes what it adds and identifies its own key by label
// rather than assuming it is the only one present.
func TestLivePersonalSSHKeyLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	label := testsupport.UniqueName("live-suite-personal-")
	// A comment that is not the label: Bitbucket labels a key sent without one
	// with its comment, so a --label bb dropped would still read back as sent.
	publicKey := generateSSHPublicKey(t, testsupport.UniqueName("live-suite-comment-"))

	addOutput, err := executeLiveCLI(t, "--json", "ssh-key", "add", publicKey, "--label", label)
	if err != nil {
		t.Fatalf("ssh-key add failed: %v\noutput: %s", err, addOutput)
	}
	keyID, ok := numericOrStringID(decodeJSONMap(t, addOutput)["id"])
	if !ok {
		t.Fatalf("expected a key id in the add output: %s", addOutput)
	}
	removed := false
	t.Cleanup(func() {
		if !removed {
			_, _ = executeLiveCLI(t, "--json", "ssh-key", "remove", keyID, "--yes")
		}
	})

	listOutput, err := executeLiveCLI(t, "--json", "ssh-key", "list", "--all")
	if err != nil {
		t.Fatalf("ssh-key list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, label) {
		t.Fatalf("expected the added key in the listing, got: %s", listOutput)
	}
	stored, ok := listedKeyByID(t, listOutput, keyID)
	if !ok {
		t.Fatalf("key %s is not in the listing: %s", keyID, listOutput)
	}
	if stored["label"] != label {
		t.Errorf("label = %v, want %s", stored["label"], label)
	}
	if stored["text"] != publicKey {
		t.Errorf("text = %v, want %s", stored["text"], publicKey)
	}

	if _, err := executeLiveCLI(t, "--json", "ssh-key", "remove", keyID, "--yes"); err != nil {
		t.Fatalf("ssh-key remove failed: %v", err)
	}
	removed = true

	afterRemove, err := executeLiveCLI(t, "--json", "ssh-key", "list", "--all")
	if err != nil {
		t.Fatalf("ssh-key list after remove failed: %v\noutput: %s", err, afterRemove)
	}
	if strings.Contains(afterRemove, label) {
		t.Fatalf("expected the key to be gone after remove, got: %s", afterRemove)
	}
	if _, ok := listedKeyByID(t, afterRemove, keyID); ok {
		t.Fatalf("key %s is still listed after remove: %s", keyID, afterRemove)
	}
}

// TestLiveRepositoryAccessKeyLifecycle covers bb repo ssh-key add/list/remove.
//
// Repository access keys are a different resource from personal keys: they carry
// a permission and hang off the repository, so the permission round-tripping is
// the part worth asserting.
func TestLiveRepositoryAccessKeyLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	label := testsupport.UniqueName("live-suite-access-")
	// Not the label as the comment, for the reason the personal key test gives.
	publicKey := generateSSHPublicKey(t, testsupport.UniqueName("live-suite-comment-"))

	addOutput, err := executeLiveCLI(t, "--json", "repo", "ssh-key", "add", publicKey,
		"--label", label, "--read-write", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo ssh-key add failed: %v\noutput: %s", err, addOutput)
	}
	if !strings.Contains(addOutput, "REPO_WRITE") {
		t.Fatalf("expected --read-write to produce a REPO_WRITE key, got: %s", addOutput)
	}

	addData := decodeJSONMap(t, addOutput)
	keyObject, ok := addData["key"].(map[string]any)
	if !ok {
		keyObject = addData
	}
	keyID, ok := numericOrStringID(keyObject["id"])
	if !ok {
		t.Fatalf("expected a key id in the add output: %s", addOutput)
	}
	removed := false
	t.Cleanup(func() {
		if !removed {
			_, _ = executeLiveCLI(t, "--json", "repo", "ssh-key", "remove", keyID, "--repo", repoRef, "--yes")
		}
	})

	listOutput, err := executeLiveCLI(t, "--json", "repo", "ssh-key", "list", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo ssh-key list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, label) {
		t.Fatalf("expected the added access key in the listing, got: %s", listOutput)
	}
	// A listing owes the caller meta.limitReached: without it, a truncated
	// page and a complete one are the same document. This one lost the key when
	// it moved to the typed payload, and only a live run reads what was written.
	if !strings.Contains(listOutput, `"limitReached"`) {
		t.Errorf("repo ssh-key list did not report whether the limit was reached: %s", listOutput)
	}

	// The permission as the repository's listing reports it, not as the add
	// answered: a key sent without one is granted REPO_READ, so REPO_WRITE here
	// is --read-write arriving.
	stored, ok := listedKeyByID(t, listOutput, keyID)
	if !ok {
		t.Fatalf("access key %s is not in the repository's listing: %s", keyID, listOutput)
	}
	if stored["permission"] != "REPO_WRITE" {
		t.Errorf("permission = %v, want REPO_WRITE", stored["permission"])
	}
	if stored["label"] != label {
		t.Errorf("label = %v, want %s", stored["label"], label)
	}
	if stored["text"] != publicKey {
		t.Errorf("text = %v, want %s", stored["text"], publicKey)
	}

	if _, err := executeLiveCLI(t, "--json", "repo", "ssh-key", "remove", keyID, "--repo", repoRef, "--yes"); err != nil {
		t.Fatalf("repo ssh-key remove failed: %v", err)
	}
	removed = true

	afterRemove, err := executeLiveCLI(t, "--json", "repo", "ssh-key", "list", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo ssh-key list after remove failed: %v\noutput: %s", err, afterRemove)
	}
	if strings.Contains(afterRemove, label) {
		t.Fatalf("expected the access key to be gone after remove, got: %s", afterRemove)
	}
	if _, ok := listedKeyByID(t, afterRemove, keyID); ok {
		t.Fatalf("access key %s is still listed after remove: %s", keyID, afterRemove)
	}
}

// listedKeyByID finds one key in a key listing's JSON output by its id.
func listedKeyByID(t *testing.T, output, id string) (map[string]any, bool) {
	t.Helper()

	var data any
	if err := decodeJSONEnvelopeData(output, &data); err != nil {
		t.Fatalf("the key listing is not JSON: %v\n%s", err, output)
	}

	return findByID(data, id)
}
