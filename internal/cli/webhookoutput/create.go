package webhookoutput

import (
	"fmt"
	"io"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/webhookfields"
)

// PreviewCreate fills in what a create would come to, from what the scope
// holds (see webhookfields.FindExisting): nothing, when the webhook is there
// already; a conflict, when its name and URL are taken by one with other
// settings; and otherwise the create.
//
// found and err are FindExisting's. An error that is not the conflict is the
// preview's own failure, and is returned.
func PreviewCreate(item dryrunpreview.Item, existing webhookfields.Existing, found bool, err error) (dryrunpreview.Item, error) {
	item.Tier = dryrunpreview.TierPreconditionsChecked

	switch {
	case apperrors.IsKind(err, apperrors.KindConflict):
		item.PredictedAction = dryrunpreview.PredictedConflict
		item.Reason = apperrors.MessageOf(err)
		item.BlockingReasons = []string{"a webhook with this name and URL already exists with other settings"}
	case err != nil:
		return item, err
	case found:
		item.PredictedAction = dryrunpreview.PredictedNoop
		item.Reason = fmt.Sprintf("webhook %s already exists with this name, URL and settings, so nothing will be created", existing.ID)
		if existing.PasswordUncompared {
			item.Reason += "; the endpoint password was not compared, because Bitbucket does not return it"
		}
	default:
		item.PredictedAction = dryrunpreview.PredictedCreate
		item.Reason = "webhook will be created"
	}

	return item, nil
}

// NoteExisting tells a person, on stderr, that a create found its webhook
// already there with the endpoint password it was given left uncompared. The
// create changed nothing, so a new password it carried was not set, and saying
// so is all that stops a caller from believing it was.
func NoteExisting(stderr io.Writer, written webhookfields.Written, hook result.Webhook) {
	if !written.Existing || !written.PasswordUncompared {
		return
	}

	fmt.Fprintln(stderr, style.Warning.Render(fmt.Sprintf(
		"Note: webhook %d was already there with this name, URL and settings, and was left as it is. "+
			"Bitbucket does not return a webhook's endpoint password, so the one given was not compared with it; "+
			"update the webhook to set it.", hook.ID)))
}
