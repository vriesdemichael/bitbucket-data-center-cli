package webhookfields

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Existing is the webhook a scope already has that a create asks for.
type Existing struct {
	// Webhook is the scope listing's entry for it, which is a read of what
	// Bitbucket holds.
	Webhook any
	// ID is its id.
	ID string
	// PasswordUncompared says the create names an endpoint password, which
	// Bitbucket never returns, so it could not be compared with the stored one.
	PasswordUncompared bool
}

// FindInScope reads a scope's webhooks through list and looks among them for
// the one a create of input would make (see FindExisting).
func FindInScope(ctx context.Context, input CreateInput, list ListPage) (Existing, bool, error) {
	listed, err := openapi.PageThrough(ctx, 0, scopeLimit, list)
	if err != nil {
		return Existing{}, false, err
	}

	return FindExisting(listed, input)
}

// FindExisting looks among a scope's webhooks for the one a create of input
// would make: the same name and URL, and the same settings.
//
// Bitbucket does not refuse a webhook with the name, URL and settings of one it
// has; it stores a second, and both fire. A setup script run twice made two
// webhooks and was told it succeeded both times (#729), so a create that finds
// its webhook already there reports that one instead of adding another.
//
// One with the same name and URL and other settings is not that webhook, and
// changing it is an update the caller did not ask for, so it is a conflict,
// naming each such webhook and what differs. A secret is compared but never
// named; the endpoint password cannot be compared at all, because no read
// returns it, and Existing says so.
func FindExisting(listed []json.RawMessage, input CreateInput) (Existing, bool, error) {
	body, err := NewCreateBody(input)
	if err != nil {
		return Existing{}, false, err
	}

	var differing []string
	for _, raw := range listed {
		var hook storedWebhook
		if json.Unmarshal(raw, &hook) != nil || hook.ID == nil {
			continue
		}
		if hook.Name != valueOf(body.Name) || hook.URL != valueOf(body.Url) {
			continue
		}

		id := strconv.FormatInt(*hook.ID, 10)
		if differences := hook.differencesFrom(body); len(differences) > 0 {
			differing = append(differing, fmt.Sprintf("webhook %s has %s", id, strings.Join(differences, ", ")))
			continue
		}

		// Cannot fail: raw decoded into storedWebhook above.
		var payload any
		_ = json.Unmarshal(raw, &payload)

		return Existing{Webhook: payload, ID: id, PasswordUncompared: input.CredentialsPassword != nil}, true, nil
	}

	if len(differing) > 0 {
		return Existing{}, false, apperrors.New(apperrors.KindConflict, fmt.Sprintf(
			"a webhook named %q calling %s already exists with other settings: %s. Update it by its id, or delete it and create this one",
			valueOf(body.Name), valueOf(body.Url), strings.Join(differing, "; ")), nil)
	}

	return Existing{}, false, nil
}

// storedWebhook is what comparing a listed webhook with a create takes.
type storedWebhook struct {
	ID                      *int64         `json:"id"`
	Name                    string         `json:"name"`
	URL                     string         `json:"url"`
	Events                  []string       `json:"events"`
	Active                  *bool          `json:"active"`
	SSLVerificationRequired *bool          `json:"sslVerificationRequired"`
	Configuration           map[string]any `json:"configuration"`
	Credentials             *struct {
		Username string `json:"username"`
	} `json:"credentials"`
}

// differencesFrom says, field by field, how the webhook differs from what body
// would create.
//
// TLS verification counts only when the create asks for one: a create that
// says nothing takes whatever Bitbucket defaults to, and the webhook there is
// as good a result of that as a new one would be.
func (hook storedWebhook) differencesFrom(body openapigenerated.RestWebhook) []string {
	var differences []string

	if requested := valueOf(body.Events); !sameEvents(hook.Events, requested) {
		differences = append(differences, fmt.Sprintf("events %s rather than %s", sortedList(hook.Events), sortedList(requested)))
	}

	if active := hook.Active == nil || *hook.Active; active != valueOf(body.Active) {
		differences = append(differences, fmt.Sprintf("active %t rather than %t", active, valueOf(body.Active)))
	}

	if requested := body.SslVerificationRequired; requested != nil {
		if hook.SSLVerificationRequired == nil {
			differences = append(differences, fmt.Sprintf("TLS verification not reported rather than %t", *requested))
		} else if *hook.SSLVerificationRequired != *requested {
			differences = append(differences, fmt.Sprintf("TLS verification %t rather than %t", *hook.SSLVerificationRequired, *requested))
		}
	}

	stored, _ := hook.Configuration["secret"].(string)
	var requested string
	if body.Configuration != nil {
		requested, _ = (*body.Configuration)["secret"].(string)
	}
	switch {
	case stored == requested:
	case requested == "":
		differences = append(differences, "a shared secret where the create sets none")
	case stored == "":
		differences = append(differences, "no shared secret where the create sets one")
	default:
		differences = append(differences, "another shared secret")
	}

	var storedUsername, requestedUsername string
	if hook.Credentials != nil {
		storedUsername = hook.Credentials.Username
	}
	if body.Credentials != nil {
		requestedUsername = valueOf(body.Credentials.Username)
	}
	if storedUsername != requestedUsername {
		differences = append(differences, fmt.Sprintf("endpoint username %q rather than %q", storedUsername, requestedUsername))
	}

	return differences
}

// sortedList renders events in a fixed order, since Bitbucket keeps no order.
func sortedList(events []string) string {
	sorted := slices.Clone(events)
	slices.Sort(sorted)
	if len(sorted) == 0 {
		return "(none)"
	}

	return strings.Join(sorted, " ")
}
