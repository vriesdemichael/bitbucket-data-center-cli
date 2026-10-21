package preflight

import (
	"context"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
)

// CommentDeletion is what deleting a comment would do, for --dry-run: delete
// it, or be blocked, with why, and the kind of error the real run fails with.
type CommentDeletion struct {
	Predicted string
	Reason    string
	Blocking  []string
	Fails     apperrors.Kind
}

// PredictCommentDeletion reads the comment and says what Bitbucket does with a
// delete of it. bb repo comment delete and bb pr comment delete both ask it,
// so the two cannot come to predict different things.
//
// Bitbucket lets the author delete a comment, and anyone who administers the
// repository: a repository admin deleting somebody else's comment succeeds, so
// being somebody else is only a refusal without admin, which Bitbucket answers
// with a 401 AuthorisationException. It asks that first, and only then refuses
// a comment that still has replies, with a 409 CommentDeletionException.
func PredictCommentDeletion[C RepoChecker](
	ctx context.Context,
	newChecker func(*openapigenerated.ClientWithResponses) C,
	client *openapigenerated.ClientWithResponses,
	service *commentservice.Service,
	target commentservice.Target,
	commentID string,
	currentUser string,
) (CommentDeletion, error) {
	prediction := CommentDeletion{
		Predicted: "delete",
		Reason:    "comment will be deleted",
		Blocking:  []string{},
		Fails:     apperrors.KindConflict,
	}

	current, err := service.Get(ctx, target, commentID)
	if err != nil {
		if apperrors.ExitCode(err) == 4 {
			// Deleting a comment that is not there is a 404.
			prediction.Predicted = "blocked"
			prediction.Reason = "comment was not found"
			prediction.Fails = apperrors.KindNotFound

			return prediction, nil
		}

		return CommentDeletion{}, err
	}

	if user := strings.TrimSpace(currentUser); user != "" && !CommentOwnedBy(current, user) {
		adminErr := RepoPermission(ctx, newChecker, client, target.Repository.ProjectKey, target.Repository.Slug, openapi.RepoAdmin)
		switch {
		case adminErr == nil:
		case apperrors.IsKind(adminErr, apperrors.KindAuthorization):
			prediction.Predicted = "blocked"
			prediction.Reason = "comment is owned by another user, and only its author or a repository admin may delete it"
			prediction.Blocking = []string{prediction.Reason}
			prediction.Fails = apperrors.KindAuthorization
		default:
			return CommentDeletion{}, adminErr
		}
	}
	if prediction.Predicted == "delete" && current.Comments != nil && len(*current.Comments) > 0 {
		prediction.Predicted = "blocked"
		prediction.Reason = "comment has replies, which have to be deleted first"
		prediction.Blocking = []string{prediction.Reason}
	}

	return prediction, nil
}

// CommentOwnedBy reports whether username wrote the comment, by the author's
// name or slug.
func CommentOwnedBy(comment openapigenerated.RestComment, username string) bool {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" || comment.Author == nil {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(comment.Author.Name), trimmed) ||
		strings.EqualFold(strings.TrimSpace(comment.Author.Slug), trimmed)
}
