package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

const binaryDigest = "599d0dc9270847b648feade729765abacfd17a1fb711e24da5725166c9935913"

var linuxRelease = expectation{version: "v4.1.0", goos: "linux", goarch: "amd64"}

func linuxBinary() *debug.BuildInfo {
	return &debug.BuildInfo{
		GoVersion: "go1.26.6",
		Main:      debug.Module{Path: "github.com/vriesdemichael/bitbucket-data-center-cli", Version: "v0.0.0-20260914195618-d14b6f5b671e"},
		Deps: []*debug.Module{
			{Path: "github.com/godbus/dbus/v5", Version: "v5.1.0"},
			{Path: "github.com/spf13/cobra", Version: "v1.10.2"},
		},
		Settings: []debug.BuildSetting{
			{Key: "GOOS", Value: "linux"},
			{Key: "GOARCH", Value: "amd64"},
			{Key: "vcs.modified", Value: "false"},
		},
	}
}

// describing returns the document syft writes for a scan of the binary info
// was read from: the binary as the described root, then each module it links.
func describing(info *debug.BuildInfo, digest string) document {
	doc := document{
		SPDXVersion: "SPDX-2.3",
		Packages: []spdxPackage{{
			SPDXID:           "SPDXRef-DocumentRoot-File-bb",
			Name:             "bb",
			VersionInfo:      "v4.1.0",
			LicenseConcluded: noAssertion,
			Checksums:        []checksum{{Algorithm: "SHA256", Value: digest}},
		}},
		Relationships: []relationship{{Element: "SPDXRef-DOCUMENT", Related: "SPDXRef-DocumentRoot-File-bb", Type: "DESCRIBES"}},
	}

	add := func(name, version, licence string) {
		doc.Packages = append(doc.Packages, goModule(name, version, licence))
	}
	add("stdlib", info.GoVersion, noAssertion)
	add(info.Main.Path, info.Main.Version, noAssertion)
	for _, dependency := range info.Deps {
		add(dependency.Path, dependency.Version, "Apache-2.0")
	}

	return doc
}

func goModule(name, version, licence string) spdxPackage {
	return spdxPackage{
		SPDXID:           "SPDXRef-Package-go-module-" + name,
		Name:             name,
		VersionInfo:      version,
		LicenseConcluded: licence,
		ExternalRefs:     []externalRef{{Type: "purl", Locator: "pkg:golang/" + name + "@" + version}},
	}
}

func withoutPackage(doc document, name string) document {
	kept := []spdxPackage{}
	for _, pkg := range doc.Packages {
		if pkg.Name != name {
			kept = append(kept, pkg)
		}
	}
	doc.Packages = kept

	return doc
}

func TestCheckAcceptsTheSBOMOfTheBinary(t *testing.T) {
	t.Parallel()

	info := linuxBinary()
	if problems := check(describing(info, binaryDigest), info, binaryDigest, linuxRelease); len(problems) != 0 {
		t.Fatalf("expected no problems, got %v", problems)
	}
}

// TestCheckFindsWhatAnSBOMGetsWrong is the sabotage: each case breaks one
// thing the attestation claims, and the check has to name it.
func TestCheckFindsWhatAnSBOMGetsWrong(t *testing.T) {
	t.Parallel()

	cobra := func(doc *document) *spdxPackage {
		for index := range doc.Packages {
			if doc.Packages[index].Name == "github.com/spf13/cobra" {
				return &doc.Packages[index]
			}
		}
		t.Fatal("the fixture lists no cobra")

		return nil
	}

	cases := map[string]struct {
		breaks func(doc *document, info *debug.BuildInfo, want *expectation)
		want   string
	}{
		"an older SPDX version": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) { doc.SPDXVersion = "SPDX-2.2" },
			`spdxVersion is "SPDX-2.2"`,
		},
		"a binary built for another platform": {
			func(_ *document, _ *debug.BuildInfo, want *expectation) { want.goarch = "arm64" },
			"built for linux/amd64, expected linux/arm64",
		},
		"a binary built from a modified checkout": {
			func(_ *document, info *debug.BuildInfo, _ *expectation) {
				info.Settings[2].Value = "true"
			},
			`vcs.modified is "true"`,
		},
		"a binary built without version control information": {
			func(_ *document, info *debug.BuildInfo, _ *expectation) { info.Settings = info.Settings[:2] },
			`vcs.modified is ""`,
		},
		"the scanned file under its runner path": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) { doc.Packages[0].Name = "/tmp/bb" },
			`names the binary "/tmp/bb"`,
		},
		"the binary without the release version": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) {
				doc.Packages[0].VersionInfo = "sha256:" + binaryDigest
			},
			`expected "v4.1.0"`,
		},
		"another binary": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) {
				doc.Packages[0].Checksums[0].Value = strings.Repeat("0", 64)
			},
			"the binary's is",
		},
		"no described element": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) { doc.Relationships = nil },
			"describes 0 elements",
		},
		"a described element that is not a package": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) {
				doc.Relationships[0].Related = "SPDXRef-DocumentRoot-Directory-dist"
			},
			"SPDXRef-DocumentRoot-Directory-dist, which is not one of its packages",
		},
		"a package from a scan of the checkout": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) {
				doc.Packages = append(doc.Packages, spdxPackage{
					Name:         "mkdocs",
					VersionInfo:  "1.6.1",
					ExternalRefs: []externalRef{{Type: "purl", Locator: "pkg:pypi/mkdocs@1.6.1"}},
				})
			},
			`mkdocs 1.6.1 is not a Go module (purl "pkg:pypi/mkdocs@1.6.1")`,
		},
		"a module only another platform links": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) {
				doc.Packages = append(doc.Packages, goModule("github.com/danieljoos/wincred", "v1.2.2", "MIT"))
			},
			"github.com/danieljoos/wincred v1.2.2 is listed, but the binary does not link it",
		},
		"a dependency left out": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) {
				*doc = withoutPackage(*doc, "github.com/godbus/dbus/v5")
			},
			"links github.com/godbus/dbus/v5 v5.1.0, which the SBOM does not list",
		},
		"the standard library left out": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) { *doc = withoutPackage(*doc, "stdlib") },
			"links stdlib go1.26.6, which the SBOM does not list",
		},
		"the main module left out": {
			func(doc *document, info *debug.BuildInfo, _ *expectation) {
				*doc = withoutPackage(*doc, info.Main.Path)
			},
			"links github.com/vriesdemichael/bitbucket-data-center-cli v0.0.0-20260914195618-d14b6f5b671e, which the SBOM does not list",
		},
		"a dependency at another version": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) { cobra(doc).VersionInfo = "v1.9.0" },
			"github.com/spf13/cobra is listed at v1.9.0, but the binary links v1.10.2",
		},
		"a dependency listed twice": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) {
				doc.Packages = append(doc.Packages, *cobra(doc))
			},
			"github.com/spf13/cobra is listed 2 times",
		},
		"a dependency without a licence": {
			func(doc *document, _ *debug.BuildInfo, _ *expectation) { cobra(doc).LicenseConcluded = noAssertion },
			"github.com/spf13/cobra v1.10.2 has no licence",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			info := linuxBinary()
			doc := describing(info, binaryDigest)
			want := linuxRelease
			testCase.breaks(&doc, info, &want)

			problems := check(doc, info, binaryDigest, want)
			for _, problem := range problems {
				if strings.Contains(problem, testCase.want) {
					return
				}
			}
			t.Fatalf("no problem contains %q: %v", testCase.want, problems)
		})
	}
}

type archiveEntry struct {
	name    string
	content []byte
}

func writeTarGz(t *testing.T, archivePath string, entries ...archiveEntry) {
	t.Helper()

	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(entry.content)), Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := writer.Write(entry.content); err != nil {
			t.Fatalf("write tar entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(archivePath, buffer.Bytes(), 0o600); err != nil {
		t.Fatalf("write %s: %v", archivePath, err)
	}
}

func writeZip(t *testing.T, archivePath string, entries ...archiveEntry) {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := file.Write(entry.content); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := os.WriteFile(archivePath, buffer.Bytes(), 0o600); err != nil {
		t.Fatalf("write %s: %v", archivePath, err)
	}
}

func TestArchivedBinaryReadsTheBinaryOutOfEitherArchiveFormat(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	tarball := filepath.Join(directory, "bb_4.1.0_linux_amd64.tar.gz")
	writeTarGz(t, tarball, archiveEntry{"bb", []byte("linux binary")})
	zipped := filepath.Join(directory, "bb_4.1.0_windows_amd64.zip")
	writeZip(t, zipped, archiveEntry{"bb.exe", []byte("windows binary")})

	for archivePath, want := range map[string]string{tarball: "linux binary", zipped: "windows binary"} {
		binary, err := archivedBinary(archivePath)
		if err != nil {
			t.Fatalf("archivedBinary(%s): %v", filepath.Base(archivePath), err)
		}
		if string(binary) != want {
			t.Errorf("archivedBinary(%s) = %q, want %q", filepath.Base(archivePath), binary, want)
		}
	}
}

func TestArchivedBinaryRefusesAnArchiveHoldingAnythingElse(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	extraFile := filepath.Join(directory, "extra.tar.gz")
	writeTarGz(t, extraFile, archiveEntry{"bb", []byte("binary")}, archiveEntry{"README.md", []byte("readme")})
	otherName := filepath.Join(directory, "other.zip")
	writeZip(t, otherName, archiveEntry{"tool.exe", []byte("binary")})
	empty := filepath.Join(directory, "empty.tar.gz")
	writeTarGz(t, empty)

	cases := map[string]string{
		extraFile:                               "holds 2 files [README.md bb]",
		otherName:                               "holds tool.exe",
		empty:                                   "holds 0 files",
		filepath.Join(directory, "bb.deb"):      "expected a .tar.gz or .zip archive",
		filepath.Join(directory, "gone.tar.gz"): "gone.tar.gz",
	}
	for archivePath, want := range cases {
		_, err := archivedBinary(archivePath)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("archivedBinary(%s) error = %v, want one containing %q", filepath.Base(archivePath), err, want)
		}
	}
}

// TestRunReadsTheBuildInformationOfARealBinary packs this test's own
// executable the way the release packs bb, so the path from archive to build
// information to digest runs against a binary the Go linker wrote.
func TestRunReadsTheBuildInformationOfARealBinary(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test executable: %v", err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatalf("read the test executable: %v", err)
	}
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		t.Fatalf("read the test executable's build information: %v", err)
	}
	sum := sha256.Sum256(binary)

	directory := t.TempDir()
	archivePath := filepath.Join(directory, "bb.tar.gz")
	writeTarGz(t, archivePath, archiveEntry{"bb", binary})

	raw, err := json.Marshal(struct {
		SPDXVersion   string         `json:"spdxVersion"`
		Packages      []spdxPackage  `json:"packages"`
		Relationships []relationship `json:"relationships"`
	}(describing(info, hex.EncodeToString(sum[:]))))
	if err != nil {
		t.Fatalf("encode the SBOM: %v", err)
	}
	sbomPath := filepath.Join(directory, "bb.spdx.json")
	if err := os.WriteFile(sbomPath, raw, 0o600); err != nil {
		t.Fatalf("write the SBOM: %v", err)
	}

	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}

	problems, err := run(sbomPath, archivePath, expectation{version: "v4.1.0", goos: settings["GOOS"], goarch: settings["GOARCH"]})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// go test does not stamp version control information, so its absence is
	// the one problem a test executable is allowed.
	for _, problem := range problems {
		if !strings.HasPrefix(problem, "vcs.modified") {
			t.Errorf("unexpected problem: %s", problem)
		}
	}
}
