package pullrequestactivity

import (
	"context"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// The review endpoint serialises an inline draft's anchor path as a plain
// string, as the timeline does, and one such draft made the whole review fail
// to decode (OPENAPI-037).
func TestDecodeReviewPageAcceptsStringAnchorPaths(t *testing.T) {
	t.Parallel()

	page, err := decodeReviewPage([]byte(`{"isLastPage":false,"nextPageStart":2,"values":[
      {"id":10,"text":"needs a guard","state":"PENDING","anchor":{"line":7,"lineType":"ADDED","path":"internal/cli/root.go"}},
      {"id":20,"text":"published","state":"OPEN","anchor":{"line":3,"path":{"parent":"docs","name":"README.md"}},
        "comments":[{"id":21,"text":"a draft reply","state":"PENDING","anchor":{"line":3,"path":"docs/README.md"}}]}
    ]}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(page.Values) != 2 {
		t.Fatalf("decoded %d threads, want 2", len(page.Values))
	}
	stringAnchor := page.Values[0].Anchor
	if stringAnchor == nil || stringAnchor.Path == nil || stringAnchor.Path.Name == nil || *stringAnchor.Path.Name != "root.go" ||
		stringAnchor.Path.Parent == nil || *stringAnchor.Path.Parent != "internal/cli" {
		t.Errorf("the string path did not come back as its object form: %#v", stringAnchor)
	}
	replies := page.Values[1].Comments
	if replies == nil || len(*replies) != 1 || (*replies)[0].Anchor == nil || (*replies)[0].Anchor.Path == nil ||
		(*replies)[0].Anchor.Path.Name == nil || *(*replies)[0].Anchor.Path.Name != "README.md" {
		t.Errorf("the reply's string path did not come back as its object form: %#v", page.Values[1].Comments)
	}
	if page.IsLastPage == nil || *page.IsLastPage || page.NextPageStart == nil || *page.NextPageStart != 2 {
		t.Errorf("the page's position was lost: last=%v next=%v", page.IsLastPage, page.NextPageStart)
	}
}

// A review of no repository, or of no pull request, is refused before any
// request: the client points at a port nothing listens on.
func TestReviewRefusesATargetItCannotName(t *testing.T) {
	t.Parallel()

	client, err := openapigenerated.NewClientWithResponses("http://127.0.0.1:1/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	service := NewService(client)

	if _, err := service.Review(context.Background(), RepositoryRef{}, "12", ListOptions{MaxResults: 1}); err == nil {
		t.Error("a review of no repository was not refused")
	}
	if _, err := service.Review(context.Background(), RepositoryRef{ProjectKey: "PAY", Slug: "ledger"}, "twelve", ListOptions{MaxResults: 1}); err == nil {
		t.Error("a review of a pull request id that is not a number was not refused")
	}
}

// A page that is not a review is an error, not an empty review.
func TestDecodeReviewPageRefusesWhatIsNotAReview(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"not JSON":                    `<html>`,
		"a comment that is no object": `{"isLastPage":true,"values":[{"id":"ten"}]}`,
	} {
		if _, err := decodeReviewPage([]byte(body)); err == nil {
			t.Errorf("%s: decoded without an error", name)
		}
	}
}
