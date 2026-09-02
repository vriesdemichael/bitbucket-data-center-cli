package result

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
