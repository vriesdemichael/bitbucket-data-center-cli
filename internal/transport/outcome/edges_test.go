package outcome_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/outcome"
)

// The edges of classification that need no connection: what a read passes
// through untouched, and what a request with nothing specified is taken to be.

func TestAReadThatDidNotFailIsLeftAlone(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodPost, "http://bitbucket.example", nil)
	_, exchange := outcome.Track(request)

	if err := exchange.ClassifyRead(nil); err != nil {
		t.Fatalf("a clean read was classified: %v", err)
	}
	// io.EOF is how a body says it is finished, not a failure.
	if err := exchange.ClassifyRead(io.EOF); !errors.Is(err, io.EOF) || apperrors.KindOf(err) != apperrors.KindInternal {
		t.Fatalf("the end of a body was classified: %v", err)
	}
	already := apperrors.New(apperrors.KindPermanent, "decided further down", nil)
	if err := exchange.ClassifyRead(already); !errors.Is(err, already) {
		t.Fatalf("an error something had already classified was classified again: %v", err)
	}
}

func TestAnInterruptedReadOfARequestThatIsReplayedIsCancelled(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "http://bitbucket.example", nil)
	_, exchange := outcome.Track(request)

	assertKind(t, exchange.ClassifyRead(context.Canceled), apperrors.KindCancelled, "interrupted")
}

func TestNoBodyStaysNoBody(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "http://bitbucket.example", nil)
	_, exchange := outcome.Track(request)

	if body := exchange.Body(nil); body != nil {
		t.Fatalf("a missing body became %T", body)
	}
}

// A request that names no method is a GET, as net/http sends it, so it is
// classified as the replayable request it is.
func TestARequestThatNamesNoMethodIsAGet(t *testing.T) {
	t.Parallel()

	request := (&http.Request{}).WithContext(context.Background())
	_, exchange := outcome.Track(request)

	if err := exchange.Status(http.StatusGatewayTimeout, nil); err != nil {
		t.Fatalf("a gateway's 504 to a request with no method was treated as a lost mutation: %v", err)
	}
}
