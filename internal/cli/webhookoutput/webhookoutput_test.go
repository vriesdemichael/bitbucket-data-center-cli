package webhookoutput

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/webhookfields"
)

// ansiEscape is what a colour-capable terminal adds around a label.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(buffer *bytes.Buffer) string {
	return ansiEscape.ReplaceAllString(buffer.String(), "")
}

func TestPublishedSaysNothingForAWebhookThatWasReadBack(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	hook := Published(stderr, webhookfields.Written{
		Webhook: map[string]any{"id": float64(42), "configuration": map[string]any{"secret": "s3cret"}},
	}, "create")

	if hook.ID != 42 || !hook.SecretConfigured {
		t.Fatalf("published %+v, want webhook 42 with its secret configured", hook)
	}
	if stderr.Len() != 0 {
		t.Fatalf("a webhook that was read back warned: %q", stderr.String())
	}
}

// A webhook that was not read back is still the answer, and stderr says what it
// is instead: which webhook, after which write, why, and which field the write
// supplied.
func TestPublishedWarnsWhenTheWebhookWasNotReadBack(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	hook := Published(stderr, webhookfields.Written{
		Webhook: map[string]any{"id": float64(42), "configuration": map[string]any{"secret": "s3cret"}},
		Unread:  errors.New("transient: failed to get webhook"),
	}, "update")

	if hook.ID != 42 || !hook.SecretConfigured {
		t.Fatalf("published %+v, want webhook 42 as the answer described it", hook)
	}
	warning := plain(stderr)
	for _, want := range []string{"webhook 42", "after the update", "transient: failed to get webhook", "shared secret"} {
		if !strings.Contains(warning, want) {
			t.Errorf("the warning does not say %q: %q", want, warning)
		}
	}
	if strings.Contains(warning, "s3cret") {
		t.Errorf("the warning repeated the secret: %q", warning)
	}

	// Without an id there is no webhook number to name.
	stderr.Reset()
	Published(stderr, webhookfields.Written{Webhook: map[string]any{}, Unread: errors.New("no id")}, "create")
	if warning := plain(stderr); !strings.Contains(warning, "the webhook was not read back after the create") {
		t.Errorf("the warning for a webhook without an id = %q", warning)
	}
}

func TestDetailSaysWhetherASecretIsConfiguredAndNotWhatItIs(t *testing.T) {
	t.Parallel()

	verified := true
	output := &bytes.Buffer{}
	Detail(output, result.Webhook{
		ID:                      42,
		Name:                    "ci",
		URL:                     "https://ci.example/hook",
		Active:                  true,
		Events:                  []string{"repo:refs_changed", "pr:merged"},
		ScopeType:               "repository",
		SSLVerificationRequired: &verified,
		SecretConfigured:        true,
		CredentialsUsername:     "hookuser",
	})

	rendered := plain(output)
	for _, want := range []string{
		"ID: 42\n",
		"Name: ci\n",
		"URL: https://ci.example/hook\n",
		"Active: true\n",
		"Events: repo:refs_changed, pr:merged\n",
		"Scope: repository\n",
		"SSL verification required: true\n",
		"Shared secret configured: true\n",
		"Endpoint credentials username: hookuser\n",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the detail does not carry %q:\n%s", want, rendered)
		}
	}
}

func TestDetailLeavesOutWhatTheServerDidNotSay(t *testing.T) {
	t.Parallel()

	output := &bytes.Buffer{}
	Detail(output, result.Webhook{Name: "ci"})

	rendered := plain(output)
	// An id of 0 reads as an id to the commands that take one.
	if strings.Contains(rendered, "ID:") {
		t.Errorf("a webhook without an id was shown with one:\n%s", rendered)
	}
	if !strings.Contains(rendered, "SSL verification required: not reported\n") {
		t.Errorf("an unreported TLS setting was not shown as unreported:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Shared secret configured: false\n") || strings.Contains(rendered, "Endpoint credentials username:") {
		t.Errorf("a webhook without credentials was shown with some:\n%s", rendered)
	}
}
