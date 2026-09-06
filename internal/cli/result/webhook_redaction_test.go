package result

import (
	"encoding/json"
	"strings"
	"testing"
)

// The payloads here are the shapes a live 10.4.2 instance sends. The live suite
// proves bb does not publish a credential from a real webhook; these cover the
// branches one live payload cannot reach at once -- a delivery record nested in
// a list, an instance that answered with something other than an object, a
// configuration object that carries no secret.

func TestWebhookFromReadsWhetherASecretIsConfigured(t *testing.T) {
	t.Parallel()

	hook := WebhookFrom(map[string]any{
		"id":                      float64(7),
		"name":                    "hook",
		"url":                     "https://example.com",
		"active":                  true,
		"events":                  []any{"repo:refs_changed", ""},
		"scopeType":               "repository",
		"sslVerificationRequired": false,
		"configuration":           map[string]any{"secret": "s3cr3t"},
		"credentials":             map[string]any{"username": "hookuser"},
	})

	if !hook.SecretConfigured {
		t.Error("secretConfigured is false for a webhook that has one")
	}
	if hook.Secret != "" {
		t.Errorf("WebhookFrom filled the secret field, which only --reveal-secret may do: %q", hook.Secret)
	}
	if hook.CredentialsUsername != "hookuser" {
		t.Errorf("credentialsUsername = %q", hook.CredentialsUsername)
	}
	if hook.ScopeType != "repository" {
		t.Errorf("scopeType = %q", hook.ScopeType)
	}
	if hook.SSLVerificationRequired == nil || *hook.SSLVerificationRequired {
		t.Errorf("sslVerificationRequired = %v, want a pointer to false", hook.SSLVerificationRequired)
	}
	if len(hook.Events) != 1 {
		t.Errorf("events = %#v, want the blank dropped", hook.Events)
	}
}

func TestWebhookFromLeavesUnreportedFieldsUnreported(t *testing.T) {
	t.Parallel()

	// Absent is not false. An audit asking whether a webhook verifies TLS must
	// not read "the server did not say" as "no", which is why the field is a
	// pointer and why omitempty keeps it out of the published object.
	hook := WebhookFrom(map[string]any{"id": float64(7), "configuration": map[string]any{}})

	if hook.SSLVerificationRequired != nil {
		t.Errorf("sslVerificationRequired = %v for a payload that did not report it", *hook.SSLVerificationRequired)
	}
	if hook.SecretConfigured {
		t.Error("secretConfigured is true for an empty configuration object")
	}

	encoded, err := json.Marshal(hook)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "sslVerificationRequired") {
		t.Errorf("an unreported field was published anyway: %s", encoded)
	}
	// secretConfigured has no omitempty: an auditor needs a definite answer
	// from a read, and a read that returns no secret means there is none.
	if !strings.Contains(string(encoded), `"secretConfigured":false`) {
		t.Errorf("secretConfigured was omitted, so a read gives no answer at all: %s", encoded)
	}
}

func TestWebhookSecretFromRefusesEverythingButASecret(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		payload any
	}{
		{name: "not an object", payload: "webhook"},
		{name: "nil", payload: nil},
		{name: "no configuration", payload: map[string]any{"id": float64(7)}},
		{name: "configuration is not an object", payload: map[string]any{"configuration": "secret"}},
		{name: "no secret in it", payload: map[string]any{"configuration": map[string]any{}}},
		{name: "secret is not a string", payload: map[string]any{"configuration": map[string]any{"secret": 7}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := WebhookSecretFrom(testCase.payload); got != "" {
				t.Errorf("WebhookSecretFrom = %q, want empty", got)
			}
		})
	}

	if got := WebhookSecretFrom(map[string]any{"configuration": map[string]any{"secret": "s3cr3t"}}); got != "s3cr3t" {
		t.Errorf("WebhookSecretFrom = %q", got)
	}
}

func TestRedactedDeliveryTakesOutAuthorizationAtAnyDepth(t *testing.T) {
	t.Parallel()

	// The record has no shape in the specification -- RestWebhookRequestResponse
	// is a bare interface -- and it has already changed shape once, so the walk
	// has to find the header wherever it is rather than at a known path.
	record := map[string]any{
		"request": map[string]any{
			"headers": map[string]any{
				"Authorization":   "Basic aG9va3VzZXI6aG9va3Bhc3M=",
				"X-Hub-Signature": "sha256=abc",
				"Content-Type":    "application/json",
			},
		},
		"attempts": []any{
			map[string]any{"headers": map[string]any{"proxy-authorization": "Basic other"}},
			"a string in a list",
		},
	}

	redacted, ok := RedactedDelivery(record).(map[string]any)
	if !ok {
		t.Fatal("expected an object back")
	}

	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "aG9va3VzZXI6aG9va3Bhc3M=") ||
		strings.Contains(string(encoded), "Basic other") {
		t.Errorf("a credential survived the redaction: %s", encoded)
	}
	// On the structure rather than the encoded string: json.Marshal escapes the
	// angle brackets, so counting the marker in the encoding measures the
	// encoder rather than the redaction.
	requestHeaders := redacted["request"].(map[string]any)["headers"].(map[string]any)
	if requestHeaders["Authorization"] != redactedValue {
		t.Errorf("Authorization = %#v", requestHeaders["Authorization"])
	}
	nestedHeaders := redacted["attempts"].([]any)[0].(map[string]any)["headers"].(map[string]any)
	if nestedHeaders["proxy-authorization"] != redactedValue {
		t.Errorf("a header nested inside a list was not redacted: %#v", nestedHeaders)
	}

	// The signature is derived from the shared secret, not the secret, and the
	// receiver needs it to verify the delivery. Dropping it would make the
	// record useless without making it safer.
	if !strings.Contains(string(encoded), "sha256=abc") {
		t.Errorf("the signature header was redacted too: %s", encoded)
	}
	if !strings.Contains(string(encoded), "application/json") {
		t.Errorf("an ordinary header was redacted: %s", encoded)
	}

	// The copy is a copy: the caller's payload is not quietly rewritten.
	original := record["request"].(map[string]any)["headers"].(map[string]any)
	if original["Authorization"] == redactedValue {
		t.Error("RedactedDelivery mutated the payload it was given")
	}
}

func TestRedactedDeliveryPassesThroughWhatItCannotWalk(t *testing.T) {
	t.Parallel()

	if got := RedactedDelivery("a string"); got != "a string" {
		t.Errorf("RedactedDelivery = %#v", got)
	}
	if got := RedactedDelivery(nil); got != nil {
		t.Errorf("RedactedDelivery(nil) = %#v", got)
	}

	list, ok := RedactedDelivery([]any{float64(1), "two"}).([]any)
	if !ok || len(list) != 2 || list[0] != float64(1) || list[1] != "two" {
		t.Errorf("RedactedDelivery on a bare list = %#v", list)
	}
}

func TestCredentialHeadersAreTheOnesHTTPDefinesAsCredentials(t *testing.T) {
	t.Parallel()

	// Deliberately not a search for "secret" or "token" in the key: that
	// matches field names holding nothing sensitive and misses the one that
	// does.
	for _, name := range []string{"Authorization", "authorization", " PROXY-AUTHORIZATION "} {
		if !isCredentialHeader(name) {
			t.Errorf("isCredentialHeader(%q) = false", name)
		}
	}
	for _, name := range []string{"X-Hub-Signature", "Content-Type", "secret", "token", ""} {
		if isCredentialHeader(name) {
			t.Errorf("isCredentialHeader(%q) = true", name)
		}
	}
}
