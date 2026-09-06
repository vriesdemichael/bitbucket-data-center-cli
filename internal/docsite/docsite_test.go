package docsite_test

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/docsite"
)

// The project was renamed; GitHub redirects the repository but not the Pages
// site, so the former host is a hard 404 wherever it survives.
func TestBaseURLNamesTheCurrentPagesSite(t *testing.T) {
	t.Parallel()

	if strings.Contains(docsite.BaseURL, docsite.RetiredSlug) {
		t.Fatalf("BaseURL still names the retired Pages host: %s", docsite.BaseURL)
	}
	if want := "https://vriesdemichael.github.io/bitbucket-data-center-cli"; docsite.BaseURL != want {
		t.Fatalf("BaseURL = %q, want %q", docsite.BaseURL, want)
	}
}

func TestURLPlacesThePathUnderTheVersion(t *testing.T) {
	t.Parallel()

	got := docsite.URL("v4.0.0", "reference/schemas/output/output.pr.get.schema.json")
	want := "https://vriesdemichael.github.io/bitbucket-data-center-cli/v4.0.0/reference/schemas/output/output.pr.get.schema.json"
	if got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}
