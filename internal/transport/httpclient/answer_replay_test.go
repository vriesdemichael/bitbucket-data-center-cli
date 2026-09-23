package httpclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// serialiserRace is what Bitbucket answered a webhook create with in CI,
// verbatim: its serialiser tripped over the configuration it was echoing.
const serialiserRace = `{"errors":[{"message":"(was java.util.ConcurrentModificationException) (through reference chain: com.atlassian.webhooks.internal.rest.RestWebhook[\"configuration\"])","exceptionName":"com.fasterxml.jackson.databind.JsonMappingException"}]}`

type answeringTransport func(*http.Request) (*http.Response, error)

func (transport answeringTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// clientAnswering is a client whose attempts get the next of answers, and the
// last one after them: a 400 when it is the race, a 200 otherwise. It records
// what each attempt sent.
func clientAnswering(answers ...string) (*Client, *atomic.Int32, *[]string) {
	var attempts atomic.Int32
	sent := []string{}

	client := NewFromConfig(config.AppConfig{BitbucketURL: "http://bitbucket.example", RetryCount: 2, RetryBackoff: time.Nanosecond})
	client.http.Transport = answeringTransport(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		sent = append(sent, string(body))

		answer := answers[min(int(attempts.Add(1)), len(answers))-1]
		status := http.StatusOK
		if answer == serialiserRace {
			status = http.StatusBadRequest
		}

		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(answer)), Header: make(http.Header)}, nil
	})

	return client, &attempts, &sent
}

// TestDoRequestSendsAgainWhatBitbucketFailedToAnswer is ADR-011 on the raw
// client. An update whose answer Bitbucket failed to write is sent again, and
// is transient only when every attempt was answered that way. A create in the
// same position is sent once, and its outcome is unknown.
func TestDoRequestSendsAgainWhatBitbucketFailedToAnswer(t *testing.T) {
	t.Parallel()

	request := func(method string) RequestOptions {
		return RequestOptions{Method: method, Path: "/rest/api/latest/projects/P/repos/r/webhooks/1", Body: []byte(`{"name":"renamed"}`)}
	}

	t.Run("an update answered that way once is sent again, as it was", func(t *testing.T) {
		t.Parallel()

		client, attempts, sent := clientAnswering(serialiserRace, `{"id":1}`)
		response, err := client.DoRequest(context.Background(), request(http.MethodPut))
		if err != nil {
			t.Fatalf("DoRequest: %v", err)
		}
		if response.StatusCode != http.StatusOK || attempts.Load() != 2 {
			t.Fatalf("got %d after %d attempts, want the 200 the second attempt was answered with", response.StatusCode, attempts.Load())
		}
		if strings.Join(*sent, "|") != `{"name":"renamed"}|{"name":"renamed"}` {
			t.Fatalf("the attempts sent %q, want the update twice", *sent)
		}
	})

	t.Run("an update answered that way every time is transient", func(t *testing.T) {
		t.Parallel()

		client, attempts, _ := clientAnswering(serialiserRace)
		_, err := client.DoRequest(context.Background(), request(http.MethodPut))
		if !apperrors.IsKind(err, apperrors.KindTransient) || apperrors.ExitCode(err) != 10 {
			t.Fatalf("got %v (exit %d), want transient and exit 10: it was validation, exit 2, for an update Bitbucket had applied",
				err, apperrors.ExitCode(err))
		}
		if attempts.Load() != 3 {
			t.Fatalf("sent %d times, want the first attempt and both retries", attempts.Load())
		}
		if got := apperrors.DetailsOf(err)["upstreamStatus"]; got != "400" {
			t.Fatalf("upstreamStatus = %q, want the 400 Bitbucket answered", got)
		}
	})

	t.Run("a create answered that way is sent once and has an unknown outcome", func(t *testing.T) {
		t.Parallel()

		client, attempts, _ := clientAnswering(serialiserRace)
		_, err := client.DoRequest(context.Background(), request(http.MethodPost))
		if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) || attempts.Load() != 1 {
			t.Fatalf("got %v after %d attempts, want unknown_outcome after one", err, attempts.Load())
		}
	})
}
