package webhookfields

import (
	"context"
	"encoding/json"
	"slices"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// ListPage reads one page of the webhooks in the scope a create targets.
type ListPage func(ctx context.Context, start, limit int) (openapi.Page[json.RawMessage], error)

// Create sends a webhook create and returns the webhook Bitbucket answered with.
type Create func(ctx context.Context, body openapigenerated.RestWebhook) (any, error)

// scopeLimit is how many of a scope's webhooks a check reads. A create beyond
// it is not found, which leaves the outcome unknown rather than inventing one.
const scopeLimit = 1000

// CreateChecked creates a webhook, and when the create ends with an unknown
// outcome finds out whether the webhook is there instead of passing the doubt
// on.
//
// A timeout, a lost connection, a gateway's 502 or 504, and the 400 Bitbucket
// sends when writing its answer failed all leave a create unconfirmed; the last
// comes after the webhook was stored. Exit 13 would send the caller to look, and
// bb can look itself. It lists the scope's webhooks before the create and again
// after, and exactly one new webhook with the requested name, URL and events is
// the one this create made. Both listings are needed: Bitbucket accepts
// identical webhooks, so a match alone can be one that was already there.
//
// Anything else stays unknown_outcome: nothing new, more than one, or a second
// listing that failed. The create is never sent again. A request Bitbucket is
// still processing can land after a check that missed it, and a second create
// would then be a duplicate.
func CreateChecked(ctx context.Context, input CreateInput, list ListPage, create Create) (any, error) {
	body, err := NewCreateBody(input)
	if err != nil {
		return nil, err
	}

	before, err := openapi.PageThrough(ctx, 0, scopeLimit, list)
	if err != nil {
		return nil, err
	}

	created, err := create(ctx, body)
	if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) {
		return created, err
	}

	after, listErr := openapi.PageThrough(ctx, 0, scopeLimit, list)
	if listErr != nil {
		return nil, err
	}
	if found, ok := newlyCreated(before, after, body); ok {
		return found, nil
	}

	return nil, err
}

// DecodePage reads one page of a webhook listing.
func DecodePage(body []byte) (openapi.Page[json.RawMessage], error) {
	var page struct {
		Values        []json.RawMessage `json:"values"`
		IsLastPage    *bool             `json:"isLastPage"`
		NextPageStart *int              `json:"nextPageStart"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return openapi.Page[json.RawMessage]{}, apperrors.New(apperrors.KindPermanent, "failed to decode the webhook listing", err)
	}

	return openapi.Page[json.RawMessage]{Values: page.Values, IsLastPage: page.IsLastPage, NextPageStart: page.NextPageStart}, nil
}

// listedWebhook is what telling webhooks apart takes. The generated
// RestWebhook has no id, and the id is the field that does it.
type listedWebhook struct {
	ID     *int64   `json:"id"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// newlyCreated finds the one webhook in after that was not in before and is the
// webhook body asks for, decoded the way a create's answer is.
func newlyCreated(before, after []json.RawMessage, body openapigenerated.RestWebhook) (any, bool) {
	existing := map[int64]bool{}
	for _, raw := range before {
		var hook listedWebhook
		if json.Unmarshal(raw, &hook) == nil && hook.ID != nil {
			existing[*hook.ID] = true
		}
	}

	var found json.RawMessage
	matches := 0
	for _, raw := range after {
		var hook listedWebhook
		if json.Unmarshal(raw, &hook) != nil || hook.ID == nil || existing[*hook.ID] {
			continue
		}
		if hook.Name == valueOf(body.Name) && hook.URL == valueOf(body.Url) && sameEvents(hook.Events, valueOf(body.Events)) {
			found = raw
			matches++
		}
	}
	if matches != 1 {
		return nil, false
	}

	// Cannot fail: found decoded into listedWebhook above.
	var payload any
	_ = json.Unmarshal(found, &payload)

	return payload, true
}

// sameEvents compares two event lists as sets; Bitbucket need not keep the
// order a create sent them in.
func sameEvents(listed, requested []string) bool {
	listed, requested = slices.Clone(listed), slices.Clone(requested)
	slices.Sort(listed)
	slices.Sort(requested)

	return slices.Equal(listed, requested)
}

func valueOf[T any](pointer *T) T {
	var zero T
	if pointer == nil {
		return zero
	}

	return *pointer
}
