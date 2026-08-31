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
	// BaseURL is the GitHub Pages host serving the documentation.
	//
	// The project was renamed from bitbucket-server-cli. GitHub redirects a
	// renamed repository but not its Pages site, so the former host is a hard
	// 404; tools/docs-lint fails the build if it reappears.
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
