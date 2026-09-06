package bulk

import (
	"strings"
	"testing"
)

// A plan is a file: written to disk, committed, attached to a change request
// and read by whoever reviews it. These cover the rules that keep a credential
// out of it -- what a secretEnv is allowed to contain, what a plan prints about
// one, and what happens at apply time when the variable it names is not there.
// The live suite drives the whole path; this reaches the branches one policy
// cannot hold at once.

func TestSecretEnvMustNameAVariableRatherThanHoldOne(t *testing.T) {
	t.Parallel()

	// The mistake the field invites is reading "secretEnv" as "the secret". A
	// real credential almost never survives this pattern: it carries
	// punctuation, or base64's = and /, or spaces.
	for _, pasted := range []string{
		"hunter2/not-a-name=",
		"s3cr3t value",
		"with-dashes",
		"1LEADING_DIGIT",
		"$BB_WEBHOOK_SECRET",
	} {
		normalized, problems := normalizeOperation(OperationSpec{
			Type:      OperationRepoWebhookCreate,
			Name:      "hook",
			URL:       "https://example.com",
			SecretEnv: pasted,
		})
		_ = normalized
		if len(problems) == 0 {
			t.Errorf("secretEnv %q was accepted as a variable name", pasted)
			continue
		}
		if !strings.Contains(strings.Join(problems, " "), "secretEnv") {
			t.Errorf("the refusal for %q did not name the field: %v", pasted, problems)
		}
	}
}

func TestCredentialsPasswordEnvIsCheckedTheSameWay(t *testing.T) {
	t.Parallel()

	// The second variable is as easy to paste a password into as the first,
	// and a check applied to one field only is the shape of gap #522 was
	// counting in the first place.
	_, problems := normalizeOperation(OperationSpec{
		Type:                   OperationRepoWebhookCreate,
		Name:                   "hook",
		URL:                    "https://example.com",
		CredentialsPasswordEnv: "hunter2/not-a-name=",
	})
	if len(problems) == 0 {
		t.Fatal("a pasted password was accepted as a variable name")
	}
	if !strings.Contains(strings.Join(problems, " "), "credentialsPasswordEnv") {
		t.Errorf("the refusal did not name the field: %v", problems)
	}
}

func TestSecretEnvAcceptsAVariableName(t *testing.T) {
	t.Parallel()

	normalized, problems := normalizeOperation(OperationSpec{
		Type:                   OperationRepoWebhookCreate,
		Name:                   "hook",
		URL:                    "https://example.com",
		SecretEnv:              "  BB_WEBHOOK_SECRET  ",
		CredentialsUsername:    "  hookuser  ",
		CredentialsPasswordEnv: "_private",
	})
	if len(problems) != 0 {
		t.Fatalf("a valid policy was refused: %v", problems)
	}

	if normalized.SecretEnv != "BB_WEBHOOK_SECRET" {
		t.Errorf("secretEnv = %q, want it trimmed", normalized.SecretEnv)
	}
	if normalized.CredentialsUsername != "hookuser" {
		t.Errorf("credentialsUsername = %q", normalized.CredentialsUsername)
	}
	if normalized.CredentialsPasswordEnv != "_private" {
		t.Errorf("credentialsPasswordEnv = %q", normalized.CredentialsPasswordEnv)
	}
}

func TestAPolicyThatNamesNoVariableIsStillAValidPolicy(t *testing.T) {
	t.Parallel()

	// Most webhooks have no secret, and requiring one would make the operation
	// unusable for them.
	if _, problems := normalizeOperation(OperationSpec{
		Type: OperationRepoWebhookCreate,
		Name: "hook",
		URL:  "https://example.com",
	}); len(problems) != 0 {
		t.Errorf("a webhook without a secret was refused: %v", problems)
	}
}

func TestDescribeOperationNamesTheVariableThePlanWillRead(t *testing.T) {
	t.Parallel()

	// The description is what a plan prints. What it has to answer is which
	// variable the apply reads, because naming the wrong one is the mistake a
	// plan makes -- and it must answer that without holding the value.
	described := DescribeOperation(OperationSpec{
		Type:                   OperationRepoWebhookCreate,
		Name:                   "ci",
		SecretEnv:              "BB_WEBHOOK_SECRET",
		CredentialsPasswordEnv: "BB_WEBHOOK_PASSWORD",
	})

	for _, want := range []string{"create webhook ci", "$BB_WEBHOOK_SECRET", "$BB_WEBHOOK_PASSWORD"} {
		if !strings.Contains(described, want) {
			t.Errorf("description %q does not mention %q", described, want)
		}
	}

	plain := DescribeOperation(OperationSpec{Type: OperationRepoWebhookCreate, Name: "ci"})
	if plain != "create webhook ci" {
		t.Errorf("a webhook with no secret described as %q", plain)
	}
}

func TestSecretFromEnvRefusesAVariableThatIsNotThere(t *testing.T) {
	t.Setenv("BB_TEST_PLAN_SECRET", "")

	// Rather than creating the webhook without it: a webhook whose deliveries
	// carry no signature is not a smaller version of one whose deliveries do,
	// and finding that out from a receiver that has started rejecting
	// everything is a bad way to find out.
	_, err := secretFromEnv("BB_TEST_PLAN_SECRET", "secretEnv")
	if err == nil {
		t.Fatal("an unset variable was accepted")
	}
	// The message has to name both, because a plan applied on a machine where
	// the variable was never exported is the whole failure mode.
	if !strings.Contains(err.Error(), "secretEnv") || !strings.Contains(err.Error(), "BB_TEST_PLAN_SECRET") {
		t.Errorf("the failure named neither the field nor the variable: %v", err)
	}
}

func TestSecretFromEnvReadsTheVariableAtApplyTime(t *testing.T) {
	t.Setenv("BB_TEST_PLAN_SECRET", "s3cr3t")

	secret, err := secretFromEnv("BB_TEST_PLAN_SECRET", "secretEnv")
	if err != nil {
		t.Fatalf("secretFromEnv: %v", err)
	}
	if secret == nil || *secret != "s3cr3t" {
		t.Errorf("secret = %v", secret)
	}

	// A plan that named nothing wants nothing, which is not a failure.
	unnamed, err := secretFromEnv("   ", "secretEnv")
	if err != nil || unnamed != nil {
		t.Errorf("an unnamed variable produced %v, %v", unnamed, err)
	}
}
