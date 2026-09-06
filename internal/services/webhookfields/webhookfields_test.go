package webhookfields

import (
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// The payload shapes here are the ones a live 10.4.2 instance actually sends;
// they were read off it rather than invented (see the webhook live tests).
// Nothing in this file stands up a server: these are the pure decisions about
// what to put in a request body and what to keep out of a published one, and
// each of them has three states that a live test can only reach one at a time.

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }

func TestNewCreateBodyRefusesWhatCannotWork(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		input CreateInput
	}{
		{name: "no name", input: CreateInput{URL: "https://example.com"}},
		{name: "blank name", input: CreateInput{Name: "   ", URL: "https://example.com"}},
		{name: "no url", input: CreateInput{Name: "hook"}},
		{name: "blank url", input: CreateInput{Name: "hook", URL: "  "}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewCreateBody(testCase.input); err == nil {
				t.Error("expected a refusal")
			}
		})
	}
}

func TestNewCreateBodyCarriesTheCredentialsItWasGiven(t *testing.T) {
	t.Parallel()

	body, err := NewCreateBody(CreateInput{
		Name:                    "  hook  ",
		URL:                     "  https://example.com  ",
		Events:                  []string{" repo:refs_changed ", "", "   "},
		Active:                  true,
		SSLVerificationRequired: boolPointer(false),
		Secret:                  stringPointer("s3cr3t"),
		CredentialsUsername:     " hookuser ",
		CredentialsPassword:     stringPointer("hookpass"),
	})
	if err != nil {
		t.Fatalf("NewCreateBody: %v", err)
	}

	if *body.Name != "hook" || *body.Url != "https://example.com" {
		t.Errorf("name and url were not trimmed: %q %q", *body.Name, *body.Url)
	}
	if len(*body.Events) != 1 || (*body.Events)[0] != "repo:refs_changed" {
		t.Errorf("events = %#v, want the one non-blank entry", *body.Events)
	}
	if *body.SslVerificationRequired {
		t.Error("sslVerificationRequired was not carried through")
	}
	if secret, _ := (*body.Configuration)["secret"].(string); secret != "s3cr3t" {
		t.Errorf("configuration.secret = %q", secret)
	}
	if *body.Credentials.Username != "hookuser" || *body.Credentials.Password != "hookpass" {
		t.Errorf("credentials = %#v", body.Credentials)
	}
}

func TestNewCreateBodyLeavesUnmentionedFieldsToTheServer(t *testing.T) {
	t.Parallel()

	// The reason SSLVerificationRequired is a pointer: bb does not know
	// Bitbucket's default and must not invent one by sending false.
	body, err := NewCreateBody(CreateInput{Name: "hook", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("NewCreateBody: %v", err)
	}

	if body.SslVerificationRequired != nil {
		t.Error("a create that said nothing about TLS sent a value anyway")
	}
	if body.Configuration != nil {
		t.Error("a create that said nothing about a secret sent a configuration object")
	}
	if body.Credentials != nil {
		t.Error("a create that said nothing about credentials sent a credentials object")
	}
	if len(*body.Events) != 1 || (*body.Events)[0] != DefaultEvent {
		t.Errorf("events = %#v, want the default", *body.Events)
	}
}

// currentWebhook is what a read returns, which is what an update starts from.
func currentWebhook() openapigenerated.RestWebhook {
	return openapigenerated.RestWebhook{
		Name:                    stringPointer("hook"),
		Url:                     stringPointer("https://example.com"),
		Events:                  &[]string{"repo:refs_changed"},
		Active:                  boolPointer(true),
		SslVerificationRequired: boolPointer(true),
		Configuration:           &map[string]any{"secret": "existing"},
		Credentials:             &openapigenerated.RestWebhookCredentials{Username: stringPointer("hookuser")},
	}
}

func TestApplyUpdateLeavesUnmentionedFieldsAlone(t *testing.T) {
	t.Parallel()

	// The endpoint replaces the webhook rather than patching it, so "leave it
	// alone" means sending back what was read. An update naming only the name
	// used to clear the rest, which is the defect class this whole release is
	// full of.
	body := currentWebhook()
	ApplyUpdate(&body, UpdateInput{Name: "renamed"})

	if *body.Name != "renamed" {
		t.Errorf("name = %q", *body.Name)
	}
	if *body.Url != "https://example.com" {
		t.Errorf("url was changed by an update that did not mention it: %q", *body.Url)
	}
	if secret, _ := (*body.Configuration)["secret"].(string); secret != "existing" {
		t.Errorf("the shared secret was dropped: %q", secret)
	}
	if body.Credentials == nil || *body.Credentials.Username != "hookuser" {
		t.Errorf("the endpoint credentials were dropped: %#v", body.Credentials)
	}
	if !*body.SslVerificationRequired {
		t.Error("sslVerificationRequired was changed by an update that did not mention it")
	}
}

func TestApplyUpdateDistinguishesClearingFromLeavingAlone(t *testing.T) {
	t.Parallel()

	// Bitbucket clears the secret when an update arrives without a
	// configuration object, so nil-and-nil would make "remove it" and "leave
	// it" the same request.
	body := currentWebhook()
	ApplyUpdate(&body, UpdateInput{ClearSecret: true, ClearCredentials: true})

	if body.Configuration != nil {
		t.Errorf("--no-secret sent a configuration object anyway: %#v", *body.Configuration)
	}
	if body.Credentials != nil {
		t.Errorf("--no-credentials sent a credentials object anyway: %#v", body.Credentials)
	}
}

func TestApplyUpdateReplacesTheSecretWhenGivenOne(t *testing.T) {
	t.Parallel()

	body := currentWebhook()
	ApplyUpdate(&body, UpdateInput{
		URL:                     " https://elsewhere.example.com ",
		Events:                  []string{" pr:opened ", ""},
		Active:                  boolPointer(false),
		SSLVerificationRequired: boolPointer(false),
		Secret:                  stringPointer("replacement"),
	})

	if *body.Url != "https://elsewhere.example.com" {
		t.Errorf("url = %q", *body.Url)
	}
	if len(*body.Events) != 1 || (*body.Events)[0] != "pr:opened" {
		t.Errorf("events = %#v", *body.Events)
	}
	if *body.Active || *body.SslVerificationRequired {
		t.Error("active and sslVerificationRequired were not replaced")
	}
	if secret, _ := (*body.Configuration)["secret"].(string); secret != "replacement" {
		t.Errorf("configuration.secret = %q", secret)
	}
}

func TestApplyUpdateKeepsTheUsernameWhenOnlyAPasswordIsGiven(t *testing.T) {
	t.Parallel()

	// Bitbucket never returns the password, so bb cannot carry it forward the
	// way it carries the secret; what it can do is not lose the username that
	// tells the server which credentials these are.
	body := currentWebhook()
	ApplyUpdate(&body, UpdateInput{CredentialsPassword: stringPointer("newpass")})

	if body.Credentials == nil {
		t.Fatal("the credentials object was dropped")
	}
	if *body.Credentials.Username != "hookuser" {
		t.Errorf("username = %q, want the one that was already there", *body.Credentials.Username)
	}
	if *body.Credentials.Password != "newpass" {
		t.Errorf("password = %q", *body.Credentials.Password)
	}
}

func TestApplyUpdateChangesTheUsernameOnItsOwn(t *testing.T) {
	t.Parallel()

	body := currentWebhook()
	ApplyUpdate(&body, UpdateInput{CredentialsUsername: " otheruser "})

	if *body.Credentials.Username != "otheruser" {
		t.Errorf("username = %q", *body.Credentials.Username)
	}
	if body.Credentials.Password != nil {
		t.Error("a password bb was never given was sent anyway")
	}
}

func TestApplyUpdateOnNothingDoesNothing(t *testing.T) {
	t.Parallel()

	// Called with a nil body when a read failed and the error was handled
	// elsewhere; panicking here would turn a reported failure into a crash.
	ApplyUpdate(nil, UpdateInput{Name: "renamed"})
}

func TestWithoutCredentialsKeepsWhetherAndDropsWhat(t *testing.T) {
	t.Parallel()

	published, ok := WithoutCredentials(map[string]any{
		"id":            float64(7),
		"name":          "hook",
		"configuration": map[string]any{"secret": "s3cr3t"},
		"credentials":   map[string]any{"username": "hookuser"},
	}).(map[string]any)
	if !ok {
		t.Fatal("expected an object back")
	}

	if _, present := published["configuration"]; present {
		t.Error("the configuration object was published")
	}
	if _, present := published["credentials"]; present {
		t.Error("the credentials object was published")
	}
	if configured, _ := published["secretConfigured"].(bool); !configured {
		t.Error("secretConfigured was not reported for a payload carrying a secret")
	}
	if username, _ := published["credentialsUsername"].(string); username != "hookuser" {
		t.Errorf("credentialsUsername = %q", username)
	}
	if published["name"] != "hook" || published["id"] != float64(7) {
		t.Errorf("the fields that are not credentials were not carried through: %#v", published)
	}
}

func TestWithoutCredentialsSaysNothingWhenThePayloadDidNot(t *testing.T) {
	t.Parallel()

	// A create response carries an empty configuration object about half the
	// time, for identical requests. There it means "the server did not say",
	// not "no secret" -- and reporting false would state a fact this payload
	// cannot know.
	published, _ := WithoutCredentials(map[string]any{
		"id":            float64(7),
		"configuration": map[string]any{},
	}).(map[string]any)

	if _, present := published["secretConfigured"]; present {
		t.Errorf("secretConfigured was reported from an empty configuration: %#v", published)
	}
}

func TestWithoutCredentialsPassesThroughWhatIsNotAWebhook(t *testing.T) {
	t.Parallel()

	// The create response is decoded as any, and an instance that answered
	// with something else should not be turned into a nil.
	if got := WithoutCredentials("not an object"); got != "not an object" {
		t.Errorf("WithoutCredentials(%q) = %#v", "not an object", got)
	}
	if got := WithoutCredentials(nil); got != nil {
		t.Errorf("WithoutCredentials(nil) = %#v", got)
	}
}

func TestCleanEventsDropsTheBlanksAFlagCollects(t *testing.T) {
	t.Parallel()

	got := CleanEvents([]string{" a ", "", "   ", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("CleanEvents = %#v", got)
	}
	if got := CleanEvents(nil); len(got) != 0 {
		t.Errorf("CleanEvents(nil) = %#v, want empty and not nil", got)
	}
}
