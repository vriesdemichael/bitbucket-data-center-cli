package webhookfields

import (
	"encoding/json"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// listedHook is a webhook as a scope listing carries it, with the fields a
// test changes.
func listedHook(t *testing.T, id int, change func(map[string]any)) json.RawMessage {
	t.Helper()

	hook := map[string]any{
		"id":                      id,
		"name":                    "ci",
		"url":                     "https://ci.example.com/hook",
		"events":                  []string{"repo:refs_changed", "pr:opened"},
		"active":                  true,
		"sslVerificationRequired": true,
		"configuration":           map[string]any{"secret": "s3cret"},
		"credentials":             map[string]any{"username": "hookuser"},
	}
	if change != nil {
		change(hook)
	}
	encoded, err := json.Marshal(hook)
	if err != nil {
		t.Fatalf("encode the listed webhook: %v", err)
	}

	return encoded
}

// createOf is a create that asks for listedHook's webhook as it stands.
func createOf(change func(*CreateInput)) CreateInput {
	secret := "s3cret"
	input := CreateInput{
		Name:                "ci",
		URL:                 "https://ci.example.com/hook",
		Events:              []string{"pr:opened", "repo:refs_changed"},
		Active:              true,
		Secret:              &secret,
		CredentialsUsername: "hookuser",
	}
	if change != nil {
		change(&input)
	}

	return input
}

func TestFindExistingFindsTheWebhookACreateWouldMake(t *testing.T) {
	t.Parallel()

	existing, found, err := FindExisting([]json.RawMessage{
		json.RawMessage(`"not a webhook"`),
		listedHook(t, 3, func(hook map[string]any) { hook["name"] = "other" }),
		listedHook(t, 7, nil),
	}, createOf(nil))
	if err != nil || !found {
		t.Fatalf("want webhook 7 found, got found %v err %v", found, err)
	}
	if existing.ID != "7" || existing.PasswordUncompared {
		t.Errorf("got id %q and password uncompared %v, want 7 and false", existing.ID, existing.PasswordUncompared)
	}
	if hook, _ := existing.Webhook.(map[string]any); hook["name"] != "ci" {
		t.Errorf("the webhook reported is not the listed one: %v", existing.Webhook)
	}
}

// A create that says nothing about TLS takes Bitbucket's default, and the
// webhook there is as good a result of that as a new one. A password cannot be
// read back to compare, and the result says so.
func TestFindExistingComparesOnlyWhatACreateDecides(t *testing.T) {
	t.Parallel()

	listed := []json.RawMessage{listedHook(t, 7, func(hook map[string]any) { hook["sslVerificationRequired"] = false })}
	password := "pw"
	existing, found, err := FindExisting(listed, createOf(func(input *CreateInput) { input.CredentialsPassword = &password }))
	if err != nil || !found {
		t.Fatalf("want the webhook found, got found %v err %v", found, err)
	}
	if !existing.PasswordUncompared {
		t.Error("a create with a password does not say the password went uncompared")
	}
}

func TestFindExistingRefusesTheSameNameAndURLWithOtherSettings(t *testing.T) {
	t.Parallel()

	off, on := false, true
	for name, testCase := range map[string]struct {
		listed func(map[string]any)
		create func(*CreateInput)
		says   string
	}{
		"events":       {nil, func(input *CreateInput) { input.Events = []string{"pr:merged"} }, "events pr:opened repo:refs_changed rather than pr:merged"},
		"active":       {nil, func(input *CreateInput) { input.Active = false }, "active true rather than false"},
		"TLS":          {nil, func(input *CreateInput) { input.SSLVerificationRequired = &off }, "TLS verification true rather than false"},
		"TLS unknown":  {func(hook map[string]any) { delete(hook, "sslVerificationRequired") }, func(input *CreateInput) { input.SSLVerificationRequired = &on }, "TLS verification not reported rather than true"},
		"a secret":     {nil, func(input *CreateInput) { other := "0ther"; input.Secret = &other }, "another shared secret"},
		"no secret":    {nil, func(input *CreateInput) { input.Secret = nil }, "a shared secret where the create sets none"},
		"a new secret": {func(hook map[string]any) { delete(hook, "configuration") }, nil, "no shared secret where the create sets one"},
		"username":     {nil, func(input *CreateInput) { input.CredentialsUsername = "other" }, `endpoint username "hookuser" rather than "other"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, found, err := FindExisting([]json.RawMessage{listedHook(t, 7, testCase.listed)}, createOf(testCase.create))
			if found || !apperrors.IsKind(err, apperrors.KindConflict) {
				t.Fatalf("want a conflict, got found %v err %v", found, err)
			}
			message := err.Error()
			if !strings.Contains(message, "webhook 7 has "+testCase.says) {
				t.Errorf("the refusal does not say %q:\n%s", "webhook 7 has "+testCase.says, message)
			}
			if strings.Contains(message, "s3cret") || strings.Contains(message, "0ther") {
				t.Errorf("the refusal names a secret:\n%s", message)
			}
		})
	}
}

// Webhooks created twice before bb looked are both there: the one with the
// settings asked for is the answer, whatever else shares its name and URL.
func TestFindExistingPrefersTheIdenticalWebhook(t *testing.T) {
	t.Parallel()

	existing, found, err := FindExisting([]json.RawMessage{
		listedHook(t, 4, func(hook map[string]any) { hook["active"] = false }),
		listedHook(t, 9, nil),
	}, createOf(nil))
	if err != nil || !found || existing.ID != "9" {
		t.Fatalf("want webhook 9, got %q found %v err %v", existing.ID, found, err)
	}
}

func TestFindExistingLetsANewWebhookThrough(t *testing.T) {
	t.Parallel()

	_, found, err := FindExisting([]json.RawMessage{
		listedHook(t, 7, func(hook map[string]any) { hook["url"] = "https://ci.example.com/elsewhere" }),
	}, createOf(nil))
	if err != nil || found {
		t.Fatalf("want nothing found and no refusal, got found %v err %v", found, err)
	}

	if _, _, err := FindExisting(nil, createOf(func(input *CreateInput) { input.Name = " " })); !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Errorf("a create with no name was not refused as invalid: %v", err)
	}
}
