package main

// fixtureReleaseVersion is the version the documents written for these tests
// name in their envelopes.
//
// lintMarkdown resolves the version from the repository's newest release tag,
// so a fixture saying v4.0.0 passed or failed according to which tags the
// machine happened to have. That is not a difference between a working tree
// and a broken one: CI clones with fetch-depth 1, finds no tags and skips the
// version check entirely, while a developer with tags fetched saw ten failures
// from the moment v4.1.0 was cut -- red locally, green in CI, for six days.
//
// The tests that lint the repository's own documentation still resolve the
// real version, because the thing they check is that those documents name it.
const fixtureReleaseVersion = "4.0.0"

// lintFixture lints a document written for these tests.
func lintFixture(file, contents string) ([]finding, int) {
	return lintMarkdownWithVersion(file, contents, fixtureReleaseVersion)
}
