package outcome

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// An error Bitbucket raised while writing its answer may come after it applied
// the request. That is an unknown outcome for a mutation bb does not send twice,
// and transient for a request the retry policy replays (ADR-011): sending that
// again is safe, and is how the caller finds out what became of it.
func TestAnAnswerBitbucketFailedToWriteLeavesOnlyAMutationUnknown(t *testing.T) {
	t.Parallel()

	var cause error = apperrors.New(apperrors.KindValidation, "bitbucket API returned 400: (was java.util.ConcurrentModificationException)", nil)
	cause = apperrors.WithDetail(cause, "upstreamStatus", "400")
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

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		failed := exchangeFor(method).AnswerFailed(cause)
		if !apperrors.IsKind(failed, apperrors.KindTransient) || apperrors.ExitCode(failed) != 10 {
			t.Fatalf("%s: got %v, want transient, exit 10, since it is safe to send again", method, failed)
		}
		// The 400 was read as validation, the kind this corrects, so it must
		// not print inside the message as one. What Bitbucket answered stays.
		if message := apperrors.MessageOf(failed); strings.Contains(message, "validation") || !strings.Contains(message, "ConcurrentModificationException") {
			t.Fatalf("%s: message %q, want what Bitbucket answered and not the kind it was misread as", method, message)
		}
		if got := apperrors.DetailsOf(failed)["upstreamStatus"]; got != "400" {
			t.Fatalf("%s: upstreamStatus detail = %q, want the 400's kept", method, got)
		}
	}
}
