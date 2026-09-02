package webhookcmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// SingleWebhook is what `bb webhook get` returns.
type SingleWebhook struct {
	Webhook result.Webhook `json:"webhook"`
}

// Webhooks is what `bb webhook list` returns.
type Webhooks struct {
	Webhooks []result.Webhook `json:"webhooks" jsonschema:"Webhooks configured on the repository. Empty rather than absent when there are none."`
}

// Change is what `bb webhook create` and `update` report.
type Change struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Webhook    result.Webhook    `json:"webhook"`
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
