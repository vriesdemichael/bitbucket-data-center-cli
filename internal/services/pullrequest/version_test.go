package pullrequest

import (
	"errors"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// upstreamError is a failure carrying what the transport attaches to one
// Bitbucket answered: the status and the exception name. That the transport
// really attaches them to a stale draft change is pinned live, in
// TestLivePRDraftChangeWithAStaleVersionNamesTheException; this only drives the
// decision made from them.
func upstreamError(status, exception string) error {
	var err error = apperrors.New(apperrors.KindConflict, "bitbucket API returned "+status, nil)
	err = apperrors.WithDetail(err, "upstreamStatus", status)

	return apperrors.WithDetail(err, "upstreamException", exception)
}

const illegalStateException = "com.atlassian.bitbucket.pull.IllegalPullRequestStateException"

func TestRetryOnceWhenStale(t *testing.T) {
	t.Parallel()

	stale := upstreamError("409", pullRequestOutOfDateException)
	declined := upstreamError("409", illegalStateException)

	// run drives the decision with a write that answers from outcomes in turn
	// and a re-read that reports version 1. It returns the version each write
	// was handed, how many re-reads there were, and what the decision returned.
	run := func(outcomes []error, rereadErr error) ([]int, int, error) {
		var sent []int
		rereads := 0

		err := retryOnceWhenStale(PullRequest{Version: 0},
			func() (PullRequest, error) {
				rereads++
				return PullRequest{Version: 1}, rereadErr
			},
			func(current PullRequest) error {
				sent = append(sent, current.Version)
				if len(sent) > len(outcomes) {
					return nil
				}
				return outcomes[len(sent)-1]
			},
		)

		return sent, rereads, err
	}

	t.Run("a write that lands is not repeated", func(t *testing.T) {
		t.Parallel()

		sent, rereads, err := run([]error{nil}, nil)
		if err != nil || len(sent) != 1 || rereads != 0 {
			t.Fatalf("err=%v sent=%v rereads=%d, want one write and no re-read", err, sent, rereads)
		}
	})

	t.Run("a stale version is read again and sent once more", func(t *testing.T) {
		t.Parallel()

		sent, rereads, err := run([]error{stale, nil}, nil)
		if err != nil {
			t.Fatalf("expected the retry to recover, got %v", err)
		}
		if len(sent) != 2 || sent[0] != 0 || sent[1] != 1 || rereads != 1 {
			t.Fatalf("sent=%v rereads=%d, want the re-read version on the second write", sent, rereads)
		}
	})

	t.Run("stale twice is reported rather than retried again", func(t *testing.T) {
		t.Parallel()

		sent, _, err := run([]error{stale, stale, stale}, nil)
		if !isStaleVersion(err) {
			t.Fatalf("expected the second conflict to be reported, got %v", err)
		}
		if len(sent) != 2 {
			t.Fatalf("sent=%v, want exactly two writes", sent)
		}
	})

	t.Run("another conflict is not retried", func(t *testing.T) {
		t.Parallel()

		sent, rereads, err := run([]error{declined}, nil)
		if !errors.Is(err, declined) || len(sent) != 1 || rereads != 0 {
			t.Fatalf("err=%v sent=%v rereads=%d, want the refusal reported untouched", err, sent, rereads)
		}
	})

	t.Run("a write whose outcome is unknown is not retried", func(t *testing.T) {
		t.Parallel()

		lost := errors.New("connection reset by peer")
		sent, rereads, err := run([]error{lost}, nil)
		if !errors.Is(err, lost) || len(sent) != 1 || rereads != 0 {
			t.Fatalf("err=%v sent=%v rereads=%d, want the failure reported untouched", err, sent, rereads)
		}
	})

	t.Run("a failed re-read reports the conflict", func(t *testing.T) {
		t.Parallel()

		sent, _, err := run([]error{stale}, errors.New("read failed"))
		if !errors.Is(err, stale) || len(sent) != 1 {
			t.Fatalf("err=%v sent=%v, want the original conflict and no second write", err, sent)
		}
	})
}

func TestIsStaleVersionReadsTheExceptionNotJustTheStatus(t *testing.T) {
	t.Parallel()

	if isStaleVersion(nil) {
		t.Error("nil is not a stale version")
	}
	if !isStaleVersion(upstreamError("409", pullRequestOutOfDateException)) {
		t.Error("a 409 naming PullRequestOutOfDateException is a stale version")
	}
	if isStaleVersion(upstreamError("409", illegalStateException)) {
		t.Error("a 409 naming another exception is not a stale version")
	}
	if isStaleVersion(upstreamError("400", pullRequestOutOfDateException)) {
		t.Error("the exception name under a status other than 409 is not a stale version")
	}
	if isStaleVersion(apperrors.New(apperrors.KindConflict, "a conflict nobody named", nil)) {
		t.Error("a conflict without upstream details is not a stale version")
	}
}
