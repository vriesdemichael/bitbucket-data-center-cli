// Package docsite knows where the published documentation lives.
//
// The site is versioned by mike: each release is published under its own tag
// and the "latest" alias is repointed at it, so one document is reachable at
// two addresses. A schema's $id is its canonical identity rather than its
// address, so it names the version the snapshot was published under. Naming
// the alias instead would have every release assert the same identity for a
// different document, which is what a validator resolves $ref against and
// caches by.
package docsite

const (
	// RetiredSlug is the name the project carried before it was renamed.
	//
	// This is the one place in the repository that writes it. GitHub redirects a
	// renamed repository but not its Pages site, so the former host is a hard
	// 404, and the name kept coming back because it sat in front of everyone who
	// read the code -- it reached the AGENT NOTICE, the installation docs and the
	// Codecov upload slug, and that last one discarded coverage reports for two
	// months. tools/docs-lint reads this constant and fails the build wherever
	// the name reappears, so anything that needs to name it should import it
	// rather than spell it out and become the next place it leaks from.
	RetiredSlug = "bitbucket-server-cli"

	// BaseURL is the GitHub Pages host serving the documentation.
	BaseURL = "https://vriesdemichael.github.io/bitbucket-data-center-cli"

	// LatestVersion is the mike alias that always resolves to the newest
	// release. Artifacts committed to the repository carry it, because the
	// version they will be published under is not known until release time.
	LatestVersion = "latest"
)

// URL returns the address of docPath within the given version of the site.
func URL(siteVersion, docPath string) string {
	return BaseURL + "/" + siteVersion + "/" + docPath
}
