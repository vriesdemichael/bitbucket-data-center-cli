package webhookflags

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/webhookfields"
)

// Nothing here talks to Bitbucket. These are the decisions made before the
// request exists: which of two secrets a caller meant, whether a flag or a
// variable wins, and what a dry run is allowed to say about either.

// commandWith parses args against a command carrying the update flag set, which
// is the superset.
func commandWith(t *testing.T, stdin string, args ...string) (*cobra.Command, *Fields) {
	t.Helper()

	fields := &Fields{}
	command := &cobra.Command{Use: "update", RunE: func(*cobra.Command, []string) error { return nil }}
	fields.RegisterUpdate(command)
	command.SetIn(strings.NewReader(stdin))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(args)

	if err := command.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}

	return command, fields
}

func TestSSLVerificationIsThreeStated(t *testing.T) {
	t.Parallel()

	// bb does not know Bitbucket's default and must not invent one, so a
	// command that says nothing about TLS sends nothing.
	command, fields := commandWith(t, "")
	input, err := fields.UpdateInput(command)
	if err != nil {
		t.Fatalf("UpdateInput: %v", err)
	}
	if input.SSLVerificationRequired != nil {
		t.Errorf("an unmentioned --ssl-verification produced %v", *input.SSLVerificationRequired)
	}

	for _, testCase := range []struct {
		flag string
		want bool
	}{{flag: "--ssl-verification=true", want: true}, {flag: "--ssl-verification=false", want: false}} {
		command, fields := commandWith(t, "", testCase.flag)
		input, err := fields.UpdateInput(command)
		if err != nil {
			t.Fatalf("UpdateInput %s: %v", testCase.flag, err)
		}
		if input.SSLVerificationRequired == nil || *input.SSLVerificationRequired != testCase.want {
			t.Errorf("%s produced %v", testCase.flag, input.SSLVerificationRequired)
		}
	}
}

func TestTwoSecretsCannotShareOneStdin(t *testing.T) {
	t.Parallel()

	command, fields := commandWith(t, "s3cr3t", "--secret-stdin", "--credentials-password-stdin")

	_, err := fields.UpdateInput(command)
	if err == nil {
		t.Fatal("both stdin flags were accepted, and there is only one stdin")
	}
	// The message has to say what to do instead, because the caller has two
	// secrets and needs somewhere to put the second.
	if !strings.Contains(err.Error(), SecretEnv) || !strings.Contains(err.Error(), PasswordEnv) {
		t.Errorf("the refusal did not name the variables to use instead: %v", err)
	}
}

func TestAFlagOutranksTheEnvironment(t *testing.T) {
	// An automation host that exports the variable for every command must not
	// thereby make removing a credential impossible.
	t.Setenv(SecretEnv, "ambient")
	t.Setenv(PasswordEnv, "ambient")

	command, fields := commandWith(t, "", "--no-secret", "--no-credentials")
	input, err := fields.UpdateInput(command)
	if err != nil {
		t.Fatalf("UpdateInput: %v", err)
	}

	if !input.ClearSecret || input.Secret != nil {
		t.Errorf("--no-secret did not win over the variable: clear=%v secret=%v", input.ClearSecret, input.Secret)
	}
	if !input.ClearCredentials || input.CredentialsPassword != nil {
		t.Errorf("--no-credentials did not win over the variable")
	}
	// And nothing is left in the origins to be printed by a dry run.
	if origins := fields.Origins(); len(origins) != 0 {
		t.Errorf("origins = %#v, want empty", origins)
	}
}

func TestRemovingAndSettingTheSameThingIsRefused(t *testing.T) {
	t.Parallel()

	// Said out loud twice, in two directions: one of the two has to win and
	// neither should.
	command, fields := commandWith(t, "s3cr3t", "--no-secret", "--secret-stdin")
	if _, err := fields.UpdateInput(command); err == nil {
		t.Error("--no-secret with --secret-stdin was accepted")
	}

	command, fields = commandWith(t, "", "--no-credentials", "--credentials-username", "hookuser")
	if _, err := fields.UpdateInput(command); err == nil {
		t.Error("--no-credentials with --credentials-username was accepted")
	}
}

func TestCreateInputCarriesTheSecretAndItsOrigin(t *testing.T) {
	t.Setenv(SecretEnv, "from-the-environment")

	fields := &Fields{}
	command := &cobra.Command{Use: "create"}
	fields.RegisterCreate(command)
	command.SetIn(strings.NewReader(""))

	input, err := fields.CreateInput(command)
	if err != nil {
		t.Fatalf("CreateInput: %v", err)
	}
	if input.Secret == nil || *input.Secret != "from-the-environment" {
		t.Errorf("secret = %v", input.Secret)
	}
	if origin := fields.Origins()["secret"]; origin != "$"+SecretEnv {
		t.Errorf("origin = %q, want the variable named", origin)
	}

	// A create has nothing to clear, so it registers no removal flags.
	if command.Flags().Lookup("no-secret") != nil {
		t.Error("create registered --no-secret, which has nothing to remove")
	}
}

func TestABadPipeIsReportedRatherThanIgnored(t *testing.T) {
	t.Parallel()

	// Both entry points have to refuse it. A create that swallowed the error
	// and carried on would configure a webhook with no secret, which is the
	// failure that only shows up at the receiving endpoint.
	fields := &Fields{}
	create := &cobra.Command{Use: "create"}
	fields.RegisterCreate(create)
	create.SetIn(strings.NewReader("two words\n"))
	if err := create.ParseFlags([]string{"--secret-stdin"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := fields.CreateInput(create); err == nil {
		t.Error("CreateInput accepted a pipe carrying two words")
	}

	command, updateFields := commandWith(t, "two words\n", "--credentials-password-stdin")
	if _, err := updateFields.UpdateInput(command); err == nil {
		t.Error("UpdateInput accepted a pipe carrying two words")
	}
}

func TestDescribeNamesTheOriginAndNeverTheValue(t *testing.T) {
	t.Parallel()

	secret, password := "s3cr3t", "hookpass"
	target := map[string]any{"name": "hook"}
	Describe(target, webhookfields.UpdateInput{
		SSLVerificationRequired: func() *bool { value := true; return &value }(),
		Secret:                  &secret,
		CredentialsUsername:     "hookuser",
		CredentialsPassword:     &password,
	}, map[string]string{"secret": "$" + SecretEnv, "credentialsPassword": "--credentials-password-stdin"})

	if target["secret"] != "will be set from $"+SecretEnv {
		t.Errorf("secret = %#v", target["secret"])
	}
	if target["credentialsPassword"] != "will be set from --credentials-password-stdin" {
		t.Errorf("credentialsPassword = %#v", target["credentialsPassword"])
	}
	if target["credentialsUsername"] != "hookuser" {
		t.Errorf("credentialsUsername = %#v", target["credentialsUsername"])
	}
	if target["sslVerificationRequired"] != true {
		t.Errorf("sslVerificationRequired = %#v", target["sslVerificationRequired"])
	}

	for key, value := range target {
		if text, ok := value.(string); ok && (strings.Contains(text, secret) || strings.Contains(text, password)) {
			t.Errorf("the preview carried a credential in %q: %q", key, text)
		}
	}
}

func TestDescribeSaysWhenSomethingIsBeingRemoved(t *testing.T) {
	t.Parallel()

	secret := "s3cr3t"
	target := map[string]any{}
	// Clearing wins over a value that arrived from the environment, the same
	// way it does in UpdateInput -- a preview that said "will be set" for a
	// --no-secret run would predict the opposite of what happens.
	Describe(target, webhookfields.UpdateInput{
		Secret:           &secret,
		ClearSecret:      true,
		ClearCredentials: true,
	}, map[string]string{"secret": "$" + SecretEnv})

	if target["secret"] != "will be removed" {
		t.Errorf("secret = %#v", target["secret"])
	}
	if target["credentials"] != "will be removed" {
		t.Errorf("credentials = %#v", target["credentials"])
	}
}

func TestDescribeSaysNothingAboutFieldsNobodyMentioned(t *testing.T) {
	t.Parallel()

	target := map[string]any{"name": "hook"}
	Describe(target, webhookfields.UpdateInput{}, nil)

	if len(target) != 1 {
		t.Errorf("a preview grew fields the command was not given: %#v", target)
	}

	// Called with no target when a command builds its preview differently;
	// panicking there would turn a rendering choice into a crash.
	Describe(nil, webhookfields.UpdateInput{ClearSecret: true}, nil)
}

func TestDescribeCreateIsDescribeWithoutTheRemovals(t *testing.T) {
	t.Parallel()

	secret := "s3cr3t"
	target := map[string]any{}
	DescribeCreate(target, webhookfields.CreateInput{
		Secret:              &secret,
		CredentialsUsername: "hookuser",
	}, map[string]string{"secret": "$" + SecretEnv})

	if target["secret"] != "will be set from $"+SecretEnv {
		t.Errorf("secret = %#v", target["secret"])
	}
	if _, present := target["credentials"]; present {
		t.Error("a create preview talked about removing credentials")
	}
}

func TestWarnRevealedSaysWhatHappenedWithoutRepeatingIt(t *testing.T) {
	t.Parallel()

	written := &bytes.Buffer{}
	WarnRevealed(written, "the webhook's shared secret")

	if !strings.Contains(written.String(), "--reveal-secret") {
		t.Errorf("the warning did not say why a credential was printed: %q", written)
	}
	if !strings.Contains(written.String(), "the webhook's shared secret") {
		t.Errorf("the warning did not say what was printed: %q", written)
	}
}
