// Package webhookfields shapes the request body Bitbucket's webhook endpoints
// take.
//
// Repository webhooks and project webhooks are the same object behind two
// routes, and their two services had each written the body out by hand. That
// was survivable while the body was four plain fields. It stopped being
// survivable when three of the fields grew a third state -- set it, clear it,
// leave it alone -- because the endpoint replaces the webhook rather than
// patching it, so "leave it alone" means sending back what was read and
// "clear it" means deliberately not sending it. Getting that wrong in one of
// the two places, and only one, is the shape of every defect #522 collected.
package webhookfields

import (
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// DefaultEvent is what a webhook subscribes to when the caller names nothing.
const DefaultEvent = "repo:refs_changed"

// CreateInput is a new webhook.
type CreateInput struct {
	Name   string
	URL    string
	Events []string
	Active bool
	// Nil means "let Bitbucket decide", which is how a create that says
	// nothing about TLS keeps the server's default rather than picking one.
	SSLVerificationRequired *bool
	// Secret is the shared secret Bitbucket signs deliveries with, as
	// X-Hub-Signature. A pointer for the same reason: unset is not empty.
	Secret              *string
	CredentialsUsername string
	CredentialsPassword *string
}

// UpdateInput is what an update changes, field by field.
type UpdateInput struct {
	Name                    string
	URL                     string
	Events                  []string
	Active                  *bool
	SSLVerificationRequired *bool
	Secret                  *string
	// ClearSecret removes the shared secret. Distinct from a nil Secret,
	// which means "leave whatever is there": Bitbucket clears the secret when
	// an update arrives without a configuration object, so bb has to know
	// which of the two the caller meant before it decides what to send.
	ClearSecret         bool
	CredentialsUsername string
	CredentialsPassword *string
	ClearCredentials    bool
}

// NewCreateBody builds the body for a create, refusing one that cannot work.
func NewCreateBody(input CreateInput) (openapigenerated.RestWebhook, error) {
	name := strings.TrimSpace(input.Name)
	url := strings.TrimSpace(input.URL)
	if name == "" {
		return openapigenerated.RestWebhook{}, apperrors.New(apperrors.KindValidation, "webhook name is required", nil)
	}
	if url == "" {
		return openapigenerated.RestWebhook{}, apperrors.New(apperrors.KindValidation, "webhook url is required", nil)
	}

	events := CleanEvents(input.Events)
	if len(events) == 0 {
		events = []string{DefaultEvent}
	}

	active := input.Active
	body := openapigenerated.RestWebhook{
		Name:                    &name,
		Url:                     &url,
		Events:                  &events,
		Active:                  &active,
		SslVerificationRequired: input.SSLVerificationRequired,
	}

	if input.Secret != nil {
		body.Configuration = &map[string]any{"secret": *input.Secret}
	}
	if credentials := newCredentials(input.CredentialsUsername, input.CredentialsPassword); credentials != nil {
		body.Credentials = credentials
	}

	return body, nil
}

// ApplyUpdate folds the requested changes into the webhook as it stands.
//
// current is what the server returned, which is what makes the three states
// work: an unmentioned field is already correct because it is still there.
func ApplyUpdate(current *openapigenerated.RestWebhook, input UpdateInput) {
	if current == nil {
		return
	}

	if name := strings.TrimSpace(input.Name); name != "" {
		current.Name = &name
	}
	if url := strings.TrimSpace(input.URL); url != "" {
		current.Url = &url
	}
	if events := CleanEvents(input.Events); len(events) > 0 {
		current.Events = &events
	}
	if input.Active != nil {
		current.Active = input.Active
	}
	if input.SSLVerificationRequired != nil {
		current.SslVerificationRequired = input.SSLVerificationRequired
	}

	switch {
	case input.ClearSecret:
		// Not an empty configuration object: Bitbucket keeps a configuration
		// it is sent and only forgets the secret when none arrives.
		current.Configuration = nil
	case input.Secret != nil:
		current.Configuration = &map[string]any{"secret": *input.Secret}
	}

	switch {
	case input.ClearCredentials:
		current.Credentials = nil
	case strings.TrimSpace(input.CredentialsUsername) != "" || input.CredentialsPassword != nil:
		current.Credentials = updatedCredentials(current.Credentials, input.CredentialsUsername, input.CredentialsPassword)
	}
}

// WithoutCredentials copies a webhook payload with its credentials removed.
//
// For the callers that publish what the server sent rather than a model of it.
// A bulk apply is the one that matters: its status document is written to disk
// and printed under --json, and it carried the webhook Bitbucket answered with,
// field for field.
//
// The create response is why this cannot be skipped. Bitbucket answers an
// identical create with `"configuration": {"secret": "..."}` sometimes and
// `"configuration": {}` other times -- five identical requests against one
// repository produced two of the first and three of the second. So a create
// response may carry the secret, and the apply status must not.
//
// The same inconsistency is why an empty configuration object leaves
// secretConfigured out rather than setting it false. On a read that shape means
// "no secret"; on a create it means "the server did not say", and the two are
// indistinguishable here. Saying false would state as fact something this
// payload cannot know, which is the mistake the sslVerificationRequired pointer
// exists to avoid.
func WithoutCredentials(payload any) any {
	object, ok := payload.(map[string]any)
	if !ok {
		return payload
	}

	copied := make(map[string]any, len(object))
	for key, value := range object {
		switch key {
		case "configuration":
			// Whether, not what -- and only when the payload said.
			configuration, _ := value.(map[string]any)
			if secret, _ := configuration["secret"].(string); strings.TrimSpace(secret) != "" {
				copied["secretConfigured"] = true
			}
		case "credentials":
			credentials, _ := value.(map[string]any)
			if username, _ := credentials["username"].(string); username != "" {
				copied["credentialsUsername"] = username
			}
		default:
			copied[key] = value
		}
	}

	return copied
}

// CleanEvents drops the blanks a --event flag collects.
func CleanEvents(events []string) []string {
	cleaned := make([]string, 0, len(events))
	for _, event := range events {
		if trimmed := strings.TrimSpace(event); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	return cleaned
}

func newCredentials(username string, password *string) *openapigenerated.RestWebhookCredentials {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" && password == nil {
		return nil
	}

	credentials := &openapigenerated.RestWebhookCredentials{}
	if trimmed != "" {
		credentials.Username = &trimmed
	}
	credentials.Password = password

	return credentials
}

// updatedCredentials changes one half of the endpoint's basic auth without
// losing the other.
//
// Bitbucket never returns the password, so a read-modify-write cannot carry it
// forward the way it carries the shared secret. It does not have to: sending
// the credentials object back with the username alone keeps the stored
// password, which a real delivery confirms -- the Authorization header still
// arrives complete after such an update.
func updatedCredentials(current *openapigenerated.RestWebhookCredentials, username string, password *string) *openapigenerated.RestWebhookCredentials {
	updated := &openapigenerated.RestWebhookCredentials{}
	if current != nil {
		updated.Username = current.Username
	}
	if trimmed := strings.TrimSpace(username); trimmed != "" {
		updated.Username = &trimmed
	}
	updated.Password = password

	return updated
}
