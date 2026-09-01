package webhookcmd

import (
	"encoding/json"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// Webhook is one repository webhook.
//
// The service hands back an untyped any decoded from Bitbucket, and the human
// path already re-decoded it into this shape to render a row. Only the JSON
// path published the raw object -- so the two renderings of the same webhook
// disagreed about what a webhook is, and the JSON one had no contract at all.
// Both read this now.
type Webhook struct {
	ID     int      `json:"id,omitempty" jsonschema:"Webhook identifier, for bb webhook get, update and delete."`
	Name   string   `json:"name,omitempty" jsonschema:"Webhook name."`
	URL    string   `json:"url,omitempty" jsonschema:"Endpoint Bitbucket posts to."`
	Active bool     `json:"active" jsonschema:"Whether the webhook currently fires."`
	Events []string `json:"events,omitempty" jsonschema:"Event keys the webhook subscribes to, for example repo:refs_changed."`
}

// SingleWebhook is what `bb webhook get` returns.
type SingleWebhook struct {
	Webhook Webhook `json:"webhook"`
}

// Webhooks is what `bb webhook list` returns.
type Webhooks struct {
	Webhooks []Webhook `json:"webhooks" jsonschema:"Webhooks configured on the repository. Empty rather than absent when there are none."`
}

// Change is what `bb webhook create` and `update` report.
type Change struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Webhook    Webhook           `json:"webhook"`
}

// Deletion is what `bb webhook delete` reports.
type Deletion struct {
	result.Status
	Repository result.Repository `json:"repository"`
	WebhookID  string            `json:"webhookId" jsonschema:"Identifier of the webhook that was deleted."`
}

func init() {
	result.Declare("webhook get", result.For[SingleWebhook](nil))
	result.Declare("webhook list", result.For[Webhooks](nil))
	result.Declare("webhook create", result.For[Change](nil))
	result.Declare("webhook update", result.For[Change](nil))
	result.Declare("webhook delete", result.For[Deletion](nil))

	// webhook test and webhook stats are deliberately not declared. The service
	// returns them as an untyped any -- whatever Bitbucket sent -- and the
	// command pretty-prints it without interpreting a single field, so there is
	// no shape here to publish. Declaring a bare object would claim a contract
	// that says nothing, which is worse than --describe answering honestly that
	// none is published. Typing them means typing the service response first.
}

// webhookFrom decodes one webhook out of the untyped payload the service
// returns.
//
// A round trip through JSON rather than a field-by-field copy, because the
// input is already a decoded any: there are no fields to copy from, only a map.
func webhookFrom(payload any) Webhook {
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

// webhooksFrom decodes a list, never returning nil.
//
// Bitbucket answers this endpoint two ways depending on version: a bare array,
// or a paginated object with the webhooks under values. Both are handled, which
// the human path already did -- and the JSON path did not, because it published
// whatever arrived without looking. A caller therefore got an array from one
// instance and a pagination envelope from another, for the same command.
func webhooksFrom(payload any) []Webhook {
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
