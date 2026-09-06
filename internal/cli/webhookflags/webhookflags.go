// Package webhookflags registers the webhook fields that are the same wherever
// a webhook is configured.
//
// A webhook can be created from three places -- `bb webhook`, `bb project
// webhook` and `bb repo settings workflow webhooks` -- and #522 was, in part,
// the observation that they had drifted: the same object, three flag sets, four
// of its fields settable from none of them. Registering the flags from one
// place is what keeps the answer to "can I set the shared secret here" from
// depending on which command the caller reached for.
//
// Two of the fields are credentials, so ADR-047 applies: no flag takes a secret
// as its value. Each secret arrives on stdin behind a --*-stdin flag or in a
// named environment variable, and every one of them reports where it came from
// so a dry run can say a secret will be set without saying what it is.
package webhookflags

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/secretinput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/webhookfields"
)

// SecretEnv and PasswordEnv are where automation puts the two credentials.
//
// The environment rather than a flag, and named here rather than at each call
// site, because a bulk plan refers to a variable by name and the name it refers
// to has to be the one the commands read.
const (
	//nolint:gosec // G101: the name of a variable, not the credential in it
	SecretEnv = "BB_WEBHOOK_SECRET"
	//nolint:gosec // G101: the name of a variable, not the credential in it
	PasswordEnv = "BB_WEBHOOK_PASSWORD"
)

const secretStdinFlag = "secret-stdin"
const passwordStdinFlag = "credentials-password-stdin"

// Fields holds the flag values shared by every webhook create and update.
type Fields struct {
	sslVerification     string
	secretStdin         bool
	noSecret            bool
	credentialsUsername string
	passwordStdin       bool
	noCredentials       bool

	// origins records where each secret came from, for a dry run to report.
	origins map[string]string
}

// RegisterCreate adds the fields a create can set.
//
// --ssl-verification is a three-state string rather than a bool with a default
// because bb does not know Bitbucket's default and should not invent one: a
// create that says nothing about TLS must leave the server's own answer alone.
func (fields *Fields) RegisterCreate(command *cobra.Command) {
	fields.registerCommon(command)
}

// RegisterUpdate adds the same fields plus the two that remove a credential.
//
// Without them a secret set once could never be taken off again through bb,
// because "no secret given" already means "leave the one that is there".
func (fields *Fields) RegisterUpdate(command *cobra.Command) {
	fields.registerCommon(command)
	command.Flags().BoolVar(&fields.noSecret, "no-secret", false, "Remove the shared secret from the webhook")
	command.Flags().BoolVar(&fields.noCredentials, "no-credentials", false, "Remove the endpoint credentials from the webhook")
}

func (fields *Fields) registerCommon(command *cobra.Command) {
	enumflag.RegisterStrict(command.Flags(), &fields.sslVerification, "ssl-verification", "",
		[]string{"true", "false"}, "Whether Bitbucket verifies the endpoint's TLS certificate, left as the server has it when omitted")
	command.Flags().BoolVar(&fields.secretStdin, secretStdinFlag, false,
		"Read the shared secret from stdin (or set "+SecretEnv+")")
	command.Flags().StringVar(&fields.credentialsUsername, "credentials-username", "",
		"Username Bitbucket authenticates to the endpoint with")
	command.Flags().BoolVar(&fields.passwordStdin, passwordStdinFlag, false,
		"Read the endpoint password from stdin (or set "+PasswordEnv+")")
}

// RegisterReveal adds the flag that turns a redaction off.
//
// Publishing a credential has to be an act, not an accident: bb redacts by
// default so that a webhook read in a pipeline cannot put a secret in a log,
// and this is how an operator who came to recover one says so out loud.
func RegisterReveal(command *cobra.Command, target *bool, what string) {
	command.Flags().BoolVar(target, "reveal-secret", false,
		"Print "+what+" instead of redacting it")
}

// WarnRevealed announces on stderr that a credential was written to stdout.
//
// stderr because under --json stdout carries the machine contract and prose
// there makes the envelope unparseable (ADR-047), and because the point of the
// warning is that the reader of the log learns a secret passed through it.
func WarnRevealed(writer io.Writer, what string) {
	fmt.Fprintf(writer, "Warning: %s was printed to stdout in plaintext because --reveal-secret was given.\n", what)
}

// CreateInput resolves the flags into the fields a create sends.
func (fields *Fields) CreateInput(command *cobra.Command) (webhookfields.CreateInput, error) {
	secret, password, err := fields.resolveSecrets(command)
	if err != nil {
		return webhookfields.CreateInput{}, err
	}

	return webhookfields.CreateInput{
		SSLVerificationRequired: fields.sslVerificationValue(command),
		Secret:                  secret,
		CredentialsUsername:     fields.credentialsUsername,
		CredentialsPassword:     password,
	}, nil
}

// UpdateInput resolves the flags into the fields an update changes.
func (fields *Fields) UpdateInput(command *cobra.Command) (webhookfields.UpdateInput, error) {
	secret, password, err := fields.resolveSecrets(command)
	if err != nil {
		return webhookfields.UpdateInput{}, err
	}

	// Removing and setting the same thing in one command is not a preference
	// to resolve, it is a caller who meant one of the two -- but only when
	// both were said out loud. An environment variable is ambient: an
	// automation host that exports BB_WEBHOOK_SECRET for every command would
	// otherwise make --no-secret impossible to use at all, which is how the
	// first version of this refusal behaved. A flag typed on this invocation
	// outranks a variable set somewhere else.
	if fields.noSecret {
		if fields.secretStdin {
			return webhookfields.UpdateInput{}, apperrors.New(apperrors.KindValidation,
				"--no-secret cannot be combined with --"+secretStdinFlag+": pick removing the secret or setting it", nil)
		}
		secret = nil
		delete(fields.origins, "secret")
	}
	if fields.noCredentials {
		if fields.passwordStdin || fields.credentialsUsername != "" {
			return webhookfields.UpdateInput{}, apperrors.New(apperrors.KindValidation,
				"--no-credentials cannot be combined with --credentials-username or --"+passwordStdinFlag+
					": pick removing the credentials or setting them", nil)
		}
		password = nil
		delete(fields.origins, "credentialsPassword")
	}

	return webhookfields.UpdateInput{
		SSLVerificationRequired: fields.sslVerificationValue(command),
		Secret:                  secret,
		ClearSecret:             fields.noSecret,
		CredentialsUsername:     fields.credentialsUsername,
		CredentialsPassword:     password,
		ClearCredentials:        fields.noCredentials,
	}, nil
}

// Origins describes where each credential came from, for a dry run.
//
// Names, never values: "$BB_WEBHOOK_SECRET" is what a preview can print, and
// it is also the more useful thing to print, because what a caller checks
// before applying is that the plan reads the variable they meant.
func (fields *Fields) Origins() map[string]string {
	described := make(map[string]string, len(fields.origins))
	for field, origin := range fields.origins {
		described[field] = origin
	}

	return described
}

// Describe adds the shared fields to a dry-run target, describing the secrets
// rather than carrying them.
//
// A preview is written to stdout, is the thing a caller reads before applying,
// and is the thing they paste into a ticket when it looks wrong. What it has to
// answer is "will this set the secret, and from where" -- so it names the
// origin, "$BB_WEBHOOK_SECRET" or the stdin flag, and never the value. Naming
// the variable is also the more useful answer: the mistake a plan makes is
// reading the wrong one.
func Describe(target map[string]any, input webhookfields.UpdateInput, origins map[string]string) {
	if target == nil {
		return
	}

	if input.SSLVerificationRequired != nil {
		target["sslVerificationRequired"] = *input.SSLVerificationRequired
	}
	switch {
	case input.ClearSecret:
		target["secret"] = "will be removed"
	case input.Secret != nil:
		target["secret"] = "will be set from " + origins["secret"]
	}
	if input.CredentialsUsername != "" {
		target["credentialsUsername"] = input.CredentialsUsername
	}
	switch {
	case input.ClearCredentials:
		target["credentials"] = "will be removed"
	case input.CredentialsPassword != nil:
		target["credentialsPassword"] = "will be set from " + origins["credentialsPassword"]
	}
}

// DescribeCreate is Describe for a create, which has nothing to clear.
func DescribeCreate(target map[string]any, input webhookfields.CreateInput, origins map[string]string) {
	Describe(target, webhookfields.UpdateInput{
		SSLVerificationRequired: input.SSLVerificationRequired,
		Secret:                  input.Secret,
		CredentialsUsername:     input.CredentialsUsername,
		CredentialsPassword:     input.CredentialsPassword,
	}, origins)
}

func (fields *Fields) sslVerificationValue(command *cobra.Command) *bool {
	if !command.Flags().Changed("ssl-verification") {
		return nil
	}

	// enumflag has already refused anything but true or false, at parse time.
	value := fields.sslVerification == "true"

	return &value
}

// resolveSecrets reads both credentials, refusing the case where they would
// have to share one stdin.
func (fields *Fields) resolveSecrets(command *cobra.Command) (*string, *string, error) {
	if fields.secretStdin && fields.passwordStdin {
		return nil, nil, apperrors.New(apperrors.KindValidation,
			"--"+secretStdinFlag+" and --"+passwordStdinFlag+" cannot both be used: stdin carries one secret. "+
				"Put the other in "+SecretEnv+" or "+PasswordEnv+".", nil)
	}

	fields.origins = map[string]string{}

	// The command's reader, never os.Stdin directly: a command under test is
	// given its own, and an ungated fallback to the process's stdin is what
	// ADR-073 forbids. Nil here becomes "stdin is not available", which is the
	// honest answer when a flag asked for a pipe that does not exist.
	var reader io.Reader
	if command != nil {
		reader = command.InOrStdin()
	}

	secret, err := secretinput.Resolve(fields.secretStdin, reader, "--"+secretStdinFlag, SecretEnv,
		"printf '%s' \"$SECRET\" | bb webhook create <name> <url> --"+secretStdinFlag)
	if err != nil {
		return nil, nil, err
	}

	password, err := secretinput.Resolve(fields.passwordStdin, reader, "--"+passwordStdinFlag, PasswordEnv,
		"printf '%s' \"$PASSWORD\" | bb webhook create <name> <url> --"+passwordStdinFlag)
	if err != nil {
		return nil, nil, err
	}

	var secretValue, passwordValue *string
	if secret.Given {
		secretValue = &secret.Value
		fields.origins["secret"] = secret.Origin
	}
	if password.Given {
		passwordValue = &password.Value
		fields.origins["credentialsPassword"] = password.Origin
	}

	return secretValue, passwordValue, nil
}
