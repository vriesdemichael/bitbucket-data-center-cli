package result

import (
	"encoding/json"
)

// Webhook is one webhook, on a repository or on a project.
//
// The services hand these back as an untyped any decoded from Bitbucket, so
// this is where the shape is decided rather than inherited. Both the repository
// commands and the project ones read it: they configure the same object through
// two endpoints, and each used to publish whatever arrived.
type Webhook struct {
	ID     int      `json:"id,omitempty" jsonschema:"Webhook identifier, which get, update and delete address."`
	Name   string   `json:"name,omitempty" jsonschema:"Webhook name."`
	URL    string   `json:"url,omitempty" jsonschema:"Endpoint Bitbucket posts to."`
	Active bool     `json:"active" jsonschema:"Whether the webhook currently fires."`
	Events []string `json:"events,omitempty" jsonschema:"Event keys the webhook subscribes to, for example repo:refs_changed."`
}

// WebhookFrom decodes one webhook out of the untyped payload a service returns.
//
// A round trip through JSON rather than a field-by-field copy, because the
// input is already a decoded any: there are no fields to copy from, only a map.
func WebhookFrom(payload any) Webhook {
	if payload == nil {
		return Webhook{}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return Webhook{}
	}

	var converted Webhook
	if err := json.Unmarshal(raw, &converted); err != nil {
		return Webhook{}
	}

	return converted
}

// WebhooksFrom decodes a list, never returning nil.
//
// Bitbucket answers the listing endpoints two ways depending on version: a bare
// array, or a paginated object with the webhooks under values. Both are handled
// here, which the human renderers already did -- and the JSON paths did not,
// because they published whatever arrived without looking. A caller therefore
// got an array from one instance and a pagination envelope from another, for
// the same command.
func WebhooksFrom(payload any) []Webhook {
	converted := []Webhook{}
	if payload == nil {
		return converted
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return converted
	}

	if err := json.Unmarshal(raw, &converted); err == nil && len(converted) > 0 {
		return converted
	}

	var paginated struct {
		Values []Webhook `json:"values"`
	}
	if err := json.Unmarshal(raw, &paginated); err == nil && paginated.Values != nil {
		return paginated.Values
	}

	return []Webhook{}
}

// PageOfWebhooks applies --start and --limit to a list the service returned
// whole.
//
// Bitbucket's webhook endpoints do not page, so bb does it. Both renderings
// read the paged list: applying it to only the human one -- which is what the
// repository and project listings each did -- made --json and the table answer
// differently to the same flags.
func PageOfWebhooks(webhooks []Webhook, start int, limit int) []Webhook {
	if start < 0 {
		start = 0
	}
	if start >= len(webhooks) {
		return []Webhook{}
	}

	end := start + limit
	if limit <= 0 || end > len(webhooks) {
		end = len(webhooks)
	}

	return webhooks[start:end]
}
