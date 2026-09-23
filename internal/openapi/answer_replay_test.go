package openapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// A failed answer to a request bb may send again is sent again. One to a
// request it may not is left for the outcome to report (ADR-011).
func TestRetriableAnswerResendsTheRaceOnlyWhereThatIsSafe(t *testing.T) {
	t.Parallel()

	refusal := `{"errors":[{"message":"Unrecognized field \"nme\"","exceptionName":"com.fasterxml.jackson.databind.JsonMappingException"}]}`
	for _, testCase := range []struct {
		name   string
		method string
		status int
		body   string
		want   bool
	}{
		{"the race answering an update", http.MethodPut, http.StatusBadRequest, serialiserRaceBody, true},
		{"the race answering a read", http.MethodGet, http.StatusBadRequest, serialiserRaceBody, true},
		{"the race answering a delete", http.MethodDelete, http.StatusBadRequest, serialiserRaceBody, true},
		{"the race answering a create", http.MethodPost, http.StatusBadRequest, serialiserRaceBody, false},
		{"a body Bitbucket could not read", http.MethodPut, http.StatusBadRequest, refusal, false},
		{"a status the policy retries", http.MethodGet, http.StatusServiceUnavailable, "", true},
		{"that status answering a create", http.MethodPost, http.StatusServiceUnavailable, "", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := RetriableAnswer(testCase.method, testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("RetriableAnswer(%s, %d) = %v, want %v", testCase.method, testCase.status, got, testCase.want)
			}
		})
	}
}

// answering is a transport that gives each attempt the next of answers, and the
// last one to every attempt after them. An answer is a 400 when it is the race
// Bitbucket answered with in CI, and a 200 otherwise. It records what each
// attempt sent.
func answering(answers ...string) (*retryTransport, *atomic.Int32, *[]string) {
	var attempts atomic.Int32
	sent := []string{}
	transport := &retryTransport{
		base: retryRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(request.Body)
			sent = append(sent, string(body))

			answer := answers[min(int(attempts.Add(1)), len(answers))-1]
			status := http.StatusOK
			if answer == serialiserRaceBody {
				status = http.StatusBadRequest
			}

			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(answer)), Header: make(http.Header)}, nil
		}),
		retries:     2,
		baseBackoff: time.Nanosecond,
	}

	return transport, &attempts, &sent
}

func webhookRequest(t *testing.T, method string) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(context.Background(), method,
		"http://bitbucket.example/rest/api/latest/projects/P/repos/r/webhooks/1", bytes.NewReader([]byte(`{"name":"renamed"}`)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	return request
}

func TestTheRetryTransportSendsAgainWhatBitbucketFailedToAnswer(t *testing.T) {
	t.Parallel()

	t.Run("an update is sent again, as it was", func(t *testing.T) {
		t.Parallel()

		transport, attempts, sent := answering(serialiserRaceBody, `{"id":1}`)
		response, err := transport.RoundTrip(webhookRequest(t, http.MethodPut))
		defer closeResponse(response)
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		if response.StatusCode != http.StatusOK || attempts.Load() != 2 {
			t.Fatalf("got %d after %d attempts, want the 200 the second attempt was answered with", response.StatusCode, attempts.Load())
		}
		if strings.Join(*sent, "|") != `{"name":"renamed"}|{"name":"renamed"}` {
			t.Fatalf("the attempts sent %q, want the update twice", *sent)
		}
	})

	t.Run("the failed answer comes back readable once every attempt got it", func(t *testing.T) {
		t.Parallel()

		transport, attempts, _ := answering(serialiserRaceBody)
		response, err := transport.RoundTrip(webhookRequest(t, http.MethodPut))
		defer closeResponse(response)
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		if attempts.Load() != 3 {
			t.Fatalf("sent %d times, want the first attempt and both retries", attempts.Load())
		}
		// The body decides what the failure is, so whoever reads the response
		// next has to find it there.
		if body, _ := io.ReadAll(response.Body); response.StatusCode != http.StatusBadRequest || string(body) != serialiserRaceBody {
			t.Fatalf("got %d with %q, want the 400 and its body", response.StatusCode, body)
		}
	})

	t.Run("a create is sent once", func(t *testing.T) {
		t.Parallel()

		transport, attempts, _ := answering(serialiserRaceBody)
		response, err := transport.RoundTrip(webhookRequest(t, http.MethodPost))
		defer closeResponse(response)
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		if body, _ := io.ReadAll(response.Body); attempts.Load() != 1 || string(body) != serialiserRaceBody {
			t.Fatalf("sent %d times, answered %q; want once, with the answer untouched", attempts.Load(), body)
		}
	})
}

// TestTheGeneratedClientReportsAFailedAnswerByWhetherItCanBeSentAgain is
// ADR-011 through both halves of the generated client: the transport that
// retries and the doer that classifies.
func TestTheGeneratedClientReportsAFailedAnswerByWhetherItCanBeSentAgain(t *testing.T) {
	t.Parallel()

	t.Run("an update answered that way once succeeds", func(t *testing.T) {
		t.Parallel()

		transport, _, _ := answering(serialiserRaceBody, `{"id":1}`)
		response, err := classifyingDoer{client: &http.Client{Transport: transport}}.Do(webhookRequest(t, http.MethodPut))
		defer closeResponse(response)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("got %v, want the second attempt's 200", err)
		}
	})

	t.Run("an update answered that way every time is transient", func(t *testing.T) {
		t.Parallel()

		transport, _, _ := answering(serialiserRaceBody)
		response, err := classifyingDoer{client: &http.Client{Transport: transport}}.Do(webhookRequest(t, http.MethodPut))
		closeResponse(response)
		if !apperrors.IsKind(err, apperrors.KindTransient) || apperrors.ExitCode(err) != 10 {
			t.Fatalf("got %v (exit %d), want transient and exit 10: it was validation, exit 2, for an update Bitbucket had applied",
				err, apperrors.ExitCode(err))
		}
		if got := apperrors.DetailsOf(err)["upstreamStatus"]; got != "400" {
			t.Fatalf("upstreamStatus = %q, want the 400 Bitbucket answered", got)
		}
	})

	t.Run("a create answered that way has an unknown outcome", func(t *testing.T) {
		t.Parallel()

		transport, attempts, _ := answering(serialiserRaceBody)
		response, err := classifyingDoer{client: &http.Client{Transport: transport}}.Do(webhookRequest(t, http.MethodPost))
		closeResponse(response)
		if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) || attempts.Load() != 1 {
			t.Fatalf("got %v after %d attempts, want unknown_outcome after one", err, attempts.Load())
		}
	})
}
