package outcome

import (
	"context"
	"errors"
	"net/http"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// An error Bitbucket raised while writing its answer may come after it applied
// the request. That is an unknown outcome for a mutation, and nothing lost for
// a request the retry policy replays.
func TestAnAnswerBitbucketFailedToWriteLeavesOnlyAMutationUnknown(t *testing.T) {
	t.Parallel()

	cause := apperrors.New(apperrors.KindValidation, "bitbucket API returned 400: (was java.util.ConcurrentModificationException)", nil)
	exchangeFor := func(method string) *Exchange {
		request, err := http.NewRequestWithContext(context.Background(), method, "http://bitbucket.example/rest/api/latest/projects", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		_, exchange := Track(request)
		return exchange
	}

	failed := exchangeFor(http.MethodPost).AnswerFailed(cause)
	if !apperrors.IsKind(failed, apperrors.KindUnknownOutcome) || !errors.Is(failed, cause) {
		t.Fatalf("POST: got %v, want unknown_outcome carrying the mapped error", failed)
	}

	if failed := exchangeFor(http.MethodGet).AnswerFailed(cause); failed != nil {
		t.Fatalf("GET: got %v, want nothing, since a read is replayed", failed)
	}
}
