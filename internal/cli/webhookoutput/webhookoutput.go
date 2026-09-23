// Package webhookoutput reports a webhook the same way from each of the three
// command groups that configure one.
//
// `bb webhook`, `bb project webhook` and `bb repo settings workflow webhooks`
// write one object through two routes. A create or an update in any of them
// publishes the webhook as it was read back after the write, and shows a person
// the fields `bb webhook get` shows -- the shared secret line among them, which
// is what someone who just set a secret comes to check.
package webhookoutput

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/webhookfields"
)

// Published is the webhook a create or an update left, as a command publishes
// it. write names the write -- "create" or "update" -- for the warning.
//
// A webhook that was not read back is still published and the command still
// succeeds: the write happened, and a failure would send the caller to repeat
// it (see webhookfields.ReadBack). The difference is told on stderr, in both
// output modes. Under --json stdout is the machine contract, and whoever reads
// the log is who needs to know that what it shows is the write's answer.
func Published(stderr io.Writer, written webhookfields.Written, write string) result.Webhook {
	hook := result.WebhookFrom(written.Webhook)
	if written.Unread == nil {
		return hook
	}

	subject := "the webhook"
	if hook.ID != 0 {
		subject = "webhook " + strconv.Itoa(hook.ID)
	}
	fmt.Fprintln(stderr, style.Warning.Render(fmt.Sprintf(
		"Warning: %s was not read back after the %s (%v), so what is shown is Bitbucket's answer to the %s; "+
			"whether a shared secret is configured is taken from what the %s sent.",
		subject, write, written.Unread, write, write)))

	return hook
}

// Detail renders one webhook for a person.
//
// Every field the model carries, including the two that say whether a
// credential is configured without saying what it is. The secret and the
// endpoint password are the reason this exists rather than a JSON dump.
func Detail(writer io.Writer, hook result.Webhook) {
	// An id the server did not send is left out rather than printed as 0,
	// which the commands that take an id would read as one.
	if hook.ID != 0 {
		fmt.Fprintf(writer, "%s %s\n", style.Label.Render("ID:"), style.Secondary.Render(strconv.Itoa(hook.ID)))
	}
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Name:"), hook.Name)
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("URL:"), hook.URL)
	fmt.Fprintf(writer, "%s %t\n", style.Label.Render("Active:"), hook.Active)
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Events:"), strings.Join(hook.Events, ", "))
	if hook.ScopeType != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Scope:"), hook.ScopeType)
	}
	// Absent is its own answer: the server did not say, which is not the same
	// as saying no.
	verification := "not reported"
	if hook.SSLVerificationRequired != nil {
		verification = strconv.FormatBool(*hook.SSLVerificationRequired)
	}
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("SSL verification required:"), verification)
	fmt.Fprintf(writer, "%s %t\n", style.Label.Render("Shared secret configured:"), hook.SecretConfigured)
	if hook.CredentialsUsername != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Endpoint credentials username:"), hook.CredentialsUsername)
	}
}
