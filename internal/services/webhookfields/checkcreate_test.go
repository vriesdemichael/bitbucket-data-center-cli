package webhookfields

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// That a create whose answer was lost shows up in the listing is Bitbucket's
// behaviour, and the live suite checks it against Bitbucket. What is checked
// here is bb's side: which webhook counts as the one the create made, that the
// create is sent once whatever the check finds, and that the webhook it made is
// the one read back.

func TestNewlyCreatedIsTheOneNewWebhookTheCreateAskedFor(t *testing.T) {
	t.Parallel()

	name, url, events := "deploy", "https://ci.example/hook", []string{"repo:refs_changed", "pr:merged"}
	body := openapigenerated.RestWebhook{Name: &name, Url: &url, Events: &events}

	hook := func(id int, name, url string, events ...string) json.RawMessage {
		raw, err := json.Marshal(map[string]any{"id": id, "name": name, "url": url, "events": events})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return raw
	}
	existing := hook(1, name, url, events...)
	notAWebhook := json.RawMessage(`"nope"`)

	for _, testCase := range []struct {
		name          string
		before, after []json.RawMessage
		wantID        int
	}{
		{"one new match, events in another order", nil, []json.RawMessage{hook(7, name, url, "pr:merged", "repo:refs_changed")}, 7},
		{"an identical webhook that was already there", []json.RawMessage{existing}, []json.RawMessage{existing}, 0},
		{"a new match beside an identical old one", []json.RawMessage{existing}, []json.RawMessage{existing, hook(8, name, url, events...)}, 8},
		{"two new matches", nil, []json.RawMessage{hook(7, name, url, events...), hook(8, name, url, events...)}, 0},
		{"a new webhook for another URL", nil, []json.RawMessage{hook(7, name, "https://other.example", events...)}, 0},
		{"a new webhook with other events", nil, []json.RawMessage{hook(7, name, url, "repo:refs_changed")}, 0},
		{"entries that are not webhooks", []json.RawMessage{notAWebhook}, []json.RawMessage{notAWebhook, json.RawMessage(`{"name":"deploy"}`)}, 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			found, ok := newlyCreated(testCase.before, testCase.after, body)
			if ok != (testCase.wantID != 0) {
				t.Fatalf("found = %v, want %v", ok, testCase.wantID != 0)
			}
			if !ok {
				return
			}
			if id, _ := found.(map[string]any)["id"].(float64); int(id) != testCase.wantID {
				t.Fatalf("found webhook %v, want id %d", found, testCase.wantID)
			}
		})
	}
}

func TestCreateCheckedChecksAndNeverSendsTheCreateTwice(t *testing.T) {
	t.Parallel()

	input := CreateInput{Name: "deploy", URL: "https://ci.example/hook"}
	unknown := apperrors.New(apperrors.KindUnknownOutcome, "the answer to the create was lost", nil)
	last := true
	pageOf := func(values ...json.RawMessage) openapi.Page[json.RawMessage] {
		return openapi.Page[json.RawMessage]{Values: values, IsLastPage: &last}
	}
	appeared := json.RawMessage(`{"id":7,"name":"deploy","url":"https://ci.example/hook","events":["repo:refs_changed"]}`)

	// listings answers each listing in turn, and fails a listing it has no
	// answer for.
	listings := func(calls *int, answers ...openapi.Page[json.RawMessage]) ListPage {
		return func(context.Context, int, int) (openapi.Page[json.RawMessage], error) {
			*calls++
			if *calls > len(answers) {
				return openapi.Page[json.RawMessage]{}, errors.New("listing failed")
			}
			return answers[*calls-1], nil
		}
	}
	creating := func(calls *int, err error) Create {
		return func(context.Context, openapigenerated.RestWebhook) (any, error) {
			*calls++
			return nil, err
		}
	}
	// reading answers every read with a webhook marked as read, and records
	// which ids were asked for.
	reading := func(asked *[]string) Get {
		return func(_ context.Context, id string) (any, error) {
			*asked = append(*asked, id)
			return map[string]any{"id": id, "read": true}, nil
		}
	}
	// notReading fails the test when a read is made: there is no webhook to
	// read after a create that did not make one.
	notReading := func(t *testing.T) Get {
		return func(context.Context, string) (any, error) {
			t.Error("read back a webhook the create did not make")
			return nil, errors.New("unexpected read")
		}
	}
	wasRead := func(written Written) bool {
		read, _ := written.Webhook.(map[string]any)["read"].(bool)
		return read && written.Unread == nil
	}

	t.Run("a webhook that appeared is the answer, as a read returns it", func(t *testing.T) {
		t.Parallel()

		var lists, creates int
		var asked []string
		written, err := CreateChecked(context.Background(), input, listings(&lists, pageOf(), pageOf(appeared)), creating(&creates, unknown), reading(&asked))
		if err != nil || creates != 1 {
			t.Fatalf("got %v after %d creates, want the webhook after one", err, creates)
		}
		if !slices.Equal(asked, []string{"7"}) || !wasRead(written) {
			t.Fatalf("read back %v and published %+v, want webhook 7 as its read returned it", asked, written)
		}
	})

	t.Run("a webhook the create answered with is read back by its id", func(t *testing.T) {
		t.Parallel()

		answering := func(context.Context, openapigenerated.RestWebhook) (any, error) {
			return map[string]any{"id": float64(9), "configuration": map[string]any{}}, nil
		}
		var lists int
		var asked []string
		written, err := CreateChecked(context.Background(), input, listings(&lists, pageOf()), answering, reading(&asked))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if !slices.Equal(asked, []string{"9"}) || !wasRead(written) {
			t.Fatalf("read back %v and published %+v, want webhook 9 as its read returned it", asked, written)
		}
	})

	t.Run("nothing appeared: still unknown, and not sent again", func(t *testing.T) {
		t.Parallel()

		var lists, creates int
		_, err := CreateChecked(context.Background(), input, listings(&lists, pageOf(), pageOf()), creating(&creates, unknown), notReading(t))
		if !errors.Is(err, unknown) || creates != 1 {
			t.Fatalf("got %v after %d creates, want the unknown outcome after one", err, creates)
		}
	})

	t.Run("a check that cannot list stays unknown", func(t *testing.T) {
		t.Parallel()

		var lists, creates int
		_, err := CreateChecked(context.Background(), input, listings(&lists, pageOf()), creating(&creates, unknown), notReading(t))
		if !errors.Is(err, unknown) || creates != 1 {
			t.Fatalf("got %v after %d creates, want the unknown outcome after one", err, creates)
		}
	})

	t.Run("a create that failed plainly is not checked", func(t *testing.T) {
		t.Parallel()

		refused := apperrors.New(apperrors.KindAuthorization, "not allowed", nil)
		var lists, creates int
		_, err := CreateChecked(context.Background(), input, listings(&lists, pageOf()), creating(&creates, refused), notReading(t))
		if !errors.Is(err, refused) || lists != 1 {
			t.Fatalf("got %v after %d listings, want the refusal after the one before the create", err, lists)
		}
	})

	t.Run("nothing is sent when the listing before fails", func(t *testing.T) {
		t.Parallel()

		var lists, creates int
		if _, err := CreateChecked(context.Background(), input, listings(&lists), creating(&creates, nil), notReading(t)); err == nil || creates != 0 {
			t.Fatalf("got %v after %d creates, want the listing error and no create", err, creates)
		}
	})

	t.Run("nothing is sent for a body the create would refuse", func(t *testing.T) {
		t.Parallel()

		var lists, creates int
		_, err := CreateChecked(context.Background(), CreateInput{}, listings(&lists, pageOf()), creating(&creates, nil), notReading(t))
		if !apperrors.IsKind(err, apperrors.KindValidation) || lists != 0 || creates != 0 {
			t.Fatalf("got %v after %d listings and %d creates, want validation and no requests", err, lists, creates)
		}
	})
}

func TestDecodePageReadsAWebhookListing(t *testing.T) {
	t.Parallel()

	page, err := DecodePage([]byte(`{"values":[{"id":1}],"isLastPage":false,"nextPageStart":25}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Values) != 1 || page.IsLastPage == nil || *page.IsLastPage || page.NextPageStart == nil || *page.NextPageStart != 25 {
		t.Fatalf("decoded %+v", page)
	}

	if _, err := DecodePage([]byte("<html>")); !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("a body that is not a listing: got %v, want permanent", err)
	}
}
