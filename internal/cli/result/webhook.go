package result

import "strings"

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
	// A pointer because absent and false are different answers, and this is the
	// field an operator audits. A webhook whose server did not report it would
	// otherwise read as one that does not verify TLS.
	SSLVerificationRequired *bool  `json:"sslVerificationRequired,omitempty" jsonschema:"Whether Bitbucket verifies the endpoint's TLS certificate. Absent when the server did not report it."`
	ScopeType               string `json:"scopeType,omitempty" jsonschema:"Where the webhook is configured: repository or project."`
	// Whether, never what. Bitbucket returns the shared secret in plaintext on
	// every read, and publishing it would write a credential to stdout, where it
	// lands in CI logs and shell history.
	SecretConfigured    bool   `json:"secretConfigured" jsonschema:"Whether a shared secret is configured. The secret itself is only published when --reveal-secret is given."`
	CredentialsUsername string `json:"credentialsUsername,omitempty" jsonschema:"Username Bitbucket authenticates to the endpoint with. Bitbucket never returns the password, so bb cannot publish it."`
	// Never filled by WebhookFrom. A command sets it only when the caller
	// asked for it out loud, which is what keeps a secret out of a log that
	// nobody meant to put one in.
	Secret string `json:"secret,omitempty" jsonschema:"The shared secret in plaintext. Present only when --reveal-secret was given."`
}

// WebhookFrom reads one webhook out of the untyped payload a service returns.
//
// Field by field off the decoded value rather than a round trip through a typed
// struct. The round trip made one unexpected field type fatal for the whole
// object -- an id Bitbucket sent as a string, say -- and the failure was
// swallowed, so the webhook came back empty rather than partly read. Reading
// each field on its own terms loses only the field that surprised us.
func WebhookFrom(payload any) Webhook {
	object, ok := payload.(map[string]any)
	if !ok {
		return Webhook{}
	}

	converted := Webhook{
		Name: stringOf(object["name"]),
		URL:  stringOf(object["url"]),
	}
	converted.ID = intOf(object["id"])
	if active, ok := object["active"].(bool); ok {
		converted.Active = active
	}
	if verificationRequired, ok := object["sslVerificationRequired"].(bool); ok {
		converted.SSLVerificationRequired = &verificationRequired
	}
	converted.ScopeType = stringOf(object["scopeType"])
	// configuration and credentials are read for one bit each and then
	// dropped. Bitbucket sends the shared secret back in full on every read,
	// and the object this builds is what every --json path publishes.
	if configuration, ok := object["configuration"].(map[string]any); ok {
		converted.SecretConfigured = strings.TrimSpace(stringOf(configuration["secret"])) != ""
	}
	if credentials, ok := object["credentials"].(map[string]any); ok {
		converted.CredentialsUsername = stringOf(credentials["username"])
	}
	if events, ok := object["events"].([]any); ok {
		converted.Events = make([]string, 0, len(events))
		for _, event := range events {
			if name := stringOf(event); name != "" {
				converted.Events = append(converted.Events, name)
			}
		}
	}

	return converted
}

// WebhookSecretFrom reads the shared secret out of the payload.
//
// Separate from WebhookFrom, and returning the value rather than filling the
// model, so that reading a webhook cannot publish its secret by default. A
// command has to reach for this deliberately, which is the whole guarantee.
func WebhookSecretFrom(payload any) string {
	object, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	configuration, ok := object["configuration"].(map[string]any)
	if !ok {
		return ""
	}

	return stringOf(configuration["secret"])
}

// WebhooksFrom reads a list, never returning nil.
//
// Bitbucket answers the listing endpoints two ways depending on version: a bare
// array, or a paginated object with the webhooks under values. Both are handled
// here, which the human renderers already did -- and the JSON paths did not,
// because they published whatever arrived without looking.
func WebhooksFrom(payload any) []Webhook {
	switch typed := payload.(type) {
	case []any:
		return webhooksFromValues(typed)
	case map[string]any:
		if values, ok := typed["values"].([]any); ok {
			return webhooksFromValues(values)
		}
		// A single webhook where a list was expected is still one webhook, and
		// an id or a url is what says this object is one rather than some other
		// envelope that happens to have no values key.
		if typed["id"] != nil || typed["url"] != nil {
			return []Webhook{WebhookFrom(typed)}
		}
	}

	return []Webhook{}
}

func webhooksFromValues(values []any) []Webhook {
	converted := make([]Webhook, 0, len(values))
	for _, value := range values {
		converted = append(converted, WebhookFrom(value))
	}

	return converted
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

// redactedValue is what stands in for a credential bb refuses to publish.
//
// A marker rather than an omission: a caller reading the delivery record can
// see that the request carried authentication, which is the operationally
// interesting fact, without the value that makes it a credential.
const redactedValue = "<redacted>"

// RedactedDelivery copies a webhook delivery record with its credentials taken
// out.
//
// `bb webhook test` publishes what Bitbucket sent to the endpoint, headers
// included, and Bitbucket puts the endpoint's basic-auth credentials in the
// Authorization header. Base64 is not encryption: the record published the
// endpoint password to stdout, which under --json is the machine contract and
// in CI is a log line.
//
// The payload has no shape in the specification -- RestWebhookRequestResponse
// is a bare interface -- so this walks it rather than modelling it. Every map
// key that names an authorization header is replaced wherever it appears, at
// any depth, because the record nests request and response under keys that are
// themselves not documented and have already changed shape once.
func RedactedDelivery(payload any) any {
	switch typed := payload.(type) {
	case map[string]any:
		copied := make(map[string]any, len(typed))
		for key, value := range typed {
			if isCredentialHeader(key) {
				copied[key] = redactedValue
				continue
			}
			copied[key] = RedactedDelivery(value)
		}

		return copied
	case []any:
		copied := make([]any, len(typed))
		for index, value := range typed {
			copied[index] = RedactedDelivery(value)
		}

		return copied
	default:
		return payload
	}
}

// isCredentialHeader names the headers that carry a credential by definition.
//
// Deliberately not a search for "secret" or "token" in the key: those match
// field names that hold nothing sensitive and miss the one that does. These
// two are the headers HTTP defines as carrying credentials. The signature
// header is left alone -- it is derived from the shared secret, not the secret,
// and the receiver needs it to verify the delivery.
func isCredentialHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization":
		return true
	default:
		return false
	}
}

// stringOf reads a value Bitbucket may send as a string or as a number.
func stringOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

// intOf reads an identifier however it arrived.
//
// A decode gives float64; an instance that quotes its ids gives a string. The
// typed round trip this replaced rejected the second outright and lost the
// whole object with it.
func intOf(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case string:
		parsed := 0
		for _, digit := range typed {
			if digit < '0' || digit > '9' {
				return 0
			}
			parsed = parsed*10 + int(digit-'0')
		}

		return parsed
	default:
		return 0
	}
}
