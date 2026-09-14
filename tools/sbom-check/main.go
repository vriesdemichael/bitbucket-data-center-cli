// Command sbom-check verifies that an SPDX SBOM describes the binary packed in
// a release archive.
//
// The release attests each archive with its SBOM, which claims the SBOM lists
// what that archive's binary is built from. syft writes the document and
// nothing else reads it before it ships, so this holds it to the binary's own
// build information -- written by the Go linker, which syft only interprets:
// the same file, the same modules at the same versions, the standard library
// the binary was linked with, nothing that is not a Go module, and a licence
// for every dependency.
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
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"runtime/debug"
	"slices"
	"strings"
)

// describedName is the name the release gives syft for the scanned binary, so
// the SBOM names the program rather than a path on the build runner.
const describedName = "bb"

const noAssertion = "NOASSERTION"

// maxBinaryBytes bounds what is read out of an archive. bb is tens of
// megabytes; an entry past this is not a release binary.
const maxBinaryBytes = 512 << 20

type expectation struct {
	version string
	goos    string
	goarch  string
}

type document struct {
	SPDXVersion   string         `json:"spdxVersion"`
	Packages      []spdxPackage  `json:"packages"`
	Relationships []relationship `json:"relationships"`
}

type spdxPackage struct {
	SPDXID           string        `json:"SPDXID"`
	Name             string        `json:"name"`
	VersionInfo      string        `json:"versionInfo"`
	LicenseConcluded string        `json:"licenseConcluded"`
	Checksums        []checksum    `json:"checksums"`
	ExternalRefs     []externalRef `json:"externalRefs"`
}

type checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"checksumValue"`
}

type externalRef struct {
	Type    string `json:"referenceType"`
	Locator string `json:"referenceLocator"`
}

type relationship struct {
	Element string `json:"spdxElementId"`
	Related string `json:"relatedSpdxElement"`
	Type    string `json:"relationshipType"`
}

func main() {
	sbomPath := flag.String("sbom", "", "SPDX JSON document to check")
	archivePath := flag.String("archive", "", "Release archive (.tar.gz or .zip) holding the binary the SBOM describes")
	version := flag.String("version", "", "Release version the SBOM must give the binary")
	goos := flag.String("goos", "", "GOOS the binary must be built for")
	goarch := flag.String("goarch", "", "GOARCH the binary must be built for")
	flag.Parse()

	if *sbomPath == "" || *archivePath == "" || *version == "" || *goos == "" || *goarch == "" {
		fmt.Fprintln(os.Stderr, "sbom-check: -sbom, -archive, -version, -goos and -goarch are all required")
		os.Exit(2)
	}

	problems, err := run(*sbomPath, *archivePath, expectation{version: *version, goos: *goos, goarch: *goarch})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbom-check: %v\n", err)
		os.Exit(1)
	}
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "%s does not describe the binary in %s:\n", *sbomPath, *archivePath)
		for _, problem := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", problem)
		}
		os.Exit(1)
	}

	fmt.Printf("%s describes the binary in %s\n", *sbomPath, *archivePath)
}

func run(sbomPath, archivePath string, want expectation) ([]string, error) {
	binary, err := archivedBinary(archivePath)
	if err != nil {
		return nil, err
	}

	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		return nil, fmt.Errorf("read the build information of the binary in %s: %w", archivePath, err)
	}

	raw, err := os.ReadFile(sbomPath)
	if err != nil {
		return nil, err
	}
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", sbomPath, err)
	}

	digest := sha256.Sum256(binary)

	return check(doc, info, hex.EncodeToString(digest[:]), want), nil
}

// check holds the SBOM to the binary's build information and returns every
// way it differs, in a stable order.
func check(doc document, info *debug.BuildInfo, digest string, want expectation) []string {
	problems := []string{}

	if doc.SPDXVersion != "SPDX-2.3" {
		problems = append(problems, fmt.Sprintf("spdxVersion is %q, expected SPDX-2.3", doc.SPDXVersion))
	}

	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != want.goos || settings["GOARCH"] != want.goarch {
		problems = append(problems, fmt.Sprintf("the binary is built for %s/%s, expected %s/%s", settings["GOOS"], settings["GOARCH"], want.goos, want.goarch))
	}
	// The go command stamps vcs.modified=true on a binary built from a checkout
	// with changes or untracked files, and the module version it records then
	// names a commit the binary was not built from.
	if settings["vcs.modified"] != "false" {
		problems = append(problems, fmt.Sprintf("vcs.modified is %q; build the binary from an unmodified checkout", settings["vcs.modified"]))
	}

	described, describedProblems := describedPackage(doc)
	problems = append(problems, describedProblems...)
	describedID := ""
	if described != nil {
		describedID = described.SPDXID
		if described.Name != describedName {
			problems = append(problems, fmt.Sprintf("the SBOM names the binary %q, expected %q", described.Name, describedName))
		}
		if described.VersionInfo != want.version {
			problems = append(problems, fmt.Sprintf("the SBOM gives the binary version %q, expected %q", described.VersionInfo, want.version))
		}
		if sum := sha256Of(*described); sum != digest {
			problems = append(problems, fmt.Sprintf("the SBOM describes a file with SHA-256 %q, the binary's is %q", sum, digest))
		}
	}

	// Everything the binary links, as syft names it: the standard library at
	// the toolchain version, the main module, and each dependency.
	linked := map[string]string{"stdlib": info.GoVersion, info.Main.Path: info.Main.Version}
	for _, dependency := range info.Deps {
		linked[dependency.Path] = dependency.Version
	}

	listed := map[string]int{}
	for _, pkg := range doc.Packages {
		if pkg.SPDXID == describedID {
			continue
		}

		purl := purlOf(pkg)
		if !strings.HasPrefix(purl, "pkg:golang/") {
			problems = append(problems, fmt.Sprintf("%s %s is not a Go module (purl %q); the binary links only Go modules", pkg.Name, pkg.VersionInfo, purl))
			continue
		}

		listed[pkg.Name]++
		version, isLinked := linked[pkg.Name]
		switch {
		case !isLinked:
			problems = append(problems, fmt.Sprintf("%s %s is listed, but the binary does not link it", pkg.Name, pkg.VersionInfo))
		case pkg.VersionInfo != version:
			problems = append(problems, fmt.Sprintf("%s is listed at %s, but the binary links %s", pkg.Name, pkg.VersionInfo, version))
		case pkg.Name != "stdlib" && pkg.Name != info.Main.Path && (pkg.LicenseConcluded == "" || pkg.LicenseConcluded == noAssertion):
			problems = append(problems, fmt.Sprintf("%s %s has no licence; syft reads it from the module cache, so generate the SBOM where the build downloaded the modules", pkg.Name, pkg.VersionInfo))
		}
	}

	for _, name := range slices.Sorted(maps.Keys(linked)) {
		switch count := listed[name]; {
		case count == 0:
			problems = append(problems, fmt.Sprintf("the binary links %s %s, which the SBOM does not list", name, linked[name]))
		case count > 1:
			problems = append(problems, fmt.Sprintf("%s is listed %d times", name, count))
		}
	}

	return problems
}

// describedPackage returns the one package the document says it describes,
// which for a scan of a file is the file.
func describedPackage(doc document) (*spdxPackage, []string) {
	described := []string{}
	for _, rel := range doc.Relationships {
		if rel.Type == "DESCRIBES" && rel.Element == "SPDXRef-DOCUMENT" {
			described = append(described, rel.Related)
		}
	}
	if len(described) != 1 {
		return nil, []string{fmt.Sprintf("the document describes %d elements, expected exactly one: the binary", len(described))}
	}

	for index := range doc.Packages {
		if doc.Packages[index].SPDXID == described[0] {
			return &doc.Packages[index], nil
		}
	}

	return nil, []string{fmt.Sprintf("the document describes %s, which is not one of its packages", described[0])}
}

func purlOf(pkg spdxPackage) string {
	for _, ref := range pkg.ExternalRefs {
		if ref.Type == "purl" {
			return ref.Locator
		}
	}

	return ""
}

func sha256Of(pkg spdxPackage) string {
	for _, sum := range pkg.Checksums {
		if sum.Algorithm == "SHA256" {
			return sum.Value
		}
	}

	return ""
}

// archivedBinary returns the one file a release archive holds.
func archivedBinary(archivePath string) ([]byte, error) {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return binaryFromTarGz(archivePath)
	case strings.HasSuffix(archivePath, ".zip"):
		return binaryFromZip(archivePath)
	default:
		return nil, fmt.Errorf("%s: expected a .tar.gz or .zip archive", archivePath)
	}
}

func binaryFromTarGz(archivePath string) ([]byte, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	compressed, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", archivePath, err)
	}
	defer func() { _ = compressed.Close() }()

	entries := map[string][]byte{}
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", archivePath, err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		content, err := readLimited(reader, header.Name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", archivePath, err)
		}
		entries[header.Name] = content
	}

	return onlyBinary(archivePath, entries)
}

func binaryFromZip(archivePath string) ([]byte, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = archive.Close() }()

	entries := map[string][]byte{}
	for _, entry := range archive.File {
		if !entry.Mode().IsRegular() {
			continue
		}

		content, err := readZipEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", archivePath, err)
		}
		entries[entry.Name] = content
	}

	return onlyBinary(archivePath, entries)
}

func readZipEntry(entry *zip.File) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	return readLimited(reader, entry.Name)
}

func readLimited(reader io.Reader, name string) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxBinaryBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxBinaryBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, maxBinaryBytes)
	}

	return content, nil
}

// onlyBinary returns the binary from an archive's files, refusing an archive
// that holds anything else: the SBOM describes one binary, so an archive with
// more in it ships something the SBOM does not cover.
func onlyBinary(archivePath string, entries map[string][]byte) ([]byte, error) {
	names := slices.Sorted(maps.Keys(entries))
	if len(names) != 1 {
		return nil, fmt.Errorf("%s holds %d files %v, expected only the bb binary", archivePath, len(names), names)
	}
	if base := path.Base(names[0]); base != "bb" && base != "bb.exe" {
		return nil, fmt.Errorf("%s holds %s, expected only the bb binary", archivePath, names[0])
	}

	return entries[names[0]], nil
}
