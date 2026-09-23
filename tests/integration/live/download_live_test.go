//go:build live

package live_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liveBinaryFile is a file no text handling passes through unchanged. It opens
// with a newline and ends in whitespace, which trimming drops; it carries NUL,
// CR LF and bytes that are not UTF-8, which a JSON string or a line-ending
// conversion alters; and it is longer than one read, so it arrives in pieces.
// The NUL near the start is also what makes git store it as binary.
func liveBinaryFile() []byte {
	content := []byte{'\n', 0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x00, 0xff, 0xfe}
	for index := range 100_000 {
		content = append(content, byte(index*37+index/256))
	}

	return append(content, '\r', '\n', '\n', ' ', '\t')
}

// seedBinaryFile seeds a repository and commits liveBinaryFile to it at path.
func seedBinaryFile(t *testing.T, path string) (repoRef string, binary []byte) {
	t.Helper()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed the repository: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	binary = liveBinaryFile()
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", path, string(binary)); err != nil {
		t.Fatalf("push %s: %v", path, err)
	}

	return seeded.Key + "/" + repo.Slug, binary
}

// zipEntries reads every file out of a zip archive, keyed by its path.
func zipEntries(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("the archive is not a zip (%d bytes): %v", len(archive), err)
	}

	entries := map[string][]byte{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			t.Fatalf("open %s in the archive: %v", file.Name, err)
		}
		content, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			t.Fatalf("read %s from the archive: %v", file.Name, err)
		}
		entries[strings.TrimPrefix(file.Name, "./")] = content
	}

	return entries
}

// TestLiveRawFileCommandsWriteTheBytesExactly: bb repo cat and bb repo browse
// raw write a binary file to stdout byte for byte, and under --json return it
// as base64 that decodes to the same bytes.
func TestLiveRawFileCommandsWriteTheBytesExactly(t *testing.T) {
	t.Parallel()

	repoRef, binary := seedBinaryFile(t, "assets/logo.bin")

	for _, command := range [][]string{
		{"repo", "cat", "assets/logo.bin", "--repo", repoRef},
		{"repo", "browse", "raw", "assets/logo.bin", "--repo", repoRef},
	} {
		stdout, stderr, err := executeLiveCLISplit(t, "", command...)
		if err != nil {
			t.Fatalf("%s failed: %v\nstderr: %s", strings.Join(command[:3], " "), err, stderr)
		}
		if !bytes.Equal([]byte(stdout), binary) {
			t.Fatalf("%s wrote %d bytes that differ from the %d pushed", strings.Join(command[:3], " "), len(stdout), len(binary))
		}
	}

	for name, output := range map[string]string{
		"repo cat":        mustLiveCLI(t, "repo", "cat", "assets/logo.bin", "--repo", repoRef),
		"repo browse raw": mustLiveCLI(t, "repo", "browse", "raw", "assets/logo.bin", "--repo", repoRef),
	} {
		document := decodeJSONMap(t, output)
		if document["encoding"] != "base64" {
			t.Fatalf("%s --json encoded a file that is not text as %v", name, document["encoding"])
		}
		content, _ := document["content"].(string)
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil || !bytes.Equal(decoded, binary) {
			t.Fatalf("%s --json content decodes to %d bytes (%v) that differ from the %d pushed", name, len(decoded), err, len(binary))
		}
	}
}

// TestLiveAPIWritesABinaryBodyExactly: bb api writes a body that is not text
// byte for byte. Here that is a file's raw bytes, which it used to trim and end
// with a newline -- dropping the whitespace this file opens and ends with.
func TestLiveAPIWritesABinaryBodyExactly(t *testing.T) {
	t.Parallel()

	repoRef, binary := seedBinaryFile(t, "assets/logo.bin")
	projectKey, slug, _ := strings.Cut(repoRef, "/")
	rawPath := "/rest/api/latest/projects/" + projectKey + "/repos/" + slug + "/raw/assets/logo.bin"

	stdout, stderr, err := executeLiveCLISplit(t, "", "api", rawPath)
	if err != nil {
		t.Fatalf("bb api %s failed: %v\nstderr: %s", rawPath, err, stderr)
	}
	if !bytes.Equal([]byte(stdout), binary) {
		t.Fatalf("bb api wrote %d bytes that differ from the %d pushed", len(stdout), len(binary))
	}

	// Text is still formatted: a JSON answer is indented and ends in one
	// newline, as it always has been.
	output := mustLiveHumanCLI(t, "api", "/rest/api/latest/projects/"+projectKey)
	if !strings.HasPrefix(output, "{\n  ") || !strings.HasSuffix(output, "}\n") || strings.HasSuffix(output, "\n\n") {
		t.Fatalf("a JSON answer was not indented and ended with one newline:\n%q", output)
	}
}

// TestLiveRepoArchiveHoldsTheRepositoryExactly: bb repo archive writes the
// repository's tree, a binary file among it byte for byte, to a file and to
// standard output. The file is put in place whole: nothing is left beside it.
func TestLiveRepoArchiveHoldsTheRepositoryExactly(t *testing.T) {
	t.Parallel()

	repoRef, binary := seedBinaryFile(t, "assets/logo.bin")

	directory := t.TempDir()
	archivePath := filepath.Join(directory, "snapshot.zip")
	output, err := executeLiveCLI(t, "repo", "archive", "--repo", repoRef, "--format", "zip", "-o", archivePath)
	if err != nil {
		t.Fatalf("repo archive failed: %v\noutput: %s", err, output)
	}

	written, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("expected an archive at %s: %v", archivePath, err)
	}
	entries := zipEntries(t, written)
	if !bytes.Equal(entries["assets/logo.bin"], binary) {
		t.Fatalf("assets/logo.bin in the archive is %d bytes that differ from the %d pushed", len(entries["assets/logo.bin"]), len(binary))
	}
	if _, ok := entries["seed.txt"]; !ok {
		t.Fatalf("the archive holds %d files and not the seeded seed.txt", len(entries))
	}
	if listed, _ := os.ReadDir(directory); len(listed) != 1 {
		t.Fatalf("the archive's directory holds %d entries, want the archive alone", len(listed))
	}

	stdout, stderr, err := executeLiveCLISplit(t, "", "repo", "archive", "--repo", repoRef, "--format", "zip", "-o", "-")
	if err != nil {
		t.Fatalf("repo archive -o - failed: %v\nstderr: %s", err, stderr)
	}
	if streamed := zipEntries(t, []byte(stdout)); !bytes.Equal(streamed["assets/logo.bin"], binary) {
		t.Fatalf("assets/logo.bin in the streamed archive is %d bytes that differ from the %d pushed", len(streamed["assets/logo.bin"]), len(binary))
	}
}
